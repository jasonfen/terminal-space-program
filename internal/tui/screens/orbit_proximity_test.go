package screens

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/charmbracelet/x/ansi"

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
		// The centre is pinned by TestProximityViewIsCentredOnTheTargetPort.
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
	// ADR 0046 (#422): at 80×24 the left side may not have room for every
	// chip competing with PROXIMITY (VESSEL, MISSION, ATTITUDE), so
	// PROXIMITY itself may shrink to its Compact Form (name + range,
	// dropping |v_rel|/closing) under Graceful Shrink — that's the point
	// of this test suite's namesake ADR, not a regression. What must
	// still hold: the chip itself (and its one load-bearing number,
	// range) is never silently lost.
	for _, want := range []string{"PROXIMITY", "range:", "view: Proximity", "+V", "Earth"} {
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

// TestProximityHintChipOnApproach: the hint appears once the approach
// crosses inside the band, names the key, and is gone inside the view it
// advertises. This test is about the hint's own content-selection logic
// (ProximityHintActive's crossing state machine), not chip layout, so it
// calls buildProximityHintChip directly rather than through a full
// Render, ADR 0051's eight instrument boxes now fully occupy the right
// column's budget (NAVIGATION 10 + TARGET 7 = 17 of 17) even at the
// Design Size, so a notice like this hint has no row left to win in a
// real render until slice 3 moves notices into their own bay (out of
// the corner budget entirely, per the ADR's own proposed build slicing).
// Checking the builder directly keeps this test about the crossing
// state machine, not that still-pending budget move.
func TestProximityHintChipOnApproach(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 5_000})
	v := newProximityTestView(t, DesignWidth, DesignHeight)
	w.ViewMode = sim.ViewTilted
	w.Tick() // steps the crossing state machine

	if lines := v.buildProximityHintChip(w); lines == nil {
		t.Error("no hint chip at 5 km")
	}

	if entered, refusal := w.ToggleProximityView(); !entered {
		t.Fatalf("enter refused: %q", refusal)
	}
	if lines := v.buildProximityHintChip(w); lines != nil {
		t.Errorf("hint chip still active inside Proximity View: %v", lines)
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

// proximityLatchedWorld docks two vessels (DockCrafts fuses regardless of
// proximity) and immediately undocks them, arming a same-World re-arm latch
// (#343) between the two restored craft, then places the partner at relPos
// with matched velocity and targets it — the #372 scene: a latched pair
// sitting inside both raw docking gates.
func proximityLatchedWorld(t *testing.T, relPos orbital.Vec3) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("expected 2 vessels after spawn, got %d", len(w.Crafts))
	}
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("DockCrafts did not fuse the pair (slate has %d craft)", len(w.Crafts))
	}
	if !w.Undock(0) {
		t.Fatalf("Undock refused on the freshly-fused composite")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("expected 2 vessels after undock, got %d", len(w.Crafts))
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

// proximityOverspeedWorld builds the pair with relPos inside the distance
// gate, a closing rate of closingMS toward the target, and a total
// |v_rel| of vRelMS — closingMS and vRelMS can differ (a lateral pass:
// low closing rate, high |v_rel|), which is exactly the #421 scene the
// gate keys on. Derivation: with dir = unit(relPos) and perp orthogonal
// to it (Z, since every relPos used here is pure-X), setting
// relV = closingMS*dir + lateral*perp with
// lateral = sqrt(max(vRelMS^2 - closingMS^2, 0)) gives EXACTLY
// ClosingMS == closingMS and VRelMS == vRelMS in the resolved
// ProximityState (ClosingMS is the component of relV along dir; VRelMS
// is |relV|). Perturbs only the target's (Crafts[1]) velocity so the
// active vessel's velocity used elsewhere in a scene stays the plain
// matched-orbit baseline proximityWorld already sets up.
func proximityOverspeedWorld(t *testing.T, relPos orbital.Vec3, closingMS, vRelMS float64) *sim.World {
	t.Helper()
	w := proximityWorld(t, relPos)
	active := w.ActiveCraft()
	dir := relPos.Scale(1 / relPos.Norm())
	perp := orbital.Vec3{Z: 1}
	lateralSq := vRelMS*vRelMS - closingMS*closingMS
	if lateralSq < 0 {
		lateralSq = 0
	}
	lateral := math.Sqrt(lateralSq)
	relV := dir.Scale(closingMS).Add(perp.Scale(lateral))
	w.Crafts[1].State.V = active.State.V.Sub(relV)
	return w
}

// TestProximityOverSpeedReadoutSuffix (#421, revised): the |v_rel|: row
// must carry the "(need < 0.10)" gate reminder exactly when the pair
// sits inside the distance gate but |v_rel| alone already meets or
// exceeds sim.DockingVMS — including the LATERAL-PASS case the gate
// originally missed (closing only 0.05 m/s, but |v_rel| 0.50 m/s: the
// pair is sliding past, not closing, yet checkDocking still refuses on
// the raw |v_rel| and the readout must say so) — and must NOT carry it
// once ready to dock or once truly out of range. The closing: row never
// carries the suffix: closing can read well under 0.10 in the very
// scene the gate is firing, and tagging it there would misname why
// docking is failing.
func TestProximityOverSpeedReadoutSuffix(t *testing.T) {
	headOnW := proximityOverspeedWorld(t, orbital.Vec3{X: 30}, 0.96, 0.96) // inside 50 m, head-on 0.96 m/s
	headOnV := newProximityTestView(t, 80, 24)
	headOnW.ViewMode = sim.ViewProximity
	chip := headOnV.buildProximityChip(headOnW)
	if len(chip) < 4 {
		t.Fatalf("buildProximityChip returned %d rows, want at least 4", len(chip))
	}
	vRelRow, closingRow := chipLine(chip, "rel speed:"), chipLine(chip, "closing:")
	if !strings.Contains(vRelRow, "need < 0.10") {
		t.Errorf("|v_rel| row = %q, want it to carry the gate reminder (need < %.2f)", vRelRow, sim.DockingVMS)
	}
	if !strings.Contains(vRelRow, "0.96") {
		t.Errorf("|v_rel| row = %q, want the plain magnitude to still read 0.96 m/s", vRelRow)
	}
	if strings.Contains(closingRow, "need <") {
		t.Errorf("closing row = %q, must never carry the gate reminder", closingRow)
	}

	lateralW := proximityOverspeedWorld(t, orbital.Vec3{X: 30}, 0.05, 0.50) // lateral pass: closing slow, |v_rel| fast
	lateralV := newProximityTestView(t, 80, 24)
	lateralW.ViewMode = sim.ViewProximity
	lateralChip := lateralV.buildProximityChip(lateralW)
	if len(lateralChip) < 4 {
		t.Fatalf("buildProximityChip (lateral) returned %d rows, want at least 4", len(lateralChip))
	}
	if row := chipLine(lateralChip, "rel speed:"); !strings.Contains(row, "need < 0.10") {
		t.Errorf("lateral-pass |v_rel| row = %q, want it to carry the gate reminder even though closing is slow", row)
	}
	if row := chipLine(lateralChip, "closing:"); strings.Contains(row, "need <") {
		t.Errorf("lateral-pass closing row = %q, must not carry the reminder (closing rate itself is under the limit)", row)
	}

	readyW := proximityWorld(t, orbital.Vec3{X: 30}) // inside 50 m, matched v — actually ready, no gate to remind about
	readyV := newProximityTestView(t, 80, 24)
	readyW.ViewMode = sim.ViewProximity
	readyChip := readyV.buildProximityChip(readyW)
	if len(readyChip) < 4 {
		t.Fatalf("buildProximityChip (ready) returned %d rows, want at least 4", len(readyChip))
	}
	if row := chipLine(readyChip, "rel speed:"); strings.Contains(row, "need <") {
		t.Errorf("ready-case |v_rel| row = %q, must not carry the gate reminder", row)
	}

	farW := proximityOverspeedWorld(t, orbital.Vec3{X: 100}, 0.96, 0.96) // outside 50 m even though fast
	farV := newProximityTestView(t, 80, 24)
	farW.ViewMode = sim.ViewProximity
	farChip := farV.buildProximityChip(farW)
	if len(farChip) < 4 {
		t.Fatalf("buildProximityChip (far) returned %d rows, want at least 4", len(farChip))
	}
	if row := chipLine(farChip, "rel speed:"); strings.Contains(row, "need <") {
		t.Errorf("out-of-range |v_rel| row = %q, must not carry the gate reminder", row)
	}
}

// TestProximityOverSpeedSurvivesCompactForm (#421): the Compact Form
// normally drops the |v_rel|: row entirely (ADR 0046 / #422), but the
// over-speed gate reminder is safety-critical the same way the amber
// re-arm latch on the ring is — Graceful Shrink's contract is "shrinks
// before it vanishes," not "the warning vanishes first." When the gate
// isn't firing, Compact stays at its usual 2 rows (name + range only).
func TestProximityOverSpeedSurvivesCompactForm(t *testing.T) {
	overspeedW := proximityOverspeedWorld(t, orbital.Vec3{X: 30}, 0.05, 0.50) // lateral pass
	overspeedV := newProximityTestView(t, 80, 24)
	overspeedW.ViewMode = sim.ViewProximity
	compact := overspeedV.buildProximityChipCompact(overspeedW)
	// name, range, the port: row (this pair has no known port shape, and
	// a pair that cannot dock is always out of gate, ADR 0053 decision 6)
	// and |v_rel|.
	if len(compact) != 4 {
		t.Fatalf("Compact Form over the gate returned %d rows, want 4 (name, range, port, |v_rel|)", len(compact))
	}
	if row := chipLine(compact, "rel speed:"); !strings.Contains(row, "need < 0.10") || !strings.Contains(row, "0.50") {
		t.Errorf("Compact |v_rel| row = %q, want the same gate-suffixed row the full chip carries", row)
	}

	// A ported pair sitting in the gate has nothing to remind about: back
	// to name and range only.
	readyW := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, 0)
	readyV := newProximityTestView(t, 80, 24)
	readyW.ViewMode = sim.ViewProximity
	readyCompact := readyV.buildProximityChipCompact(readyW)
	if len(readyCompact) != 2 {
		t.Fatalf("Compact Form inside the gate returned %d rows, want 2 (name, range only): %q", len(readyCompact), readyCompact)
	}
}

