package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

var keyC = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}

// liquidPairApp flies a synthetic liquid booster under a liquid core
// (no catalog vessel has one yet), burning by hand.
func liquidPairApp(t *testing.T) (*App, *spacecraft.Spacecraft) {
	t.Helper()
	a, sls := burningSLSApp(t)
	sls.Stages = []spacecraft.Stage{
		{Name: "LRB", DryMass: 50_000, FuelMass: 400_000, FuelCapacity: 400_000,
			Thrust: 10e6, Isp: 300, FuelType: spacecraft.FuelTypeHydrolox, LitWithAbove: true},
		{Name: "Core", DryMass: 30_000, FuelMass: 300_000, FuelCapacity: 300_000,
			Thrust: 5e6, Isp: 400, FuelType: spacecraft.FuelTypeHydrolox},
	}
	sls.Stages[1].CommandSource = spacecraft.CommandCrewed
	sls.SyncFields()
	return a, sls
}

// ADR 0058 guard 15.[6]: c cycles and flashes the new target.
func TestCKeyCyclesThrottleTargetAndFlashes(t *testing.T) {
	a, c := liquidPairApp(t)
	for _, want := range []string{"core", "boosters", "all"} {
		pressMsg(a, keyC)
		if a.statusMsg != "throttle: "+want {
			t.Fatalf("statusMsg = %q, want %q", a.statusMsg, "throttle: "+want)
		}
	}
	// Routing end to end: boosters target, x, then the core still burns.
	pressMsg(a, keyC)
	pressMsg(a, keyC) // boosters
	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if c.ThrottleTarget != spacecraft.ThrottleBoosters || c.BoosterThrottleEff() != 0 || c.Throttle != 1 {
		t.Errorf("x on boosters: target=%v boosters=%v core=%v", c.ThrottleTarget, c.BoosterThrottleEff(), c.Throttle)
	}
}

// ADR 0058 guard 15.[6]: a one-throttle vessel (SLS, solid boosters) says so.
func TestCKeyOneThrottleVessel(t *testing.T) {
	a, sls := burningSLSApp(t)
	pressMsg(a, keyC)
	if a.statusMsg != "one throttle" || sls.ThrottleTarget != spacecraft.ThrottleAll {
		t.Errorf("statusMsg = %q target=%v, want %q and all", a.statusMsg, sls.ThrottleTarget, "one throttle")
	}
}
