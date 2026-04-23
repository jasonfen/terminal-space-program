// Package spacecraft holds the Spacecraft type and its mutable runtime
// state (current primary + state vector + fuel). Physics in internal/physics
// operates on StateVector directly; this package is the glue between a
// named vessel, its current primary body, and the propagator.
package spacecraft

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// Spacecraft is the player vessel. Mass split: DryMass is the bus, Fuel
// is consumable. State is relative to Primary.
type Spacecraft struct {
	Name    string
	DryMass float64 // kg
	Fuel    float64 // kg
	Isp     float64 // s — specific impulse, used post-v0.1 for finite burns

	Primary bodies.CelestialBody
	State   physics.StateVector
}

// TotalMass returns dry + fuel.
func (s *Spacecraft) TotalMass() float64 { return s.DryMass + s.Fuel }

// Altitude returns |r| − primary mean radius. Can go negative if the
// spacecraft is inside the primary — the caller (HUD / crash detection)
// decides what to do about that.
func (s *Spacecraft) Altitude() float64 {
	return s.State.R.Norm() - s.Primary.RadiusMeters()
}

// OrbitalSpeed returns |v| in the primary-relative frame.
func (s *Spacecraft) OrbitalSpeed() float64 { return s.State.V.Norm() }

// NewInLEO builds a spacecraft in a 200 km circular prograde parking orbit
// around the provided primary (typically Earth). Orbit lies in the primary's
// equatorial plane (z=0) with periapsis along +X, velocity along +Y.
// Mass numbers: 500 kg dry + 500 kg fuel, Isp 300s — generic upper-stage
// ballpark, good enough for a sandbox.
func NewInLEO(earth bodies.CelestialBody) *Spacecraft {
	r := earth.RadiusMeters() + 200e3
	mu := earth.GravitationalParameter()
	v := math.Sqrt(mu / r)
	return &Spacecraft{
		Name:    "LEO-1",
		DryMass: 500,
		Fuel:    500,
		Isp:     300,
		Primary: earth,
		State: physics.StateVector{
			R: orbital.Vec3{X: r},
			V: orbital.Vec3{Y: v},
			M: 1000,
		},
	}
}
