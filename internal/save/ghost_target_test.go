package save

import (
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Review follow-up: a ghost target (session-local by design) is
// normalised to no-target on save — never a stuck Kind with a dropped
// owner, and the persisted Kind vocabulary stays pre-v0.27.
func TestGhostTargetNormalizedOnSave(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:x", CraftID: 7, PrimaryID: c.Primary.ID}}
	w.SetTargetGhost("SHA256:x", 7)

	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Target.Kind != sim.TargetNone {
		t.Errorf("loaded Target.Kind = %v, want TargetNone (ghost refs must not persist)", got.Target.Kind)
	}
	if got.ActiveCraft().Target.Kind != sim.TargetNone {
		t.Errorf("loaded craft Target.Kind = %v, want TargetNone", got.ActiveCraft().Target.Kind)
	}
}
