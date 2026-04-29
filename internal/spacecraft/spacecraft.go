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

	// v0.8.0 — RCS / monopropellant precision-maneuver thruster.
	// Monoprop is the consumable propellant pool (kg); MonopropCapacity
	// is the max tank load. RCSThrust is total RCS engine thrust (N),
	// sized linearly off DryMass at construction. RCSIsp is the
	// specific impulse of the monoprop engine (~220 s, vs ~420 s for
	// the J-2 main).
	//
	// All four are zero on legacy v3 saves; the loader populates
	// defaults from DryMass so old saves inherit RCS without a
	// schema bump.
	Monoprop         float64
	MonopropCapacity float64
	RCSThrust        float64
	RCSIsp           float64

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

// TotalMass returns dry + fuel + monoprop.
func (s *Spacecraft) TotalMass() float64 { return s.DryMass + s.Fuel + s.Monoprop }

// RCSDeltaV estimates how much more Δv the current monoprop pool
// supports via the rocket equation against TotalMass minus monoprop
// (i.e. the dry-fuel mass after the monoprop is exhausted). v0.8.0+.
func (s *Spacecraft) RCSDeltaV() float64 {
	if s.RCSIsp <= 0 || s.Monoprop <= 0 {
		return 0
	}
	mDry := s.DryMass + s.Fuel
	if mDry <= 0 {
		return 0
	}
	m0 := mDry + s.Monoprop
	return s.RCSIsp * g0 * math.Log(m0/mDry)
}

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
	dry := 11000.0
	fuel := 40000.0
	monoprop, monoCap, rcsThrust, rcsIsp := DefaultRCSLoadout(dry)
	return &Spacecraft{
		Name:             "S-IVB-1",
		DryMass:          dry,
		Fuel:             fuel,
		Isp:              421,
		Thrust:           1023000, // 1023 kN — J-2 vacuum thrust
		Throttle:         1.0,     // full throttle by default
		Monoprop:         monoprop,
		MonopropCapacity: monoCap,
		RCSThrust:        rcsThrust,
		RCSIsp:           rcsIsp,
		Primary:          earth,
		State: physics.StateVector{
			R: orbital.Vec3{X: r},
			V: orbital.Vec3{Y: v},
			M: dry + fuel + monoprop,
		},
	}
}

// DefaultRCSLoadout returns canonical (monoprop, capacity, thrust, isp)
// for a craft of the given dry mass. Linear scaling per v0.8 plan
// scoping decision #8: RCSThrust = k_T · m_dry, capacity = k_M · m_dry,
// tuned so a default S-IVB-1-class craft (11000 kg dry + 40000 kg
// fuel) gets ~30 m/s of RCS Δv budget — enough for proximity ops
// without being twitchy.
//
// kCap was 50/11000 in the initial v0.8.0 cut; the v0.8 plan's "~28
// m/s budget" formula `Isp · g₀ · ln(m₀/m_dry)` conflated total fuel
// ejection with monoprop ejection, and the realised budget at 50 kg
// was only ~2 m/s. To actually hit the planned ~30 m/s on a 51 t
// wet craft at Isp=220 the monoprop pool needs ~720 kg (a 1.4 %
// mass fraction — physically realistic). v0.8.0+.
func DefaultRCSLoadout(dryMass float64) (monoprop, capacity, thrust, isp float64) {
	const (
		kCap   = 720.0 / 11000.0 // ~720 kg monoprop on a 11000 kg dry craft → ~30 m/s budget
		kThr   = 440.0 / 11000.0 // ~440 N RCS thrust on a 11000 kg dry craft
		ispRCS = 220.0           // typical hypergolic monoprop Isp
	)
	capacity = kCap * dryMass
	thrust = kThr * dryMass
	isp = ispRCS
	monoprop = capacity // ship full
	return
}
