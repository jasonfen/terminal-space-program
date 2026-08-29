package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// A ghost target resolves position/velocity/name from the transient
// slate; a stale ref resolves to nothing (never a wrong answer).
func TestTargetGhostResolution(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	primary := w.System().Bodies[0]
	pos := w.BodyPosition(primary).Add(orbital.Vec3{X: 7e6})
	w.Ghosts = []Ghost{{
		Owner: "SHA256:gern", CraftID: 42, Handle: "gern",
		Name: "Aloft", PrimaryID: primary.ID,
		Pos: pos, Vel: orbital.Vec3{Y: 7500},
	}}

	w.SetTargetGhost("SHA256:gern", 42)
	if w.Target.Kind != TargetGhost {
		t.Fatalf("Kind = %v, want TargetGhost", w.Target.Kind)
	}
	st, ok := w.TargetState()
	if !ok {
		t.Fatal("TargetState did not resolve a live ghost")
	}
	if d := st.R.Sub(pos).Norm(); d > 1e-6 {
		t.Errorf("ghost target position off by %g m", d)
	}
	if name := w.TargetName(); name != "gern's Aloft" {
		t.Errorf("TargetName = %q", name)
	}
	if _, _, ok := w.TargetStateRelativeToActivePrimary(); !ok {
		t.Error("relative state did not resolve — rendezvous tooling dead vs ghosts")
	}

	// Slate cleared (owner left, other system): resolves to nothing.
	w.Ghosts = nil
	if _, ok := w.TargetState(); ok {
		t.Error("stale ghost target still resolves")
	}
	// #294 review finding 4: the lock itself is NOT the same as
	// TargetNone — Kind stays TargetGhost so a later resolve can
	// re-latch it (see reportingModel.reconcileTargetLock), so
	// TargetName reports a pending state rather than "" (which used to
	// read exactly like an untargeted craft — the TARGET chip vanished
	// outright instead of showing "still locked, just waiting").
	if name := w.TargetName(); name != "pending target lock" {
		t.Errorf("stale ghost TargetName = %q, want the pending-lock label", name)
	}
	if w.Target.Kind != TargetGhost {
		t.Errorf("stale ghost target lost its Kind: %+v, want it to stay TargetGhost for later re-latch", w.Target)
	}
	// #294 review finding 4 (round 2): HasRelativeTarget went back to
	// Kind-only — a ghost target counts as relative whether or not it
	// currently resolves (round 1's resolve requirement demoted a docker
	// off NavTarget mid-proximity-ops the instant the ghost slate briefly
	// emptied). The zero-direction hazard round 1 guarded against is
	// covered independently by attitudeContext's own fallback — see
	// TestAttitudeContextFallsBackWhenGhostUnresolved.
	if !w.HasRelativeTarget() {
		t.Error("HasRelativeTarget false for an unresolved ghost — NavTarget/rendezvous surfaces would wrongly hide")
	}
}

// #294 review finding 4: GhostOwnerHandle backs TargetName's pending
// label with the roster handle when a live session has one, so the
// player sees "who", not just "something", while a lock is pending.
func TestGhostOwnerHandleFromSession(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if h := w.GhostOwnerHandle("SHA256:gern"); h != "" {
		t.Errorf("GhostOwnerHandle with no session = %q, want empty", h)
	}
	w.Session = &SessionInfo{Players: []SessionPlayer{
		{Fingerprint: "SHA256:gern", Handle: "gern"},
	}}
	if h := w.GhostOwnerHandle("SHA256:gern"); h != "gern" {
		t.Errorf("GhostOwnerHandle = %q, want %q", h, "gern")
	}
	if h := w.GhostOwnerHandle("SHA256:someone-else"); h != "" {
		t.Errorf("GhostOwnerHandle for an unknown owner = %q, want empty", h)
	}

	w.SetTargetGhost("SHA256:gern", 42)
	w.Ghosts = nil // unresolved — pending
	if name := w.TargetName(); name != "gern's vessel (pending)" {
		t.Errorf("TargetName = %q, want the roster handle in the pending label", name)
	}
}
