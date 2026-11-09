package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// plantOn plants an impulsive prograde node on the craft at slate idx,
// dt out from now, and returns its stable ID.
func plantOn(t *testing.T, w *World, idx int, dt time.Duration) uint64 {
	t.Helper()
	w.SetActiveCraftIdx(idx)
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(dt),
		DV:          10,
		Mode:        spacecraft.BurnPrograde,
	})
	nodes := w.Crafts[idx].Nodes
	return nodes[len(nodes)-1].ID
}

// TestEngageAutoWarpPicksGloballySoonest — engage aims at the earliest
// BurnStart across ALL vessels, freezing T = BurnStart − leadTime.
func TestEngageAutoWarpPicksGloballySoonest(t *testing.T) {
	w, a, b, _ := threeCraftSlate(t)

	// Active craft A: a burn 5 days out. Craft B: a sooner burn 2 days out.
	plantOn(t, w, 0, 5*24*time.Hour)
	bID := plantOn(t, w, 1, 2*24*time.Hour)
	w.SetActiveCraftIdx(0) // fly A; Auto-Warp must still aim at B's sooner burn

	if !w.EngageAutoWarp() {
		t.Fatal("EngageAutoWarp returned false with two eligible burns")
	}
	if w.AutoWarp == nil {
		t.Fatal("AutoWarp nil after a successful engage")
	}
	if w.AutoWarp.CraftID != b.ID || w.AutoWarp.NodeID != bID {
		t.Errorf("Auto-Warp aimed at craft %d node %d, want B (craft %d node %d)",
			w.AutoWarp.CraftID, w.AutoWarp.NodeID, b.ID, bID)
	}
	wantT := w.Crafts[1].Nodes[0].BurnStart().Add(-autoWarpLeadTime)
	if !w.AutoWarp.T.Equal(wantT) {
		t.Errorf("AutoWarp.T = %v, want BurnStart−lead = %v", w.AutoWarp.T, wantT)
	}
	_ = a
}

// TestEngageAutoWarpNoEligibleBurn — no nodes, or only burns inside the
// lead window, makes engage a no-op returning false.
func TestEngageAutoWarpNoEligibleBurn(t *testing.T) {
	w := mustWorld(t)
	if w.EngageAutoWarp() {
		t.Error("engage returned true with no nodes planted")
	}
	if w.AutoWarp != nil {
		t.Error("AutoWarp set despite no eligible burn")
	}
	// A burn inside the lead window is not eligible.
	plantOn(t, w, 0, autoWarpLeadTime/2)
	if w.EngageAutoWarp() {
		t.Error("engage returned true for a burn inside the lead window")
	}
}

// TestEngageAutoWarpAutoUnpauses — engaging while paused resumes time.
func TestEngageAutoWarpAutoUnpauses(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.Clock.Paused = true
	if !w.EngageAutoWarp() {
		t.Fatal("engage failed")
	}
	if w.Clock.Paused {
		t.Error("engaging Auto-Warp did not auto-unpause")
	}
}

// TestAutoWarpRaisesEffectiveWarp — with Selected Warp at 1× and a
// far-off target, the engaged driver max-seeds the effective rate well
// above 1×, without touching WarpIdx.
func TestAutoWarpRaisesEffectiveWarp(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 10*24*time.Hour)
	w.Clock.WarpIdx = 0 // Selected Warp 1×

	before := w.EffectiveWarp()
	if before != 1 {
		t.Fatalf("pre-engage effective warp = %v, want 1", before)
	}
	if !w.EngageAutoWarp() {
		t.Fatal("engage failed")
	}
	if got := w.EffectiveWarp(); got <= 1 {
		t.Errorf("engaged effective warp = %v, want > 1 (max-seeded)", got)
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("Auto-Warp mutated WarpIdx to %d; Selected Warp must stay untouched", w.Clock.WarpIdx)
	}
}

