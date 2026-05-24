// Package sim — v0.11.0 Slice 1 regression suite for the ViewLaunch
// route/release state machine. Tests exercise the per-tick handler
// installed in World.Tick that detects active-slot Landed-false→true
// transitions and apo-crossing auto-release.
package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLaunchRouteOnLandedTransition — tracer bullet for v0.11.0
// Slice 1. After a Launchpad spawn (which sets the new active vessel
// Landed=true), the next World.Tick fires the per-tick route handler:
// ViewMode switches to ViewLaunch, a session opens
// (LaunchSessionActive=true), and the prior ViewMode is stashed in
// PrevViewMode so auto-release can later restore it.
//
// Proves the path end-to-end: the route handler is wired into
// World.Tick, the Landed-transition detection (wasActiveLanded
// false→true) fires, and the three primary observable session-state
// fields are written.
func TestLaunchRouteOnLandedTransition(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// ViewTilted is the zero-value default; capture it explicitly so
	// the assertion below tracks whatever the world started in.
	priorView := w.ViewMode

	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}

	// Per-tick route handler runs inside World.Tick, before the render
	// layer reads ViewMode. One tick should be enough — the Landed
	// shadow defaults to false, the spawned active is Landed=true,
	// and the transition fires on first read.
	w.Tick()

	if w.ViewMode != ViewLaunch {
		t.Errorf("after Landed spawn + Tick: ViewMode = %v, want ViewLaunch", w.ViewMode)
	}
	if !w.LaunchSessionActive {
		t.Error("after Landed spawn + Tick: LaunchSessionActive = false, want true")
	}
	if w.PrevViewMode != priorView {
		t.Errorf("PrevViewMode = %v, want %v (the ViewMode at the moment of route)",
			w.PrevViewMode, priorView)
	}
}

// TestLaunchRouteSeedsSessionState — the route handler also writes
// the rest of the session-scoped state: LaunchT0 stamped to current
// sim-time (so the HUD T+ readout has its anchor), LaunchMaxQ zeroed
// (no peak dynamic pressure carried from any prior session), the
// breadcrumb trail cleared, and LaunchZoom reset to 0 (auto-zoom).
//
// Pre-pollutes the four fields with non-default values to verify the
// route actively resets them — not just that a fresh World happens to
// have zeroes there.
func TestLaunchRouteSeedsSessionState(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	// Pre-pollute. A real session would never leave these non-zero
	// outside an active session, but a deterministic test must verify
	// the *reset*, not just default initialisation.
	w.LaunchMaxQ = 42.0
	w.LaunchZoom = 99.0
	w.LaunchTrail = []TrailPoint{
		{LatDeg: 10, LonDeg: 20, AltM: 30},
	}

	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}
	w.Tick()

	if w.LaunchT0.IsZero() {
		t.Error("LaunchT0 was not stamped after route")
	}
	if !w.LaunchT0.Equal(w.Clock.SimTime) {
		t.Errorf("LaunchT0 = %v, want %v (the post-tick sim-time anchor)",
			w.LaunchT0, w.Clock.SimTime)
	}
	if w.LaunchMaxQ != 0 {
		t.Errorf("LaunchMaxQ = %v, want 0 (route must reset)", w.LaunchMaxQ)
	}
	if w.LaunchZoom != 0 {
		t.Errorf("LaunchZoom = %v, want 0 (route must reset to auto)", w.LaunchZoom)
	}
	// Route clears the polluted trail; the same-tick sampler then
	// seeds a fresh sample (empty-buffer cadence rule), so len = 1
	// is the steady-state — but the sample must NOT be the polluted
	// {10, 20, 30}.
	if len(w.LaunchTrail) != 1 {
		t.Errorf("LaunchTrail len = %d, want 1 (cleared + fresh sample seeded)",
			len(w.LaunchTrail))
	} else if w.LaunchTrail[0].LatDeg == 10 && w.LaunchTrail[0].LonDeg == 20 {
		t.Errorf("LaunchTrail still contains the polluted pre-route sample")
	}
}

