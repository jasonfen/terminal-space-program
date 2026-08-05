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

// TestProximityDockGateReady checks all four quadrants around the
// DockingDistM / DockingVMS thresholds — ready iff BOTH gates are
// satisfied, using the exact same constants checkDocking's auto-fuse
// reads, so the ring and the actual dock event can never disagree.
func TestProximityDockGateReady(t *testing.T) {
	w := mustWorld(t)
	tests := []struct {
		name      string
		rangeM    float64
		vRelMS    float64
		wantReady bool
	}{
		{"inside both gates", DockingDistM - 1, DockingVMS - 0.01, true},
		{"outside range, inside velocity", DockingDistM + 1, DockingVMS - 0.01, false},
		{"inside range, outside velocity", DockingDistM - 1, DockingVMS + 0.01, false},
		{"outside both gates", DockingDistM + 1, DockingVMS + 0.01, false},
		{"exactly at range threshold (not <)", DockingDistM, DockingVMS - 0.01, false},
		{"exactly at velocity threshold (not <)", DockingDistM - 1, DockingVMS, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := ProximityState{RangeM: tc.rangeM, VRelMS: tc.vRelMS}
			if got := w.ProximityDockGateReady(st); got != tc.wantReady {
				t.Errorf("range=%.1f vRel=%.3f: ready=%v, want %v", tc.rangeM, tc.vRelMS, got, tc.wantReady)
			}
		})
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
