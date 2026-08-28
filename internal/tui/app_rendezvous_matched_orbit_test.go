package tui

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// TestEngageRendezvousSmallLagRefusesPhantomArm (#276, wording updated
// for PR #392 review finding 1): a small co-orbital phase lag (same
// circular orbit, differing only by -0.5°) is the geometry
// TestEngageRendezvousCloseCANoGapNote already documents as one where
// the K-nudge advisory finds a real, plantable burn (~43 km post-burn
// CA). But the player here has not pressed K — no burn has been made
// or planted, and matched orbits never close on their own. Engage must
// refuse (pointing at K, #276's own remedy — now a real next step since
// RendezvousCommit honors an actually-planted node), not silently arm
// toward the advisory's unfired preview.
func TestEngageRendezvousSmallLagRefusesPhantomArm(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.world.ActiveCraft() == nil {
		t.Fatal("no active craft in a fresh world")
	}
	lagGhostWorld(a, -0.5)

	model, _ := a.applySessionCommand(screens.SessionCommand{
		Kind: screens.SessionCmdRendezvous, Owner: "SHA256:gern", CraftID: 7, Handle: "gern",
	})
	app := model.(*App)

	if app.statusMsg == "" {
		t.Fatal("Engage produced no status message")
	}
	if !strings.Contains(app.statusMsg, "plant a rendezvous nudge") {
		t.Errorf("Engage armed on a phantom encounter instead of refusing: %q", app.statusMsg)
	}
	if app.world.RendezvousArm != nil {
		t.Error("RendezvousArm set despite no encounter on the current courses (#276)")
	}
}
