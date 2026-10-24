package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// ADR 0055 slice 2: the Centerline view. Every positional pin here measures
// the cell (or the lit pixel) it names, in the rendered frame or the right
// pane's canvas, from arithmetic done in the test, never by reading back the
// number the code computed.

// Design Size scope, as the ADR's arithmetic gives it (A1): a 34 x 17 framed
// panel at canvas col 104, row 0; a 28 x 14 disc (56 x 56 dots) whose first
// cell is canvas (107, 2); the crosshair in disc cell (14, 7) = canvas
// (121, 9), at dot (28, 28).
const (
	clPanelCol  = 104
	clPanelW    = 34
	clPanelH    = 17
	clDiscCol0  = 107
	clDiscRow0  = 2
	clDiscCols  = 28
	clDiscRows  = 14
	clCrossCol  = 14 // disc cell
	clCrossRow  = 7
	clCrossPxX  = 28 // disc dot
	clCrossPxY  = 28
	clRimPx     = 27.0
	clRingR     = 0.5
	clGateDeg   = 4.0
	clFrameLine = 2 // canvas row r is frame line r+2 (title, top border)
)

// dotsPerM is the scope's scale at a width step: the disc's 56 dots across S.
func dotsPerM(stepM float64) float64 { return 56 / stepM }

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

