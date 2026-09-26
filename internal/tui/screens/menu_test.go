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
		{"c", MenuActionControls},
		{"C", MenuActionControls},
		{"h", MenuActionHelp},
		{"H", MenuActionHelp},
		{"q", MenuActionQuit},
		{"Q", MenuActionQuit},
		{"esc", MenuActionCancel},
		{"x", MenuActionNone},
		{"", MenuActionNone},
	}
	for _, c := range cases {
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

	// Quit fires MenuActionQuit directly on click (#474 removed the
	// menu's own [Yes]/[No] confirm sub-state — the app-level quit
	// prompt owns confirmation now).
	quitCol := (m.quitBtn.colStart + m.quitBtn.colEnd) / 2
	if got := m.HandleClick(quitCol, m.quitBtn.row); got != MenuActionQuit {
		t.Errorf("Quit click = %v, want MenuActionQuit", got)
	}
}

// TestMenuControlsRowRenamedAndHelpRowAdded (#425): the [Controls] row
// (which sounded like the keybinding list but was only the QWERTY/QWERTZ
// picker) is now labelled "[Keyboard layout]" on the same `c` key/action,
// and a new "[Help (F1)]" row opens the actual keybinding list.
func TestMenuControlsRowRenamedAndHelpRowAdded(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
	}
	m := NewMenu(th)
	out := m.Render(80)

	if strings.Contains(out, "[Controls]") {
		t.Error("menu still shows the old [Controls] label")
	}
	if !strings.Contains(out, "[Keyboard layout]") {
		t.Errorf("menu missing [Keyboard layout] row:\n%s", out)
	}
	if !strings.Contains(out, "[Help (F1)]") {
		t.Errorf("menu missing [Help (F1)] row:\n%s", out)
	}

	lines := strings.Split(out, "\n")
	if m.controlsBtn.row >= len(lines) || !strings.Contains(lines[m.controlsBtn.row], "[Keyboard layout]") {
		t.Errorf("controlsBtn row %d doesn't contain [Keyboard layout]: %q", m.controlsBtn.row, lines[min(m.controlsBtn.row, len(lines)-1)])
	}
	if m.helpBtn.row >= len(lines) || !strings.Contains(lines[m.helpBtn.row], "[Help (F1)]") {
		t.Errorf("helpBtn row %d doesn't contain [Help (F1)]: %q", m.helpBtn.row, lines[min(m.helpBtn.row, len(lines)-1)])
	}

	// Both mouse click and key letter must fire the same action.
	if got := m.HandleKey("h"); got != MenuActionHelp {
		t.Errorf("HandleKey(h) = %v, want MenuActionHelp", got)
	}
	m.Reset()
	m.Render(80) // repopulate button ranges after Reset cleared them
	col := (m.helpBtn.colStart + m.helpBtn.colEnd) / 2
	if got := m.HandleClick(col, m.helpBtn.row); got != MenuActionHelp {
		t.Errorf("HandleClick(helpBtn) = %v, want MenuActionHelp", got)
	}
	col = (m.controlsBtn.colStart + m.controlsBtn.colEnd) / 2
	if got := m.HandleClick(col, m.controlsBtn.row); got != MenuActionControls {
		t.Errorf("HandleClick(controlsBtn) = %v, want MenuActionControls (same action, renamed label)", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestMenuQuitFiresDirectlyBothPaths (#474): the menu's Quit row no
// longer owns any confirm state of its own — both the "q" key and the
// [Quit] click fire MenuActionQuit immediately, same as every other
// row. Confirmation now lives entirely on the app-level quit prompt
// (App.applyMenuAction arms it), which is the single question ctrl+c
// also raises.
func TestMenuQuitFiresDirectlyBothPaths(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
	}
	m := NewMenu(th)
	_ = m.Render(80) // populate button ranges

	for _, key := range []string{"q", "Q"} {
		if got := m.HandleKey(key); got != MenuActionQuit {
			t.Errorf("HandleKey(%q) = %v, want MenuActionQuit", key, got)
		}
	}
	quitCol := (m.quitBtn.colStart + m.quitBtn.colEnd) / 2
	if got := m.HandleClick(quitCol, m.quitBtn.row); got != MenuActionQuit {
		t.Errorf("Quit click = %v, want MenuActionQuit", got)
	}
}
