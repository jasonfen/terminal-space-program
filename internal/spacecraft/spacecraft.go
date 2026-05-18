// Package spacecraft holds the Spacecraft type and its mutable runtime
// state (current primary + state vector + fuel). Physics in internal/physics
// operates on StateVector directly; this package is the glue between a
// named vessel, its current primary body, and the propagator.
package spacecraft

import (
	"math"
	"time"

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

	// LoadoutID, Role, Glyph, Color (v0.8.2+) are the craft-type
	// axes from the v0.8 plan §scoping #3:
	//   (i)   propulsion loadout — references Loadouts[ID]; the
	//         per-craft Thrust / Isp / DryMass / Fuel are populated
	//         from the loadout at construction time.
	//   (ii)  role — free-form tag (transfer-stage / lander /
	//         orbiter / tug). HUD/mission-predicate facing only;
	//         no physics impact in v0.8.
	//   (iii) visual — Glyph + Color override the canvas marker so
	//         each craft reads distinctly even when zoomed out
	//         beyond the chevron-resolving threshold.
	//
	// All four are zero-default-safe: pre-v0.8.2 saves load with
	// empty strings, and the lookup paths fall back to the S-IVB-1
	// default loadout when LoadoutID is empty.
	LoadoutID string
	Role      string
	Glyph     string
	Color     string

	// Throttle is the engine power factor in [0, 1]; effective
	// thrust = Thrust * Throttle. v0.7.3+. Zero is a sentinel for
	// "legacy / unset" (treated as 1.0 by EffectiveThrottle) so
	// pre-v0.7.3 Spacecraft constructions and v3 saves keep firing
	// at full thrust. New callers that need the engine off route
	// through ManualBurn = nil rather than a zero throttle.
	Throttle float64

	// LastThrottleChangeAt is the sim-time at which Throttle most
	// recently changed value. v0.8.6.x+: the warp clamp uses this
	// to suppress high warp for a brief window after the player
	// adjusts throttle, so a 1000× throttle ramp doesn't alias the
	// integrator the same way a finite burn does. Zero value means
	// "never changed since spawn" — treated as no recent change.
	// Not persisted to saves; resets on load (acceptable since the
	// clamp window is sub-second).
	LastThrottleChangeAt time.Time `json:"-"`

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

	// v0.8.4 — atmospheric drag coupling. BallisticCoefficient is
	// (C_D · A / m) in m²/kg — the multiplicative factor in the drag
	// equation a = -0.5 · ρ · |v_rel|² · BC · v̂_rel. Higher means
	// more drag per unit dynamic pressure (the inverse of the
	// aerospace-standard m/(C_D·A) convention; named for what the
	// integrator actually multiplies). Zero is treated as the default
	// 0.01 m²/kg (S-IVB-1 baseline) so legacy saves don't need a
	// schema bump — see EffectiveBallisticCoefficient.
	BallisticCoefficient float64

	Primary bodies.CelestialBody
	State   physics.StateVector

	// v0.8.1+ — per-craft mission/flight state. Pre-v0.8.1 these
	// lived on World, which meant a single planted burn was shared
	// across all craft and the in-flight ActiveBurn followed
	// whichever craft was active at integrator time. Per-craft
	// ownership ties planted nodes + live engine state to the craft
	// they were planted for, regardless of which craft the player
	// is currently flying.
	//
	// Nodes are sorted by TriggerTime ascending (sim package owns
	// the sort helper). ActiveBurn / ManualBurn are mutually
	// exclusive — a planted finite burn or a held manual burn, not
	// both. AttitudeMode + EngineMode are the live manual-flight
	// state.
	Nodes        []ManeuverNode
	ActiveBurn   *ActiveBurn
	ManualBurn   *ManualBurn
	AttitudeMode BurnMode
	EngineMode   EngineMode

	// CurrentAttitudeDir (v0.10.0+) is the craft's *actual* nose
	// unit vector in the same world/primary frame as State.R/V —
	// the physical orientation, distinct from the *commanded*
	// direction recomputed from AttitudeMode each tick. The slew
	// integrator (sim.integrateOneCraft) rotates this toward the
	// commanded direction at SlewRate; stepThrust + the navball
	// sub-observer read it instead of recomputing, so burning
	// before alignment bleeds Δv to cosine loss. A zero vector
	// means "uninitialized" — the first slew tick snaps it to the
	// commanded direction (no slew-from-garbage, no nose teleport
	// on a pre-v0.10.0 save). Persists in saves so a craft caught
	// mid-slew restores its real nose.
	CurrentAttitudeDir orbital.Vec3

	// CommandedRollDeg / CurrentRollDeg (v0.10.0+) are the craft's
	// roll about its lengthwise (nose) axis, in degrees, measured
	// from the "heads-up" reference (body-up as close to local
	// vertical / radial-out as the nose allows — see BodyFrame).
	// CommandedRollDeg is the player target (0 = heads-up, the
	// default); CurrentRollDeg is the actual roll, which the slew
	// integrator rotates toward the command at SlewRate (snapped
	// under InstantSAS and while Landed). Together with
	// CurrentAttitudeDir this gives the craft a full body frame
	// {nose, up, right} so the navball has a stable left/right and
	// the player can bank. Both persist in saves; range is wrapped
	// to (-180, 180].
	CommandedRollDeg float64
	CurrentRollDeg   float64

	// SlewRateDegPerSec (v0.10.0+) caps attitude angular rate in
	// **sim-time** (deg/s, integrated against the warp-scaled tick).
	// Zero => DefaultSlewRateDegPerSec. Set from the loadout at
	// construction (NewFromLoadout); not stage-derived, so SyncFields
	// does not touch it, and it is re-applied via the loadout on load
	// rather than persisted.
	SlewRateDegPerSec float64

	// DockedComponents (v0.8.3+) records the original craft that
	// fused into this composite, so an Undock keystroke can
	// restore them. Empty for non-composite craft. Populated by
	// sim.DockCrafts in render-order; flattened across chained
	// docks (a composite that docks with another contributes both
	// its own components and the other's identity to the result).
	DockedComponents []DockedComponent

	// Landed (v0.9.2+): true when the craft is parked on the
	// primary's surface co-rotating with the ground. While Landed,
	// the integrator bypasses gravity / drag / thrust and recomputes
	// R from `LaunchLatDeg` / `LaunchLonDeg` each tick using the
	// renderer's `BodyFixedToWorld` projection — so the craft stays
	// at the texture-rendered "Cape Canaveral" pixel as the body
	// rotates. Cleared automatically when the engine ignites — see
	// `World.StartManualBurn` and the planted-burn fire path. Set
	// on `SpawnSpec.Launchpad=true` spawns. Persists in saves so a
	// paused-on-pad session restores correctly.
	Landed bool

	// LaunchLatDeg / LaunchLonDeg (v0.9.2+) record the body-fixed
	// (lat, lon) of the launchpad spawn. Only meaningful when
	// Landed=true; the integrator re-derives R from these +
	// the body's current rotation phase each tick (rather than
	// rotating R via Rodrigues, which drifted off the texture's
	// Florida pixel because the v0.8.5+ Snyder-orthographic
	// rendering has a sub-observer-frame rotation that's view-
	// dependent — see render.BodyFixedToWorld doc).
	//
	// Latitude in degrees north positive; longitude in degrees east
	// positive (real-Earth-style). Persists in saves.
	LaunchLatDeg float64
	LaunchLonDeg float64

	// PitchTrim (v0.9.2+) is a signed pitch offset (radians)
	// applied on top of the active BurnMode's computed direction.
	// Positive values rotate the thrust vector eastward of the
	// mode's natural direction (about the local-north axis at the
	// craft's current position); negative rotates west. Used by
	// ascent gravity-turn flight: the player launches BurnRadialOut
	// (vertical), trims +5–15° east via the `<` / `>` keys to start
	// the gravity turn, then switches to BurnSurfacePrograde once
	// surface-relative velocity is established. Reset via the `\`
	// key. Persists in saves so a paused-mid-ascent session restores
	// the player's trim setting.
	PitchTrim float64

	// Stages (v0.9.1+) is the source of truth for dry mass /
	// propellant / engine numbers. Stages[0] is the BOTTOM stage
	// (the currently-firing engine + the next to be jettisoned by
	// World.StageActive); Stages[len-1] is the TOP stage (core
	// payload — last to fire). Single-stage craft carry exactly
	// one Stage.
	//
	// The historical flat fields above (DryMass, Fuel, Thrust,
	// Isp, Monoprop, MonopropCapacity, RCSThrust, RCSIsp) are
	// derived shadow-mirror values refreshed by SyncFields
	// (stage.go). Read sites use the flat fields directly — no
	// API churn for the dozens of pre-v0.9.1 consumers. Write
	// sites must mutate the relevant Stage entry and call
	// SyncFields (or use the BurnFuel / BurnMonoprop helpers).
	//
	// Save schema v6 serializes Stages; the flat fields are
	// re-derived on Load via SyncFields. Pre-v6 saves migrate by
	// wrapping the v5 flat fields into a single-element Stages
	// slice (see internal/save/save_migrate_v5_to_v6.go).
	Stages []Stage

	// Target (v0.9.3 polish) is this craft's bound target. Pre-
	// polish, target was a single World.Target slot shared across
	// all crafts; pressing `T` while controlling craft A would
	// toggle the target visible to craft B too. Per-craft Target
	// gives each vessel its own binding, restored on switch via
	// World.setActiveCraftIdx so w.Target stays in sync with the
	// currently-active craft. Zero value (TargetNone) is the safe
	// default for fresh / loaded crafts.
	Target Target
}

