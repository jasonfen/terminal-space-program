package screens

import (
	"fmt"
	"math"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// Proximity View (ADR 0043) — the screen half. The canvas stops being a
// window on the world frame and becomes a window on the TARGET: the
// target sits at the centre, its orbital prograde runs screen-right and
// the primary lies screen-down (orbital.LVLHBasis owns that math), and
// the active vessel is drawn at its true relative position and scale.
//
// This is a ViewMode, not a screen. It reuses OrbitView's canvas,
// Framing-Event machinery, navball, chips and title bar, so every flight
// key and every readout behaves exactly as it does on the map — the
// picture changes, the cockpit does not.
//
// This slice draws deliberately little: two glyphs, the V-bar / R-bar
// cross, and a readout chip. The drift path, range rings and dock gate
// are the follow-up cue slice; hull sprites the one after.

// proximityFitMarginFrac is how much room past the current range the
// entry fit leaves. Canvas.FitTo frames a circle of the given radius at
// ~90% of the SHORTER pixel axis, and the terminal's shorter axis is the
// vertical one, so the multiplier here is what decides where the chaser
// lands in both directions: it sits at 0.45/margin of the half-height,
// and at the same pixel count horizontally.
//
// 2.25 was measured, not guessed. Fitting to the bare range (margin 1)
// puts the chaser at 90% of the half-height — against the edge for a
// radial offset — and 15 cells left of centre at 80 columns, which is
// underneath the pinned VESSEL chip, so on a textbook V-bar approach the
// player's own vessel was drawn where they could not see it. 2.25 lands
// it at ~40% of the half-height and ~8 cells from centre, clear of the
// chip at the minimum supported terminal, and leaves the room ahead of
// the chaser that the drift path and range rings need in the cue slice.
const proximityFitMarginFrac = 2.25

// proximityFitFloorM keeps the entry fit sane at contact range. Range
// goes to metres as a dock completes, and a scale derived from that would
// magnify sub-metre integrator jitter into a picture that shakes. 50 m is
// the game's own DOCK READY radius — at that range the whole encounter is
// already in frame, so it is the natural floor.
const proximityFitFloorM = 50.0

// proximityFitRadius resolves the Framing-Event fit for Proximity View:
// derived from the range at the moment of entry, never re-derived after.
// The Camera Contract (ADR 0021, unchanged by 0043) is what forbids the
// obvious-looking alternative of tracking range continuously — a camera
// that zooms itself as you close is a camera the player has stopped
// owning, and the picture would never hold still enough to read drift
// against.
func proximityFitRadius(st sim.ProximityState) float64 {
	r := st.RangeM * proximityFitMarginFrac
	if r < proximityFitFloorM {
		r = proximityFitFloorM
	}
	return r
}

// renderProximity composes the close-range frame. Mirrors Render's shape:
// a Framing-Event block, the scene, then the shared overlay tail.
func (v *OrbitView) renderProximity(w *sim.World, totalCols, totalRows int) string {
	st, sceneOK := w.ProximityState()

	// Framing Event resolution (ADR 0021 A), Proximity's own context:
	// entering the view, changing system, or retargeting. Range closing is
	// pointedly NOT in this list — the fit is written once, on entry, and
	// then belongs to the player.
	slot := proximityZoomSlot(w)
	craftHere := w.CraftVisibleHere()
	arrivingFrom := v.lastViewMode
	contextChanged := v.lastSystemIdx != w.SystemIdx ||
		v.lastViewMode != sim.ViewProximity ||
		v.zoomSlot != slot ||
		v.lastCraftHere != craftHere
	if contextChanged || !v.fitted {
		if contextChanged && arrivingFrom != sim.ViewProximity {
			// Park the map's pan for the return leg (see mapPanOffset).
			v.mapPanOffset = v.panOffset
			v.mapPanSaved = true
		}
		v.lastSystemIdx = w.SystemIdx
		v.lastViewMode = sim.ViewProximity
		v.lastFocus = w.Focus
		v.lastCraftHere = craftHere
		v.zoomSlot = slot
		v.fitted = true
		if sceneOK {
			v.canvas.FitTo(proximityFitRadius(st))
			v.baseScale = v.canvas.Scale()
		}
		if contextChanged {
			v.userZoom = v.zoomMemoryFor(slot)
			v.panOffset = orbital.Vec3{}
			v.InspectClear()
		}
		v.burnFrozenCenter = nil
	}

	v.canvas.Clear()
	// Inspect is a map affordance — it steps through what the map drew.
	// The proximity scene draws two glyphs the player already knows the
	// identity of, so the set stays empty and [j] is a no-op here rather
	// than a highlight with nothing to say.
	v.resetInspectables()

	if sceneOK {
		v.canvas.SetBasis(widgets.Basis{X: st.Frame.AlongTrack, Y: st.Frame.RadialOut})
		v.canvas.SetScale(v.baseScale * v.userZoom)
		// The target is the centre. Pan displaces the view exactly as it
		// does on the map — the centre keeps tracking, just offset.
		v.canvas.Center(st.TargetWorld.Add(v.panOffset))
		v.drawProximityAxes(w, st)
		v.drawProximityRangeRings(w, st)
		v.drawProximityDockGate(w, st)
		v.drawProximityDriftPath(w, st)
		v.drawProximityVessels(w, st)
		v.drawProximityVelocityVector(st)
	}
	// The no-target case draws nothing on the canvas on purpose: the
	// explanation and both ways out ride the PROXIMITY chip, which paints
	// an opaque block after everything else. A centred canvas label there
	// was measurably worse — the navball clipped it to "no vessel tar",
	// and a half-erased refusal reads as a bug rather than an answer.

	// The bottom-border label carries the axis legend as well as the view
	// name. The on-canvas ▴+R / +V▸ markers sit where the bars are, which
	// is where they read best — but corner chips and the navball can cover
	// any of them at 80×24, and an unlabelled frame is a frame whose
	// orientation the player has to guess. This row is the one place
	// nothing composites over.
	label := "view: proximity"
	if sceneOK {
		label += " — " + st.TargetName + "   →+V prograde  ↓" + proximityPrimaryName(w)
	}
	v.canvas.SetCellLabelColored(0, v.canvas.Rows()-1, label, v.theme.Primary.GetForeground())

	canvasStr := v.canvas.String()

	// The shared overlay tail, identical to the map's: same navball, same
	// chips, same click routing. This is what "it's a view, not a screen"
	// means in code.
	v.navballControls = v.navballControls[:0]
	cCols, cRows := v.canvas.Cols(), v.canvas.Rows()
	if !v.declutter {
		canvasStr = v.composeNavballOverlay(w, canvasStr, cCols, cRows, true)
	}
	navballReserved := v.navballReservedRows(w, cCols, cRows)
	canvasStr = v.composeChips(canvasStr, cCols, cRows, navballReserved, 1, 2, v.assembleChips(w))
	canvasStr = v.composeInspectChip(canvasStr, cCols, cRows)

	canvasPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.theme.Primary.GetForeground()).
		Render(canvasStr)

	craftChip := ""
	if n := len(w.Crafts); n > 1 {
		craftChip = fmt.Sprintf(" — VESSEL %d/%d", w.ActiveCraftIdx+1, n)
	}
	title := v.renderTitleBar(w.System().Name+craftChip, w, totalCols)
	return title + "\n" + canvasPanel
}

