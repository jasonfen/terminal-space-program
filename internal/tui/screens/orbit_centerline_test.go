package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// ADR 0055 slice 2: the Centerline view. Every positional pin here measures
// the cell (or the lit pixel) it names, in the rendered frame or the right
// pane's canvas, from arithmetic done in the test, never by reading back the
// number the code computed.

// Design Size right pane, as the ADR's cell budget arithmetic gives it and
// the first capture confirmed: 69 + 69 columns, the navball's frame at screen
// column 104, the crosshair at pane column 17 (screen column 86) on row 15.
const (
	clPaneCols  = 69
	clCrossCol  = 17 // screen column 86
	clCrossRow  = 15
	clCrossPxX  = 2*clCrossCol + 1
	clCrossPxY  = 4*clCrossRow + 2
	clWindowPx  = 66.0 // 33 columns across
	clRingR     = 0.5
	clGateDeg   = 4.0
	clFrameLine = 2 // canvas row r is frame line r+2 (title, top border)
)

func renderSplit(t *testing.T, w *sim.World) (*OrbitView, string) {
	t.Helper()
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	out := v.Render(w, 0, DesignWidth, DesignHeight)
	return v, out
}

// frameRune is the canvas cell (col, row) of a rendered frame.
func frameRune(frame string, col, row int) rune {
	lines := strings.Split(ansi.Strip(frame), "\n")
	if row+clFrameLine >= len(lines) {
		return 0
	}
	r := []rune(lines[row+clFrameLine])
	if col+1 >= len(r) {
		return 0
	}
	return r[col+1]
}

// ringWorld parks the active vessel so the target's port sits up metres above
// and right metres to the right of its own port, 3 m ahead along the axis.
// portedPairWorld offsets are the active port from the target's, in LVLH
// (along, radial, cross), and the face basis's up is radial and its right is
// cross-track for an along-track nose, so the sign flips.
func ringWorld(t *testing.T, up, right, tiltDeg float64) *sim.World {
	t.Helper()
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 3, Y: -up, Z: -right}, tiltDeg)
	st, ok := w.ProximityState()
	if !ok || !st.HasPort {
		t.Fatal("fixture has no port scene")
	}
	if math.Abs(st.OffAxisUpM-up) > 1e-6 || math.Abs(st.OffAxisRightM-right) > 1e-6 {
		t.Fatalf("fixture lateral = (%.4f right, %.4f up), want (%.2f, %.2f)", st.OffAxisRightM, st.OffAxisUpM, right, up)
	}
	return w
}

// litPixels lists the right pane's lit dots that are not on the crosshair's
// own two lines.
func litExcludingCrosshair(c *widgets.Canvas) (minX, minY, maxX, maxY, n int) {
	minX, minY = 1<<30, 1<<30
	maxX, maxY = -1, -1
	for py := 0; py < c.Rows()*4; py++ {
		for px := 0; px < c.Cols()*2; px++ {
			if !c.SubPixelSet(px, py) || px == clCrossPxX || py == clCrossPxY {
				continue
			}
			n++
			minX, maxX = min(minX, px), max(maxX, px)
			minY, maxY = min(minY, py), max(maxY, py)
		}
	}
	return
}

// expectedScale is the entry fit's pixels per metre, from the ADR's rule:
// the 33-column window across the larger of 2 x (lateral + 0.5 m ring) and 2 m.
func expectedScale(lateralM float64) float64 {
	return clWindowPx / math.Max(2*(lateralM+clRingR), 2)
}

