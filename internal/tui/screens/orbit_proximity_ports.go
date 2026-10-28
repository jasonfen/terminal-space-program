package screens

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// Docking ports in the Proximity View (ADR 0053 decision 6, slice A): the
// `○` port marks, the port line and its length label, and the `port:` /
// `misalign:` / `lateral:` chip rows. Everything here READS the numbers
// sim.ProximityState derives; none of it decides whether a dock happens.
// The capture gate, the green line and the magnet are slice B's.

const (
	// proximityChipInnerWidth is the widest a PROXIMITY chip line may be
	// at Design Size: the box is 41 cells wide with its border, 39 inside
	// (ADR 0053 decision 6, measured on 604-prox-after-undock-75m-main).
	proximityChipInnerWidth = 39

	// proximityPortNeedM / proximityPortNeedDeg are the gate's port-range
	// and alignment terms, as the "(need < ...)" reminders print them.
	// Readouts only in slice A: nothing here gates a dock.
	proximityPortNeedM   = 0.5
	proximityPortNeedDeg = 4.0
)

// proximityPortMarkGlyph is the port mark on a hull.
const proximityPortMarkGlyph = '○'

// proximityInsideRing reports whether the pair is inside the 50 m zone the
// "(need < ...)" reminders key on, the same test the rel speed suffix and the
// warp cap use: hull to hull (st.HullGapM), not stack bottom to stack bottom.
func proximityInsideRing(st sim.ProximityState) bool {
	return st.HullGapM < sim.DockingDistM
}

// proximityPortNeedSuffix is "  (need < 0.50)" exactly when the pair is
// inside 50 m and the port range term is failing; "  [+] zoom" when the
// port gap has shrunk under one canvas cell at the current scale (the
// picture can no longer show it, the number still can, and `+` zooms);
// empty otherwise. scale is canvas pixels per metre, 0 when unknown.
func proximityPortNeedSuffix(st sim.ProximityState, scale float64) string {
	if !st.HasPort {
		return ""
	}
	if proximityInsideRing(st) && st.PortRangeM > proximityPortNeedM {
		return fmt.Sprintf("  (need < %.2f)", proximityPortNeedM)
	}
	if scale > 0 && proximityInsideRing(st) && st.PortRangeM*scale < canvasCellPxW {
		return "  [+] zoom"
	}
	return ""
}

// proximityAlignNeedSuffix is "  (need < 4°)" exactly when the pair is
// inside 50 m and the alignment term is failing.
func proximityAlignNeedSuffix(st sim.ProximityState) string {
	if !st.HasPort || !proximityInsideRing(st) || st.AlignDeg <= proximityPortNeedDeg {
		return ""
	}
	return fmt.Sprintf("  (need < %.0f°)", proximityPortNeedDeg)
}

// proximityPortRow is the `port:` row: port-to-port range with the gate
// reminder, or why there is no port to measure.
func (v *OrbitView) proximityPortRow(st sim.ProximityState, capturing bool) string {
	if st.HasPort && capturing {
		// The magnet has the vessel: no reminder to act on, the pull is
		// closing the last half metre itself (ADR 0053 decision 5).
		return chipRow("port:", readout.PortDistance(st.PortRangeM)+"  capturing")
	}
	if st.HasPort {
		return chipRow("port:", readout.PortDistance(st.PortRangeM)+proximityPortNeedSuffix(st, v.canvas.Scale()))
	}
	return chipRow("port:", fitPortNote(st.PortNote, proximityChipInnerWidth-chipValueCol))
}

// proximityLatchedRow is the standing line under port: while the re-arm latch
// holds the pair (#794). 26 cells, the width of a chip value.
func proximityLatchedRow() string {
	return fmt.Sprintf("  latched: back %.0f m or %s", sim.ReArmDistM, ReArmKeyLabel)
}

// ReArmKeyLabel is the re-arm docking key. The keymap binds it (input.go) and
// the chip line and the bump caption name it, so the three cannot drift apart.
const ReArmKeyLabel = "alt+u"

// proximityAlignRow is the `misalign:` row (IDSS "pitch/yaw misalignment"),
// whole degrees off straight. ADR 0055 decision 2: renamed from `align:`,
// same number.
func proximityAlignRow(st sim.ProximityState) string {
	return chipRow("misalign:", fmt.Sprintf("%.0f° off", st.AlignDeg)+proximityAlignNeedSuffix(st))
}

