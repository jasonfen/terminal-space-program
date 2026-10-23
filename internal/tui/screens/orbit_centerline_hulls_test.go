package screens

import (
	"math"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// ADR 0055 A5 (slice S2): both hulls end-on. Every position is derived here
// by hand from dots = metres x scale, scale = 56 dots / step (the disc is 56
// dots across), the crosshair at dot (28, 28), right = +x, up = -y.

// hullWorld is ringWorld with the vessels and the tilt chosen by the caller.
func hullWorld(t *testing.T, own, tgt string, up, right, tiltDeg float64) *sim.World {
	t.Helper()
	w := portedPairWorld(t, []string{own}, []string{tgt}, orbital.Vec3{X: 3, Y: -up, Z: -right}, tiltDeg)
	st, ok := w.ProximityState()
	if !ok || !st.HasPort {
		t.Fatal("fixture has no port scene")
	}
	if math.Abs(st.OffAxisUpM-up) > 1e-6 || math.Abs(st.OffAxisRightM-right) > 1e-6 {
		t.Fatalf("fixture lateral = (%.4f right, %.4f up), want (%.2f, %.2f)", st.OffAxisRightM, st.OffAxisUpM, right, up)
	}
	return w
}

// discDot is the dot of a point right/up metres from the crosshair at a step.
func discDot(stepM, rightM, upM float64) (x, y float64) {
	s := 56 / stepM
	return 28 + rightM*s, 28 - upM*s
}

// hullDots lists the lit disc dots that are neither scope furniture nor in
// the rim band (distance from the crosshair over 24 dots).
func hullDots(c *widgets.Canvas) [][2]float64 {
	var out [][2]float64
	for py := 0; py < c.Rows()*4; py++ {
		for px := 0; px < c.Cols()*2; px++ {
			if !c.SubPixelSet(px, py) || scopeFurniture(px, py) {
				continue
			}
			out = append(out, [2]float64{float64(px), float64(py)})
		}
	}
	return out
}

// onCircle reports whether a dot lies within tol dots of the circle.
func onCircle(d [2]float64, cx, cy, r, tol float64) bool {
	return math.Abs(math.Hypot(d[0]-cx, d[1]-cy)-r) <= tol
}

// arcLit counts the sample points of a circle (every 15 degrees, only those
// inside the rim band and clear of the reticle arms) that have a lit hull dot
// within 2 dots, and the number of sample points considered.
func arcLit(dots [][2]float64, cx, cy, r float64) (lit, considered int) {
	for deg := 0; deg < 360; deg += 15 {
		px := cx + r*math.Cos(float64(deg)*math.Pi/180)
		py := cy - r*math.Sin(float64(deg)*math.Pi/180)
		if math.Hypot(px-28, py-28) > 23 || math.Abs(px-28) < 3 || math.Abs(py-28) < 3 {
			continue
		}
		considered++
		for _, d := range dots {
			if math.Hypot(d[0]-px, d[1]-py) <= 2 {
				lit++
				break
			}
		}
	}
	return
}

// litNear reports whether any dot within one dot of (px, py) is lit.
func litNear(c *widgets.Canvas, px, py int) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if c.SubPixelSet(px+dx, py+dy) {
				return true
			}
		}
	}
	return false
}

func stepOf(t *testing.T, v *OrbitView, want float64) {
	t.Helper()
	if v.clStepM != want {
		t.Fatalf("fixture ranged to the %v m step, want %v", v.clStepM, want)
	}
}