// The layout the ADR's arithmetic predicted, confirmed on the first capture:
// two 69-cell panes, crosshair at screen column 86 row 15, a 33 x 17 window.
func TestCenterlineLayoutAtDesignSize(t *testing.T) {
	g, ok := centerlineLayout(138, 37)
	if !ok {
		t.Fatal("no split at the Design Size canvas (138x37)")
	}
	if g.leftCols != 69 || g.paneCols != 69 {
		t.Errorf("panes = %d + %d columns, want 69 + 69", g.leftCols, g.paneCols)
	}
	if g.leftCols+g.crossCol != 86 || g.crossRow != 15 {
		t.Errorf("crosshair at screen column %d row %d, want 86, 15", g.leftCols+g.crossCol, g.crossRow)
	}
	if 2*g.winHalfCols+1 != 33 || 2*g.winHalfRows+1 != 17 {
		t.Errorf("ring window = %d x %d, want 33 x 17", 2*g.winHalfCols+1, 2*g.winHalfRows+1)
	}
}

// The right pane exists at 140x40 with its label on the pane's first cell, and
// is absent below the Design Size, where the left pane keeps the whole width.
func TestCenterlineSplitOnlyAtDesignSize(t *testing.T) {
	w := ringWorld(t, 1, 0.6, 0)
	v, out := renderSplit(t, w)
	if v.canvas.Cols() != 69 {
		t.Errorf("left pane = %d columns at 140x40, want 69", v.canvas.Cols())
	}
	for i, r := range []rune("Centerline") {
		if got := frameRune(out, 69+i, 0); got != r {
			t.Fatalf("pane cell (%d, 0) = %q, want %q of the Centerline label", 69+i, got, r)
		}
	}

	small := NewOrbitView(chipTestTheme())
	small.Resize(80, 24)
	text := ansi.Strip(small.Render(w, 0, 80, 24))
	if strings.Contains(text, "Centerline") {
		t.Error("80x24 draws the Centerline pane; Compact Form drops it first")
	}
	if small.canvas.Cols() != 78 {
		t.Errorf("80x24 left pane = %d columns, want the full 78", small.canvas.Cols())
	}
	// One row short of the Design Size is compact too.
	short := NewOrbitView(chipTestTheme())
	short.Resize(DesignWidth, DesignHeight-1)
	if strings.Contains(ansi.Strip(short.Render(w, 0, DesignWidth, DesignHeight-1)), "Centerline") {
		t.Error("140x39 draws the Centerline pane")
	}
}

// Without a port to look at there is nothing to centre on: the side view
// keeps the full width.
func TestCenterlineAbsentWithoutAPort(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"s-ivb-1"}, orbital.Vec3{X: 3}, 0)
	v, out := renderSplit(t, w)
	if strings.Contains(ansi.Strip(out), "Centerline") || v.canvas.Cols() != 138 {
		t.Errorf("a portless target split the view (left pane %d columns)", v.canvas.Cols())
	}
}

// The crosshair is fixed at the pane's cell (86, 15): lit there, in the
// frame, whatever the ring is doing.
func TestCenterlineCrosshairIsFixedAtItsCell(t *testing.T) {
	for _, off := range [][2]float64{{0, 0}, {1.2, -0.8}, {-3, 2}} {
		w := ringWorld(t, off[0], off[1], 0)
		v, out := renderSplit(t, w)
		if !v.cl.SubPixelSet(clCrossPxX, clCrossPxY) {
			t.Errorf("lateral %v: pixel (%d, %d) at the crosshair centre is not lit", off, clCrossPxX, clCrossPxY)
		}
		// A straight nose puts the standoff cross's '+' on the crosshair.
		if r := frameRune(out, 69+clCrossCol, clCrossRow); (r < 0x2801 || r > 0x28FF) && r != '+' {
			t.Errorf("lateral %v: frame cell (86, 15) = %q, want crosshair ink", off, r)
		}
	}
}

