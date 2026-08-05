package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// proximityPairWorld builds two vessels sharing Earth: the active one at
// idx 0 and a sister placed by hand at a known offset from it, targeted.
// Placing the sister's state directly (rather than spawning at an
// altitude) is what lets the tests below assert exact relative geometry.
func proximityPairWorld(t *testing.T, relPos, relVel orbital.Vec3) *World {
	t.Helper()
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("expected 2 vessels after spawn, got %d", len(w.Crafts))
	}
	w.ActiveCraftIdx = 0
	active := w.ActiveCraft()
	// Put the sister exactly relPos/relVel away from the active vessel,
	// in the same primary-relative frame.
	w.Crafts[1].Primary = active.Primary
	w.Crafts[1].State.R = active.State.R.Add(relPos)
	w.Crafts[1].State.V = active.State.V.Add(relVel)
	w.SetTargetCraft(1)
	return w
}

// TestProximityStateResolvesRelativeMotion: the scene's readout triple
// must agree with the geometry it was built from, with closing positive
// while the gap shrinks (the TARGET chip's long-standing convention).
func TestProximityStateResolvesRelativeMotion(t *testing.T) {
	// Target is 1 km away along +X; we are drifting toward it at 2 m/s.
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{X: -2})
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused a resolvable vessel target")
	}
	if math.Abs(st.RangeM-1000) > 1e-6 {
		t.Errorf("RangeM = %g, want 1000", st.RangeM)
	}
	if math.Abs(st.VRelMS-2) > 1e-9 {
		t.Errorf("VRelMS = %g, want 2", st.VRelMS)
	}
	// The target moves at -2 m/s along +X relative to us, i.e. toward us.
	if st.ClosingMS <= 0 {
		t.Errorf("ClosingMS = %g, want > 0 while the gap shrinks", st.ClosingMS)
	}
	if math.Abs(st.ClosingMS-2) > 1e-9 {
		t.Errorf("ClosingMS = %g, want 2", st.ClosingMS)
	}
	// RelPos is US relative to THEM: we sit 1 km along -X from the target.
	if math.Abs(st.RelPos.X+1000) > 1e-6 {
		t.Errorf("RelPos.X = %g, want -1000 (own vessel relative to target)", st.RelPos.X)
	}
	// And the two world positions differ by exactly that.
	if d := st.CraftWorld.Sub(st.TargetWorld).Sub(st.RelPos).Norm(); d > 1e-6 {
		t.Errorf("CraftWorld - TargetWorld != RelPos (off by %g m)", d)
	}
}

// TestProximityStateOwnCraftScreenPosition: the whole point of the frame
// is that a known relative state lands in a known place on screen. A
// vessel trailing the target along -V and sitting below it must decompose
// to negative along-track (left of centre) and negative radial (below).
func TestProximityStateOwnCraftScreenPosition(t *testing.T) {
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	active := w.ActiveCraft()
	// Build the offset in the TARGET's own LVLH axes so the expectation
	// is stated in the frame's vocabulary, not in world axes.
	tgtR := active.State.R
	tgtV := active.State.V
	frame, ok := orbital.LVLHBasis(tgtR, tgtV)
	if !ok {
		t.Fatal("precondition: spawned orbit has no LVLH frame")
	}
	// The TARGET keeps the state the frame was solved from; the active
	// vessel is the one displaced — 800 m behind, 150 m below.
	offset := frame.AlongTrack.Scale(-800).Add(frame.RadialOut.Scale(-150))
	w.Crafts[1].Primary = active.Primary
	w.Crafts[1].State.R = tgtR
	w.Crafts[1].State.V = tgtV
	active.State.R = tgtR.Add(offset)
	w.SetTargetCraft(1)

	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused a resolvable vessel target")
	}
	local := st.Frame.ToFrame(st.RelPos)
	// Tolerance is sub-millimetre on a 6.8e6 m position vector — tight
	// enough to catch a swapped axis or a sign flip, loose enough for FP.
	const tol = 1e-6
	if math.Abs(local.X+800) > tol {
		t.Errorf("along-track = %g, want -800 (trailing → screen-left)", local.X)
	}
	if math.Abs(local.Y+150) > tol {
		t.Errorf("radial = %g, want -150 (below → screen-down)", local.Y)
	}
	if math.Abs(local.Z) > tol {
		t.Errorf("cross-track = %g, want 0 (in-plane offset)", local.Z)
	}
}

