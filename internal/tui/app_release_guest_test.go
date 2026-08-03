package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// pressUndock sends [U] and returns whatever command the App produced.
func pressUndock(t *testing.T, a *App) (*App, tea.Cmd) {
	t.Helper()
	m, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'U'}})
	got, ok := m.(*App)
	if !ok {
		t.Fatalf("Update returned %T", m)
	}
	return got, cmd
}

// crossPlayerStack builds an App whose active craft is a stack carrying
// another player's component — the owner seat of a cross-player dock.
func crossPlayerStack(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.World()
	guest := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	guest.Primary = w.ActiveCraft().Primary
	guest.State = w.ActiveCraft().State
	guest.ID = 601
	w.AdoptCraft(guest, false)
	if _, idx, ok := w.DockGuestCraft(0, guest, "SHA256:gern"); !ok {
		t.Fatalf("DockGuestCraft refused")
	} else {
		w.SetActiveCraftIdx(idx)
	}
	return a
}

// TestUndockFromTheOwnerSeatReleasesThroughTheLedger (#312, ADR 0040 §3):
// [U] on a stack carrying someone else's craft used to refuse outright,
// which is why a guest disconnecting while docked stranded the docker with
// no way out from either seat. It must now emit the release intent — and it
// must NOT split locally, which would clone the guest's craft into this
// World while they still believe they are docked.
func TestUndockFromTheOwnerSeatReleasesThroughTheLedger(t *testing.T) {
	a := crossPlayerStack(t)
	before := len(a.World().Crafts)

	a, cmd := pressUndock(t, a)
	if cmd == nil {
		t.Fatalf("[U] on an owner-held cross-player stack produced no intent")
	}
	if _, ok := cmd().(ReleaseGuestMsg); !ok {
		t.Fatalf("[U] emitted %T, want ReleaseGuestMsg", cmd())
	}
	if len(a.World().Crafts) != before {
		t.Errorf("the stack was split locally — the guest's craft is now cloned into this World")
	}
	if !sim.StackHasGuest(a.World().ActiveCraft()) {
		t.Errorf("the composite dissolved on the owner's own tick")
	}
}

// TestUndockFromTheOwnerSeatRefusesTheBottomPeel (ADR 0040 §5, #314): after a
// control transfer the other party's components sit under the owner's, where
// a peel would hand each player the other's hardware. The seat refuses and
// names the recovery rather than emitting an intent that can only fail.
func TestUndockFromTheOwnerSeatRefusesTheBottomPeel(t *testing.T) {
	a := crossPlayerStack(t)
	w := a.World()
	// The transfer retag without a restack: the guest's tag moves to the
	// BOTTOM component, exactly as RetagStackForTransfer leaves it.
	stack := w.ActiveCraft()
	stack.DockedComponents[0].Owner = "SHA256:gern"
	stack.DockedComponents[len(stack.DockedComponents)-1].Owner = ""

	a, cmd := pressUndock(t, a)
	if cmd != nil {
		if _, ok := cmd().(ReleaseGuestMsg); ok {
			t.Fatalf("[U] armed a release that would swap the two players' vehicles")
		}
	}
	if !strings.Contains(a.statusMsg, "[J]") {
		t.Errorf("refusal %q does not name the way out", a.statusMsg)
	}
}