// proximityOffAxisRow is the `lateral:` row (IDSS "lateral misalignment",
// renamed from `off-axis:`, ADR 0055 decision 2): their port relative to yours
// across the port face, up/down then left/right, one decimal.
func proximityOffAxisRow(st sim.ProximityState) string {
	up, right := "↑", "▸"
	if st.OffAxisUpM < 0 {
		up = "↓"
	}
	if st.OffAxisRightM < 0 {
		right = "◂"
	}
	return chipRow("lateral:", readout.PortOffset(st.OffAxisUpM)+" "+up+"  "+readout.PortOffset(st.OffAxisRightM)+" "+right)
}

// proximityHoldRow is the `hold:` row (ADR 0055 decision 2): the attitude
// the vessel is holding, read from the same builder as GUIDANCE's hold:
// row (attitudeHoldLabel) so the two cannot drift.
func proximityHoldRow(w *sim.World) string {
	c := w.ActiveCraft()
	if c == nil {
		return chipRow(readout.LabelHold, "—")
	}
	return chipRow(readout.LabelHold, attitudeHoldLabel(w, c.AttitudeMode))
}

// proximityPortOutOfGate is the Compact Form's "out of gate" for the port
// rows: the port gate not holding, read from the sim's own predicate so the
// chip, the ring and the dock cannot disagree.
func proximityPortOutOfGate(st sim.ProximityState) bool {
	return !sim.PortGateTermsHold(st)
}

// proximityChipHeader is "PROXIMITY  <target>  port: <top>" (or "no port"),
// the target name shortened so the line never outgrows the chip.
func (v *OrbitView) proximityChipHeader(st sim.ProximityState) string {
	tag := "no port"
	switch {
	case st.Docked:
		tag = "docked"
	case st.PortUnknown:
		tag = "port: unknown"
	case st.TargetHasPort:
		tag = "port: " + st.PortName
	}
	const lead = "PROXIMITY  "
	room := proximityChipInnerWidth - lipgloss.Width(lead) - 2 - lipgloss.Width(tag)
	if room < 6 { // a port name too long to leave the target readable
		tag = truncateCells(tag, proximityChipInnerWidth-lipgloss.Width(lead)-2-6)
		room = 6
	}
	return v.theme.Primary.Render("PROXIMITY") + "  " + truncateCells(st.TargetName, room) + "  " + tag
}

// fitPortNote shrinks a "none (...)" note to budget cells: the full text
// if it fits, else the reason's noun alone ("none (Ground Station)"), else
// cut with an ellipsis.
func fitPortNote(note string, budget int) string {
	if lipgloss.Width(note) <= budget {
		return note
	}
	short := note
	for _, cut := range []string{" has no port", "top is ", "yours: "} {
		short = replaceOnce(short, cut, "")
	}
	if lipgloss.Width(short) <= budget {
		return short
	}
	return truncateCells(short, budget)
}

func replaceOnce(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}

// proximityPortWorld is where a hull's port is DRAWN: the anchor plus the
// port offset along the hull's axis as the sprite projects it into the
// screen plane (the same stackDirScreen the composers use, including its
// near-vertical snap), so the mark sits on the nose that was drawn rather
// than on the nose of a 3-D axis the flat canvas cannot show.
func proximityPortWorld(anchor, attitude orbital.Vec3, basis widgets.Basis, offsetM float64) orbital.Vec3 {
	sx, sy := stackDirScreen(attitude, basis)
	return anchor.Add(basis.X.Scale(sx * offsetM)).Add(basis.Y.Scale(sy * offsetM))
}

