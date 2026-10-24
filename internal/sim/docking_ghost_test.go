package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0053 slice E: the approacher captures a friend's REPORTED port, in its
// own World, through the same gate and magnet as a local dock. These tests
// drive StepGhostCapture / CheckGhostBumps the way the serve layer does (once
// per tick, after the ghost slate is fresh), advancing the clock by hand so a
// ghost pinned at its reported state stays comparable to the vessel.

// asGhost reports vessel v the way relay's reporter would and a viewer's
// ghost slate would decode it: the derived hull, port and pointing.
func asGhost(v *spacecraft.Spacecraft, owner string, id uint64) Ghost {
	g := VesselPortGeometry(v)
	gh := Ghost{
		Owner: owner, CraftID: id, Name: v.Name, PrimaryID: v.Primary.ID,
		RelPos: v.State.R, Vel: v.State.V,
		Attitude: g.Axis, NoseM: g.NoseM, WidthM: g.WidthM,
	}
	for _, p := range g.Ports {
		gh.Ports = append(gh.Ports, GhostPort{OffsetM: p.OffsetM, Name: p.Name})
	}
	return gh
}

const ghostOwner = "SHA256:friend"

// ghostPair is newPortPair with the partner turned into a ghost: the world
// holds only the active vessel, w.Ghosts holds the partner's report.
func ghostPair(t *testing.T, partA, partB string, gapM, relMS, tiltDeg float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	p := newPortPair(t, partA, partB, gapM, relMS, tiltDeg)
	p.w.Crafts = p.w.Crafts[:1]
	p.w.Ghosts = []Ghost{asGhost(p.b, ghostOwner, 42)}
	p.w.Ghosts[0].Pos = p.w.BodyPosition(p.a.Primary).Add(p.w.Ghosts[0].RelPos)
	p.w.Clock.WarpIdx = 0
	return p.w, p.a
}

// stepGhost advances sim time by 50 ms and runs the capture once, returning
// whether the pull completed on this step.
func stepGhost(w *World) (GhostRef, bool) {
	w.Clock.SimTime = w.Clock.SimTime.Add(50 * time.Millisecond)
	return w.StepGhostCapture(nil)
}

// runGhost steps for sim seconds and returns when the pull completed (zero
// if it did not).
func runGhost(w *World, seconds float64) (completedAt float64) {
	start := w.Clock.SimTime
	end := start.Add(time.Duration(seconds * float64(time.Second)))
	for w.Clock.SimTime.Before(end) {
		if _, done := stepGhost(w); done {
			return w.Clock.SimTime.Sub(start).Seconds()
		}
	}
	return 0
}

// ADR 0053 tests table, cross-player: 0.4 m / 0.05 m/s / 2 deg starts the
// magnet; the Claim waits for the pull to finish; nothing at 4 s, done by 5 s.
func TestGhostCaptureCompletesAfterFiveSecondsNotBefore(t *testing.T) {
	w, a := ghostPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	if at := runGhost(w, 4); at != 0 {
		t.Fatalf("pull completed at %.2f s, inside the 5 s magnet", at)
	}
	if !w.GhostCapturing(ghostOwner, 42) {
		t.Fatal("no pull running against the ghost after 4 s with the gate held")
	}
	at := runGhost(w, 2)
	if at == 0 {
		t.Fatal("pull never completed")
	}
	if total := 4 + at; total < 4.95 || total > 5.2 {
		t.Errorf("pull completed after %.2f s, want ~5", total)
	}
	_ = a
}

// The pull moves the approacher onto the ghost's port and turns its nose onto
// the port axis; the ghost is untouched (one mover: ADR 0053 item 2).
func TestGhostCaptureMovesOnlyTheApproacher(t *testing.T) {
	w, a := ghostPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	before := w.Ghosts[0]
	if runGhost(w, 7) == 0 {
		t.Fatal("pull never completed")
	}
	g := w.Ghosts[0]
	if g.RelPos != before.RelPos || g.Vel != before.Vel || g.Attitude != before.Attitude {
		t.Errorf("ghost moved: %+v -> %+v", before, g)
	}
	ga, gg := VesselPortGeometry(a), GhostPortGeometry(g)
	if gap := gg.PortPoint(g.RelPos, 0).Sub(ga.PortPoint(a.State.R, 0)).Norm(); gap > 1e-6 {
		t.Errorf("port gap after the pull = %.6f m, want 0", gap)
	}
	if ang := PortAlignmentDeg(a.CurrentAttitudeDir, g.Attitude); ang > 1e-6 {
		t.Errorf("noses %.6f deg off after the pull, want 0", ang)
	}
}

