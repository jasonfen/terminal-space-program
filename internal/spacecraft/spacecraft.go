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
	// ID is the vessel's stable identity (v0.14.x / ADR 0012). Assigned
	// once from a monotonic World counter when the craft enters the
	// slate; never reused. Targets reference a craft by ID (not by its
	// slice position), so a slate mutation — end-flight, dock, undock,
	// stage — that shifts indices can no longer re-point a stored target
	// at the wrong vessel (GH #87). Zero means "unstamped"; the World
	// stamps it on spawn/load. Persisted (save schema v7+).
	ID uint64

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
	// thrust = Thrust * Throttle. v0.7.3+. Zero means "engine off"
	// (the live value after the player cuts throttle) — it is NOT a
	// legacy/unset sentinel promoted to 1.0; Spacecraft.EffectiveThrottle
	// returns it verbatim. Every constructor must therefore set Throttle
	// explicitly (NewInLEO and the save-load path do); literal
	// Spacecraft{} test fixtures use Thrust=0 so the engine path is
	// never entered and the value is moot. (ManeuverNode.EffectiveThrottle
	// is the one that maps 0→1.0, for the per-node firing throttle —
	// that promotion is node-local and does not apply here.)
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

	// SystemIdx (v0.16 / ADR 0015) binds this Vessel to one System for
	// its lifetime, fixed at spawn. It is an index into the
	// name-sorted-Sol-first w.Systems slice. The simulator integrates
	// each Vessel against w.Systems[SystemIdx] — not the currently-viewed
	// system — so a parked Sol craft keeps orbiting correctly while the
	// player flies a craft in another System. There is no interstellar
	// transfer; SOI transitions and Docking stay within one System. Zero
	// (Sol) is the correct default for the seed Vessel and any save
	// predating the per-Vessel binding (see save_migrate_v7_to_v8).
	SystemIdx int

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

	// Crashed (v0.11.4+): destructive-impact lifecycle flag. Set by
	// the surface-contact predicate (physics.ClampToSurface call
	// site) when impact velocity exceeds V_CRIT or nose alignment
	// fails NOSE_TOL, or when the vessel is not designed to
	// soft-land. While Crashed the vessel skips integration (no
	// gravity / drag / thrust / slew) and renders dimmed with no
	// flame. Cleared only by end-flight removal (vessel leaves the
	// world). Persists in saves so a paused-mid-impact session
	// restores the crashed state. See ADR 0004 for the full
	// lifecycle. `omitempty`-default-false; no SchemaVersion bump.
	Crashed bool

	// CanSoftLand (v0.11.4+): true when the vessel kind is designed
	// to land — Apollo-LM-style Lander, Falcon-9 first stage. The
	// surface-contact predicate consults this as a hard prerequisite
	// for the soft-land branch: a Saturn V capsule that grazes the
	// surface at 5 m/s is Crashed, not Landed, even though the
	// kinematic checks would otherwise qualify. Sourced from the
	// catalog loadout at construction; not mutated at runtime.
	// `omitempty`-default-false (existing vessels are crash-only).
	CanSoftLand bool

	// HasParachute (v0.12 Slice 3, ADR 0008): the Vessel-level mirror
	// of the bottom stage's per-Stage parachute capability. Re-derived
	// from Stages[0] on every SyncFields exactly like CanSoftLand, so
	// it rides the hardware across a decouple (the chute capability
	// becomes "active" once the chute-bearing stage is the bottom /
	// surviving core). Gates the Stage-action arm path and the
	// auto-deploy check. `omitempty`-default-false.
	HasParachute bool

	// Crewed / Controllable (v0.23 / ADR 0027): vessel-level mirrors of
	// the per-stage CommandSource, re-derived by SyncFields across the
	// whole stack on every staging / dock / load. Controllable is true
	// when the vessel has any command source (crewed pod or probe core);
	// Crewed is true when any is a crewed pod (crewed vessels are never
	// comms-gated). A vessel with neither is passive debris. Construction
	// (NewFromLoadout / NewFromStages) and save-load stamp a default
	// command source on a command-less *vessel* so it stays controllable;
	// jettisoned stages get no default, so a spent booster is debris.
	Crewed       bool
	Controllable bool

	// AntennaKind / AntennaRangeM (v0.23 / ADR 0027): the vessel's
	// effective comms antenna — the longest-ranged one across its stages,
	// re-derived by SyncFields. Read by the connectivity graph. AntennaNone /
	// zero means no antenna. AntennaRangeM is a rated range in metres (the
	// combinability model; see sim.commLinkRangeM).
	AntennaKind   string
	AntennaRangeM float64

	// ChuteState (v0.12 Slice 3, ADR 0008): the runtime parachute
	// deploy state — STOWED → ARMED → DEPLOYED, one-way, DEPLOYED
	// terminal. Lives alongside Landed / Crashed (the other surface-
	// lifecycle runtime flags). Zero value = ChuteStowed, so pre-Slice-3
	// saves load stowed; `omitempty`, no SchemaVersion bump. While
	// ChuteDeployed, EffectiveBallisticCoefficient returns the fixed
	// ChuteDeployedBC and the surface-arrival predicate gains a second
	// (nose-waived) route into Landed.
	ChuteState ChuteState

	// OnPad (v0.11.4+): true between Launchpad spawn and first
	// liftoff. Set by surfaceSpawnPosVel; cleared on the first
	// Landed=false transition. Distinguishes "fresh launchpad
	// spawn" from "post-flight soft land" for the ViewLaunch
	// auto-route handler (which fires only when OnPad && Landed
	// transitions false→true). Soft-lands clear OnPad on liftoff,
	// so the post-flight Landed transition does NOT rip the player
	// into ViewLaunch mid-touchdown.
	OnPad bool

	// LandedLatDeg / LandedLonDeg (v0.11.4+): soft-landed touchdown
	// coordinates. When non-zero, integrateLanded reads these instead
	// of LaunchLatDeg / LaunchLonDeg (which retain their original
	// spawn-site meaning — useful for downrange-from-launch reads
	// even after a return-and-relaunch cycle). Same north-positive /
	// east-positive convention as LaunchLatDeg.
	LandedLatDeg float64
	LandedLonDeg float64

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

	// DecouplePlan (v0.12 Slice 2 / ADR 0007) is a bottom-up list of
	// group sizes describing how many contiguous bottom Stages each
	// staging press releases as a single jettisoned craft. Nil/empty
	// ⇒ all-ones (one Stage per press — the historical behaviour).
	// Copied from the Loadout at construction (NewFromLoadout) and
	// consumed positionally by World.StageActive: each press pops
	// DecouplePlan[0] bottom stages, then advances DecouplePlan =
	// DecouplePlan[1:]. The Apollo Stack ships [1,1,1,2] so the
	// descent + ascent LM pair extracts together as one 2-stage
	// craft, leaving the CSM core. A released multi-stage craft
	// inherits NO plan, so its internal boundaries fall back to
	// single-pop (the extracted LM surface-stages its descent alone
	// with no special-casing). Persisted on the save wire as
	// decouple_plan,omitempty so a mission saved mid-staging restores
	// the correct remaining grouping.
	DecouplePlan []int

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
	// CanSoftLand / HasParachute (v0.12 Slice 3, ADR 0008): the two
	// surface-arrival capability flags, captured so Undock can restore
	// them onto the rebuilt single-stage craft. Without this a chute-
	// bearing capsule (or a CanSoftLand lander) that docks then undocks
	// loses its capability — the restored Stages[0] would default false
	// and SyncFields would re-derive a false mirror, crashing the Earth
	// splashdown the chute exists for. (DockedComponent still doesn't
	// record the full per-stage breakdown — that broader gap is the
	// banked v0.9.1.x follow-up the Undock comment notes — but the
	// landing capabilities are cheap to carry and load-bearing.)
	CanSoftLand  bool
	HasParachute bool
	// Stages (v0.12 / ADR 0009): the component's full per-stage
	// breakdown, captured so Undock can restore a MULTI-stage craft
	// (e.g. the Apollo LM = Descent + Ascent released as a docked nose
	// payload after transposition). Closes the v0.9.1.x gap the flat
	// single-stage fields above couldn't cover. Empty/nil ⇒ Undock
	// falls back to the legacy single-stage prorate rebuild (old saves,
	// single-stage components). The recorded FuelMass is a dock-time
	// snapshot used only for the stage COUNT + identity; Undock reads
	// LIVE per-stage fuel from the composite's current Stages (the
	// firing Stages[0] is drained while docked, so the snapshot fuel
	// goes stale — see sim.Undock).
	Stages []Stage
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
		CanSoftLand:      s.CanSoftLand,
		HasParachute:     s.HasParachute,
		// v0.12 / ADR 0009: record the full stage breakdown so a
		// multi-stage component (the LM) round-trips through Undock.
		Stages: append([]Stage(nil), s.Stages...),
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

