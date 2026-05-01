package physics

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// AtmosphericDensity returns ρ at altitude h above primary's surface
// using the body's exponential-density model:
// ρ(h) = ρ₀ · exp(-h / H) for h < cutoff, 0 above. Returns 0 when the
// primary has no atmosphere.
func AtmosphericDensity(primary bodies.CelestialBody, altitude float64) float64 {
	atm := primary.Atmosphere
	if atm == nil || atm.SurfaceDensity <= 0 || atm.ScaleHeight <= 0 {
		return 0
	}
	if altitude < 0 {
		// Craft below the surface is a numerically degenerate state
		// (would-be terrain collision; real impact handling is
		// deferred to v0.9+). Clamp drag to zero so the integrator
		// doesn't blow up while the state is invalid.
		return 0
	}
	if altitude >= atm.CutoffAltitude {
		return 0
	}
	return atm.SurfaceDensity * math.Exp(-altitude/atm.ScaleHeight)
}

// AtmosphereOmega returns the body's spin angular-velocity vector (rad/s)
// in the inertial frame the simulation uses (Z is the orbital-pole axis;
// the ecliptic-plane assumption from elsewhere in the codebase carries
// over here so the atmosphere co-rotates about +Z). Returns zero when
// SideralRotation isn't populated. SideralRotation is stored in hours
// (sidereal day length) per the JSON catalog convention.
func AtmosphereOmega(primary bodies.CelestialBody) orbital.Vec3 {
	if primary.SideralRotation == 0 {
		return orbital.Vec3{}
	}
	periodSec := primary.SideralRotation * 3600
	if periodSec == 0 {
		return orbital.Vec3{}
	}
	return orbital.Vec3{Z: 2 * math.Pi / periodSec}
}

// DragAccel returns the atmospheric-drag acceleration vector on a craft
// with state (r, v) relative to primary, given a ballistic coefficient
// BC = C_D · A / m (m²/kg). Returns zero outside the body's atmosphere
// (no Atmosphere defined, altitude above cutoff, or BC ≤ 0).
//
// Drag direction opposes the craft's velocity *relative to the
// atmosphere*: v_rel = v - ω × r where ω is the body's spin vector.
// That distinction matters for low-altitude flight — Earth's surface
// rotates ~465 m/s eastward at the equator, so a craft sitting at LEO
// orbital speed (~7800 m/s) feels drag against ~7335 m/s of relative
// flow, not the full inertial speed.
//
// Magnitude: a = 0.5 · ρ · |v_rel|² · BC, applied along -v̂_rel.
// v0.8.4+.
func DragAccel(r, v orbital.Vec3, primary bodies.CelestialBody, bc float64) orbital.Vec3 {
	if bc <= 0 || primary.Atmosphere == nil {
		return orbital.Vec3{}
	}
	rMag := r.Norm()
	if rMag == 0 {
		return orbital.Vec3{}
	}
	altitude := rMag - primary.RadiusMeters()
	rho := AtmosphericDensity(primary, altitude)
	if rho == 0 {
		return orbital.Vec3{}
	}
	vRel := v.Sub(AtmosphereOmega(primary).Cross(r))
	vRelMag := vRel.Norm()
	if vRelMag == 0 {
		return orbital.Vec3{}
	}
	// |a| = 0.5 ρ v² BC, direction = -v̂_rel.
	// Combine the unit-vector division and magnitude into one scale
	// factor: -(0.5 · ρ · v · BC) so we multiply by vRel directly.
	factor := -0.5 * rho * vRelMag * bc
	return vRel.Scale(factor)
}