// frameLine is canvas row r of a rendered frame, colour stripped.
func frameLine(frame string, row int) string {
	lines := strings.Split(ansi.Strip(frame), "\n")
	if row+clFrameLine >= len(lines) {
		return ""
	}
	r := []rune(lines[row+clFrameLine])
	return string(r[1 : len(r)-1])
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

// scopeFurniture reports whether a disc dot belongs to the scope's own
// marks: the two reticle lines, the rim and the four edge ticks. Anything
// else lit is the ring (or, from S2, a hull).
func scopeFurniture(px, py int) bool {
	if px == clCrossPxX || py == clCrossPxY {
		return true
	}
	if d := math.Hypot(float64(px-clCrossPxX), float64(py-clCrossPxY)); d > 24 {
		return true
	}
	return false
}

// litExcludingCrosshair lists the disc's lit dots that are not scope furniture.
func litExcludingCrosshair(c *widgets.Canvas) (minX, minY, maxX, maxY, n int) {
	minX, minY = 1<<30, 1<<30
	maxX, maxY = -1, -1
	for py := 0; py < c.Rows()*4; py++ {
		for px := 0; px < c.Cols()*2; px++ {
			if !c.SubPixelSet(px, py) || scopeFurniture(px, py) {
				continue
			}
			n++
			minX, maxX = min(minX, px), max(maxX, px)
			minY, maxY = min(minY, py), max(maxY, py)
		}
	}
	return
}

// inkAt reports whether the frame cell holds braille ink or the cross glyph.
func inkAt(frame string, col, row int) bool {
	r := frameRune(frame, col, row)
	return (r > 0x2800 && r <= 0x28FF) || r == '+'
}

// A rendered title row is "CENTERLINE  <step>", then the port range flush
// right, in the panel's 32 inner columns.
func wantTitle(step string, st sim.ProximityState) string {
	l, r := "CENTERLINE  "+step, "port "+readout.PortDistance(st.PortRangeM)
	return l + strings.Repeat(" ", 32-len(l)-len(r)) + r
}

// A1: the panel is 34 x 17, framed, at canvas cols 104-137 rows 0-16, title
// row inside; the canvas keeps the full 138 columns; the navball below it is
// untouched at rows 17-35.
func TestCenterlinePanelIsFramedTopRightAndCanvasKeepsItsWidth(t *testing.T) {
	w := ringWorld(t, 1, 0.6, 0)
	st, _ := w.ProximityState()
	v, out := renderSplit(t, w)
	if v.canvas.Cols() != 138 {
		t.Errorf("canvas = %d columns in Proximity, want the full 138", v.canvas.Cols())
	}
	for _, c := range []struct {
		col, row int
		want     rune
	}{
		{clPanelCol, 0, '╭'}, {clPanelCol + clPanelW - 1, 0, '╮'},
		{clPanelCol, clPanelH - 1, '╰'}, {clPanelCol + clPanelW - 1, clPanelH - 1, '╯'},
	} {
		if got := frameRune(out, c.col, c.row); got != c.want {
			t.Errorf("panel corner (%d, %d) = %q, want %q", c.col, c.row, got, c.want)
		}
	}
	line := []rune(frameLine(out, 1))
	if len(line) < clPanelCol+clPanelW {
		t.Fatalf("title row too short: %q", string(line))
	}
	if got, want := string(line[clPanelCol+1:clPanelCol+clPanelW-1]), wantTitle("4 m", st); got != want {
		t.Errorf("title row = %q, want %q", got, want)
	}

	// The navball is where it was: its frame opens on the row under the panel.
	g := navballGeometry(138, 37)
	atCol, atRow := 138-g.panelW, 37-g.panelH-1
	if atRow != clPanelH {
		t.Fatalf("fixture: navball opens on row %d, the panel was drawn for row %d", atRow, clPanelH)
	}
	if got := frameRune(out, atCol, atRow); got != '╭' {
		t.Errorf("navball top-left = %q at (%d, %d), want ╭", got, atCol, atRow)
	}
	if got := frameRune(out, atCol, atRow+g.panelH-1); got != '╰' {
		t.Errorf("navball bottom-left = %q, want ╰", got)
	}
}

// A3: the crosshair is the disc's centre cell, canvas (121, 9); its arms run
// to the rim; the rim carries ticks at up / right / down / left. All of it
// whatever the ring is doing.
func TestCenterlineCrosshairIsFixedAtItsCell(t *testing.T) {
	for _, off := range [][2]float64{{0, 0}, {1.2, -0.8}, {-3, 2}} {
		w := ringWorld(t, off[0], off[1], 0)
		v, out := renderSplit(t, w)
		lit := false
		for dx := 0; dx < 2; dx++ { // the centre cell's 2 x 4 dots
			for dy := 0; dy < 4; dy++ {
				lit = lit || v.cl.SubPixelSet(clCrossPxX+dx, clCrossPxY+dy)
			}
		}
		if !lit {
			t.Errorf("lateral %v: the crosshair's centre cell has no lit dot", off)
		}
		if !inkAt(out, clDiscCol0+clCrossCol, clDiscRow0+clCrossRow) {
			t.Errorf("lateral %v: frame cell (121, 9) = %q, want crosshair ink", off, frameRune(out, clDiscCol0+clCrossCol, clDiscRow0+clCrossRow))
		}
	}
}

// The reticle's arms cross on the centre dot and run the width of the disc:
// ink within a few dots of each rim point, and the rim's four ticks beside
// them (the tick is a bar across the arm, two dots inside the rim).
func TestCenterlineReticleReachesTheRimAndTheRimHasTicks(t *testing.T) {
	w := ringWorld(t, 0, 0, 0)
	v, out := renderSplit(t, w)
	any := func(pts ...[2]int) bool {
		for _, p := range pts {
			if v.cl.SubPixelSet(p[0], p[1]) {
				return true
			}
		}
		return false
	}
	if !any([2]int{28, 0}, [2]int{28, 1}, [2]int{28, 2}) {
		t.Error("up arm does not reach the rim")
	}
	if !any([2]int{28, 53}, [2]int{28, 54}, [2]int{28, 55}) {
		t.Error("down arm does not reach the rim")
	}
	if !any([2]int{0, 28}, [2]int{1, 28}, [2]int{2, 28}) {
		t.Error("left arm does not reach the rim")
	}
	if !any([2]int{53, 28}, [2]int{54, 28}, [2]int{55, 28}) {
		t.Error("right arm does not reach the rim")
	}
	// Ticks: off-arm dots 2 px either side of the arm, 2 px inside the rim.
	for name, p := range map[string][2]int{
		"up": {26, 3}, "up+": {30, 3}, "down": {26, 53}, "down+": {30, 53},
		"left": {3, 26}, "left+": {3, 30}, "right": {53, 26}, "right+": {53, 30},
	} {
		if !v.cl.SubPixelSet(p[0], p[1]) {
			t.Errorf("rim tick %s: dot %v not lit", name, p)
		}
	}
	// The four compass cells (disc cell -> canvas) carry ink.
	for name, c := range map[string][2]int{
		"up": {clCrossCol, 0}, "down": {clCrossCol, clDiscRows - 1},
		"left": {0, clCrossRow}, "right": {clDiscCols - 1, clCrossRow},
	} {
		if !inkAt(out, clDiscCol0+c[0], clDiscRow0+c[1]) {
			t.Errorf("compass cell %s (%d, %d) has no ink", name, clDiscCol0+c[0], clDiscRow0+c[1])
		}
	}
	// And the rim is a round line, not a box: the disc's corner cell is empty.
	if inkAt(out, clDiscCol0, clDiscRow0) {
		t.Error("disc corner cell has ink; the rim should be a circle")
	}
}

// The ring is centred at (right, up) metres from the crosshair at 56 / S dots
// per metre: 1 m up and 0.6 m left is e = 1.67 m, the 4 m step, 14 dots / m.
func TestCenterlineRingSitsAtTheLateralOffset(t *testing.T) {
	const up, right = 1.0, -0.6
	w := ringWorld(t, up, right, 0)
	v, _ := renderSplit(t, w)
	scale := dotsPerM(4)
	wantX := float64(clCrossPxX) + right*scale
	wantY := float64(clCrossPxY) - up*scale
	minX, minY, maxX, maxY, n := litExcludingCrosshair(v.cl)
	if n == 0 {
		t.Fatal("no ring ink in the disc")
	}
	midX, midY := float64(minX+maxX)/2, float64(minY+maxY)/2
	if math.Abs(midX-wantX) > 3 || math.Abs(midY-wantY) > 3 {
		t.Errorf("ring centre at dot (%.1f, %.1f), want (%.1f, %.1f) = crosshair + (%.2f m right, %.2f m up) at %.2f dots/m",
			midX, midY, wantX, wantY, right, up, scale)
	}
	if got, want := float64(maxX-minX)/2, clRingR*scale; math.Abs(got-want) > 3 {
		t.Errorf("ring half-width = %.1f dots, want %.1f (0.5 m at %.2f dots/m)", got, want, scale)
	}
}

// A w pulse in PORT (BurnPortUp) moves the ring DOWN the disc by the metres
// the pulse bought at the scope's scale, and the lateral: row's up number
// falls by the same: the ring, the row and the pulse read one PortFaceBasis.
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
	v.Render(w, 0, DesignWidth, DesignHeight)
	st1, _ := w.ProximityState()
	if got := st0.OffAxisUpM - st1.OffAxisUpM; math.Abs(got-moved) > 0.05*moved {
		t.Fatalf("lateral up fell by %.3f m, the pulse bought %.3f m", got, moved)
	}
	_, y1min, _, y1max, _ := litExcludingCrosshair(v.cl)
	scale := dotsPerM(4)
	dy := float64(y1min+y1max)/2 - float64(y0min+y0max)/2
	if dy <= 0 {
		t.Fatalf("ring moved %.1f dots (positive is down) after pushing up; want down", dy)
	}
	if want := moved * scale; math.Abs(dy-want) > 4 {
		t.Errorf("ring moved %.1f dots down, want %.1f (%.3f m at %.2f dots/m)", dy, want, moved, scale)
	}
}

