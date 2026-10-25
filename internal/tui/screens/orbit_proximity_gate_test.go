package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// ADR 0053 slice B, the screen half: the ring and the port line read the
// same gate the dock does (port range, relative speed, alignment), not the
// old 50 m / 0.1 m/s pair.

// withRelVelocity sets the active vessel's velocity to the target's plus rel.
func withRelVelocity(w *sim.World, rel orbital.Vec3) *sim.World {
	w.Crafts[0].State.V = w.Crafts[1].State.V.Add(rel)
	return w
}

// latchedPortedWorld docks two ported tugs, undocks them (arming the #343
// re-arm latch), then parks them port to port 0.3 m apart, aligned and at
// rest: a pair the gate would capture were it not latched.
func latchedPortedWorld(t *testing.T) *sim.World {
	t.Helper()
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, 0)
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("fixture dock failed: %d vessels", len(w.Crafts))
	}
	if !w.Undock(0) {
		t.Fatal("Undock refused")
	}
	lead, top := w.Crafts[0], w.Crafts[1]
	axis := lead.CurrentAttitudeDir.Unit()
	top.CurrentAttitudeDir = axis.Scale(-1)
	leadPort := sim.VesselPortGeometry(lead).PortPoint(lead.State.R, 0)
	top.State.R = leadPort.Add(axis.Scale(0.3)).Add(axis.Scale(sim.VesselPortGeometry(top).Ports[0].OffsetM))
	top.State.V = lead.State.V
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	w.ViewMode = sim.ViewProximity
	return w
}

type gateScene struct {
	name  string
	world func(t *testing.T) *sim.World
	want  lipgloss.Color
}

func gateScenes() []gateScene {
	tug, nudge := []string{"rcs-tug"}, []string{"nudge-tug"}
	return []gateScene{
		{"in the gate: 0.3 m, aligned, at rest", func(t *testing.T) *sim.World {
			return portedPairWorld(t, tug, nudge, orbital.Vec3{X: 0.3}, 0)
		}, render.ColorTarget},
		{"lateral pass: 0.05 m/s, port 3 m away", func(t *testing.T) *sim.World {
			return withRelVelocity(portedPairWorld(t, tug, nudge, orbital.Vec3{X: 3}, 0), orbital.Vec3{Z: 0.05})
		}, render.ColorDim},
		{"20 deg misaligned at 0.3 m", func(t *testing.T) *sim.World {
			return portedPairWorld(t, tug, nudge, orbital.Vec3{X: 0.3}, 20)
		}, render.ColorDim},
		{"in position and aligned but too fast (0.3 m/s)", func(t *testing.T) *sim.World {
			return withRelVelocity(portedPairWorld(t, tug, nudge, orbital.Vec3{X: 0.3}, 0), orbital.Vec3{Z: 0.3})
		}, render.ColorAlert},
		{"port-less target at 0.3 m, aligned, at rest", func(t *testing.T) *sim.World {
			return portedPairWorld(t, tug, []string{"s-ivb-1"}, orbital.Vec3{X: 0.3}, 0)
		}, render.ColorDim},
		{"latched but in the gate", latchedPortedWorld, render.ColorWarning},
	}
}

// The ring's colour follows the port gate, term by term, and is green exactly
// when ProximityDockGateReady is: the OLD 50 m / 0.1 m/s pair would have
// painted the lateral pass and the 20 deg miss green.
func TestProximityGateRingColorFollowsThePortGate(t *testing.T) {
	for _, sc := range gateScenes() {
		w := sc.world(t)
		st, ok := w.ProximityState()
		if !ok {
			t.Fatalf("%s: no ProximityState", sc.name)
		}
		got := proximityGateRingColor(w, st)
		if got != sc.want {
			t.Errorf("%s: ring colour %v, want %v", sc.name, got, sc.want)
		}
		if isGreen, ready := got == render.ColorTarget, w.ProximityDockGateReady(st); isGreen != ready {
			t.Errorf("%s: ring green=%v but ProximityDockGateReady=%v", sc.name, isGreen, ready)
		}
	}
}

// The colour reaches the canvas: drawing the ring alone on a cleared canvas
// leaves cells of the expected colour and none of the others, so a state shown only in a helper
// cannot pass.
func TestProximityGateRingColourReachesTheCanvas(t *testing.T) {
	colours := []lipgloss.Color{render.ColorTarget, render.ColorWarning, render.ColorAlert, render.ColorDim}
	for _, sc := range gateScenes() {
		w := sc.world(t)
		v := newProximityTestView(t, DesignWidth, DesignHeight)
		v.Render(w, 0, DesignWidth, DesignHeight)
		st, ok := w.ProximityState()
		if !ok {
			t.Fatalf("%s: no ProximityState", sc.name)
		}
		if !v.proximityRingVisible(st, sim.DockingDistM) {
			t.Fatalf("%s: precondition: the 50 m ring is not on screen", sc.name)
		}
		// Clear the frame so only the ring is on the canvas, then draw it.
		v.canvas.Clear()
		v.drawProximityDockGate(w, st)
		for _, c := range colours {
			n := v.canvas.CountColor(c)
			if c == sc.want && n <= 0 {
				t.Errorf("%s: the ring drew no %v cells", sc.name, c)
			}
			if c != sc.want && n != 0 {
				t.Errorf("%s: the ring drew %d %v cells, want only %v", sc.name, n, c, sc.want)
			}
		}
	}
}

