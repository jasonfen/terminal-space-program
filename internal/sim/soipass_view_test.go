package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// moonCoast puts the active craft on the node-free LEO→Moon coast where
// LiveSOIPass returns ok with Body.ID == "moon" — the deterministic SOI-Pass
// setup described in the predict_test helper's docstring.
func moonCoast(t *testing.T, w *World) {
	t.Helper()
	leg := coplanarLEOTowardMoon(t, w)
	c := w.ActiveCraft()
	c.State = leg.State
	c.Primary = leg.Primary
	w.Clock.SimTime = leg.StartClock
	c.Nodes = nil
	c.Landed = false
}

// TestSOIPassViewFramingFramesPassBody: on the node-free LEO→Moon coast the
// SOI-Pass framing centers on the Pass Body (the Moon), not the system origin,
// and fits a positive radius — so ViewSOIPass has real geometry to frame
// (ADR 0019 F, #137).
func TestSOIPassViewFramingFramesPassBody(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w)

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("precondition: no live SOI Pass on the Moon coast")
	}

	center, radius, ok := w.SOIPassViewFraming()
	if !ok {
		t.Fatal("SOIPassViewFraming returned ok=false with an active pass")
	}
	if radius <= 0 {
		t.Errorf("fit radius = %v, want > 0", radius)
	}
	moonPos := w.BodyPosition(pass.Body)
	if got := center.Sub(moonPos).Norm(); got > 1 {
		t.Errorf("framing center is %v m from the Pass Body, want centered on it", got)
	}
}

// TestSOIPassViewFramingNilWithoutPass: with no active SOI Pass (a stable LEO)
// the framing reports ok=false, so ViewSOIPass falls through to the ordinary
// focus center instead of framing nothing.
func TestSOIPassViewFramingNilWithoutPass(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.Landed = false
	c.Nodes = nil
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}

	if _, ok := w.LiveSOIPass(); ok {
		t.Fatal("precondition: stable LEO should have no SOI Pass")
	}
	if _, _, ok := w.SOIPassViewFraming(); ok {
		t.Error("SOIPassViewFraming should report ok=false without a pass")
	}
}

// TestSOIPassViewFramingWidensForApproach: when the craft is still far outside
// the Pass Body's SOI, the fit radius widens to the craft→Body distance so the
// craft stays on-canvas during approach — the ADR 0019 F small-SOI watch-point
// (the same widening TargetViewFraming applies).
func TestSOIPassViewFramingWidensForApproach(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w)

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("precondition: no live SOI Pass on the Moon coast")
	}
	center, radius, ok := w.SOIPassViewFraming()
	if !ok {
		t.Fatal("SOIPassViewFraming returned ok=false with an active pass")
	}

	// Early on the LEO→Moon coast the craft is far outside Luna's SOI, so the
	// fit must have widened to at least the craft→Moon distance.
	dist := w.CraftInertial().Sub(center).Norm()
	if dist <= 0 {
		t.Fatal("craft→Body distance is non-positive; setup is degenerate")
	}
	if radius < dist {
		t.Errorf("fit radius %v < craft→Body distance %v: craft would fall off-canvas during approach", radius, dist)
	}
	_ = pass
}

// TestSOIPassViewSelectableInCycle: the `v` cycle offers ViewSOIPass only when
// a pass is active (LiveSOIPass ok), and skips it otherwise — so a player who
// isn't heading into a SOI never lands on a dead view (ADR 0019 F).
func TestSOIPassViewSelectableInCycle(t *testing.T) {
	w := mustWorld(t)

	// Stable LEO: no pass → ViewSOIPass not selectable.
	c := w.ActiveCraft()
	c.Landed = false
	c.Nodes = nil
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	if w.viewModeSelectable(ViewSOIPass) {
		t.Error("ViewSOIPass should be unselectable on a stable LEO (no pass)")
	}

	// Moon coast: pass active → ViewSOIPass selectable, and a full forward
	// cycle reaches it.
	moonCoast(t, w)
	if !w.viewModeSelectable(ViewSOIPass) {
		t.Fatal("ViewSOIPass should be selectable while coasting toward the Moon")
	}
	reached := false
	w.ViewMode = ViewTilted
	for i := 0; i < len(AllViewModes); i++ {
		w.CycleViewMode()
		if w.ViewMode == ViewSOIPass {
			reached = true
			break
		}
	}
	if !reached {
		t.Error("cycling v never reached ViewSOIPass with an active pass")
	}
}

// TestSOIPassViewDoesNotMutateTarget: entering and leaving ViewSOIPass via the
// `v` cycle never touches w.Target — the SOI Pass is Target-independent (ADR
// 0019 F, acceptance criterion 3, #137).
func TestSOIPassViewDoesNotMutateTarget(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w)

	if w.Target.Kind != TargetNone {
		t.Fatalf("precondition: Target should be unset, got %v", w.Target)
	}
	before := w.Target

	// Cycle all the way around through ViewSOIPass and back; Target must be
	// untouched at every step.
	w.ViewMode = ViewTilted
	sawPass := false
	for i := 0; i < 2*len(AllViewModes); i++ {
		w.CycleViewMode()
		if w.ViewMode == ViewSOIPass {
			sawPass = true
		}
		if w.Target != before {
			t.Fatalf("cycling view mutated Target: %v -> %v", before, w.Target)
		}
	}
	if !sawPass {
		t.Error("cycle never visited ViewSOIPass; the no-mutate guarantee was not exercised")
	}
}
