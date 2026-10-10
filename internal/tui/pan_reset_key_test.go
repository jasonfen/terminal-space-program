package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// g drops the Pan and keeps Focus and zoom (Jason 2026-10-10: "there
// should be a reset key for the pan camera function ... make it g, move the
// reset to system view alt+g"). Before, g was the jump to the whole
// system, which cleared the pan only as a side effect of refocusing.
func TestGSnapsThePanBackAndKeepsFocus(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.active = screenOrbit
	a.world.Focus = sim.Focus{Kind: sim.FocusCraft}
	before := a.View()
	focus := a.world.Focus

	a.Update(tea.KeyMsg{Type: tea.KeyShiftLeft})
	a.Update(tea.KeyMsg{Type: tea.KeyShiftUp})
	if panned := a.View(); panned == before {
		t.Fatal("setup: shift+arrows did not move the view")
	}

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if a.world.Focus != focus {
		t.Errorf("g changed Focus to %+v; it should only drop the pan", a.world.Focus)
	}
	if after := a.View(); after != before {
		t.Error("after g the view is not back where it was before panning")
	}
}

// alt+g is the old g: the camera on the whole system.
func TestAltGFocusesTheWholeSystem(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.active = screenOrbit
	a.world.Focus = sim.Focus{Kind: sim.FocusCraft}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}, Alt: true})
	if a.world.Focus.Kind != sim.FocusSystem {
		t.Errorf("alt+g left Focus at %+v, want the whole system", a.world.Focus)
	}
}
