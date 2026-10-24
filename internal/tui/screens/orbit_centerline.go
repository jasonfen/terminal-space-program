package screens

import (
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// The Centerline scope (ADR 0055 decision 1 and the 2026-10-23 amendment): a
// framed panel in the Proximity View's top-right corner, the navball's twin.
// You are looking out of your own docking port along your nose. Your port is
// a fixed dotted crosshair; the Target's port is a ring of the port gate's
// radius drawn where it is across your face; the standoff cross inside the
// ring slides off its centre by the alignment lean. The disc ranges itself
// (orbit_centerline_scale.go) and ignores the side picture's zoom.
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

	// centerlineGlyphCells / centerlineCrossCells: a ring under 3 cells
	// across is the port glyph (the hull sprite's own switch, ADR 0053
	// decision 6), and the standoff cross is drawn from 5 cells across.
	centerlineGlyphCells = 3.0
	centerlineCrossCells = 5.0

	// centerlineCorridorM is how far the corridor runs out along the
	// target's port axis: the dock gate's radius (sim.DockingDistM).
	centerlineCorridorM = sim.DockingDistM

	// The panel: a 32-column inner width at Design Size (a 28-column disc
	// with two spare cells each side), a border, and a title row above the
	// disc. The disc is square in dots (2 x 4 dots per cell): 28 x 14 cells.
	centerlineBaseDiscRows = 14
	centerlinePadCols      = 2
	centerlineChromeRows   = 3 // top border, title row, bottom border
	centerlineChromeCols   = 2*centerlinePadCols + 2

	// The rim sits one dot inside the disc; the edge ticks are 5-dot bars
	// across the arms, two dots inside the rim.
	centerlineRimInsetPx  = 1.0
	centerlineTickInsetPx = 3.0
	centerlineTickHalfPx  = 2.0
)

// centerlineGeom is the scope panel's cell budget: the disc, the panel round
// it, and where it sits on the canvas. discCols is always 2 x discRows.
type centerlineGeom struct {
	discCols, discRows int
	panelW, panelH     int
	atCol, atRow       int
	// winHalfCols / winHalfRows are the disc's half extents in cells around
	// the crosshair (the standoff cross is kept inside them).
	winHalfCols, winHalfRows int
}

// centerlineLayout resolves the panel for a canvas of cCols x cRows, and
// whether it exists at all: below the Design Size it is the first thing
// dropped (Compact Form, ADR 0046 / ADR 0055 A1). It grows with height like
// the navball (heightScaledCells, ADR 0051 W5), capped so it never reaches
// the navball's top row nor passes the navball's column cap.
func centerlineLayout(cCols, cRows int) (centerlineGeom, bool) {
	if cCols < designCanvasCols || cRows < designCanvasRows {
		return centerlineGeom{}, false
	}
	rows := heightScaledCells(centerlineBaseDiscRows, cRows)
	nb := navballGeometry(cCols, cRows)
	if maxRows := cRows - nb.panelH - 1 - centerlineChromeRows; rows > maxRows {
		rows = maxRows
	}
	// The navball's column cap, so the scope never outgrows the width the
	// notice bay leaves it (#769); panelW = 2*rows + chrome.
	if maxDisc := (navballMaxPanelCols(cCols) - centerlineChromeCols) / 2; rows > maxDisc {
		rows = maxDisc
	}
	g := centerlineGeom{discRows: rows, discCols: 2 * rows, winHalfCols: rows, winHalfRows: rows / 2}
	g.panelW = g.discCols + centerlineChromeCols
	g.panelH = g.discRows + centerlineChromeRows
	g.atCol = cCols - g.panelW
	return g, true
}

// centerlineCanvas returns the disc's canvas, created on first use.
func (v *OrbitView) centerlineCanvas() *widgets.Canvas {
	if v.cl == nil {
		v.cl = widgets.NewCanvas(4, 4)
	}
	return v.cl
}

// centerlineRingPx is the ring's radius in canvas dots at a scale.
func centerlineRingPx(scale float64) float64 { return centerlineRingRadiusM * scale }

// centerlineCrossOffsetM is the standoff cross's displacement from the ring's
// centre in metres across the face: R_ring x (eps / 4 deg) per axis, so it
// reaches the ring's edge exactly at the gate's 4 degrees (ADR 0055 decision
// 1, confirmed by Jason). Right and up are the face basis's own signs.
func centerlineCrossOffsetM(st sim.ProximityState) (right, up float64) {
	return centerlineRingRadiusM * st.MisalignRightDeg / proximityPortNeedDeg,
		centerlineRingRadiusM * st.MisalignUpDeg / proximityPortNeedDeg
}

