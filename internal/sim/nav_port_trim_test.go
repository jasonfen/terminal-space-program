package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0055 decision 4 / "Settled here" 2: one trim pair is live at a time.
// Stepping onto PORT zeroes the surface pair; leaving PORT (by `;` or by
// reconcileNavMode dropping it) zeroes the port pair. Sabotage: both pairs
// live (no zeroing) fails every case.
func TestEnteringPortZeroesSurfacePairLeavingZeroesPortPair(t *testing.T) {
	w := portPairWorld(t)
	c := w.ActiveCraft()
	w.NavMode = NavTarget
	c.PitchTrim, c.HeadingTrim = 0.1, -0.2
	if got := w.CycleNavMode(); got != NavPort {
		t.Fatalf("Target -> %v, want PORT", got)
	}
	if c.PitchTrim != 0 || c.HeadingTrim != 0 {
		t.Errorf("entering PORT left the surface pair live: (%v, %v)", c.PitchTrim, c.HeadingTrim)
	}
	if c.PortPitchTrim != 0 || c.PortYawTrim != 0 {
		t.Errorf("entering PORT: port pair = (%v, %v), want 0", c.PortPitchTrim, c.PortYawTrim)
	}

	c.PortPitchTrim, c.PortYawTrim = 0.05, -0.03
	if got := w.CycleNavMode(); got != NavSurface {
		t.Fatalf("Port -> %v, want SURF", got)
	}
	if c.PortPitchTrim != 0 || c.PortYawTrim != 0 {
		t.Errorf("leaving PORT by ; left the port pair live: (%v, %v)", c.PortPitchTrim, c.PortYawTrim)
	}
}

func TestReconcileDroppingPortZeroesPortPair(t *testing.T) {
	w := portPairWorld(t)
	c := w.ActiveCraft()
	w.NavMode = NavPort
	c.PortPitchTrim, c.PortYawTrim = 0.05, -0.03
	// The target loses its port (not its identity): reconcileNavMode alone drops PORT.
	w.Crafts[1].Stages = []spacecraft.Stage{stageOf(t, "s-ivb-1")}
	w.Crafts[1].SyncFields()
	w.reconcileNavMode()
	if w.NavMode == NavPort {
		t.Fatal("PORT survived losing the target")
	}
	if c.PortPitchTrim != 0 || c.PortYawTrim != 0 {
		t.Errorf("PORT dropped but the port pair is (%v, %v)", c.PortPitchTrim, c.PortYawTrim)
	}
}

// A trim set in one hold's frame must not follow the player to another
// target while PORT stays up.
func TestPortPairDoesNotSurviveATargetChange(t *testing.T) {
	w := portPairWorld(t)
	c := w.ActiveCraft()
	w.NavMode = NavPort
	c.PortPitchTrim = 0.05
	w.SetTargetCraft(1) // same target: nothing changes
	if c.PortPitchTrim != 0.05 {
		t.Fatalf("re-selecting the same target zeroed the trim: %v", c.PortPitchTrim)
	}
	third := *w.Crafts[1] // a second ported vessel to retarget onto
	third.ID = 0
	w.Crafts = append(w.Crafts, &third)
	w.stampCraftID(&third)
	w.SetTargetCraft(2)
	if w.NavMode != NavPort {
		t.Fatalf("fixture: retargeting dropped PORT (%v), the test would pass for the wrong reason", w.NavMode)
	}
	if c.PortPitchTrim != 0 {
		t.Errorf("target changed but port pair = %v", c.PortPitchTrim)
	}
}

// ADR 0055 "Not verified": the magnet ignores the trim pair. A pull begun
// with a lean on the pair still lands the nose exactly on -axis; sabotage:
// let the slew chase the leaned hold during the pull (the old behaviour of
// any attitude command) and a residual lean stays on the nose.
func TestDockMagnetIgnoresPortTrimPair(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	p.w.SetTargetCraft(1)
	p.w.NavMode = NavPort
	p.a.AttitudeMode = spacecraft.BurnPortIn
	p.a.PortPitchTrim = 1 * math.Pi / 180
	p.a.PortYawTrim = -1 * math.Pi / 180
	tickFor(p.w, 0.5)
	if p.w.dockMagnet == nil {
		t.Skip("gate did not hold with the lean on; fixture needs a smaller lean")
	}
	tickFor(p.w, 4.0)
	if p.w.dockMagnet == nil {
		t.Fatal("magnet let go: the trim pair pulled the pair out of the gate")
	}
	st, ok := p.w.ProximityState()
	if !ok {
		t.Fatal("no proximity state mid-pull")
	}
	if st.AlignDeg > 0.2 {
		t.Errorf("4.5 s into the pull the noses are %.3f deg apart; the pull should have closed it (the trim pair must not fight it)", st.AlignDeg)
	}
	tickFor(p.w, 1.0)
	if len(p.w.Crafts) != 1 {
		t.Errorf("slate = %d, want the dock to complete with the trim pair set", len(p.w.Crafts))
	}
}
