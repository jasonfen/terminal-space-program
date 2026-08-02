package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// #285: the flight view labels a K refusal "rendezvous:" at the display
// layer, the way `circularize:` and `save:` are labelled — so the sim's
// refusal reasons must not carry the label themselves. Before the fix
// every refusal rendered as "rendezvous: rendezvous: no craft target".
//
// Asserted on the composed status line rather than the sentinel strings:
// the double label is a property of what the player reads, and a future
// refusal-reason pass (#278) is free to reword the reasons as long as
// the label still appears once.
func TestRendezvousRefusalLabelsOnce(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// No craft target — the simplest refusal to provoke from a fresh world.
	a.world.Target = sim.Target{}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})

	if a.statusMsg == "" {
		t.Fatal("K with no target produced no status message")
	}
	if n := strings.Count(a.statusMsg, "rendezvous:"); n != 1 {
		t.Errorf("status %q carries the rendezvous label %d times, want exactly 1", a.statusMsg, n)
	}
}
