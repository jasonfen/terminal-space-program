package sim

import (
	"fmt"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// World holds the simulation state: loaded systems, active-system index,
// the sim-clock, and — post-C15 — the spacecraft.
type World struct {
	Systems    []bodies.System
	SystemIdx  int
	Calculator orbital.Calculator
	Clock      *Clock

	// Crafts is the slate of player vessels. v0.8.1+: replaces the
	// pre-v0.8.1 single-pointer `Craft` field. Empty when no primary
	// is loaded; otherwise has at least one entry. ActiveCraftIdx is
	// the index of the currently-controlled craft (the one the HUD,
	// manual flight, and node planter all bind to). Cycled via the
	// `[` / `]` keys.
	//
	// All historical call sites that read `w.ActiveCraft()` go through the
	// `ActiveCraft()` accessor below — there's no longer a "the"
	// craft, only "the active one."
	Crafts         []*spacecraft.Spacecraft
	ActiveCraftIdx int

	// NextCraftID is the monotonic source of stable Spacecraft.IDs
	// (v0.14.x / ADR 0012). Stamped onto each craft as it enters the
	// slate; never decremented or reused, so a target bound to an ID
	// can never alias a different vessel after a slate mutation (GH #87).
	// Persisted (save schema v7). Zero is pre-stamp; EnsureCraftIDs
	// initialises it to len(Crafts)+1 and stamps any unstamped craft.
	NextCraftID uint64

	// NextNodeID is the monotonic source of stable ManeuverNode.IDs
	// (v0.16 / ADR 0016). Stamped onto each node as it is planted; never
	// decremented or reused, so a feature that follows one specific node
	// (Auto-Warp's frozen target) can resolve it across the sortNodes
	// reorder that runs on every plant. Persisted alongside NextCraftID;
	// EnsureNodeIDs primes it past every live node ID and back-fills any
	// node still carrying the zero (pre-field / legacy-save) value.
	NextNodeID uint64

	// AutoWarp is the engaged Auto-Warp driver, or nil when off (v0.16 /
	// ADR 0016). While set, the tick max-seeds clampedWarp and ramps to a
	// frozen target T = BurnStart − autoWarpLeadTime, then hands off to 1×.
	// Transient — never persisted; a save/load mid-warp lands disengaged.
	AutoWarp *AutoWarpTarget

	// LastDockEvent records the most recent fusion for HUD flash
	// + diagnostic. Cleared by app.go after the message is shown.
	// v0.8.3+.
	LastDockEvent *DockEvent

	// LastLaunchReleaseEvent records the most recent ViewLaunch
	// switch-end release so the App can surface an
	// `"ORBIT READY — returning to <prev view>"` toast. Cleared by
	// app.go after the message fires; same pattern as LastDockEvent.
	// v0.11.0+ (the apoapsis-floor auto-release that also stamped
	// this was retired by ADR 0021 D).
	LastLaunchReleaseEvent *LaunchReleaseEvent

	// Focus selects what the OrbitView canvas is centered on. Zero value
	// (FocusSystem) matches v0.1.0 behavior.
	Focus Focus

	// Target is the unified pointer-at-the-thing-the-player-is-aiming-at.
	// v0.9.0+: replaces the implicit body-cursor that pre-v0.9 PlanTransfer
	// / PlanInclinationChange consumed via App.selectedBody, and absorbs
	// the rendezvous target-craft idx that v0.9.3 will plumb. Zero value
	// (TargetNone) means no target — every consumer falls back to its
	// kind-less default (equatorial plane, Hohmann no-op).
	Target Target

	// NavMode selects the reference frame the SAS axis hotkeys
	// interpret against (KSP-style nav-ball mode cycle). Zero value
	// (NavOrbit) reproduces the pre-v0.9.3 behavior. Cycled via the
	// `;` hot-key; auto-snaps to NavOrbit when a craft target is
	// dropped. v0.9.3+.
	NavMode NavMode

	// ViewMode selects the canvas projection basis. v0.6.4+; v0.10.6+
	// prepended ViewTilted as the new zero-value default. Set per-
	// session via the `v` hot-key; not persisted to save (UI
	// preference, not game state).
	ViewMode ViewMode

	// ViewTilt carries the polar tilt θ and yaw φ (degrees) that
	// ViewTilted applies to the projection basis. v0.10.6+. Per-
	// session UI state — not persisted. NewWorld seeds defaults via
	// DefaultViewTilt(); struct-literal Worlds (test fixtures) get
	// the {0, 0} zero value, which evaluates to an identity rotation
	// and therefore behaves the same as ViewTop.
	ViewTilt ViewTilt

	// LaunchSessionActive is the v0.11.0+ ViewLaunch session sentinel.
	// True between the per-tick route handler firing on an active-slot
	// Landed-false→true transition and the manual `v` cycle out (or a
	// switch onto a flying vessel — ADR 0021 D retired the apoapsis-
	// floor auto-release). Distinguished from
	// PrevViewMode because PrevViewMode's zero value (ViewTilted)
	// collides with "no session" — the boolean is the unambiguous
	// signal that session-scoped state (PrevViewMode, LaunchT0,
	// LaunchMaxQ, LaunchTrail, LaunchZoom) is meaningful. Per-session
	// UI state; not persisted (a reload re-routes naturally if the
	// active craft is Landed).
	LaunchSessionActive bool

	// PrevViewMode is the ViewMode the player was in when the route
	// handler routed into ViewLaunch — restored by the switch-end
	// release (a manual `v` cycle advances instead of restoring).
	// Meaningful only when LaunchSessionActive == true. Not persisted.
	PrevViewMode ViewMode

	// LaunchT0 is the sim-time stamp the route handler captured when
	// the current session opened — the anchor for the HUD T+ readout.
	// Meaningful only when LaunchSessionActive == true. Re-stamped on
	// active-vessel-switch hand-off (treating the new active as a
	// fresh launch from the switch moment). Not persisted.
	LaunchT0 time.Time

	// LaunchMaxQ is the peak dynamic pressure (Pa) observed across the
	// current session — the HUD's max-Q readout. Cleared by the route
	// handler at session open. Not persisted.
	LaunchMaxQ float64

	// LaunchTrail is the breadcrumb buffer of body-fixed (lat, lon,
	// alt) samples the chase-cam scene re-projects each render so the
	// trace visibly rotates with the body. FIFO cap 256, sampled at
	// 1 s sim-time cadence. Cleared by the route handler at session
	// open and by the switch-end release / hand-off. Not persisted.
	LaunchTrail []TrailPoint

	// LaunchZoom is the player's `+/-` zoom override for the chase-cam
	// scene. 0 means auto-altitude-driven scale; non-zero pins the
	// scale until session end (manual `v` cycle, switch-end, hand-off,
	// or next route). Multiplicative ×0.8 per `+`, ×1.25 per `-`. Not
	// persisted.
	LaunchZoom float64

	// lastActiveCraft + wasActiveLanded are per-tick shadows owned by
	// the ViewLaunch state machine (internal/sim/view_launch.go).
	// Pointer-keyed (not index) so undock slot-renumbering at
	// docking.go:330 doesn't fire a spurious switch handler — the
	// logical active is unchanged, only its position in Crafts moved.
	// Both reset to zero values on save-load (the post-load shadow
	// reconciliation is part of save_apply, not tracked here).
	lastActiveCraft *spacecraft.Spacecraft
	wasActiveLanded bool

	// InstantSAS opts back into the legacy instantaneous-attitude
	// path. v0.10.0+ makes rate-limited slew the DEFAULT (zero value
	// = false = slew on); toggling this true restores the pre-v0.10
	// "magic SAS" snap (the byte-identical regression baseline). Like
	// ViewMode it is a per-session UI preference — NOT persisted; a
	// reload returns to the slew default. Surfaced as a new hot-key +
	// navball [MODE] MANUAL/AUTO tag (binding is the app/render
	// slice's call).
	InstantSAS bool

	// LastTransfer (v0.12.x) records the dual-strategy Δv comparison from
	// the most recent intra-primary [H] auto-plant (combined fused-Lambert
	// vs split raise+plane-change), for the HUD flash. Not persisted —
	// a transient UI artifact of the last plant. See PlanTransfer / ADR 0005.
	LastTransfer TransferComparison

	// rcsPuffs is a small ring of recent RCS pulses, surfaced by the
	// orbit canvas as a fading marker for visual feedback. v0.8.0
	// ships a placeholder visual; v0.8.2 replaces it with per-thruster
	// glyphs once craft visual differentiation lands.
	rcsPuffs   [rcsPuffCap]rcsPuff
	rcsPuffIdx int
	rcsPuffLen int

	// Missions are pass/fail objectives evaluated against World state
	// each Tick. Seeded from the embedded starter catalog at NewWorld
	// time; Status fields progress as the player flies. v0.6.5+.
	Missions []missions.Mission

	// soiCheckCounter throttles primary-reevaluation — we only need to
	// check every few ticks, not every Verlet sub-step.
	soiCheckCounter int

	// rendezvousCache stores the most recent v0.10.2 rendezvous
	// advisory keyed by (activeIdx, targetIdx, sim-time). Per-frame
	// HUD callers (TARGET HUD ACH-CA block) hit this; the Lambert
	// scan + NextClosestApproach verification only runs on cache
	// miss. See internal/sim/rendezvous.go for the policy.
	rendezvousCache rendezvousAdvisoryCache

	// trail is a ring buffer of recent craft samples for the vessel-
	// position-trail render. Each sample stores the primary's body ID
	// and the craft's position *in that primary's frame* (v0.5.4) — at
	// render time the inertial position is reconstructed via
	// BodyPosition(primary). Pre-v0.5.4 stored heliocentric inertial
	// directly, which made the trail a heliocentric trace rather than
	// following the craft's apparent orbit around its primary.
	//
	// trailLen ≤ trailCap is the live count. trailAccumSec is sim-time
	// accrued since the last sample — we sample at trailIntervalSec,
	// not every tick, so trail length covers ~trailCap × trailIntervalSec
	// of sim history regardless of warp.
	trail         [trailCap]trailSample
	trailIdx      int
	trailLen      int
	trailAccumSec float64
}

// trailSample captures the craft's position in its primary's frame at
// the moment of capture. The primary may differ across samples (an
// SOI crossing changes which body the craft is bound to); each sample
// is independently re-translated at render time.
type trailSample struct {
	primaryID string
	relR      orbital.Vec3
}

const (
	trailCap         = 200
	trailIntervalSec = 10.0

	// rcsPuffCap is the number of recent RCS pulses retained for
	// visual feedback; rcsPuffTTL is how long (sim seconds) each puff
	// remains visible before the canvas drops it. v0.8.0+.
	rcsPuffCap = 12
	rcsPuffTTL = 3.0
)

// rcsPuff captures one fired RCS pulse for the canvas-side renderer.
// v0.8.3+: tracks the craft pointer rather than a primary-frame
// position snapshot — the puff visually emanates from the craft's
// thruster nozzle and tracks the craft as it moves rather than
// being left behind in inertial space (an exhaust cloud is the
// physically correct model, but for game-feedback the player wants
// to see "what direction did I just nudge?" anchored to the craft
// glyph, not floating away).
type rcsPuff struct {
	craft *spacecraft.Spacecraft
	dir   orbital.Vec3 // unit anti-thrust direction (where exhaust goes)
	at    time.Time    // sim-time when the pulse fired
}

// NewWorld loads the embedded systems, seeds clock at J2000 + 50 ms base
// step, and spawns a spacecraft in LEO around Sol's Earth.
func NewWorld() (*World, error) {
	systems, err := bodies.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("load systems: %w", err)
	}
	if len(systems) == 0 {
		return nil, fmt.Errorf("no systems loaded")
	}
	w := &World{
		Systems:   systems,
		SystemIdx: 0,
		Clock:     NewClock(bodies.J2000, 50*time.Millisecond),
		ViewTilt:  DefaultViewTilt(),
	}
	w.Calculator = orbital.ForSystem(w.System())

	// v0.6.5: seed missions from the embedded starter catalog. A failure
	// to load the catalog is non-fatal — missions are an additive
	// feature and shouldn't block worldgen if the JSON is malformed.
	if cat, err := missions.DefaultCatalog(); err == nil {
		w.Missions = missions.Clone(cat.Missions)
	}

	// Spawn spacecraft in LEO. v0.1: craft is always in Sol.
	// v0.8.1+: spawned into the multi-craft slate; subsequent craft
	// arrive via SpawnCraft (`n` keystroke) or staging.
	earth := w.Systems[0].FindBody("Earth")
	if earth != nil {
		seed := spacecraft.NewInLEO(*earth)
		seed.SystemIdx = 0 // v0.16 / ADR 0015: the seed Vessel is bound to Sol.
		w.Crafts = []*spacecraft.Spacecraft{seed}
		// v0.6.1: open with the camera focused on the craft. The
		// system-wide view (FocusSystem) at heliocentric scale shows
		// nothing useful for a craft in LEO — the player has to cycle
		// focus before the orbit even renders. Spawning in
		// FocusCraft puts the live orbit + maneuver previews in
		// frame from the first tick.
		w.Focus = Focus{Kind: FocusCraft}
	}
	w.EnsureCraftIDs() // stamp the initial craft + prime NextCraftID (ADR 0012)
	w.EnsureNodeIDs()  // prime NextNodeID (ADR 0016); the seed craft has no nodes yet
	return w, nil
}

