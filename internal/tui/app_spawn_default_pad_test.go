package tui

import (
	"math"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// openLaunchpadSpawn opens the spawn form through the real 'n' handler and
// selects the LAUNCHPAD position mode the way a player does (Tab to the
// position field, arrow right twice: orbit -> alongside -> launchpad).
func openLaunchpadSpawn(t *testing.T, scenario *sim.StartScenario) *App {
	t.Helper()
	a, err := New(scenario)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if a.active != screenSpawn {
		t.Fatalf("'n' did not open the spawn form (active=%v)", a.active)
	}
	pressMsg(a, tea.KeyMsg{Type: tea.KeyTab})
	pressMsg(a, tea.KeyMsg{Type: tea.KeyRight})
	pressMsg(a, tea.KeyMsg{Type: tea.KeyRight})
	if !a.spawn.SelectedLaunchpad() {
		t.Fatalf("setup: form did not reach LAUNCHPAD mode")
	}
	return a
}

// TestDefaultPadForKernIsEquatorial (#461): open the spawn form in Lumen,
// leave every field alone but pick LAUNCHPAD, and the default pad is the
// equator (so a plain ascent arrives flat at Cursor, not in a 32 degree
// orbit). Sol keeps Cape Canaveral. Goes through the 'n' key handler, the
// form's own cycling, and Enter into World.SpawnCraft.
func TestDefaultPadForKernIsEquatorial(t *testing.T) {
	lumen := openLaunchpadSpawn(t, &sim.StartScenario{SystemName: "Lumen"})
	if got := lumen.spawn.SelectedLatitudeDeg(); got != 0 {
		t.Fatalf("Lumen default pad latitude = %v, want 0 (equatorial)", got)
	}
	pressMsg(lumen, tea.KeyMsg{Type: tea.KeyEnter})
	if lumen.active != screenOrbit {
		t.Fatalf("Enter did not spawn (active=%v)", lumen.active)
	}
	c := lumen.world.ActiveCraft()
	if c == nil || c.LaunchLatDeg != 0 {
		t.Fatalf("spawned Lumen vessel LaunchLatDeg = %+v, want 0", c)
	}

	sol := openLaunchpadSpawn(t, nil)
	if got := sol.spawn.SelectedLatitudeDeg(); math.Abs(got-sim.DefaultLaunchpadLatitude) > 1e-9 {
		t.Fatalf("Sol default pad latitude = %v, want KSC %v", got, sim.DefaultLaunchpadLatitude)
	}
}

// TestKernPadNamedInSpawnForm (review LOW 98): the Lumen form's LAUNCH SITE
// row names the default pad Kern Space Center, not the generic Equator.
func TestKernPadNamedInSpawnForm(t *testing.T) {
	lumen := openLaunchpadSpawn(t, &sim.StartScenario{SystemName: "Lumen"})
	if v := lumen.spawn.Render(140, 40); !strings.Contains(v, "Kern Space Center") {
		t.Fatalf("Lumen spawn form does not name Kern Space Center:\n%s", v)
	}
}