// TestLaunchAutoReleaseAtApoFloor — when the active craft is in
// an orbit whose apoapsis exceeds LaunchMissionFloorM (200 km), the
// per-tick handler's auto-release predicate fires: PrevViewMode is
// restored to ViewMode, LaunchSessionActive flips false, and the
// session-scoped state (T0, MaxQ, trail, zoom) clears so the next
// route doesn't see stale data.
//
// Predicate parity with v0.10.7's LaunchAnchorPhi (`apoAlt >
// LaunchMissionFloorM`) is the architectural commitment from ADR
// 0002 — the ORBIT READY callout and the ViewLaunch release fire
// off the same gate.
func TestLaunchAutoReleaseAtApoFloor(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("earth")
	if earth == nil {
		t.Fatal("setup: earth not found in default system")
	}
	mu := earth.GravitationalParameter()
	primaryR := earth.RadiusMeters()

	// 201 km circular orbit — apoAlt = 201 km, just past the 200 km
	// floor. Same construction pattern as launch_anchor_test.go's
	// `craftAt` helper.
	r := primaryR + 201_000
	v := math.Sqrt(mu / r)
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	c.Primary = *earth
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: v},
		M: c.TotalMass(),
	}
	// Replace the default LEO craft with our synthesized 201 km craft.
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0

	// Plant the world mid-session: route already fired, ViewMode is
	// ViewLaunch, PrevViewMode remembers ViewTop.
	w.LaunchSessionActive = true
	w.PrevViewMode = ViewTop
	w.ViewMode = ViewLaunch
	w.LaunchT0 = w.Clock.SimTime
	w.LaunchMaxQ = 1234.0
	w.LaunchTrail = []TrailPoint{{LatDeg: 5, LonDeg: 6, AltM: 100_000}}
	w.LaunchZoom = 0.5

	w.Tick()

	if w.LaunchSessionActive {
		t.Error("after apo>floor + Tick: LaunchSessionActive still true, want false (auto-release)")
	}
	if w.ViewMode != ViewTop {
		t.Errorf("ViewMode = %v, want ViewTop (restored from PrevViewMode)", w.ViewMode)
	}
	if w.LaunchMaxQ != 0 {
		t.Errorf("LaunchMaxQ = %v, want 0 (release must clear)", w.LaunchMaxQ)
	}
	if w.LaunchZoom != 0 {
		t.Errorf("LaunchZoom = %v, want 0 (release must clear)", w.LaunchZoom)
	}
	if len(w.LaunchTrail) != 0 {
		t.Errorf("LaunchTrail len = %d, want 0 (release must clear)", len(w.LaunchTrail))
	}
	if !w.LaunchT0.IsZero() {
		t.Errorf("LaunchT0 = %v, want zero (release must clear)", w.LaunchT0)
	}
}

// TestLaunchAutoReleaseSkipsHyperbolic — a launch session whose
// active craft is on a hyperbolic (e ≥ 1) or degenerate (a ≤ 0)
// trajectory must NOT auto-release. Apoapsis is undefined on
// hyperbolic orbits; the plan's contract is that the session stays
// open until the trajectory becomes bound again (the player is in
// a state they should be looking at anyway).
//
// Setup: craft at 100 km altitude with 1.5× local escape velocity,
// guaranteeing e > 1.
func TestLaunchAutoReleaseSkipsHyperbolic(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("earth")
	if earth == nil {
		t.Fatal("setup: earth not found")
	}
	mu := earth.GravitationalParameter()
	primaryR := earth.RadiusMeters()

	r := primaryR + 100_000
	vEsc := math.Sqrt(2 * mu / r)
	v := vEsc * 1.5 // generously hyperbolic
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	c.Primary = *earth
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: v},
		M: c.TotalMass(),
	}
	// Pre-flight check: confirm the orbit is actually hyperbolic, so
	// a future change to constants doesn't silently turn this test
	// into a bound-orbit test that vacuously passes.
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if el.E < 1 {
		t.Fatalf("setup: orbit not hyperbolic (e = %.3f); test cannot exercise the exempt branch", el.E)
	}

	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	w.LaunchSessionActive = true
	w.PrevViewMode = ViewTop
	w.ViewMode = ViewLaunch
	w.LaunchT0 = w.Clock.SimTime

	w.Tick()

	if !w.LaunchSessionActive {
		t.Error("hyperbolic orbit: session was auto-released, want it to stay active")
	}
	if w.ViewMode != ViewLaunch {
		t.Errorf("ViewMode = %v, want ViewLaunch (no release on hyperbolic)", w.ViewMode)
	}
}

