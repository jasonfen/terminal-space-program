package screens

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// The Centerline view (ADR 0055 decision 1, slice 2): the right half of the
// Proximity View at Design Size. You are looking out of your own docking port
// along your nose. Your port is a fixed crosshair; the Target's port is a
// ring of the port gate's radius drawn where it is across your face; the
// standoff cross inside the ring slides off its centre by the alignment lean.
//
// Three readers share one definition (the ADR's "three readers" rule): the
// ring's position, the `lateral:` row and the w/s/a/d pulses all read
// PortFaceBasis(a_target), through ProximityState.OffAxisUpM / OffAxisRightM;
// the cross and the navball's pitch / yaw row read ProximityState.
// MisalignUpDeg / MisalignRightDeg. Nothing here derives its own.

const (
	// centerlineRingRadiusM is the port gate's range term, the only honest
	// radius a port without a size has (ADR 0055 decision 1).
	centerlineRingRadiusM = proximityPortNeedM

	// centerlineFitFloorM is the entry fit's floor: the span, in metres,
	// the ring window is fitted to when the lateral miss is smaller (ADR
	// 0055 "Settled here" 5).
	centerlineFitFloorM = 2.0

	// centerlineGlyphCells / centerlineCrossCells: a ring under 3 cells
	// across is the port glyph (the hull sprite's own switch, ADR 0053
	// decision 6), and the standoff cross is drawn from 5 cells across.
	centerlineGlyphCells = 3.0
	centerlineCrossCells = 5.0

	// centerlineCorridorM is how far the corridor runs out along the
	// target's port axis: the dock gate's radius (sim.DockingDistM).
	centerlineCorridorM = sim.DockingDistM

	// centerlineCrosshairArmPx are the crosshair arms' half-lengths in
	// canvas pixels (3 cells across, 3 cells up).
	centerlineCrosshairArmXPx = 6.0
	centerlineCrosshairArmYPx = 12.0
)

// centerlineGeom is the right pane's cell budget (ADR 0055 "Measured cell
// budget"): where the pane starts, where the crosshair sits and how big a
// ring the clear window holds. All cells are pane-relative.
type centerlineGeom struct {
	leftCols int // the left pane's width; the pane starts here on screen
	paneCols int
	rows     int
	// crossCol / crossRow is the crosshair's cell: the centre column of the
	// strip the navball and the chips leave clear, above the pane's middle
	// so the lower rows keep the range label.
	crossCol, crossRow int
	// winHalfCols / winHalfRows are the ring window's half extents in
	// cells around the crosshair (a 33 x 17 window at Design Size).
	winHalfCols, winHalfRows int
}

// centerlineLayout resolves the split for a canvas of fullCols x rows, and
// whether it exists at all: below the Design Size the right pane is the
// first thing dropped (Compact Form, ADR 0046 / ADR 0055 decision 1).
func centerlineLayout(fullCols, rows int) (centerlineGeom, bool) {
	if fullCols < designCanvasCols || rows < designCanvasRows {
		return centerlineGeom{}, false
	}
	g := centerlineGeom{leftCols: fullCols / 2, rows: rows}
	g.paneCols = fullCols - g.leftCols
	// The strip the navball leaves clear: pane columns 0..clearLast, one
	// blank cell before the navball's frame.
	nb := navballGeometry(fullCols, rows)
	clearLast := fullCols - nb.panelW - 1 - g.leftCols
	g.crossCol = clearLast / 2
	g.winHalfCols = (clearLast - 2) / 2
	g.winHalfRows = 8
	g.crossRow = rows/2 - 3
	return g, true
}

// centerlineFitSpanM is the width in metres the right pane's clear window is
// fitted to on entry: twice the lateral miss plus the ring's own radius so
// the ring starts wholly on screen, never under the 2 m floor.
func centerlineFitSpanM(st sim.ProximityState) float64 {
	lateral := math.Hypot(st.OffAxisRightM, st.OffAxisUpM)
	return math.Max(2*(lateral+centerlineRingRadiusM), centerlineFitFloorM)
}

