package screens

import (
	"strings"
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

// TestMenuButtonRowsMatchRenderedLines: the row stored in each
// buttonRange must equal the index of the line in the rendered
// output that visually contains that button. Pre-fix, the helpers
// recorded local row indices that didn't account for the title row
// prepended by Render — clicks fired one row above the visual
// button.
func TestMenuButtonRowsMatchRenderedLines(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
	}
	m := NewMenu(th)

	// List state: Save / Load / Quit labels should be on the rows
	// the buttonRange records.
	out := m.Render(80)
	lines := strings.Split(out, "\n")
	for _, b := range []struct {
		name  string
		btn   buttonRange
		label string
	}{
		{"save", m.saveBtn, "[Save Game]"},
		{"load", m.loadBtn, "[Load Game]"},
		{"quit", m.quitBtn, "[Quit]"},
	} {
		if b.btn.row >= len(lines) {
			t.Errorf("%s: row %d out of bounds (len=%d)", b.name, b.btn.row, len(lines))
			continue
		}
		if !strings.Contains(lines[b.btn.row], b.label) {
			t.Errorf("%s: row %d = %q, expected to contain %q", b.name, b.btn.row, lines[b.btn.row], b.label)
		}
	}

	// Confirm state: [Yes] / [No] on the same row recorded. Quit is
	// the only click-confirm gate left (v0.26 moved Save / Load onto
	// the Saves screen, which owns its own confirms).
	quitCol := (m.quitBtn.colStart + m.quitBtn.colEnd) / 2
	m.HandleClick(quitCol, m.quitBtn.row)
	out = m.Render(80)
	lines = strings.Split(out, "\n")
	if m.yesBtn.row >= len(lines) || !strings.Contains(lines[m.yesBtn.row], "[Yes]") {
		t.Errorf("confirm Yes: row %d = %q, expected [Yes]", m.yesBtn.row,
			lines[min(m.yesBtn.row, len(lines)-1)])
	}
	if m.noBtn.row >= len(lines) || !strings.Contains(lines[m.noBtn.row], "[No]") {
		t.Errorf("confirm No: row %d = %q, expected [No]", m.noBtn.row,
			lines[min(m.noBtn.row, len(lines)-1)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestMenuClickConfirmFlow: clicking Quit in the list state
// transitions into a confirm sub-screen rather than firing
// immediately; [Yes] then commits, [No] returns to the list. Save /
// Load clicks fire their action directly — v0.26 (ADR 0033 §F) made
// them screen-openers like VAB / Settings, with the destructive
// confirms living on the Saves screen itself.
func TestMenuClickConfirmFlow(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
	}
	m := NewMenu(th)
	_ = m.Render(80) // populate button ranges

	// Save / Load clicks open the Saves screen directly — no gate.
	saveCol := (m.saveBtn.colStart + m.saveBtn.colEnd) / 2
	if got := m.HandleClick(saveCol, m.saveBtn.row); got != MenuActionSave {
		t.Errorf("Save list click: got %v, want MenuActionSave (opens the Saves screen)", got)
	}
	_ = m.Render(80)
	loadCol := (m.loadBtn.colStart + m.loadBtn.colEnd) / 2
	if got := m.HandleClick(loadCol, m.loadBtn.row); got != MenuActionLoad {
		t.Errorf("Load list click: got %v, want MenuActionLoad (opens the Saves screen)", got)
	}

	// Click Quit in the list — should not immediately fire MenuActionQuit.
	_ = m.Render(80)
	quitCol := (m.quitBtn.colStart + m.quitBtn.colEnd) / 2
	if got := m.HandleClick(quitCol, m.quitBtn.row); got != MenuActionNone {
		t.Errorf("Quit list click: got %v, want MenuActionNone (confirm gate)", got)
	}
	if m.mode != menuModeConfirmQuit {
		t.Errorf("mode after Quit click = %v, want menuModeConfirmQuit", m.mode)
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
	m.HandleClick(quitCol, m.quitBtn.row)
	_ = m.Render(80)
	yesCol := (m.yesBtn.colStart + m.yesBtn.colEnd) / 2
	if got := m.HandleClick(yesCol, m.yesBtn.row); got != MenuActionQuit {
		t.Errorf("Yes click after Quit: got %v, want MenuActionQuit", got)
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

	// Click Quit again, then press 'n' to cancel.
	m.Reset()
	_ = m.Render(80)
	quitCol = (m.quitBtn.colStart + m.quitBtn.colEnd) / 2
	m.HandleClick(quitCol, m.quitBtn.row)
	if got := m.HandleKey("n"); got != MenuActionNone {
		t.Errorf("HandleKey(n) after Quit click: got %v, want MenuActionNone", got)
	}
	if m.mode != menuModeList {
		t.Errorf("mode after n: got %v, want menuModeList", m.mode)
	}
}