// drawCenterline paints the disc onto v.cl and returns the width step it
// ranged to, for the title row.
func (v *OrbitView) drawCenterline(w *sim.World, st sim.ProximityState, g centerlineGeom) float64 {
	c := v.centerlineCanvas()
	c.Resize(g.discCols, g.discRows)
	c.Clear()
	c.SetBasis(widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Y: 1}})

	e := math.Hypot(st.OffAxisRightM, st.OffAxisUpM) + centerlineRingRadiusM
	v.clStepM = centerlineStep(v.clStepM, e)
	pxW := float64(g.discCols * 2)
	scale := pxW / v.clStepM // dots per metre: the disc's width is the step
	c.SetScale(scale)
	c.Center(orbital.Vec3{})
	rimPx := pxW/2 - centerlineRimInsetPx

	own := render.ColorCraftMarker
	if a := w.ActiveCraft(); a != nil && a.Color != "" {
		own = lipgloss.Color(a.Color)
	}

	// Rim and its four edge ticks (the ODS monitor's tick marks, R[4]).
	x, y := orbital.Vec3{X: 1}, orbital.Vec3{Y: 1}
	c.PlotPolylineClass(proximityRingPoints(orbital.Vec3{}, x, y, rimPx/scale), render.ColorDim, widgets.ClassReal)
	tick, half := (pxW/2-centerlineTickInsetPx)/scale, centerlineTickHalfPx/scale
	for _, d := range []orbital.Vec3{{X: 1}, {X: -1}, {Y: 1}, {Y: -1}} {
		along := orbital.Vec3{X: d.Y, Y: d.X} // across the arm
		c.PlotPolylineClass([]orbital.Vec3{
			d.Scale(tick).Sub(along.Scale(half)), d.Scale(tick).Add(along.Scale(half)),
		}, render.ColorDim, widgets.ClassReal)
	}

	// Crosshair: the COAS reticle, dotted, arms to the rim.
	c.PlotPolylineClass([]orbital.Vec3{{X: -rimPx / scale}, {X: rimPx / scale}}, own, widgets.ClassScenery)
	c.PlotPolylineClass([]orbital.Vec3{{Y: -rimPx / scale}, {Y: rimPx / scale}}, own, widgets.ClassScenery)

	// Hull outlines (S2) go under the ring and the cross.
	v.drawCenterlineHulls(c, w, st, scale)

	// Ring: their port, centred at the lateral offset. TARGET colour;
	// dashed until the dock gate holds, then solid, as the port line is.
	// Beyond the last step the glyph sits on the rim in the port's direction.
	centre := orbital.Vec3{X: st.OffAxisRightM, Y: st.OffAxisUpM}
	if e > centerlineStepOutFrac*v.clStepM/2 {
		centre = centre.Unit().Scale(rimPx / scale)
	}
	if drawCenterlineBehind(c, st, centre) { // A6: behind your port plane
		return v.clStepM
	}
	ringCells := centerlineRingPx(scale) // diameter in cells = radius in dots
	if ringCells < centerlineGlyphCells || e > centerlineStepOutFrac*v.clStepM/2 {
		c.SetCellOverlayColored(centre, proximityPortMarkGlyph, render.ColorTarget)
	} else {
		pts := proximityRingPoints(centre, x, y, centerlineRingRadiusM)
		c.PlotPolylineClass(pts, render.ColorTarget, proximityPortLineClass(w, st))
	}

	// Standoff cross: the lean of your axis across their port's face.
	if ringCells >= centerlineCrossCells && e <= centerlineStepOutFrac*v.clStepM/2 {
		dr, du := centerlineCrossOffsetM(st)
		cross := centerlineClampToWindow(g, scale, centre, orbital.Vec3{X: dr, Y: du})
		c.SetCellOverlayColored(centre.Add(cross), '+', own)
	}
	return v.clStepM
}

// composeCenterlineScope draws the panel over the canvas at its top-right,
// after the navball (ADR 0055 A1). No panel below Design Size or without a
// port to look at (A2).
func (v *OrbitView) composeCenterlineScope(w *sim.World, canvasStr string, cCols, cRows int) string {
	st, ok := w.ProximityState()
	g, fits := centerlineLayout(cCols, cRows)
	if !ok || !st.HasPort || !fits {
		return canvasStr
	}
	step := v.drawCenterline(w, st, g)

	edge := lipgloss.NewStyle().Foreground(v.theme.Primary.GetForeground())
	inner := g.panelW - 2
	left, right := "CENTERLINE  "+strconv.FormatFloat(step, 'f', -1, 64)+" m", "port "+centerlineTitleRange(st)
	gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	title := v.theme.Primary.Render("CENTERLINE") + v.theme.Dim.Render(left[len("CENTERLINE"):]+strings.Repeat(" ", gap)+right)
	pad := strings.Repeat(" ", centerlinePadCols)
	lines := []string{edge.Render("╭" + strings.Repeat("─", inner) + "╮"), edge.Render("│") + title + edge.Render("│")}
	for _, row := range strings.Split(v.cl.String(), "\n") {
		lines = append(lines, edge.Render("│")+pad+row+pad+edge.Render("│"))
	}
	lines = append(lines, edge.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(overlayStyledBlock(strings.Split(canvasStr, "\n"), strings.Join(lines, "\n"), g.atRow, g.atCol, cCols), "\n")
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
		// Room from the ring centre to the window edge on the lean's side.
		room := halfPx - math.Copysign(1, offPx)*centrePx
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
