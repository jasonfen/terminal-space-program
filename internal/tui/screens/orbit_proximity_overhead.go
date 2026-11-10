package screens

// The landing view's OVERHEAD inset (ADR 0067, answers P17 to P19): a framed
// panel in the top-right corner, the Centerline scope's twin. Always on while
// the landing view is open, hidden by F2 with the other instruments. Looking
// straight down: the track runs right, the left of the track is up the panel.
// Along the track the scale is true and ranges itself (like the scope, it
// ignores the side picture's zoom); across it has its own scale, printed as a
// bar on the left edge, so the thrust pointer's and the velocity stub's drawn
// angles are stretched with it (P19, accepted).
//
// Reason for a new file: the inset's drawing, as orbit_centerline.go is the
// scope's.

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

// Playtest-tuned feel numbers (ADR 0067: provisional until flown).
const (
	// overheadFlagFrac is how far from the left edge the flag sits, so the
	// approach has room behind it and the stop a little ahead.
	overheadFlagFrac = 0.7
	// overheadCrossFloorM is the smallest cross bar the inset ranges to.
	overheadCrossFloorM = 50.0
	// overheadStubPx is the ground-velocity stub's length in dots.
	overheadStubPx = 8.0
	// overheadPointerMaxPx is the thrust pointer's length at a full lean, which
	// is overheadPointerFullLean (sin of the lean) and over; shorter below it.
	overheadPointerMaxPx    = 14.0
	overheadPointerFullLean = 0.26
	// overheadMinSpeedMS is the ground speed under which the stub is not drawn.
	overheadMinSpeedMS = 0.5
	// The ladder gates, the scope's own pair: out at 95 % of the width, back in
	// when the need would sit inside 45 % of the next step down.
	overheadOutFrac = 0.95
	overheadInFrac  = 0.45
)

// The panel: an 8-column gutter (the cross bar and its label), a 24 x 12 cell
// disc, a title row, a legend row and the border.
const (
	overheadGutterCols = 8
	overheadDiscCols   = 24
	overheadDiscRows   = 12
	overheadPanelW     = overheadGutterCols + overheadDiscCols + 2
	overheadPanelH     = overheadDiscRows + 4
	overheadBarCells   = 10 // the along bar's width in cells
	overheadBarRows    = 6  // the cross bar's height in rows
)

// overheadLadder is the bar values in metres: 1, 2, 5 per decade.
var overheadLadder = func() []float64 {
	var l []float64
	for d := 1.0; d <= 1e7; d *= 10 {
		l = append(l, d, 2*d, 5*d)
	}
	return l
}()

// overheadPick ranges a ladder: the smallest rung whose capacity (rung * per)
// holds need, holding prev until need leaves its band (ranged=false: an entry).
func overheadPick(prev int, ranged bool, need, per float64) int {
	last := len(overheadLadder) - 1
	if !ranged {
		prev = 0
	}
	for prev < last && need > overheadOutFrac*overheadLadder[prev]*per {
		prev++
	}
	for ranged && prev > 0 && need < overheadInFrac*overheadLadder[prev-1]*per {
		prev--
	}
	return prev
}

// overheadFloorRung is the first ladder rung at or over m.
func overheadFloorRung(m float64) int {
	for i, r := range overheadLadder {
		if r >= m {
			return i
		}
	}
	return len(overheadLadder) - 1
}

// overheadPoint is a ground point (metres along the track and to the left of
// it, from the flag) as a canvas coordinate; stretch is the cross scale over
// the along scale.
func overheadPoint(along, left, stretch float64) orbital.Vec3 {
	return orbital.Vec3{X: along, Y: left * stretch}
}

// overheadRel reads a world point as (along, left) metres from the flag.
func overheadRel(st sim.ProximityState, p orbital.Vec3) (along, left float64) {
	d := p.Sub(st.TargetWorld)
	return d.Dot(st.Frame.AlongTrack), d.Dot(st.Frame.CrossTrack)
}

func (v *OrbitView) overheadCanvas() *widgets.Canvas {
	if v.ov == nil {
		v.ov = widgets.NewCanvas(4, 4)
	}
	return v.ov
}

// overheadMarks are the points the inset ranges to, as (along, left) from the flag.
type overheadMarks struct {
	craft, cs, stop [2]float64
	hasStop         bool
	arc             []orbital.Vec3
}