// The crosshair's two arms cross on the pixel of the pane's cell (86, 15): the
// vertical arm is lit in that pixel column and dark three columns either side,
// the horizontal arm in that row and dark three rows above and below.
func TestCenterlineCrosshairArmsCrossAtTheCell(t *testing.T) {
	w := ringWorld(t, 0, 0, 0) // ring 16 px out: clear of the arms
	v, _ := renderSplit(t, w)
	lit := func(px0, py0, px1, py1 int) (n int) {
		for py := py0; py <= py1; py++ {
			for px := px0; px <= px1; px++ {
				if v.cl.SubPixelSet(px, py) {
					n++
				}
			}
		}
		return
	}
	if n := lit(clCrossPxX, clCrossPxY-10, clCrossPxX, clCrossPxY+10); n < 6 {
		t.Errorf("vertical arm lit %d dots in pixel column %d, want a line", n, clCrossPxX)
	}
	if n := lit(clCrossPxX-3, clCrossPxY-10, clCrossPxX-3, clCrossPxY+10) + lit(clCrossPxX+3, clCrossPxY-10, clCrossPxX+3, clCrossPxY+10); n != 0 {
		t.Errorf("%d dots lit three pixel columns off the crosshair's column", n)
	}
	if n := lit(clCrossPxX-5, clCrossPxY, clCrossPxX+5, clCrossPxY); n < 3 {
		t.Errorf("horizontal arm lit %d dots in pixel row %d, want a line", n, clCrossPxY)
	}
	if n := lit(clCrossPxX-5, clCrossPxY-3, clCrossPxX+5, clCrossPxY-3) + lit(clCrossPxX-5, clCrossPxY+3, clCrossPxX+5, clCrossPxY+3); n != 0 {
		t.Errorf("%d dots lit three pixel rows off the crosshair's row", n)
	}
}

// The ring is centred at (right, up) metres from the crosshair at the shared
// fit: 1 m up and 0.6 m left of the crosshair.
func TestCenterlineRingSitsAtTheLateralOffset(t *testing.T) {
	const up, right = 1.0, -0.6
	w := ringWorld(t, up, right, 0)
	v, _ := renderSplit(t, w)
	scale := expectedScale(math.Hypot(up, right))
	wantX := float64(clCrossPxX) + right*scale
	wantY := float64(clCrossPxY) - up*scale
	minX, minY, maxX, maxY, n := litExcludingCrosshair(v.cl)
	if n == 0 {
		t.Fatal("no ring ink in the right pane")
	}
	midX, midY := float64(minX+maxX)/2, float64(minY+maxY)/2
	if math.Abs(midX-wantX) > 3 || math.Abs(midY-wantY) > 3 {
		t.Errorf("ring centre at pixel (%.1f, %.1f), want (%.1f, %.1f) = crosshair + (%.2f m right, %.2f m up) at %.2f px/m",
			midX, midY, wantX, wantY, right, up, scale)
	}
	// And its size is the 0.5 m radius at that scale.
	if got, want := float64(maxX-minX)/2, clRingR*scale; math.Abs(got-want) > 3 {
		t.Errorf("ring half-width = %.1f px, want %.1f (0.5 m at %.2f px/m)", got, want, scale)
	}
}

