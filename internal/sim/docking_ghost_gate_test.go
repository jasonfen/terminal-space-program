package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// fakeGate is a scripted GhostPullGate that counts calls.
type fakeGate struct {
	approacher, begin, pullingToward bool
	begins, ends                     int
}

func (g *fakeGate) Approacher(GhostRef) bool    { return g.approacher }
func (g *fakeGate) BeginPull(GhostRef) bool     { g.begins++; return g.begin }
func (g *fakeGate) EndPull(GhostRef)            { g.ends++ }
func (g *fakeGate) PullingToward(GhostRef) bool { return g.pullingToward }

// Review 2026-10-14 finding 3: the seat the rule does not name never starts a
// pull, never moves, never turns and never claims; the named seat does (the
// control that proves the instrument can return a positive).
func TestGhostCaptureOnlyForTheApproacher(t *testing.T) {
	for _, tc := range []struct {
		name      string
		gate      *fakeGate
		wantMoves bool
	}{
		{"approacher", &fakeGate{approacher: true, begin: true}, true},
		{"not the approacher", &fakeGate{approacher: false, begin: true}, false},
		{"pair latched by the other seat", &fakeGate{approacher: true, begin: false}, false},
	} {
		w, a := ghostPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
		w.SetGhostPullGate(tc.gate)
		r0, att0 := a.State.R, a.CurrentAttitudeDir
		at := runGhost(w, 6)
		pulled := w.GhostCapturing(ghostOwner, 42) || at != 0
		moved := a.State.R != r0 || a.CurrentAttitudeDir != att0
		if pulled != tc.wantMoves || (tc.wantMoves && at == 0) {
			t.Errorf("%s: pulled=%v completedAt=%v, want pull=%v", tc.name, pulled, at, tc.wantMoves)
		}
		if !tc.wantMoves && moved {
			t.Errorf("%s: the passive vessel was moved or turned", tc.name)
		}
		if tc.wantMoves && tc.gate.ends != 1 {
			t.Errorf("%s: EndPull called %d times, want 1 on completion", tc.name, tc.gate.ends)
		}
		if !tc.wantMoves && tc.gate.ends != 0 {
			t.Errorf("%s: EndPull called without a pull", tc.name)
		}
		if refs := w.GhostGateRefusals(); !tc.wantMoves && len(refs) != 0 {
			t.Errorf("%s: a seat that does not approach filed a refusal", tc.name)
		}
	}
}

// A pull let go mid-way (the ghost leaves the slate) releases the registration.
func TestGhostCaptureLetGoReleasesTheGate(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	g := &fakeGate{approacher: true, begin: true}
	w.SetGhostPullGate(g)
	runGhost(w, 2)
	if !w.GhostCapturing(ghostOwner, 42) {
		t.Fatal("fixture: no pull running")
	}
	w.Ghosts = nil
	stepGhost(w)
	if w.GhostCapturing(ghostOwner, 42) || g.ends != 1 {
		t.Errorf("after the ghost vanished: capturing=%v ends=%d, want false 1", w.GhostCapturing(ghostOwner, 42), g.ends)
	}
}

// The passive seat's World must not kick its own vessel off the approacher's
// ghost while the approacher's magnet is closing the last half metre: the dock
// takes the touch. The control (no pull registered toward us) bumps.
func TestNoBumpWhileTheFriendIsPullingTowardUs(t *testing.T) {
	for _, pulling := range []bool{false, true} {
		w, a := bumpGhostWorld(t, 30, 1.0)
		w.SetGhostPullGate(&fakeGate{approacher: true, pullingToward: pulling})
		walkGhostBump(w, a, 2000)
		if got := w.LastBumpEvent != nil; got == pulling {
			t.Errorf("pullingToward=%v: bumped=%v", pulling, got)
		}
	}
}

// Review 2026-10-14 finding 1, cross-player: a nose-first approach on a
// friend's hull that is a little crooked (their nose tilted about its BASE, so
// the noses are laterally offset by more than 5 cm) or misses by 1-2 m used to
// pass straight through: the axis-to-axis closest-point normal is lateral
// there, so the kick was ~0. It must bounce with the true closing speed.
func TestOffLineNoseFirstHitOnAGhostBounces(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tiltDeg float64 // about the ghost's base
		latM    float64 // ghost shifted sideways
	}{
		{"coaxial", 0, 0},
		{"tilt 3 deg about the base", 3, 0},
		// Tugs are 1.0 m wide (ADR 0061): keep the miss under the radii sum.
		{"0.4 m lateral miss", 0, 0.4},
		{"2 deg tilt and 0.6 m miss", 2, 0.6},
	} {
		w, a := ghostPair(t, "rcs-tug", "rcs-tug", 20, 0, 0)
		g := &w.Ghosts[0]
		g.Vel = a.State.V                               // ghost at rest relative to us; WE close at 0.3 m/s
		a.State.V = a.State.V.Add(orbital.Vec3{Y: 0.3}) // toward the ghost (it lies along +Y)
		g.Attitude = orbital.Rotate(g.Attitude, orbital.Vec3{Z: 1}, tc.tiltDeg*math.Pi/180).Unit()
		g.RelPos = g.RelPos.Add(orbital.Vec3{X: tc.latM})
		w.Ghosts[0].Pos = w.BodyPosition(a.Primary).Add(g.RelPos)
		walkGhostBump(w, a, 1500)
		e := w.LastBumpEvent
		if e == nil {
			t.Errorf("%s: the hull passed through the ghost (no bump)", tc.name)
			continue
		}
		if math.Abs(e.SpeedMS-0.3) > 0.03 {
			t.Errorf("%s: bump speed %.3f m/s, want the true closing speed 0.3", tc.name, e.SpeedMS)
		}
		// Opening along the approach axis afterwards, and no deep overlap.
		if open := g.Vel.Sub(a.State.V).Dot(orbital.Vec3{Y: 1}); open < 0.1 {
			t.Errorf("%s: opening speed %.3f m/s along the axis, want a real bounce", tc.name, open)
		}
	}
}