// ActiveCraft returns the currently-controlled craft, or nil if no
// craft is loaded. v0.8.1+. All historical call sites that read
// `w.ActiveCraft()` now go through this accessor.
func (w *World) ActiveCraft() *spacecraft.Spacecraft {
	if len(w.Crafts) == 0 {
		return nil
	}
	if w.ActiveCraftIdx < 0 || w.ActiveCraftIdx >= len(w.Crafts) {
		return nil
	}
	return w.Crafts[w.ActiveCraftIdx]
}

// CycleActiveCraft advances ActiveCraftIdx by delta (typically +1
// or -1), wrapping at the slate's boundaries. No-op when fewer than
// two craft are loaded. v0.8.1+.
func (w *World) CycleActiveCraft(delta int) {
	n := len(w.Crafts)
	if n < 2 {
		return
	}
	idx := (w.ActiveCraftIdx + delta) % n
	if idx < 0 {
		idx += n
	}
	w.SetActiveCraftIdx(idx)
	// Engine state is per-active-craft in v0.8.1 (planted nodes,
	// manual burn, attitude all live on World as "what the active
	// craft is doing"). Cycling resets the live RCS-or-main mode
	// so the new active craft starts in a known state. Manual burn
	// is dropped since it was tied to the prior active craft's
	// engine.
	w.StopManualBurn()
}

// SwitchToCraftIdx jumps control directly to the craft at idx
// (0-based), backing the numbered craft-slot keys (1..9 → idx
// 0..8). Unlike CycleActiveCraft it does not wrap: a slot key with
// no craft behind it (idx ≥ len(Crafts)) or a request for the
// already-active craft is a no-op. Returns true only when the
// active craft actually changed. Mirrors CycleActiveCraft's
// StopManualBurn so the newly-selected craft starts in a known
// engine state. v0.12.0+.
func (w *World) SwitchToCraftIdx(idx int) bool {
	if idx < 0 || idx >= len(w.Crafts) || idx == w.ActiveCraftIdx {
		return false
	}
	w.SetActiveCraftIdx(idx)
	w.StopManualBurn()
	return true
}

// SetActiveCraftIdx switches control to the craft at idx, syncing
// per-craft Target so each vessel keeps its own target binding
// across switches (v0.9.3 polish). Outgoing's live `w.Target`
// is checkpointed onto the outgoing craft, then `w.Target` is
// reloaded from the incoming craft's stored Target. NavMode is
// not yet per-craft (acceptable scope cap; revisit if NavMode
// drift across switches becomes friction).
//
// Caller is responsible for bounds-checking idx against
// len(Crafts); out-of-range idx is treated as "no checkpoint, no
// load" so spawn paths that assign before any craft exists don't
// touch w.Target.
func (w *World) SetActiveCraftIdx(idx int) {
	if w.ActiveCraftIdx >= 0 && w.ActiveCraftIdx < len(w.Crafts) {
		if outgoing := w.Crafts[w.ActiveCraftIdx]; outgoing != nil {
			outgoing.Target = w.Target
		}
	}
	w.ActiveCraftIdx = idx
	if idx >= 0 && idx < len(w.Crafts) {
		if incoming := w.Crafts[idx]; incoming != nil {
			// v0.16 / ADR 0015: the camera follows the Active Vessel's
			// System. Snap the view to the incoming Vessel's System,
			// recreating the Calculator and resetting Focus exactly as
			// CycleSystem does, so the Vessel you fly is always in frame.
			// Guarded against an out-of-range SystemIdx (a corrupt save)
			// so it can't panic w.Systems[...].
			if incoming.SystemIdx >= 0 && incoming.SystemIdx < len(w.Systems) && incoming.SystemIdx != w.SystemIdx {
				w.SystemIdx = incoming.SystemIdx
				w.Calculator = orbital.ForSystem(w.System())
				w.ResetFocus()
			}
			w.Target = incoming.Target
			w.reconcileNavMode()
			return
		}
	}
	w.Target = spacecraft.Target{}
	w.reconcileNavMode()
}

// System returns the currently active system.
func (w *World) System() bodies.System { return w.Systems[w.SystemIdx] }