// crossCell finds the standoff cross's '+' in the disc, as a disc cell.
func crossCell(frame string) (col, row int, ok bool) {
	for r := clDiscRow0; r < clDiscRow0+clDiscRows; r++ {
		for c := clDiscCol0; c < clDiscCol0+clDiscCols; c++ {
			if frameRune(frame, c, r) == '+' {
				return c - clDiscCol0, r - clDiscRow0, true
			}
		}
	}
	return 0, 0, false
}

// cellAt is the disc cell holding the dot dx dots right and dy dots up of the
// crosshair, rounded the way the canvas projects a world point.
func cellAt(dx, dy float64) (int, int) {
	return (clCrossPxX + int(math.Round(dx))) / 2, (clCrossPxY - int(math.Round(dy))) / 4
}

// The standoff cross sits on the ring's edge at exactly 4 degrees of lean,
// inside at 3, and below the centre for a nose tipped down. Lateral 0 is the
// 2 m step: 28 dots / m, a 14-dot ring radius.
func TestCenterlineCrossReachesTheEdgeAtTheGate(t *testing.T) {
	ringR := clRingR * dotsPerM(2)
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
			t.Fatalf("tilt %+g: no standoff cross in the disc", tc.tilt)
		}
		wc, wr := cellAt(0, tc.wantPx)
		if col != wc || row != wr {
			t.Errorf("tilt %+g deg: cross at disc cell (%d, %d), want (%d, %d) = %.1f dots above the ring centre",
				tc.tilt, col, row, wc, wr, tc.wantPx)
		}
	}
}

