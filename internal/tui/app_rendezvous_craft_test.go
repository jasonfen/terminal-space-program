package tui

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// #295: arming acts on whatever craft is active, and nothing in the arm
// flow named it. A pilot with four craft who had cycled their active
// slot earlier armed a rendezvous on a dry science probe instead of the
// intended tug; the mistake was only visible from the PARTNER's seat, as
// a wildly wrong closest approach on the join prompt. Name the vessel on
// the arming side, where the mistake is made.
func TestRendezvousArmNamesTheActingCraft(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world
	w.Ghosts = []sim.Ghost{ghostBesideActive(w, "SHA256:guest", 42)}
	w.ActiveCraft().Name = "Relay Tug-1"

	a.applySessionCommand(screens.SessionCommand{
		Kind: screens.SessionCmdRendezvous, Owner: "SHA256:guest", CraftID: 42, Handle: "gern",
	})
	if w.RendezvousArm == nil {
		t.Fatal("rendezvous command did not arm")
	}
	if w.RendezvousArm.CraftName != "Relay Tug-1" {
		t.Errorf("arm CraftName = %q, want the acting craft's name", w.RendezvousArm.CraftName)
	}
	if !strings.Contains(a.statusMsg, "Relay Tug-1") {
		t.Errorf("armed status %q does not name the acting craft", a.statusMsg)
	}
	if !strings.Contains(a.statusMsg, "gern") {
		t.Errorf("armed status %q stopped naming the partner", a.statusMsg)
	}
}