// systemForCraft returns the System a Vessel is bound to (v0.16 / ADR
// 0015). The integrator resolves the body LIST and SOI table against this
// rather than the currently-viewed System, so a Vessel's primary can only
// switch to a body in its own System — never get yanked onto a Sol body by
// a view-/Sol-hardcoded check. Body POSITIONS in these paths still come
// from w.BodyPosition (the viewed System's Calculator + ParentOf), which is
// exact when the craft's System is the viewed one — always true for the
// Active Vessel, since the camera follows it (SetActiveCraftIdx). A passive
// Vessel in a non-viewed System still coasts correctly on two-body
// propagation (mu is taken from c.Primary directly, not from body
// positions); only its every-20-tick SOI backstop reads positions, and a
// parked Vessel in a stable orbit won't cross an SOI boundary there.
// Clamps to Sol (index 0) for a nil craft or an out-of-range SystemIdx (a
// corrupt binding) rather than panicking.
func (w *World) systemForCraft(c *spacecraft.Spacecraft) bodies.System {
	if c != nil && c.SystemIdx >= 0 && c.SystemIdx < len(w.Systems) {
		return w.Systems[c.SystemIdx]
	}
	return w.Systems[0]
}

// CycleSystem advances to the next system (wraps). Recreates the calculator.
// v0.16 / ADR 0015: this is a browse-only "telescope" view toggle — the
// Active Vessel does NOT follow. Cycling to a System that isn't the Active
// Vessel's hides the flight HUD (CraftVisibleHere is false there) and shows
// that System's map; switching active (SetActiveCraftIdx) or cycling back
// snaps the view home. This browse-then-spawn flow is also the only way to
// get a Vessel into another System: a spawned Vessel binds to the *viewed*
// System at spawn time. Resets focus to system-wide because body indices
// don't carry across systems.
func (w *World) CycleSystem() {
	w.SystemIdx = (w.SystemIdx + 1) % len(w.Systems)
	w.Calculator = orbital.ForSystem(w.System())
	w.ResetFocus()
}

// CraftVisibleHere reports whether the Active Vessel should be drawn in the
// currently-viewed system. v0.16 / ADR 0015: true iff the Active Vessel is
// bound to the viewed System. The camera follows the Active Vessel
// (SetActiveCraftIdx snaps w.SystemIdx), so this is normally true; it goes
// false only while the player is browsing another System via CycleSystem.
func (w *World) CraftVisibleHere() bool {
	c := w.ActiveCraft()
	return c != nil && c.SystemIdx == w.SystemIdx
}

// BodyPosition returns the inertial position (m) of a body in the
// current system at the current sim time. Convenience wrapper over
// BodyPositionAt at w.Clock.SimTime.
func (w *World) BodyPosition(b bodies.CelestialBody) orbital.Vec3 {
	return w.BodyPositionAt(b, w.Clock.SimTime)
}

// BodyPositionAt returns the inertial position (m) of a body at an
// arbitrary sim time. Primary (index 0) is anchored at origin;
// bodies with ParentID resolve recursively as parent + position-
// relative-to-parent. v0.8.2.x: the time-aware variant lets the
// chained-prediction path snapshot bodies at each node's actual
// trigger time rather than at SimTime, which fixes inclination
// previews on multi-day transfers (Luna moves ~30° in 3 days, so
// using SimTime body positions misplaces the arrival rebase by
// the same amount).
func (w *World) BodyPositionAt(b bodies.CelestialBody, t time.Time) orbital.Vec3 {
	if b.SemimajorAxis == 0 {
		return orbital.Vec3{}
	}
	M := w.Calculator.CalculateMeanAnomaly(b, t)
	E := orbital.SolveKepler(M, b.Eccentricity)
	nu := orbital.TrueAnomaly(E, b.Eccentricity)
	el := orbital.ElementsFromBody(b)
	rRel := orbital.PositionAtTrueAnomaly(el, nu)
	if b.ParentID == "" {
		return rRel
	}
	sys := w.System()
	parent := sys.ParentOf(b)
	if parent == nil {
		// Malformed ParentID — fall back to top-level treatment.
		return rRel
	}
	return w.BodyPositionAt(*parent, t).Add(rRel)
}

// CraftInertial returns the spacecraft's inertial position (Sun-centered)
// for rendering on the heliocentric canvas. Adds craft's primary-centric
// position to the primary's inertial position.
func (w *World) CraftInertial() orbital.Vec3 {
	if w.ActiveCraft() == nil {
		return orbital.Vec3{}
	}
	primaryPos := w.BodyPosition(w.ActiveCraft().Primary)
	return primaryPos.Add(w.ActiveCraft().State.R)
}

// Tick advances sim-time one base step (scaled by warp factor) and
// integrates the spacecraft with velocity-Verlet sub-stepping so each
// sub-step is < 1/100th of the current orbital period.
func (w *World) Tick() {
	if w.Clock.Paused {
		return
	}

	// v0.6.0: resolve any event-relative nodes against the live orbit
	// before the warp-clamp + dispatch pass, so freshly-resolved
	// trigger times participate in the finite-burn warp clamp below.
	// Resolution is idempotent: nodes already resolved are skipped.
	w.resolveEventNodes()

	// v0.16 / ADR 0016: advance or release the Auto-Warp target before
	// the warp clamp reads it, so this tick's rate reflects the result
	// (a reached / invalidated target hands back to Selected Warp here).
	w.resolveAutoWarp()

	// Apply SOI warp cap per plan §C21: if the current warp × base-step
	// would force the integrator to exceed its 1024-sub-step cap, reduce
	// effective warp this tick. Doesn't change the clock's displayed warp
	// (user still sees the level they picked); just prevents numerical
	// blow-up at pathologically high warps inside short-period orbits.
	effWarp := w.clampedWarp()
	simDelta := time.Duration(float64(w.Clock.BaseStep) * effWarp)

	// v0.5.12: clamp simDelta to land exactly on the next finite-burn
	// TriggerTime if it falls within this tick. At high warp the tick
	// otherwise overshoots the trigger by hundreds of seconds — the
	// burn fires late and EndTime (= TriggerTime + Duration) leaves a
	// shrunken burn window. Without this clamp, even centered planning
	// gets cut short and apoapsis falls way short. Pure free-flight
	// ticks (no upcoming finite burn) are unaffected.
	if w.ActiveCraft() != nil {
		nextBurn := w.nextFiniteBurnTrigger()
		if !nextBurn.IsZero() {
			until := nextBurn.Sub(w.Clock.SimTime)
			if until > 0 && until < simDelta {
				simDelta = until
			}
		}
	}
	w.Clock.SimTime = w.Clock.SimTime.Add(simDelta)
	// v0.8.5.7+: advance RotationTime alongside SimTime, capped at
	// RotationCapWarp so visible rotation stays smooth even at warp
	// 100000×. World.Tick mutates SimTime directly (clamped to the
	// next finite-burn trigger) instead of going through
	// Clock.Advance, so the rotation-time update has to happen here
	// too. Without this, RotationTime stays stuck at NewClock's
	// initial value and planet textures never rotate.
	rotEffWarp := effWarp
	if rotEffWarp > RotationCapWarp {
		rotEffWarp = RotationCapWarp
	}
	rotDelta := time.Duration(float64(simDelta) * rotEffWarp / effWarp)
	if effWarp == 0 {
		rotDelta = 0
	}
	w.Clock.RotationTime = w.Clock.RotationTime.Add(rotDelta)

	if len(w.Crafts) > 0 {
		// Integrate every craft in the slate. Each craft owns its
		// own Nodes / ActiveBurn / ManualBurn / AttitudeMode /
		// EngineMode (v0.8.1+), so a planted burn fires on the craft
		// it was planted for, regardless of which craft the player
		// happens to be flying when it triggers.
		for _, c := range w.Crafts {
			if c == nil {
				continue
			}
			w.integrateOneCraft(c, simDelta)
		}
		w.executeDueNodes()
		w.soiCheckCounter++
		if w.soiCheckCounter >= 20 {
			w.soiCheckCounter = 0
			for _, c := range w.Crafts {
				if c == nil {
					continue
				}
				w.maybeSwitchPrimaryFor(c)
			}
		}
		w.maybeRecordTrail(simDelta.Seconds())
		w.pruneRCSPuffs()
		// v0.8.3+: docking proximity check fires after integration
		// + node dispatch so two craft converging under a planted
		// rendezvous burn can fuse on the same tick the maneuver
		// completes. The result (success / which partners) is
		// stashed on World.LastDockEvent for the HUD flash.
		if a, b, ok := w.checkDocking(); ok {
			w.LastDockEvent = &DockEvent{
				When:          w.Clock.SimTime,
				CraftIdx:      a,
				PartnerIdx:    b,
				CompositeName: w.ActiveCraft().Name,
			}
		}
		w.evaluateMissions()
	}
	// v0.11.0+: per-tick ViewLaunch route/release state machine.
	// Runs after integration so the post-tick Landed state is
	// authoritative (engine ignition clears Landed inside
	// integrateOneCraft; this needs to see that). Cheap when no
	// session is active and no Landed transition is in progress.
	w.tickLaunchView()
}