// TestManualCycleClearsSessionWithoutRestore — when the player
// manually presses `v` to leave ViewLaunch mid-session, the session
// sentinel clears but PrevViewMode is NOT restored. ViewMode advances
// to the next mode in the cycle order (ViewLaunch → wraps to
// ViewTilted), matching the player's mental model: cycle = move
// forward, not "go back."
//
// Auto-release will not fire again for this session even if apo
// later crosses the floor — the sentinel is cleared, the player has
// taken over view selection.
func TestManualCycleClearsSessionWithoutRestore(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	// Plant mid-session as if the route handler had fired earlier:
	// ViewMode is ViewLaunch, PrevViewMode remembers the player's
	// prior choice (ViewTop), sentinel is on.
	w.LaunchSessionActive = true
	w.PrevViewMode = ViewTop
	w.ViewMode = ViewLaunch

	w.CycleViewMode()

	if w.LaunchSessionActive {
		t.Error("after manual cycle out of ViewLaunch: LaunchSessionActive = true, want false")
	}
	if w.ViewMode != ViewTilted {
		t.Errorf("ViewMode = %v, want ViewTilted (next after ViewLaunch in the cycle), NOT %v (PrevViewMode — cycle is advance, not restore)",
			w.ViewMode, w.PrevViewMode)
	}
}

// TestManualLaunchEntryNeverAutoReleases — a player who cycles into
// ViewLaunch outside of a session (LaunchSessionActive == false)
// gets a standalone chase-cam view: the auto-release predicate
// stays gated off, even when the active craft's apoapsis exceeds
// LaunchMissionFloorM. Locks the contract that auto-release only
// fires for *route-opened* sessions, not manually-entered views.
func TestManualLaunchEntryNeverAutoReleases(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("earth")
	mu := earth.GravitationalParameter()
	primaryR := earth.RadiusMeters()

	// 201 km circular orbit — would trigger auto-release if a session
	// were active.
	r := primaryR + 201_000
	v := math.Sqrt(mu / r)
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	c.Primary = *earth
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: v},
		M: c.TotalMass(),
	}
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0

	// Simulate manual entry: player has cycled `v` keys until ViewMode
	// lands on ViewLaunch. No session opened — LaunchSessionActive
	// stays false, PrevViewMode is meaningless.
	w.ViewMode = ViewLaunch
	// LaunchSessionActive deliberately left at its zero value (false).

	w.Tick()

	if w.LaunchSessionActive {
		t.Error("manual entry: session opened unexpectedly")
	}
	if w.ViewMode != ViewLaunch {
		t.Errorf("ViewMode = %v, want ViewLaunch (auto-release fired on a manual entry — should be gated)",
			w.ViewMode)
	}
}

// TestSwitchInSessionToLandedHandsOff — switch handler quadrant 1:
// player is mid-session on craft B, switches active to craft A which
// is also Landed. Hand-off: session stays active, ViewMode stays
// ViewLaunch, LaunchT0 re-stamps to the switch moment, trail / MaxQ
// / zoom clear. Player sees the new vessel as if it had just spawned
// (T+ ticking from the switch).
func TestSwitchInSessionToLandedHandsOff(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Spawn two launchpad vessels at different sites so they're
	// distinguishable. Each spawn makes the new craft active.
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft A: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     0,
	}); err != nil {
		t.Fatalf("SpawnCraft B: %v", err)
	}
	// NewWorld pre-seeds a default LEO craft at idx 0; our two
	// launchpad spawns become idx 1 (A) and idx 2 (B). The latest
	// spawn (B) is active.
	const idxA, idxB = 1, 2
	if w.ActiveCraftIdx != idxB {
		t.Fatalf("setup: active idx = %d, want %d (latest spawn = B)", w.ActiveCraftIdx, idxB)
	}

	// First tick: route fires for B (the Landed active). T0 stamped.
	w.Tick()
	if !w.LaunchSessionActive {
		t.Fatalf("setup: expected route to open a session on first tick after Landed spawn")
	}
	t0BeforeSwitch := w.LaunchT0

	// Pollute session-scoped state to verify hand-off reset.
	w.LaunchMaxQ = 7777
	w.LaunchTrail = append(w.LaunchTrail, TrailPoint{LatDeg: 9, LonDeg: 9})
	w.LaunchZoom = 0.3

	// Switch to craft A (also Landed). This advances sim-time
	// not at all — but the *next* Tick will advance it before the
	// switch handler runs, so the re-stamped T0 must differ.
	w.SetActiveCraftIdx(idxA)

	w.Tick()

	if !w.LaunchSessionActive {
		t.Error("hand-off ended the session, want it kept")
	}
	if w.ViewMode != ViewLaunch {
		t.Errorf("ViewMode = %v, want ViewLaunch (hand-off keeps the view)", w.ViewMode)
	}
	if !w.LaunchT0.After(t0BeforeSwitch) {
		t.Errorf("LaunchT0 = %v, want strictly after %v (hand-off must re-stamp)",
			w.LaunchT0, t0BeforeSwitch)
	}
	if w.LaunchMaxQ != 0 {
		t.Errorf("LaunchMaxQ = %v, want 0 (hand-off must clear)", w.LaunchMaxQ)
	}
	// Hand-off clears the polluted trail; the same-tick sampler then
	// seeds a fresh sample for the new vessel. The polluted {9, 9}
	// entry must be gone.
	if len(w.LaunchTrail) != 1 {
		t.Errorf("LaunchTrail len = %d, want 1 (cleared + fresh sample for new vessel)",
			len(w.LaunchTrail))
	} else if w.LaunchTrail[0].LatDeg == 9 && w.LaunchTrail[0].LonDeg == 9 {
		t.Errorf("LaunchTrail still contains the polluted pre-hand-off sample")
	}
	if w.LaunchZoom != 0 {
		t.Errorf("LaunchZoom = %v, want 0 (hand-off must clear)", w.LaunchZoom)
	}
}

