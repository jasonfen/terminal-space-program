package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The quit flow is two ordinary one-step cards (#699, ADR 0054 amendment
// 2026-10-17): "Save before quitting?" (Yes / No / Back, opens on Yes) and
// "Are you sure?" (Yes / No / Back, opens on No).

const (
	cardSave = "Save before quitting?"
	cardSure = "Are you sure?"
)

// quitFlowApp is a fresh 140x40 App with the quit card raised from where:
// "ctrl+c" (flight) or "menu" (esc, then the Quit row).
func quitFlowApp(t *testing.T, where string) (*App, string) {
	t.Helper()
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if where == "menu" {
		a.Update(tea.KeyMsg{Type: tea.KeyEsc})
		a.Update(keyRunes("q"))
	} else {
		a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	}
	if _, ok := cardUp(a, cardSave); !ok {
		t.Fatalf("%s: no %q card:\n%s", where, cardSave, ansi.Strip(a.View()))
	}
	return a, dir
}

// cursorRow is the label the ▸ is on in the card titled title.
func cursorRow(t *testing.T, a *App, title string) string {
	t.Helper()
	card, ok := cardUp(a, title)
	if !ok {
		t.Fatalf("no %q card:\n%s", title, ansi.Strip(a.View()))
	}
	for _, ln := range strings.Split(card, "\n") {
		if k := strings.Index(ln, "▸"); k >= 0 {
			f := strings.Fields(ln[k+len("▸"):])
			if len(f) > 0 {
				return f[0]
			}
		}
	}
	t.Fatalf("no ▸ in %q card:\n%s", title, card)
	return ""
}

// clickCardRow clicks the row labelled label in the card titled title.
func clickCardRow(t *testing.T, a *App, title, label string) {
	t.Helper()
	col, top, rows := cardGeometry(a.View(), title)
	if col < 0 {
		t.Fatalf("no %q card to click", title)
	}
	for i, r := range rows {
		if i > 1 && strings.Contains(r, label) {
			mouseClick(a, col+6, top+i)
			return
		}
	}
	t.Fatalf("no %q row in %q card", label, title)
}

func TestQuitFlowCardsShowTheirRows(t *testing.T) {
	a, _ := quitFlowApp(t, "ctrl+c")
	card, _ := cardUp(a, cardSave)
	for _, w := range []string{"Yes", "No", "Back", "esc"} {
		if !strings.Contains(card, w) {
			t.Errorf("first card lacks %q:\n%s", w, card)
		}
	}
	if got := cursorRow(t, a, cardSave); got != "Yes" {
		t.Errorf("first card opens on %q, want Yes", got)
	}
	a.Update(keyRunes("n"))
	if card, ok := cardUp(a, cardSure); !ok || strings.Contains(card, "yes, ") {
		t.Fatalf("second card missing or still has a confirm step:\n%s", card)
	}
	if got := cursorRow(t, a, cardSure); got != "No" {
		t.Errorf("Are you sure? opens on %q, want No", got)
	}
}

func TestQuitFlowYesSavesAndQuitsAtOnce(t *testing.T) {
	for _, where := range []string{"ctrl+c", "menu"} {
		for _, pick := range []string{"y", "enter"} {
			a, dir := quitFlowApp(t, where)
			var cmd tea.Cmd
			if pick == "y" {
				_, cmd = a.Update(keyRunes("y"))
			} else {
				_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyEnter})
			}
			if !isQuitCmd(cmd) {
				t.Fatalf("%s/%s: Yes did not quit in one press", where, pick)
			}
			if files := savesDirFiles(t, dir); len(files) != 1 {
				t.Fatalf("%s/%s: saves dir = %v, want one autosave", where, pick, files)
			}
		}
	}
}

func TestQuitFlowNoOpensAreYouSureOnNo(t *testing.T) {
	for _, where := range []string{"ctrl+c", "menu"} {
		a, dir := quitFlowApp(t, where)
		_, cmd := a.Update(keyRunes("n"))
		if isQuitCmd(cmd) {
			t.Fatalf("%s: No quit", where)
		}
		if _, ok := cardUp(a, cardSure); !ok {
			t.Fatalf("%s: No did not open %q", where, cardSure)
		}
		if files := savesDirFiles(t, dir); len(files) != 0 {
			t.Fatalf("%s: wrote %v before the answer", where, files)
		}
		// enter on the default (No) goes back to the first card.
		a.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if _, ok := cardUp(a, cardSave); !ok {
			t.Fatalf("%s: enter on No did not return to %q", where, cardSave)
		}
	}
}

func TestQuitFlowSureYesQuitsWithoutSaving(t *testing.T) {
	for _, where := range []string{"ctrl+c", "menu"} {
		a, dir := quitFlowApp(t, where)
		a.Update(keyRunes("n"))
		_, cmd := a.Update(keyRunes("y"))
		if !isQuitCmd(cmd) {
			t.Fatalf("%s: n, y did not quit", where)
		}
		if files := savesDirFiles(t, dir); len(files) != 0 {
			t.Fatalf("%s: quit without saving wrote %v", where, files)
		}
		// Same through the arrows: up to Yes, enter.
		a, dir = quitFlowApp(t, where)
		a.Update(keyRunes("n"))
		a.Update(tea.KeyMsg{Type: tea.KeyUp})
		if got := cursorRow(t, a, cardSure); got != "Yes" {
			t.Fatalf("%s: up from No lands on %q", where, got)
		}
		if _, cmd = a.Update(tea.KeyMsg{Type: tea.KeyEnter}); !isQuitCmd(cmd) {
			t.Fatalf("%s: up, enter did not quit", where)
		}
		if files := savesDirFiles(t, dir); len(files) != 0 {
			t.Fatalf("%s: quit without saving wrote %v", where, files)
		}
	}
}