// DockEvent records the latest fuse for HUD-side messaging. v0.8.3+.
type DockEvent struct {
	When          time.Time
	CraftIdx      int    // active partner's index (becomes the composite slot)
	PartnerIdx    int    // index of the partner that was removed
	CompositeName string // name of the resulting composite craft
}

// evaluateMissions steps each mission's predicate against the live
// craft state. Terminal-state missions are evaluated too, but their
// status is sticky in missions.Mission.Evaluate. v0.6.5+.
func (w *World) evaluateMissions() {
	if len(w.Missions) == 0 || w.ActiveCraft() == nil {
		return
	}
	ctx := missions.EvalContext{
		PrimaryID:      w.ActiveCraft().Primary.ID,
		PrimaryRadiusM: w.ActiveCraft().Primary.RadiusMeters(),
		PrimaryMu:      w.ActiveCraft().Primary.GravitationalParameter(),
		State:          w.ActiveCraft().State,
		SimTime:        w.Clock.SimTime,
	}
	for i := range w.Missions {
		w.Missions[i].Status = w.Missions[i].Evaluate(ctx)
	}
}

// ActiveMission returns the first in-progress mission, or nil if all
// missions are passed/failed (or none are loaded). v0.6.5+. Used by
// the HUD to surface a single-line status — multi-mission UX is a
// follow-up.
func (w *World) ActiveMission() *missions.Mission {
	for i := range w.Missions {
		if w.Missions[i].Status == missions.InProgress {
			return &w.Missions[i]
		}
	}
	return nil
}

// nextFiniteBurnTrigger returns the BurnStart sim-time of the soonest
// pending finite-burn node (Duration > 0), or the zero time if no
// finite-burn node is queued. v0.5.14+: BurnStart is TriggerTime -
// Duration/2, so the Tick clamp lands on the moment the integrator
// will actually fire the engine, not on the (later) burn-center
// TriggerTime that the HUD displays.
func (w *World) nextFiniteBurnTrigger() time.Time {
	var best time.Time
	// v0.8.1+: walk every craft's node list — a planted burn on a
	// non-active craft still needs to clamp warp to its trigger
	// moment so the integrator doesn't overshoot.
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		for _, n := range c.Nodes {
			if n.Duration <= 0 {
				continue
			}
			if !n.IsResolved() {
				continue
			}
			t := n.BurnStart()
			if best.IsZero() || t.Before(best) {
				best = t
			}
		}
	}
	return best
}

// maybeRecordTrail appends the craft's current state (in its primary's
// frame) to the trail ring buffer when trailIntervalSec of sim time
// has elapsed since the last sample. Storing primary-relative R and
// re-translating at render time keeps the trail aligned with the
// craft's apparent orbit around its primary — pre-v0.5.4 we stored
// heliocentric inertial directly, which made LEO trails appear to
// drift through space at Earth's orbital speed (~30 km/s).
//
// The sim-time gating means trail density stays roughly constant
// across warp levels — at warp=1 we sample every ~10 sim seconds
// (≈200 ticks), at warp=10000× every ~10 sim seconds (≈one tick).
// Either way the visible trail covers trailCap × trailIntervalSec
// ≈ 33 minutes of sim history.
func (w *World) maybeRecordTrail(secs float64) {
	w.trailAccumSec += secs
	if w.trailAccumSec < trailIntervalSec {
		return
	}
	w.trailAccumSec = 0
	w.trail[w.trailIdx] = trailSample{
		primaryID: w.ActiveCraft().Primary.ID,
		relR:      w.ActiveCraft().State.R,
	}
	w.trailIdx = (w.trailIdx + 1) % trailCap
	if w.trailLen < trailCap {
		w.trailLen++
	}
}

// CraftTrail returns the trail samples in oldest-to-newest order,
// each translated into current-tick inertial coordinates via
// BodyPosition(sample.primary). The returned slice is a fresh copy —
// callers can iterate / reverse safely. Empty when the craft has
// just spawned and hasn't accumulated trailIntervalSec of sim time
// yet.
//
// Note: the inertial positions returned here move with the body each
// frame — a stationary LEO craft over 100 ticks produces samples whose
// raw stored .relR is identical, but whose translated inertial drifts
// with Earth. The trail effectively floats with the primary, which is
// what the player sees (Earth is fixed at canvas center under
// FocusBody, and the trail loops around it).
func (w *World) CraftTrail() []orbital.Vec3 {
	if w.trailLen == 0 {
		return nil
	}
	sys := w.System()
	out := make([]orbital.Vec3, w.trailLen)
	start := w.trailIdx - w.trailLen
	if start < 0 {
		start += trailCap
	}
	for i := 0; i < w.trailLen; i++ {
		s := w.trail[(start+i)%trailCap]
		// Re-translate to current inertial. Primary lookup falls back
		// to system primary if the recorded ID isn't found (e.g. a
		// system swap removed the body — defensive, shouldn't normally
		// happen).
		var primaryPos orbital.Vec3
		if b := sys.FindBody(s.primaryID); b != nil {
			primaryPos = w.BodyPosition(*b)
		}
		out[i] = primaryPos.Add(s.relR)
	}
	return out
}

// clampedWarp returns min(selected warp, max warp allowed by the step-size
// guard, burn-warp cap if a finite burn is active). max = (1024 sub-steps
// × period/100) / base_step. Active-burn cap = 10× per designdocs/terminal-space-program/plan.md
// §Time-warp UX — finite burns at >10× warp would let the integrator
// blast past the EndTime in a single tick and lose temporal resolution.
func (w *World) clampedWarp() float64 {
	selected := w.Clock.Warp()
	if w.ActiveCraft() == nil {
		// No craft to clamp against — return the player's own selected
		// warp untouched. The Auto-Warp max-seed below is deliberately
		// skipped here: with none of the clamps (period guard, approach
		// ramps) able to run, forcing the top factor would have nothing
		// to ramp it back down before a burn.
		return selected
	}
	// v0.16 / ADR 0016: while Auto-Warp is engaged (and not paused) the
	// driver max-seeds the "selected" baseline to the top warp factor and
	// lets every clamp below pick the real rate — it never touches
	// Clock.WarpIdx. Paused keeps the 0 from Clock.Warp() so a paused
	// engaged driver freezes time rather than racing ahead.
	if w.autoWarpEngaged() && !w.Clock.Paused {
		selected = WarpFactors[len(WarpFactors)-1]
	}
	// Any craft in flight in a finite or manual burn forces the
	// 10× cap — high warp during thrust would let the integrator
	// blast past the EndTime in a single tick. Walking all crafts
	// (not just the active one) catches a planted burn firing on a
	// non-active craft while the player is flying another.
	if w.anyCraftThrusting() && selected > 10 {
		selected = 10
	}
	// v0.8.6.x+: throttle-change clamp. A throttle adjust at high
	// warp ramps thrust faster than the integrator's per-tick step
	// can resolve, the same aliasing path that motivates the burn-
	// active cap above. Hold warp at 10× for a brief window after
	// any craft's throttle changed so the integrator absorbs the
	// new throttle before the next big sim-time leap.
	if selected > 10 && w.recentlyChangedThrottle(throttleClampWindow) {
		selected = 10
	}
	// v0.12 Slice 3 (ADR 0008): chute-descent clamp. An armed or deployed
	// parachute inside the atmosphere caps warp to 10×, like an active
	// burn — otherwise a single high-warp tick can leap across the q
	// deploy window (auto-deploy missed) or inflate the canopy in one
	// giant integration step (instant-inflation overshoot). Keeps the
	// per-tick sub-steps fine enough for the deploy check to resolve.
	if selected > 10 && w.anyCraftChuteLive() {
		selected = 10
	}
	// v0.8.6.x+: upcoming-node approach clamp. At 100000× warp one
	// tick advances 5000 s of sim time, so a planted node 30 s out
	// would be skipped entirely before the burn-active cap could
	// engage. Find the soonest upcoming TriggerTime across all
	// crafts and clamp warp so the integrator fits at least
	// approachClampSubsteps ticks inside the approach window. The
	// formula is continuous in approachTime — far-future nodes
	// produce a huge maxWarp (no effect), and as the node nears,
	// maxWarp ramps smoothly down toward the floor of 1×.
	if dt := w.soonestUpcomingNodeIn(); dt > 0 {
		maxNodeWarp := dt / (approachClampSubsteps * w.Clock.BaseStep.Seconds())
		if maxNodeWarp < 1 {
			maxNodeWarp = 1
		}
		if selected > maxNodeWarp {
			selected = maxNodeWarp
		}
	}
	// v0.16 / ADR 0016: Auto-Warp approach term. Anchored at the engaged
	// target T (= BurnStart − autoWarpLeadTime) rather than the burn
	// itself, so the effective rate ramps smoothly to the 1× floor by T
	// and the hand-off to 1× is a glide, not a slam. Same continuous
	// formula as the node-approach clamp above.
	if w.autoWarpEngaged() {
		if dt := w.AutoWarp.T.Sub(w.Clock.SimTime).Seconds(); dt > 0 {
			maxApproachWarp := dt / (approachClampSubsteps * w.Clock.BaseStep.Seconds())
			if maxApproachWarp < 1 {
				maxApproachWarp = 1
			}
			if selected > maxApproachWarp {
				selected = maxApproachWarp
			}
		}
	}
	mu := w.ActiveCraft().Primary.GravitationalParameter()
	period := orbitalPeriod(w.ActiveCraft().State, mu)
	if math.IsInf(period, 0) || math.IsNaN(period) || period <= 0 {
		return selected
	}
	maxStep := period / 100.0
	maxSimDelta := 1024.0 * maxStep // seconds — our sub-step cap
	maxWarp := maxSimDelta / w.Clock.BaseStep.Seconds()
	if selected > maxWarp {
		return maxWarp
	}
	return selected
}