// A nose tipped toward the face's right puts the cross right of the ring's
// centre by the same rule, and a centred nose puts it on the crosshair.
func TestCenterlineCrossFollowsYawAndCentresWhenStraight(t *testing.T) {
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
	e := math.Hypot(st.OffAxisRightM, st.OffAxisUpM) + clRingR
	step := 2.0
	for e > 0.95*step/2 {
		step *= 2
	}
	scale := dotsPerM(step)
	ringR := clRingR * scale
	wc, wr := cellAt(st.OffAxisRightM*scale+ringR, st.OffAxisUpM*scale)
	if !ok || col != wc || row != wr {
		t.Errorf("4 deg right: cross at (%d, %d) ok=%v, want (%d, %d)", col, row, ok, wc, wr)
	}
}

// Under 3 cells across the ring is the port glyph, under 5 there is no cross.
// 6 m up is the 16 m step: the ring is 1.75 cells.
func TestCenterlineSmallRingIsTheGlyphAndHasNoCross(t *testing.T) {
	w := ringWorld(t, 6, 0, 4)
	_, out := renderSplit(t, w)
	if _, _, ok := crossCell(out); ok {
		t.Error("a sub-5-cell ring carries a standoff cross")
	}
	col, row := cellAt(0, 6*dotsPerM(16))
	if got := frameRune(out, clDiscCol0+col, clDiscRow0+row); got != proximityPortMarkGlyph {
		t.Errorf("cell (%d, %d) = %q, want the %q port glyph at the ring's centre", clDiscCol0+col, clDiscRow0+row, got, proximityPortMarkGlyph)
	}
}