// The port line is dashed until the gate holds, solid after.
func TestProximityPortLineSolidOnlyWhenGateReady(t *testing.T) {
	for _, sc := range gateScenes() {
		w := sc.world(t)
		st, ok := w.ProximityState()
		if !ok {
			t.Fatalf("%s: no ProximityState", sc.name)
		}
		want := widgets.ClassPlanned
		if sc.want == render.ColorTarget {
			want = widgets.ClassReal
		}
		if got := proximityPortLineClass(w, st); got != want {
			t.Errorf("%s: port line class %v, want %v", sc.name, got, want)
		}
	}
}

// While the magnet pulls, the port: row reads "capturing" in place of the
// gate reminder; before it, and after it lets go, it does not.
func TestProximityPortRowCapturingWhileTheMagnetPulls(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, 0)
	// The fixture's nose points retrograde and SAS would slew it prograde at
	// 10 deg/s, moving the port faster than the gate allows (ADR 0055 slice
	// 0). Hold the nose so the gate meets a parked pair.
	w.Crafts[0].SlewRateDegPerSec = 1e-9
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	if row := chipLine(v.buildProximityChip(w), "port:"); strings.Contains(row, "capturing") {
		t.Fatalf("port row says capturing before any tick: %q", row)
	}
	w.Tick() // the gate holds: the magnet starts
	row := chipLine(v.buildProximityChip(w), "port:")
	if !strings.Contains(row, "capturing") {
		t.Fatalf("port row = %q while the magnet pulls, want it to say capturing", row)
	}
	if strings.Contains(row, "need <") {
		t.Errorf("port row %q carries a gate reminder during the pull", row)
	}
	compact := newProximityTestView(t, 80, 24).buildProximityChipCompact(w)
	if row := chipLine(compact, "port:"); !strings.Contains(row, "capturing") {
		t.Errorf("Compact Form lost the capturing row: %q", compact)
	}
	// Pull the other vessel out of the gate: the magnet lets go and the row
	// goes back to the numbers.
	w.Crafts[1].State.V = w.Crafts[1].State.V.Add(orbital.Vec3{Z: 0.5})
	w.Tick()
	if row := chipLine(v.buildProximityChip(w), "port:"); strings.Contains(row, "capturing") {
		t.Errorf("port row still says capturing after the target left the gate: %q", row)
	}
}

// The TARGET box's DOCK READY badge is the same gate the ring and the dock
// read, not the old "under 50 m and 0.1 m/s": a port-less pair at rest 30 m
// apart, a lateral pass and a 20 deg miss all used to light it.
func TestTargetBoxDockReadyFollowsThePortGate(t *testing.T) {
	for _, sc := range gateScenes() {
		w := sc.world(t)
		w.ViewMode = sim.ViewTilted
		v := NewOrbitView(chipTestTheme())
		v.Resize(DesignWidth, DesignHeight)
		got := strings.Contains(strings.Join(v.buildTargetBox(w), "\n"), "DOCK READY")
		if want := sc.want == render.ColorTarget; got != want {
			t.Errorf("%s: TARGET badge DOCK READY = %v, want %v", sc.name, got, want)
		}
	}
	// A port-less pair 30 m apart at rest: the old badge's own case.
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"s-ivb-1"}, orbital.Vec3{X: 30}, 0)
	w.ViewMode = sim.ViewTilted
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	if strings.Contains(strings.Join(v.buildTargetBox(w), "\n"), "DOCK READY") {
		t.Error("a port-less pair 30 m apart reads DOCK READY")
	}
}

// #794: while the re-arm latch holds the pair, the chip carries a standing
// "latched: back 5 m or alt+u" under the port: row, whole at Design Size; a
// pair that is not latched carries none, and neither does one after alt+u.
func TestProximityChipStandingLatchedLine(t *testing.T) {
	const want = "latched: back 5 m or alt+u"
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	for _, sc := range gateScenes() {
		w := sc.world(t)
		chip := v.buildProximityChip(w)
		got := strings.Contains(strings.Join(chip, "\n"), want)
		if wantIt := sc.name == "latched but in the gate"; got != wantIt {
			t.Errorf("%s: latched line present = %v, want %v", sc.name, got, wantIt)
		}
		for _, l := range chip {
			if lipgloss.Width(l) > proximityChipInnerWidth {
				t.Errorf("%s: row %q is wider than %d cells", sc.name, l, proximityChipInnerWidth)
			}
		}
	}
	w := latchedPortedWorld(t)
	if _, ok := w.ReArmDocking(); !ok {
		t.Fatal("alt+u found no latch")
	}
	if strings.Contains(strings.Join(v.buildProximityChip(w), "\n"), want) {
		t.Error("the latched line stayed after alt+u cleared the latch")
	}
}