// Both hulls are 6 m wide here (r = 3 m). At the 16 m step (3.5 dots/m,
// r = 10.5 dots) their port 7 m above yours puts the circles' centres 24.5
// dots apart: 3.5 dots clear. Every lit hull dot sits on one of the two
// circles, and each circle is really drawn.
func TestCenterlineHullsApartAtSevenMetresLateral(t *testing.T) {
	w := hullWorld(t, "rcs-tug", "nudge-tug", 7, 0, 0)
	v, _ := renderSplit(t, w)
	stepOf(t, v, 16)
	ox, oy := discDot(16, 0, 0) // (28, 28)
	tx, ty := discDot(16, 0, 7) // (28, 3.5)
	const r = 10.5
	dots := hullDots(v.cl)
	for _, d := range dots {
		if math.Hypot(d[0]-28, d[1]-28) > 24 {
			continue // rim band
		}
		if !onCircle(d, ox, oy, r, 1.6) && !onCircle(d, tx, ty, r, 1.6) {
			t.Errorf("stray hull dot %v is on neither circle", d)
		}
	}
	if lit, n := arcLit(dots, ox, oy, r); n < 8 || lit < n {
		t.Errorf("your circle: %d of %d sample points lit, want all", lit, n)
	}
	if lit, n := arcLit(dots, tx, ty, r); n < 4 || lit < n {
		t.Errorf("their circle: %d of %d sample points lit, want all", lit, n)
	}
	// Apart: no dot of theirs comes within 3.5 dots - 1.6 tolerance of your
	// centre closer than r.
	for _, d := range dots {
		if onCircle(d, tx, ty, r, 1.6) && math.Hypot(d[0]-ox, d[1]-oy) < r-0.5 {
			t.Errorf("their dot %v is inside your circle: the circles overlap at 7 m lateral", d)
		}
	}
}

// Jason's scene: their port 4 m above yours is also the 16 m step; the
// centres are 14 dots apart, so their circle dips 7 dots into yours.
func TestCenterlineHullsOverlapAtFourMetresLateral(t *testing.T) {
	w := hullWorld(t, "rcs-tug", "nudge-tug", 4, 0, 0)
	v, _ := renderSplit(t, w)
	stepOf(t, v, 16)
	ox, oy := discDot(16, 0, 0)
	tx, ty := discDot(16, 0, 4) // (28, 14)
	const r = 10.5
	dots := hullDots(v.cl)
	inside := 0
	for _, d := range dots {
		if math.Hypot(d[0]-28, d[1]-28) > 24 {
			continue
		}
		a, b := onCircle(d, ox, oy, r, 1.6), onCircle(d, tx, ty, r, 1.6)
		if !a && !b {
			t.Errorf("stray hull dot %v is on neither circle", d)
		}
		if b && math.Hypot(d[0]-ox, d[1]-oy) < r-3 {
			inside++
		}
	}
	if inside < 4 {
		t.Errorf("only %d of their dots are inside your circle, want the circles overlapping", inside)
	}
	if lit, n := arcLit(dots, tx, ty, r); n < 6 || lit < n {
		t.Errorf("their circle: %d of %d sample points lit", lit, n)
	}
}

// Stadium: an SM+CM stack (15 m long here) leaning 20 degrees nose-up has its
// far circle 15 m * sin 20 = 5.130 m below the port. At the 16 m step that is
// 17.96 dots below the crosshair, (28, 45.96); the tangents run down x = 28
// +/- 10.5 between the two centres. Their port 7 m up keeps the step at 16.
func TestCenterlineHullStadiumFarCircleAtNoseTimesSinLean(t *testing.T) {
	w := hullWorld(t, "csm", "nudge-tug", 7, 0, 20)
	st, _ := w.ProximityState()
	if st.Own.NoseM != 15 {
		t.Fatalf("fixture nose = %v m, want 15", st.Own.NoseM)
	}
	if math.Abs(st.MisalignUpDeg-20) > 0.5 || math.Abs(st.MisalignRightDeg) > 0.5 {
		t.Fatalf("fixture lean = (%.2f up, %.2f right), want (20, 0)", st.MisalignUpDeg, st.MisalignRightDeg)
	}
	v, _ := renderSplit(t, w)
	stepOf(t, v, 16)
	const r = 10.5
	far := 28 + 5.130*3.5 // 45.96
	dots := hullDots(v.cl)
	near := func(x, y float64) bool {
		for _, d := range dots {
			if math.Hypot(d[0]-x, d[1]-y) <= 2 {
				return true
			}
		}
		return false
	}
	// The far circle's two flanks (its widest points) and two diagonals.
	for _, p := range [][2]float64{{28 - r, far}, {28 + r, far}} {
		if !near(p[0], p[1]) {
			t.Errorf("no far-circle dot near (%.1f, %.1f)", p[0], p[1])
		}
	}
	// Tangent lines: midway between the centres, at the near circle's x = 28 +/- r.
	for _, x := range []float64{28 - r, 28 + r} {
		if !near(x, (28+far)/2) {
			t.Errorf("no tangent-line dot near (%.1f, %.1f)", x, (28+far)/2)
		}
	}
	// And the far circle is not a circle drawn at the crosshair only.
	if lit, n := arcLit(dots, 28, far, r); n < 3 || lit < n {
		t.Errorf("far circle: %d of %d sample points lit", lit, n)
	}
}