func (v *OrbitView) overheadGather(w *sim.World, st sim.ProximityState) overheadMarks {
	var m overheadMarks
	m.craft[0], m.craft[1] = overheadRel(st, st.CraftWorld)
	m.cs[0], m.cs[1] = overheadRel(st, computerSiteWorld(st))
	arc, ok := v.stopArc, v.stopArcOK
	if !v.stopArcFresh { // the landing scene did not draw this frame
		arc, ok = w.LandingStopArc()
	}
	if ok {
		m.arc = arc.Points
		if arc.Stopped && len(arc.Points) > 0 {
			m.stop[0], m.stop[1] = overheadRel(st, arc.Points[len(arc.Points)-1])
			m.hasStop = true
		}
	}
	return m
}

// drawOverhead paints the disc onto v.ov and returns the two bar values.
func (v *OrbitView) drawOverhead(w *sim.World, st sim.ProximityState) (alongBarM, crossBarM float64) {
	c := v.overheadCanvas()
	c.Resize(overheadDiscCols, overheadDiscRows)
	c.Clear()
	c.SetBasis(widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Y: 1}})
	m := v.overheadGather(w, st)

	// Along: the flag sits overheadFlagFrac from the left; the width must hold
	// what is behind it on that side and what is ahead on the other.
	minA, maxA := math.Min(0, math.Min(m.craft[0], m.cs[0])), math.Max(0, math.Max(m.craft[0], m.cs[0]))
	if m.hasStop {
		minA, maxA = math.Min(minA, m.stop[0]), math.Max(maxA, m.stop[0])
	}
	needW := math.Max(-minA/overheadFlagFrac, maxA/(1-overheadFlagFrac))
	alongPer := float64(overheadDiscCols) / overheadBarCells // metres of width per metre of bar
	v.ovAlong = overheadPick(v.ovAlong, v.ovRanged, needW, alongPer)
	// Across: the larger of cross: and the aim's cross component, floor 50 m;
	// the vessel's own and the computer's site's offsets join them so nothing
	// the inset draws runs off its edge.
	lb, _ := w.LandBesideReading()
	ext := math.Abs(lb.CrossM)
	if cr := w.ActiveCraft(); cr != nil && cr.Landing != nil {
		ext = math.Max(ext, math.Abs(cr.Landing.AimAcrossM))
	}
	ext = math.Max(ext, math.Max(math.Abs(m.craft[1]), math.Abs(m.cs[1])))
	if m.hasStop {
		ext = math.Max(ext, math.Abs(m.stop[1]))
	}
	crossPer := float64(overheadDiscRows) / overheadBarRows
	v.ovAcross = max(overheadPick(v.ovAcross, v.ovRanged, 2*ext, crossPer), overheadFloorRung(overheadCrossFloorM))
	v.ovRanged = true
	alongBarM, crossBarM = overheadLadder[v.ovAlong], overheadLadder[v.ovAcross]

	pxPerM := 2.0 * overheadBarCells / alongBarM // dots per metre along
	crossPx := 4.0 * overheadBarRows / crossBarM // dots per metre across
	stretch := crossPx / pxPerM
	widthM := alongBarM * alongPer
	c.SetScale(pxPerM)
	c.Center(orbital.Vec3{X: (0.5 - overheadFlagFrac) * widthM})
	pt := func(a, l float64) orbital.Vec3 { return overheadPoint(a, l, stretch) }

	// The track line through the flag.
	reach := widthM
	c.PlotPolylineClass([]orbital.Vec3{pt(-reach, 0), pt(reach, 0)}, render.ColorDim, widgets.ClassScenery)
	// The stop arc's trace on the ground, dashed, you to the stop.
	if len(m.arc) > 1 {
		tr := make([]orbital.Vec3, len(m.arc))
		for i, p := range m.arc {
			a, l := overheadRel(st, p)
			tr[i] = pt(a, l)
		}
		c.PlotPolylineClass(tr, render.ColorPredictedPath, widgets.ClassPlanned)
	}
	if m.hasStop {
		c.SetCellOverlayColored(pt(m.stop[0], m.stop[1]), landingStopGlyph, render.ColorPredictedPath)
	}
	// ⌖ first, ⚑ over it, as the side picture does.
	differs := computerSiteWorld(st).Sub(st.TargetWorld).Norm() > 0.5
	c.SetCellOverlayColored(pt(m.cs[0], m.cs[1]), computerSiteGlyph, render.ColorTarget)
	if flagRevealed(differs, st.RangeM) {
		c.SetCellOverlayColored(pt(0, 0), landingSiteMark(w), render.ColorTarget)
	}

	// You, with the ground-velocity stub (dotted) and the thrust pointer (solid).
	own := render.ColorCraftMarker
	craft := w.ActiveCraft()
	if craft != nil && craft.Color != "" {
		own = lipgloss.Color(craft.Color)
	}
	here := pt(m.craft[0], m.craft[1])
	if speed := math.Hypot(st.FwdVelMS, st.LatVelMS); speed >= overheadMinSpeedMS {
		d := orbital.Vec3{X: st.FwdVelMS, Y: -st.LatVelMS * stretch}.Unit() // LatVelMS is + right
		c.PlotPolylineClass([]orbital.Vec3{here, here.Add(d.Scale(overheadStubPx / pxPerM))}, own, widgets.ClassScenery)
	}
	if craft != nil {
		if a, l, ok := overheadNoseLean(craft.CurrentAttitudeDir, craft.State.R, st); ok {
			h := math.Hypot(a, l)
			d := orbital.Vec3{X: a, Y: l * stretch}.Unit()
			n := overheadPointerMaxPx * math.Min(1, h/overheadPointerFullLean)
			c.PlotPolylineClass([]orbital.Vec3{here, here.Add(d.Scale(n / pxPerM))}, render.ColorWarning, widgets.ClassReal)
		}
	}
	c.SetCellOverlayColored(here, '➤', own)
	return alongBarM, crossBarM
}