// TestSwitchInSessionToFlyingEndsSession — switch handler quadrant
// 2: player is mid-session on a Landed vessel, switches active to a
// flying (non-Landed) vessel. Session ends, ViewMode restores to
// PrevViewMode, session state clears. Player gets their saved view
// back, focused on the new flying vessel.
//
// Critically the substitute vessel orbits at <200km apo, so the
// auto-release predicate cannot fire and cover for an unimplemented
// end-branch — the test isolates the switch handler's end quadrant.
func TestSwitchInSessionToFlyingEndsSession(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("earth")
	mu := earth.GravitationalParameter()
	primaryR := earth.RadiusMeters()

	// Replace the default LEO craft (apo > 200km would let auto-release
	// fake the end behavior) with a 100km-circular craft — well below
	// LaunchMissionFloorM so auto-release stays off.
	r := primaryR + 100_000
	v := math.Sqrt(mu / r)
	cLow := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	cLow.Primary = *earth
	cLow.State = physics.StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: v},
		M: cLow.TotalMass(),
	}
	w.Crafts[0] = cLow

	// Spawn a launchpad vessel at idx 1 (becomes active).
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}
	if w.ActiveCraftIdx != 1 {
		t.Fatalf("setup: active = %d, want 1", w.ActiveCraftIdx)
	}

	// First tick: route fires for the launchpad vessel.
	w.Tick()
	if !w.LaunchSessionActive {
		t.Fatalf("setup: route didn't open a session")
	}
	stashedView := w.PrevViewMode // captured by the route handler.

	// Switch to the 100km flying craft.
	w.SetActiveCraftIdx(0)
	w.Tick()

	if w.LaunchSessionActive {
		t.Error("session should have ended after switch to flying vessel")
	}
	if w.ViewMode != stashedView {
		t.Errorf("ViewMode = %v, want %v (PrevViewMode restored)",
			w.ViewMode, stashedView)
	}
}

// TestSwitchNotInSessionToLandedOpensFreshSession — switch handler
// quadrant 3, the quadrant the pre-grill design missed: player is
// outside a session, switches active onto a Landed vessel. A fresh
// session must open inline (same effect as a route-handler call).
//
// Without this branch, the player would land in a Landed vessel with
// no session — the Landed-transition predicate (step 2) wouldn't fire
// because the switch handler eagerly updates wasActiveLanded to
// `true`, suppressing the transition detection on the same tick.
//
// Sequence: spawn Landed → tick (session opens) → switch to flying
// (session ends via quadrant 2) → switch back to Landed (this branch
// must open a fresh session).
func TestSwitchNotInSessionToLandedOpensFreshSession(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("earth")
	mu := earth.GravitationalParameter()
	primaryR := earth.RadiusMeters()

	// Replace default LEO with a 100km craft so auto-release doesn't
	// muddy the switch-handler behavior.
	r := primaryR + 100_000
	v := math.Sqrt(mu / r)
	cLow := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	cLow.Primary = *earth
	cLow.State = physics.StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: v},
		M: cLow.TotalMass(),
	}
	w.Crafts[0] = cLow

	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}

	// Step 1: tick to open the initial session.
	w.Tick()
	if !w.LaunchSessionActive {
		t.Fatalf("setup: initial route didn't fire")
	}

	// Step 2: switch to flying craft, tick → session ends (quadrant 2).
	w.SetActiveCraftIdx(0)
	w.Tick()
	if w.LaunchSessionActive {
		t.Fatalf("setup: switch to flying should have ended session (quadrant 2)")
	}

	// Step 3: switch back to the Landed craft. Now we're !in-session
	// and switching onto a Landed vessel — quadrant 3 must open a
	// fresh inline session.
	w.SetActiveCraftIdx(1)
	w.Tick()

	if !w.LaunchSessionActive {
		t.Error("quadrant 3: fresh inline session not opened on switch to Landed vessel")
	}
	if w.ViewMode != ViewLaunch {
		t.Errorf("ViewMode = %v, want ViewLaunch (fresh inline session)", w.ViewMode)
	}
	if w.LaunchT0.IsZero() {
		t.Error("LaunchT0 was not stamped by the fresh-inline-session branch")
	}
}

