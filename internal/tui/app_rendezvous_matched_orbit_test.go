package tui

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// TestEngageRendezvousSmallLagFormsUnplannedAgreement (ADR 0045 S7, #400
// — supersedes the PR #392 review finding 1 contract this test used to
// pin, where Engage refused a "phantom" arm outright): a small
// co-orbital phase lag (same circular orbit, differing only by -0.5°) is
// the geometry TestEngageRendezvousCloseCANoGapNote already documents as
// one where the K-nudge advisory finds a real, plantable burn (~43 km
// post-burn CA) — but the player here has not pressed K, so
// RendezvousCommitWithPlan's current-course search (Source 3) is what
// runs, and a slow phase lag on matched circular orbits doesn't close
// inside the 4h search window. Engage now SUCCEEDS anyway (ADR 0045 §5:
// the 4h window bounds a search, not the agreement) — forming the
// agreed-no-plan state rather than the old outright refusal. The
// commit's own gates are unchanged: this still isn't a real committed
// encounter, so Tau stays zero and there is no Meeting Place to name.
func TestEngageRendezvousSmallLagFormsUnplannedAgreement(t *testing.T) {
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
	if !strings.Contains(app.statusMsg, "no plan yet") {
		t.Errorf("status %q missing the agreed-no-plan wording", app.statusMsg)
	}
	if app.world.RendezvousArm == nil {
		t.Fatal("Engage did not form an agreement (ADR 0045 S7, #400)")
	}
	if !app.world.RendezvousArm.Tau.IsZero() {
		t.Errorf("Tau = %v, want zero — no node was planted and the current-course search doesn't close inside 4h", app.world.RendezvousArm.Tau)
	}
	if app.world.RendezvousArm.MeetingPlaceLabel != "" {
		t.Errorf("MeetingPlaceLabel = %q, want empty — nothing was planted to name a Place from", app.world.RendezvousArm.MeetingPlaceLabel)
	}
}
