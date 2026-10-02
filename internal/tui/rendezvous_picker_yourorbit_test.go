package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Wave B review fix (REVIEW LOW 164): on "your orbit" the partner burns, and
// lowering rows are mostly unsafe by the periapsis gate. The Enter refusal on
// such a row must tell the pilot what to do from HERE: the generic "circularize
// [C] first" is about their own vessel, which is not the one burning.
func TestRendezvousPicker_YourOrbitUnsafeRow_RefusalPointsAtTheirOrbit(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 900e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)
	pressRune(a, 'K')
	if !a.orbitView.RendezvousPickerOpen() {
		t.Fatal("picker did not open")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRight}) // your orbit
	ladder := a.orbitView.RendezvousPickerLadder()
	for i := range ladder.Rows {
		if ladder.Rows[i].Reason == "burn drops periapsis unsafely" {
			for j := 0; j < i; j++ {
				a.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
			a.Update(tea.KeyMsg{Type: tea.KeyEnter})
			msg := a.statusMsg
			if !strings.Contains(msg, "their orbit") {
				t.Errorf("refusal %q should point the pilot at [←] their orbit", msg)
			}
			if strings.Contains(msg, "[C]") {
				t.Errorf("refusal %q names [C], which is about the wrong vessel here", msg)
			}
			return
		}
	}
	t.Fatalf("fixture has no unsafe your-orbit row: %+v", ladder.Rows)
}

// Wave B review fix (REVIEW LOW 147): the picker-open flash was 118 cells, 20
// to spare at the 140 floor. Pin a ceiling through the real K path.
func TestRendezvousPicker_OpenFlashFitsTheDesignFloorWithRoom(t *testing.T) {
	a := rendezvousPickerPhaseMismatchApp(t)
	pressRune(a, 'K')
	if !a.orbitView.RendezvousPickerOpen() {
		t.Fatal("picker did not open")
	}
	if w := lipgloss.Width(a.statusMsg); w > 108 {
		t.Errorf("open flash is %d cells, want <= 108 (%q)", w, a.statusMsg)
	}
	if !strings.Contains(a.statusMsg, "[G]") || !strings.Contains(a.statusMsg, "Enter") {
		t.Errorf("shortened flash lost the keys: %q", a.statusMsg)
	}
}
