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

	// Throttle is the engine power factor in [0, 1]; effective
	// thrust = Thrust * Throttle. v0.7.3+. Zero is a sentinel for
	// "legacy / unset" (treated as 1.0 by EffectiveThrottle) so
	// pre-v0.7.3 Spacecraft constructions and v3 saves keep firing
	// at full thrust. New callers that need the engine off route
	// through ManualBurn = nil rather than a zero throttle.
	Throttle float64

	Primary bodies.CelestialBody
	State   physics.StateVector
}

// EffectiveThrottle returns Throttle clamped to [0, 1]. Zero means
// "engine off" — that's the real value the player sees in the HUD
// after pressing `x` (cut throttle), so it cannot be silently
// promoted to 1.0. All Spacecraft constructors must set Throttle
// explicitly (see NewInLEO and the save-load path); the test
// fixtures that build literal Spacecraft{} use Thrust=0 so the
// engine path is never entered and the throttle value is moot.
func (s *Spacecraft) EffectiveThrottle() float64 {
	t := s.Throttle
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
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

// NewInLEO builds a spacecraft in a 500 km circular prograde parking orbit
// around the provided primary (typically Earth). Orbit lies in the primary's
// equatorial plane (z=0) with periapsis along +X, velocity along +Y.
// v0.6.1: bumped from 200 → 500 km — clears the visual zone close to the
// Earth disk so the live orbit ellipse and craft glyph are immediately
// distinguishable from the body when the camera spawns focused on the craft.
//
// Mass / propulsion numbers (v0.5.13+) modeled on the Saturn V S-IVB —
// the J-2-powered third stage that performed trans-lunar injection for
// every Apollo Moon mission:
//   - DryMass 11000 kg (S-IVB empty was ~12 400 kg; rounded down for
//     a no-payload solo profile)
//   - Fuel 40000 kg (much less than real S-IVB's ~106 t — sized for
//     Δv 6.3 km/s, enough for Luna round trip without over-provisioning)
//   - Isp 421 s (J-2 vacuum)
//   - Thrust 1023 kN (J-2 spec)
//
// Δv budget = 421 × g₀ × ln(51000/11000) ≈ 6.3 km/s — Luna round trip
// with margin. TLI burn time at this thrust ≈ 110 s (vs ~10 min for the
// pre-v0.5.13 ICPS). The short burn keeps gravity-rotation finite-burn
// loss < 0.1%, so the auto-plant Hohmann delivers near-exact apoapsis
// without needing the impulsive workaround. Pre-v0.5.13 the ICPS-class
// vessel had a 10-min TLI that lost ~27% of apoapsis-raise to
// integration error.
//
// History: v0.5.6 ICPS-1, v0.5.13+ S-IVB-1. Apollo's actual TLI stage
// is a better fit for the no-payload Luna-mission profile this default
// targets.
func NewInLEO(earth bodies.CelestialBody) *Spacecraft {
	r := earth.RadiusMeters() + 500e3
	mu := earth.GravitationalParameter()
	v := math.Sqrt(mu / r)
	return &Spacecraft{
		Name:     "S-IVB-1",
		DryMass:  11000,
		Fuel:     40000,
		Isp:      421,
		Thrust:   1023000, // 1023 kN — J-2 vacuum thrust
		Throttle: 1.0,     // full throttle by default
		Primary:  earth,
		State: physics.StateVector{
			R: orbital.Vec3{X: r},
			V: orbital.Vec3{Y: v},
			M: 51000,
		},
	}
}