// drawProximityAxes lays the V-bar / R-bar cross through the target and
// names its ends. Without it the frame is a claim the picture can't back
// up: two dots on a black field say nothing about which way is prograde.
// Drawn Scenery-class (dotted, faint) because it is backdrop — the
// vessels and, later, the drift path are the subject.
func (v *OrbitView) drawProximityAxes(w *sim.World, st sim.ProximityState) {
	scale := v.canvas.Scale()
	if scale <= 0 {
		return
	}
	// Reach past the canvas diagonal so the bars run edge to edge whatever
	// the pan offset is.
	reach := (float64(v.canvas.Cols()*2) + float64(v.canvas.Rows()*4)) / scale
	c := st.TargetWorld
	v.canvas.PlotPolylineClass([]orbital.Vec3{
		c.Sub(st.Frame.AlongTrack.Scale(reach)),
		c.Add(st.Frame.AlongTrack.Scale(reach)),
	}, render.ColorDim, widgets.ClassScenery)
	v.canvas.PlotPolylineClass([]orbital.Vec3{
		c.Sub(st.Frame.RadialOut.Scale(reach)),
		c.Add(st.Frame.RadialOut.Scale(reach)),
	}, render.ColorDim, widgets.ClassScenery)

	// End labels, in the vocabulary a rendezvous pilot already has: +V is
	// ahead along the target's track, +R is up and away from the primary,
	// so the primary itself is named at the bottom of the R-bar.
	//
	// Placed HALFWAY between the target and each edge rather than at the
	// edges themselves: the corners belong to the chips, which composite
	// last and would simply erase an edge-anchored label. The bottom
	// border row carries a duplicate legend for the cases where even this
	// gets covered — see renderProximity.
	cols, rows := v.canvas.Cols(), v.canvas.Rows()
	tgtCol, tgtRow, _ := v.canvasCell(st.TargetWorld)
	tgtCol = clampInt(tgtCol, 0, cols-1)
	tgtRow = clampInt(tgtRow, 0, rows-1)
	dim := v.theme.Dim.GetForeground()

	leftLabel, rightLabel := "◂−V", "+V▸"
	v.canvas.SetCellLabelColored(clampInt(tgtCol/2, 0, cols-1), tgtRow, leftLabel, dim)
	v.canvas.SetCellLabelColored(
		clampInt(tgtCol+(cols-tgtCol)/2, 0, cols-len([]rune(rightLabel))), tgtRow, rightLabel, dim)

	downLabel := "▾" + proximityPrimaryName(w)
	v.canvas.SetCellLabelColored(clampInt(tgtCol-1, 0, cols-3), clampInt(tgtRow/2, 0, rows-1), "▴+R", dim)
	v.canvas.SetCellLabelColored(
		clampInt(tgtCol-1, 0, cols-len([]rune(downLabel))),
		clampInt(tgtRow+(rows-tgtRow)/2, 0, rows-2), downLabel, dim)
}

