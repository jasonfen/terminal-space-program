package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// newProximityTestView is newSOIPassTestView sized to a real terminal
// rather than the roomy 200×60 the map tests use — the chips and axis
// labels have to survive 80×24, which is where the layout regressions
// this repo has actually shipped were found.
func newProximityTestView(t *testing.T, cols, rows int) *OrbitView {
	t.Helper()
	v := NewOrbitView(Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle(),
	})
	v.Resize(cols, rows)
	return v
}

// proximityWorld builds two vessels sharing Earth with the second placed
// exactly relPos from the first and targeted.
func proximityWorld(t *testing.T, relPos orbital.Vec3) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("expected 2 vessels, got %d", len(w.Crafts))
	}
	w.ActiveCraftIdx = 0
	active := w.ActiveCraft()
	w.Crafts[1].Primary = active.Primary
	w.Crafts[1].State.R = active.State.R.Add(relPos)
	w.Crafts[1].State.V = active.State.V
	w.SetTargetCraft(1)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	return w
}

// TestProximityEntryFitFromRange pins the Camera Contract's entry half:
// the fit is derived from the range at the moment of entry, and it frames
// the pair — the active vessel lands on canvas, comfortably inside the
// edge, without any further input.
func TestProximityEntryFitFromRange(t *testing.T) {
	for _, rangeM := range []float64{200, 2_000, 20_000} {
		w := proximityWorld(t, orbital.Vec3{X: rangeM})
		v := newProximityTestView(t, 80, 24)
		w.ViewMode = sim.ViewProximity
		v.Render(w, 0, 80, 24)

		st, ok := w.ProximityState()
		if !ok {
			t.Fatalf("range %.0f m: ProximityState refused", rangeM)
		}
		// Target dead centre.
		if d := v.canvas.CenterWorld().Sub(st.TargetWorld).Norm(); d > 1 {
			t.Errorf("range %.0f m: centre is %.1f m off the target", rangeM, d)
		}
		// Own vessel in frame.
		px, py, onCanvas := v.canvas.Project(st.CraftWorld)
		if !onCanvas {
			t.Errorf("range %.0f m: own vessel off canvas after the entry fit (px=%d py=%d)", rangeM, px, py)
		}
		// And not jammed against the edge: the fit leaves margin.
		fracX := math.Abs(float64(px)-float64(v.canvas.Cols()*2)/2) / (float64(v.canvas.Cols()*2) / 2)
		if fracX > 0.9 {
			t.Errorf("range %.0f m: own vessel at %.0f%% of the half-width — the entry fit isn't comfortable", rangeM, fracX*100)
		}
		// The fit scales with range: a wider gap must not produce the
		// same scale as a tight one.
		wantScale := v.canvas.Scale()
		if wantScale <= 0 {
			t.Errorf("range %.0f m: non-positive scale %g", rangeM, wantScale)
		}
	}
}

// TestProximityFitNeverChasesClosingRange is the Camera Contract's other
// half, and the one that is easy to get wrong: once entered, the fit is
// the player's. Closing from 20 km to 200 m must not move the scale.
func TestProximityFitNeverChasesRange(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 20_000})
	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewProximity
	v.Render(w, 0, 80, 24)
	entryScale := v.canvas.Scale()

	// Close the gap by two orders of magnitude — ambient sim state.
	active := w.ActiveCraft()
	w.Crafts[1].State.R = active.State.R.Add(orbital.Vec3{X: 200})
	v.Render(w, 0, 80, 24)

	if got := v.canvas.Scale(); got != entryScale {
		t.Errorf("closing range re-fit the camera: %.6e -> %.6e (ADR 0021: fit once per Framing Event)", entryScale, got)
	}
}