// TestProximityFrameRotatesWithTarget: the basis is rebuilt from the
// target's CURRENT state, so advancing the world visibly rotates it. If
// this ever froze, relative drift would unroll into an inertial spiral —
// the exact failure the target-centred-inertial alternative was rejected
// for.
func TestProximityFrameRotatesWithTarget(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
	before, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused a resolvable vessel target")
	}
	// A few minutes of a ~400 km orbit is several degrees of arc.
	for i := 0; i < 12; i++ {
		w.Clock.WarpUp()
	}
	for i := 0; i < 20; i++ {
		w.Tick()
	}
	after, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused after stepping the world")
	}
	cos := before.Frame.AlongTrack.Dot(after.Frame.AlongTrack)
	if cos > 0.9999 {
		t.Errorf("along-track axis barely moved (cos = %.6f) — frame is not tracking the target's orbit", cos)
	}
	// It is still a frame, not garbage.
	if math.Abs(after.Frame.AlongTrack.Norm()-1) > 1e-9 {
		t.Errorf("|AlongTrack| = %g, want 1", after.Frame.AlongTrack.Norm())
	}
}

// TestProximityRefusalPaths: every way in must either work or say why.
func TestProximityRefusalPaths(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
	if r := w.ProximityRefusal(); r != "" {
		t.Fatalf("vessel target refused: %q", r)
	}

	w.ClearTarget()
	if !w.ProximityAvailable() {
		if r := w.ProximityRefusal(); r == "" {
			t.Error("no target: refusal is empty — a silent no-op reads as broken")
		}
	} else {
		t.Error("ProximityAvailable with no target")
	}

	// A body target is the interesting refusal: the aim slot is full, but
	// with the wrong kind of thing.
	w.SetTargetBody(1)
	if w.ProximityAvailable() {
		t.Error("ProximityAvailable with a BODY target")
	}
	if r := w.ProximityRefusal(); r == "" {
		t.Error("body target: refusal is empty")
	}
}

// TestToggleProximityViewRoundTrip: press to enter, press to return to
// exactly the ViewMode you left — not the next stop on the `v` cycle.
func TestToggleProximityViewRoundTrip(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
	w.ViewMode = ViewOrbitFlat

	entered, refusal := w.ToggleProximityView()
	if !entered || refusal != "" {
		t.Fatalf("enter: entered=%v refusal=%q", entered, refusal)
	}
	if w.ViewMode != ViewProximity {
		t.Fatalf("ViewMode = %s, want proximity", w.ViewMode)
	}

	entered, refusal = w.ToggleProximityView()
	if entered || refusal != "" {
		t.Fatalf("leave: entered=%v refusal=%q", entered, refusal)
	}
	if w.ViewMode != ViewOrbitFlat {
		t.Errorf("ViewMode = %s, want orbit-flat (the view we jumped from)", w.ViewMode)
	}
}

// TestToggleProximityViewRefusesWithoutVesselTarget: the refusal is
// visible (a reason string the caller can toast) and leaves the view
// exactly where it was.
func TestToggleProximityViewRefusesWithoutVesselTarget(t *testing.T) {
	w := mustWorld(t)
	w.ViewMode = ViewTop
	entered, refusal := w.ToggleProximityView()
	if entered {
		t.Fatal("entered Proximity View with no vessel target")
	}
	if refusal == "" {
		t.Error("refusal is empty — a silent no-op reads as broken")
	}
	if w.ViewMode != ViewTop {
		t.Errorf("ViewMode = %s, want top (a refused jump must not move the camera)", w.ViewMode)
	}
}

// TestToggleProximityAlwaysLeaves: leaving never refuses, even once the
// target has gone stale — otherwise a vanished partner would strand the
// player in a view that can only apologise.
func TestToggleProximityAlwaysLeaves(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
	w.ViewMode = ViewTilted
	if entered, _ := w.ToggleProximityView(); !entered {
		t.Fatal("precondition: could not enter Proximity View")
	}
	w.ClearTarget()
	if _, refusal := w.ToggleProximityView(); refusal != "" {
		t.Errorf("leaving refused with %q — the exit must never be gated", refusal)
	}
	if w.ViewMode != ViewTilted {
		t.Errorf("ViewMode = %s, want tilted", w.ViewMode)
	}
}

