package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// A composite's free ports exclude the ports consumed at a dock seam, and the
// partner's engine end never carries one (review finding 2). With one nose
// port per vessel, a nose-to-nose pair has no free port at all.
func TestCompositeAfterNoseToNoseDockHasNoFreePort(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
	p.w.DockCrafts(0, 1)
	if len(p.w.Crafts) != 1 {
		t.Fatalf("DockCrafts left %d vessels", len(p.w.Crafts))
	}
	c := p.w.Crafts[0]
	if g := VesselPortGeometry(c); g.HasPort() {
		t.Errorf("docked pair reports a free port %+v (the partner's engine end)", g.Ports)
	}
	// A third tug parked port to the composite's far end must not dock there.
	third := spacecraft.NewFromStages([]spacecraft.Stage{stageOf(t, "rcs-tug")})
	third.Primary, third.SystemIdx = c.Primary, c.SystemIdx
	gc, gt := VesselPortGeometry(c), VesselPortGeometry(third)
	third.CurrentAttitudeDir = c.CurrentAttitudeDir.Scale(-1)
	far := c.State.R.Add(gc.Axis.Scale(gc.NoseM - dockPortTipInsetM + 0.3))
	third.State.R = far.Sub(third.CurrentAttitudeDir.Scale(gt.Ports[0].OffsetM))
	third.State.V = c.State.V
	third.State.M = third.TotalMass()
	if dockPairGateHolds(c, third) {
		t.Error("the dock gate holds against a port on the partner's engine bell")
	}
}

// After `space` + `D` the Apollo CSM and LM are a composite: the LM descent
// engine is not a port.
func TestTransposedApolloCompositeHasNoFreePort(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	crewTendActive(w)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	if err := w.Transpose(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	if g := VesselPortGeometry(c); g.HasPort() {
		t.Errorf("transposed CSM+LM reports a free port %+v", g.Ports)
	}
}

// A lone vessel still exposes its top stage's port, and gets it back when the
// partner leaves.
func TestUndockedVesselsRegainTheirPorts(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
	p.w.DockCrafts(0, 1)
	if !p.w.Undock(0) {
		t.Fatal("undock refused")
	}
	for i, c := range p.w.Crafts {
		if !VesselPortGeometry(c).HasPort() {
			t.Errorf("vessel %d (%s) has no port after the undock", i, c.Name)
		}
	}
}

// Pieces come back 0.6 m apart whatever the stages at the seam (one separator
// row, or two where both are tall enough for the sprite's taper row, used to
// make it 1.5 or 3 m). Measured port to port right after the undock, from the
// two ports' positions, and just outside the 0.5 m port gate.
func TestUndockPortGapIsTheDockSeamGap(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"rcs-tug", "nudge-tug"}, // one separator row at the seam
		{"ntr-tug", "ntr-tug"},   // taper plus separator
	} {
		p := newPortPair(t, c.a, c.b, 0.3, 0, 0)
		p.w.DockCrafts(0, 1)
		if !p.w.Undock(0) || len(p.w.Crafts) != 2 {
			t.Fatalf("%s+%s: undock failed", c.a, c.b)
		}
		lo, hi := p.w.Crafts[0], p.w.Crafts[1]
		gl, gh := VesselPortGeometry(lo), VesselPortGeometry(hi)
		got := gh.PortPoint(hi.State.R, 0).Sub(gl.PortPoint(lo.State.R, 0)).Norm()
		if !near(got, 0.6, 0.01) {
			t.Errorf("%s+%s: ports %.3f m apart after undock, want 0.6", c.a, c.b, got)
		}
		if got <= DockPortRangeM {
			t.Errorf("%s+%s: ports %.3f m apart sit inside the %.1f m gate", c.a, c.b, got, DockPortRangeM)
		}
	}
}