// A w pulse in PORT (BurnPortUp) moves the ring DOWN the pane by the metres
// the pulse bought at the pane's scale, and the lateral: row's up number falls
// by the same: the ring, the row and the pulse read one PortFaceBasis.
func TestCenterlinePortUpPulseMovesTheRingDown(t *testing.T) {
	w := ringWorld(t, 1.0, 0, 0)
	w.Clock.Paused = false
	v, _ := renderSplit(t, w)
	_, y0min, _, y0max, _ := litExcludingCrosshair(v.cl)
	st0, _ := w.ProximityState()

	c := w.ActiveCraft()
	// Hold both noses exactly as placed: the SAS slew would swing the long
	// stack about its pivot and move the port the ring is measuring.
	w.Crafts[0].SlewRateDegPerSec, w.Crafts[1].SlewRateDegPerSec = 1e-9, 1e-9
	c.EngineMode = spacecraft.EngineRCS
	if !w.FireRCSPulse(spacecraft.BurnPortUp) {
		t.Fatal("PortUp pulse refused")
	}
	t0 := w.Clock.SimTime
	for i := 0; i < 100; i++ {
		w.Tick()
	}
	secs := w.Clock.SimTime.Sub(t0).Seconds()
	moved := 0.1 * secs // RCSDvQuantum 0.1 m/s along up
	out := v.Render(w, 0, DesignWidth, DesignHeight)
	_ = out
	st1, _ := w.ProximityState()
	if got := st0.OffAxisUpM - st1.OffAxisUpM; math.Abs(got-moved) > 0.05*moved {
		t.Fatalf("lateral up fell by %.3f m, the pulse bought %.3f m", got, moved)
	}
	_, y1min, _, y1max, _ := litExcludingCrosshair(v.cl)
	scale := expectedScale(1.0)
	dy := float64(y1min+y1max)/2 - float64(y0min+y0max)/2
	if dy <= 0 {
		t.Fatalf("ring moved %.1f px (positive is down) after pushing up; want down", dy)
	}
	if want := moved * scale; math.Abs(dy-want) > 4 {
		t.Errorf("ring moved %.1f px down, want %.1f (%.3f m at %.2f px/m)", dy, want, moved, scale)
	}
}

// crossCell finds the standoff cross's '+' in the right pane.
func crossCell(frame string) (col, row int, ok bool) {
	for r := 1; r < 36; r++ {
		for c := 69; c < 104; c++ {
			if frameRune(frame, c, r) == '+' {
				return c - 69, r, true
			}
		}
	}
	return 0, 0, false
}

// pxCell is the pane cell holding pixel (px, py).
func pxCell(px, py float64) (int, int) { return int(math.Floor(px)) / 2, int(math.Floor(py)) / 4 }

// The standoff cross sits on the ring's edge at exactly 4 degrees of lean,
// inside at 3, and below the centre for a nose tipped down.
func TestCenterlineCrossReachesTheEdgeAtTheGate(t *testing.T) {
	scale := expectedScale(0)
	ringR := clRingR * scale
	cases := []struct {
		tilt   float64
		wantPx float64 // up is positive
	}{
		{4, ringR},
		{3, ringR * 0.75},
		{-3, -ringR * 0.75},
		{-4, -ringR},
		{2, ringR * 0.5},
	}
	for _, tc := range cases {
		w := ringWorld(t, 0, 0, tc.tilt)
		_, out := renderSplit(t, w)
		col, row, ok := crossCell(out)
		if !ok {
			t.Fatalf("tilt %+g: no standoff cross in the right pane", tc.tilt)
		}
		wc, wr := pxCell(float64(clCrossPxX), float64(clCrossPxY)-tc.wantPx)
		if col != wc || row != wr {
			t.Errorf("tilt %+g deg: cross at pane cell (%d, %d), want (%d, %d) = %.1f px above the ring centre",
				tc.tilt, col, row, wc, wr, tc.wantPx)
		}
	}
}

