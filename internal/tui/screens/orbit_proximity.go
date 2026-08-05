package screens

import (
	"fmt"

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
		v.drawProximityVessels(w, st)
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
