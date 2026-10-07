package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The PROXIMITY box says whether RCS is engaged, the pulse step each
// translation key fires, and what's left in the tank (Jason's playtest
// 2026-10-07: "proximity view is lacking some readouts that are helpful,
// like whether rcs is engaged and what level it is set to"). Before, RCS
// on/off showed only as the colour of the navball's RCS button.
func proximityChipText(t *testing.T, w *sim.World) string {
	t.Helper()
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	return ansi.Strip(strings.Join(v.buildProximityChip(w), "\n"))
}

func proximityRCSWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	a := w.ActiveCraftIdx
	if _, err := w.SpawnCraft(sim.SpawnSpec{Alongside: true}); err != nil {
		t.Fatalf("spawn alongside: %v", err)
	}
	w.ActiveCraftIdx = a
	w.SetTargetCraft(len(w.Crafts) - 1)
	w.ViewMode = sim.ViewProximity
	return w
}

func TestProximityBoxShowsRCSStateAndPulseStep(t *testing.T) {
	w := proximityRCSWorld(t)
	box := proximityChipText(t, w)
	if !strings.Contains(box, "rcs:") || !strings.Contains(box, "off") {
		t.Errorf("RCS off: PROXIMITY lacks an `rcs: off` row:\n%s", box)
	}
	w.CycleEngineMode() // r: RCS on
	box = proximityChipText(t, w)
	if strings.Contains(box, "off") || !strings.Contains(box, "pulse 0.1 m/s") {
		t.Errorf("RCS on: PROXIMITY lacks `on` and the 0.1 m/s pulse step:\n%s", box)
	}
	w.CycleRCSPulseScale() // p: 0.01 m/s
	if box = proximityChipText(t, w); !strings.Contains(box, "0.01 m/s") {
		t.Errorf("after p: pulse step 0.01 m/s not shown:\n%s", box)
	}
	w.CycleRCSPulseScale() // p: 0.001 m/s
	if box = proximityChipText(t, w); !strings.Contains(box, "0.001 m/s") {
		t.Errorf("after p p: pulse step 0.001 m/s not shown:\n%s", box)
	}
}

func TestProximityBoxShowsMonopropLeft(t *testing.T) {
	w := proximityRCSWorld(t)
	box := proximityChipText(t, w)
	if !strings.Contains(box, "monoprop:") || !strings.Contains(box, "kg") {
		t.Errorf("PROXIMITY lacks a monoprop: row with the tank mass:\n%s", box)
	}
}

// "add engine readouts too" (Jason, 2026-10-07): the main engine's throttle
// state (idle / FIRING / DRY, as in the ENGINE box) and the Δv it has left.
func TestProximityBoxShowsEngineThrottleAndDeltaV(t *testing.T) {
	w := proximityRCSWorld(t)
	c := w.ActiveCraft()
	c.Throttle = 0.4
	box := proximityChipText(t, w)
	if !strings.Contains(box, "throttle:") || !strings.Contains(box, "40%") || !strings.Contains(box, "idle") {
		t.Errorf("PROXIMITY lacks `throttle: 40%% idle`:\n%s", box)
	}
	want := ansi.Strip(deltaVReadout(c))
	if !strings.Contains(box, "Δv") || !strings.Contains(box, want) {
		t.Errorf("PROXIMITY lacks the main engine's Δv %q:\n%s", want, box)
	}
}