// approachClampSubsteps is the number of integrator ticks the
// upcoming-node clamp tries to fit inside the approach window — high
// enough that the burn-active cap (10×) takes over with margin
// before the node fires. Picked at 10: at BaseStep 0.05 s, warp
// reaches 10× when the node is 5 s out (10 × 0.05 × 10 = 5), and
// the burn-active cap takes over within the same window once
// ActiveBurn populates. v0.8.6.x+.
const approachClampSubsteps = 10.0

// soonestUpcomingNodeIn returns the seconds of sim-time until the
// earliest resolved future planted node across all crafts. Returns
// -1 when no qualifying node exists (no nodes, only event-trigger
// nodes still resolving, or all nodes already past). Walks every
// craft so a planted burn on a non-active vessel still slows warp
// for the player flying another craft.
func (w *World) soonestUpcomingNodeIn() float64 {
	soonest := -1.0
	now := w.Clock.SimTime
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		for _, n := range c.Nodes {
			if !n.IsResolved() {
				continue
			}
			dt := n.TriggerTime.Sub(now).Seconds()
			if dt <= 0 {
				continue
			}
			if soonest < 0 || dt < soonest {
				soonest = dt
			}
		}
	}
	return soonest
}

// throttleClampWindow is the sim-time window after a Spacecraft's
// Throttle changes during which the warp clamp pins to 10×. Picked
// at 1 sim-second: long enough that one ManualBurn / RK4 sub-step
// at 10× warp absorbs the throttle ramp (BaseStep × 10 = 0.2 s ≪
// 1 s), short enough that the player feels warp returning to the
// selected level promptly after a Z / X tap. v0.8.6.x+.
const throttleClampWindow = time.Second

// recentlyChangedThrottle reports whether any craft's Throttle was
// updated within the last `window` of sim time. Walks every craft
// so a player flying craft A while craft B's planted burn ramps its
// throttle still triggers the clamp. v0.8.6.x+.
func (w *World) recentlyChangedThrottle(window time.Duration) bool {
	now := w.Clock.SimTime
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		if c.LastThrottleChangeAt.IsZero() {
			continue
		}
		if now.Sub(c.LastThrottleChangeAt) < window {
			return true
		}
	}
	return false
}

// anyCraftThrusting reports whether any craft in the slate is
// currently firing — either a planted finite burn or a held manual
// burn. Used by clampedWarp to apply the 10× burn cap regardless of
// which craft owns the burn. v0.8.1+.
func (w *World) anyCraftThrusting() bool {
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		if c.ActiveBurn != nil || c.ManualBurn != nil {
			return true
		}
	}
	return false
}

// anyCraftChuteLive reports whether any craft has an armed or deployed
// parachute while inside its primary's atmosphere. v0.12 Slice 3 (ADR
// 0008): drives the chute-descent warp clamp in clampedWarp. High warp
// lets a single tick span a large sim-time leap; for a chute craft that
// means the integrator can step clean across the q deploy window
// (auto-deploy missed → crash undeployed) or inflate the canopy in one
// giant step (the instant-inflation overshoot ADR 0008 banked). Walking
// all crafts mirrors the burn cap (a passive chute craft descending while
// the player flies another still needs fine sub-steps).
func (w *World) anyCraftChuteLive() bool {
	for _, c := range w.Crafts {
		if c == nil || c.ChuteState == spacecraft.ChuteStowed {
			continue
		}
		atm := c.Primary.Atmosphere
		if atm == nil {
			continue
		}
		if c.Altitude() < atm.CutoffAltitude {
			return true
		}
	}
	return false
}

// EffectiveWarp exposes the clamped warp for HUD display. Returns the same
// as Clock.Warp() when the user isn't hitting the step-size guard.
func (w *World) EffectiveWarp() float64 { return w.clampedWarp() }

