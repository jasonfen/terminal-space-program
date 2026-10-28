package sim

import (
	"math"
	"testing"
	"time"

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

// TestCycleViewModeFromProximityAnchorsOnReturnView: issue #348 §4 took
// ViewProximity out of the `v` cycle entirely — Proximity is a toggling
// jump key (`o`) now, never a cycle stop, target availability or not.
// Pressing `v` while inside it still has to produce ONE of the six
// projections, so CycleViewMode anchors on ProxReturnView (the map the
// `o` toggle would have returned to) and advances one step from there —
// mirroring the equivalent Launch case in
// TestManualCycleClearsSessionWithoutRestore.
func TestCycleViewModeFromProximityAnchorsOnReturnView(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
	w.ViewMode = ViewProximity
	w.ProxReturnView = ViewTop

	w.CycleViewMode()

	if w.ViewMode != ViewRight {
		t.Errorf("ViewMode = %s, want right (one step past ProxReturnView=top)", w.ViewMode)
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

// coCircularProximityWorld builds two vessels on the SAME circular orbit
// (idx 1 = target, unchanged from the spawn; idx 0 = active, rotated by
// deltaTheta about the orbit normal so it sits deltaTheta further along
// the same circle) — the formation-flying configuration
// TestProximityDriftPathMatchesAnalyticCoCircular checks against a
// closed-form result. Returns the world and the shared orbital radius.
func coCircularProximityWorld(t *testing.T, deltaTheta float64) (*World, float64) {
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
	tgtR, tgtV := active.State.R, active.State.V
	w.Crafts[1].Primary = active.Primary
	w.Crafts[1].State.R = tgtR
	w.Crafts[1].State.V = tgtV

	frame, ok := orbital.LVLHBasis(tgtR, tgtV)
	if !ok {
		t.Fatal("precondition: no LVLH frame for the spawned circular orbit")
	}
	n := frame.CrossTrack
	active.State.R = rotateAboutAxis(tgtR, n, deltaTheta)
	active.State.V = rotateAboutAxis(tgtV, n, deltaTheta)
	w.SetTargetCraft(1)
	return w, tgtR.Norm()
}

// TestProximityDriftPathMatchesAnalyticCoCircular checks
// ProximityDriftPath against a closed-form result that is entirely
// independent of predictStep/KeplerStep: two craft on the SAME circular
// orbit at a fixed angular phase offset Δθ have an EXACTLY constant
// separation in the LVLH frame for all time — elementary circular-motion
// geometry, since both craft's angle advances by the identical ω·t, so
// their angular difference (and therefore the r̂/v̂-frame decomposition
// of their separation) never changes:
//
//	local_along  = r·sin(Δθ)
//	local_radial = r·(cos(Δθ) − 1)
//	local_cross  = 0
//
// This single scenario does double duty as the frozen-frame regression
// guard the issue calls for: a caller that reused TODAY's frame for
// every future sample (instead of resolving each sample's frame from
// the target's OWN state at that instant) would show these two craft
// sweeping around each other as the real physics rotates out from under
// the frozen axes — exactly the bug orbital.LVLHBasis's doc comment
// warns about. Holding constant across the WHOLE returned path is what
// this test actually checks, not just at t=0.
func TestProximityDriftPathMatchesAnalyticCoCircular(t *testing.T) {
	const deltaTheta = 0.05 // rad — a few km of arc at LEO altitude
	w, r := coCircularProximityWorld(t, deltaTheta)

	points, ok := w.ProximityDriftPath(10 * time.Minute)
	if !ok {
		t.Fatal("ProximityDriftPath refused a resolvable co-circular pair")
	}
	if len(points) < 2 {
		t.Fatalf("got %d points, want at least 2", len(points))
	}

	want := orbital.Vec3{
		X: r * math.Sin(deltaTheta),
		Y: r * (math.Cos(deltaTheta) - 1),
	}
	const tol = 0.05 // metres — exact Kepler propagation, FP roundoff only
	for i, p := range points {
		if d := p.Local.Sub(want).Norm(); d > tol {
			t.Errorf("point %d (t=%s): Local = %+v, want %+v (off by %.4f m) — co-circular formation should show near-zero drift", i, p.T, p.Local, want, d)
		}
	}
}

// TestProximityDriftPathMatchesAnalyticDifferentRadii is the
// non-degenerate companion to the co-circular test above: two craft on
// DIFFERENT circular orbits (r1 = target, r2 = active), sharing a plane,
// so their separation genuinely changes shape over the horizon. Closed
// form (elementary circular-motion geometry, independent of
// predictStep): with target angle θ1(t) = ω1·t (reference zero) and
// craft angle θ2(t) = θ0 + ω2·t,
//
//	local_along(t)  = r2·sin(θ0 + (ω2 − ω1)·t)
//	local_radial(t) = r2·cos(θ0 + (ω2 − ω1)·t) − r1
//
// checked at every sample's OWN elapsed time (ProximityDriftPoint.T),
// not just at the end of the horizon.
func TestProximityDriftPathMatchesAnalyticDifferentRadii(t *testing.T) {
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("expected 2 vessels after spawn, got %d", len(w.Crafts))
	}
	w.ActiveCraftIdx = 0
	active := w.ActiveCraft()
	tgtR, tgtV := active.State.R, active.State.V
	w.Crafts[1].Primary = active.Primary
	w.Crafts[1].State.R = tgtR
	w.Crafts[1].State.V = tgtV

	frame, ok := orbital.LVLHBasis(tgtR, tgtV)
	if !ok {
		t.Fatal("precondition: no LVLH frame for the spawned circular orbit")
	}
	mu := active.Primary.GravitationalParameter()
	r1 := tgtR.Norm()
	const theta0 = 0.3 // rad — craft starts ahead of the target
	r2Val := r1 + 50e3 // 50 km higher than the target's circular radius
	v2 := math.Sqrt(mu / r2Val)

	n := frame.CrossTrack
	rHat0 := rotateAboutAxis(tgtR.Scale(1/r1), n, theta0)
	vHat0 := rotateAboutAxis(frame.AlongTrack, n, theta0)
	active.State.R = rHat0.Scale(r2Val)
	active.State.V = vHat0.Scale(v2)
	w.SetTargetCraft(1)

	omega1 := math.Sqrt(mu / (r1 * r1 * r1))
	omega2 := math.Sqrt(mu / (r2Val * r2Val * r2Val))

	points, ok := w.ProximityDriftPath(10 * time.Minute)
	if !ok {
		t.Fatal("ProximityDriftPath refused a resolvable pair")
	}
	if len(points) < 2 {
		t.Fatalf("got %d points, want at least 2", len(points))
	}

	const tol = 0.5 // metres — exact Kepler propagation, FP roundoff only
	for i, p := range points {
		secs := p.T.Seconds()
		phase := theta0 + (omega2-omega1)*secs
		want := orbital.Vec3{
			X: r2Val * math.Sin(phase),
			Y: r2Val*math.Cos(phase) - r1,
		}
		if d := p.Local.Sub(want).Norm(); d > tol {
			t.Errorf("point %d (t=%s): Local = %+v, want %+v (off by %.4f m)", i, p.T, p.Local, want, d)
		}
	}
}

// TestProximityDriftPathRefusalPaths: the same degenerate-input cases
// the rest of Proximity View already refuses (no active craft, no
// resolvable target) must refuse here too, plus a non-positive horizon —
// ProximityDriftPath's own new guard.
func TestProximityDriftPathRefusalPaths(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
	if _, ok := w.ProximityDriftPath(10 * time.Minute); !ok {
		t.Fatal("refused a resolvable vessel target")
	}
	if _, ok := w.ProximityDriftPath(0); ok {
		t.Error("accepted a zero horizon")
	}
	if _, ok := w.ProximityDriftPath(-time.Minute); ok {
		t.Error("accepted a negative horizon")
	}

	w.ClearTarget()
	if _, ok := w.ProximityDriftPath(10 * time.Minute); ok {
		t.Error("accepted a world with no target")
	}
}

// TestProximityDockGateReady walks the port gate's terms one at a time: ready
// iff the pair carries ports AND port range <= 0.5 m AND relative speed
// < 0.1 m/s AND the noses are within 4 degrees. It calls the same predicate
// checkDocking fuses on, so the ring and the dock can never disagree. The
// first row is the positive control.
func TestProximityDockGateReady(t *testing.T) {
	w := mustWorld(t)
	tests := []struct {
		name      string
		hasPort   bool
		portM     float64
		vRelMS    float64
		alignDeg  float64
		wantReady bool
	}{
		{"inside all three terms", true, 0.4, 0.05, 2, true},
		{"port range 0.6 m", true, 0.6, 0.05, 2, false},
		{"exactly 0.5 m (<= holds)", true, 0.5, 0.05, 2, true},
		{"rel speed exactly 0.1 (not <)", true, 0.4, DockingVMS, 2, false},
		{"rel speed 0.15", true, 0.4, 0.15, 2, false},
		{"lateral pass: 3 m port range at 0.05 m/s", true, 3, 0.05, 2, false},
		{"20 deg misaligned at 0.3 m", true, 0.3, 0.05, 20, false},
		{"exactly 4 deg (<= holds)", true, 0.4, 0.05, 4, true},
		{"4.5 deg", true, 0.4, 0.05, 4.5, false},
		{"no port on one side", false, 0.1, 0.01, 0, false},
		{"inside the OLD 50 m / 0.1 m/s pair but not at a port", true, 30, 0.01, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := ProximityState{HasPort: tc.hasPort, PortRangeM: tc.portM, VRelMS: tc.vRelMS, AlignDeg: tc.alignDeg, RangeM: tc.portM}
			if got := w.ProximityDockGateReady(st); got != tc.wantReady {
				t.Errorf("ready=%v, want %v", got, tc.wantReady)
			}
		})
	}
}

// ProximityDockGateReady and the real dock agree on a live pair: sweep a
// lateral pass and a misalignment through the real ProximityState and check
// the ring predicate equals the dock predicate at every point.
func TestProximityDockGateReadyMatchesDockPredicate(t *testing.T) {
	for _, c := range []struct {
		name           string
		gap, rel, tilt float64
	}{
		{"in gate", 0.4, 0.05, 2},
		{"lateral pass 0.05 m/s, port 3 m", 3, 0.05, 0},
		{"20 deg at 0.3 m", 0.3, 0.05, 20},
		{"too fast", 0.3, 0.3, 0},
	} {
		p := newPortPair(t, "rcs-tug", "rcs-tug", c.gap, c.rel, c.tilt)
		p.w.SetTargetCraft(1)
		st, ok := p.w.ProximityState()
		if !ok {
			t.Fatalf("%s: no ProximityState", c.name)
		}
		want := dockPairGateHolds(p.a, p.b)
		if got := p.w.ProximityDockGateReady(st); got != want {
			t.Errorf("%s: ring ready=%v but the dock gate says %v", c.name, got, want)
		}
	}
}

// TestProximityDockGateReadyFalseWhileLatched is the #372 acceptance
// regression test: a same-World re-arm latch (#343) between the active
// vessel and its Target must hold ProximityDockGateReady false even while
// the pair sits inside the whole gate, otherwise the ring lies green at the
// exact moment checkDocking is refusing the pair. The unlatched read is the
// positive control.
func TestProximityDockGateReadyFalseWhileLatched(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 0.3, 0, 0)
	w := p.w
	w.SetTargetCraft(1)
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused a resolvable vessel target")
	}
	if !w.ProximityDockGateReady(st) {
		t.Fatal("control: the unlatched pair in the gate must read ready")
	}
	w.localReArms = append(w.localReArms, localReArm{idA: p.a.ID, idB: p.b.ID})
	if w.ProximityDockGateReady(st) {
		t.Fatal("ProximityDockGateReady = true for a latched pair inside the gate: the ring would show green while the dock is refused")
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

// ADR 0063 section 2/3: docking keeps Proximity View up. The aim slot is
// stranded on the partner's retired ID (nothing clears it), no target
// resolves, and the scene anchors on the newest seam.
func TestProximityStateDockedCompositeAnchorsOnSeam(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
	w := p.w
	w.SetTargetCraft(1)
	partnerID := w.Crafts[1].ID
	w.DockCrafts(0, 1)
	if w.Target.Kind != TargetCraft || w.Target.CraftID != partnerID {
		t.Fatalf("aim slot = %+v, want the partner's retired ID %d kept", w.Target, partnerID)
	}
	st, ok := w.ProximityState()
	if !ok || !st.Docked {
		t.Fatalf("ProximityState ok=%v docked=%v, want the docked scene", ok, st.Docked)
	}
	if st.TargetName != "Bravo" {
		t.Errorf("partner = %q, want Bravo (the piece above the newest seam)", st.TargetName)
	}
	c := w.ActiveCraft()
	wantOff := float64(StackRows(c.Stages[:1])) * DockStrideM
	if !near(st.SeamOffsetM, wantOff, 1e-9) {
		t.Errorf("SeamOffsetM = %g, want %g", st.SeamOffsetM, wantOff)
	}
	if d := st.TargetWorld.Sub(st.CraftWorld).Sub(c.CurrentAttitudeDir.Unit().Scale(wantOff)).Norm(); d > 1e-6 {
		t.Errorf("scene centre is %g m off the seam point", d)
	}
	if st.HasPort || st.HullGapM != 0 || st.RangeM != 0 {
		t.Errorf("docked scene carries gap data: %+v", st)
	}
	if r := w.ProximityRefusal(); r != "" {
		t.Errorf("docked composite refused: %q", r)
	}
	// o out and back in while docked: admitted both ways.
	w.ViewMode = ViewOrbitFlat
	for _, wantEntered := range []bool{true, false, true} {
		if entered, refusal := w.ToggleProximityView(); entered != wantEntered || refusal != "" {
			t.Fatalf("toggle: entered=%v refusal=%q, want entered=%v", entered, refusal, wantEntered)
		}
	}
	// A body target while docked still gets the docked scene; a lone vessel
	// with no target is still refused.
	w.SetTargetBody(1)
	if !w.ProximityAvailable() {
		t.Error("docked composite with a body target refused")
	}
	w.Undock(0)
	w.ClearTarget()
	if w.ProximityAvailable() {
		t.Error("lone vessel with no target admitted")
	}
}
