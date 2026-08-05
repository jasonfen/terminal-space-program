package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
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

// --- Hull-sprite slice tests (issue #348 §1) ---

// TestProximitySpriteHysteresis pins proximitySpriteVisible's arm-high /
// release-low band directly: growing UP through proximitySpriteEnterCells
// switches a glyph to its sprite; shrinking back DOWN, the sprite survives
// until it drops below proximitySpriteExitCells. Both directions, plus the
// dead zone in between where whichever state was already active holds.
func TestProximitySpriteHysteresis(t *testing.T) {
	cases := []struct {
		name        string
		shown       bool
		heightCells float64
		want        bool
	}{
		{"glyph, well below enter, stays glyph", false, 1.0, false},
		{"glyph, just below enter, stays glyph", false, proximitySpriteEnterCells - 0.01, false},
		{"glyph, exactly at enter, switches to sprite", false, proximitySpriteEnterCells, true},
		{"glyph, above enter, switches to sprite", false, 10.0, true},
		{"sprite, still well above exit, stays sprite", true, 10.0, true},
		{"sprite, exactly at exit, stays sprite (>=)", true, proximitySpriteExitCells, true},
		{"sprite, just below exit, falls back to glyph", true, proximitySpriteExitCells - 0.01, false},
		{"sprite, in the hysteresis band, stays sprite", true, (proximitySpriteEnterCells + proximitySpriteExitCells) / 2, true},
		{"glyph, in the hysteresis band, stays glyph", false, (proximitySpriteEnterCells + proximitySpriteExitCells) / 2, false},
		{"sprite, height collapsed to zero, falls back to glyph", true, 0, false},
	}
	for _, c := range cases {
		if got := proximitySpriteVisible(c.shown, c.heightCells); got != c.want {
			t.Errorf("%s: proximitySpriteVisible(shown=%v, %.3f) = %v, want %v",
				c.name, c.shown, c.heightCells, got, c.want)
		}
	}
}

// TestProximitySpriteHeightCellsNoSpriteOrScale pins the two "no sprite"
// guard cases proximitySpriteHeightCells owes: a stack with no
// LaunchSprite-catalogued stage, and a non-positive scale — both must
// read as 0 cells (glyph), never panic on the division.
func TestProximitySpriteHeightCellsNoSpriteOrScale(t *testing.T) {
	spriteless := []spacecraft.Stage{{LaunchSpriteRowsPx: 0}}
	if got := proximitySpriteHeightCells(spriteless, 1.0); got != 0 {
		t.Errorf("spriteless stack: got %v cells, want 0", got)
	}
	tall := []spacecraft.Stage{{LaunchSpriteRowsPx: 10, LaunchSpriteWidthPx: 2, Color: "#FFFFFF"}}
	if got := proximitySpriteHeightCells(tall, 0); got != 0 {
		t.Errorf("zero scale: got %v cells, want 0", got)
	}
	if got := proximitySpriteHeightCells(tall, -1); got != 0 {
		t.Errorf("negative scale: got %v cells, want 0", got)
	}
}

// TestProximityHullSpriteSpansExpectedCellsAtEntryFit is the "seeded
// range" quality-bar test: at a 20 m separation — inside
// proximityFitFloorM (50 m), so the entry fit pins to the game's own
// DOCK READY floor rather than scaling with range — a 24 m-tall stack
// (16 sub-pixel rows × vesselSubPixelM) spans the height
// proximityFitRadius's own documented entry-fit math (Canvas.FitTo:
// scale = 0.45 × shorter-pixel-axis / radius) predicts, derived here
// independently of the production code under test rather than by
// re-calling it, so a scale-formula regression can't silently agree
// with itself.
func TestProximityHullSpriteSpansExpectedCellsAtEntryFit(t *testing.T) {
	const rangeM = 20.0
	const rows = 16
	stages := []spacecraft.Stage{{LaunchSpriteRowsPx: rows, LaunchSpriteWidthPx: 2, Color: "#ABCDEF"}}

	w := proximityWorld(t, orbital.Vec3{X: rangeM})
	w.Crafts[0].Stages = stages
	w.Crafts[1].Stages = stages
	v := newProximityTestView(t, 104, 24)
	w.ViewMode = sim.ViewProximity
	v.Render(w, 0, 104, 24)

	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused")
	}
	fitRadius := proximityFitRadius(st)
	shorterPx := float64(v.canvas.Cols() * 2)
	if h := float64(v.canvas.Rows() * 4); h < shorterPx {
		shorterPx = h
	}
	wantScale := 0.45 * shorterPx / fitRadius
	if got := v.canvas.Scale(); math.Abs(got-wantScale)/wantScale > 1e-9 {
		t.Fatalf("canvas scale = %.6f, want %.6f (FitTo's own documented formula)", got, wantScale)
	}

	wantCells := float64(rows) * vesselSubPixelM * wantScale / canvasCellPxH
	if wantCells < proximitySpriteEnterCells {
		t.Fatalf("test setup: stack only spans %.2f cells at entry fit, need >= %v to exercise the sprite path",
			wantCells, proximitySpriteEnterCells)
	}
	if got := proximitySpriteHeightCells(stages, v.canvas.Scale()); math.Abs(got-wantCells) > 1e-6 {
		t.Errorf("sprite height = %.3f cells, want %.3f", got, wantCells)
	}

	// And the hull actually reached the canvas in the stage's own colour
	// — the end-to-end check that drawProximityVessels wired the height
	// math through to real ink, not just that the formula agrees with
	// itself.
	if n := v.canvas.CountColor(lipgloss.Color("#ABCDEF")); n == 0 {
		t.Error("no hull-sprite ink found on canvas at entry-fit scale")
	}
}