// The same stadium leaning sideways: the SM+CM tipped 20 degrees toward the
// LVLH cross-track axis, which is the face's RIGHT (ringWorld offsets Z = -right
// for the one who is offset), so the nose is right of parallel,
// MisalignRightDeg = +20, and the stack bottom projects to the LEFT: far circle
// at 5.130 m left, dot (28 - 17.96, 28) = (10.04, 28). Their port 7 m up keeps
// the 16 m step.
func TestCenterlineHullStadiumLeansSideways(t *testing.T) {
	w := hullWorld(t, "csm", "nudge-tug", 7, 0, 0)
	a := w.Crafts[0]
	frame, _ := orbital.LVLHBasis(w.Crafts[1].State.R, w.Crafts[1].State.V)
	old := a.CurrentAttitudeDir
	a.CurrentAttitudeDir = frame.AlongTrack.Scale(-math.Cos(20 * math.Pi / 180)).Add(frame.CrossTrack.Scale(math.Sin(20 * math.Pi / 180)))
	a.State.R = a.State.R.Add(old.Sub(a.CurrentAttitudeDir).Scale(sim.VesselPortGeometry(a).Ports[0].OffsetM)) // port stays put
	st, _ := w.ProximityState()
	if math.Abs(st.MisalignRightDeg-20) > 0.5 || math.Abs(st.MisalignUpDeg) > 0.5 || math.Abs(st.OffAxisUpM-7) > 1e-6 || math.Abs(st.OffAxisRightM) > 1e-6 {
		t.Fatalf("fixture lean (%.2f right, %.2f up) lateral (%.2f right, %.2f up), want (20, 0) and (0, 7)", st.MisalignRightDeg, st.MisalignUpDeg, st.OffAxisRightM, st.OffAxisUpM)
	}
	v, _ := renderSplit(t, w)
	stepOf(t, v, 16)
	farX := 28 - 5.130*3.5 // 10.04; far circle r 10.5 around (10.04, 28)
	dots := hullDots(v.cl)
	if lit, n := arcLit(dots, farX, 28, 10.5); n < 3 || lit < n {
		t.Errorf("far circle: %d of %d sample points lit", lit, n)
	}
	for _, d := range dots { // nothing to the right, where a sign slip would put it
		if d[0] > 28+10.5+2 && math.Hypot(d[0]-28, d[1]-28) <= 24 && d[1] > 12 {
			t.Errorf("hull dot %v right of your circle", d)
		}
	}
}

// With no lean there is no stadium: the same SM+CM at 0 degrees is one circle.
func TestCenterlineHullNoStadiumWhenAligned(t *testing.T) {
	w := hullWorld(t, "csm", "nudge-tug", 7, 0, 0)
	v, _ := renderSplit(t, w)
	stepOf(t, v, 16)
	for _, d := range hullDots(v.cl) {
		if d[1] > 28+10.5+2 && math.Hypot(d[0]-28, d[1]-28) <= 24 {
			t.Errorf("hull dot %v below your circle with the axes parallel", d)
		}
	}
}

// A ghost's circle is its reported width_m: 6 draws the very circle a local
// 6 m vessel does, and 4 draws a 2 m radius (7 dots at 3.5 dots/m).
func TestCenterlineGhostHullReadsWidthM(t *testing.T) {
	off := orbital.Vec3{X: 3, Y: -7}
	local := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, off, 0)
	ghost := ghostPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, off, 0)
	if g := ghost.Ghosts[0]; g.WidthM != 6 {
		t.Fatalf("ghost fixture width_m = %v, want 6", g.WidthM)
	}
	vl, _ := renderSplit(t, local)
	vg, _ := renderSplit(t, ghost)
	stepOf(t, vg, 16)
	// The ghost is placed from its report, so a dot may land one over.
	for _, pair := range [][2]*widgets.Canvas{{vl.cl, vg.cl}, {vg.cl, vl.cl}} {
		for py := 0; py < 56; py++ {
			for px := 0; px < 56; px++ {
				if pair[0].SubPixelSet(px, py) && !litNear(pair[1], px, py) {
					t.Fatalf("dot (%d, %d) has no counterpart between a local 6 m target and a ghost with width_m 6", px, py)
				}
			}
		}
	}
	ghost.Ghosts[0].WidthM = 4
	v4, _ := renderSplit(t, ghost)
	tx, ty := discDot(16, 0, 7)
	n := 0
	for _, d := range hullDots(v4.cl) {
		if math.Hypot(d[0]-28, d[1]-28) > 24 {
			continue
		}
		if onCircle(d, tx, ty, 7, 1.6) {
			n++
		} else if !onCircle(d, 28, 28, 10.5, 1.6) {
			t.Errorf("stray dot %v with width_m 4", d)
		}
	}
	if n < 8 {
		t.Errorf("only %d dots on the 7-dot circle for width_m 4", n)
	}
}