// integrateOneCraft sub-steps the integrator for a single craft so
// each step dt obeys dt < period/100. When the craft is the active
// one and an ActiveBurn or ManualBurn is in flight, sub-steps run
// RK4 with engine thrust on top of gravity; otherwise pure Verlet
// for energy-conserving free flight. Non-active craft never thrust
// (planted nodes / manual burns belong to the active craft only in
// v0.8.1; per-craft node attribution defers to v0.9+).
//
// SOI check runs *inside* the sub-step loop (v0.4.2): when a sub-
// step crosses a sphere-of-influence boundary, the state is rebased
// to the new primary's frame and μ switches for subsequent steps.
//
// "Warp lock" (v0.4.3): when warp > 1× AND no active burn AND the
// orbit is bound with apoapsis comfortably inside the primary's SOI,
// take a single analytic Kepler step instead of looping Verlet.
// Multi-craft (v0.8.1+): each craft is integrated independently via
// this helper. Tick loops over Crafts.
func (w *World) integrateOneCraft(c *spacecraft.Spacecraft, simDelta time.Duration) {
	// v0.9.2+: Landed craft bypass normal integration. They co-rotate
	// with the primary's surface — R rotates about world +Z by ω·dt
	// per tick, V is recomputed as ω × R. No gravity, no drag, no
	// thrust. Cleared automatically when the engine ignites (see
	// StartManualBurn / planted-burn fire path). Without this, a
	// craft sitting on the pad with V = ω × r has gravitational
	// energy that puts its periapsis way below the primary's center;
	// warp time and the integrator flies it along that fictitious
	// orbit (= "shoots off into space" without the engine running).
	if c.Landed {
		integrateLanded(w, c, simDelta)
		return
	}
	// v0.11.4+ (ADR 0004): Crashed vessels skip integration entirely.
	// No gravity, no drag, no thrust, no slew — the wreckage sits at
	// its last clamped (lat, lon) on the surface and renders dimmed.
	// Cleared only by end-flight removal (`[E]`), which deletes the
	// vessel from the world rather than reviving it.
	if c.Crashed {
		return
	}
	mu := c.Primary.GravitationalParameter()
	period := orbitalPeriod(c.State, mu)
	secs := simDelta.Seconds()

	// v0.10.0: rate-limited attitude. Slew the physical nose toward
	// the commanded direction once per tick, BEFORE the Kepler
	// fast-path gate below — otherwise a coasting craft (the common
	// warp case, which early-returns at the gate) would never
	// converge and the navball would freeze mid-slew. dt is the
	// whole-tick warp-scaled sim seconds (constant angular velocity
	// in sim-time; collapses to ~instant at very high warp — accepted).
	// Landed craft are already out (they co-rotate; nose is cosmetic).
	// InstantSAS opts back into the legacy snap (the consumers in
	// stepThrust / the navball read commanded directly in that mode).
	if !w.InstantSAS {
		c.SlewToward(w.slewTargetFor(c), secs)
	}

	// Warp-lock fast path: analytic Kepler propagation in chunks small
	// enough that the craft can't outrun any other body's SOI per
	// chunk. Falls back to Verlet sub-stepping if the gate rejects
	// (active burn, hyperbolic, warp=1) or any chunk's KeplerStep
	// fails. Each craft owns its own ActiveBurn / ManualBurn so this
	// gate is per-craft.
	if w.canKeplerStep(c, simDelta) {
		if w.keplerStepWithSOICheck(c, simDelta) {
			return
		}
	}

	maxStep := period / 100.0
	if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
		maxStep = 1.0
	}
	nSteps := int(math.Ceil(secs / maxStep))
	if nSteps < 1 {
		nSteps = 1
	}
	// Cap sub-steps per tick so a warp spike can't grind the frame loop.
	// 1024 sub-steps per wall-tick at 20 Hz gives ≈ 20 kHz force evals.
	if nSteps > 1024 {
		nSteps = 1024
	}
	dt := secs / float64(nSteps)
	tickStart := w.Clock.SimTime.Add(-simDelta)
	stepDur := time.Duration(dt * float64(time.Second))

	sys := w.systemForCraft(c) // v0.16 / ADR 0015: integrate against the Vessel's own System.
	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	clock := tickStart
	// v0.12 Slice 3 (ADR 0008): a descending chute craft always takes
	// this Verlet path — canKeplerStepState rejects the analytic fast
	// path once periapsis dips below the atmosphere cutoff — so the
	// auto-deploy check lives here. It runs once per SUB-step (below),
	// not once per tick: at high warp a single tick spans a large
	// simDelta, and a capsule can cross the q deploy floor AND reach the
	// surface within one tick. A tick-granularity check would miss the
	// crossing and crash the capsule undeployed. The per-sub-step call
	// short-circuits cheaply once stowed/deployed/no-chute, and re-reads
	// the effective BC the moment the chute fires so the canopy drag
	// engages for the remaining sub-steps of the tick the deploy fires on.
	bc := c.EffectiveBallisticCoefficient()
	for i := 0; i < nSteps; i++ {
		if maybeDeployParachute(c) {
			bc = c.EffectiveBallisticCoefficient()
		}
		if w.thrustingAt(c, tickStart, dt, i) {
			w.stepThrust(c, mu, dt)
		} else {
			// v0.8.4: drag closure binds the craft's current primary
			// (re-bound after each SOI rebase below by the loop var
			// capture). Zero drag automatically when the primary has
			// no atmosphere or the craft is above cutoff.
			c.State = physics.StepVerletWithAccel(c.State, mu, dt, func(r, v orbital.Vec3) orbital.Vec3 {
				return physics.DragAccel(r, v, c.Primary, bc)
			})
		}
		// v0.8.5: halt sub-stepping at surface contact. Without this,
		// a craft that aerobrakes past altitude 0 keeps falling toward
		// r=0 and the gravity singularity slingshots it back out.
		// v0.11.4+ (ADR 0004): classify the contact as soft-touchdown
		// (Landed) or destructive impact (Crashed) using pre-clamp
		// V_impact + nose alignment + CanSoftLand catalog gate. The
		// predicate runs here (not inside physics.ClampToSurface) so
		// the physics package stays predicate-free and the sub-craft-
		// point inverse projection has access to the live sim clock.
		if clamped, hit := physics.ClampToSurface(c.State, c.Primary); hit {
			outcome, lat, lon := classifySurfaceArrival(c, c.State.R, c.State.V, clock)
			applySurfaceArrival(c, clamped, outcome, lat, lon)
			break
		}
		clock = clock.Add(stepDur)

		// Per-sub-step SOI re-evaluation. v0.8.4: refresh body
		// positions at the chunk's clock so high-warp ticks see body
		// motion within the tick — matches the predictor, which is
		// also time-aware. Without this an Earth→Mars Hohmann at high
		// warp diverges from the dashed predictor line.
		for _, b := range sys.Bodies {
			positions[b.ID] = w.BodyPositionAt(b, clock)
		}
		inertial := positions[c.Primary.ID].Add(c.State.R)
		cand := physics.FindPrimary(sys, inertial, positions)
		if cand.Body.ID != c.Primary.ID {
			vOld := w.bodyInertialVelocityAt(c.Primary, clock)
			vNew := w.bodyInertialVelocityAt(cand.Body, clock)
			c.State = physics.Rebase(c.State, positions[c.Primary.ID], cand.Inertial, vOld.Sub(vNew))
			c.Primary = cand.Body
			mu = c.Primary.GravitationalParameter()
		}
	}
	// Tear down the burn if it exhausted (Δv delivered, or EndTime passed
	// with fuel still aboard). v0.12.x pause-and-resume: if instead the
	// firing stage ran dry with Δv still owed, keep the burn alive but
	// stall it — push its EndTime by this tick's span so the duration
	// window is paused, not consumed, while the craft coasts. Thrust
	// auto-resumes the moment the player decouples to a fueled stage
	// (thrustingAt gates on ActiveStageFuel) so a multi-stage burn — e.g.
	// an Apollo TLI the S-IVB can't finish alone — carries across staging
	// instead of silently cancelling.
	if c.ActiveBurn != nil {
		if w.burnExhausted(c) {
			c.ActiveBurn = nil
		} else if c.ActiveStageFuel() <= 0 {
			c.ActiveBurn.EndTime = c.ActiveBurn.EndTime.Add(simDelta)
		}
	}
	// Manual burns end on fuel exhaustion only; the player ends them
	// explicitly via StopManualBurn (e.g. on `x`). v0.9.4+: check
	// the bottom-stage fuel rather than summed s.Fuel — the firing
	// engine has run dry, even if upper stages still hold propellant.
	if c.ManualBurn != nil && c.ActiveStageFuel() <= 0 {
		c.ManualBurn = nil
	}
}

// thrustingAt reports whether sub-step i of the current tick should fire
// the engine on the given craft. Caller is responsible for ensuring the
// craft is the active one (v0.8.1+: non-active craft never thrust).
// Either ActiveBurn (planted finite burn) or ManualBurn (v0.7.3+ player-
// held flight) qualifies; both share the same RK4 thrust path. Fuel
// must be positive in either case.
func (w *World) thrustingAt(c *spacecraft.Spacecraft, tickStart time.Time, dt float64, i int) bool {
	if c.ActiveStageFuel() <= 0 {
		return false
	}
	if c.ActiveBurn != nil {
		if c.ActiveBurn.DVRemaining <= 0 {
			return false
		}
		subStart := tickStart.Add(time.Duration(float64(i) * dt * float64(time.Second)))
		return subStart.Before(c.ActiveBurn.EndTime)
	}
	return c.ManualBurn != nil
}

// stepThrust advances one RK4 sub-step with engine thrust, debits the
// active-burn Δv budget by the analytical thrust contribution
// (Thrust×Throttle/mass × dt), and burns fuel via the configured mass
// flow. Dispatches between ActiveBurn (planted node, fixed mode +
// throttle captured at fire-time) and ManualBurn (v0.7.3+, mode and
// throttle driven by live World.AttitudeMode + Craft.Throttle).
//
// v0.7.6+: planted burns honour their per-node throttle rather than
// the live craft setting, so the player can tweak the throttle knob
// during a coast without slowing an in-flight planted burn.
// ToggleInstantSAS flips the legacy instant-attitude opt-out. Default
// (false) is rate-limited slew (v0.10.0+); true restores the pre-v0.10
// instant snap. Session UI preference — not persisted (see the
// InstantSAS field doc).
func (w *World) ToggleInstantSAS() { w.InstantSAS = !w.InstantSAS }

// attitudeContext resolves the burn mode and target snapshot the
// craft's attitude is driven by this tick. Extracted from stepThrust
// (v0.10.0) so the slew integrator, the thrust closure, and the
// navball sub-observer all agree on the *commanded* direction.
//
//   - mode: ActiveBurn.Mode (planted finite burn, fixed at fire) when
//     a burn is in flight, else the live c.AttitudeMode (SAS hold).
//   - (rT, vT): for ActiveBurn the target bound at plant time
//     (survives a mid-burn target switch); otherwise the live
//     World.Target snapshot so a manual hold can retarget. Non-target
//     modes ignore it. v0.9.3+ logic, unchanged — just relocated.
func (w *World) attitudeContext(c *spacecraft.Spacecraft) (mode spacecraft.BurnMode, rT, vT orbital.Vec3, planeRad float64, burnDir orbital.Vec3) {
	mode = c.AttitudeMode
	if c.ActiveBurn != nil {
		mode = c.ActiveBurn.Mode
		planeRad = c.ActiveBurn.PlaneChangeRad
		burnDir = c.ActiveBurn.BurnDirUnit
	}
	if c.ActiveBurn != nil && c.ActiveBurn.TargetCraftID != 0 {
		if tc, _, ok := w.craftByID(c.ActiveBurn.TargetCraftID); ok && tc.Primary.ID == c.Primary.ID {
			rT, vT = tc.State.R, tc.State.V
		}
	} else {
		rT, vT, _ = w.TargetStateRelativeToActivePrimary()
	}
	return mode, rT, vT, planeRad, burnDir
}

// commandedDirFor is the world-unit nose direction the craft's
// attitude is *commanded* toward this tick (recomputed from mode +
// live state). The slew integrator chases it; with InstantSAS the
// craft is pinned to it. v0.10.0+.
func (w *World) commandedDirFor(c *spacecraft.Spacecraft) orbital.Vec3 {
	mode, rT, vT, planeRad, burnDir := w.attitudeContext(c)
	return c.BurnDirectionForBurn(mode, rT, vT, planeRad, burnDir)
}

