package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// quitCardRows returns the rows of the rounded quit card in a rendered
// frame (just the card's own cells), or nil when no card containing want is
// on screen, plus the card's left column so a test can check it is centred.
// It reads by column: the card's left edge is the last │ before want on
// want's row, and its top and bottom are the ╭ / ╰ in that same column (the
// map's own boxes have corners elsewhere).
func quitCardRows(out, want string) (rows []string, col int) {
	lines := strings.Split(ansi.Strip(out), "\n")
	grid := make([][]rune, len(lines))
	for i, ln := range lines {
		grid[i] = []rune(ln)
	}
	at := func(r, c int) rune {
		if r < 0 || r >= len(grid) || c < 0 || c >= len(grid[r]) {
			return 0
		}
		return grid[r][c]
	}
	wr := -1
	col = -1
	for i, ln := range lines {
		if k := strings.Index(ln, want); k >= 0 {
			wr = i
			for c := len([]rune(ln[:k])) - 1; c >= 0; c-- {
				if grid[i][c] == '│' {
					col = c
					break
				}
			}
			break
		}
	}
	if wr < 0 || col < 0 {
		return nil, -1
	}
	top, bot := wr, wr
	for top > 0 && at(top, col) != '╭' {
		top--
	}
	for bot < len(grid)-1 && at(bot, col) != '╰' {
		bot++
	}
	for r := top; r <= bot; r++ {
		end := col + screens.QuitCardW
		if end > len(grid[r]) {
			end = len(grid[r])
		}
		rows = append(rows, string(grid[r][col:end]))
	}
	return rows, col
}

// TestQuitPromptIsACardOverTheScreen (Jason 2026-10-05: "when I press q,
// can a quit menu showing the save before quitting? option selections
// display instead of putting the options down in the bottom of the
// screen?"): the quit prompt is a centred card like the pause card, with
// its three choices as rows, at both design sizes, from the map and from
// the pause menu.
func TestQuitPromptIsACardOverTheScreen(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		for _, from := range []string{"map ctrl+c", "pause menu q"} {
			testStateDirs(t)
			a, err := New(nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			a.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			if from == "map ctrl+c" {
				a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			} else {
				a.Update(tea.KeyMsg{Type: tea.KeyEsc})
				a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
			}
			out := a.View()
			rows, col := quitCardRows(out, "Save before quitting?")
			if rows == nil {
				t.Fatalf("%dx%d %s: no quit card on screen:\n%s", sz[0], sz[1], from, ansi.Strip(out))
			}
			card := strings.Join(rows, "\n")
			for _, want := range []string{"Save and quit", "Quit without saving", "Stay", "▸"} {
				if !strings.Contains(card, want) {
					t.Errorf("%dx%d %s: quit card lacks %q:\n%s", sz[0], sz[1], from, want, card)
				}
			}
			if left, right := col, sz[0]-(col+screens.QuitCardW); left < 10 || abs(left-right) > 2 {
				t.Errorf("%dx%d %s: card not centred: %d cells left, %d right", sz[0], sz[1], from, left, right)
			}
			last := strings.Split(ansi.Strip(out), "\n")
			if strings.Contains(last[len(last)-1], "Save before quitting") {
				t.Errorf("%dx%d %s: the old bottom-row prompt is still drawn", sz[0], sz[1], from)
			}
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// TestQuitCardArrowsAndEnterPick: ↑/↓ move the ▸ and enter picks, the same
// way the pause card works; y / n / esc keep working directly (the existing
// quit-prompt tests pin those).
func TestQuitCardArrowsAndEnterPick(t *testing.T) {
	// Default row is Save and quit: enter writes an autosave and quits.
	dir := testStateDirs(t)
	a, _ := New(nil)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !isQuitCmd(cmd) {
		t.Fatal("enter on Save and quit did not quit")
	}
	if files := savesDirFiles(t, dir); len(files) != 1 {
		t.Fatalf("enter on Save and quit: saves dir = %v, want one autosave", files)
	}

	// Down once: Quit without saving, writes nothing.
	dir = testStateDirs(t)
	a, _ = New(nil)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	if rows, _ := quitCardRows(a.View(), "Save before quitting?"); rows == nil || !strings.Contains(strings.Join(rows, "\n"), "▸ Quit without saving") {
		t.Fatalf("down did not move the cursor to Quit without saving:\n%s", strings.Join(rows, "\n"))
	}
	_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !isQuitCmd(cmd) {
		t.Fatal("enter on Quit without saving did not quit")
	}
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("Quit without saving wrote %v", files)
	}

	// Down twice: Stay, which closes the card and keeps playing.
	testStateDirs(t)
	a, _ = New(nil)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if isQuitCmd(cmd) || a.quitConfirm {
		t.Fatal("enter on Stay quit or left the card open")
	}
	// Up wraps from the top row back to Stay rather than running off.
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	a.Update(tea.KeyMsg{Type: tea.KeyUp})
	if rows, _ := quitCardRows(a.View(), "Save before quitting?"); !strings.Contains(strings.Join(rows, "\n"), "▸ Stay") {
		t.Errorf("up from the top did not wrap to Stay:\n%s", strings.Join(rows, "\n"))
	}
}

// TestGuestQuitCardOffersNoDecline: a multiplayer guest's flight saves on
// the way out whatever they choose, so their card has Quit and Stay only.
func TestGuestQuitCardOffersNoDecline(t *testing.T) {
	testStateDirs(t)
	a, _ := New(nil)
	a.guestSave = func(*sim.World) error { return nil }
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	rows, _ := quitCardRows(a.View(), "Quit?")
	if rows == nil {
		t.Fatalf("guest: no quit card:\n%s", ansi.Strip(a.View()))
	}
	card := strings.Join(rows, "\n")
	if strings.Contains(card, "without saving") {
		t.Errorf("guest card offers a decline:\n%s", card)
	}
	for _, want := range []string{"saves automatically", "Quit", "Stay"} {
		if !strings.Contains(card, want) {
			t.Errorf("guest card lacks %q:\n%s", want, card)
		}
	}
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if isQuitCmd(cmd) || a.quitConfirm {
		t.Error("guest: second row should be Stay")
	}
}

// TestQuitCardFromThePauseMenuReplacesIt: raised from the pause menu, the
// quit card stands alone over the map; no edge of the pause card shows.
func TestQuitCardFromThePauseMenuReplacesIt(t *testing.T) {
	testStateDirs(t)
	a, _ := New(nil)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	out := ansi.Strip(a.View())
	if strings.Contains(out, "Save Game") || strings.Contains(out, "[esc] fly") {
		t.Errorf("the pause card is still drawn under the quit card:\n%s", out)
	}
	_, col := quitCardRows(a.View(), "Save before quitting?")
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		r := []rune(ln)
		if col >= 0 && col < len(r) && (r[col] == '╭' || r[col] == '╰') {
			if i+1 < len(lines) && strings.Contains(lines[i+1], "Save before quitting?") {
				continue // the quit card's own top edge
			}
			if strings.Count(out[:strings.Index(out, ln)], "Save before quitting?") == 1 && r[col] == '╰' {
				continue // the quit card's own bottom edge (after its title)
			}
			t.Errorf("a second card edge in the card's column at row %d:\n%s", i, ln)
		}
	}
}
