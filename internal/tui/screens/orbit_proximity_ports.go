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
// `align:` / `off-axis:` chip rows. Everything here READS the numbers
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

// proximityInsideRing reports whether the pair is inside the 50 m ring the
// "(need < ...)" reminders key on, the same test the rel speed suffix uses.
func proximityInsideRing(st sim.ProximityState) bool {
	return st.RangeM < sim.DockingDistM
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
func (v *OrbitView) proximityPortRow(st sim.ProximityState) string {
	if st.HasPort {
		return chipRow("port:", readout.PortDistance(st.PortRangeM)+proximityPortNeedSuffix(st, v.canvas.Scale()))
	}
	return chipRow("port:", fitPortNote(st.PortNote, proximityChipInnerWidth-chipValueCol))
}

// proximityAlignRow is the `align:` row, whole degrees off straight.
func proximityAlignRow(st sim.ProximityState) string {
	return chipRow("align:", fmt.Sprintf("%.0f° off", st.AlignDeg)+proximityAlignNeedSuffix(st))
}

// proximityOffAxisRow is the `off-axis:` row: their port relative to yours
// across the port face, up/down then left/right, one decimal.
func proximityOffAxisRow(st sim.ProximityState) string {
	up, right := "↑", "▸"
	if st.OffAxisUpM < 0 {
		up = "↓"
	}
	if st.OffAxisRightM < 0 {
		right = "◂"
	}
	return chipRow("off-axis:", readout.PortOffset(st.OffAxisUpM)+" "+up+"  "+readout.PortOffset(st.OffAxisRightM)+" "+right)
}

// proximityPortOutOfGate is the Compact Form's "out of gate" for the port
// rows: any of the three gate terms failing. A reading of the numbers, not
// the gate itself (slice B owns that predicate).
func proximityPortOutOfGate(st sim.ProximityState) bool {
	if !st.HasPort {
		return true
	}
	return st.PortRangeM > proximityPortNeedM ||
		st.VRelMS >= sim.DockingVMS ||
		st.AlignDeg > proximityPortNeedDeg
}

// proximityChipHeader is "PROXIMITY  <target>  port: <top>" (or "no port"),
// the target name shortened so the line never outgrows the chip.
func (v *OrbitView) proximityChipHeader(st sim.ProximityState) string {
	tag := "no port"
	switch {
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
// Dashed Planned-class in TARGET green; the solid-green capture state is
// slice B's, with the gate it reads.
func (v *OrbitView) drawProximityPortLine(w *sim.World, st sim.ProximityState) {
	if !st.HasPort {
		return
	}
	scale := v.canvas.Scale()
	active := w.ActiveCraft()
	tgt, _, ok := w.ResolveTargetCraft()
	if active == nil || !ok || scale <= 0 {
		return
	}
	if !proximitySpriteVisible(v.proxOwnSpriteShown, proximitySpriteHeightCells(active.Stages, scale)) ||
		!proximitySpriteVisible(v.proxTargetSpriteShown, proximitySpriteHeightCells(tgt.Stages, scale)) {
		return
	}
	basis := widgets.Basis{X: st.Frame.AlongTrack, Y: st.Frame.RadialOut}
	a := proximityPortWorld(st.CraftWorld, active.CurrentAttitudeDir, basis, st.Own.Ports[0].OffsetM)
	b := proximityPortWorld(st.TargetWorld, tgt.CurrentAttitudeDir, basis, st.Target.Ports[0].OffsetM)
	v.canvas.PlotPolylineClass([]orbital.Vec3{a, b}, render.ColorTarget, widgets.ClassPlanned)
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
	left := col - lipgloss.Width(label)/2
	at := row
	for _, dr := range []int{-1, 1} {
		free := true
		for x := left; x < left+lipgloss.Width(label); x++ {
			if v.canvas.CellOccupied(x, row+dr) {
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

// drawProximityPortMark stamps the `○` on one hull's drawn port, in that
// hull's colour. Called only for a hull whose sprite is shown.
func (v *OrbitView) drawProximityPortMark(anchor, attitude orbital.Vec3, frame orbital.LVLHFrame, offsetM float64, color lipgloss.TerminalColor) {
	basis := widgets.Basis{X: frame.AlongTrack, Y: frame.RadialOut}
	v.canvas.SetCellOverlayColored(proximityPortWorld(anchor, attitude, basis, offsetM), proximityPortMarkGlyph, color)
}