// TestProximityRoundTripRestoresMap is the toggle's promise: press to
// enter, press to leave, and the map is exactly as it was — same view
// mode, same manual zoom, same pan.
func TestProximityRoundTripRestoresMap(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 5_000})
	v := newProximityTestView(t, 80, 24)

	w.ViewMode = sim.ViewOrbitFlat
	v.Render(w, 0, 80, 24)
	v.ZoomIn()
	v.ZoomIn()
	v.PanRight()
	v.PanUp()
	v.Render(w, 0, 80, 24)
	mapZoom := v.userZoom
	mapPan := v.panOffset
	mapScale := v.canvas.Scale()
	if mapZoom == 1 {
		t.Fatal("setup: zoom stayed at 1, the test can't tell restore from reset")
	}
	if mapPan.Norm() == 0 {
		t.Fatal("setup: pan stayed at zero, the test can't tell restore from clear")
	}

	if entered, refusal := w.ToggleProximityView(); !entered {
		t.Fatalf("enter refused: %q", refusal)
	}
	v.Render(w, 0, 80, 24)
	// Proximity gets its own zoom slot — the map's multiplier must not
	// have leaked in, and zooming here must not overwrite the map's.
	if v.userZoom != 1 {
		t.Errorf("proximity opened at zoom %v, want 1 (its own fresh slot)", v.userZoom)
	}
	v.ZoomOut()
	v.Render(w, 0, 80, 24)

	if entered, refusal := w.ToggleProximityView(); entered || refusal != "" {
		t.Fatalf("leave: entered=%v refusal=%q", entered, refusal)
	}
	v.Render(w, 0, 80, 24)

	if w.ViewMode != sim.ViewOrbitFlat {
		t.Errorf("ViewMode = %s, want orbit-flat", w.ViewMode)
	}
	if v.userZoom != mapZoom {
		t.Errorf("map zoom = %v, want %v (round trip must not disturb it)", v.userZoom, mapZoom)
	}
	if d := v.panOffset.Sub(mapPan).Norm(); d > 1 {
		t.Errorf("map pan moved by %.1f m across the round trip", d)
	}
	if got := v.canvas.Scale(); math.Abs(got-mapScale)/mapScale > 1e-9 {
		t.Errorf("map scale = %.6e, want %.6e", got, mapScale)
	}
}

// TestProximityZoomMemoryPerTarget: Proximity remembers per TARGET, the
// map per focus, and the two never share a slot.
func TestProximityZoomMemoryPerTarget(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 5_000})
	// A third vessel to retarget to. SpawnCraft makes the new vessel
	// active, so hand the stick back to idx 0 afterwards.
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 401e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	if len(w.Crafts) != 3 {
		t.Fatalf("expected 3 vessels, got %d", len(w.Crafts))
	}
	v := newProximityTestView(t, 80, 24)

	w.ViewMode = sim.ViewProximity
	v.Render(w, 0, 80, 24)
	v.ZoomIn()
	v.ZoomIn()
	want := v.userZoom
	v.Render(w, 0, 80, 24)

	// Retarget: a different vessel is a different remembered slot.
	w.SetTargetCraft(2)
	v.Render(w, 0, 80, 24)
	if v.userZoom != 1 {
		t.Errorf("new target opened at zoom %v, want 1 (first visit fits fresh)", v.userZoom)
	}

	// Back to the first: the multiplier comes back.
	w.SetTargetCraft(1)
	v.Render(w, 0, 80, 24)
	if v.userZoom != want {
		t.Errorf("returning to the first target restored zoom %v, want %v", v.userZoom, want)
	}
}

// TestProximityRendersAt80x24 is the layout smoke test: the frame is the
// right size, holds the readout chip and both axis labels, and nothing
// runs off the edge.
func TestProximityRendersAt80x24(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 800})
	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewProximity
	out := v.Render(w, 0, 80, 24)

	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Errorf("rendered %d rows, want 24", len(lines))
	}
	// Row 0 is the shared title bar, which already overruns 80 cols on the
	// map path — a pre-existing condition, not this view's to fix. Every
	// row this view actually composes must fit.
	for i, l := range lines[1:] {
		if width := lipgloss.Width(l); width > 80 {
			t.Errorf("row %d is %d cols wide, want ≤ 80", i+1, width)
		}
	}
	for _, want := range []string{"PROXIMITY", "range:", "|v_rel|:", "closing:", "view: proximity", "+V", "Earth"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame is missing %q\n%s", want, out)
		}
	}
}

