package tui

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// TestEngageRendezvousNoEncounterFormsUnplannedAgreement (ADR 0045 S7,
// #400 — supersedes the PR #392 review finding 1 contract this test used
// to pin, where Engage refused outright on a stalemated geometry) — a
// genuinely stalemated geometry (matched circular orbit, opposite phase —
// zero relative drift, an encounter can never form on the current
// courses, #276) with NO rendezvous nudge planted must now SUCCEED:
// Engage stops meaning "we found an encounter" and starts meaning "we
// are going to meet" (ADR 0045 §5). The agreement forms with no
// committed τ — RendezvousArm is set, RendezvousUnplanned() is true, and
// the status message names the new state rather than refusing.
func TestEngageRendezvousNoEncounterFormsUnplannedAgreement(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	active := a.world.ActiveCraft()
	if active == nil {
		t.Fatal("no active craft in a fresh world")
	}
	// Opposite phase on the identical circular orbit: relative motion is
	// exactly zero for all time — the matched-orbit stalemate (#276).
	a.world.Ghosts = []sim.Ghost{{
		Owner:     "SHA256:gern",
		CraftID:   7,
		Handle:    "gern",
		Name:      "Aloft",
		PrimaryID: active.Primary.ID,
		Pos:       a.world.BodyPosition(active.Primary).Add(active.State.R.Scale(-1)),
		RelPos:    active.State.R.Scale(-1),
		Vel:       active.State.V.Scale(-1),
	}}

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
		t.Fatal("Engage did not form an agreement (ADR 0045 S7, #400: it should have — no encounter is no longer a refusal)")
	}
	if !app.world.RendezvousArm.Tau.IsZero() {
		t.Errorf("Tau = %v, want zero (no encounter was found to commit to)", app.world.RendezvousArm.Tau)
	}
	if !app.world.RendezvousUnplanned() {
		t.Error("RendezvousUnplanned() = false, want true")
	}
}
