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
		{"k", MenuActionControls},
		{"K", MenuActionControls},
		{"c", MenuActionNone}, // ADR 0052 decision 6: Keyboard layout moved c -> k
		{"C", MenuActionNone},
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
// card that visually contains that button (card-relative, B11).
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
	out := m.Render()
	lines := strings.Split(out, "\n")
	for _, b := range []struct {
		name  string
		btn   buttonRange
		label string
	}{
		{"save", m.saveBtn, "Save Game"},
		{"load", m.loadBtn, "Load Game"},
		{"quit", m.quitBtn, "Quit"},
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
	out := m.Render()

	if strings.Contains(out, "Controls") {
		t.Error("menu still shows the old Controls label")
	}
	if !strings.Contains(out, "Keyboard layout") {
		t.Errorf("menu missing Keyboard layout row:\n%s", out)
	}
	if !strings.Contains(out, "Help") || !strings.Contains(out, "h  F1") {
		t.Errorf("menu missing the Help row with its F1 hint:\n%s", out)
	}

	lines := strings.Split(out, "\n")
	if m.controlsBtn.row >= len(lines) || !strings.Contains(lines[m.controlsBtn.row], "Keyboard layout") {
		t.Errorf("controlsBtn row %d doesn't contain Keyboard layout: %q", m.controlsBtn.row, lines[min(m.controlsBtn.row, len(lines)-1)])
	}
	if m.helpBtn.row >= len(lines) || !strings.Contains(lines[m.helpBtn.row], "Help") {
		t.Errorf("helpBtn row %d doesn't contain Help: %q", m.helpBtn.row, lines[min(m.helpBtn.row, len(lines)-1)])
	}

	// Both mouse click and key letter must fire the same action.
	if got := m.HandleKey("h"); got != MenuActionHelp {
		t.Errorf("HandleKey(h) = %v, want MenuActionHelp", got)
	}
	m.Reset()
	m.Render() // repopulate button ranges after Reset cleared them
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
	_ = m.Render() // populate button ranges

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

// TestMenuArrowsMoveHighlightAndEnterOpensRow (ADR 0052 decision 6): up/down
// walk a highlighted row, enter fires it, the cursor wraps, and Reset puts
// it back on the first row. Letters keep working as shortcuts.
func TestMenuArrowsMoveHighlightAndEnterOpensRow(t *testing.T) {
	m := NewMenu(Theme{})
	order := []MenuAction{MenuActionSave, MenuActionLoad, MenuActionVAB,
		MenuActionSettings, MenuActionControls, MenuActionHelp, MenuActionQuit}

	if got := m.HandleKey("enter"); got != MenuActionSave {
		t.Fatalf("enter on a fresh menu = %v, want the first row (Save)", got)
	}
	for i, want := range order {
		if got := m.HandleKey("enter"); got != want {
			t.Errorf("row %d: enter = %v, want %v", i, got, want)
		}
		if got := m.HandleKey("down"); got != MenuActionNone {
			t.Errorf("down returned action %v, want None", got)
		}
	}
	// Past the last row it wraps to the first.
	if got := m.HandleKey("enter"); got != MenuActionSave {
		t.Errorf("down past the last row did not wrap: enter = %v", got)
	}
	m.HandleKey("up")
	if got := m.HandleKey("enter"); got != MenuActionQuit {
		t.Errorf("up from the first row did not wrap to Quit: enter = %v", got)
	}
	m.Reset()
	if got := m.HandleKey("enter"); got != MenuActionSave {
		t.Errorf("Reset did not return the cursor to Save: enter = %v", got)
	}
}

// TestMenuRendersHighlightAndKeyedFooter: the highlighted row carries a
// marker the others lack, the letters stay beside every row, and the footer
// names the arrow keys instead of the old letter string.
func TestMenuRendersHighlightAndKeyedFooter(t *testing.T) {
	th := Theme{Primary: lipgloss.NewStyle(), Title: lipgloss.NewStyle(), Dim: lipgloss.NewStyle(), Footer: lipgloss.NewStyle()}
	m := NewMenu(th)
	m.HandleKey("down") // Load
	out := m.Render()
	var marked []string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "▸") {
			marked = append(marked, ln)
		}
	}
	if len(marked) != 1 || !strings.Contains(marked[0], "Load Game") {
		t.Errorf("want exactly one ▸ row and it is Load; got %q", marked)
	}
	// Every row carries its shortcut letter in a column to the right.
	lines := strings.Split(out, "\n")
	for _, r := range menuRows {
		found := false
		for _, ln := range lines {
			if strings.Contains(ln, r.label) && strings.HasSuffix(strings.TrimRight(strings.TrimSpace(strings.TrimRight(ln, "│ ")), " "), menuKeyHint(r)) {
				found = true
			}
		}
		if !found {
			t.Errorf("row %q has no shortcut %q at its right:\n%s", r.label, menuKeyHint(r), out)
		}
	}
	for _, want := range []string{"[↑/↓]", "[enter]", "[esc] fly"} {
		if !strings.Contains(out, want) {
			t.Errorf("menu render lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(c)") || strings.Contains(out, "s/l/b/t/c/h/q") {
		t.Errorf("menu still advertises the old c key:\n%s", out)
	}
	// The card is the size the grill fixed (G9 Q6): 40x13, wordmark and
	// version on its first inner row.
	if len(lines) != MenuCardH {
		t.Errorf("card is %d rows, want %d", len(lines), MenuCardH)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != MenuCardW {
			t.Errorf("card row %d is %d cells wide, want %d: %q", i, w, MenuCardW, ln)
		}
	}
	if !strings.Contains(lines[1], "Terminal Space Program") {
		t.Errorf("card has no wordmark on its first inner row: %q", lines[1])
	}
}

// TestMenuLetterKIsKeyboardLayoutAndHasNoVimCursor (review LOW 120, no code
// change): the finding observes that `k` is the menu's Keyboard-layout
// shortcut (ADR 0052 decision 6) while it is SAS on the orbit screen. The
// menu is modal and prints (k) beside the row, and it has no vim cursor
// keys, so `j`/`k` can never mean "move" here. Pins both so a later vim
// binding cannot silently steal `k`.
func TestMenuLetterKIsKeyboardLayoutAndHasNoVimCursor(t *testing.T) {
	m := NewMenu(Theme{})
	if got := m.HandleKey("k"); got != MenuActionControls {
		t.Errorf("k = %v, want MenuActionControls (keyboard layout)", got)
	}
	m = NewMenu(Theme{})
	before := m.cursor
	if got := m.HandleKey("j"); got != MenuActionNone || m.cursor != before {
		t.Errorf("j moved or fired in the menu (action %v, cursor %d -> %d)", got, before, m.cursor)
	}
}
