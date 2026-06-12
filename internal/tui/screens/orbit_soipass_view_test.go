package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestViewSOIPassRefitsToPassBody: with ViewSOIPass selected on the node-free
// Moon coast, the orbit view refits the canvas to the SOI-Pass framing — and
// that framing matches what ViewTarget would produce with the Moon targeted,
// proving it reuses TargetViewFraming geometry against the Pass Body (ADR 0019
// F) without ever setting the Target. The render must not panic and the SOI
// PASS chip stays present (#137 acceptance 1+2).
func TestViewSOIPassRefitsToPassBody(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := setupMoonCoast(t, w)

	// SOI-Pass view, no Target set: record the auto-fit scale.
	w.ViewMode = sim.ViewSOIPass
	out := v.Render(w, 0, 200, 60)
	passScale := v.canvas.Scale()
	if w.Target.Kind != sim.TargetNone {
		t.Fatalf("ViewSOIPass mutated the Target slot: %v", w.Target)
	}

	// The same canvas under ViewTarget with the Moon targeted must land on the
	// identical fit — same framing geometry, just sourced from the Pass Body
	// instead of the Target.
	w.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: moonIdx}
	w.ViewMode = sim.ViewTarget
	v.Render(w, 0, 200, 60)
	targetScale := v.canvas.Scale()

	if math.Abs(passScale-targetScale) > 1e-12 {
		t.Errorf("ViewSOIPass scale %v != ViewTarget scale %v; framing geometry should match", passScale, targetScale)
	}
	if !strings.Contains(out, "SOI PASS") {
		t.Errorf("SOI PASS chip should render in ViewSOIPass with no Target set")
	}
}

// TestPlainViewFocusBodyCentersOnEncounter is the screen-level check for the
// "focus on Cursor" playtest path (#144, reshaped by ADR 0021 B): "focus on
// the Moon" in an ordinary view (ViewTilted — not the v-cycle
// ViewTarget/ViewSOIPass). With an upcoming encounter the canvas re-centers
// on the drawn encounter — which now lives Local-to-Body, at the Moon's
// CURRENT position plus the body-relative perilune offset — so the capture
// curve and the Moon's drawn disk are both on-canvas.
func TestPlainViewFocusBodyCentersOnEncounter(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := setupMoonCoast(t, w)
	moon := w.System().Bodies[moonIdx]

	// Ordinary view, focused on the Moon — the plain "look at the body" case.
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.Render(w, 0, 200, 60)

	got := v.canvas.CenterWorld()
	eCenter, _, ok := w.FocusEncounterFraming()
	if !ok {
		t.Fatal("FocusEncounterFraming ok=false on the Moon coast; expected an encounter")
	}
	if d := got.Sub(eCenter).Norm(); d > 1 {
		t.Errorf("canvas centered %.0f km off the encounter frame; the override didn't fire in ViewTilted", d/1e3)
	}
	// And that center is at the Moon — within the parent-relative SOI of its
	// current position, where the Local-to-Body arc is drawn (ADR 0021 B).
	moonParent := w.System().Bodies[0]
	for _, b := range w.System().Bodies {
		if b.ID == moon.ParentID {
			moonParent = b
		}
	}
	soi := physics.SOIRadius(moon, moonParent)
	if d := got.Sub(w.BodyPosition(moon)).Norm(); d > soi {
		t.Errorf("canvas centered %.0f km from the Moon's current position (SOI %.0f km) — not framing the Local-to-Body arc", d/1e3, soi/1e3)
	}
}

// TestViewSOIPassFallsBackWhenPassClears: if the SOI Pass disappears while
// ViewSOIPass is selected (here: the craft drops to a stable LEO with no
// reachable SOI), the view must not leave the camera framing nothing — it
// falls through to the ordinary focus center and still renders (ADR 0019 F /
// #137 acceptance 4, graceful degradation).
func TestViewSOIPassFallsBackWhenPassClears(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	setupMoonCoast(t, w)
	w.ViewMode = sim.ViewSOIPass
	v.Render(w, 0, 200, 60)

	// Collapse to a stable LEO — no SOI Pass any more.
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	if _, _, ok := w.SOIPassViewFraming(); ok {
		t.Fatal("precondition: pass should have cleared on the stable LEO")
	}

	// Still renders without panicking, and falls back rather than staying
	// framed on the now-absent pass.
	out := v.Render(w, 0, 200, 60)
	if strings.TrimSpace(out) == "" {
		t.Error("ViewSOIPass rendered empty after the pass cleared; expected graceful fallback")
	}
}
