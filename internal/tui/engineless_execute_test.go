package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// Audit 18: execute-now (zero TriggerTime, impulsive) on a vessel with no
// engine changes nothing and says why.
func TestExecuteNowEnginelessFlashesAndGainsNothing(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	c.Thrust, c.Isp = 0, 0
	v0 := c.State.V
	a.Update(screens.BurnExecutedMsg{EditingIdx: -1, Mode: spacecraft.BurnPrograde, DV: 1000})
	if d := c.State.V.Sub(v0).Norm(); d != 0 {
		t.Errorf("engine-less vessel gained %.1f m/s from execute-now", d)
	}
	if want := c.Name + ": no engine"; a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
}