// overheadNoseLean is the nose's horizontal component over the ground under
// the vessel (up = its own radial), along the track and to the left of it.
// ok=false for no attitude or a nose straight up.
func overheadNoseLean(nose, craftR orbital.Vec3, st sim.ProximityState) (along, left float64, ok bool) {
	up := craftR.Unit()
	if nose.Norm() == 0 || up.Norm() == 0 {
		return 0, 0, false
	}
	n := nose.Unit()
	f := st.Frame.AlongTrack.Sub(up.Scale(st.Frame.AlongTrack.Dot(up)))
	if f.Norm() < 1e-9 {
		return 0, 0, false
	}
	f = f.Unit()
	l := up.Cross(f)
	along, left = n.Dot(f), n.Dot(l)
	return along, left, math.Hypot(along, left) > 1e-3
}

// overheadBarLabel is a bar's value in metres as the readout ladder prints it.
func overheadBarLabel(m float64) string { return readout.Distance(m) }

// composeOverheadInset draws the panel over the canvas at its top-right. No
// panel off the landing view or below the Design Size (the first thing dropped).
func (v *OrbitView) composeOverheadInset(w *sim.World, canvasStr string, cCols, cRows int) string {
	st, ok := w.ProximityState()
	if !ok || !st.Surface || cCols < designCanvasCols || cRows < designCanvasRows {
		return canvasStr
	}
	alongBar, crossBar := v.drawOverhead(w, st)

	edge := lipgloss.NewStyle().Foreground(v.theme.Primary.GetForeground())
	inner := overheadPanelW - 2
	title := v.theme.Primary.Render("OVERHEAD") + "  " + truncateCells(st.TargetName, inner-10)
	title += strings.Repeat(" ", max(0, inner-lipgloss.Width(title)))

	gutter := make([]string, overheadDiscRows)
	for i := range gutter {
		gutter[i] = strings.Repeat(" ", overheadGutterCols)
	}
	top := (overheadDiscRows - overheadBarRows) / 2
	for i := 0; i < overheadBarRows; i++ {
		g := "│"
		switch i {
		case 0:
			g = "┬"
		case overheadBarRows - 1:
			g = "┴"
		}
		cell := g + strings.Repeat(" ", overheadGutterCols-1)
		if i == overheadBarRows/2 {
			cell = g + " " + overheadBarLabel(crossBar)
			cell += strings.Repeat(" ", max(0, overheadGutterCols-lipgloss.Width(cell)))
		}
		gutter[top+i] = cell
	}
	lines := []string{edge.Render("╭" + strings.Repeat("─", inner) + "╮"), edge.Render("│") + title + edge.Render("│")}
	for i, row := range strings.Split(v.ov.String(), "\n") {
		lines = append(lines, edge.Render("│")+v.theme.Dim.Render(gutter[i])+row+edge.Render("│"))
	}
	bar := "├" + centerCells(overheadBarLabel(alongBar), overheadBarCells-2, '─') + "┤"
	legend := "↑L  →track"
	legend += strings.Repeat(" ", max(1, inner-lipgloss.Width(legend)-lipgloss.Width(bar)-1)) + bar + " "
	lines = append(lines, edge.Render("│")+v.theme.Dim.Render(legend)+edge.Render("│"), edge.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(overlayStyledBlock(strings.Split(canvasStr, "\n"), strings.Join(lines, "\n"), 0, cCols-overheadPanelW, cCols), "\n")
}

// centerCells pads s to n cells with fill, centred (s longer than n is kept).
func centerCells(s string, n int, fill rune) string {
	gap := n - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	l := gap / 2
	return strings.Repeat(string(fill), l) + s + strings.Repeat(string(fill), gap-l)
}