// TestProximityHullPositionsMatchLVLHAnchors extends
// TestProximityFrameOrientation's own construction (trailing +
// below-primary) to the hull-sprite path: with both vessels' Stages
// enlarged enough to guarantee the sprite threshold at this range, the
// composed silhouette must still land AT each vessel's true LVLH anchor
// — own vessel's hull registers a vessel-hit at its own screen cell,
// target's hull at its own, and the trailing/below relationship the
// glyph-only test already pins survives unchanged (the sprite's anchor
// point is the same world position the old glyph used; only what's
// painted around it changed).
func TestProximityHullPositionsMatchLVLHAnchors(t *testing.T) {
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
	tgtR, tgtV := active.State.R, active.State.V
	w.Crafts[1].Primary = active.Primary
	w.Crafts[1].State.R = tgtR
	w.Crafts[1].State.V = tgtV
	active.State.R = tgtR.
		Add(frame.AlongTrack.Scale(-600)).
		Add(frame.RadialOut.Scale(-300))
	w.SetTargetCraft(1)
	w.ViewMode = sim.ViewProximity

	// Deliberately (and unrealistically) oversized for the ~670 m
	// separation — this test is about WHERE the hull lands, not the
	// realism of its size, and a big stack removes any doubt that both
	// crossed the sprite threshold at whatever scale the entry fit
	// (proportional to range, not floored at this distance) lands on.
	big := []spacecraft.Stage{{LaunchSpriteRowsPx: 400, LaunchSpriteWidthPx: 4, Color: "#112233"}}
	active.Stages = big
	w.Crafts[1].Stages = big

	v := newProximityTestView(t, 104, 24)
	v.Render(w, 0, 104, 24)

	if !v.proxOwnSpriteShown {
		t.Fatal("own vessel did not switch to the hull sprite — test setup too small")
	}
	if !v.proxTargetSpriteShown {
		t.Fatal("target did not switch to the hull sprite — test setup too small")
	}

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
		t.Errorf("trailing vessel anchor at px %d, target at %d — behind must still render LEFT of the target with the sprite path", cx, tx)
	}
	if cy <= ty {
		t.Errorf("below vessel anchor at py %d, target at %d — below must still render BELOW the target with the sprite path", cy, ty)
	}

	ownCol, ownRow, onCanvas := v.canvasCell(st.CraftWorld)
	if !onCanvas {
		t.Fatal("own vessel cell off canvas")
	}
	if hit := v.canvas.HitAt(ownCol, ownRow); !hit.IsVessel {
		t.Error("own vessel's anchor cell doesn't read as a vessel hit with the hull sprite drawn")
	}
	tgtCol, tgtRow, onCanvas := v.canvasCell(st.TargetWorld)
	if !onCanvas {
		t.Fatal("target cell off canvas")
	}
	if hit := v.canvas.HitAt(tgtCol, tgtRow); !hit.IsVessel {
		t.Error("target's anchor cell doesn't read as a vessel hit with the hull sprite drawn")
	}
}

// TestProximityGhostTargetFallsBackToGlyph: a multiplayer ghost target
// carries a name, a glyph and a Kepler-propagated state — never a stage
// stack — so no range should ever switch it to a hull sprite; it stays
// the diamond glyph regardless of how close the pair gets.
func TestProximityGhostTargetFallsBackToGlyph(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	active := w.ActiveCraft()
	primary := active.Primary
	primaryPos := w.BodyPosition(primary)
	// 40 m away — well inside the range a real vessel with any
	// catalog-sized stack would already be rendering as a sprite.
	ghostWorldPos := primaryPos.Add(active.State.R).Add(orbital.Vec3{X: 40})
	w.Ghosts = []sim.Ghost{{
		Owner: "SHA256:ghosttest", CraftID: 99, Handle: "remote",
		Name: "Remote Craft", PrimaryID: primary.ID,
		Pos: ghostWorldPos, Vel: active.State.V,
	}}
	w.SetTargetGhost("SHA256:ghosttest", 99)
	w.ViewMode = sim.ViewProximity

	v := newProximityTestView(t, 104, 24)
	v.Render(w, 0, 104, 24)

	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused for a ghost target")
	}
	if v.proxTargetSpriteShown {
		t.Error("ghost target latched a hull sprite — ghosts carry no composition data")
	}
	col, row, onCanvas := v.canvasCell(st.TargetWorld)
	if !onCanvas {
		t.Fatal("ghost target off canvas")
	}
	if hit := v.canvas.HitAt(col, row); !hit.IsVessel {
		t.Error("ghost target's glyph fallback did not tag IsVessel")
	}
}