// drawProximityPortLine draws the dashed line joining the two drawn ports,
// before the hulls so hull pixels win any overlap, and labels it with the
// port-to-port range at its midpoint. Only while both hulls are drawn as
// sprites (the marks exist only then), and only when both carry a port.
// TARGET green, dashed (Planned-class) until sim.ProximityDockGateReady
// holds, then solid (Real-class) for the capture.
func (v *OrbitView) drawProximityPortLine(w *sim.World, st sim.ProximityState) {
	if !st.HasPort {
		return
	}
	scale := v.canvas.Scale()
	active := w.ActiveCraft()
	if active == nil || scale <= 0 {
		return
	}
	// The target's drawn hull: a local vessel's stack, or a ghost's reported
	// rectangle (ADR 0053 decision 9). Either way the mark and the line need
	// the hull shown as a sprite, and the attitude the sprite points along.
	var tgtAttitude orbital.Vec3
	if tgt, _, ok := w.ResolveTargetCraft(); ok {
		if !proximitySpriteVisible(v.proxTargetSpriteShown, proximitySpriteHeightCells(tgt.Stages, scale)) {
			return
		}
		tgtAttitude = tgt.CurrentAttitudeDir
	} else if g, _, ok := w.ResolveTargetGhost(); ok {
		if !proximitySpriteVisible(v.proxTargetSpriteShown, ghostHullHeightCells(g, scale)) {
			return
		}
		tgtAttitude = g.Attitude
	} else {
		return
	}
	if !proximitySpriteVisible(v.proxOwnSpriteShown, proximitySpriteHeightCells(active.Stages, scale)) {
		return
	}
	basis := widgets.Basis{X: st.Frame.AlongTrack, Y: st.Frame.RadialOut}
	a := proximityPortWorld(st.CraftWorld, active.CurrentAttitudeDir, basis, st.Own.Ports[0].OffsetM)
	b := proximityPortWorld(st.TargetWorld, tgtAttitude, basis, st.Target.Ports[0].OffsetM)
	// Dashed while the gate is open; solid green the tick it holds (and
	// through the magnet's pull): the same predicate the dock reads.
	v.canvas.PlotPolylineClass([]orbital.Vec3{a, b}, render.ColorTarget, proximityPortLineClass(w, st))
	mid := a.Add(b).Scale(0.5)
	col, row, onCanvas := v.canvasCell(mid)
	if !onCanvas {
		return
	}
	// The label sits above the line, below it if that is taken, on it as a
	// last resort: on the line itself it lands on the +V axis row, where the
	// range-ring labels live, and over the two marks when the gap is only a
	// few cells.
	label := readout.PortDistance(st.PortRangeM)
	// The two marks are drawn after this, over whatever is there: a label
	// cell under one reads `4.1○ m` (ADR 0055 B6).
	markCell := func(x, y int) bool {
		for _, p := range []orbital.Vec3{a, b} {
			if c, r, ok := v.canvasCell(p); ok && c == x && r == y {
				return true
			}
		}
		return false
	}
	left := col - lipgloss.Width(label)/2
	at := row
	for _, dr := range []int{-1, 1} {
		free := true
		for x := left; x < left+lipgloss.Width(label); x++ {
			if v.canvas.CellOccupied(x, row+dr) || markCell(x, row+dr) {
				free = false
				break
			}
		}
		if free {
			at = row + dr
			break
		}
	}
	v.canvas.SetCellLabelColored(left, at, label, v.theme.Dim.GetForeground())
}

// proximityPortLineClass is the port line's stroke: dashed while the gate is
// open, solid the tick sim.ProximityDockGateReady holds and through the
// magnet's pull. The same predicate the dock reads.
func proximityPortLineClass(w *sim.World, st sim.ProximityState) widgets.LineClass {
	if w.ProximityDockGateReady(st) {
		return widgets.ClassReal
	}
	return widgets.ClassPlanned
}

// drawProximityPortMark stamps the `○` on one hull's drawn port, in that
// hull's colour. Called only for a hull whose sprite is shown.
func (v *OrbitView) drawProximityPortMark(anchor, attitude orbital.Vec3, frame orbital.LVLHFrame, offsetM float64, color lipgloss.TerminalColor) {
	basis := widgets.Basis{X: frame.AlongTrack, Y: frame.RadialOut}
	v.canvas.SetCellOverlayColored(proximityPortWorld(anchor, attitude, basis, offsetM), proximityPortMarkGlyph, color)
}

// proximityCapturing reports whether the magnet is pulling the active vessel
// onto the target's port, local or cross-player: the chip's "capturing".
func proximityCapturing(w *sim.World) bool {
	switch w.Target.Kind {
	case sim.TargetCraft:
		return w.DockCapturing(w.Target.CraftID)
	case sim.TargetGhost:
		return w.GhostCapturing(w.Target.GhostOwner, w.Target.CraftID)
	}
	return false
}

// ghostHullHeightCells is a ghost rectangle's on-screen length in terminal
// cells at the canvas scale (pixels per metre): the hysteresis input
// proximitySpriteVisible reads for a hull that has no stage list.
func ghostHullHeightCells(g sim.Ghost, scale float64) float64 {
	if scale <= 0 || g.NoseM <= 0 {
		return 0
	}
	return g.NoseM * scale / canvasCellPxH
}

