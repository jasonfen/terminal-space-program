package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// enterSaveAsNaming opens the Saves browser in save-mode and steps into
// the Save-As name-entry field (Enter on the New-save row).
func enterSaveAsNaming(t *testing.T, a *App) {
	t.Helper()
	a.openSaves(screens.SavesModeSave)
	a.Update(tea.KeyMsg{Type: tea.KeyEnter}) // New-save row → name entry
	if !a.saves.CapturingText() {
		t.Fatal("did not enter the Save-As name-entry state")
	}
}

// TestSavesNameInputSwallowsBacktick — finding 9. A backtick typed into
// the Save-As name field is a literal character, NOT the global boss-key
// hotkey: the screen stays put and the character reaches the input.
func TestSavesNameInputSwallowsBacktick(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	enterSaveAsNaming(t, a)

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("`")})
	if a.active == screenBoss {
		t.Fatal("backtick opened the boss shell from the name field (finding 9)")
	}
	if a.active != screenSaves {
		t.Fatalf("active = %v, want screenSaves (still naming)", a.active)
	}
	if out := a.saves.Render(200, 40); !strings.Contains(out, "`") {
		t.Errorf("name input did not receive the literal backtick\n%s", out)
	}
}

// TestSavesNameInputNotLayoutNormalized — finding 3. While the name field
// captures text, keyboard-layout normalization is bypassed so a QWERTZ
// player's keycaps aren't transposed: a physical 'z' types 'z', not the
// QWERTY-normalized 'y'.
func TestSavesNameInputNotLayoutNormalized(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.layout = keylayout.QWERTZ

	// A named save on disk to rename, with a controlled prefill.
	if _, err := save.WriteNamed(a.world, "abc"); err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	a.openSaves(screens.SavesModeLoad) // cursor 0 = the "abc" save

	// 'r' opens rename (browse state still normalizes, but 'r' is not a
	// swapped key, so it maps to itself and opens the field).
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if !a.saves.CapturingText() {
		t.Fatalf("'r' did not open rename; active=%v", a.active)
	}

	// Physical 'z' under QWERTZ would normalize to 'y' — the bypass keeps
	// it raw.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	out := a.saves.Render(200, 40)
	if !strings.Contains(out, "abcz") {
		t.Errorf("name field did not append the raw 'z' (normalization not bypassed)\n%s", out)
	}
	if strings.Contains(out, "abcy") {
		t.Errorf("name field shows the QWERTY-normalized 'y' — normalization leaked into text capture\n%s", out)
	}
}