// A friend whose report carries no shape (an older build, a landed craft)
// draws as the diamond and cannot be captured, however well it sits.
func TestGhostWithoutShapeCannotBeCaptured(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", 0.3, 0, 0)
	g := w.Ghosts[0]
	g.Attitude, g.NoseM, g.WidthM, g.Ports = orbital.Vec3{}, 0, 0, nil
	w.Ghosts[0] = g
	if at := runGhost(w, 12); at != 0 {
		t.Fatalf("a ghost with no reported shape was captured at %.2f s", at)
	}
	if w.GhostCapturing(ghostOwner, 42) {
		t.Error("a pull started on a ghost with no reported shape")
	}
}

// A shape without a port (a reported S-IVB) never captures either.
func TestGhostWithoutPortCannotBeCaptured(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "s-ivb-1", 0.3, 0, 0)
	if len(w.Ghosts[0].Ports) != 0 {
		t.Fatalf("fixture: S-IVB reported ports %v", w.Ghosts[0].Ports)
	}
	if at := runGhost(w, 12); at != 0 || w.GhostCapturing(ghostOwner, 42) {
		t.Fatalf("a port-less ghost was captured (at %.2f s)", at)
	}
}

// The magnet lets go when the ghost leaves the gate mid-pull (here its report
// turns it 10 degrees); nothing completes and the pull does not restart.
func TestGhostCaptureLetsGoWhenGhostTurnsOutOfGate(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	if runGhost(w, 2) != 0 || !w.GhostCapturing(ghostOwner, 42) {
		t.Fatal("fixture: pull should be running at 2 s")
	}
	g := w.Ghosts[0]
	g.Attitude = orbital.Rotate(g.Attitude, orbital.Vec3{Z: 1}, 10*math.Pi/180).Unit()
	w.Ghosts[0] = g
	if at := runGhost(w, 8); at != 0 {
		t.Fatalf("pull completed at %.2f s after the ghost turned out of the gate", at)
	}
	if w.GhostCapturing(ghostOwner, 42) {
		t.Error("magnet still pulling a ghost that left the gate")
	}
}

// Once a pull completes the pair stays latched: no second pull (and no second
// Claim) while it sits in the gate.
func TestGhostCaptureDoesNotRestartAfterCompleting(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	if runGhost(w, 7) == 0 {
		t.Fatal("pull never completed")
	}
	for i := 0; i < 100; i++ {
		if _, done := stepGhost(w); done {
			t.Fatal("a second pull completed on the same pair")
		}
	}
	if w.GhostCapturing(ghostOwner, 42) {
		t.Error("a second pull started on a completed pair")
	}
}

// A ghost the ledger would refuse (re-arm latch, engaged end) never starts a
// pull and is reported as a gate refusal so the serve layer can arm the
// ledger's notice; the ring never reads ready on it.
func TestGhostCaptureSkipsBlockedGhostAndReportsRefusal(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	w.SetGhostDockBlocks([]GhostRef{{ghostOwner, 42}})
	w.SetTargetGhost(ghostOwner, 42)
	if at := runGhost(w, 8); at != 0 || w.GhostCapturing(ghostOwner, 42) {
		t.Fatalf("a blocked ghost was captured (at %.2f s)", at)
	}
	if r := w.GhostGateRefusals(); len(r) != 1 || r[0] != (GhostRef{ghostOwner, 42}) {
		t.Errorf("refusals = %v, want the blocked ghost", r)
	}
	// The serve layer re-evaluates Pos at every tick; the primary has moved.
	w.Ghosts[0].Pos = w.BodyPosition(w.ActiveCraft().Primary).Add(w.Ghosts[0].RelPos)
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("no proximity state")
	}
	if !PortGateTermsHold(st) {
		t.Fatalf("fixture: gate terms should hold (%+v)", st)
	}
	if w.ProximityDockGateReady(st) {
		t.Error("ring ready on a ghost the ledger would refuse")
	}
	w.SetGhostDockBlocks(nil)
	if !w.ProximityDockGateReady(st) {
		t.Error("ring not ready on an unblocked ghost holding the gate")
	}
}

