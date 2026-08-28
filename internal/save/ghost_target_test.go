package save

import (
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// #294 fix: a ghost target now round-trips through save/load intact
// (Kind, CraftID, and the owner fingerprint). It used to normalise to
// no-target on save on the reasoning that the fingerprint was
// session-local and would never resolve again — which was harmless in
// single-player (nothing there ever sets a ghost target) but silently
// dropped a guest's cross-player rendezvous lock on every reconnect
// that round-trips through the per-player session payload, most
// visibly the [u] restart-to-adopt flow. The fingerprint IS meaningful
// within the session it was set in, so it now persists; the deferred
// re-latch (reportingModel in internal/serve) is what actually
// resolves it back to a live ghost once the owner's reports resume.
func TestGhostTargetPersistsOnSave(t *testing.T) {
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
	if got.Target.Kind != sim.TargetGhost || got.Target.GhostOwner != "SHA256:x" || got.Target.CraftID != 7 {
		t.Errorf("loaded Target = %+v, want ghost target at (SHA256:x, 7)", got.Target)
	}
	if ct := got.ActiveCraft().Target; ct.Kind != sim.TargetGhost || ct.GhostOwner != "SHA256:x" || ct.CraftID != 7 {
		t.Errorf("loaded craft Target = %+v, want ghost target at (SHA256:x, 7)", ct)
	}
}