func TestQuitFlowSureNoReturnsToSaveCard(t *testing.T) {
	a, _ := quitFlowApp(t, "ctrl+c")
	a.Update(keyRunes("n"))
	_, cmd := a.Update(keyRunes("n"))
	if isQuitCmd(cmd) {
		t.Fatal("n, n quit: the second n is No")
	}
	if _, ok := cardUp(a, cardSave); !ok {
		t.Fatal("n, n did not return to Save before quitting?")
	}
	if got := cursorRow(t, a, cardSave); got != "Yes" {
		t.Errorf("returned card opens on %q, want Yes", got)
	}
}

func TestQuitFlowBackReturnsToWhereItOpened(t *testing.T) {
	for _, where := range []string{"ctrl+c", "menu"} {
		for _, depth := range []int{1, 2} {
			a, dir := quitFlowApp(t, where)
			wantActive := a.active
			if depth == 2 {
				a.Update(keyRunes("n"))
			}
			_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if isQuitCmd(cmd) || a.card != nil {
				t.Fatalf("%s/%d: esc quit or left a card up", where, depth)
			}
			if a.active != wantActive {
				t.Errorf("%s/%d: active = %v, want %v", where, depth, a.active, wantActive)
			}
			if where == "menu" && a.active != screenMenu {
				t.Errorf("%s/%d: not back on the pause menu", where, depth)
			}
			if where == "ctrl+c" && a.active != screenOrbit {
				t.Errorf("%s/%d: not back in flight", where, depth)
			}
			if files := savesDirFiles(t, dir); len(files) != 0 {
				t.Errorf("%s/%d: back wrote %v", where, depth, files)
			}
		}
	}
}

func TestQuitFlowSwallowsStrayKeys(t *testing.T) {
	for _, title := range []string{cardSave, cardSure} {
		a, dir := quitFlowApp(t, "ctrl+c")
		if title == cardSure {
			a.Update(keyRunes("n"))
		}
		for _, k := range []string{"x", "s", "q", "1", "?", "c", " ", "m", "Y2"} {
			_, cmd := a.Update(keyRunes(k))
			if isQuitCmd(cmd) {
				t.Fatalf("%s: %q quit", title, k)
			}
			if _, ok := cardUp(a, title); !ok {
				t.Fatalf("%s: %q changed the card:\n%s", title, k, ansi.Strip(a.View()))
			}
		}
		for _, k := range []tea.KeyType{tea.KeyF1, tea.KeyTab, tea.KeyLeft, tea.KeyRight} {
			a.Update(tea.KeyMsg{Type: k})
			if _, ok := cardUp(a, title); !ok {
				t.Fatalf("%s: key %v changed the card", title, k)
			}
		}
		if files := savesDirFiles(t, dir); len(files) != 0 {
			t.Fatalf("%s: stray keys wrote %v", title, files)
		}
	}
}

func TestQuitFlowClicks(t *testing.T) {
	// Card 1: Yes saves and quits.
	a, dir := quitFlowApp(t, "menu")
	clickCardRow(t, a, cardSave, "Yes")
	if files := savesDirFiles(t, dir); len(files) != 1 {
		t.Fatalf("click Yes: saves = %v, want one autosave", files)
	}
	// Card 1: No opens card 2; card 2 Yes quits without saving.
	a, dir = quitFlowApp(t, "menu")
	clickCardRow(t, a, cardSave, "No")
	if _, ok := cardUp(a, cardSure); !ok {
		t.Fatal("click No did not open Are you sure?")
	}
	clickCardRow(t, a, cardSure, "No")
	if _, ok := cardUp(a, cardSave); !ok {
		t.Fatal("click No on Are you sure? did not return")
	}
	clickCardRow(t, a, cardSave, "No")
	_, cmd := func() (tea.Model, tea.Cmd) {
		col, top, rows := cardGeometry(a.View(), cardSure)
		for i, r := range rows {
			if i > 1 && strings.Contains(r, "Yes") {
				return a.Update(tea.MouseMsg{X: col + 6, Y: top + i, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			}
		}
		t.Fatal("no Yes row")
		return a, nil
	}()
	if !isQuitCmd(cmd) {
		t.Fatal("click Yes on Are you sure? did not quit")
	}
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("quit without saving wrote %v", files)
	}
	// Back on either card returns to the pause menu.
	for _, title := range []string{cardSave, cardSure} {
		a, _ = quitFlowApp(t, "menu")
		if title == cardSure {
			a.Update(keyRunes("n"))
		}
		clickCardRow(t, a, title, "Back")
		if a.card != nil || a.active != screenMenu {
			t.Errorf("click Back on %q: card=%v active=%v", title, a.card != nil, a.active)
		}
	}
	// A click off the card is swallowed.
	a, _ = quitFlowApp(t, "menu")
	mouseClick(a, 2, 20)
	if _, ok := cardUp(a, cardSave); !ok {
		t.Error("a click outside dismissed the card")
	}
}
