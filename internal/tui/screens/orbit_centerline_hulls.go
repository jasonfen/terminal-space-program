package screens

import (
	"math"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// centerlineHullSegments is how finely a hull circle is sampled: fine enough
// that clipping at the rim lands within a dot or two at the 2 m step.
const centerlineHullSegments = 360

// drawCenterlineHulls draws both hulls end-on onto the scope canvas c (ADR
// 0055 A5), before the ring and the cross so they win shared cells. scale is
// dots per metre. Their hull is a dotted circle of Target.WidthM / 2 around
// the ring's true centre (the lateral offset, even when the ring itself is
// the rim glyph); yours is a circle of Own.WidthM / 2 around the crosshair,
// stretched into a stadium toward where your stack bottom projects when your
// nose leans: the far circle sits at -Own.NoseM * (sin right, sin up), joined
// to the near one by two tangent lines. Scenery class (dotted, never filled),
// clipped at the rim; theirs goes Real (solid) while st.HullsAlongside. Each hull is skipped when its own width is zero.
func (v *OrbitView) drawCenterlineHulls(c *widgets.Canvas, w *sim.World, st sim.ProximityState, scale float64) {
	rimM := (float64(c.Cols()*2)/2 - centerlineRimInsetPx) / scale // the rim, in metres
	own := render.ColorCraftMarker
	if a := w.ActiveCraft(); a != nil && a.Color != "" {
		own = lipgloss.Color(a.Color)
	}
	x, y := orbital.Vec3{X: 1}, orbital.Vec3{Y: 1}

	if r := st.Target.WidthM / 2; r > 0 {
		centre := orbital.Vec3{X: st.OffAxisRightM, Y: st.OffAxisUpM}
		class := widgets.ClassScenery
		if st.HullsAlongside {
			class = widgets.ClassReal // beside you: solid (ADR 0055 B2)
		}
		plotClipped(c, proximityRingPointsN(centre, x, y, r, centerlineHullSegments), render.ColorTarget, rimM, class)
	}

	r := st.Own.WidthM / 2
	if r <= 0 {
		return
	}
	plotClipped(c, proximityRingPointsN(orbital.Vec3{}, x, y, r, centerlineHullSegments), own, rimM, widgets.ClassScenery)
	far := orbital.Vec3{
		X: -st.Own.NoseM * math.Sin(st.MisalignRightDeg*math.Pi/180),
		Y: -st.Own.NoseM * math.Sin(st.MisalignUpDeg*math.Pi/180),
	}
	if far.Norm() == 0 {
		return
	}
	plotClipped(c, proximityRingPointsN(far, x, y, r, centerlineHullSegments), own, rimM, widgets.ClassScenery)
	side := orbital.Vec3{X: -far.Y, Y: far.X}.Unit().Scale(r) // across the stretch
	for _, s := range []float64{1, -1} {
		a, b := side.Scale(s), far.Add(side.Scale(s))
		line := make([]orbital.Vec3, 0, 65)
		for i := 0; i <= 64; i++ {
			line = append(line, a.Add(b.Sub(a).Scale(float64(i)/64)))
		}
		plotClipped(c, line, own, rimM, widgets.ClassScenery)
	}
}

// proximityRingPointsN is proximityRingPoints with a chosen sample count.
func proximityRingPointsN(center, e1, e2 orbital.Vec3, radiusM float64, n int) []orbital.Vec3 {
	pts := make([]orbital.Vec3, 0, n+1)
	for i := 0; i <= n; i++ {
		theta := 2 * math.Pi * float64(i) / float64(n)
		pts = append(pts, center.Add(e1.Scale(radiusM*math.Cos(theta))).Add(e2.Scale(radiusM*math.Sin(theta))))
	}
	return pts
}

// plotClipped draws the runs of pts that lie within rimM of the crosshair as
// polylines of the given class in colour.
func plotClipped(c *widgets.Canvas, pts []orbital.Vec3, colour lipgloss.Color, rimM float64, class widgets.LineClass) {
	var run []orbital.Vec3
	flush := func() {
		if len(run) > 1 {
			c.PlotPolylineClass(run, colour, class)
		}
		run = nil
	}
	for _, p := range pts {
		if p.Norm() > rimM {
			flush()
			continue
		}
		run = append(run, p)
	}
	flush()
}
