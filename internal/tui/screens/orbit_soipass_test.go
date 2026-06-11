package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// setupMoonCoast aligns the active craft coplanar with the Moon, plants a
// transfer to it, then drops the craft onto the post-departure coast (live
// state, no maneuver node) — the no-node case the SOI Pass must render. It
// returns the Moon's body index. Mirrors the sim package's
// coplanarLEOTowardMoon, replicated here because that helper isn't visible
// across packages.
func setupMoonCoast(t *testing.T, w *sim.World) int {
	t.Helper()
	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon not in loaded Sol system")
	}
	moon := sys.Bodies[moonIdx]
	mel := orbital.ElementsFromBody(moon)
	sI, cI := math.Sin(mel.I), math.Cos(mel.I)
	sO, cO := math.Sin(mel.Omega), math.Cos(mel.Omega)
	moonN := orbital.Vec3{X: sO * sI, Y: -cO * sI, Z: cI}.Unit()
	ref := orbital.Vec3{X: 1}
	if math.Abs(moonN.Dot(ref)) > 0.9 {
		ref = orbital.Vec3{Y: 1}
	}
	e1 := ref.Sub(moonN.Scale(moonN.Dot(ref))).Unit()
	e2 := moonN.Cross(e1)

	c := w.ActiveCraft()
	c.Landed = false
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	v := math.Sqrt(mu / r)
	c.State.R = e1.Scale(r)
	c.State.V = e2.Scale(v)

	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs after PlanTransfer")
	}
	leg := legs[0]
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	c.Nodes = nil
	return moonIdx
}

func newSOIPassTestView() *OrbitView {
	v := NewOrbitView(Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle(),
	})
	v.Resize(200, 60)
	return v
}

// TestSOIPassChipAppearsCoastingToMoon: coasting on an LEO→Moon transfer
// with no node, the SOI PASS chip renders — the encounter surfaces ahead of
// SOI entry (ADR 0019, #135). (The Perilune marker glyph isn't asserted on
// the rendered string: the navball's prograde marker is also ⊕, so a string
// match can't isolate the canvas marker — the perilune point is covered at
// the sim level by TestLiveSOIPassDetectsMoonPass, and the glyph draw by the
// #134 marker tests.)
func TestSOIPassChipAppearsCoastingToMoon(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	setupMoonCoast(t, w)

	out := v.Render(w, 0, 200, 60)
	if !strings.Contains(out, "SOI PASS") {
		t.Errorf("expected the SOI PASS chip while coasting toward the Moon")
	}
}

// TestSOIPassCachedOnCoast: rendering a coasting craft repeatedly at the
// same clock recomputes the SOI Pass only once — the predict-on-change
// cache holds (ADR 0019 / ADR 0017 C), so the forward predictor stays off
// the per-frame hot path even though both the canvas draw and the chip
// consult it each frame.
func TestSOIPassCachedOnCoast(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	setupMoonCoast(t, w)

	for i := 0; i < 5; i++ {
		v.Render(w, 0, 200, 60)
	}
	if v.soiPassCacheComputes != 1 {
		t.Errorf("soiPassCacheComputes = %d after 5 coast renders, want 1 (predict-on-change cache)", v.soiPassCacheComputes)
	}
}

// TestSOIPassChipDeDupesWithTarget: when the Pass Body is also the current
// body Target, the SOI PASS chip suppresses (the TARGET chip already shows
// perilune/TCA) — basic de-dupe per ADR 0019 E.
func TestSOIPassChipDeDupesWithTarget(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := setupMoonCoast(t, w)

	// Sanity: with no target, the SOI PASS chip is present.
	if out := v.Render(w, 0, 200, 60); !strings.Contains(out, "SOI PASS") {
		t.Fatalf("precondition: SOI PASS chip absent before targeting")
	}

	// Target the Moon — the chips should collapse to just TARGET.
	w.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: moonIdx}
	out := v.Render(w, 0, 200, 60)
	if strings.Contains(out, "SOI PASS") {
		t.Errorf("SOI PASS chip should de-dupe away when the Moon is the Target")
	}
	if !strings.Contains(out, "TARGET") {
		t.Errorf("TARGET chip should remain when the Moon is targeted")
	}
}

// TestSOIPassAbsentForStableLEO: a stable LEO that reaches no sibling SOI
// shows no SOI PASS chip and no Perilune marker (apoapsis-reach guard).
func TestSOIPassAbsentForStableLEO(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = false
	c.Nodes = nil
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}

	out := v.Render(w, 0, 200, 60)
	if strings.Contains(out, "SOI PASS") {
		t.Errorf("stable LEO must not show a SOI PASS chip")
	}
}