// fillHullRect fills one hull rectangle in true metres: from tailM to headM
// along the unit vector `along` and widthM wide across `across`, both
// measured from anchor. Two scanlines per pixel across, so neighbours closer
// than a pixel can never leave a gap row; at least 2 so a hair-thin hull
// still inks, capped at proximityGhostMaxScanlines at deep zoom.
func (v *OrbitView) fillHullRect(anchor, along, across orbital.Vec3, tailM, headM, widthM float64, tag widgets.CellTag) {
	lines := int(widthM*v.canvas.Scale()*2) + 2
	lines = max(2, min(lines, proximityGhostMaxScanlines))
	for i := 0; i < lines; i++ {
		u := (float64(i)/float64(lines-1) - 0.5) * widthM
		a := anchor.Add(along.Scale(tailM)).Add(across.Scale(u))
		b := anchor.Add(along.Scale(headM)).Add(across.Scale(u))
		v.canvas.PlotDenseLineTagged(a, b, tag)
	}
}

// proximityGhostMaxScanlines bounds the rectangle fill at deep zoom: a
// 6 m hull at 100 px/m would otherwise be 600 lines a frame.
const proximityGhostMaxScanlines = 800

// drawProximityGhostHull draws a friend's reported hull as one filled
// rectangle, nose_m long and width_m wide, in the ghost's colour (ColorDim,
// the colour every ghost draws in, ADR 0041 section 3), anchored at the
// ghost's position (the bottom of its stack) and pointing along its reported
// attitude as the flat canvas projects it. The stage list is not on the wire,
// so there is no silhouette (ADR 0053 "Settled here" item 8). shown is the
// caller's hysteresis latch. Returns false, latch cleared, when the report
// carries no shape or the hull is under the 3-cell sprite switch, so the
// caller draws the diamond.
func (v *OrbitView) drawProximityGhostHull(shown *bool, g sim.Ghost, anchor orbital.Vec3, frame orbital.LVLHFrame) bool {
	pg := sim.GhostPortGeometry(g)
	scale := v.canvas.Scale()
	if !pg.Known || scale <= 0 {
		*shown = false
		return false
	}
	*shown = proximitySpriteVisible(*shown, ghostHullHeightCells(g, scale))
	if !*shown {
		return false
	}
	basis := widgets.Basis{X: frame.AlongTrack, Y: frame.RadialOut}
	sx, sy := stackDirScreen(g.Attitude, basis)
	along := basis.X.Scale(sx).Add(basis.Y.Scale(sy)) // screen-plane unit along the hull
	across := basis.X.Scale(-sy).Add(basis.Y.Scale(sx))
	tail := -sim.DockPortTipInsetM()
	v.fillHullRect(anchor, along, across, tail, pg.NoseM+tail, pg.WidthM, widgets.CellTag{Color: render.ColorDim})
	return true
}

// hullBoxEpsilonM absorbs the float noise in HullDepthM (a Z offset of exactly
// the radii sum reads 4.0000000001) so that "exactly the sum" stays filled.
const hullBoxEpsilonM = 1e-6

// proximityHullBoxed is the dotted-box rule (ADR 0055 B1): the target's hull
// is outlined, not filled, when the two axes are further apart across the
// view's depth than the two radii added. Strict: exactly the sum is filled.
// No range gate.
func proximityHullBoxed(st sim.ProximityState) bool {
	return st.Own.Known && st.Target.Known &&
		st.HullDepthM > (st.Own.WidthM+st.Target.WidthM)/2+hullBoxEpsilonM
}

// drawProximityHullBox draws a hull out of your plane as the dotted outline of
// its nose_m x width_m rectangle, in colour, anchored and pointed like the
// filled hull it replaces. shown is the hull's own hysteresis latch and
// heightCells the length that feeds it, so a box never appears where the
// glyph would. Returns false, latch cleared, when it is under the sprite
// switch.
func (v *OrbitView) drawProximityHullBox(shown *bool, heightCells float64, anchor, attitude orbital.Vec3, frame orbital.LVLHFrame, pg sim.PortGeometry, colour lipgloss.Color) bool {
	*shown = proximitySpriteVisible(*shown, heightCells)
	if !*shown {
		return false
	}
	basis := widgets.Basis{X: frame.AlongTrack, Y: frame.RadialOut}
	sx, sy := stackDirScreen(attitude, basis)
	along := basis.X.Scale(sx).Add(basis.Y.Scale(sy))
	across := basis.X.Scale(-sy).Add(basis.Y.Scale(sx))
	tail := -sim.DockPortTipInsetM()
	head, half := pg.NoseM+tail, pg.WidthM/2
	corner := func(a, u float64) orbital.Vec3 {
		return anchor.Add(along.Scale(a)).Add(across.Scale(u))
	}
	v.canvas.PlotPolylineClass([]orbital.Vec3{corner(tail, -half), corner(head, -half), corner(head, half), corner(tail, half), corner(tail, -half)},
		colour, widgets.ClassScenery)
	return true
}
