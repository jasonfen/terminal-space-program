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
	if name := w.TargetName(); name != "" {
		t.Errorf("stale ghost TargetName = %q, want empty", name)
	}
}