// A friend's report with no shape has no ring, so no scope and no hulls.
func TestCenterlineGhostWithNoShapeDrawsNoScope(t *testing.T) {
	ghost := ghostPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 3, Y: -7}, 0)
	ghost.Ghosts[0].NoseM = 0
	ghost.Ghosts[0].Ports = nil
	v, out := renderSplit(t, ghost)
	if st, _ := ghost.ProximityState(); st.HasPort {
		t.Fatal("fixture still has a port scene")
	}
	if strings.Contains(out, "CENTERLINE") {
		t.Error("a shapeless ghost still draws the Centerline scope")
	}
	if v.cl != nil {
		if n := len(hullDots(v.cl)); n != 0 {
			t.Errorf("%d hull dots drawn for a shapeless ghost", n)
		}
	}
}

// Scenery class: dotted, a dot every 2 dots along the circle, not solid ink.
func TestCenterlineHullIsDottedNotSolid(t *testing.T) {
	w := hullWorld(t, "rcs-tug", "nudge-tug", 4, 0, 0)
	v, _ := renderSplit(t, w)
	stepOf(t, v, 16)
	// Your circle, r = 10.5 dots, circumference 66; clear of their circle
	// (the lower half is 7 dots away) in its lower half-plane.
	n := 0
	for _, d := range hullDots(v.cl) {
		if d[1] > 31 && onCircle(d, 28, 28, 10.5, 0.6) {
			n++
		}
	}
	// Measured: the dotted (Scenery) lower half has 13 such dots, solid
	// (Real) ink 23; the arms are excluded and the circle is 66 dots round.
	if n < 8 || n > 17 {
		t.Errorf("%d dots on the lower half of your circle, want a dotted ~13 (solid ink gives ~23)", n)
	}
}

// Clipped at the rim: the circle of a target 5 m right and 5 m up (the 16 m
// step, centre dot (45.5, 10.5), r 10.5) reaches the disc's square corner,
// 34 dots from the crosshair, which is outside the 27-dot rim. Nothing but
// the rim itself is lit beyond 28 dots.
func TestCenterlineHullsClipAtTheRim(t *testing.T) {
	w := hullWorld(t, "rcs-tug", "nudge-tug", 5, 5, 0)
	v, _ := renderSplit(t, w)
	stepOf(t, v, 16)
	inside := 0
	for py := 0; py < 56; py++ {
		for px := 0; px < 56; px++ {
			if !v.cl.SubPixelSet(px, py) {
				continue
			}
			d := math.Hypot(float64(px)-28, float64(py)-28)
			if d > 28 {
				t.Errorf("dot (%d, %d) lit %.1f dots from the crosshair, past the rim", px, py, d)
			}
			if d < 24 && onCircle([2]float64{float64(px), float64(py)}, 45.5, 10.5, 10.5, 1.6) {
				inside++
			}
		}
	}
	if inside < 8 {
		t.Errorf("only %d dots of their circle inside the rim: it is not drawn at all", inside)
	}
}