// proximityPrimaryName is the body the R-bar points down at — the label
// that turns "screen-down" from a convention into a place.
func proximityPrimaryName(w *sim.World) string {
	if c := w.ActiveCraft(); c != nil {
		return c.Primary.EnglishName
	}
	return "primary"
}

// Cue slice (issue #348 §1, follow-up to the Proximity View core in PR
// #357): the drift path, range rings, dock gate, and v_rel vector stub.
// Hull sprites at true scale are the slice after this one.

// proximityDriftHorizonFloorMps floors the relative speed
// proximityDriftHorizonFor divides by. A pair sitting almost exactly
// matched in velocity (the stable end-state of a good approach) would
// otherwise make "time to cross the framed radius" blow up toward +Inf;
// the floor just means that case takes the horizon ceiling instead of a
// division that has to be special-cased.
const proximityDriftHorizonFloorMps = 0.01

// proximityDriftHorizonFor picks how far ahead the no-burn drift path
// looks: enough time, at the CURRENT |v_rel|, to cross roughly the
// radius the Framing Event fitted — so a tight zoom (close range, small
// frame) gets a proportionally short forecast and a wide one gets a
// longer one, instead of a fixed window that either vanishes into a dot
// at wide zoom or rockets off-canvas at close range. Clamped to
// [ProximityDriftHorizonMin, ProximityDriftHorizonMax] (issue #348 §1's
// own words: "a few minutes to tens of minutes"). Stated here, not
// re-derived per call site, so the PR has exactly one place to point at
// for "why this horizon."
func proximityDriftHorizonFor(v *OrbitView, st sim.ProximityState) time.Duration {
	scale := v.canvas.Scale()
	if scale <= 0 {
		return sim.ProximityDriftHorizonMin
	}
	halfSpanPx := float64(v.canvas.Cols() * 2)
	if h := float64(v.canvas.Rows() * 4); h < halfSpanPx {
		halfSpanPx = h
	}
	halfSpanPx /= 2
	halfSpanM := halfSpanPx / scale

	speed := st.VRelMS
	if speed < proximityDriftHorizonFloorMps {
		speed = proximityDriftHorizonFloorMps
	}
	horizon := time.Duration(halfSpanM / speed * float64(time.Second))
	if horizon < sim.ProximityDriftHorizonMin {
		return sim.ProximityDriftHorizonMin
	}
	if horizon > sim.ProximityDriftHorizonMax {
		return sim.ProximityDriftHorizonMax
	}
	return horizon
}

