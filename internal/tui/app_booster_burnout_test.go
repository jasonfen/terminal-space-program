package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// burningSLSApp is an App flying the marked SLS in orbit with the burn on.
func burningSLSApp(t *testing.T) (*App, *spacecraft.Spacecraft) {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world
	sls := spacecraft.NewFromLoadout("SLS-Block1")
	sls.Primary = w.Crafts[0].Primary
	sls.State = w.Crafts[0].State
	sls.Stages[len(sls.Stages)-1].CommandSource = spacecraft.CommandCrewed
	sls.SyncFields()
	sls.State.M = sls.TotalMass()
	w.Crafts[0] = sls
	w.ActiveCraftIdx = 0
	sls.Throttle = 1
	sls.EngineMode = spacecraft.EngineMain
	w.StartManualBurn()
	if sls.ManualBurn == nil {
		t.Fatal("setup: burn did not start")
	}
	return a, sls
}

// ADR 0058 decision 5: the burnout is an Event Flash, once.
func TestBoostersBurnedOutFlashesOnce(t *testing.T) {
	a, sls := burningSLSApp(t)
	a.Update(sim.TickMsg(time.Now()))
	if strings.Contains(a.statusMsg, "burned out") {
		t.Fatalf("flash before burnout: %q", a.statusMsg)
	}
	// ~1.5 s of solid left at 12,176 kg/s: burns out within a few ticks.
	sls.Stages[0].FuelMass = 18000
	for i := 0; i < 40 && sls.Stages[0].FuelMass > 0; i++ {
		a.Update(sim.TickMsg(time.Now()))
	}
	if sls.Stages[0].FuelMass > 0 {
		t.Fatal("setup: solids never ran dry")
	}
	if a.statusMsg != "boosters burned out" {
		t.Fatalf("statusMsg = %q, want %q", a.statusMsg, "boosters burned out")
	}
	a.statusMsg = ""
	a.Update(sim.TickMsg(time.Now()))
	if a.statusMsg != "" {
		t.Errorf("flashed again on the next tick: %q", a.statusMsg)
	}
}

// ADR 0058 decision 5: `space` while the lit solids burn flashes the bare
// refusal, not the generic "stage failed: ..." wrapper.
func TestSpaceRefusedFlashWhileSolidsBurn(t *testing.T) {
	a, sls := burningSLSApp(t)
	n := len(sls.Stages)
	pressMsg(a, tea.KeyMsg{Type: tea.KeySpace})
	if a.statusMsg != "boosters still burning" {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, "boosters still burning")
	}
	if len(sls.Stages) != n {
		t.Errorf("stages %d -> %d: the refused press dropped something", n, len(sls.Stages))
	}
}