// slewTargetFor is the direction the slew integrator chases this tick.
// Normally the live commanded direction; Step 6 (lead-compensated
// nodes) overrides it with an upcoming node's ignition direction so
// the craft is converged at BurnStart. v0.10.0+.
// initCraftAttitude seeds a freshly-spawned craft's physical nose with
// its commanded direction so the navball is correct on the first frame
// (before any Tick runs the slew integrator). The integrator's
// zero-init snap guard already covers correctness — this is HUD
// polish. No-op if already set (defensive). v0.10.0+.
func (w *World) initCraftAttitude(c *spacecraft.Spacecraft) {
	if c == nil || c.CurrentAttitudeDir.Norm() != 0 {
		return
	}
	c.CurrentAttitudeDir = w.commandedDirFor(c)
}

func (w *World) slewTargetFor(c *spacecraft.Spacecraft) orbital.Vec3 {
	if dir, ok := w.nodeLeadActive(c); ok {
		return dir
	}
	return w.commandedDirFor(c)
}

func (w *World) stepThrust(c *spacecraft.Spacecraft, mu, dt float64) {
	mode, rT, vT, planeRad, burnDir := w.attitudeContext(c)
	throttle := c.EffectiveThrottle()
	if c.ActiveBurn != nil {
		throttle = c.ActiveBurn.Throttle
		if throttle <= 0 {
			// Defensive fallback: legacy v3-save ActiveBurn with no
			// captured throttle (loaded with zero) → treat as full
			// open, matching the pre-v0.7.6 universal behaviour.
			throttle = 1.0
		}
	}
	// v0.10.0: by default thrust along the craft's physical nose
	// (CurrentAttitudeDir, slewed at the tick top) so burning before
	// alignment bleeds Δv to cosine loss. InstantSAS restores the
	// legacy per-k-step recompute from the commanded mode (byte-
	// identical to pre-v0.10).
	var thrustFn func(r, v orbital.Vec3, t float64) orbital.Vec3
	switch {
	case w.InstantSAS && (mode == spacecraft.BurnPlaneChange || mode == spacecraft.BurnVector):
		// BurnPlaneChange's tilted direction and BurnVector's captured
		// direction can't be decoded from the BurnMode alone, so the
		// per-sub-step ThrustAccelFnAtWithTarget switch can't resolve
		// them. Under InstantSAS thrust along the resolved commanded
		// direction (recomputed per tick — ample for a short burn).
		thrustFn = c.ThrustAccelFnFixedDir(
			c.BurnDirectionForBurn(mode, rT, vT, planeRad, burnDir), mu, throttle)
	case w.InstantSAS:
		thrustFn = c.ThrustAccelFnAtWithTarget(mode, mu, throttle, rT, vT)
	default:
		thrustFn = c.ThrustAccelFnFixedDir(c.CurrentAttitudeDir, mu, throttle)
	}
	bc := c.EffectiveBallisticCoefficient()
	primary := c.Primary
	// v0.8.4: drag adds to thrust + gravity inside the RK4 closure so
	// finite-burn ascent / descent through atmosphere feels the
	// expected resistance.
	accelFn := func(r, v orbital.Vec3, t float64) orbital.Vec3 {
		return thrustFn(r, v, t).Add(physics.DragAccel(r, v, primary, bc))
	}
	c.State = physics.StepRK4(c.State, dt, accelFn, 0)

	if c.ActiveBurn != nil {
		mass := c.TotalMass()
		if mass > 0 {
			dvApplied := (c.Thrust * throttle / mass) * dt
			if dvApplied > c.ActiveBurn.DVRemaining {
				dvApplied = c.ActiveBurn.DVRemaining
			}
			c.ActiveBurn.DVRemaining -= dvApplied
		}
	}
	// v0.9.1+: route fuel burn through BurnFuel so Stages[0].FuelMass
	// (the source of truth) decrements + SyncFields keeps the flat
	// shadow fields coherent. Pre-v0.9.1 wrote `c.Fuel -= fuelBurned`
	// directly; with Stages now authoritative, that path would leave
	// the bottom stage's tank artificially full and the burn would
	// never terminate from fuel exhaustion.
	fuelBurned := c.MassFlowRateAt(throttle) * dt
	c.BurnFuel(fuelBurned)
	c.State.M = c.TotalMass()
}

// burnExhausted reports whether the active burn should be torn down:
// either its Δv was delivered, or its duration window elapsed while the
// firing stage still had fuel. v0.12.x: a dry firing stage with Δv still
// owed is NOT exhausted — it is stalled, and the caller keeps the burn
// alive (pausing its EndTime) so it resumes after the player stages. Only
// the fuelled-but-timed-out case terminates on duration.
func (w *World) burnExhausted(c *spacecraft.Spacecraft) bool {
	if c.ActiveBurn.DVRemaining <= 0 {
		return true
	}
	if c.ActiveStageFuel() <= 0 {
		return false // stalled, not exhausted — kept alive for staging
	}
	return !w.Clock.SimTime.Before(c.ActiveBurn.EndTime)
}

// canKeplerStep reports whether the analytic warp-lock fast path is
// valid for this tick. Conditions (v0.4.4):
//   - warp > 1× (else Verlet is fine and we want to avoid behavioral
//     differences between paused/realtime and the live integrator)
//   - no active burn (analytic propagation can't accommodate thrust)
//   - bound orbit (e < 1) — KeplerStep itself rejects hyperbolic cases
//
// SOI containment is no longer gated here: keplerStepWithSOICheck
// chunks the analytic step finely enough to detect crossings between
// chunks (v0.4.4 fix for the v0.4.3 heliocentric-transfer-skips-Mars
// bug). If e ≥ 1 we still fall back to Verlet so the per-sub-step SOI
// path handles the non-conic case correctly.
func (w *World) canKeplerStep(c *spacecraft.Spacecraft, simDelta time.Duration) bool {
	// Per-craft active-burn gating: a craft running its own burn or
	// manual-fire can't use the analytic Kepler fast path. Other
	// craft are unaffected.
	if c.ActiveBurn != nil || c.ManualBurn != nil {
		return false
	}
	// Gate on the EFFECTIVE warp this tick — the step size simDelta
	// actually implies — not the player's Selected Warp (Clock.Warp()).
	// Auto-Warp (ADR 0016) drives a high effective warp through
	// clampedWarp's max-seed WITHOUT touching WarpIdx, so Clock.Warp() can
	// read 1× while simDelta is thousands of seconds (e.g. right after a
	// prior Auto-Warp leg set WarpIdx=0). Gating on Clock.Warp() then sent
	// Auto-Warp down the Verlet slow path, which sub-steps at period/100 —
	// a single ~5000 s step through the perigee of a long-period eccentric
	// transfer orbit, aliasing it into a hyperbolic escape (the "Auto-Warp
	// to the plane change flings the craft into solar orbit" bug). At ≤1×
	// (realtime / paused) Verlet's fine sub-stepping is correct and we want
	// it for parity with the live integrator.
	base := w.Clock.BaseStep.Seconds()
	if base <= 0 || simDelta.Seconds() <= base {
		return false
	}
	return canKeplerStepState(c.State, c.Primary.GravitationalParameter(), c.Primary)
}

// canKeplerStepState is the orbital-geometry half of canKeplerStep,
// shared so the Predictor's Kepler-vs-Verlet switch (predict.go) agrees
// with the live integrator on which coast legs the analytic propagator
// can handle. Analytic Kepler propagation is valid only when:
//   - the orbit is bound (e < 1, a > 0) — KeplerStep rejects hyperbolic
//     and parabolic arcs (returns ok=false) anyway, but bail here too;
//   - v0.8.4: periapsis sits above the primary's atmospheric cutoff —
//     drag breaks the conic, so a peri that grazes atmosphere needs the
//     per-sub-step drag accel the Verlet path integrates;
//   - v0.11.4-followup: periapsis sits above the surface — an impactor
//     would Kepler-step straight through an airless body, since only the
//     sub-step loop's ClampToSurface knows about surface contact.
func canKeplerStepState(s physics.StateVector, mu float64, primary bodies.CelestialBody) bool {
	if mu <= 0 {
		return false
	}
	el := orbital.ElementsFromState(s.R, s.V, mu)
	if el.E >= 1 || el.A <= 0 {
		return false
	}
	periAlt := el.A*(1-el.E) - primary.RadiusMeters()
	if atm := primary.Atmosphere; atm != nil && periAlt < atm.CutoffAltitude {
		return false
	}
	if periAlt < 0 {
		return false
	}
	return true
}