// drawProximityDriftPath draws the no-burn forecast: where the active
// vessel drifts relative to the target over the next few minutes if
// nobody touches the throttle. ClassPlanned (dashed) — a plan, not a
// live orbit (ADR 0041 §2), the same vocabulary the ascent/descent arcs
// use for "what happens if flight continues exactly as it is now."
//
// sim.ProximityDriftPath hands back LVLH-LOCAL coordinates, each
// resolved in the TARGET's frame at that future instant (see its doc
// comment for why that matters). Re-anchoring every point at TODAY's
// target position using TODAY's frame axes is what turns "a series of
// co-rotating-frame offsets" into a single curve on the canvas Camera
// Contract holds fixed for this render — it's the same picture a pilot
// re-entering this view at each future instant would see, drawn as one
// continuous line from where they are now.
func (v *OrbitView) drawProximityDriftPath(w *sim.World, st sim.ProximityState) {
	horizon := proximityDriftHorizonFor(v, st)
	samples, ok := w.ProximityDriftPath(horizon)
	if !ok || len(samples) < 2 {
		return
	}
	pts := make([]orbital.Vec3, len(samples))
	for i, s := range samples {
		pts[i] = st.TargetWorld.
			Add(st.Frame.AlongTrack.Scale(s.Local.X)).
			Add(st.Frame.RadialOut.Scale(s.Local.Y)).
			Add(st.Frame.CrossTrack.Scale(s.Local.Z))
	}
	v.canvas.PlotPolylineClass(pts, render.ColorPlannedNode, widgets.ClassPlanned)
}

// proximityRingRadii are the log-spaced range rings issue #348 §1 asks
// for: 10 km, 1 km, 100 m — a coarse decade ladder a pilot can eyeball
// distance against without reading the range chip. The 50 m dock gate
// (sim.DockingDistM) is drawn separately by drawProximityDockGate: it
// isn't a fourth entry here because its colour is gated on |v_rel| too,
// not radius alone.
var proximityRingRadii = []float64{10_000, 1_000, 100}

// proximityRingSegments is how many straight edges approximate each
// ring. The braille canvas's own pixel grid (cols*2 × rows*4) is coarse
// enough at any terminal size this game actually supports that this many
// segments is indistinguishable from a true circle — unlike
// DrawEllipseClass's adaptive flattening, this never needs to scale with
// projected radius because a range ring is always a perfect circle in
// the LVLH plane, never foreshortened by a tilted orbit.
const proximityRingSegments = 96

// proximityRingMinPixels is the same floor drawSOIRing already uses
// (soiRingMinPixels, internal/tui/screens/orbit.go): below this a ring's
// outline degenerates into scattered noise rather than a readable
// circle, so it's skipped entirely rather than drawn wrong.
const proximityRingMinPixels = 4

