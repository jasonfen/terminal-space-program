package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// findHitCol scans row 0's columns [0, width) for the first one where
// hit reports true, for click tests that need a real column rather
// than a hardcoded guess at a screen's current layout.
func findHitCol(width int, hit func(col, row int) bool) (int, bool) {
	for col := 0; col < width; col++ {
		if hit(col, 0) {
			return col, true
		}
	}
	return 0, false
}

// TestLaunchViewBurnButtonClickTogglesAutoWarpNotMissions (#456): a
// click on the Launch View's own [»Burn] button used to fall through
// to the shared OrbitView's Menu/Missions/Burn hit-test columns, which
// LaunchView.Render never updates — they stay frozen at whatever the
// orbit MAP's own, differently-laid-out title bar last computed. Since
// the map's rightmost title-bar button is [Missions], a click on the
// launch view's [»Burn] button (which sits at a different column than
// the map's own burn button) could land inside the map's stale
// [Missions] range instead, sending the player to the Missions screen.
//
// Reproduces the exact bug shape: render the orbit map once first (the
// realistic case — the player was on the map before this launch, or
// will cycle back to it), THEN render Launch View, THEN click where
// Launch View's own [»Burn] button visually sits.
func TestLaunchViewBurnButtonClickTogglesAutoWarpNotMissions(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})

	// Render the orbit map once — seeds a.orbitView's title-bar hit-test
	// fields with the MAP's own layout.
	a.View()

	// Now switch to Launch View and plant an eligible burn so a
	// successful dispatch to toggleAutoWarpBurn is observable (engages
	// Auto-Warp), not just "nothing happened".
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("fresh world has no active craft")
	}
	a.world.PlanNode(sim.ManeuverNode{
		DV:          10,
		Mode:        spacecraft.BurnPrograde,
		TriggerTime: a.world.Clock.SimTime.Add(2 * time.Hour),
	})
	a.world.ViewMode = sim.ViewLaunch
	a.View() // render Launch View — sets a.launchView's own hit-test range

	col, ok := findHitCol(140, a.launchView.HitBurnButton)
	if !ok {
		t.Fatal("test setup: LaunchView never computed a [»Burn] hit-test range")
	}

	a.Update(tea.MouseMsg{X: col, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})

	if a.active == screenMissions {
		t.Fatalf("clicking Launch View's [»Burn] button navigated to Missions (stale map hit-test), want it to stay on the flight screen")
	}
	if a.active == screenMenu {
		t.Fatalf("clicking Launch View's [»Burn] button opened the Menu (stale map hit-test), want it to stay on the flight screen")
	}
	if !a.world.AutoWarpEngaged() {
		t.Errorf("clicking Launch View's [»Burn] button did not engage Auto-Warp")
	}
}

// TestLaunchViewIgnoresStaleMapMissionsHit (#456): the orbit map's own
// [Missions] button hit-test range, left over from rendering the map
// before this launch, must not fire while Launch View is showing — the
// exact mechanism behind the reported bug, isolated from whether the
// two ranges happen to overlap on any particular layout.
func TestLaunchViewIgnoresStaleMapMissionsHit(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.View() // seed the map's own Menu/Missions/Burn hit-test columns

	col, ok := findHitCol(140, a.orbitView.HitMissionsButton)
	if !ok {
		t.Fatal("test setup: orbit map never computed a [Missions] hit-test range")
	}

	a.world.ViewMode = sim.ViewLaunch
	a.View()
	before := a.active

	a.Update(tea.MouseMsg{X: col, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})

	if a.active != before {
		t.Errorf("a click on the map's stale [Missions] column changed the screen while Launch View was showing: %v -> %v", before, a.active)
	}
}
