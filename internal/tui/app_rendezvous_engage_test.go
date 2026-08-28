package tui

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// TestEngageRendezvousNoEncounterCoachesPhasingBurn — PR #392 review
// finding 1 supersedes ADR 0039 S2 / #277 for this refusal: a genuinely
// stalemated geometry (matched circular orbit, opposite phase — zero
// relative drift, an encounter can never form on the current courses,
// #276) with NO rendezvous nudge planted must refuse Engage with #276's
// own requested remedy, "plant a rendezvous nudge [K] first" — no
// longer a dead end now that RendezvousCommit actually honors a planted
// node (finding 1), so pointing at K is a real next step again instead
// of the generic phasing-coach line ADR 0039 S2 substituted when it
// wasn't.
func TestEngageRendezvousNoEncounterCoachesPhasingBurn(t *testing.T) {
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
	if !strings.Contains(app.statusMsg, "plant a rendezvous nudge") {
		t.Errorf("refusal %q missing the #276 remedy (plant a rendezvous nudge [K] first)", app.statusMsg)
	}
}
