package screens

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// moonIdxOf returns the Moon's body index in the active system (skipping the
// test when absent) — shared lookup for the Camera Contract tests below.
func moonIdxOf(t *testing.T, w *sim.World) int {
	t.Helper()
	for i, b := range w.System().Bodies {
		if b.EnglishName == "Moon" {
			return i
		}
	}
	t.Skip("Moon not in loaded Sol system")
	return -1
}

// TestPassAppearingLeavesCameraUntouched pins the Camera Contract (ADR 0021
// A): a SOI Pass appearing mid-coast is ambient sim state, not a Framing
// Event — the canvas scale stays exactly where the last Framing Event put it,
// and the center keeps tracking the focused body (what Focus means) rather
// than jumping to any encounter geometry.
func TestPassAppearingLeavesCameraUntouched(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := moonIdxOf(t, w)
	moon := w.System().Bodies[moonIdx]

	// Steady focus on the Moon from a stable LEO — no pass anywhere.
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.Render(w, 0, 200, 60)
	scaleBefore := v.canvas.Scale()

	// A pass appears mid-coast (setupMoonCoast drops the craft onto the
	// LEO→Moon coast; focus / view / system are untouched).
	setupMoonCoast(t, w)
	if _, ok := w.LiveSOIPass(); !ok {
		t.Fatal("precondition: no live SOI Pass after setupMoonCoast")
	}
	v.Render(w, 0, 200, 60)

	if got := v.canvas.Scale(); got != scaleBefore {
		t.Errorf("pass appearing moved the scale: %.6e -> %.6e (contract: ambient sim state never re-fits)", scaleBefore, got)
	}
	if d := v.canvas.CenterWorld().Sub(w.BodyPosition(moon)).Norm(); d > 1 {
		t.Errorf("center is %.0f km off the focused Moon — focus tracking broke or an encounter framer re-centered", d/1e3)
	}
}

