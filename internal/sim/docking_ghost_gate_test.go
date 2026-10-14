package sim

import "testing"

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
		w.SetGhostPullGate(&fakeGate{pullingToward: pulling})
		walkGhostBump(w, a, 2000)
		if got := w.LastBumpEvent != nil; got == pulling {
			t.Errorf("pullingToward=%v: bumped=%v", pulling, got)
		}
	}
}