// ADR 0053 decision 8's 50 m zone is hull to hull: a friend's long stack whose
// ANCHOR is over 50 m away but whose hull is inside it caps the warp at 10x.
func TestWarpCapsNearAFriendsLongStackByHullNotAnchor(t *testing.T) {
	w, a := ghostPair(t, "rcs-tug", "rcs-tug", 20, 0, 0)
	g := &w.Ghosts[0]
	// Make the friend's reported hull 60 m long, anchor 70 m behind our nose
	// so the anchor is beyond 50 m while the nose is 10 m from our nose.
	g.NoseM = 60
	up := a.CurrentAttitudeDir.Unit()
	aNose := a.State.R.Add(up.Scale(6))
	g.Attitude = up.Scale(-1)              // its hull runs back toward us
	g.RelPos = aNose.Add(up.Scale(60 + 5)) // base 65 m ahead, nose 5 m from ours
	g.Pos = w.BodyPosition(a.Primary).Add(g.RelPos)
	if d := g.RelPos.Sub(a.State.R).Norm(); d <= DockingDistM {
		t.Fatalf("fixture: anchor %.1f m is not beyond 50 m", d)
	}
	if !w.activeNearGhostHull() {
		t.Error("a friend's hull 5 m from ours does not count as inside the 50 m zone (anchor distance used)")
	}
}

// "A bump off a friend's hull pushes only you": now that the bump works, the
// seat the approacher rule does NOT name must not kick itself off the
// bumper's ghost too (both seats would bounce, doubling the separation).
func TestOnlyTheApproacherBumpsOffAGhost(t *testing.T) {
	for _, approacher := range []bool{true, false} {
		w, a := bumpGhostWorld(t, 30, 1.0)
		w.SetGhostPullGate(&fakeGate{approacher: approacher})
		walkGhostBump(w, a, 2000)
		if got := w.LastBumpEvent != nil; got != approacher {
			t.Errorf("approacher=%v: bumped=%v", approacher, got)
		}
	}
}

// A ghost target reads the same hull-gap zone as a local one: a long stack
// whose anchor is 65 m away but whose nose is ~5 m from ours has HullGapM
// inside the ring, while RangeM stays anchor to anchor.
func TestProximityHullGapForAGhostTarget(t *testing.T) {
	w, a := ghostPair(t, "rcs-tug", "rcs-tug", 20, 0, 0)
	g := &w.Ghosts[0]
	up := a.CurrentAttitudeDir.Unit()
	g.NoseM = 60
	g.Attitude = up.Scale(-1)
	g.RelPos = a.State.R.Add(up.Scale(6 + 60 + 5))
	g.Pos = w.BodyPosition(a.Primary).Add(g.RelPos)
	w.SetTargetGhost(ghostOwner, 42)
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("no proximity state for the ghost")
	}
	if st.RangeM < 60 {
		t.Fatalf("fixture: anchor range %.1f m should be beyond 50", st.RangeM)
	}
	if st.HullGapM > 10 || st.HullGapM >= DockingDistM {
		t.Errorf("hull gap = %.1f m, want ~5 (inside the 50 m ring)", st.HullGapM)
	}
}

// A friend whose report lists no ports (a composite whose nose port is mated)
// reads "none", never offers PORT and is never captured, even at the gate.
func TestGhostWithNoReportedPortsIsNeverCaptured(t *testing.T) {
	w, _ := ghostPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	w.Ghosts[0].Ports = nil
	w.SetTargetGhost(ghostOwner, 42)
	st, _ := w.ProximityState()
	if st.HasPort || st.PortUnknown || len(st.PortNote) < 4 || st.PortNote[:4] != "none" {
		t.Errorf("HasPort=%v unknown=%v note=%q, want a plain none", st.HasPort, st.PortUnknown, st.PortNote)
	}
	if at := runGhost(w, 7); at != 0 || w.GhostCapturing(ghostOwner, 42) {
		t.Error("a ghost with no reported ports was captured")
	}
}