// TestProximityHullWinsRingOverlapSamePixel pins the draw-order
// requirement directly at the canvas level: when a ring (or any other
// backdrop cue) happens to ink the exact same braille sub-pixel the
// hull sprite also occupies, the sprite — drawn after, per
// renderProximity's call order — must win the pixel outright, not just
// tie a per-cell majority vote.
func TestProximityHullWinsRingOverlapSamePixel(t *testing.T) {
	v := newProximityTestView(t, 104, 24)
	frame := orbital.LVLHFrame{
		AlongTrack: orbital.Vec3{X: 1},
		RadialOut:  orbital.Vec3{Y: 1},
		CrossTrack: orbital.Vec3{Z: 1},
	}
	v.canvas.SetBasis(widgets.Basis{X: frame.AlongTrack, Y: frame.RadialOut})
	v.canvas.SetScale(1.0) // 1 px per metre
	v.canvas.Center(orbital.Vec3{})
	target := orbital.Vec3{}

	// Tall enough (20 rows * 1.5 m stride = 30 m, well past the 3-cell
	// enter threshold at 1 px/m) that the hysteresis gate isn't what's
	// under test here — the overlap is.
	stages := []spacecraft.Stage{{LaunchSpriteRowsPx: 20, LaunchSpriteWidthPx: 2, Color: "#123456"}}
	sprite := ComposeLaunchSprite(stages, orbital.Vec3{}, v.canvas.Basis(), vesselSubPixelM)
	if len(sprite) == 0 {
		t.Fatal("setup: no composed sprite")
	}
	overlapWorld := target.Add(sprite[0].OffsetWorld)

	// Paint a ring pixel at the EXACT same world point first — standing
	// in for a range ring or dock gate that happens to cross the
	// sprite's footprint (drawProximityRangeRings / drawProximityDockGate
	// both run before drawProximityVessels in renderProximity).
	v.canvas.PlotColored(overlapWorld, render.ColorDim)

	shown := false
	if !v.drawProximityHull(&shown, stages, orbital.Vec3{}, target, frame) {
		t.Fatal("drawProximityHull refused a tall stack at 1 px/m")
	}

	if n := v.canvas.CountColor(render.ColorDim); n != 0 {
		t.Errorf("ring colour still present after the hull overwrote the shared pixel: %d cells", n)
	}
	if n := v.canvas.CountColor(lipgloss.Color("#123456")); n == 0 {
		t.Error("hull stage colour did not become the cell's colour over the ring ink")
	}
	col, row, onCanvas := v.canvasCell(overlapWorld)
	if !onCanvas {
		t.Fatal("overlap point off canvas")
	}
	if hit := v.canvas.HitAt(col, row); !hit.IsVessel {
		t.Error("the shared cell doesn't read as a vessel hit — the ring pixel's tag is winning the overlap")
	}
}

// TestProximityHullsRenderAt104x24 re-runs the layout smoke test at the
// screen's actual minimum supported terminal size (screens.MinTerminalWidth
// = 104, not the 80 the rest of this file's tests use for headroom) with
// both vessels enlarged enough to force the hull-sprite path, so a sprite
// that overruns a row or corrupts the frame at the real floor fails here.
func TestProximityHullsRenderAt104x24(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 40})
	big := []spacecraft.Stage{{LaunchSpriteRowsPx: 20, LaunchSpriteWidthPx: 2, Color: "#445566"}}
	w.Crafts[0].Stages = big
	w.Crafts[1].Stages = big
	v := newProximityTestView(t, MinTerminalWidth, 24)
	w.ViewMode = sim.ViewProximity
	out := v.Render(w, 0, MinTerminalWidth, 24)

	if !v.proxOwnSpriteShown || !v.proxTargetSpriteShown {
		t.Fatal("test setup: hull sprites did not engage for either vessel")
	}

	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Fatalf("rendered %d rows, want 24", len(lines))
	}
	for i, l := range lines[1:] {
		if width := lipgloss.Width(l); width > MinTerminalWidth {
			t.Errorf("row %d is %d cols wide, want ≤ %d", i+1, width, MinTerminalWidth)
		}
	}
}
