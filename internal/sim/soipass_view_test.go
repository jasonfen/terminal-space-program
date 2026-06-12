package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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

// TestSOIPassViewFramingFramesEncounter: on the node-free LEO→Moon coast the
// SOI-Pass framing centers on the drawn encounter — the pass's PerilunePoint, a
// sample on the foreign-SOI arc at the Moon's *arrival* location — not the
// Moon's *current* position (ADR 0019 F, #137; #144). The two diverge by far
// more than the SOI over a multi-day coast (the Earth–Moon system translates
// along Earth's heliocentric orbit), so centering on the current position pushes
// the encounter off-canvas — the bug this regression pins.
func TestSOIPassViewFramingFramesEncounter(t *testing.T) {
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
	if got := center.Sub(pass.PerilunePoint).Norm(); got > 1 {
		t.Errorf("framing center is %v m from the drawn PerilunePoint, want centered on it", got)
	}
	current := w.BodyPosition(pass.Body)
	// The encounter sits a whole transit-translation away from the Moon's
	// current position — far more than the Moon's true SOI.
	soiVsParent := physics.SOIRadius(pass.Body, parentBody(w, pass.Body))
	if got := center.Sub(current).Norm(); got <= soiVsParent {
		t.Fatalf("encounter only %v m from the Moon's current position (SOI %v m) — #144 repro premise stale", got, soiVsParent)
	}
}

// parentBody returns the body p orbits (its ParentID), or the system root when
// p has no parent in the catalog. Test helper for SOI-vs-parent checks.
func parentBody(w *World, p bodies.CelestialBody) bodies.CelestialBody {
	for _, b := range w.System().Bodies {
		if b.ID == p.ParentID {
			return b
		}
	}
	return w.System().Bodies[0]
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

// TestSOIPassViewFramingFitsToArcExtent: once an encounter resolves, the fit
// frames the *drawn arc's* extent — every foreign-SOI arc sample lands within
// the radius — so the whole capture curve fills the canvas. In the system frame
// the body translates through the pass, smearing the in-SOI hyperbola across
// many times the SOI, so an SOI-sized fit would frame a single point (issue
// #144). The fit is also *not* widened to the craft→encounter distance: the
// craft is a whole transfer away, and widening to it would shrink the arc back
// to a dot.
func TestSOIPassViewFramingFitsToArcExtent(t *testing.T) {
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

	// Every drawn arc sample is within the fit radius — the curve fills the
	// canvas rather than overflowing it.
	for _, s := range pass.ArcSegments {
		for _, p := range s.Points {
			if d := p.Sub(center).Norm(); d > radius {
				t.Errorf("arc sample %.0f km from center exceeds fit radius %.0f km — capture curve overflows the canvas", d/1e3, radius/1e3)
			}
		}
	}
	// And the fit is the arc's extent, not the craft→encounter distance: the
	// distant craft is intentionally left off-canvas so the curve fills it.
	if dist := w.CraftInertial().Sub(center).Norm(); radius >= dist {
		t.Errorf("fit radius %.0f km ≥ craft→encounter distance %.0f km: framing widened to include the distant craft (pre-#144 behavior)", radius/1e3, dist/1e3)
	}
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