// TestCycleViewModeSkipsUnavailableProximity: `v` steps over the slot
// when there is no vessel target, and lands on it when there is.
func TestCycleViewModeSkipsUnavailableProximity(t *testing.T) {
	w := mustWorld(t)
	w.ViewMode = ViewLaunch
	w.CycleViewMode()
	if w.ViewMode != ViewTilted {
		t.Errorf("no target: cycle from launch → %s, want tilted (proximity skipped)", w.ViewMode)
	}

	w2 := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
	w2.ViewMode = ViewLaunch
	w2.CycleViewMode()
	if w2.ViewMode != ViewProximity {
		t.Errorf("vessel target: cycle from launch → %s, want proximity", w2.ViewMode)
	}
	// Arriving by cycle still records a return address for the jump key.
	if w2.ProxReturnView != ViewLaunch {
		t.Errorf("ProxReturnView = %s, want launch", w2.ProxReturnView)
	}
}

// TestProximityHintHysteresis: the hint arms once on the way in and does
// not re-arm until the pair has separated past the release range — the
// one-chip-per-change discipline, protected against a range oscillating
// on the threshold.
func TestProximityHintHysteresis(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 100e3}, orbital.Vec3{})
	w.updateProximityHint()
	if w.ProximityHintActive() {
		t.Fatal("hint active at 100 km — outside the band")
	}

	// Cross in.
	setProximityRange(t, w, proximityHintRangeM-1000)
	w.updateProximityHint()
	if !w.ProximityHintActive() {
		t.Fatal("hint not active just inside the band")
	}

	// Oscillate back out just past the arm range but inside the release
	// range: still one crossing, so the hint stays exactly as it was.
	setProximityRange(t, w, proximityHintRangeM+2000)
	w.updateProximityHint()
	if !w.ProximityHintActive() {
		t.Error("hint dropped inside the hysteresis band — this is the flicker the band exists to prevent")
	}

	// Answering the hint (entering the view) retires it for this crossing.
	if entered, _ := w.ToggleProximityView(); !entered {
		t.Fatal("could not enter Proximity View")
	}
	w.ToggleProximityView() // back to the map
	w.updateProximityHint()
	if w.ProximityHintActive() {
		t.Error("hint came back after the player acted on it")
	}

	// Separate past the release range, then close again: a NEW crossing,
	// so the hint is offered afresh.
	setProximityRange(t, w, proximityHintReleaseM+5000)
	w.updateProximityHint()
	setProximityRange(t, w, proximityHintRangeM-1000)
	w.updateProximityHint()
	if !w.ProximityHintActive() {
		t.Error("hint did not re-arm on a fresh crossing")
	}
}

// TestProximityHintSuppressedInView: the chip advertises a key; inside
// the view it advertises, it has nothing to say.
func TestProximityHintSuppressedInView(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
	w.updateProximityHint()
	if !w.ProximityHintActive() {
		t.Fatal("precondition: hint not active at 1 km")
	}
	w.ViewMode = ViewProximity
	if w.ProximityHintActive() {
		t.Error("hint still active inside Proximity View")
	}
}

// TestProximityHintResetsOnRetarget: a new aim slot is a new question,
// so the crossing restarts rather than inheriting the old one's state.
func TestProximityHintResetsOnRetarget(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
	w.updateProximityHint()
	if !w.ProximityHintActive() {
		t.Fatal("precondition: hint not active at 1 km")
	}
	w.ClearTarget()
	w.updateProximityHint()
	if w.ProximityHintActive() {
		t.Error("hint survived losing the target")
	}
}

// setProximityRange moves the target vessel to sit exactly rangeM from
// the active vessel along its current separation direction, leaving the
// orbit otherwise untouched.
func setProximityRange(t *testing.T, w *World, rangeM float64) {
	t.Helper()
	active := w.ActiveCraft()
	tc, _, ok := w.ResolveTargetCraft()
	if !ok {
		t.Fatal("setProximityRange: target vessel did not resolve")
	}
	dir := tc.State.R.Sub(active.State.R).Unit()
	if dir.Norm() == 0 {
		dir = orbital.Vec3{X: 1}
	}
	tc.State.R = active.State.R.Add(dir.Scale(rangeM))
}