// A nose tipped toward the face's right puts the cross right of the ring's
// centre by the same rule, and a centred nose puts it on the crosshair.
func TestCenterlineCrossFollowsYawAndCentresWhenStraight(t *testing.T) {
	scale := expectedScale(0)
	ringR := clRingR * scale
	w := ringWorld(t, 0, 0, 0)
	_, out := renderSplit(t, w)
	if col, row, ok := crossCell(out); !ok || col != clCrossCol || row != clCrossRow {
		t.Errorf("straight nose: cross at (%d, %d) ok=%v, want the crosshair cell (%d, %d)", col, row, ok, clCrossCol, clCrossRow)
	}

	// Yaw 4 degrees toward the face's right (cross-track for an along-track nose).
	a := w.Crafts[0]
	frame, _ := orbital.LVLHBasis(w.Crafts[1].State.R, w.Crafts[1].State.V)
	r := 4 * math.Pi / 180
	a.CurrentAttitudeDir = frame.AlongTrack.Scale(-math.Cos(r)).Add(frame.CrossTrack.Scale(math.Sin(r)))
	st, _ := w.ProximityState()
	if math.Abs(st.MisalignRightDeg-4) > 0.01 {
		t.Fatalf("fixture yaw reads %.3f deg right, want 4", st.MisalignRightDeg)
	}
	_, out = renderSplit(t, w)
	col, row, ok := crossCell(out)
	// Turning the nose swings its port (the lever arm is the stack), so the
	// ring is wherever the sim now puts it; the cross is one ring radius right of it.
	scale = expectedScale(math.Hypot(st.OffAxisRightM, st.OffAxisUpM))
	ringR = clRingR * scale
	wc, wr := pxCell(float64(clCrossPxX)+st.OffAxisRightM*scale+ringR, float64(clCrossPxY)-st.OffAxisUpM*scale)
	if !ok || col != wc || row != wr {
		t.Errorf("4 deg right: cross at (%d, %d) ok=%v, want (%d, %d)", col, row, ok, wc, wr)
	}
}

// Below 3 cells across the ring is the port glyph, below 5 there is no cross.
func TestCenterlineSmallRingIsTheGlyphAndHasNoCross(t *testing.T) {
	w := ringWorld(t, 30, 0, 4) // 30 m up: the ring is under a cell
	_, out := renderSplit(t, w)
	if _, _, ok := crossCell(out); ok {
		t.Error("a sub-5-cell ring carries a standoff cross")
	}
	scale := expectedScale(30)
	col, row := pxCell(float64(clCrossPxX), float64(clCrossPxY)-30*scale)
	if got := frameRune(out, 69+col, row); got != proximityPortMarkGlyph {
		t.Errorf("cell (%d, %d) = %q, want the %q port glyph at the ring's centre", 69+col, row, got, proximityPortMarkGlyph)
	}
}

// The ring is dashed until the dock gate holds and solid when it does.
func TestCenterlineRingSolidOnlyWhenGateReady(t *testing.T) {
	ready := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, 0)
	fast := withRelVelocity(portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, 0), orbital.Vec3{Z: 0.3})
	if st, _ := ready.ProximityState(); !ready.ProximityDockGateReady(st) {
		t.Fatal("fixture gate is not ready")
	}
	if st, _ := fast.ProximityState(); fast.ProximityDockGateReady(st) {
		t.Fatal("fast fixture's gate is ready")
	}
	vr, _ := renderSplit(t, ready)
	vf, _ := renderSplit(t, fast)
	_, _, _, _, nr := litExcludingCrosshair(vr.cl)
	_, _, _, _, nf := litExcludingCrosshair(vf.cl)
	if nr < nf*3/2 {
		t.Errorf("ring ink: %d dots when the gate holds, %d when it does not; want a solid ring well above the dashed one", nr, nf)
	}
}

// +/- zoom both panes by the same factor from their own bases, and the scale
// bar is drawn once.
func TestCenterlineZoomMovesBothPanesAndTheBarIsDrawnOnce(t *testing.T) {
	w := ringWorld(t, 1, 0.5, 0)
	v, _ := renderSplit(t, w)
	l0, r0 := v.canvas.Scale(), v.cl.Scale()
	v.ZoomIn()
	out := v.Render(w, 0, DesignWidth, DesignHeight)
	l1, r1 := v.canvas.Scale(), v.cl.Scale()
	if math.Abs(l1/l0-1.25) > 1e-9 || math.Abs(r1/r0-1.25) > 1e-9 {
		t.Errorf("after + the left pane scaled %.4fx and the Centerline pane %.4fx, want 1.25x both", l1/l0, r1/r0)
	}
	v.ZoomOut()
	v.ZoomOut()
	out = v.Render(w, 0, DesignWidth, DesignHeight)
	if got := v.cl.Scale() / r0; math.Abs(got-1/1.25) > 1e-9 {
		t.Errorf("after + - - the Centerline pane is %.4fx its fit, want %.4f", got, 1/1.25)
	}
	bar, ok := scaleBarFor(v.canvas.Scale())
	if !ok {
		t.Fatal("no scale bar for the left pane")
	}
	if n := strings.Count(ansi.Strip(out), bar.text()); n != 1 {
		t.Errorf("scale bar %q drawn %d times, want once", bar.text(), n)
	}
}