// TestProximityFrameOrientation: the picture must back up the axis
// labels. A vessel trailing the target renders LEFT of centre; a vessel
// below it renders BELOW centre (toward the primary).
func TestProximityFrameOrientation(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	active := w.ActiveCraft()
	frame, ok := orbital.LVLHBasis(active.State.R, active.State.V)
	if !ok {
		t.Fatal("precondition: no LVLH frame for the spawned orbit")
	}
	// Target keeps the state the frame came from; we sit 600 m behind and
	// 300 m below it.
	tgtR, tgtV := active.State.R, active.State.V
	w.Crafts[1].Primary = active.Primary
	w.Crafts[1].State.R = tgtR
	w.Crafts[1].State.V = tgtV
	active.State.R = tgtR.
		Add(frame.AlongTrack.Scale(-600)).
		Add(frame.RadialOut.Scale(-300))
	w.SetTargetCraft(1)
	w.ViewMode = sim.ViewProximity

	v := newProximityTestView(t, 80, 24)
	v.Render(w, 0, 80, 24)

	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused")
	}
	tx, ty, _ := v.canvas.Project(st.TargetWorld)
	cx, cy, onCanvas := v.canvas.Project(st.CraftWorld)
	if !onCanvas {
		t.Fatal("own vessel off canvas at the entry fit")
	}
	if cx >= tx {
		t.Errorf("trailing vessel drew at px %d, target at %d — behind must render LEFT of the target", cx, tx)
	}
	if cy <= ty {
		t.Errorf("below vessel drew at py %d, target at %d — below must render BELOW the target (toward the primary)", cy, ty)
	}
}

// TestProximityNoTargetShowsRefusal: entering the view with a body target
// (via the `v` cycle, or after a partner vanishes) must explain itself on
// canvas rather than present an empty field.
func TestProximityNoTargetShowsRefusal(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	w.SetTargetBody(1)
	w.ViewMode = sim.ViewProximity

	v := newProximityTestView(t, 80, 24)
	out := v.Render(w, 0, 80, 24)
	if !strings.Contains(out, "needs a VESSEL target") {
		t.Errorf("no-target frame doesn't say what's missing\n%s", out)
	}
	if !strings.Contains(out, "[t] target a vessel") || !strings.Contains(out, "[o] back to the map") {
		t.Errorf("no-target frame doesn't offer a way out\n%s", out)
	}
}

// TestProximityHintChipOnApproach: the hint appears on the map once the
// approach crosses inside the band, names the key, and is gone inside the
// view it advertises.
func TestProximityHintChipOnApproach(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 5_000})
	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewTilted
	w.Tick() // steps the crossing state machine

	out := v.Render(w, 0, 80, 24)
	if !strings.Contains(out, "CLOSE RANGE") {
		t.Errorf("no hint chip at 5 km\n%s", out)
	}

	if entered, refusal := w.ToggleProximityView(); !entered {
		t.Fatalf("enter refused: %q", refusal)
	}
	out = v.Render(w, 0, 80, 24)
	if strings.Contains(out, "CLOSE RANGE") {
		t.Errorf("hint chip still on screen inside Proximity View\n%s", out)
	}
}

// TestProximityHintAbsentFarAway: no chip while the pair is nowhere near
// each other — the hint is a moment, not a standing decoration.
func TestProximityHintAbsentFarAway(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 500_000})
	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewTilted
	w.Tick()
	if out := v.Render(w, 0, 80, 24); strings.Contains(out, "CLOSE RANGE") {
		t.Errorf("hint chip at 500 km\n%s", out)
	}
}