// TestSwitchBetweenFlyingVesselsIsNoOp — switch handler quadrant 4:
// player switches between two flying (non-Landed) vessels with no
// active session. Should be a complete no-op for the ViewLaunch
// state machine — no session opens, ViewMode unchanged, no fields
// touched. Locks the contract that the switch handler doesn't
// spuriously perturb session-irrelevant transitions.
func TestSwitchBetweenFlyingVesselsIsNoOp(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("earth")
	mu := earth.GravitationalParameter()
	primaryR := earth.RadiusMeters()

	// Two crafts at sub-floor circular orbits (well below the 200km
	// auto-release predicate). Replace default idx 0 + a second
	// craft appended at idx 1.
	craftAt := func(altM float64) *spacecraft.Spacecraft {
		r := primaryR + altM
		v := math.Sqrt(mu / r)
		c := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
		c.Primary = *earth
		c.State = physics.StateVector{
			R: orbital.Vec3{X: r},
			V: orbital.Vec3{Y: v},
			M: c.TotalMass(),
		}
		return c
	}
	w.Crafts[0] = craftAt(80_000)
	w.Crafts = append(w.Crafts, craftAt(120_000))
	w.ActiveCraftIdx = 0

	// Tick once with idx 0 active to seed the shadow (so the next
	// switch is detected, rather than the first-tick snapshot path).
	w.Tick()
	if w.LaunchSessionActive {
		t.Fatalf("setup: session leaked open with no Landed craft and no plant")
	}
	preView := w.ViewMode

	w.SetActiveCraftIdx(1)
	w.Tick()

	if w.LaunchSessionActive {
		t.Error("flying→flying switch opened a session unexpectedly")
	}
	if w.ViewMode != preView {
		t.Errorf("ViewMode = %v, want %v (no-op switch shouldn't touch view)", w.ViewMode, preView)
	}
}

// TestUndockRenumberIsNotASwitch — the v0.10+ undock path at
// internal/sim/docking.go:330 shifts ActiveCraftIdx down when the
// active craft sits above the dropped slot. The logical active is
// unchanged, only its position in Crafts moved. A pointer-keyed
// shadow correctly treats this as a no-op; an index-keyed shadow
// would spuriously fire the switch handler (re-stamping T0, clearing
// trail/MaxQ/zoom) on every undock.
//
// Without invoking the full undock path, the test directly shifts
// Crafts and ActiveCraftIdx to simulate the renumber while
// preserving the active pointer, then confirms session state stays
// untouched across the next Tick.
func TestUndockRenumberIsNotASwitch(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}
	activeCraft := w.ActiveCraft()
	activeIdxBefore := w.ActiveCraftIdx

	// Tick to open the session.
	w.Tick()
	if !w.LaunchSessionActive {
		t.Fatalf("setup: route didn't open a session")
	}
	t0Before := w.LaunchT0
	w.LaunchMaxQ = 5555 // canary value the hand-off branch would zero.
	w.LaunchTrail = append(w.LaunchTrail, TrailPoint{LatDeg: 1, LonDeg: 2})

	// Simulate undock renumber: prepend a decoy craft to Crafts so
	// the active's slot shifts up by one. ActiveCraftIdx must follow
	// to keep the logical-active pointer stable (the mirror of
	// docking.go:330's `w.ActiveCraftIdx--` for the post-drop case).
	decoy := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	w.Crafts = append([]*spacecraft.Spacecraft{decoy}, w.Crafts...)
	w.ActiveCraftIdx = activeIdxBefore + 1

	if w.ActiveCraft() != activeCraft {
		t.Fatalf("setup: active pointer changed across the renumber simulation")
	}

	w.Tick()

	if !w.LaunchSessionActive {
		t.Error("renumber spuriously ended the session")
	}
	if !w.LaunchT0.Equal(t0Before) {
		t.Errorf("LaunchT0 changed across renumber: %v → %v (hand-off must NOT fire)",
			t0Before, w.LaunchT0)
	}
	if w.LaunchMaxQ != 5555 {
		t.Errorf("LaunchMaxQ = %v, want 5555 preserved (renumber must not run hand-off clear)", w.LaunchMaxQ)
	}
	if len(w.LaunchTrail) == 0 {
		t.Error("LaunchTrail cleared by spurious hand-off across renumber")
	}
}