// Beyond the 64 m step the port glyph sits on the rim in the port's direction.
func TestCenterlineBeyondTheLastStepTheGlyphSitsOnTheRim(t *testing.T) {
	for _, c := range []struct {
		name      string
		up, right float64
		col, row  int // disc cell
	}{
		{"above", 50, 0, clCrossCol, 0},
		{"below", -50, 0, clCrossCol, clDiscRows - 1},
		{"right", 0, 50, clDiscCols - 1, clCrossRow},
		{"left", 0, -50, 0, clCrossRow},
	} {
		w := ringWorld(t, c.up, c.right, 0)
		_, out := renderSplit(t, w)
		if got := frameRune(out, clDiscCol0+c.col, clDiscRow0+c.row); got != proximityPortMarkGlyph {
			t.Errorf("%s: rim cell (%d, %d) = %q, want %q", c.name, clDiscCol0+c.col, clDiscRow0+c.row, got, proximityPortMarkGlyph)
		}
		if line := frameLine(out, 1); !strings.Contains(line, "CENTERLINE  64 m") {
			t.Errorf("%s: title %q does not read the 64 m step", c.name, line)
		}
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

// A4: the scope has no zoom. `+` rescales the side picture and nothing in the
// disc: the ring stays on the same dots. The scale bar is still drawn once,
// and the bottom label now reads straight across (no seam).
func TestCenterlineZoomMovesTheSidePictureOnlyAndTheBarIsDrawnOnce(t *testing.T) {
	w := ringWorld(t, 1, 0.5, 0)
	v, _ := renderSplit(t, w)
	l0 := v.canvas.Scale()
	r0 := v.cl.Scale()
	x0, y0, x1, y1, n0 := litExcludingCrosshair(v.cl)
	v.ZoomIn()
	out := v.Render(w, 0, DesignWidth, DesignHeight)
	if got := v.canvas.Scale() / l0; math.Abs(got-1.25) > 1e-9 {
		t.Errorf("after + the side picture scaled %.4fx, want 1.25x", got)
	}
	if got := v.cl.Scale() / r0; math.Abs(got-1) > 1e-9 {
		t.Errorf("after + the scope scaled %.4fx, want 1x (no zoom)", got)
	}
	a, b, c, d, n1 := litExcludingCrosshair(v.cl)
	if a != x0 || b != y0 || c != x1 || d != y1 || n1 != n0 {
		t.Errorf("after + the ring moved: bbox (%d,%d)-(%d,%d) n=%d, was (%d,%d)-(%d,%d) n=%d", a, b, c, d, n1, x0, y0, x1, y1, n0)
	}
	bar, ok := scaleBarFor(v.canvas.Scale())
	if !ok {
		t.Fatal("no scale bar for the side picture")
	}
	if n := strings.Count(ansi.Strip(out), bar.text()); n != 1 {
		t.Errorf("scale bar %q drawn %d times, want once", bar.text(), n)
	}
	if !strings.Contains(frameLine(out, 36), "view: Proximity") {
		t.Errorf("bottom label = %q, want it to start with view: Proximity", frameLine(out, 36))
	}
}

// Compact Form: the scope is the first thing dropped. At 80x24 and one row
// short of the Design Size there is no panel; the side picture keeps the
// whole width and the chip's port: / misalign: rows survive.
func TestCenterlinePanelOnlyAtDesignSize(t *testing.T) {
	w := ringWorld(t, 1, 0.6, 0)
	small := NewOrbitView(chipTestTheme())
	small.Resize(80, 24)
	text := ansi.Strip(small.Render(w, 0, 80, 24))
	if strings.Contains(text, "CENTERLINE") {
		t.Error("80x24 draws the Centerline panel; Compact Form drops it first")
	}
	if small.canvas.Cols() != 78 {
		t.Errorf("80x24 canvas = %d columns, want the full 78", small.canvas.Cols())
	}
	for _, want := range []string{"port:", "misalign:"} {
		if !strings.Contains(text, want) {
			t.Errorf("80x24 lost the %q row", want)
		}
	}
	short := NewOrbitView(chipTestTheme())
	short.Resize(DesignWidth, DesignHeight-1)
	if strings.Contains(ansi.Strip(short.Render(w, 0, DesignWidth, DesignHeight-1)), "CENTERLINE") {
		t.Error("140x39 draws the Centerline panel")
	}
	_, out := renderSplit(t, w)
	if !strings.Contains(ansi.Strip(out), "CENTERLINE") {
		t.Error("140x40 draws no Centerline panel")
	}
}

// Taller terminals grow the panel with the navball (heightScaledCells), and
// it never reaches the navball's top row.
func TestCenterlinePanelGrowsWithHeightAndStopsAboveTheNavball(t *testing.T) {
	w := ringWorld(t, 1, 0.6, 0)
	rows := DesignHeight + 12
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, rows)
	out := v.Render(w, 0, DesignWidth, rows)
	cRows := rows - 3
	disc := heightScaledCells(14, cRows)
	g := navballGeometry(138, cRows)
	navTop := cRows - g.panelH - 1
	if want := disc + 3; want > navTop {
		t.Fatalf("fixture: panel %d rows does not fit above the navball at row %d", want, navTop)
	}
	if disc <= 14 {
		t.Fatalf("disc did not grow at %d rows", rows)
	}
	panelW := 2*disc + 6
	left := 138 - panelW
	if got := frameRune(out, left, 0); got != '╭' {
		t.Errorf("grown panel top-left = %q at (%d, 0), want ╭", got, left)
	}
	if got := frameRune(out, left, disc+2); got != '╰' {
		t.Errorf("grown panel bottom-left = %q at (%d, %d), want ╰", got, left, disc+2)
	}
}

// Without a port to look at there is nothing to centre on: the side view
// keeps its corner.
func TestCenterlineAbsentWithoutAPort(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"s-ivb-1"}, orbital.Vec3{X: 3}, 0)
	v, out := renderSplit(t, w)
	if strings.Contains(ansi.Strip(out), "CENTERLINE") || v.canvas.Cols() != 138 {
		t.Errorf("a portless target drew the panel (canvas %d columns)", v.canvas.Cols())
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

// The panel must not break mouse routing or the map: a click on it is not a
// HUD click, and the canvas is never resized by entering or leaving Proximity.
func TestCenterlinePanelKeepsMouseAndMapIntact(t *testing.T) {
	w := ringWorld(t, 1, 0.5, 0)
	v, _ := renderSplit(t, w)
	if v.IsHudClick(120) {
		t.Error("a click inside the Centerline panel routes as a HUD click")
	}
	if v.canvas.Cols() != 138 {
		t.Errorf("canvas = %d columns in Proximity, want 138", v.canvas.Cols())
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

// F2 declutter hides the scope with the navball and the chips, and it comes
// back when declutter clears (v0.60.0 batch review F1).
func TestCenterlineHidesUnderDeclutter(t *testing.T) {
	w := ringWorld(t, 1, 0.6, 0)
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	v.SetDeclutter(true)
	if out := ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight)); strings.Contains(out, "CENTERLINE") {
		t.Error("scope drawn while F2 declutter is on")
	}
	v.SetDeclutter(false)
	if out := ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight)); !strings.Contains(out, "CENTERLINE") {
		t.Error("scope missing after declutter cleared")
	}
}
