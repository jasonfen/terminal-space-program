package screens

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestMenuHandleKey(t *testing.T) {
	m := NewMenu(Theme{})
	cases := []struct {
		in   string
		want MenuAction
	}{
		{"s", MenuActionSave},
		{"S", MenuActionSave},
		{"l", MenuActionLoad},
		{"L", MenuActionLoad},
		{"q", MenuActionQuit},
		{"Q", MenuActionQuit},
		{"esc", MenuActionCancel},
		{"x", MenuActionNone},
		{"", MenuActionNone},
	}
	for _, c := range cases {
		// Each iteration starts from the list state — reset between
		// cases so a previous keystroke's transition (in particular
		// the y/n confirm path added in v0.7.4) doesn't bleed in.
		m.Reset()
		if got := m.HandleKey(c.in); got != c.want {
			t.Errorf("HandleKey(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestMenuClickConfirmFlow: clicking Save / Load / Quit in the list
// state transitions into a confirm sub-screen rather than firing
// immediately. [Yes] then commits the action; [No] returns to the
// list. The keyboard direct path still bypasses confirm (covered
// above) — confirm gating is mouse-only.
func TestMenuClickConfirmFlow(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
	}
	m := NewMenu(th)
	_ = m.Render(80) // populate button ranges

	// Click Save in the list — should not immediately fire MenuActionSave.
	saveCol := (m.saveBtn.colStart + m.saveBtn.colEnd) / 2
	if got := m.HandleClick(saveCol, m.saveBtn.row); got != MenuActionNone {
		t.Errorf("Save list click: got %v, want MenuActionNone (confirm gate)", got)
	}
	if m.mode != menuModeConfirmSave {
		t.Errorf("mode after Save click = %v, want menuModeConfirmSave", m.mode)
	}

	// Re-render to populate Yes/No ranges, click No → back to list.
	_ = m.Render(80)
	noCol := (m.noBtn.colStart + m.noBtn.colEnd) / 2
	if got := m.HandleClick(noCol, m.noBtn.row); got != MenuActionNone {
		t.Errorf("No click: got %v, want MenuActionNone", got)
	}
	if m.mode != menuModeList {
		t.Errorf("mode after No click = %v, want menuModeList", m.mode)
	}

	// List → confirm again, this time Yes → commits.
	_ = m.Render(80)
	loadCol := (m.loadBtn.colStart + m.loadBtn.colEnd) / 2
	m.HandleClick(loadCol, m.loadBtn.row)
	_ = m.Render(80)
	yesCol := (m.yesBtn.colStart + m.yesBtn.colEnd) / 2
	if got := m.HandleClick(yesCol, m.yesBtn.row); got != MenuActionLoad {
		t.Errorf("Yes click after Load: got %v, want MenuActionLoad", got)
	}
	if m.mode != menuModeList {
		t.Errorf("mode after Yes click = %v, want menuModeList (back to list)", m.mode)
	}
}

// TestMenuKeyboardConfirmStillWorks: keyboard "y" / "n" in confirm
// sub-state commit / cancel the same way [Yes] / [No] clicks do —
// so a player who clicks Save and then types y still saves.
func TestMenuKeyboardConfirmStillWorks(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
	}
	m := NewMenu(th)
	_ = m.Render(80)

	// Click Quit, then press 'y' to confirm.
	quitCol := (m.quitBtn.colStart + m.quitBtn.colEnd) / 2
	m.HandleClick(quitCol, m.quitBtn.row)
	if got := m.HandleKey("y"); got != MenuActionQuit {
		t.Errorf("HandleKey(y) after Quit click: got %v, want MenuActionQuit", got)
	}

	// Click Save, then press 'n' to cancel.
	m.Reset()
	_ = m.Render(80)
	saveCol := (m.saveBtn.colStart + m.saveBtn.colEnd) / 2
	m.HandleClick(saveCol, m.saveBtn.row)
	if got := m.HandleKey("n"); got != MenuActionNone {
		t.Errorf("HandleKey(n) after Save click: got %v, want MenuActionNone", got)
	}
	if m.mode != menuModeList {
		t.Errorf("mode after n: got %v, want menuModeList", m.mode)
	}
}
