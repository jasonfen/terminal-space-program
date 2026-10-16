package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// ADR 0055 amendment: `;` (or the navball [MODE] click) reaching PORT turns
// nothing and flashes exactly `nav: PORT`, on a tug and on an Apollo CM
// alike. The sabotage (the dropped Apollo-era hint, or an auto-hold) fails
// the flash text and the hold checks.
func TestSemicolonIntoPortFlashesNavPortAndHoldsNothing(t *testing.T) {
	for _, part := range []string{"rcs-tug", "command-module"} {
		for _, via := range []string{"key", "click"} {
			a, err := New(nil)
			if err != nil {
				t.Fatal(err)
			}
			w := a.world
			if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
				t.Fatal(err)
			}
			w.ActiveCraftIdx = 0
			own, tgt := w.Crafts[0], w.Crafts[1]
			tgt.Primary = own.Primary
			st, ok := spacecraft.BuildStage(part)
			if !ok {
				t.Fatalf("no part %q", part)
			}
			own.Stages = []spacecraft.Stage{st}
			own.SyncFields()
			tp, _ := spacecraft.BuildStage("nudge-tug")
			tgt.Stages = []spacecraft.Stage{tp}
			tgt.SyncFields()
			w.SetTargetCraft(1)
			if !w.TargetHasPort() {
				t.Fatal("fixture target has no port")
			}
			own.AttitudeMode = spacecraft.BurnPrograde
			w.NavMode = sim.NavTarget
			a.statusMsg = ""
			if via == "key" {
				a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{';'}})
			} else {
				a.dispatchNavballControl(screens.NavballControlMode)
			}
			if w.NavMode != sim.NavPort {
				t.Fatalf("%s/%s: NavMode = %v, want PORT", part, via, w.NavMode)
			}
			if a.statusMsg != "nav: PORT" {
				t.Errorf("%s/%s: flash = %q, want %q", part, via, a.statusMsg, "nav: PORT")
			}
			if own.AttitudeMode != spacecraft.BurnPrograde {
				t.Errorf("%s/%s: reaching PORT set hold %v", part, via, own.AttitudeMode)
			}
		}
	}
}