// DockedComponent is a snapshot of one pre-dock craft identity
// kept on its composite. Used by sim.Undock to restore the
// original vessels. State (position / velocity / nodes / burns)
// isn't preserved — the docked craft sit at the composite's state
// while joined; on undock they re-emerge near the composite's
// current state. v0.8.3+.
type DockedComponent struct {
	Name             string
	LoadoutID        string
	Role             string
	Glyph            string
	Color            string
	DryMass          float64
	FuelCapacity     float64
	MonopropCapacity float64
	Isp              float64
	Thrust           float64
	RCSThrust        float64
	RCSIsp           float64
}

// AsDockedComponent captures s's identity + capacity fields into a
// DockedComponent record. v0.8.3+: used by DockCrafts to populate
// the composite's DockedComponents list.
func (s *Spacecraft) AsDockedComponent() DockedComponent {
	return DockedComponent{
		Name:             s.Name,
		LoadoutID:        s.LoadoutID,
		Role:             s.Role,
		Glyph:            s.Glyph,
		Color:            s.Color,
		DryMass:          s.DryMass,
		FuelCapacity:     s.Fuel, // pre-dock fuel becomes "capacity" for share calc on undock
		MonopropCapacity: s.MonopropCapacity,
		Isp:              s.Isp,
		Thrust:           s.Thrust,
		RCSThrust:        s.RCSThrust,
		RCSIsp:           s.RCSIsp,
	}
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

// DefaultBallisticCoefficient is the v0.8.4 baseline (C_D · A / m)
// for an S-IVB-1-class craft in m²/kg. Used as the fallback when
// Spacecraft.BallisticCoefficient is zero — legacy saves and any
// loadout that hasn't been tuned yet inherit this.
const DefaultBallisticCoefficient = 0.01

// EffectiveBallisticCoefficient returns the per-craft drag
// coefficient (C_D · A / m, m²/kg). v0.9.2.1+: prefers the bottom
// stage's per-stage BC (Stages[0].BallisticCoefficient) so a
// multi-stage craft gets the firing stage's actual cross-section /
// mass profile — critical for low-altitude Saturn V launches where
// the v0.8.4 default (0.01 m²/kg, tuned for an LEO S-IVB-1 where
// drag was always zero) makes drag dominate at sea level. Falls
// back to s.BallisticCoefficient (legacy field), then
// DefaultBallisticCoefficient.
func (s *Spacecraft) EffectiveBallisticCoefficient() float64 {
	if len(s.Stages) > 0 && s.Stages[0].BallisticCoefficient > 0 {
		return s.Stages[0].BallisticCoefficient
	}
	if s.BallisticCoefficient > 0 {
		return s.BallisticCoefficient
	}
	return DefaultBallisticCoefficient
}

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
	c := NewFromLoadout(LoadoutSIVB1ID)
	c.Name = "S-IVB-1" // first vessel of the slate keeps the historical instance name.
	c.Primary = earth
	// v0.8.6+: rotate the body-frame circular state into world coords
	// so the orbit physically lies in Earth's equatorial plane (passes
	// over the equator), not the world XY plane (which is offset by
	// Earth's 23.44° axial tilt). Pre-v0.8.5.7 there were no tilts so
	// the two coincided.
	frame := orbital.ReferenceFrameForPrimary(earth)
	c.State = physics.StateVector{
		R: frame.ToWorld(orbital.Vec3{X: r}),
		V: frame.ToWorld(orbital.Vec3{Y: v}),
		M: c.TotalMass(),
	}
	return c
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
