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
	Isp     float64 // s — specific impulse, used by finite burns
	Thrust  float64 // N — max engine thrust; zero disables finite burns

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
//
// Mass / propulsion numbers (v0.5.6) modeled on the SLS Interim Cryogenic
// Propulsion Stage (ICPS) — the upper stage that performs trans-lunar
// injection on Artemis II:
//   - DryMass 3500 kg (close to ICPS empty mass 3490 kg)
//   - Fuel 25000 kg (close to ICPS LH2/LOX capacity ~27220 kg)
//   - Isp 462 s (RL-10C-3, ICPS's engine)
//   - Thrust 108 kN (RL-10C-3 spec)
//
// Δv budget = 462 × g₀ × ln(28500/3500) ≈ 9.5 km/s — comfortable for a
// LEO → TLI → LOI → TEI → Earth-return round trip with margin. Pre-v0.5.6
// the default (500/500/Isp 300, ~2 km/s) couldn't even reach Luna one-way.
func NewInLEO(earth bodies.CelestialBody) *Spacecraft {
	r := earth.RadiusMeters() + 200e3
	mu := earth.GravitationalParameter()
	v := math.Sqrt(mu / r)
	return &Spacecraft{
		Name:    "ICPS-1",
		DryMass: 3500,
		Fuel:    25000,
		Isp:     462,
		Thrust:  108000, // 108 kN — RL-10C-3 vacuum thrust
		Primary: earth,
		State: physics.StateVector{
			R: orbital.Vec3{X: r},
			V: orbital.Vec3{Y: v},
			M: 28500,
		},
	}
}