// TestAutoWarpPausedDoesNotAdvance — pausing while engaged keeps
// Auto-Warp on but freezes time (effective warp 0); it resumes on unpause.
func TestAutoWarpPausedDoesNotAdvance(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 10*24*time.Hour)
	w.EngageAutoWarp()
	w.Clock.Paused = true
	if got := w.EffectiveWarp(); got != 0 {
		t.Errorf("paused effective warp = %v, want 0", got)
	}
	if w.AutoWarp == nil {
		t.Error("pause disengaged Auto-Warp; it should stay engaged")
	}
	w.Clock.Paused = false
	if got := w.EffectiveWarp(); got <= 1 {
		t.Errorf("post-unpause effective warp = %v, want > 1", got)
	}
}

// TestAutoWarpRampsDownNearTarget — as SimTime nears T the effective
// rate ramps toward the 1× floor (the new approach term anchored at T).
func TestAutoWarpRampsDownNearTarget(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 10*24*time.Hour)
	w.Clock.WarpIdx = len(WarpFactors) - 1 // Selected at max so the term, not WarpIdx, governs
	w.EngageAutoWarp()

	far := w.EffectiveWarp()
	// Jump SimTime to a few seconds before T — well inside the ramp.
	w.Clock.SimTime = w.AutoWarp.T.Add(-3 * time.Second)
	near := w.EffectiveWarp()
	if near >= far {
		t.Errorf("effective warp did not ramp down near target: far=%v near=%v", far, near)
	}
	if near < 1 {
		t.Errorf("ramp undershot the 1× floor: %v", near)
	}
}

// TestAutoWarpReachingTargetDropsToOneXAndDisengages — once SimTime ≥ T
// the per-tick resolve forces Selected Warp to 1× and releases the driver.
func TestAutoWarpReachingTargetDropsToOneXAndDisengages(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.Clock.WarpIdx = 3
	w.EngageAutoWarp()

	w.Clock.SimTime = w.AutoWarp.T.Add(time.Second) // past the lead point
	w.resolveAutoWarp()

	if w.AutoWarp != nil {
		t.Error("Auto-Warp still engaged after reaching the target")
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("reaching the target left WarpIdx at %d, want 0 (1×)", w.Clock.WarpIdx)
	}
}

// TestAutoWarpDisengagesWhenNodeDeleted — deleting the target node makes
// the next resolve disengage, leaving Selected Warp untouched (a node-
// invalidated cancel, not a reached-target stop).
func TestAutoWarpDisengagesWhenNodeDeleted(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.Clock.WarpIdx = 4
	w.EngageAutoWarp()

	w.Crafts[0].Nodes = nil // delete the target
	w.resolveAutoWarp()

	if w.AutoWarp != nil {
		t.Error("Auto-Warp stayed engaged after its target was deleted")
	}
	if w.Clock.WarpIdx != 4 {
		t.Errorf("node-invalidated disengage clobbered WarpIdx to %d, want 4 (untouched)", w.Clock.WarpIdx)
	}
}

// TestAutoWarpRefreezesOnNodeMove — editing the target node's BurnStart
// re-freezes T to track it.
func TestAutoWarpRefreezesOnNodeMove(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.EngageAutoWarp()
	origT := w.AutoWarp.T

	// Push the node 1 day later (edit in place; ID is preserved).
	w.Crafts[0].Nodes[0].TriggerTime = w.Crafts[0].Nodes[0].TriggerTime.Add(24 * time.Hour)
	w.resolveAutoWarp()

	wantT := w.Crafts[0].Nodes[0].BurnStart().Add(-autoWarpLeadTime)
	if !w.AutoWarp.T.Equal(wantT) {
		t.Errorf("T not re-frozen after node move: T=%v want=%v", w.AutoWarp.T, wantT)
	}
	if w.AutoWarp.T.Equal(origT) {
		t.Error("T unchanged after the node moved 1 day later")
	}
}

