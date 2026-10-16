package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0054 slice A: the four flight asks (F9 quickload, E end flight, D
// transpose, Y deploy) and the quit prompt are cards now; app_card_test.go
// holds their guards (enter picks the highlighted row, an action asks twice).
// What stays below are the asks other slices convert (saves, roster, VAB).
//
// GK Q7 (#589): Enter never answers a [y/n] ask. Every ask in the game is
// driven here through the real App.Update: raise the ask, press Enter, and
// the ask must still be up with nothing changed; then the same ask must
// still confirm on y and cancel on esc. Quit is not in the table: it is a
// highlighted list (Enter takes the row), not a [y/n] line.

type ynAsk struct {
	name string
	// setup returns an app with the ask raised.
	setup func(t *testing.T) *App
	// up reports whether the ask is on screen.
	up func(a *App) bool
	// untouched reports that no side effect of "yes" has landed.
	untouched func(a *App) bool
}

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

func rosterApp(t *testing.T) *App {
	t.Helper()
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.world.Session = &sim.SessionInfo{
		IsHost: true, CanAdminister: true, Self: "local",
		Players: []sim.SessionPlayer{
			{Fingerprint: "local", Handle: "jason", Role: "host", Online: true},
			{Fingerprint: "SHA256:guest", Handle: "pilot2", Role: "guest", Online: true},
		},
	}
	a.Update(keyRunes("O"))
	if a.active != screenSession {
		t.Fatalf("setup: O did not open the roster (active=%v)", a.active)
	}
	a.Update(keyRunes("j")) // onto pilot2
	return a
}

func savedGameApp(t *testing.T, item string) (*App, string) {
	t.Helper()
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyF5}) // quicksave lane: loadable and deletable
	if f := savesDirFiles(t, dir); len(f) != 1 {
		t.Fatalf("setup: quicksave files = %v", f)
	}
	return a, dir
}

func ynAsks() []ynAsk {
	saves := func(mode, ask string, down int) func(t *testing.T) *App {
		return func(t *testing.T) *App {
			a, _ := savedGameApp(t, mode)
			if mode == "s" {
				// Overwrite needs a named save: the quicksave lane is
				// reserved, so write one by name first.
				if _, err := save.WriteNamed(a.world, "keeper"); err != nil {
					t.Fatalf("WriteNamed: %v", err)
				}
			}
			openSavesVia(t, a, mode)
			for i := 0; i < down; i++ {
				press(a, "down")
			}
			press(a, ask)
			return a
		}
	}
	savesUp := func(a *App) bool {
		return a.active == screenSaves && strings.Contains(a.View(), "[Yes]")
	}
	return []ynAsk{
		{"saves load", func(t *testing.T) *App {
			a := saves("l", "enter", 0)(t)
			a.world.Clock.WarpIdx = 3 // a load would reset this to the quicksaved value
			return a
		}, savesUp, func(a *App) bool { return a.world.Clock.WarpIdx == 3 }},
		{"saves overwrite", func(t *testing.T) *App {
			a := saves("s", "enter", 1)(t)
			return a
		}, savesUp, func(a *App) bool { return a.active == screenSaves }},
		{"saves delete", func(t *testing.T) *App {
			return saves("l", "d", 0)(t)
		}, savesUp, func(a *App) bool {
			dir, _ := save.SavesDir()
			e, _ := os.ReadDir(dir)
			n := 0
			for _, f := range e {
				if filepath.Ext(f.Name()) == ".json" {
					n++
				}
			}
			return n >= 1
		}},
		{"roster remove player", func(t *testing.T) *App {
			a := rosterApp(t)
			a.Update(keyRunes("x"))
			return a
		}, func(a *App) bool { return strings.Contains(a.View(), "they stay connected until they disconnect") }, func(a *App) bool { return true }},
		{"roster stop hosting", func(t *testing.T) *App {
			a := rosterApp(t)
			a.Update(keyRunes("h"))
			return a
		}, func(a *App) bool { return strings.Contains(a.View(), "stop hosting?") }, func(a *App) bool { return true }},
		{"roster restart server", func(t *testing.T) *App {
			a := rosterApp(t)
			a.Update(tea.KeyMsg{Type: tea.KeyF4})
			return a
		}, func(a *App) bool { return strings.Contains(a.View(), "restart server? drops") }, func(a *App) bool { return true }},
		{"VAB delete design", func(t *testing.T) *App {
			testStateDirs(t)
			a, err := New(nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
			press(a, "esc")
			press(a, "b")
			if a.active != screenVAB {
				t.Fatalf("setup: b did not open the VAB (active=%v)", a.active)
			}
			press(a, "s")
			for _, r := range "Moon Lifter" {
				press(a, string(r))
			}
			press(a, "enter")
			press(a, "o")
			if !strings.Contains(a.View(), "LOAD DESIGN") {
				t.Fatalf("setup: o did not open the design list:\n%s", a.View())
			}
			press(a, "x")
			return a
		}, func(a *App) bool { return strings.Contains(a.View(), "This cannot be undone. [y/n]") }, func(a *App) bool {
			return designFileCount() == 1
		}},
	}
}


func designFileCount() int {
	e, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "terminal-space-program", "designs", "*.json"))
	return len(e)
}

func TestEnterNeverAnswersAYNAsk(t *testing.T) {
	for _, c := range ynAsks() {
		t.Run(c.name, func(t *testing.T) {
			a := c.setup(t)
			if !c.up(a) {
				t.Fatalf("setup: the ask is not up:\n%s", a.View())
			}
			a.Update(enterKey())
			if !c.up(a) {
				t.Errorf("Enter dismissed or answered the ask; it must do nothing and leave the question up:\n%s", a.View())
			}
			if !c.untouched(a) {
				t.Errorf("Enter changed state behind the ask")
			}
		})
	}
}

// y still says yes and esc still says no on every ask: Q7 removes Enter, not
// the answers.
func TestYNAsksStillAnswerYAndEsc(t *testing.T) {
	for _, c := range ynAsks() {
		t.Run(c.name+"/esc", func(t *testing.T) {
			a := c.setup(t)
			if !c.up(a) {
				t.Fatalf("setup: the ask is not up")
			}
			a.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if c.up(a) && c.name != "saves delete" && c.name != "saves load" && c.name != "saves overwrite" {
				t.Errorf("esc did not cancel the ask")
			}
			if strings.HasPrefix(c.name, "saves") && strings.Contains(a.View(), "[Yes]") {
				t.Errorf("esc did not cancel the saves ask")
			}
			if !c.untouched(a) && !strings.HasPrefix(c.name, "VAB") {
				t.Errorf("esc changed state")
			}
		})
		t.Run(c.name+"/y", func(t *testing.T) {
			a := c.setup(t)
			if !c.up(a) {
				t.Fatalf("setup: the ask is not up")
			}
			_, cmd := a.Update(keyRunes("y"))
			if c.up(a) {
				t.Errorf("y left the ask up")
			}
			switch c.name {
			case "roster remove player", "roster stop hosting", "roster restart server":
				if cmd == nil {
					t.Errorf("y on the roster ask emitted no command")
				}
			default:
				if c.untouched(a) {
					t.Errorf("y confirmed nothing: state is as before")
				}
			}
		})
	}
}