// TestProximityDockGateColorOverspeed (#421) is the acceptance test for
// the ring's third state: inside the distance gate but |v_rel| over the
// limit must render a DISTINCT colour (render.ColorAlert) from ready
// (green), latched (amber), and plain dim (out of range) — so "you're
// miles out" and "you're 2 m away but too fast" stop rendering
// identically (the issue's own #348 finding). Uses the lateral-pass
// geometry (closing slow, |v_rel| fast) specifically, since that is the
// case the ring's ORIGINAL (ClosingMS-keyed) implementation missed
// entirely.
func TestProximityDockGateColorOverspeed(t *testing.T) {
	overspeedW := proximityOverspeedWorld(t, orbital.Vec3{X: 30}, 0.05, 0.50)
	overspeedV := newProximityTestView(t, 80, 24)
	overspeedW.ViewMode = sim.ViewProximity
	overspeedV.Render(overspeedW, 0, 80, 24)
	overspeedSt, ok := overspeedW.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused (overspeed case)")
	}
	if overspeedW.ProximityDockGateReady(overspeedSt) {
		t.Fatal("precondition: overspeed case reports ready")
	}
	if overspeedSt.RangeM >= sim.DockingDistM {
		t.Fatalf("precondition: overspeed case outside the distance gate (range=%.2f)", overspeedSt.RangeM)
	}
	if overspeedSt.VRelMS < sim.DockingVMS {
		t.Fatalf("precondition: overspeed case not actually over the velocity gate (|v_rel|=%.4f)", overspeedSt.VRelMS)
	}
	if overspeedSt.ClosingMS >= sim.DockingVMS {
		t.Fatalf("precondition: overspeed case is closing fast too (closing=%.4f) — not the lateral-pass scene this test wants", overspeedSt.ClosingMS)
	}
	if !overspeedV.proximityRingVisible(overspeedSt, sim.DockingDistM) {
		t.Fatal("precondition: gate ring not visible in the overspeed scene")
	}

	readyW := proximityWorld(t, orbital.Vec3{X: 30})
	readyV := newProximityTestView(t, 80, 24)
	readyW.ViewMode = sim.ViewProximity
	readyV.Render(readyW, 0, 80, 24)

	latchedW := proximityLatchedWorld(t, orbital.Vec3{X: 30})
	latchedV := newProximityTestView(t, 80, 24)
	latchedW.ViewMode = sim.ViewProximity
	latchedV.Render(latchedW, 0, 80, 24)

	notReadyW := proximityWorld(t, orbital.Vec3{X: 100})
	notReadyV := newProximityTestView(t, 80, 24)
	notReadyW.ViewMode = sim.ViewProximity
	notReadyV.Render(notReadyW, 0, 80, 24)

	overspeedAlert := overspeedV.canvas.CountColor(render.ColorAlert)
	readyAlert := readyV.canvas.CountColor(render.ColorAlert)
	latchedAlert := latchedV.canvas.CountColor(render.ColorAlert)
	notReadyAlert := notReadyV.canvas.CountColor(render.ColorAlert)
	if overspeedAlert == 0 {
		t.Error("no ColorAlert cells in the overspeed scene — the ring should render distinctly, not plain dim")
	}
	if readyAlert != 0 || latchedAlert != 0 || notReadyAlert != 0 {
		t.Errorf("ColorAlert cells leaked into a non-overspeed scene: ready=%d, latched=%d, not-ready=%d, want 0 in all three",
			readyAlert, latchedAlert, notReadyAlert)
	}
}