// TestDisengageAutoWarpLeavesWarpIdx — a manual cancel drops the driver
// without touching Selected Warp, so the player falls back to their rate.
func TestDisengageAutoWarpLeavesWarpIdx(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.Clock.WarpIdx = 5
	w.EngageAutoWarp()
	w.DisengageAutoWarp()
	if w.AutoWarp != nil {
		t.Error("DisengageAutoWarp left AutoWarp set")
	}
	if w.Clock.WarpIdx != 5 {
		t.Errorf("manual disengage changed WarpIdx to %d, want 5", w.Clock.WarpIdx)
	}
}

// TestAutoWarpEndToEndThroughTick — engage, run real ticks, and confirm
// time advances fast then hands off to 1× near the burn with the driver
// released. Exercises the resolveAutoWarp + clampedWarp wiring in Tick.
func TestAutoWarpEndToEndThroughTick(t *testing.T) {
	w := mustWorld(t)
	burnDt := 6 * time.Hour
	plantOn(t, w, 0, burnDt)
	w.Clock.WarpIdx = 0
	start := w.Clock.SimTime
	if !w.EngageAutoWarp() {
		t.Fatal("engage failed")
	}
	target := w.AutoWarp.T

	for i := 0; i < 2_000_000 && w.AutoWarp != nil; i++ {
		w.Tick()
	}
	if w.AutoWarp != nil {
		t.Fatal("Auto-Warp never disengaged across the tick budget")
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("ended at WarpIdx %d, want 0 (1×)", w.Clock.WarpIdx)
	}
	// Landed at or just past T, and comfortably before the burn itself.
	if w.Clock.SimTime.Before(target) {
		t.Errorf("stopped before the lead point: SimTime=%v T=%v", w.Clock.SimTime, target)
	}
	if got := w.Clock.SimTime.Sub(start); got >= burnDt {
		t.Errorf("overshot the burn: advanced %v, burn was %v out", got, burnDt)
	}
}

// TestAutoWarpScopedToActiveSystem — Auto-Warp ignores a sooner burn on
// a vessel bound to a different System and targets the soonest burn in
// the active vessel's own System (ADR 0015 interaction; the camera
// follows the active vessel's System, so an off-system burn would warp
// to an off-screen event).
func TestAutoWarpScopedToActiveSystem(t *testing.T) {
	w, a, b, _ := threeCraftSlate(t)

	// Put B in a different System with a SOONER burn; A (active, system 0)
	// has a later burn. Auto-Warp must pick A's, not B's.
	b.SystemIdx = 1
	plantOn(t, w, 1, 1*24*time.Hour) // craft B, system 1, sooner
	aID := plantOn(t, w, 0, 4*24*time.Hour)
	w.SetActiveCraftIdx(0) // fly A in system 0

	if !w.EngageAutoWarp() {
		t.Fatal("engage failed with an eligible in-system burn")
	}
	if w.AutoWarp.CraftID != a.ID || w.AutoWarp.NodeID != aID {
		t.Errorf("targeted craft %d node %d, want A (craft %d node %d) — off-system burn leaked in",
			w.AutoWarp.CraftID, w.AutoWarp.NodeID, a.ID, aID)
	}

	// With only the off-system burn left, engage is a no-op.
	w.DisengageAutoWarp()
	w.Crafts[0].Nodes = nil
	if w.EngageAutoWarp() {
		t.Error("engaged on an off-system-only burn; should be a no-op")
	}
}

