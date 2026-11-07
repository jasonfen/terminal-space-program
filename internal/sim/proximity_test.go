package sim

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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
	r := w.ProximityRefusal()
	if !strings.Contains(r, "not a body") || !strings.Contains(r, "[t] target menu") || strings.Contains(r, "cycles") {
		t.Errorf("body target refusal = %q, want 'not a body' and the target menu", r)
	}
	// A site on another body refuses with where it is (ADR 0067 S3: a site on
	// this primary opens the landing view instead, pinned below).
	w.Sites = append(w.Sites, Site{ID: 1, Body: "Luna", System: w.System().Name})
	w.NextSiteID = 1
	w.SetTargetSite(1)
	if r := w.ProximityRefusal(); !strings.Contains(r, "site is on Luna") || strings.Contains(r, "cycles") {
		t.Errorf("off-primary site refusal = %q, want 'site is on Luna'", r)
	}
	w.ClearTarget()
	if r := w.ProximityRefusal(); !strings.Contains(r, "[t] target menu") || strings.Contains(r, "cycles") {
		t.Errorf("no-target refusal = %q, want the target menu", r)
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
	// The seam's gap centre: the lower hull's nose edge, half a gap on.
	wantOff := PortGeometryOf(c.Stages[:1], c.CurrentAttitudeDir).NoseM - dockPortTipInsetM + 0.3
	if !near(st.SeamOffsetM, wantOff, 1e-9) {
		t.Errorf("SeamOffsetM = %g, want %g", st.SeamOffsetM, wantOff)
	}
	if d := st.TargetWorld.Sub(st.CraftWorld).Sub(c.CurrentAttitudeDir.Unit().Scale(wantOff)).Norm(); d > 1e-3 { // both worlds are ~1.5e11 m from the Sun: float64 spacing there is 3e-5 m
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

// ADR 0067 S3 (decision 8): `o` on ground opens the landing view. A site, a
// landed vessel and a landed ghost on the vessel's primary open it with the
// horizon frame at the ground point; a body target still refuses; a site
// on another body refuses and says where it is. Leaving returns to the map.
func TestToggleProximityViewOpensOnGround(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	w.ViewMode = ViewOrbitFlat
	check := func(what string) ProximityState {
		t.Helper()
		if r := w.ProximityRefusal(); r != "" {
			t.Fatalf("%s: refused %q", what, r)
		}
		st, ok := w.ProximityState()
		if !ok || !st.Surface {
			t.Fatalf("%s: ok=%v Surface=%v, want the landing scene", what, ok, st.Surface)
		}
		rT, _, _ := w.TargetStateRelativeToActivePrimary()
		if d := st.Frame.RadialOut.Dot(rT.Unit()); d < 1-1e-9 {
			t.Errorf("%s: up . site radial = %g, want 1", what, d)
		}
		if d := math.Abs(st.Frame.AlongTrack.Dot(st.Frame.RadialOut)); d > 1e-9 {
			t.Errorf("%s: right is not horizontal (|right . up| = %g)", what, d)
		}
		return st
	}
	check("site")
	if entered, refusal := w.ToggleProximityView(); !entered || refusal != "" || w.ViewMode != ViewProximity {
		t.Fatalf("enter on a site: entered=%v refusal=%q view=%s", entered, refusal, w.ViewMode)
	}
	if entered, _ := w.ToggleProximityView(); entered || w.ViewMode != ViewOrbitFlat {
		t.Errorf("second press: entered=%v view=%s, want back on the map", entered, w.ViewMode)
	}

	// a landed vessel of ours
	lat, lon := w.Sites[0].LatDeg+1, w.Sites[0].LonDeg
	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = 0
	other := w.Crafts[len(w.Crafts)-1]
	other.Primary = c.Primary
	other.Landed, other.LandedLatDeg, other.LandedLonDeg = true, lat, lon
	integrateLanded(w, other, time.Second)
	w.SetTargetCraft(len(w.Crafts) - 1)
	check("landed vessel")

	// a landed ghost of a partner
	r, v := surfacePointState(c.Primary, lat, lon, w.Clock.SimTime)
	w.Ghosts = []Ghost{{Owner: "fp-b", CraftID: 3, Name: "Ghost-1", PrimaryID: c.Primary.ID, Landed: true, LatDeg: lat, LonDeg: lon,
		RelPos: r, Vel: v, Pos: w.BodyPosition(c.Primary).Add(r)}}
	w.SetTargetGhost("fp-b", 3)
	check("landed ghost")

	// a body still refuses; a site on another body refuses with the place
	w.SetTargetBody(1)
	if r := w.ProximityRefusal(); !strings.Contains(r, "not a body") {
		t.Errorf("body refusal = %q", r)
	}
	w.Sites[0].Body = "Earth"
	w.SetTargetSite(w.Sites[0].ID)
	if r := w.ProximityRefusal(); !strings.Contains(r, "site is on Earth") {
		t.Errorf("off-primary site refusal = %q, want 'site is on Earth'", r)
	}
}

// The landing view's readouts against a hand-placed state: 200 m up, 50 m
// short of the flag, falling 3 m/s while moving 10 m/s along the ground and
// 0.5 m/s to the right. alt, alt rate, fwd vel, lat vel, range and the
// fall-in-from-the-left geometry all follow from it.
func TestSurfaceProximityStateReadouts(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	s := w.Sites[0]
	rT, vT := surfacePointState(c.Primary, s.LatDeg, s.LonDeg, w.Clock.SimTime)
	up := rT.Unit()
	north, east := siteHorizon(c.Primary, up)
	// track runs east; right of an eastward track facing up is south. The first
	// call latches the track (pure east); the drift is added after.
	c.State.R = rT.Add(up.Scale(200)).Sub(east.Scale(50))
	c.State.V = vT.Add(up.Scale(-3)).Add(east.Scale(10))
	w.landTrack = landTrack{}
	if _, ok := w.ProximityState(); !ok {
		t.Fatal("no landing scene")
	}
	c.State.V = c.State.V.Add(north.Scale(-0.5))
	st, ok := w.ProximityState()
	if !ok || !st.Surface {
		t.Fatal("no landing scene")
	}
	near := func(name string, got, want, tol float64) {
		t.Helper()
		if math.Abs(got-want) > tol {
			t.Errorf("%s = %g, want %g +/- %g", name, got, want, tol)
		}
	}
	near("alt", st.AltM, 200+(rT.Norm()-c.Primary.RadiusMeters()), 0.01) // 50 m across a sphere is 0.7 mm
	near("alt rate", st.AltRateMS, -3, 0.01)
	near("fwd vel", st.FwdVelMS, 10, 0.01)
	// the latched track holds when the velocity swings: still east
	if d := st.Frame.AlongTrack.Dot(east); d < 0.999999 {
		t.Errorf("track moved with the velocity: right . east = %g", d)
	}
	near("lat vel (+ right)", st.LatVelMS, 0.5, 0.01)
	near("range", st.RangeM, math.Hypot(200, 50), 1e-3)
	if d := st.Frame.AlongTrack.Dot(east); d < 0.999 {
		t.Errorf("right . east = %g, want ~1 (the track runs east)", d)
	}
	// short of the flag means left of centre: negative along the track
	if x := st.RelPos.Dot(st.Frame.AlongTrack); x > -49 || x < -51 {
		t.Errorf("vessel x = %g, want -50 (falls in from the left)", x)
	}
	// a hover has no track: the right-hand axis points from the vessel to the flag
	c.State.V = vT
	w.landTrack = landTrack{}
	st, _ = w.ProximityState()
	if d := st.Frame.AlongTrack.Dot(east); d < 0.999 {
		t.Errorf("hover: right . east = %g, want ~1 (toward the flag)", d)
	}
}

// ADR 0067 decision 8: the arc ends where the vessel would stop. The Lander
// in orbit with the flag aimed: the arc starts at the plan's light (FromCue),
// ends on the stop the autopilot's own forecast gives (plain, pitch 0), and
// once the autopilot is braking it is integrated at the lean it flies.
func TestLandingStopArc(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	// fly the planted burn, then stop on the descent before the brake lights
	w.Clock.WarpIdx = 0
	for i := 0; i < 2*3600*4 && (len(c.Nodes) > 0 || c.ActiveBurn != nil); i++ {
		w.Tick()
	}
	if len(c.Nodes) > 0 {
		t.Fatal("deorbit burn never fired")
	}
	arc, ok := w.LandingStopArc()
	if !ok || !arc.Stopped || !arc.FromCue || arc.PitchUpRad != 0 || arc.Throttle != DeorbitBrakeThrottle {
		t.Fatalf("coast arc: ok=%v stopped=%v fromCue=%v pitch=%g", ok, arc.Stopped, arc.FromCue, arc.PitchUpRad)
	}
	if n := len(arc.Points); n < 10 {
		t.Fatalf("arc has %d points", n)
	}
	end := arc.Points[len(arc.Points)-1].Sub(w.BodyPosition(c.Primary))
	if h := end.Norm() - c.Primary.RadiusMeters(); h < 0 || h > 500 {
		t.Errorf("arc ends %g m above the ground, want a hover stop inside 500 m", h)
	}
	// engaged and braking: the lean comes from the autopilot
	c.Landing = &spacecraft.LandingAutopilot{Engaged: true, Phase: spacecraft.PhaseBrake, PitchUpRad: 0.2}
	c.Throttle = 0.95 // the autopilot's trim, off the 0.90 plan
	arc2, ok := w.LandingStopArc()
	if !ok || arc2.FromCue || arc2.PitchUpRad != 0.2 || math.Abs(arc2.Throttle-c.EffectiveThrottle()) > 1e-9 {
		t.Errorf("braking arc: ok=%v fromCue=%v pitch=%g throttle=%g, want the autopilot's 0.2 and %g from the vessel", ok, arc2.FromCue, arc2.PitchUpRad, arc2.Throttle, c.EffectiveThrottle())
	}
}