// The corridor: a dotted line from the target's port along its axis out to
// 50 m, in the left pane, nothing past it.
func TestProximityCorridorRunsFromThePortAlongItsAxisTo50m(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 40, Y: 10}, 0)
	tgt := w.Crafts[1]
	frame, _ := orbital.LVLHBasis(tgt.State.R, tgt.State.V)
	tgt.CurrentAttitudeDir = frame.AlongTrack.Add(frame.RadialOut).Unit() // off both bars
	v, _ := renderSplit(t, w)
	st, ok := w.ProximityState()
	if !ok || !st.HasPort {
		t.Fatal("no port scene")
	}
	port := st.TargetWorld.Add(st.Target.Axis.Scale(st.Target.Ports[0].OffsetM))
	lit := func(d float64) bool {
		px, py, on := v.canvas.Project(port.Add(st.Target.Axis.Unit().Scale(d)))
		if !on {
			t.Fatalf("%.0f m along the axis is off the left pane", d)
		}
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				if v.canvas.SubPixelSet(px+dx, py+dy) {
					return true
				}
			}
		}
		return false
	}
	for _, d := range []float64{10, 20, 30, 40, 48} {
		if !lit(d) {
			t.Errorf("no corridor ink %.0f m out along the target's port axis", d)
		}
	}
	for _, d := range []float64{60, 70} {
		if lit(d) {
			t.Errorf("corridor ink %.0f m out, past its 50 m end", d)
		}
	}
}

// The corridor stays in the side view when the right pane is dropped.
func TestProximityCorridorSurvivesCompactForm(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 40, Y: 10}, 0)
	tgt := w.Crafts[1]
	frame, _ := orbital.LVLHBasis(tgt.State.R, tgt.State.V)
	tgt.CurrentAttitudeDir = frame.AlongTrack.Add(frame.RadialOut).Unit()
	v := NewOrbitView(chipTestTheme())
	v.Resize(80, 24)
	v.Render(w, 0, 80, 24)
	st, _ := w.ProximityState()
	port := st.TargetWorld.Add(st.Target.Axis.Scale(st.Target.Ports[0].OffsetM))
	px, py, on := v.canvas.Project(port.Add(st.Target.Axis.Unit().Scale(25)))
	if !on {
		t.Skip("25 m out is off the 80x24 pane")
	}
	found := false
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			found = found || v.canvas.SubPixelSet(px+dx, py+dy)
		}
	}
	if !found {
		t.Error("no corridor ink in the 80x24 side view")
	}
}

// The split must not break mouse routing or the map: right-pane clicks are
// not HUD clicks, and the map gets its full canvas back.
func TestCenterlineSplitKeepsMouseAndMapIntact(t *testing.T) {
	w := ringWorld(t, 1, 0.5, 0)
	v, _ := renderSplit(t, w)
	if v.IsHudClick(100) {
		t.Error("a click inside the Centerline pane routes as a HUD click")
	}
	if v.IsCanvasClick(100, 10) {
		t.Error("a click inside the Centerline pane routes as an empty-canvas (stage a burn) click")
	}
	w.ViewMode = sim.ViewOrbitFlat
	v.Render(w, 0, DesignWidth, DesignHeight)
	if v.canvas.Cols() != 138 {
		t.Errorf("map canvas = %d columns after leaving Proximity, want 138", v.canvas.Cols())
	}
}