// TestApsisWarpStopsThirtySecondsShort: the [»] beside Ap / Pe warps an
// eccentric orbit to 30 s before that apsis, hands off at 1x, and a second
// press before arrival cancels. A circular orbit (no apsis countdown in
// NAVIGATION) has nothing to warp to.
func TestApsisWarpStopsThirtySecondsShort(t *testing.T) {
	w := mustWorld(t)
	if w.ToggleApsisWarp(true) {
		t.Fatal("engaged on the seed's circular orbit, which shows no Ap countdown")
	}
	for _, apo := range []bool{true, false} {
		w := mustWorld(t)
		c := w.ActiveCraft()
		// Faster and climbing: eccentric, and away from both apsides.
		c.State.V = c.State.V.Scale(1.05).Add(c.State.R.Unit().Scale(200))
		w.Clock.WarpIdx = 2 // the hand-off must bring it back to 1x
		if !w.ToggleApsisWarp(apo) || !w.ApsisWarpEngaged(apo) || w.ApsisWarpEngaged(!apo) {
			t.Fatalf("apo=%v: engage failed or flagged the wrong apsis", apo)
		}
		if w.ToggleApsisWarp(apo) || w.AutoWarp != nil {
			t.Fatalf("apo=%v: a second press did not cancel", apo)
		}
		w.ToggleApsisWarp(apo)
		for i := 0; i < 2_000_000 && w.AutoWarp != nil; i++ {
			w.Tick()
		}
		if w.AutoWarp != nil || w.Clock.WarpIdx != 0 {
			t.Fatalf("apo=%v: still engaged=%v at WarpIdx %d", apo, w.AutoWarp != nil, w.Clock.WarpIdx)
		}
		st := orbital.Vec3State{R: c.State.R, V: c.State.V}
		mu := c.Primary.GravitationalParameter()
		left := orbital.TimeToPeriapsis(st, mu)
		if apo {
			left = orbital.TimeToApoapsis(st, mu)
		}
		if left > autoWarpLeadTime.Seconds() || left < autoWarpLeadTime.Seconds()-5 {
			t.Errorf("apo=%v: stopped %.1f s before the apsis, want just under %v", apo, left, autoWarpLeadTime)
		}
	}
}

// TestCycleApsisWarpNearerThenOtherThenOff: alt+g warps to the nearer
// apsis, a second press switches to the other, a third cancels.
func TestCycleApsisWarpNearerThenOtherThenOff(t *testing.T) {
	w := mustWorld(t)
	if w.CycleApsisWarp() || w.AutoWarp != nil {
		t.Fatal("engaged on the seed's circular orbit")
	}
	c := w.ActiveCraft()
	c.State.V = c.State.V.Scale(1.05).Add(c.State.R.Unit().Scale(200))
	ta, _ := w.apsisWarpTarget(true)
	tp, _ := w.apsisWarpTarget(false)
	apoFirst := !ta.After(tp)
	if !w.CycleApsisWarp() || !w.ApsisWarpEngaged(apoFirst) {
		t.Fatalf("first press: want the nearer apsis (apo=%v)", apoFirst)
	}
	if !w.CycleApsisWarp() || !w.ApsisWarpEngaged(!apoFirst) {
		t.Fatalf("second press: want the other apsis (apo=%v)", !apoFirst)
	}
	if w.CycleApsisWarp() || w.AutoWarp != nil {
		t.Fatal("third press did not cancel")
	}
}

// TestApsisWarpSkipsAnApsisPastTheGround (2026-11-03): descending into the
// Moon on a 2.8 x -1,737 km path, neither apsis comes before the ground,
// so neither is a warp target (alt+g chased that periapsis into the
// surface). Climbing on the same kind of path, apoapsis comes first and
// still is.
func TestApsisWarpSkipsAnApsisPastTheGround(t *testing.T) {
	for _, c := range []struct {
		climb   float64
		apo, pe bool
	}{{-80, false, false}, {80, true, false}} {
		w, cr := ascendTestCraft(t, "moon", 800, c.climb)
		cr.State.V = cr.State.V.Add(orbital.Vec3{Y: 25})
		if got := w.ApsisWarpEligible(true); got != c.apo {
			t.Errorf("climb %+.0f m/s: Ap eligible = %v, want %v", c.climb, got, c.apo)
		}
		if got := w.ApsisWarpEligible(false); got != c.pe {
			t.Errorf("climb %+.0f m/s: Pe eligible = %v, want %v", c.climb, got, c.pe)
		}
	}
}