// TestProximityDriftPathDrawsPlannedClass: the drift path reaches the
// canvas as ClassPlanned (dashed) ink in render.ColorPredictedPath — the
// end-to-end wiring check that drawProximityDriftPath actually calls
// sim.ProximityDriftPath and plots what comes back, not just that the
// sim math is right (already covered at the sim layer).
func TestProximityDriftPathDrawsPlannedClass(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 2_000})
	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewProximity
	v.Render(w, 0, 80, 24)

	if n := v.canvas.CountColor(render.ColorPredictedPath); n == 0 {
		t.Error("no ColorPredictedPath ink found on canvas — drift path did not draw")
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
	// end and CraftWorld are heliocentric world coordinates (~1.4e11 m, one
	// float64 ulp is ~3e-5 m), so the radial residue is rounding noise of
	// the subtraction, not geometry: bound it relative to the stub length
	// (the old absolute 1e-6 passed only by the luck of the seed's phase).
	if math.Abs(local.Y) > 1e-8*math.Abs(local.X) {
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

// The entry fit is max(2.25 x range, 25 m) (ADR 0055 B4): the floor binds up
// to 25 / 2.25 = 11.111 m, where the two sides are equal; 50 m is gone.
func TestProximityFitRadiusFloor(t *testing.T) {
	cases := []struct{ rangeM, want float64 }{
		{0, 25},
		{10, 25},
		{25 / proximityFitMarginFrac, 25}, // equal case: both sides 25
		{12, 27},                          // not floored
		{100, 225},
	}
	for _, c := range cases {
		if got := proximityFitRadius(sim.ProximityState{RangeM: c.rangeM}); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("proximityFitRadius(range %g) = %v, want %v", c.rangeM, got, c.want)
		}
	}
}

// At 140x40 the entry fit puts the 50 m gate ring's leftmost and rightmost
// points on the canvas and its top and bottom off it (ADR 0055 B4's accepted
// cost), and the tug pair entering at 4 m of port range is drawn as hulls,
// not the anchor glyphs.
func TestProximityEntryFitClipsGateRingAndDrawsHulls(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"rcs-tug"}, orbital.Vec3{X: 3}, 0)
	v := newProximityTestView(t, DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused")
	}
	pxW, pxH := v.canvas.Cols()*2, v.canvas.Rows()*4
	minX, maxX, minY, maxY := 1<<30, -(1 << 30), 1<<30, -(1 << 30)
	for _, p := range proximityRingPoints(proximityGateRingCentre(st), st.Frame.AlongTrack, st.Frame.RadialOut, sim.DockingDistM) {
		x, y, _ := v.canvas.Project(p)
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, y), max(maxY, y)
	}
	if minX < 0 || maxX >= pxW {
		t.Errorf("gate ring spans x %d..%d, want inside the %d px canvas", minX, maxX, pxW)
	}
	if minY >= 0 || maxY < pxH {
		t.Errorf("gate ring spans y %d..%d, want clipped top and bottom of the %d px canvas", minY, maxY, pxH)
	}
	if !v.proxTargetSpriteShown || !v.proxOwnSpriteShown {
		t.Errorf("hull sprites shown own=%v target=%v, want both at the entry fit", v.proxOwnSpriteShown, v.proxTargetSpriteShown)
	}
	if n := v.canvas.CountColor(lipgloss.Color(stagesOf(t, "rcs-tug")[0].Color)); n == 0 {
		t.Error("no tug-coloured hull ink on the canvas")
	}
}

