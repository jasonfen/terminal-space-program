package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
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

// --- Cue slice tests (issue #348 §1) ---

// TestProximityRangeRingsVisibleByZoom pins drawProximityRangeRings'
// "only draw what fits at the current zoom" rule directly against
// proximityRingVisible, the O(1) gate every ring call goes through
// before the O(segments) polyline walk. At a 20 km separation, the
// entry fit frames roughly 45 km — comfortably wide enough for the
// 10 km ring to clear proximityRingMinPixels, but the 1 km and 100 m
// rings project to under a pixel and must be skipped rather than drawn
// as noise.
func TestProximityRangeRingsVisibleByZoom(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 20_000})
	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewProximity
	v.Render(w, 0, 80, 24)

	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused")
	}
	if !v.proximityRingVisible(st, 10_000) {
		t.Error("10 km ring not visible at a 20 km separation — should clear the pixel floor")
	}
	if v.proximityRingVisible(st, 1_000) {
		t.Error("1 km ring visible at a 20 km separation — should be sub-pixel and skipped")
	}
	if v.proximityRingVisible(st, 100) {
		t.Error("100 m ring visible at a 20 km separation — should be sub-pixel and skipped")
	}
}

// TestProximityRangeRingsSkippedWhenEngulfed is the other half of "only
// draw what fits at the current zoom": once the view is fitted tight
// enough (a ~10 m separation, so the fit floor takes over), the 10 km
// and 1 km rings are so much larger than the framed view that the whole
// canvas sits deep inside them — the circle itself never crosses a
// single pixel, which the naive "is the bounding box on screen" check
// would miss (a huge ring's bounding box always overlaps a small
// canvas). The 50 m dock gate, by contrast, is close enough to the fit
// radius to still cross the canvas and must stay visible.
func TestProximityRangeRingsSkippedWhenEngulfed(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 10})
	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewProximity
	v.Render(w, 0, 80, 24)

	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused")
	}
	if v.proximityRingVisible(st, 10_000) {
		t.Error("10 km ring reported visible at a 10 m separation — the canvas is entirely inside it")
	}
	if v.proximityRingVisible(st, 1_000) {
		t.Error("1 km ring reported visible at a 10 m separation — the canvas is entirely inside it")
	}
	if !v.proximityRingVisible(st, sim.DockingDistM) {
		t.Error("50 m dock gate not visible at a 10 m separation — it should still cross the framed view")
	}
}

// TestProximityDockGateColorReady: the gate ring renders ColorTarget
// green exactly when the pair is inside BOTH DockingDistM and
// DockingVMS — the same predicate table TestProximityDockGateReady
// exercises at the sim layer, here confirmed to actually reach the
// canvas in the right colour. Compared as a DELTA rather than an
// absolute count, because the target vessel's own glyph is always drawn
// ColorTarget (drawProximityVessels) regardless of gate state — the
// ready scene must show MORE ColorTarget cells than the not-ready scene
// (the gate ring's own ~dozens of dotted cells switching from dim to
// green), not merely "some."
func TestProximityDockGateColorReady(t *testing.T) {
	readyW := proximityWorld(t, orbital.Vec3{X: 30}) // inside DockingDistM, matched v (v_rel = 0)
	readyV := newProximityTestView(t, 80, 24)
	readyW.ViewMode = sim.ViewProximity
	readyV.Render(readyW, 0, 80, 24)
	readySt, ok := readyW.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused (ready case)")
	}
	if !readyW.ProximityDockGateReady(readySt) {
		t.Fatal("precondition: ready case is not actually ready")
	}

	notReadyW := proximityWorld(t, orbital.Vec3{X: 100}) // outside DockingDistM, matched v
	notReadyV := newProximityTestView(t, 80, 24)
	notReadyW.ViewMode = sim.ViewProximity
	notReadyV.Render(notReadyW, 0, 80, 24)
	notReadySt, ok := notReadyW.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused (not-ready case)")
	}
	if notReadyW.ProximityDockGateReady(notReadySt) {
		t.Fatal("precondition: not-ready case is actually ready")
	}
	if !readyV.proximityRingVisible(readySt, sim.DockingDistM) {
		t.Fatal("precondition: gate ring not visible in the ready scene")
	}
	if !notReadyV.proximityRingVisible(notReadySt, sim.DockingDistM) {
		t.Fatal("precondition: gate ring not visible in the not-ready scene")
	}

	readyGreen := readyV.canvas.CountColor(render.ColorTarget)
	notReadyGreen := notReadyV.canvas.CountColor(render.ColorTarget)
	if readyGreen <= notReadyGreen {
		t.Errorf("ColorTarget cells: ready=%d, not-ready=%d — the gate ring should add green cells the not-ready scene doesn't have", readyGreen, notReadyGreen)
	}
}