// centerlineFitScale is the right pane's base scale in pixels per metre:
// the fit span across the ring window.
func centerlineFitScale(st sim.ProximityState, g centerlineGeom) float64 {
	return float64((2*g.winHalfCols+1)*2) / centerlineFitSpanM(st)
}

// centerlineCanvas returns the right pane's canvas, created on first use.
func (v *OrbitView) centerlineCanvas() *widgets.Canvas {
	if v.cl == nil {
		v.cl = widgets.NewCanvas(4, 4)
	}
	return v.cl
}

// centerlineCellWorld is the world point (pane metres, X right, Y up) the
// canvas projects to the given pixel.
func centerlineOrigin(g centerlineGeom, scale float64) orbital.Vec3 {
	cx, cy := 2*g.crossCol+1, 4*g.crossRow+2
	return orbital.Vec3{
		X: float64(g.paneCols-cx) / scale,
		Y: float64(cy-g.rows*2) / scale,
	}
}

// centerlineRingPx is the ring's radius in canvas pixels at a scale.
func centerlineRingPx(scale float64) float64 { return centerlineRingRadiusM * scale }

// centerlineCrossOffsetM is the standoff cross's displacement from the ring's
// centre in metres across the face: R_ring x (eps / 4 deg) per axis, so it
// reaches the ring's edge exactly at the gate's 4 degrees (ADR 0055 decision
// 1, confirmed by Jason). Right and up are the face basis's own signs.
func centerlineCrossOffsetM(st sim.ProximityState) (right, up float64) {
	return centerlineRingRadiusM * st.MisalignRightDeg / proximityPortNeedDeg,
		centerlineRingRadiusM * st.MisalignUpDeg / proximityPortNeedDeg
}

// drawCenterline paints the right pane onto v.cl.
func (v *OrbitView) drawCenterline(w *sim.World, st sim.ProximityState, g centerlineGeom) {
	c := v.centerlineCanvas()
	c.Resize(g.paneCols, g.rows)
	c.Clear()
	c.SetBasis(widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Y: 1}})
	scale := v.clBaseScale * v.userZoom
	if !(scale > 0) {
		return
	}
	c.SetScale(scale)
	c.Center(centerlineOrigin(g, scale))

	dim := v.theme.Dim.GetForeground()
	own := render.ColorCraftMarker
	if a := w.ActiveCraft(); a != nil && a.Color != "" {
		own = lipgloss.Color(a.Color)
	}

	// Crosshair: two short Scenery-class lines through the centre cell.
	c.PlotPolylineClass([]orbital.Vec3{
		{X: -centerlineCrosshairArmXPx / scale}, {X: centerlineCrosshairArmXPx / scale},
	}, own, widgets.ClassScenery)
	c.PlotPolylineClass([]orbital.Vec3{
		{Y: -centerlineCrosshairArmYPx / scale}, {Y: centerlineCrosshairArmYPx / scale},
	}, own, widgets.ClassScenery)

	// Ring: their port, centred at the lateral offset. TARGET colour;
	// dashed until the dock gate holds, then solid, as the port line is.
	centre := orbital.Vec3{X: st.OffAxisRightM, Y: st.OffAxisUpM}
	ringCells := centerlineRingPx(scale) // diameter in px / 2 px per cell
	ringRows := 0
	if ringCells < centerlineGlyphCells {
		c.SetCellOverlayColored(centre, proximityPortMarkGlyph, render.ColorTarget)
	} else {
		pts := proximityRingPoints(centre, orbital.Vec3{X: 1}, orbital.Vec3{Y: 1}, centerlineRingRadiusM)
		c.PlotPolylineClass(pts, render.ColorTarget, proximityPortLineClass(w, st))
		ringRows = int(math.Ceil(centerlineRingPx(scale) / 4))
	}

	// Standoff cross: the lean of your axis across their port's face.
	if ringCells >= centerlineCrossCells {
		dr, du := centerlineCrossOffsetM(st)
		cross := centerlineClampToWindow(g, scale, centre, orbital.Vec3{X: dr, Y: du})
		c.SetCellOverlayColored(centre.Add(cross), '+', own)
	}

	// Labels.
	c.SetCellLabelColored(0, 0, "Centerline", v.theme.Primary.GetForeground())
	c.SetCellLabelColored(g.crossCol-1, 0, "▴up", dim)
	c.SetCellLabelColored(g.paneCols-len([]rune("right▸"))-1, g.crossRow, "right▸", dim)
	if col, row, on := centerlineCell(c, centre); on {
		label := readout.PortDistance(st.PortRangeM)
		at := clampInt(row-ringRows-1, 1, g.rows-1)
		c.SetCellLabelColored(col-lipgloss.Width(label)/2, at, label, dim)
	}
}