// TestManualZoomPersistsOnSteadyFocus pins the player-owned zoom half of the
// contract: `+`/`-` compose over the Framing-Event base scale and hold
// indefinitely on a steady focus — across renders and across ambient
// sim-state changes (here the clock advancing under an active pass).
func TestManualZoomPersistsOnSteadyFocus(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := setupMoonCoast(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.Render(w, 0, 200, 60)
	base := v.canvas.Scale()

	v.ZoomIn()
	v.ZoomIn()
	v.Render(w, 0, 200, 60)
	zoomed := v.canvas.Scale()
	if zoomed <= base {
		t.Fatalf("ZoomIn did not increase scale (base=%.3e zoomed=%.3e)", base, zoomed)
	}

	// Holds across further renders and an ambient clock advance.
	v.Render(w, 0, 200, 60)
	w.Clock.SimTime = w.Clock.SimTime.Add(10 * time.Minute)
	v.Render(w, 0, 200, 60)
	if held := v.canvas.Scale(); math.Abs(held-zoomed) > zoomed*1e-9 {
		t.Errorf("manual zoom drifted on a steady focus: zoomed=%.3e held=%.3e", zoomed, held)
	}
}

// TestFramingEventResetsZoom: userZoom resets at the next Framing Event — a
// Focus change and a ViewMode change each re-fit once and drop the manual
// multiplier (ADR 0021 A: fresh framing context, fresh zoom).
func TestFramingEventResetsZoom(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := moonIdxOf(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.Render(w, 0, 200, 60)

	v.ZoomIn()
	v.Render(w, 0, 200, 60)
	if v.userZoom == 1 {
		t.Fatal("precondition: userZoom should be off 1 after ZoomIn")
	}

	// Focus change → Framing Event → zoom reset.
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v.Render(w, 0, 200, 60)
	if v.userZoom != 1 {
		t.Errorf("focus change did not reset userZoom: %v", v.userZoom)
	}

	// ViewMode change → Framing Event → zoom reset.
	v.ZoomIn()
	v.Render(w, 0, 200, 60)
	w.CycleViewMode()
	v.Render(w, 0, 200, 60)
	if v.userZoom != 1 {
		t.Errorf("view-mode change did not reset userZoom: %v", v.userZoom)
	}
}

// TestFocusBodyWithPassFitsToParentSOI is the screen-level pin for ADR 0021
// F: focusing a body with an active SOI Pass fits the canvas to ~1.3× its
// parent-relative SOI (ring + arc + markers in frame); the same focus with
// no pass keeps the ordinary terminal-body close-up (8× body radius).
func TestFocusBodyWithPassFitsToParentSOI(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := setupMoonCoast(t, w)
	moon := w.System().Bodies[moonIdx]
	moonParent := w.System().Bodies[0]
	for _, b := range w.System().Bodies {
		if b.ID == moon.ParentID {
			moonParent = b
		}
	}
	soi := physics.SOIRadius(moon, moonParent)

	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.Render(w, 0, 200, 60)
	got := v.canvas.Scale()
	v.canvas.FitTo(soi * 1.3)
	if want := v.canvas.Scale(); math.Abs(got-want) > want*1e-9 {
		t.Errorf("with active pass: scale %.6e, want the 1.3× parent-SOI fit %.6e", got, want)
	}

	// No pass: a fresh view + world focused on the same body keeps the
	// pre-existing terminal-body zoom rule.
	v2 := newSOIPassTestView()
	w2, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w2.ViewMode = sim.ViewTilted
	w2.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v2.Render(w2, 0, 200, 60)
	got = v2.canvas.Scale()
	v2.canvas.FitTo(moon.RadiusMeters() * 8)
	if want := v2.canvas.Scale(); math.Abs(got-want) > want*1e-9 {
		t.Errorf("without pass: scale %.6e, want the 8×-radius close-up fit %.6e", got, want)
	}
}

// TestFocusBodyZoomsToTextureVisible pins the ADR 0024 surface-viewing
// floor: focusing a body (without an active SOI pass) zooms it in far
// enough that its disk renders the data-driven surface texture instead
// of the flat placeholder. A planet-with-moons previously framed its
// whole SOI, leaving the planet sub-pixel; the floor guarantees its
// pixel radius clears render.BodyTextureMinRadius.
func TestFocusBodyZoomsToTextureVisible(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Earth has a moon (Luna), so the pre-0024 default fit framed its
	// SOI — the case that used to render the placeholder disk.
	earthIdx, earth := -1, w.System().Bodies[0]
	for i, b := range w.System().Bodies {
		if b.ID == "earth" {
			earthIdx, earth = i, b
		}
	}
	if earthIdx < 0 {
		t.Skip("Earth not in loaded Sol system")
	}
	v := newSOIPassTestView()
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: earthIdx}
	v.Render(w, 0, 200, 60)
	pxR := int(math.Round(earth.RadiusMeters() * v.canvas.Scale()))
	if pxR < render.BodyTextureMinRadius {
		t.Errorf("focused Earth pixel radius %d < texture threshold %d — would render the placeholder disk", pxR, render.BodyTextureMinRadius)
	}
}

// TestFocusChangeMidBurnFitsOnceThenRefreezes pins the ADR 0021 watch-point:
// the burn-frozen center carve-out answers the player's ignition, and a
// Framing Event during the burn re-frames once — new fit, center re-captured
// at the new focus — then freezes again so the burn stays readable.
func TestFocusChangeMidBurnFitsOnceThenRefreezes(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := moonIdxOf(t, w)
	moon := w.System().Bodies[moonIdx]
	c := w.ActiveCraft()

	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v.Render(w, 0, 200, 60)

	// Light the engine: the center freezes at the craft's position.
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 500,
		EndTime:     w.Clock.SimTime.Add(time.Hour),
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
	}
	v.Render(w, 0, 200, 60)
	frozen := v.canvas.CenterWorld()
	c.State.R = c.State.R.Add(orbital.Vec3{Y: 2e6}) // craft sweeps on
	v.Render(w, 0, 200, 60)
	if d := v.canvas.CenterWorld().Sub(frozen).Norm(); d > 1 {
		t.Fatalf("burn-frozen center moved %.0f km while the burn ran", d/1e3)
	}

	// Focus change mid-burn: one fit at the new focus...
	scaleBefore := v.canvas.Scale()
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.Render(w, 0, 200, 60)
	refrozen := v.canvas.CenterWorld()
	if d := refrozen.Sub(w.BodyPosition(moon)).Norm(); d > 1 {
		t.Errorf("mid-burn focus change did not re-center on the new focus (off by %.0f km)", d/1e3)
	}
	if v.canvas.Scale() == scaleBefore {
		t.Logf("note: pre- and post-event scales coincide; fit-once not discriminated by scale")
	}

	// ...then the center re-freezes: the Moon moves on, the camera doesn't.
	w.Clock.SimTime = w.Clock.SimTime.Add(12 * time.Hour)
	if moved := w.BodyPosition(moon).Sub(refrozen).Norm(); moved < 1e6 {
		t.Fatalf("precondition: Moon moved only %.0f km in 12h; cannot discriminate re-freeze", moved/1e3)
	}
	v.Render(w, 0, 200, 60)
	if d := v.canvas.CenterWorld().Sub(refrozen).Norm(); d > 1 {
		t.Errorf("center drifted %.0f km after the mid-burn Framing Event — burn freeze did not re-engage", d/1e3)
	}
}
