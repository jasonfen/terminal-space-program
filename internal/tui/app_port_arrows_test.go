package tui

import (
	"math"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// portArrowsApp is an App with two RCS Tugs port to port 25 m apart (the
// Alongside spawn), the new one active, the other its target, NavMode PORT
// with Port+ held and the nose settled on the hold.
func portArrowsApp(t *testing.T) *App {
	t.Helper()
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	w := a.world
	old := w.Crafts[0]
	tug := spacecraft.NewFromLoadout("RCS-tug")
	tug.Primary = old.Primary
	tug.State = old.State
	tug.State.M = tug.TotalMass()
	tug.CurrentAttitudeDir = tug.State.V.Unit()
	tug.Name = "Home"
	w.Crafts[0] = tug
	if _, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: "RCS-tug", Alongside: true}); err != nil {
		t.Fatal(err)
	}
	w.SetTargetCraft(0)
	w.NavMode = sim.NavPort
	w.ActiveCraft().AttitudeMode = spacecraft.BurnPortIn
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	settlePort(t, w)
	return a
}

// settlePort ticks until the nose has slewed onto the commanded direction.
func settlePort(t *testing.T, w *sim.World) {
	t.Helper()
	for i := 0; i < 80; i++ {
		w.Tick()
	}
}

func misalign(t *testing.T, w *sim.World) (up, right float64) {
	t.Helper()
	st, ok := w.ProximityState()
	if !ok || !st.HasPort {
		t.Fatal("no port pair")
	}
	return st.MisalignUpDeg, st.MisalignRightDeg
}

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// ADR 0055 decision 4 (slice 3): in PORT a plain arrow is 1° across the port
// face in the frame the gate measures, never the 5° surface trim, and the
// directions are up / down / right / left. The settled nose is the positive
// control (misalign 0 before any key). Sabotage (capture 15's old behaviour,
// arrows apply the surface trim in PORT): misalign stays ~0 and PitchTrim /
// HeadingTrim move 5°.
func TestPortArrowsTipTheNoseOneDegreeAcrossThePortFace(t *testing.T) {
	cases := []struct {
		name         string
		msg          tea.KeyMsg
		wantU, wantR float64
	}{
		{"up", keyType(tea.KeyUp), 1, 0},
		{"down", keyType(tea.KeyDown), -1, 0},
		{"right", keyType(tea.KeyRight), 0, 1},
		{"left", keyType(tea.KeyLeft), 0, -1},
		{"alt+up", keyAlt(tea.KeyUp), 0.25, 0},
		{"alt+down", keyAlt(tea.KeyDown), -0.25, 0},
		{"alt+right", keyAlt(tea.KeyRight), 0, 0.25},
		{"alt+left", keyAlt(tea.KeyLeft), 0, -0.25},
	}
	for _, k := range cases {
		a := portArrowsApp(t)
		w := a.world
		if u, r := misalign(t, w); !approx(u, 0, 0.01) || !approx(r, 0, 0.01) {
			t.Fatalf("%s: positive control failed, settled nose reads (%.3f, %.3f)", k.name, u, r)
		}
		a.Update(k.msg)
		settlePort(t, w)
		u, r := misalign(t, w)
		if !approx(u, k.wantU, 0.02) || !approx(r, k.wantR, 0.02) {
			t.Errorf("%s: misalign = (%.3f up, %.3f right), want (%.2f, %.2f)", k.name, u, r, k.wantU, k.wantR)
		}
		c := w.ActiveCraft()
		if c.PitchTrim != 0 || c.HeadingTrim != 0 {
			t.Errorf("%s: surface pair moved in PORT: (%v, %v)", k.name, c.PitchTrim, c.HeadingTrim)
		}
	}
}

// Vector lean through the keys: ↑ x3 then ← x2 reads 3° up, 2° left on the
// two axes, and | clears it back to straight.
func TestPortArrowsCombinedLeanThenBarClears(t *testing.T) {
	a := portArrowsApp(t)
	w := a.world
	for i := 0; i < 3; i++ {
		a.Update(keyType(tea.KeyUp))
	}
	for i := 0; i < 2; i++ {
		a.Update(keyType(tea.KeyLeft))
	}
	settlePort(t, w)
	if u, r := misalign(t, w); !approx(u, 3, 0.05) || !approx(r, -2, 0.05) {
		t.Errorf("3 up + 2 left reads (%.3f, %.3f), want (3, -2)", u, r)
	}
	a.Update(keyRunes("|"))
	settlePort(t, w)
	if u, r := misalign(t, w); !approx(u, 0, 0.02) || !approx(r, 0, 0.02) {
		t.Errorf("after | misalign = (%.3f, %.3f), want 0", u, r)
	}
	c := w.ActiveCraft()
	if c.PortPitchTrim != 0 || c.PortYawTrim != 0 {
		t.Errorf("| left the port pair at (%v, %v)", c.PortPitchTrim, c.PortYawTrim)
	}
}

// The letters keep working on top of the lean (ADR 0055 Q3): the lean does
// not swallow the hold keys. And outside PORT the arrows are the surface
// trims of ADR 0052, untouched, with the port pair left alone.
func TestArrowsOutsidePortStaySurfaceTrims(t *testing.T) {
	a := portArrowsApp(t)
	w := a.world
	w.NavMode = sim.NavOrbit
	a.Update(keyType(tea.KeyRight))
	c := w.ActiveCraft()
	if !near(c.PitchTrim, spacecraft.PitchTrimStepRad) {
		t.Errorf("→ outside PORT: PitchTrim = %v, want the 5° step", c.PitchTrim)
	}
	if c.PortPitchTrim != 0 || c.PortYawTrim != 0 {
		t.Errorf("→ outside PORT moved the port pair: (%v, %v)", c.PortPitchTrim, c.PortYawTrim)
	}
}

// Stepping `;` onto PORT zeroes a surface trim set before docking, through
// the real key; stepping on past PORT zeroes the port pair.
func TestSemicolonSwitchesWhichPairIsLive(t *testing.T) {
	a := portArrowsApp(t)
	w := a.world
	c := w.ActiveCraft()
	w.NavMode = sim.NavTarget
	c.PitchTrim, c.HeadingTrim = 0.1, 0.1
	a.Update(keyRunes(";"))
	if w.NavMode != sim.NavPort {
		t.Fatalf("; from TGT -> %v, want PORT", w.NavMode)
	}
	if c.PitchTrim != 0 || c.HeadingTrim != 0 {
		t.Errorf("PORT entered with the surface pair live: (%v, %v)", c.PitchTrim, c.HeadingTrim)
	}
	a.Update(keyType(tea.KeyUp))
	if c.PortPitchTrim == 0 {
		t.Fatal("↑ in PORT did not set the port pair")
	}
	a.Update(keyRunes(";"))
	if w.NavMode == sim.NavPort {
		t.Fatal("; did not leave PORT")
	}
	if c.PortPitchTrim != 0 || c.PortYawTrim != 0 {
		t.Errorf("left PORT with the port pair live: (%v, %v)", c.PortPitchTrim, c.PortYawTrim)
	}
}