// centerlineClampToWindow shortens the cross's displacement, keeping its
// direction, until the cross stays inside the ring window; a lean of tens of
// degrees would otherwise put it off the pane.
func centerlineClampToWindow(g centerlineGeom, scale float64, centre, d orbital.Vec3) orbital.Vec3 {
	f := 1.0
	test := func(offPx, centrePx, halfPx float64) {
		if offPx == 0 {
			return
		}
		room := halfPx - math.Copysign(centrePx, offPx)
		if room < 0 {
			room = 0
		}
		if s := room / math.Abs(offPx); s < f {
			f = s
		}
	}
	test(d.X*scale, centre.X*scale, float64(g.winHalfCols*2))
	test(d.Y*scale, centre.Y*scale, float64(g.winHalfRows*4))
	return d.Scale(f)
}

// centerlineCell is canvasCell for a given canvas.
func centerlineCell(c *widgets.Canvas, p orbital.Vec3) (col, row int, on bool) {
	px, py, ok := c.Project(p)
	return px / 2, py / 4, ok
}

// joinCanvasRows places two canvases' rendered strings side by side, row by
// row. Every styled run in a canvas row is closed, so concatenation is safe.
func joinCanvasRows(left, right string) string {
	l, r := strings.Split(left, "\n"), strings.Split(right, "\n")
	n := len(l)
	if len(r) > n {
		n = len(r)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i < len(l) {
			b.WriteString(l[i])
		}
		if i < len(r) {
			b.WriteString(r[i])
		}
		if i < n-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// corridorSegment is the dotted line out along the target's port axis, from
// its port point to centerlineCorridorM: the line the approach is flown
// onto, in the side view (ADR 0055 decision 1). ok is false without a port
// with a known axis.
func corridorSegment(st sim.ProximityState) (from, to orbital.Vec3, ok bool) {
	g := st.Target
	if !st.HasPort || !g.Known || !g.HasPort() || g.Axis.Norm() == 0 {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	from = st.TargetWorld.Add(g.Axis.Scale(g.Ports[0].OffsetM))
	return from, from.Add(g.Axis.Unit().Scale(centerlineCorridorM)), true
}

// drawProximityCorridor draws the corridor in the left pane, Scenery-class
// (dotted) in the TARGET colour. Hull pixels drawn afterwards win over it.
func (v *OrbitView) drawProximityCorridor(st sim.ProximityState) {
	from, to, ok := corridorSegment(st)
	if !ok {
		return
	}
	v.canvas.PlotPolylineClass([]orbital.Vec3{from, to}, render.ColorTarget, widgets.ClassScenery)
}

// spillBottomRow paints the part of the bottom row's label and Hint Strip that
// ran past the left pane's edge onto the right pane's last row, so the legend
// reads straight across the seam as it did on one canvas.
func (v *OrbitView) spillBottomRow(label string, g centerlineGeom) {
	last := g.rows - 1
	v.cl.SetCellLabelColored(-g.leftCols, last, label, v.theme.Primary.GetForeground())
	v.cl.SetCellLabelColored(len([]rune(label))+hintStripGap-g.leftCols, last, proximityHintStripText, v.theme.Dim.GetForeground())
}