// proximityRingPoints samples a closed circle of radiusM around center,
// in the plane spanned by e1/e2 (both unit length, mutually orthogonal)
// — the raw material PlotPolylineClass turns into a dotted or dashed
// ring via its own arc-length-stable dash machinery. Closed by repeating
// the first point.
func proximityRingPoints(center, e1, e2 orbital.Vec3, radiusM float64) []orbital.Vec3 {
	pts := make([]orbital.Vec3, 0, proximityRingSegments+1)
	for i := 0; i <= proximityRingSegments; i++ {
		theta := 2 * math.Pi * float64(i) / float64(proximityRingSegments)
		pts = append(pts, center.
			Add(e1.Scale(radiusM*math.Cos(theta))).
			Add(e2.Scale(radiusM*math.Sin(theta))))
	}
	return pts
}

// proximityRingVisible reports whether a ring of radiusM around
// st.TargetWorld would show at least a readable arc on the current
// canvas: O(1) on the projected radius against the viewport rectangle,
// so a ring far outside the current zoom never reaches the O(segments)
// polyline walk at all (the same budget-before-you-draw discipline
// drawSOIRing already uses for the SOI Ring).
//
// A ring is visible exactly when its CIRCUMFERENCE crosses the canvas
// rectangle — not merely when its bounding box overlaps it, which would
// wrongly call a ring "visible" when it has zoomed in so far past the
// canvas that the whole screen sits deep inside it and the drawn line
// never comes near a single pixel (the log rings are exactly the case
// this matters for: at close range the 10 km/1 km rings dwarf the framed
// view). The standard circle-vs-rectangle test: the circle crosses the
// rect iff the rect's NEAREST point to the centre is inside-or-on the
// circle and its FARTHEST point (always a corner) is outside-or-on it.
func (v *OrbitView) proximityRingVisible(st sim.ProximityState, radiusM float64) bool {
	scale := v.canvas.Scale()
	if scale <= 0 {
		return false
	}
	pxR := radiusM * scale
	if pxR < proximityRingMinPixels {
		return false
	}
	cx, cy, _ := v.canvas.Project(st.TargetWorld)
	fcx, fcy := float64(cx), float64(cy)
	pxW, pxH := float64(v.canvas.Cols()*2), float64(v.canvas.Rows()*4)

	nearX := math.Max(0, math.Min(fcx, pxW))
	nearY := math.Max(0, math.Min(fcy, pxH))
	if math.Hypot(nearX-fcx, nearY-fcy) > pxR {
		return false // the whole canvas lies outside the ring
	}
	farX := math.Max(fcx, pxW-fcx)
	farY := math.Max(fcy, pxH-fcy)
	if math.Hypot(farX, farY) < pxR {
		return false // the whole canvas lies inside the ring; the line never crosses it
	}
	return true
}

// drawProximityRangeRings draws the log-spaced range rings around the
// target, skipping any ring that doesn't clear proximityRingMinPixels or
// falls entirely outside the canvas at the current zoom. ClassScenery
// (dotted, faint) — backdrop distance cues, never the thing being flown.
// Each ring carries a narrow label at its +V-bar crossing so the faint
// ink still reads as a specific distance rather than an unlabeled
// decoration; on-canvas gated the same way the axis end labels are.
func (v *OrbitView) drawProximityRangeRings(w *sim.World, st sim.ProximityState) {
	dim := v.theme.Dim.GetForeground()
	for _, r := range proximityRingRadii {
		if !v.proximityRingVisible(st, r) {
			continue
		}
		pts := proximityRingPoints(st.TargetWorld, st.Frame.AlongTrack, st.Frame.RadialOut, r)
		v.canvas.PlotPolylineClass(pts, render.ColorDim, widgets.ClassScenery)
		labelPt := st.TargetWorld.Add(st.Frame.AlongTrack.Scale(r))
		if col, row, onCanvas := v.canvasCell(labelPt); onCanvas {
			v.canvas.SetCellLabelColored(col, row, formatRangeM(r), dim)
		}
	}
}

