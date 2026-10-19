package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Audit 2026-10-18 #3: bubbles textinput.deleteWordForward indexes past the
// value when the cursor sits on the last rune, so alt+d / alt+delete took the
// whole game down. Every textinput in the TUI (planner dv, planner throttle,
// saves name box) must survive both chords.
func TestTextInputsSurviveDeleteWordForward(t *testing.T) {
	altD := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}, Alt: true}
	altDel := tea.KeyMsg{Type: tea.KeyDelete, Alt: true}
	left := tea.KeyMsg{Type: tea.KeyLeft}

	for name, chord := range map[string]tea.KeyMsg{"alt+d": altD, "alt+delete": altDel} {
		for _, row := range []int{2, 3} {
			m := NewManeuver(savesTheme())
			m.ResetEditing()
			focusRow(t, m, row)
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Errorf("planner row %d %s panicked: %v", row, name, p)
					}
				}()
				m.HandleKey(left, nil)
				m.HandleKey(chord, nil)
			}()
		}

		sc := NewSavesScreen(savesTheme())
		sc.Open(SavesModeSave, savesFixture(), "Default name")
		sc.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}) // New-save row opens the name box
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("saves name box %s panicked: %v", name, p)
				}
			}()
			sc.HandleKey(left)
			sc.HandleKey(chord)
		}()
	}
}