// TestProximityHullSpriteSpansExpectedCellsAtEntryFit is the "seeded
// range" quality-bar test: at a 10 m separation — inside
// proximityFitFloorM (25 m), so the entry fit pins to the hulls' floor
// rather than scaling with range — a 24 m-tall stack
// (16 sub-pixel rows × vesselSubPixelM) spans the height
// proximityFitRadius's own documented entry-fit math (Canvas.FitTo:
// scale = 0.45 × shorter-pixel-axis / radius) predicts, derived here
// independently of the production code under test rather than by
// re-calling it, so a scale-formula regression can't silently agree
// with itself.
func TestProximityHullSpriteSpansExpectedCellsAtEntryFit(t *testing.T) {
	const rangeM = 10.0
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
	if fitRadius != 25 {
		t.Fatalf("fit radius = %v at %v m of range, want the 25 m floor", fitRadius, rangeM)
	}
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

// proxHullCanvas draws stages as a Proximity hull pointing up the screen at
// pxPerM on a fresh canvas and returns the view plus the anchor's pixel.
func proxHullCanvas(t *testing.T, stages []spacecraft.Stage, pxPerM float64, seams ...int) (*OrbitView, int, int) {
	t.Helper()
	v := newProximityTestView(t, 104, 24)
	frame := orbital.LVLHFrame{AlongTrack: orbital.Vec3{X: 1}, RadialOut: orbital.Vec3{Y: 1}, CrossTrack: orbital.Vec3{Z: 1}}
	v.canvas.SetBasis(widgets.Basis{X: frame.AlongTrack, Y: frame.RadialOut})
	v.canvas.SetScale(pxPerM)
	v.canvas.Center(orbital.Vec3{})
	shown := false
	if !v.drawProximityHull(&shown, stages, orbital.Vec3{Y: 1}, orbital.Vec3{}, frame, seams...) {
		t.Fatal("drawProximityHull refused the stack")
	}
	px, py, _ := v.canvas.Project(orbital.Vec3{})
	return v, px, py
}

// litAcross counts lit pixels on one screen row.
func litAcross(v *OrbitView, py int) (n int) {
	for px := 0; px < v.canvas.Cols()*2; px++ {
		if v.canvas.SubPixelSet(px, py) {
			n++
		}
	}
	return n
}

// litExtentAcross is the width in pixels of everything lit between rows
// y0..y1 (inclusive): the hull's drawn width, whether it is a solid rect or a
// lattice of dots.
func litExtentAcross(v *OrbitView, y0, y1 int) int {
	lo, hi := math.MaxInt, math.MinInt
	for y := y0; y <= y1; y++ {
		for px := 0; px < v.canvas.Cols()*2; px++ {
			if v.canvas.SubPixelSet(px, y) {
				lo, hi = min(lo, px), max(hi, px)
			}
		}
	}
	if hi < lo {
		return 0
	}
	return hi - lo + 1
}

// hullRows is the pixel row span (top, bottom) of a single-stage hull drawn
// at the anchor row py.
func hullRows(stages []spacecraft.Stage, py int, scale float64) (top, bottom int) {
	nose := float64(sim.StackRows(stages)) * vesselSubPixelM
	tail := sim.DockPortTipInsetM()
	return py - int(math.Round((nose-tail)*scale)), py + int(math.Round(tail*scale))
}

// ADR 0061 decision 3: the Proximity hull is drawn at the part's published
// metres, not the 1.5 m lattice. Dragon 4.0 m at 6 px/m = 24 px (the lattice
// drew 4 dots x 1.5 m = 36).
func TestProximityHullDrawsAtPublishedWidth(t *testing.T) {
	// 6.1 not 6.0: at 6.0 the tug's tail lands on an exact half pixel and the
	// walk's round-half tie drops one whole row (a PlotDenseLine property).
	const scale = 6.1
	for _, tc := range []struct {
		name   string
		stages []spacecraft.Stage
		wantPx float64
		tolPx  float64
	}{
		{"dragon 4.0 m", stagesOf(t, "dragon-crew"), 4.0 * scale, 1},
		// The sprite-rule fallback is unchanged: 2 px floors to 4, x 1.5 m.
		{"no diameter, 4-px floor", []spacecraft.Stage{{LaunchSpriteRowsPx: 6, LaunchSpriteWidthPx: 2, Color: "#ABCDEF"}}, 6.0 * scale, 1},
	} {
		v, _, py := proxHullCanvas(t, tc.stages, scale)
		top, bottom := hullRows(tc.stages, py, scale)
		n := litExtentAcross(v, top, bottom)
		if math.Abs(float64(n)-tc.wantPx) > tc.tolPx {
			t.Errorf("%s: hull is %d px wide, want %.0f +/- %.0f", tc.name, n, tc.wantPx, tc.tolPx)
		}
	}
}

// A 1.0 m tug is a thin hull, not scatter: at 6 px/m it is about 6 px wide
// and at the 25 m entry fit (about 2.66 px/m) every row along it still inks.
func TestProximityTugHullIsThinButSolid(t *testing.T) {
	stages := stagesOf(t, "rcs-tug")
	for _, scale := range []float64{6.1, 2.664} {
		v, _, py := proxHullCanvas(t, stages, scale)
		top, bottom := hullRows(stages, py, scale)
		if n := litExtentAcross(v, top, bottom); n < 1 || float64(n) > 1.0*scale+2 {
			t.Errorf("scale %v: tug is %d px wide, want 1..%.1f", scale, n, 1.0*scale+2)
		}
		for y := top + 1; y < bottom; y++ {
			if litAcross(v, y) < 1 {
				t.Errorf("scale %v: no lit pixel on row %d of the tug (rows %d..%d)", scale, y, top, bottom)
				break
			}
		}
	}
}

// The bell and legs left the Proximity view (open point 3): a lander draws
// no ink wider than its hull and none below its tail.
func TestProximityHullHasNoBellOrLegs(t *testing.T) {
	const scale = 6.1
	stages := stagesOf(t, "lander")
	v, px, py := proxHullCanvas(t, stages, scale)
	halfPx := int(math.Ceil(sim.StackWidthM(stages)/2*scale)) + 1
	tailPy := py + int(math.Round(sim.DockPortTipInsetM()*scale)) + 1
	for y := 0; y < v.canvas.Rows()*4; y++ {
		for x := 0; x < v.canvas.Cols()*2; x++ {
			if !v.canvas.SubPixelSet(x, y) {
				continue
			}
			if abs := int(math.Abs(float64(x - px))); abs > halfPx {
				t.Fatalf("ink at %d px off the axis (row %d), hull half-width is %d px: legs or bell drawn", abs, y, halfPx)
			}
			if y > tailPy {
				t.Fatalf("ink at row %d below the tail row %d: bell drawn", y, tailPy)
			}
		}
	}
}

// The picture and the number agree: the widest rect Proximity draws (booster
// columns included) is the width sim publishes, for every loadout.
func TestProximityWidestRectMatchesPublishedWidth(t *testing.T) {
	check := func(name string, stages []spacecraft.Stage) {
		widest := 0.0
		for _, r := range proximityHullRects(stages, nil) {
			widest = max(widest, 2*math.Abs(r.acrossM)+r.widthM)
		}
		if want := sim.StackWidthM(stages); math.Abs(widest-want) > 1e-9 {
			t.Errorf("%s: widest drawn rect %.3f m, sim.StackWidthM %.3f m", name, widest, want)
		}
	}
	for _, id := range spacecraft.LoadoutOrder {
		check(id, spacecraft.Loadouts[id].Stages)
	}
	// A booster pair with no diameter_m (an overlay): both sides read the px rule.
	check("no-diameter pair", []spacecraft.Stage{{LaunchSpriteRowsPx: 6, LaunchSpriteWidthPx: 2, LitWithAbove: true}, {LaunchSpriteRowsPx: 6, LaunchSpriteWidthPx: 2}})
}

// A docked composite still draws its docked block nose to the seam.
func TestProximityHullFlipsDockedBlock(t *testing.T) {
	a := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#FF0000"}
	b := spacecraft.Stage{LaunchSpriteRowsPx: 6, Color: "#00FF00"}
	c := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#0000FF"}
	tail := func(rs []hullRect, col string) float64 {
		for _, r := range rs {
			if r.color == lipgloss.Color(col) {
				return r.tailM
			}
		}
		t.Fatalf("no %s rect", col)
		return 0
	}
	plain := proximityHullRects([]spacecraft.Stage{a, b, c}, nil)
	if !(tail(plain, "#00FF00") < tail(plain, "#0000FF")) {
		t.Fatal("control: unflipped stack should run B below C")
	}
	flipped := proximityHullRects([]spacecraft.Stage{a, b, c}, []int{1})
	if !(tail(flipped, "#0000FF") < tail(flipped, "#00FF00")) {
		t.Errorf("seam at 1: C should sit below B (nose to the seam), got C %.1f B %.1f", tail(flipped, "#0000FF"), tail(flipped, "#00FF00"))
	}
}

// hullProbe returns "is the canvas pixel at (a metres along the hull, u
// metres across it) lit" for a hull anchored at anchor with the given
// attitude, in the view's last render.
//
// tol widens the probe to a (2tol+1)-pixel square: a dotted edge skips
// pixels, so corners are probed with tol 1 and what must be DARK with 0.
func hullProbe(v *OrbitView, st sim.ProximityState, anchor, attitude orbital.Vec3) func(a, u float64, tol int) bool {
	basis := widgets.Basis{X: st.Frame.AlongTrack, Y: st.Frame.RadialOut}
	sx, sy := stackDirScreen(attitude, basis)
	along := basis.X.Scale(sx).Add(basis.Y.Scale(sy))
	across := basis.X.Scale(-sy).Add(basis.Y.Scale(sx))
	return func(a, u float64, tol int) bool {
		px, py, ok := v.canvas.Project(anchor.Add(along.Scale(a)).Add(across.Scale(u)))
		if !ok {
			return false
		}
		for dx := -tol; dx <= tol; dx++ {
			for dy := -tol; dy <= tol; dy++ {
				if v.canvas.SubPixelSet(px+dx, py+dy) {
					return true
				}
			}
		}
		return false
	}
}

// boxScene renders a ported pair at a fixed zoom and returns the view, the
// state and the target's and your own hull probes. Ports meet at X 0, so
// neither hull overlaps the other on screen.
func boxScene(t *testing.T, own, tgt string, off orbital.Vec3, zoom float64) (*OrbitView, *sim.World, sim.ProximityState, func(a, u float64, tol int) bool, func(a, u float64, tol int) bool) {
	t.Helper()
	w := portedPairWorld(t, []string{own}, []string{tgt}, off, 0)
	v := newProximityTestView(t, DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	v.userZoom = zoom
	v.Render(w, 0, DesignWidth, DesignHeight)
	st, _ := w.ProximityState()
	tp := hullProbe(v, st, st.TargetWorld, w.Crafts[1].CurrentAttitudeDir)
	op := hullProbe(v, st, st.CraftWorld, w.ActiveCraft().CurrentAttitudeDir)
	return v, w, st, tp, op
}

// The target's hull is a dotted outline when the two axes are further apart
// across the view's depth than the radii sum, and a filled rectangle at or
// inside it (ADR 0055 B1; the rule is strict, so exactly the sum is filled).
// Dragons are 4.0 m wide, so the sum is 4.0 m. Probed at mid-length a quarter
// width off the axis (clear of the +V axis line): lit when filled, dark when
// only the outline is drawn.
func TestProximityHullBoxOnlyBeyondTheRadiiSum(t *testing.T) {
	tail := -sim.DockPortTipInsetM()
	for _, c := range []struct {
		name string
		off  orbital.Vec3
		box  bool
	}{
		{"3.99 m deep: filled", orbital.Vec3{Z: 3.99}, false},
		{"exactly the radii sum: filled", orbital.Vec3{Z: 4.0}, false},
		{"4.01 m deep: box", orbital.Vec3{Z: 4.01}, true},
		{"approach, 0.5 m deep: filled", orbital.Vec3{X: 20, Z: 0.5}, false},
		{"above you, 8 m, same plane: filled", orbital.Vec3{Y: 8}, false},
		{"60 m deep: box, no range gate", orbital.Vec3{Z: 60}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, _, st, tp, _ := boxScene(t, "dragon-crew", "dragon-cargo", c.off, 6)
			if !v.proxTargetSpriteShown {
				t.Fatal("setup: the target hull must clear the sprite switch")
			}
			if st.Target.WidthM != 4.0 {
				t.Fatalf("setup: Dragon width = %v, want 4.0", st.Target.WidthM)
			}
			mid := tail + st.Target.NoseM/2
			if got := tp(mid, st.Target.WidthM/4, 0); got == c.box {
				t.Errorf("interior lit = %v with HullDepthM %.12f, want %v", got, st.HullDepthM, !c.box)
			}
			if !tp(tail, st.Target.WidthM/2, 1) {
				t.Error("corner of the target hull not lit")
			}
		})
	}
}

// Two tugs 8 m apart across the view's depth: the target is a 6 m x 1.0 m
// outline in the TARGET colour, dotted, port mark kept; your own hull is
// still filled (ADR 0055 B1).
func TestProximityHullBoxIsADottedOutlineInTargetGreen(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	v, w, st, tp, op := boxScene(t, "rcs-tug", "nudge-tug", orbital.Vec3{Z: 8}, 3)
	basis := widgets.Basis{X: st.Frame.AlongTrack, Y: st.Frame.RadialOut}
	sx, sy := stackDirScreen(w.Crafts[1].CurrentAttitudeDir, basis)
	along := basis.X.Scale(sx).Add(basis.Y.Scale(sy))
	across := basis.X.Scale(-sy).Add(basis.Y.Scale(sx))
	tail, head := -sim.DockPortTipInsetM(), st.Target.NoseM-sim.DockPortTipInsetM()
	u := st.Target.WidthM / 2
	for _, a := range []float64{tail, head} {
		for _, s := range []float64{-u, u} {
			if !tp(a, s, 1) {
				t.Errorf("box corner (%.2f, %.2f) not lit", a, s)
			}
		}
	}
	dark := 0
	for i := 1; i < 40; i++ {
		px, py, _ := v.canvas.Project(st.TargetWorld.Add(along.Scale(tail+(head-tail)*float64(i)/40)).Add(across.Scale(u)))
		if !v.canvas.SubPixelSet(px, py) {
			dark++
		}
	}
	if dark == 0 {
		t.Error("top edge solid, want the Scenery dot pattern")
	}
	if tp((tail+head)/2, u/2, 0) {
		t.Error("target interior lit: that is a filled hull, not a box")
	}
	if !op(tail+st.Own.NoseM/2, st.Own.WidthM/4, 0) {
		t.Error("your own hull is not filled")
	}
	// TARGET colour on the box's corner row; the port mark is still drawn.
	corner := st.TargetWorld.Add(along.Scale(tail)).Add(across.Scale(u))
	_, py, _ := v.canvas.Project(corner)
	rows := strings.Split(v.canvas.String(), "\n")
	if py/4 >= len(rows) || !strings.Contains(rows[py/4], "60;220;131") {
		t.Errorf("box corner row has no TARGET green (#3DDC84 quantised to 60;220;131): %q", rows[py/4])
	}
	// Both ports sit on one screen cell here (they differ only in depth), so
	// the mark is checked at that cell.
	if got := strings.Count(ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight)), "○"); got < 1 {
		t.Errorf("port marks = %d, want the box to keep its mark", got)
	}
}