// drawProximityDockGate draws the 50 m dock-gate ring (sim.DockingDistM)
// — the one ring whose COLOR carries state rather than just its
// presence: dim while outside either gate, ColorTarget green exactly
// when the pair sits inside BOTH DockingDistM and DockingVMS
// (sim.ProximityDockGateReady — the same predicate checkDocking's
// auto-fuse uses, so the ring's green threshold and the game's actual
// "you are about to dock" threshold can never drift apart). This is
// issue #348 §1's "DOCK READY becomes a place on screen."
//
// No separate DOCK READY text callout is added alongside it: the ready
// state is inherently momentary (checkDocking auto-fuses the pair the
// very next tick it holds true), so a chip would flicker on and off
// faster than a player could read it — exactly the failure the hint
// chip's hysteresis band exists to prevent elsewhere in this file. A
// ring doesn't need that guard because it's a place, not a popup: it's
// already there, faint, before the moment arrives, and it simply changes
// colour when the moment does.
func (v *OrbitView) drawProximityDockGate(w *sim.World, st sim.ProximityState) {
	const gateRadius = sim.DockingDistM
	if !v.proximityRingVisible(st, gateRadius) {
		return
	}
	color := render.ColorDim
	if w.ProximityDockGateReady(st) {
		color = render.ColorTarget
	}
	pts := proximityRingPoints(st.TargetWorld, st.Frame.AlongTrack, st.Frame.RadialOut, gateRadius)
	v.canvas.PlotPolylineClass(pts, color, widgets.ClassScenery)
}

// proximityVelocityStubPx is the v_rel vector stub's length in canvas
// sub-pixels — the same order of magnitude as the ascent view's
// nose/prograde stubs (ascentMarkerStubPx), long enough to read a
// direction at a glance without swamping the vessel glyph it's anchored
// to.
const proximityVelocityStubPx = 8.0

// proximityVelocityVectorEndpoint computes the world point the v_rel stub
// reaches from st.CraftWorld: the active vessel's velocity relative to
// the target (st.RelVel, world axes), unit-scaled to
// proximityVelocityStubPx worth of canvas pixels at the given
// pixels-per-metre scale. Split out from drawProximityVelocityVector as
// a pure function so the DIRECTION math — the part issue #348 §1's third
// cue is actually about — can be checked by a test without inspecting
// canvas pixels. ok is false for the two cases nothing sensible can be
// drawn: no relative motion (RelVel = 0, nothing to point at) or a
// non-positive scale.
func proximityVelocityVectorEndpoint(st sim.ProximityState, scale float64) (orbital.Vec3, bool) {
	n := st.RelVel.Norm()
	if n == 0 || scale <= 0 {
		return orbital.Vec3{}, false
	}
	dir := st.RelVel.Scale(1 / n)
	step := proximityVelocityStubPx / scale
	return st.CraftWorld.Add(dir.Scale(step)), true
}

// drawProximityVelocityVector draws a short stub from the active vessel
// in the direction of v_rel — issue #348 §1's third cue.
// render.ColorNavballMarkerPrograde: this is a "which way am I moving"
// marker, the same semantic role that colour already carries on the
// navball and the ascent attitude stubs (ADR 0041: colour is semantic,
// never identity, so it is NOT the active vessel's own marker colour
// here).
//
// A direction stub rather than a magnitude-scaled arrow: the range chip
// already gives the number, and at docking-relevant speeds (cm/s to a
// few m/s) a to-scale vector would be sub-pixel most of the time anyway.
func (v *OrbitView) drawProximityVelocityVector(st sim.ProximityState) {
	end, ok := proximityVelocityVectorEndpoint(st, v.canvas.Scale())
	if !ok {
		return
	}
	v.canvas.PlotDenseLineColored(st.CraftWorld, end, render.ColorNavballMarkerPrograde, 1)
}