// During the pull the approacher's hands are off (RCS pulses ignored) and
// warp is 1x.
func TestGhostCaptureTakesHandsOffAndHoldsWarpAtOne(t *testing.T) {
	w, a := ghostPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	a.EngineMode = spacecraft.EngineRCS
	runGhost(w, 1)
	if !w.GhostCapturing(ghostOwner, 42) {
		t.Fatal("fixture: pull not running")
	}
	v0 := a.State.V
	if w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Error("RCS pulse accepted during the cross-player pull")
	}
	if a.State.V != v0 {
		t.Error("velocity changed by an ignored pulse")
	}
	w.Clock.WarpIdx = len(WarpFactors) - 1
	if got := w.clampedWarp(); got > 1 {
		t.Errorf("warp during the pull = %v, want 1", got)
	}
}

// Inside 50 m of a friend's vessel warp is capped at the burn cap, like a
// local hull.
func TestWarpCapsWithinFiftyMetresOfGhost(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", 30, 0, 0)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	if got := w.clampedWarp(); got > burnWarpCap {
		t.Errorf("warp 30 m from a ghost = %v, want <= %v", got, burnWarpCap)
	}
	g := w.Ghosts[0]
	g.RelPos = g.RelPos.Add(orbital.Vec3{Z: 5000})
	w.Ghosts[0] = g
	if got := w.clampedWarp(); got <= burnWarpCap {
		t.Errorf("warp 5 km from a ghost = %v, want uncapped", got)
	}
}

// bumpGhostWorld: the active hull and a ghost side by side (both along +Y),
// the ghost gapM along +X, the active closing at closeMS. Returns the ghost's
// reported velocity for the immovability check.
func bumpGhostWorld(t *testing.T, gapM, closeMS float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, a, _ := bumpWorld(t, gapM, -closeMS)
	b := w.Crafts[1]
	// B is the ghost; A (active) closes on it. Flip so A moves, B is pinned.
	b.State.V = a.State.V
	a.State.V = a.State.V.Add(orbital.Vec3{X: closeMS})
	w.Crafts = w.Crafts[:1]
	w.Ghosts = []Ghost{asGhost(b, ghostOwner, 42)}
	w.Ghosts[0].Pos = w.BodyPosition(a.Primary).Add(w.Ghosts[0].RelPos)
	return w, a
}

// walkGhostBump moves the active vessel by its velocity in 50 ms steps and
// runs the ghost bump test each step, until a bump or the step budget.
func walkGhostBump(w *World, a *spacecraft.Spacecraft, steps int) {
	for i := 0; i < steps && w.LastBumpEvent == nil; i++ {
		a.State.R = a.State.R.Add(a.State.V.Scale(0.05))
		for k := range w.Ghosts {
			w.Ghosts[k].RelPos = w.Ghosts[k].RelPos.Add(w.Ghosts[k].Vel.Scale(0.05))
		}
		w.CheckGhostBumps()
	}
}

// ADR 0053 decision 8, cross-player: a 1 m/s approach missing the port cone
// bounces off the ghost's REPORTED hull. Only the bumper is pushed
// (dv = n*(s + s')), the ghost is immovable, and the caption names the ghost.
func TestBumpAgainstGhostPushesOnlyTheBumper(t *testing.T) {
	w, a := bumpGhostWorld(t, 30, 1.0)
	ghostV := w.Ghosts[0].Vel
	walkGhostBump(w, a, 2000)
	e := w.LastBumpEvent
	if e == nil {
		t.Fatal("hull passed through the ghost: no bump")
	}
	if open := w.Ghosts[0].Vel.Sub(a.State.V).X; math.Abs(open-0.5) > 0.02 {
		t.Errorf("opening speed = %.3f m/s, want 0.5 (dv = s + s' on the bumper alone)", open)
	}
	if w.Ghosts[0].Vel != ghostV {
		t.Errorf("ghost velocity changed: %v -> %v", ghostV, w.Ghosts[0].Vel)
	}
	if e.PartnerName != w.Ghosts[0].Name {
		t.Errorf("caption names %q, want the ghost %q", e.PartnerName, w.Ghosts[0].Name)
	}
	if math.Abs(e.SpeedMS-1.0) > 0.05 {
		t.Errorf("caption speed = %.3f, want 1 m/s", e.SpeedMS)
	}
}