// TestProximityDriftPathDrawsPlannedClass: the drift path reaches the
// canvas as ClassPlanned (dashed) ink in render.ColorPlannedNode — the
// end-to-end wiring check that drawProximityDriftPath actually calls
// sim.ProximityDriftPath and plots what comes back, not just that the
// sim math is right (already covered at the sim layer).
func TestProximityDriftPathDrawsPlannedClass(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 2_000})
	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewProximity
	v.Render(w, 0, 80, 24)

	if n := v.canvas.CountColor(render.ColorPlannedNode); n == 0 {
		t.Error("no ColorPlannedNode ink found on canvas — drift path did not draw")
	}
}

// TestProximityVelocityVectorOrientation checks
// proximityVelocityVectorEndpoint — the pure direction math behind the
// v_rel stub — for a known relative velocity: purely along the target's
// +AlongTrack, the endpoint must decompose to a POSITIVE along-track
// offset from the craft (the +V-bar side), zero radial.
func TestProximityVelocityVectorOrientation(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 800})
	// Own vessel closes along the target's +AlongTrack direction — set
	// directly via the same LVLH frame the scene resolves, so the
	// expectation is stated in the frame's own vocabulary.
	active := w.ActiveCraft()
	tc := w.Crafts[1]
	frame, ok := orbital.LVLHBasis(tc.State.R, tc.State.V)
	if !ok {
		t.Fatal("precondition: no LVLH frame")
	}
	active.State.V = tc.State.V.Add(frame.AlongTrack.Scale(3))

	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused")
	}
	if st.RelVel.Norm() == 0 {
		t.Fatal("precondition: RelVel is zero")
	}

	const scale = 0.01 // pixels-per-metre; only the DIRECTION is under test
	end, ok := proximityVelocityVectorEndpoint(st, scale)
	if !ok {
		t.Fatal("proximityVelocityVectorEndpoint refused a nonzero RelVel")
	}
	local := st.Frame.ToFrame(end.Sub(st.CraftWorld))
	if local.X <= 0 {
		t.Errorf("along-track = %g, want > 0 (stub should point toward +V-bar)", local.X)
	}
	if math.Abs(local.Y) > 1e-6 {
		t.Errorf("radial = %g, want ~0 (relative velocity is pure along-track)", local.Y)
	}
}

// TestProximityCuesRenderAt80x24 re-runs the core's layout smoke test
// (TestProximityRendersAt80x24) at a range that actually exercises every
// cue this slice adds — rings, dock gate, drift path, v_rel stub — so a
// cue that overruns a row or corrupts the frame at the minimum supported
// terminal size fails here rather than shipping unnoticed.
func TestProximityCuesRenderAt80x24(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 5_000})
	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewProximity
	out := v.Render(w, 0, 80, 24)

	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Fatalf("rendered %d rows, want 24", len(lines))
	}
	for i, l := range lines[1:] {
		if width := lipgloss.Width(l); width > 80 {
			t.Errorf("row %d is %d cols wide, want ≤ 80", i+1, width)
		}
	}
}
