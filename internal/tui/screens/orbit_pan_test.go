package screens

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestPanDisplacesCenterFromFocus pins the core Pan mechanic (ADR 0042,
// CONTEXT.md "Pan"): a press nudges panOffset by a nonzero world-space
// amount, and Render composes it onto FocusPosition() every frame — the
// center is not equal to the raw focus position once panned.
func TestPanDisplacesCenterFromFocus(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v.Render(w, 0, 200, 60)
	if d := v.canvas.CenterWorld().Sub(w.FocusPosition()).Norm(); d > 1 {
		t.Fatalf("precondition: center should track focus exactly before any pan (off by %.3f m)", d)
	}

	v.PanRight()
	v.Render(w, 0, 200, 60)
	offRight := v.canvas.CenterWorld().Sub(w.FocusPosition())
	if offRight.Norm() < 1 {
		t.Fatalf("PanRight did not displace the center from the focus (offset %.3f m)", offRight.Norm())
	}

	v.PanUp()
	v.Render(w, 0, 200, 60)
	offBoth := v.canvas.CenterWorld().Sub(w.FocusPosition())
	if offBoth.Norm() <= offRight.Norm() {
		t.Errorf("PanUp on top of PanRight should grow the offset further: right=%.3f both=%.3f", offRight.Norm(), offBoth.Norm())
	}
}

// TestPanContinuesTrackingMovingFocus pins the "per-frame center tracking
// continues, just displaced" half of the ADR 0042 decision: once panned, the
// offset is a constant delta added to FocusPosition() every frame, so the
// camera keeps following a moving Focus rather than freezing in place.
func TestPanContinuesTrackingMovingFocus(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := setupMoonCoast(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.Render(w, 0, 200, 60)

	v.PanRight()
	v.PanRight()
	v.Render(w, 0, 200, 60)
	offset1 := v.canvas.CenterWorld().Sub(w.FocusPosition())

	// Advance the clock so the Moon moves to a new position — no Framing
	// Event (Focus/ViewMode/System all unchanged), so the pan offset must
	// survive and the center must still be exactly focus+offset at the new
	// focus position, not the stale pre-move center.
	moon := w.System().Bodies[moonIdx]
	before := w.BodyPosition(moon)
	w.Clock.SimTime = w.Clock.SimTime.Add(6 * time.Hour)
	after := w.BodyPosition(moon)
	if after.Sub(before).Norm() < 1e6 {
		t.Fatalf("precondition: Moon moved only %.0f km in 6h; cannot discriminate tracking", after.Sub(before).Norm()/1e3)
	}

	v.Render(w, 0, 200, 60)
	offset2 := v.canvas.CenterWorld().Sub(w.FocusPosition())
	if d := offset2.Sub(offset1).Norm(); d > 1 {
		t.Errorf("pan offset drifted across a focus move with no Framing Event: %.3f -> %.3f (delta %.3f m)", offset1.Norm(), offset2.Norm(), d)
	}
	if d := v.canvas.CenterWorld().Sub(after).Norm(); d < 1 {
		t.Errorf("center did not track the Moon's new position — pan pinned the camera instead of displacing it")
	}
}

// TestPanClearsOnFocusChange pins "`g` or any refocus clears the offset":
// a Focus change is a Framing Event, and OrbitView.Render already resets
// userZoom there — panOffset must reset the same way, snapping the center
// exactly onto the new focus with no leftover displacement.
func TestPanClearsOnFocusChange(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := moonIdxOf(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v.Render(w, 0, 200, 60)

	v.PanLeft()
	v.PanDown()
	v.Render(w, 0, 200, 60)
	if v.panOffset == (orbital.Vec3{}) {
		t.Fatal("precondition: panOffset should be nonzero after PanLeft+PanDown")
	}

	// FocusReset (`g`) sets Focus to FocusSystem — a plain Focus change,
	// same trigger a refocus onto a body or vessel would hit.
	w.Focus = sim.Focus{Kind: sim.FocusSystem}
	v.Render(w, 0, 200, 60)

	if v.panOffset != (orbital.Vec3{}) {
		t.Errorf("panOffset survived a Focus change: %+v, want zero", v.panOffset)
	}
	if d := v.canvas.CenterWorld().Sub(w.FocusPosition()).Norm(); d > 1 {
		t.Errorf("center is %.3f m off the new focus after refocus — offset did not fully clear", d)
	}

	// A second refocus onto the Moon from a starting pan must also clear.
	v.PanUp()
	v.Render(w, 0, 200, 60)
	if v.panOffset == (orbital.Vec3{}) {
		t.Fatal("precondition: panOffset should be nonzero again after PanUp")
	}
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.Render(w, 0, 200, 60)
	if v.panOffset != (orbital.Vec3{}) {
		t.Errorf("panOffset survived a second Focus change: %+v, want zero", v.panOffset)
	}
}

// TestPanClearsOnViewModeChange: a ViewMode change is also a Framing Event
// (ADR 0021 A) and must clear the pan offset just like a Focus change does.
func TestPanClearsOnViewModeChange(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v.Render(w, 0, 200, 60)

	v.PanRight()
	v.Render(w, 0, 200, 60)
	if v.panOffset == (orbital.Vec3{}) {
		t.Fatal("precondition: panOffset should be nonzero after PanRight")
	}

	w.CycleViewMode()
	v.Render(w, 0, 200, 60)
	if v.panOffset != (orbital.Vec3{}) {
		t.Errorf("panOffset survived a ViewMode change: %+v, want zero", v.panOffset)
	}
}

// TestPanSurvivesResize pins the explicit decision made alongside Zoom
// Memory (ADR 0042): a bare resize is NOT a refocus, so — mirroring
// TestZoomMemoryResizeKeepsMultiplier for userZoom — it must leave the pan
// offset exactly as the player left it. Render's contextChanged guard is
// false on a resize (SystemIdx/Focus/ViewMode/craftHere are all unchanged;
// only v.fitted flips because Resize clears it), so the reset lives inside
// `if contextChanged` alongside the Zoom Memory restore, not in the outer
// `if contextChanged || !v.fitted` block that also runs on a resize.
func TestPanSurvivesResize(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusSystem}
	v.Render(w, 0, 200, 60)

	v.PanRight()
	v.PanUp()
	v.Render(w, 0, 200, 60)
	want := v.panOffset
	if want == (orbital.Vec3{}) {
		t.Fatal("precondition: panOffset should be nonzero after PanRight+PanUp")
	}

	// Same focus context, different canvas dimensions — a bare resize.
	v.Resize(320, 90)
	v.Render(w, 0, 320, 90)

	if v.panOffset != want {
		t.Errorf("resize cleared or altered the pan offset: got %+v, want kept %+v", v.panOffset, want)
	}
}
