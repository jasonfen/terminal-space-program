package physics

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// SOIRadius returns the sphere-of-influence radius of `body` orbiting
// `primary`, in meters. SOI = a_body * (m_body / m_primary)^(2/5).
// Returns 0 for bodies without orbital data (e.g. the system primary itself).
func SOIRadius(body, primary bodies.CelestialBody) float64 {
	if body.SemimajorAxis == 0 || primary.MassKg() == 0 {
		return 0
	}
	return body.SemimajorAxisMeters() * math.Pow(body.MassKg()/primary.MassKg(), 0.4)
}

// Primary captures which body the spacecraft is currently bound to and
// its inertial position at the relevant sim-time. Physics operates
// relative to the primary; world code looks this up every few ticks.
type Primary struct {
	Body     bodies.CelestialBody
	Inertial orbital.Vec3
}

// FindPrimary picks the best primary for a spacecraft at inertial position
// rInertial. Rule: smallest SOI that contains the spacecraft wins; default
// to the system primary if none contain it.
//
// Each body's SOI is computed against its actual parent (v0.5.0+):
// planets against the system primary, moons against their planet (per
// CelestialBody.ParentID). Pre-v0.5.0 every body's SOI was computed
// against the system primary, which silently treated moons as planets
// — a Luna at 384k km from Earth had its SOI sized as if it orbited
// the Sun at 384k km, an absurd value. Fixing this is what enables
// the Earth-and-Luna nested-SOI walk.
func FindPrimary(
	system bodies.System,
	rInertial orbital.Vec3,
	positions map[string]orbital.Vec3, // body.ID → inertial position (m)
) Primary {
	primary := system.Bodies[0]
	best := Primary{Body: primary, Inertial: orbital.Vec3{}}

	bestSOI := math.Inf(1)
	for i := 1; i < len(system.Bodies); i++ {
		b := system.Bodies[i]
		bPos, ok := positions[b.ID]
		if !ok {
			continue
		}
		parent := system.ParentOf(b)
		if parent == nil {
			parent = &primary
		}
		soi := SOIRadius(b, *parent)
		if soi == 0 {
			continue
		}
		d := rInertial.Sub(bPos).Norm()
		if d <= soi && soi < bestSOI {
			best = Primary{Body: b, Inertial: bPos}
			bestSOI = soi
		}
	}
	return best
}

// Rebase converts a state vector expressed relative to oldPrimary into
// one expressed relative to newPrimary. Both primaries' inertial positions
// are required. Velocity transforms by vector subtraction only if the
// primaries have non-zero relative velocity; in v0.1 planets are on fixed
// Keplerian tracks with velocities computed on demand, so we accept a
// relative-velocity parameter (dvInertial = v_oldPrimary - v_newPrimary).
func Rebase(
	s StateVector,
	oldPrimaryInertial, newPrimaryInertial orbital.Vec3,
	dvInertial orbital.Vec3,
) StateVector {
	// Position: r' = r + (old_inertial - new_inertial).
	dR := oldPrimaryInertial.Sub(newPrimaryInertial)
	// Velocity: v' = v + (v_old - v_new).
	return StateVector{
		R: s.R.Add(dR),
		V: s.V.Add(dvInertial),
		M: s.M,
	}
}
