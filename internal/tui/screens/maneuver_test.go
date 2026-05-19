package screens

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// TestManeuverClearAllBindings — the simplified `c` / `C` binding
// (and the ctrl+k back-compat alias) all emit NodeClearAllMsg and
// close the form.
func TestManeuverClearAllBindings(t *testing.T) {
	for _, key := range []string{"c", "C", "ctrl+k"} {
		m := NewManeuver(Theme{})
		cmd, done := m.HandleKey(keyMsg(key))
		if !done {
			t.Errorf("%q: done = false, want true (form should close)", key)
		}
		if cmd == nil {
			t.Fatalf("%q: nil cmd, want a NodeClearAllMsg command", key)
		}
		if _, ok := cmd().(NodeClearAllMsg); !ok {
			t.Errorf("%q: emitted %T, want NodeClearAllMsg", key, cmd())
		}
	}
}

// TestManeuverRendersPlannedNodes — the form panel lists every
// planted node for the active craft, and shows the empty-state
// hint when there are none.
func TestManeuverRendersPlannedNodes(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	m := NewManeuver(Theme{})

	if out := m.Render(w, 120, 40); !strings.Contains(out, "PLANNED NODES (none)") {
		t.Error("empty-state PLANNED NODES hint missing when no nodes planted")
	}

	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{DV: 120, TriggerTime: w.Clock.SimTime.Add(time.Hour)},
		spacecraft.ManeuverNode{DV: 45, TriggerTime: w.Clock.SimTime.Add(2 * time.Hour)},
	)
	out := m.Render(w, 120, 40)
	if !strings.Contains(out, "PLANNED NODES (2)") {
		t.Error("node-count header missing / wrong with 2 nodes planted")
	}
	if !strings.Contains(out, "120 m/s") || !strings.Contains(out, "45 m/s") {
		t.Errorf("planned-node Δv values not listed:\n%s", out)
	}
}