// Fix I: with no node, G on a descent warps to the brake cue less the lead
// and hands off at 1x with the cue about 30 s ahead.
func TestAutoWarpToBrakeCue(t *testing.T) {
	w, c := afterDeorbitBurn(t)
	in, ground, late := w.landingCueIn(c)
	if !ground || late || in < 2*autoWarpLeadTime {
		t.Fatalf("setup: cue in %v ground=%v late=%v, want a cue well ahead", in, ground, late)
	}
	if !w.AutoWarpEligible() || !w.EngageAutoWarp() || !w.AutoWarp.BrakeCue {
		t.Fatal("G with no node on a descent did not engage on the brake cue")
	}
	if got, want := w.AutoWarp.T.Sub(w.Clock.SimTime), in-autoWarpLeadTime; got < want-time.Second || got > want+time.Second {
		t.Errorf("target %v ahead, want cue - lead = %v", got, want)
	}
	for i := 0; i < 2_000_000 && w.AutoWarp != nil; i++ {
		w.Tick()
	}
	if w.AutoWarp != nil || w.Clock.WarpIdx != 0 {
		t.Fatalf("still engaged=%v at WarpIdx %d", w.AutoWarp != nil, w.Clock.WarpIdx)
	}
	if left, _, late := w.landingCueIn(c); late || left < autoWarpLeadTime-5*time.Second || left > autoWarpLeadTime+2*time.Second {
		t.Errorf("released with the cue %v ahead (late=%v), want about %v", left, late, autoWarpLeadTime)
	}
}

// A planted node wins over the cue; no cue (circular orbit) refuses as before.
func TestAutoWarpBrakeCueYieldsToNodeAndNeedsACue(t *testing.T) {
	w, c := afterDeorbitBurn(t)
	plantOn(t, w, 0, 24*time.Hour)
	if !w.EngageAutoWarp() || w.AutoWarp.BrakeCue || w.AutoWarp.NodeID == 0 {
		t.Errorf("with a node planted G did not chase it: %+v", w.AutoWarp)
	}
	w2 := mustWorld(t)
	if w2.AutoWarpEligible() || w2.EngageAutoWarp() {
		t.Error("G engaged on a circular orbit with no node")
	}
	_ = c
}

// Under N the autopilot's own cap and G's approach ramp both apply: the
// effective rate is the lower of the two, G never lifts the autopilot cap.
func TestAutoWarpBrakeCueUnderAutopilotCap(t *testing.T) {
	w, c := afterDeorbitBurn(t)
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	if !w.EngageAutoWarp() {
		t.Fatal("G refused under N on a coast to the cue")
	}
	w.Clock.WarpIdx = len(WarpFactors) - 1
	lim, capped := w.landingAutopilotWarpLimit()
	for i := 0; i < 50; i++ {
		w.Tick()
		if got := w.clampedWarp(); capped && got > lim+1e-9 {
			t.Fatalf("tick %d: warp %v above the autopilot cap %v", i, got, lim)
		}
	}
	_ = c
}

// Boundary: a cue exactly autoWarpLeadTime ahead leaves nothing to warp (refuse);
// one nanosecond more engages. Cue pinned through landingCueIn's cache.
func TestAutoWarpBrakeCueLeadBoundary(t *testing.T) {
	for _, tc := range []struct {
		extra time.Duration
		want  bool
	}{{0, false}, {time.Nanosecond, true}} {
		w, c := afterDeorbitBurn(t)
		w.landingCueIn(c) // fill the cache, then pin it
		rt := w.apRuntimeFor(c)
		rt.ground, rt.late = true, false
		rt.cueAt = w.Clock.SimTime.Add(autoWarpLeadTime + tc.extra)
		rt.nextCheck = w.Clock.SimTime.Add(time.Hour)
		if got := w.EngageAutoWarp(); got != tc.want {
			t.Errorf("cue lead+%v: engaged=%v, want %v", tc.extra, got, tc.want)
		}
	}
}

// A planted node on the active vessel keeps the cue path out, even one inside the lead.
func TestAutoWarpBrakeCueRefusedWithNodeInLead(t *testing.T) {
	w, _ := afterDeorbitBurn(t)
	plantOn(t, w, 0, autoWarpLeadTime/2)
	if w.EngageAutoWarp() {
		t.Errorf("G engaged with a node inside the lead: %+v", w.AutoWarp)
	}
}