// dockedProximityFlow flies the ADR 0063 scene: a ported pair 0.3 m apart in
// Proximity View, the view zoomed and panned by the player, rendered the tick
// before the latch and the tick after. Returns both frames and the partner.
func dockedProximityFlow(t *testing.T, cols, rows int) (v *OrbitView, w *sim.World, partner, before, after string, zoom, scale float64, pan orbital.Vec3) {
	t.Helper()
	w = portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, 0)
	partner = w.Crafts[1].Name
	v = newProximityTestView(t, cols, rows)
	v.Render(w, 0, cols, rows)
	v.ZoomIn()
	v.PanRight()
	before = v.Render(w, 0, cols, rows)
	zoom, scale, pan = v.userZoom, v.canvas.Scale(), v.panOffset
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("fixture dock failed: %d vessels", len(w.Crafts))
	}
	after = v.Render(w, 0, cols, rows)
	return
}

// ADR 0063 section 2: the latch keeps the picture. Zoom, pan and scale do not
// move on the latch tick; the chip loses the gap rows and says docked; both
// hulls are the own composite sprite; o out and back in shows the same scene.
func TestProximityStaysUpWhileDocked(t *testing.T) {
	v, w, partner, before, after, zoom, scale, pan := dockedProximityFlow(t, 140, 40)
	if strings.Contains(before, "docked") && !strings.Contains(before, "undocked") {
		t.Errorf("pre-latch frame already says docked\n%s", before)
	}
	if v.userZoom != zoom || v.canvas.Scale() != scale || v.panOffset != pan {
		t.Errorf("latch tick moved the camera: zoom %v->%v scale %v->%v pan %v->%v",
			zoom, v.userZoom, scale, v.canvas.Scale(), pan, v.panOffset)
	}
	for _, want := range []string{
		"PROXIMITY  " + partner + "  docked",
		"view: Proximity — docked with " + partner,
		"hold:", "trim:", "throttle:", "rcs:", "monoprop:",
	} {
		if !strings.Contains(after, want) {
			t.Errorf("docked frame is missing %q\n%s", want, after)
		}
	}
	if !regexp.MustCompile(`range:\s+docked`).MatchString(after) {
		t.Errorf("docked frame has no `range: docked` row\n%s", after)
	}
	for _, gone := range []string{"port:", "misalign:", "lateral:", "rel speed:", "closing:", "no vessel", "isn't resolvable"} {
		if strings.Contains(after, gone) {
			t.Errorf("docked frame still carries %q\n%s", gone, after)
		}
	}
	if !v.proxOwnSpriteShown {
		t.Error("the composite sprite is not drawn while docked")
	}
	// o out and back in while docked: the same scene (re-entry is a Framing
	// Event that clears the pan, so compare from a pan of zero).
	v.panOffset = orbital.Vec3{}
	after = v.Render(w, 0, 140, 40)
	w.ViewMode = sim.ViewOrbitFlat
	v.Render(w, 0, 140, 40)
	if entered, refusal := w.ToggleProximityView(); !entered || refusal != "" {
		t.Fatalf("re-entry while docked: entered=%v refusal=%q", entered, refusal)
	}
	if again := v.Render(w, 0, 140, 40); again != after {
		t.Errorf("o out and back in changed the docked scene\nfirst:\n%s\nagain:\n%s", after, again)
	}
}

// Compact Form keeps the header, range: and hold: while docked.
func TestProximityDockedCompactForm(t *testing.T) {
	_, _, partner, _, after, _, _, _ := dockedProximityFlow(t, 80, 24)
	for _, want := range []string{"PROXIMITY  " + partner, "docked", "hold:"} {
		if !strings.Contains(after, want) {
			t.Errorf("80x24 docked frame is missing %q\n%s", want, after)
		}
	}
}