// Colours: theirs in the TARGET colour, yours in your vessel's. The profile
// is forced, or the escape codes are not there to read.
func TestCenterlineHullColours(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	w := hullWorld(t, "rcs-tug", "nudge-tug", 7, 0, 0)
	w.ActiveCraft().Color = "#AA00FF"
	v, _ := renderSplit(t, w)
	stepOf(t, v, 16)
	rows := strings.Split(v.cl.String(), "\n")
	// Cells holding hull dots of exactly one circle and nothing of the scope's
	// furniture: their colour is that hull's. #3DDC84 renders as 60;220;131.
	own, theirs := map[[2]int]bool{}, map[[2]int]bool{}
	furniture := map[[2]int]bool{}
	for py := 0; py < 56; py++ {
		for px := 0; px < 56; px++ {
			if !v.cl.SubPixelSet(px, py) {
				continue
			}
			cell := [2]int{px / 2, py / 4}
			d := [2]float64{float64(px), float64(py)}
			switch {
			case scopeFurniture(px, py):
				furniture[cell] = true
			case onCircle(d, 28, 28, 10.5, 1.0):
				own[cell] = true
			case onCircle(d, 28, 3.5, 10.5, 1.0):
				theirs[cell] = true
			}
		}
	}
	check := func(name string, cells map[[2]int]bool, other map[[2]int]bool, want string) {
		n := 0
		for cell := range cells {
			if furniture[cell] || other[cell] {
				continue
			}
			n++
			if got := cellFG(rows[cell[1]], cell[0]); got != want {
				t.Errorf("%s hull cell %v colour = %q, want %q", name, cell, got, want)
			}
		}
		if n < 5 {
			t.Errorf("only %d clean %s hull cells to read a colour from", n, name)
		}
	}
	check("your", own, theirs, "170;0;255")
	check("their", theirs, own, "60;220;131")
}

// cellFG is the foreground "r;g;b" in force at visible cell col of an ANSI row.
func cellFG(row string, col int) string {
	re := regexp.MustCompile(`\x1b\[([0-9;]*)m`)
	fg, vis, i := "", 0, 0
	for i < len(row) {
		if loc := re.FindStringSubmatchIndex(row[i:]); loc != nil && loc[0] == 0 {
			p := row[i+loc[2] : i+loc[3]]
			if p == "" || p == "0" {
				fg = ""
			} else if strings.HasPrefix(p, "38;2;") {
				fg = strings.TrimPrefix(p, "38;2;")
			}
			i += loc[1]
			continue
		}
		_, n := utf8.DecodeRuneInString(row[i:])
		if vis == col {
			return fg
		}
		vis++
		i += n
	}
	return ""
}

// The standoff cross is a cell overlay drawn after the hulls and must win a
// cell a hull dot shares. A 0.5 m-wide ghost has a hull circle of 0.25 m
// radius; a 2 degree lean puts the cross 0.5 m * 2/4 = 0.25 m from the ring's
// centre, exactly on that circle (the ring itself is 0.5 m, seven dots
// further out at the 4 m step, 14 dots/m). The cell there must hold hull
// ink and still read '+'.
func TestCenterlineHullsLeaveTheCrossVisible(t *testing.T) {
	w := ghostPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 3, Y: -1}, 2)
	w.Ghosts[0].WidthM = 0.5
	v, out := renderSplit(t, w)
	stepOf(t, v, 4)
	st, _ := w.ProximityState()
	if math.Abs(math.Abs(st.MisalignUpDeg)-2) > 0.2 || math.Abs(st.OffAxisUpM-1) > 1e-6 {
		t.Fatalf("fixture lean %.2f deg, lateral %.2f m up, want 2 and 1", st.MisalignUpDeg, st.OffAxisUpM)
	}
	crossUpM := 1.0 + math.Copysign(0.25, st.MisalignUpDeg)
	x, y := discDot(4, 0, crossUpM)
	col, row := clDiscCol0+int(x)/2, clDiscRow0+int(y)/4
	if got := frameRune(out, col, row); got != '+' {
		t.Errorf("cell (%d, %d) = %q, want the standoff cross '+' over the hull", col, row, got)
	}
	// A hull dot really shares that cell, or the check proves nothing.
	shared := false
	for py := (int(y) / 4) * 4; py < (int(y)/4)*4+4; py++ {
		for px := (int(x) / 2) * 2; px < (int(x)/2)*2+2; px++ {
			if v.cl.SubPixelSet(px, py) && !scopeFurniture(px, py) {
				shared = true
			}
		}
	}
	if !shared {
		t.Error("no hull ink in the cross's cell: the fixture does not exercise the overlap")
	}
}
