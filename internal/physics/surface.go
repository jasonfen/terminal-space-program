package physics

import (
	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// ClampToSurface tests whether the state has crossed below the primary's
// mean radius. On hit it projects the position back to the surface along
// r̂ and zeros velocity — the craft is treated as "landed" / sitting at
// altitude 0. Returns (state, false) when the craft is above the surface.
//
// v0.8.5: minimum-viable surface-impact handling. Without this, a craft
// aerobraking past altitude 0 keeps falling toward r=0; gravity (1/r²)
// blows up and the symplectic integrator slingshots the craft back out at
// huge velocity. A real "crashed" status with destruction / structural
// damage is deferred to v0.9+ — for now landed and zero-velocity is a
// valid resting state the HUD can read.
//
// Degenerate r=0 input picks +X for the surface direction so callers
// never see NaN, though in practice the integrator never reaches r=0
// because every sub-step is followed by this clamp.
func ClampToSurface(s StateVector, primary bodies.CelestialBody) (StateVector, bool) {
	radius := primary.RadiusMeters()
	if radius <= 0 {
		return s, false
	}
	rMag := s.R.Norm()
	if rMag >= radius {
		return s, false
	}
	if rMag == 0 {
		return StateVector{
			R: orbital.Vec3{X: radius},
			V: orbital.Vec3{},
			M: s.M,
		}, true
	}
	scale := radius / rMag
	return StateVector{
		R: orbital.Vec3{X: s.R.X * scale, Y: s.R.Y * scale, Z: s.R.Z * scale},
		V: orbital.Vec3{},
		M: s.M,
	}, true
}
