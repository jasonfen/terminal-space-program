package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// B11 review fixes (H1, M2, L6): the Title Row must never be wider than the
// terminal, on any screen, so the clock keeps its column and the way out
// ([Back] / [Menu] / [Missions]) is never clipped off the edge.

func titleRowScreens(t *testing.T) []struct {
	name string
	set  func(a *App)
} {
	return []struct {
		name string
		set  func(a *App)
	}{
		{"map", func(a *App) {}},
		{"map-two-vessels", func(a *App) {
			if _, err := a.world.SpawnSisterCraft(); err != nil {
				t.Fatalf("SpawnSisterCraft: %v", err)
			}
			a.world.ActiveCraft().Name = "Saturn V-1" // the long name that overflowed in review
		}},
		{"proximity-two-vessels", func(a *App) {
			if _, err := a.world.SpawnSisterCraft(); err != nil {
				t.Fatalf("SpawnSisterCraft: %v", err)
			}
			a.world.ActiveCraftIdx = 0
			a.world.ActiveCraft().Name = "Saturn V-1" // the long name that overflowed in review
			a.world.SetTargetCraft(1)
			a.world.ViewMode = sim.ViewTop
			pressO(a)
		}},
		{"launch", func(a *App) { a.world.ViewMode = sim.ViewLaunch }},
		{"porkchop", func(a *App) {
			for i, b := range a.world.System().Bodies {
				if b.EnglishName == "Jupiter" {
					a.porkchop.Load(a.world, i)
				}
			}
			a.active = screenPorkchop
		}},
		{"settings", func(a *App) { a.active = screenSettings }},
		{"controls", func(a *App) { a.active = screenControls }},
		{"missions", func(a *App) { a.active = screenMissions }},
		{"saves", func(a *App) { a.active = screenSaves }},
		{"spawn", func(a *App) { a.active = screenSpawn }},
		{"vab", func(a *App) { a.active = screenVAB }},
		{"help", func(a *App) { a.active = screenHelp }},
		{"session", func(a *App) { a.active = screenSession }},
		{"bodyinfo", func(a *App) { a.active = screenBodyInfo }},
		{"maneuver", func(a *App) { a.active = screenManeuver }},
	}
}

func TestTitleRowNeverExceedsTheTerminalWidth(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		for _, tc := range titleRowScreens(t) {
			a := newChromeApp(t, sz[0], sz[1])
			tc.set(a)
			row := firstRow(a)
			if w := lipgloss.Width(row); w > sz[0] {
				t.Errorf("%s %dx%d: Title Row is %d cells, terminal is %d: %q", tc.name, sz[0], sz[1], w, sz[0], row)
			}
			wantBtn := "[Back]"
			if strings.HasPrefix(tc.name, "map") || strings.HasPrefix(tc.name, "proximity") || tc.name == "launch" {
				wantBtn = "[Missions]"
			}
			if !strings.Contains(row, wantBtn) {
				t.Errorf("%s %dx%d: Title Row lost %s: %q", tc.name, sz[0], sz[1], wantBtn, row)
			}
			if wantBtn == "[Back]" {
				a.View()
				if got := lipgloss.Width(row[:strings.Index(row, "[Back]")]); got != a.backStart {
					t.Errorf("%s %dx%d: [Back] drawn at col %d but hit-test starts at %d", tc.name, sz[0], sz[1], got, a.backStart)
				}
			}
		}
	}
}

// The clock must sit in the same column on every screen, overflowing left
// field or not.
func TestTitleRowClockColumnIsFixedOnEveryScreen(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		want := -1
		for _, tc := range titleRowScreens(t) {
			a := newChromeApp(t, sz[0], sz[1])
			tc.set(a)
			row := firstRow(a)
			idx := strings.Index(row, "T+")
			if idx < 0 {
				t.Errorf("%s %dx%d: no clock in %q", tc.name, sz[0], sz[1], row)
				continue
			}
			col := lipgloss.Width(row[:idx])
			if want < 0 {
				want = col
			} else if col != want {
				t.Errorf("%s %dx%d: clock at col %d, others at %d", tc.name, sz[0], sz[1], col, want)
			}
		}
	}
}

// L6: Proximity names the active vessel like the map does.
func TestProximityTitleRowNamesTheActiveVessel(t *testing.T) {
	a := newChromeApp(t, 181, 49)
	if _, err := a.world.SpawnSisterCraft(); err != nil {
		t.Fatal(err)
	}
	a.world.ActiveCraftIdx = 0
	a.world.ActiveCraft().Name = "Saturn V-1"
	a.world.SetTargetCraft(1)
	a.world.ViewMode = sim.ViewTop
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if a.world.ViewMode != sim.ViewProximity {
		t.Fatalf("not in proximity: %s", a.world.ViewMode)
	}
	name := a.world.ActiveCraft().Name
	if row := firstRow(a); !strings.Contains(row, "VESSEL 1/2 · "+name+" · focus") {
		t.Errorf("Proximity Title Row %q does not name the active vessel %q", row, name)
	}
}