// The clamp keeps the cross, at centre+d, inside the ring window around the
// crosshair while keeping d's direction from the ring centre, wherever the
// ring sits relative to the lean (found live: ring 0.8 m below the crosshair,
// nose leaning up, the cross was mis-clamped because the clamp assumed the
// ring was on the lean's side).
func TestCenterlineClampKeepsLeanDirectionInsideWindow(t *testing.T) {
	g := centerlineGeom{winHalfCols: 16, winHalfRows: 8} // 32 px x 32 px half extents
	const scale = 10.0                                   // px per metre
	const halfX, halfY = 32.0, 32.0
	cases := []struct {
		name      string
		centre, d orbital.Vec3 // metres
		clamped   bool
	}{
		{"ring below, lean up", orbital.Vec3{Y: -3}, orbital.Vec3{Y: 20}, true},
		{"ring near the bottom edge, lean up (old clamp snapped to the centre)", orbital.Vec3{Y: -3.1}, orbital.Vec3{Y: 20}, true},
		{"ring above, lean down", orbital.Vec3{Y: 3}, orbital.Vec3{Y: -20}, true},
		{"ring left, lean right", orbital.Vec3{X: -3}, orbital.Vec3{X: 20}, true},
		{"ring right, lean left", orbital.Vec3{X: 3}, orbital.Vec3{X: -20}, true},
		{"ring same side as lean (up)", orbital.Vec3{Y: 2}, orbital.Vec3{Y: 20}, true},
		{"ring same side as lean (right)", orbital.Vec3{X: 2}, orbital.Vec3{X: 20}, true},
		{"ring down-left, lean up-right", orbital.Vec3{X: -2, Y: -2}, orbital.Vec3{X: 15, Y: 30}, true},
		{"ring up-right, lean down-left", orbital.Vec3{X: 2, Y: 2}, orbital.Vec3{X: -30, Y: -15}, true},
		{"ring up-left, lean down-right", orbital.Vec3{X: -2, Y: 2}, orbital.Vec3{X: 30, Y: -15}, true},
		{"ring down-right, lean up-left", orbital.Vec3{X: 2, Y: -2}, orbital.Vec3{X: -15, Y: 30}, true},
		{"no clamp, ring below, lean up", orbital.Vec3{Y: -1}, orbital.Vec3{Y: 1}, false},
		{"no clamp, diagonal", orbital.Vec3{X: 1, Y: -1}, orbital.Vec3{X: -1, Y: 0.5}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := centerlineClampToWindow(g, scale, c.centre, c.d)
			if !c.clamped {
				if got != c.d {
					t.Fatalf("unclamped lean changed: got %+v, want %+v", got, c.d)
				}
				return
			}
			// Direction kept: got is a non-zero positive multiple of d.
			f := math.Max(math.Abs(got.X/c.d.X), math.Abs(got.Y/c.d.Y))
			if c.d.X == 0 {
				f = got.Y / c.d.Y
			} else if c.d.Y == 0 {
				f = got.X / c.d.X
			}
			if !(f > 0 && f < 1) {
				t.Fatalf("cross snapped to the ring centre or was not shortened: got %+v for d %+v", got, c.d)
			}
			if math.Abs(got.X-f*c.d.X) > 1e-9 || math.Abs(got.Y-f*c.d.Y) > 1e-9 {
				t.Fatalf("direction not kept: got %+v, d %+v", got, c.d)
			}
			// Inside the window, and touching its edge (the clamp is tight).
			ax, ay := math.Abs((c.centre.X+got.X)*scale), math.Abs((c.centre.Y+got.Y)*scale)
			if ax > halfX+1e-9 || ay > halfY+1e-9 {
				t.Fatalf("cross outside the window: abs px (%.2f, %.2f)", ax, ay)
			}
			if math.Abs(ax-halfX) > 1e-9 && math.Abs(ay-halfY) > 1e-9 {
				t.Fatalf("clamp not tight to the window edge: abs px (%.2f, %.2f)", ax, ay)
			}
		})
	}
}