// drawProximityVessels stamps the two hulls-for-now: the target dead
// centre and the active vessel wherever the relative state puts it. Hull
// sprites at true scale are the follow-up slice; a glyph at the true
// POSITION is what this slice owes.
func (v *OrbitView) drawProximityVessels(w *sim.World, st sim.ProximityState) {
	v.canvas.FillColoredDiskTagged(st.TargetWorld, 1,
		widgets.CellTag{Color: render.ColorTarget, IsVessel: true})
	v.canvas.SetCellOverlayColored(st.TargetWorld, '◆', render.ColorTarget)

	activeColor := render.ColorCraftMarker
	glyph := '▲'
	if c := w.ActiveCraft(); c != nil {
		if c.Color != "" {
			activeColor = lipgloss.Color(c.Color)
		}
		if g := []rune(c.Glyph); len(g) > 0 {
			glyph = g[0]
		}
	}
	if _, _, onCanvas := v.canvasCell(st.CraftWorld); onCanvas {
		v.canvas.FillColoredDiskTagged(st.CraftWorld, 1,
			widgets.CellTag{Color: activeColor, IsVessel: true})
		v.canvas.SetCellOverlayColored(st.CraftWorld, glyph, activeColor)
		return
	}
	// Zoomed past your own vessel. The entry fit can't produce this, but a
	// manual zoom-in can, and an empty frame with a live readout is the
	// kind of silent nothing that reads as broken — so say what happened
	// and which key undoes it.
	v.canvas.SetCellLabelColored(0, 0, "your vessel is off frame — [-] zooms out",
		v.theme.Warning.GetForeground())
}

// canvasCell projects a world point to its terminal CELL (not pixel)
// coordinate on the canvas, plus whether it landed on-canvas at all.
func (v *OrbitView) canvasCell(p orbital.Vec3) (col, row int, onCanvas bool) {
	px, py, ok := v.canvas.Project(p)
	return px / 2, py / 4, ok
}

func clampInt(x, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// buildProximityChip is the readout block: the three numbers a pilot
// flies the last kilometres on. Only ever built inside Proximity View —
// on the map the TARGET chip already carries them, and two chips saying
// the same thing is exactly the clutter ADR 0010 split the HUD to avoid.
// Always-on (empty Settings id) because it is the view's own instrument
// panel; F2 declutter still clears it like any chip.
func (v *OrbitView) buildProximityChip(w *sim.World) []string {
	if w.ViewMode != sim.ViewProximity {
		return nil
	}
	st, ok := w.ProximityState()
	if !ok {
		// The in-view refusal lives here rather than on the canvas: a chip
		// paints an opaque block after everything else, so the explanation
		// can't be half-erased by the navball or a neighbouring chip.
		return []string{
			v.theme.Primary.Render("PROXIMITY"),
			"  " + w.ProximityRefusal(),
			"  [t] target a vessel · [o] back to the map",
		}
	}
	// Three rows, not four: the target's name rides the header rather than
	// a row of its own, because every row this chip spends is a row
	// admitChipsByBudget takes off the chip below it at 80×24.
	return []string{
		v.theme.Primary.Render("PROXIMITY") + "  " + st.TargetName,
		chipRow("range:", formatRangeM(st.RangeM)),
		chipRow("|v_rel|:", fmt.Sprintf("%.2f m/s", st.VRelMS)),
		chipRow("closing:", fmt.Sprintf("%+.2f m/s", st.ClosingMS)),
	}
}

// buildProximityHintChip tells the player the view exists at the one
// moment it starts being useful — crossing inside the range at which the
// game already considers two vessels to be flying together. The
// once-per-crossing discipline (and the hysteresis that protects it) lives
// in sim.World.ProximityHintActive; this is just the words.
func (v *OrbitView) buildProximityHintChip(w *sim.World) []string {
	if !w.ProximityHintActive() {
		return nil
	}
	// One line, not a block: the chip competes for corner space against
	// readouts the pilot is actually flying on, and a hint that pushes a
	// readout off the canvas has cost more than it taught.
	return []string{
		v.theme.Primary.Render("CLOSE RANGE") + " — [o] proximity view",
	}
}