// SurfaceLatLon returns the craft's body-fixed surface coordinates in
// degrees (north-positive, east-positive): the soft-touchdown coords when
// set, else the launchpad-spawn coords ("when non-zero, read these
// instead"). Meaningful when the craft is Landed. Single source of truth
// for both the landed-integration pin (sim.integrateLanded) and the
// mission evaluator's surface position (sim.missionEvalContext). v0.21+.
func (s *Spacecraft) SurfaceLatLon() (lat, lon float64) {
	lat, lon = s.LaunchLatDeg, s.LaunchLonDeg
	if s.LandedLatDeg != 0 || s.LandedLonDeg != 0 {
		lat, lon = s.LandedLatDeg, s.LandedLonDeg
	}
	return lat, lon
}

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
	// v0.12 Slice 3 (ADR 0008): a deployed parachute swamps the
	// capsule's own drag. Absolute replace at the top of the chain so
	// terminal velocity is predictable and mass-independent.
	if s.ChuteState == ChuteDeployed {
		return ChuteDeployedBC
	}
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
	// v0.23 / ADR 0027: the player's starting vessel is crew-tended, so it
	// is never comms-gated — the player learns to fly without a connectivity
	// constraint on their first ship; the probes they later launch ARE gated.
	// Overrides the probe core EnsureCommandSource stamped at construction.
	if len(c.Stages) > 0 {
		top := len(c.Stages) - 1
		c.Stages[top].CommandSource = CommandCrewed
		c.SyncFields()
	}
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