// keplerStepWithSOICheck propagates the craft analytically across the
// tick by chunking simDelta into pieces small enough that the craft
// can't outrun any non-current-primary body's SOI per chunk. Between
// chunks, FindPrimary catches SOI crossings and rebases the state.
//
// Chunk size = min(simDelta, smallestForeignSOI / (4·speed)). The
// factor of 4 leaves a 2× safety margin past the trivial "can't
// traverse SOI in one chunk" bound — a bound orbit re-encountering
// the same SOI region within a single tick would otherwise risk a
// missed crossing at high warp.
//
// Returns ok=false if any chunk's KeplerStep fails (e.g. eccentricity
// crossed into hyperbolic mid-propagation due to a primary switch);
// caller then falls back to Verlet for the remaining time.
func (w *World) keplerStepWithSOICheck(c *spacecraft.Spacecraft, simDelta time.Duration) bool {
	sys := w.systemForCraft(c) // v0.16 / ADR 0015: SOI checks against the Vessel's own System.
	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPosition(b)
	}
	// v0.11.4-followup: snapshot the entry state + primary so a mid-
	// loop bail (KeplerStep failure or surface-impact predicate)
	// rewinds cleanly. Without the snapshot, partial analytic
	// propagation leaves c.State advanced through chunks 0..i-1
	// before the caller's Verlet fallback re-runs the *full*
	// simDelta from that already-advanced state — double-counting
	// the time. Pre-fix this only mattered when KeplerStep itself
	// failed mid-tick (rare); v0.11.4 adds a surface-impact bail
	// that triggers more often (every airless-body descent across
	// an SOI transition where peri flips sub-surface mid-tick),
	// surfacing the latent partial-advance bug.
	entryState := c.State
	entryPrimary := c.Primary

	chunkCap := chunkDtCap(sys, c.Primary, c.State.V.Norm())

	secs := simDelta.Seconds()
	if chunkCap <= 0 || math.IsInf(chunkCap, 0) || math.IsNaN(chunkCap) {
		chunkCap = secs
	}
	nChunks := int(math.Ceil(secs / chunkCap))
	if nChunks < 1 {
		nChunks = 1
	}
	// Safety cap matching the Verlet sub-step ceiling — a degenerate
	// near-zero chunk size shouldn't blow up the loop.
	if nChunks > 1024 {
		nChunks = 1024
	}
	chunk := secs / float64(nChunks)

	mu := c.Primary.GravitationalParameter()
	for i := 0; i < nChunks; i++ {
		// v0.11.4-followup: bail to Verlet whenever the current
		// orbit's periapsis sits below the primary's surface. This
		// catches both the bound-impactor case (peri sub-surface
		// from the start of the tick — canKeplerStep's gate would
		// already reject) AND the mid-tick SOI-transition case: a
		// craft cruising in Earth SOI with peri above Earth's
		// surface can rebase to Moon mid-chunk and find peri now
		// sub-surface in Moon's frame. Analytic propagation across
		// the next chunk would push the craft straight through the
		// moon; bail so the Verlet path runs ClampToSurface. The
		// entry-snapshot restore above ensures the caller's Verlet
		// re-runs the *full* simDelta from the original state
		// rather than double-stepping from the partial advance.
		if el := orbital.ElementsFromState(c.State.R, c.State.V, mu); el.E < 1 && el.A > 0 {
			periAlt := el.A*(1-el.E) - c.Primary.RadiusMeters()
			if periAlt < 0 {
				c.State = entryState
				c.Primary = entryPrimary
				return false
			}
		}
		newState, ok := physics.KeplerStep(c.State, mu, chunk)
		if !ok {
			c.State = entryState
			c.Primary = entryPrimary
			return false
		}
		c.State = newState

		inertial := positions[c.Primary.ID].Add(c.State.R)
		cand := physics.FindPrimary(sys, inertial, positions)
		if cand.Body.ID != c.Primary.ID {
			vOld := w.bodyInertialVelocity(c.Primary)
			vNew := w.bodyInertialVelocity(cand.Body)
			c.State = physics.Rebase(c.State, positions[c.Primary.ID], cand.Inertial, vOld.Sub(vNew))
			c.Primary = cand.Body
			mu = c.Primary.GravitationalParameter()
		}
	}
	return true
}

// chunkDtCap returns the maximum analytic-step duration for the
// current craft primary, given craft speed. Bound by the smallest
// in-reach foreign body's SOI radius / (4·speed) so no plausibly-
// reachable SOI can be traversed without an intermediate FindPrimary
// check. +Inf when no foreign SOI is in reach.
//
// "In reach" = siblings of the craft's primary (same ParentID, e.g.
// other planets when craft is in Earth SOI) plus direct children of
// the craft's primary (e.g. Luna when craft is in Earth SOI). Tinier
// distant SOIs (Phobos when craft is in Earth SOI, Galilean moons
// when heliocentric) are excluded — too small to enter from a tick's
// worth of travel given the parent-system geometry, and including
// them tanks chunk counts (Phobos's 13 km SOI would force ~1024
// chunks per tick from any planetary orbit).
//
// v0.5.0: pre-moons this just iterated all non-primary bodies. Adding
// moons necessitates the in-reach filter; deeper "is this SOI
// trajectory-reachable in dt" analysis is v0.5.x territory per
// designdocs/terminal-space-program/integration-design.md §6.
func chunkDtCap(sys bodies.System, currentPrimary bodies.CelestialBody, speed float64) float64 {
	if speed <= 0 {
		speed = 1.0
	}
	primaryID := sys.Bodies[0].ID
	cap := math.Inf(1)
	for _, b := range sys.Bodies {
		if b.ID == primaryID || b.ID == currentPrimary.ID {
			continue
		}
		isSibling := b.ParentID == currentPrimary.ParentID
		isChild := b.ParentID == currentPrimary.ID
		if !isSibling && !isChild {
			continue
		}
		parent := sys.ParentOf(b)
		if parent == nil {
			continue
		}
		soi := physics.SOIRadius(b, *parent)
		if soi <= 0 {
			continue
		}
		dt := soi / (4 * speed)
		if dt < cap {
			cap = dt
		}
	}
	return cap
}

// orbitalPeriod returns 2π√(a³/μ) or +Inf on unbound orbits. Used to
// size Verlet sub-steps.
func orbitalPeriod(s physics.StateVector, mu float64) float64 {
	a := physics.SemimajorAxis(s, mu)
	if a <= 0 || math.IsNaN(a) {
		return math.Inf(1)
	}
	return 2 * math.Pi * math.Sqrt(a*a*a/mu)
}

// maybeSwitchPrimaryFor runs FindPrimary on the given craft and, if a
// new body should own it, rebases its state vector. v0.8.1+: per-craft
// SOI re-evaluation; called for every craft in the slate. The 20-tick
// throttle still applies (the sub-step path inside integrateOneCraft
// catches mid-tick crossings; this is a backstop).
func (w *World) maybeSwitchPrimaryFor(c *spacecraft.Spacecraft) {
	sys := w.systemForCraft(c) // v0.16 / ADR 0015: resolve SOI against the Vessel's own System, not Sol.

	// Build body-position map in system-inertial.
	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPosition(b)
	}

	// Craft inertial position needs the *current* primary offset.
	craftInertial := positions[c.Primary.ID].Add(c.State.R)

	newPrimary := physics.FindPrimary(sys, craftInertial, positions)
	if newPrimary.Body.ID == c.Primary.ID {
		return
	}

	// Compute relative velocity between old and new primary so Rebase
	// gets the velocity delta correct. Planet velocities come from
	// orbital.VelocityAtTrueAnomaly evaluated at current sim time.
	vOld := w.bodyInertialVelocity(c.Primary)
	vNew := w.bodyInertialVelocity(newPrimary.Body)
	dv := vOld.Sub(vNew)

	oldPos := positions[c.Primary.ID]
	c.State = physics.Rebase(c.State, oldPos, newPrimary.Inertial, dv)
	c.Primary = newPrimary.Body
}

// bodyInertialVelocity returns the body's velocity in the system-
// inertial frame at the current sim time. Convenience wrapper over
// bodyInertialVelocityAt.
func (w *World) bodyInertialVelocity(b bodies.CelestialBody) orbital.Vec3 {
	return w.bodyInertialVelocityAt(b, w.Clock.SimTime)
}

// bodyInertialVelocityAt returns the body's velocity in the system-
// inertial frame at an arbitrary sim time. Mirrors BodyPositionAt
// for the chained-prediction-rebase use case. v0.8.2.x.
func (w *World) bodyInertialVelocityAt(b bodies.CelestialBody, t time.Time) orbital.Vec3 {
	if b.SemimajorAxis == 0 {
		return orbital.Vec3{}
	}
	M := w.Calculator.CalculateMeanAnomaly(b, t)
	E := orbital.SolveKepler(M, b.Eccentricity)
	nu := orbital.TrueAnomaly(E, b.Eccentricity)
	el := orbital.ElementsFromBody(b)

	sys := w.System()
	parent := sys.ParentOf(b)
	if parent == nil {
		// Malformed ParentID — treat as top-level around system primary.
		parent = sys.Primary()
	}
	mu := parent.GravitationalParameter()
	vRel := orbital.VelocityAtTrueAnomaly(el, nu, mu)
	if b.ParentID == "" {
		return vRel
	}
	return w.bodyInertialVelocityAt(*parent, t).Add(vRel)
}