// Edge-triggered: no second bump while the hulls still overlap.
func TestBumpAgainstGhostIsEdgeTriggered(t *testing.T) {
	w, a := bumpGhostWorld(t, 30, 1.0)
	walkGhostBump(w, a, 2000)
	if w.LastBumpEvent == nil {
		t.Fatal("no first bump")
	}
	v1 := a.State.V
	w.LastBumpEvent = nil
	// Still overlapping (one more step at the opening speed): no second kick.
	a.State.V = a.State.V.Add(orbital.Vec3{X: 1.0}) // driven back in: closing again, still overlapping
	v2 := a.State.V
	w.CheckGhostBumps()
	if w.LastBumpEvent != nil || a.State.V != v2 {
		t.Errorf("second bump while still overlapping (v %v -> %v, first %v)", v2, a.State.V, v1)
	}
}

// A ghost that the gate would take (aligned nose to nose, slow) docks, it does
// not bump.
func TestNoBumpAgainstGhostInsideTheGate(t *testing.T) {
	// Ports 0.3 m past each other: the noses overlap by 0.3 m, inside the
	// 0.5 m port range, aligned and slow. Touching AND closing, so only the
	// gate keeps it from being a bump.
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", -0.3, 0.05, 0)
	w.Ghosts[0].Pos = w.BodyPosition(w.ActiveCraft().Primary).Add(w.Ghosts[0].RelPos)
	if !ghostPairGateHolds(w.ActiveCraft(), w.Ghosts[0]) {
		t.Fatal("fixture: the gate should hold")
	}
	w.CheckGhostBumps()
	if w.LastBumpEvent != nil {
		t.Errorf("bumped a ghost at the port inside the gate: %+v", w.LastBumpEvent)
	}
}

// The Proximity scene reads a ghost's reported hull like a local one.
func TestProximityStateReadsGhostPort(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", 1.1, 0.05, 6)
	w.SetTargetGhost(ghostOwner, 42)
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("no proximity state for a ghost target")
	}
	if !st.HasPort || st.PortUnknown {
		t.Fatalf("ghost with a reported port read as HasPort=%v unknown=%v", st.HasPort, st.PortUnknown)
	}
	if math.Abs(st.PortRangeM-1.1) > 0.01 {
		t.Errorf("port range = %.3f, want 1.10", st.PortRangeM)
	}
	if math.Abs(st.AlignDeg-6) > 0.05 {
		t.Errorf("align = %.2f, want 6", st.AlignDeg)
	}
	if st.PortName == "" {
		t.Error("port name empty: the chip header would read 'port: '")
	}
	if _, ok := w.TargetPortAxis(); !ok {
		t.Error("PORT frame unavailable against a ghost that reported a port")
	}
}

// A report without a shape reads port: unknown and never offers PORT.
func TestProximityStateGhostWithoutShapeIsUnknown(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", 1.1, 0.05, 0)
	g := w.Ghosts[0]
	g.Attitude, g.NoseM, g.WidthM, g.Ports = orbital.Vec3{}, 0, 0, nil
	w.Ghosts[0] = g
	w.SetTargetGhost(ghostOwner, 42)
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("no proximity state")
	}
	if st.HasPort || !st.PortUnknown || st.PortNote != "unknown" {
		t.Errorf("no-shape ghost: HasPort=%v unknown=%v note=%q", st.HasPort, st.PortUnknown, st.PortNote)
	}
	if _, ok := w.TargetPortAxis(); ok {
		t.Error("PORT frame offered against a ghost with no known shape")
	}
	if w.ProximityDockGateReady(st) {
		t.Error("ring ready on an unknown-shape ghost")
	}
}

// The batch exemption is for a batch's own pieces only: a friend's vessel 30 m
// away still caps warp inside the window (ADR 0059 decision 6, guard 16;
// activeNearGhostHull is untouched).
func TestBatchExemptionDoesNotSilenceAGhost(t *testing.T) {
	w, a := ghostPair(t, "rcs-tug", "rcs-tug", 30, 0, 0)
	a.BatchID, a.BatchLead = 7, true
	a.BatchClampExemptUntil = w.Clock.SimTime.Add(time.Hour)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	if got := w.clampedWarp(); got > burnWarpCap {
		t.Errorf("warp 30 m from a ghost inside a batch window = %v, want <= %v", got, burnWarpCap)
	}
}
