package screens

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// Guards for #602 (grill GS Q7): a rounded scale bar on the Proximity view,
// the orbit map in every projection, and the launch view. They read the
// frame the player gets (View / Render at the Design Size and 181x49), find
// the bar the way a person would (the ├──┤ glyphs and the label after it),
// and check what it claims against the canvas's own projection.

var scaleBarFrameRE = regexp.MustCompile(`├(─+)┤ (\d+(?:\.\d+)?) (m|km|Mm|Gm|AU)`)

// foundBar is a bar located in a rendered frame.
type foundBar struct {
	row, col int // cell of the left tick, counted in runes
	cells    int // tick-to-tick distance
	label    string
	metres   float64
}

func stripANSIForBar(s string) string { return ansiEscapeRE.ReplaceAllString(s, "") }

// findScaleBar locates the first bar in frame.
func findScaleBar(frame string) (foundBar, bool) {
	for r, line := range strings.Split(stripANSIForBar(frame), "\n") {
		loc := scaleBarFrameRE.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}
		m := scaleBarFrameRE.FindStringSubmatch(line)
		num, _ := strconv.ParseFloat(m[2], 64)
		unit := map[string]float64{"m": 1, "km": 1e3, "Mm": 1e6, "Gm": 1e9, "AU": bodies.AU}[m[3]]
		return foundBar{
			row:    r,
			col:    utf8.RuneCountInString(line[:loc[0]]),
			cells:  utf8.RuneCountInString(m[1]) + 1,
			label:  m[2] + " " + m[3],
			metres: num * unit,
		}, true
	}
	return foundBar{}, false
}

// trueCells measures how many canvas cells `metres` really spans along the
// screen's horizontal axis, by projecting two WORLD points that far apart
// along the basis' X axis through the canvas (not through scaleBarFor).
func trueCells(c *widgets.Canvas, metres float64) float64 {
	p0 := c.CenterWorld()
	p1 := p0.Add(c.Basis().X.Scale(metres))
	x0, _, _ := c.Project(p0)
	x1, _, _ := c.Project(p1)
	return float64(x1-x0) / 2
}

// barAgreesWithCanvas is the shared "length times label equals the true
// screen distance within one cell" assertion.
func barAgreesWithCanvas(c *widgets.Canvas, b foundBar) (float64, bool) {
	got := trueCells(c, b.metres)
	return got, math.Abs(got-float64(b.cells)) <= 1
}

func TestScaleBarLabelsAreRoundOnTheLadder(t *testing.T) {
	for _, tc := range []struct {
		m    float64
		want string
	}{
		{20, "20 m"}, {500, "500 m"}, {1000, "1 km"}, {2000, "2 km"}, {5000, "5 km"},
		{20_000, "20 km"}, {5e6, "5000 km"}, {200e6, "200 Mm"}, {5e9, "5 Gm"}, {10e9, "10 Gm"},
		{bodies.AU, "1 AU"}, {5 * bodies.AU, "5 AU"}, {0.2 * bodies.AU, "0.2 AU"},
	} {
		if got := scaleBarLabel(tc.m); got != tc.want {
			t.Errorf("scaleBarLabel(%g) = %q, want %q", tc.m, got, tc.want)
		}
	}
}

func TestScaleBarForIsRoundAndAtLeastSixCells(t *testing.T) {
	for _, pxPerM := range []float64{3e-9, 1e-6, 0.0003, 0.07, 1, 2.6, 40} {
		b, ok := scaleBarFor(pxPerM)
		if !ok {
			t.Fatalf("scale %g: no bar", pxPerM)
		}
		if b.cells < scaleBarMinCells {
			t.Errorf("scale %g px/m: %d cells, want >= %d", pxPerM, b.cells, scaleBarMinCells)
		}
		// 1/2/5 x 10^n (or x AU past 0.1 AU).
		unit := 1.0
		if b.metres >= bodies.AU/10 {
			unit = bodies.AU
		}
		mant := b.metres / unit / math.Pow(10, math.Floor(math.Log10(b.metres/unit)+1e-9))
		if r := math.Round(mant); math.Abs(mant-r) > 1e-6 || (r != 1 && r != 2 && r != 5) {
			t.Errorf("scale %g px/m: %g m has mantissa %g, want 1, 2 or 5", pxPerM, b.metres, mant)
		}
		mPerCell := 2 / pxPerM
		if span := float64(b.cells) * mPerCell; math.Abs(span-b.metres) > mPerCell/2+1e-9 {
			t.Errorf("scale %g px/m: %d cells = %g m, label says %g m", pxPerM, b.cells, span, b.metres)
		}
	}
}

// The instrument must be able to return a positive and a negative before
// either is trusted: a doctored bar (twice the claimed distance) must be
// rejected by the same check the real frames go through.
func TestScaleBarInstrumentCanFail(t *testing.T) {
	c := widgets.NewCanvas(138, 37)
	c.SetBasis(widgets.DefaultBasis())
	c.SetScale(0.001)
	c.Center(orbital.Vec3{})
	b, _ := scaleBarFor(c.Scale())
	good := foundBar{cells: b.cells, metres: b.metres}
	if got, ok := barAgreesWithCanvas(c, good); !ok {
		t.Fatalf("instrument rejects an honest bar (measured %.1f cells, bar says %d)", got, good.cells)
	}
	bad := foundBar{cells: b.cells, metres: b.metres * 2}
	if _, ok := barAgreesWithCanvas(c, bad); ok {
		t.Fatal("instrument accepts a bar claiming twice its distance")
	}
}

func scaleBarMapSizes() [][2]int { return [][2]int{{140, 40}, {181, 49}} }

// Every map projection carries a bar whose length matches the screen.
func TestScaleBarOnTheMapInEveryProjection(t *testing.T) {
	for _, sz := range scaleBarMapSizes() {
		for _, mode := range sim.ProjectionViewModes {
			w, err := sim.NewWorld()
			if err != nil {
				t.Fatal(err)
			}
			w.ViewMode = mode
			v := NewOrbitView(plainTheme())
			v.Resize(sz[0], sz[1])
			frame := v.Render(w, 0, sz[0], sz[1])
			b, ok := findScaleBar(frame)
			if !ok {
				t.Errorf("%dx%d %s: no scale bar on the map", sz[0], sz[1], mode.Label())
				continue
			}
			if b.cells < scaleBarMinCells {
				t.Errorf("%dx%d %s: bar %d cells, want >= %d", sz[0], sz[1], mode.Label(), b.cells, scaleBarMinCells)
			}
			if got, ok := barAgreesWithCanvas(v.canvas, b); !ok {
				t.Errorf("%dx%d %s: bar says %d cells = %s, the canvas draws %s as %.1f cells", sz[0], sz[1], mode.Label(), b.cells, b.label, b.label, got)
			}
		}
	}
}

// Zooming moves the bar: the label (or length) changes, and it stays honest.
func TestScaleBarFollowsMapZoom(t *testing.T) {
	for _, sz := range scaleBarMapSizes() {
		w, _ := sim.NewWorld()
		w.ViewMode = sim.ViewTop
		v := NewOrbitView(plainTheme())
		v.Resize(sz[0], sz[1])
		before, ok := findScaleBar(v.Render(w, 0, sz[0], sz[1]))
		if !ok {
			t.Fatalf("%dx%d: no bar before zoom", sz[0], sz[1])
		}
		for i := 0; i < 6; i++ {
			v.ZoomIn()
		}
		in, ok := findScaleBar(v.Render(w, 0, sz[0], sz[1]))
		if !ok {
			t.Fatalf("%dx%d: no bar after zooming in", sz[0], sz[1])
		}
		if in.metres >= before.metres {
			t.Errorf("%dx%d: zoomed in %s -> %s, the bar should shrink", sz[0], sz[1], before.label, in.label)
		}
		if got, ok := barAgreesWithCanvas(v.canvas, in); !ok {
			t.Errorf("%dx%d zoomed: bar %d cells = %s but canvas says %.1f", sz[0], sz[1], in.cells, in.label, got)
		}
		for i := 0; i < 12; i++ {
			v.ZoomOut()
		}
		out, ok := findScaleBar(v.Render(w, 0, sz[0], sz[1]))
		if !ok || out.metres <= before.metres {
			t.Errorf("%dx%d: zoomed out %s -> %s, the bar should grow", sz[0], sz[1], before.label, out.label)
		}
	}
}

// Everything on the last row survives: the bar is added to free cells, never
// over the view label, the hint strip, a chip or the navball. Proved by
// rendering the same frame with the bar suppressed and diffing the cells.
func TestScaleBarOnlyTouchesItsOwnCells(t *testing.T) {
	for _, sz := range scaleBarMapSizes() {
		for _, mode := range sim.ProjectionViewModes {
			for _, declutter := range []bool{false, true} {
				w, _ := sim.NewWorld()
				w.ViewMode = mode
				v := NewOrbitView(plainTheme())
				v.SetDeclutter(declutter)
				v.Resize(sz[0], sz[1])
				with := stripANSIForBar(v.Render(w, 0, sz[0], sz[1]))
				scaleBarOff = true
				v2 := NewOrbitView(plainTheme())
				v2.SetDeclutter(declutter)
				v2.Resize(sz[0], sz[1])
				without := stripANSIForBar(v2.Render(w, 0, sz[0], sz[1]))
				scaleBarOff = false
				assertOnlyBarDiffers(t, sz, mode.Label(), with, without)
			}
		}
	}
}

// assertOnlyBarDiffers: every cell that differs between the frame with the
// bar and the frame without it is a cell of the bar itself (ticks, dashes,
// label, or the single space between them); the bar exists in `with`.
func assertOnlyBarDiffers(t *testing.T, sz [2]int, name, with, without string) {
	t.Helper()
	b, ok := findScaleBar(with)
	if !ok {
		t.Errorf("%dx%d %s: no bar in the frame", sz[0], sz[1], name)
		return
	}
	wl, ol := strings.Split(with, "\n"), strings.Split(without, "\n")
	if len(wl) != len(ol) {
		t.Errorf("%dx%d %s: bar changed the frame height %d -> %d", sz[0], sz[1], name, len(ol), len(wl))
		return
	}
	// The bar's cells plus the one after the label, which it clears so no
	// canvas dot touches the label (review #629 L3).
	width := b.cells + 2 + utf8.RuneCountInString(b.label) + 1
	for r := range wl {
		a, c := []rune(wl[r]), []rune(ol[r])
		if len(a) != len(c) {
			t.Errorf("%dx%d %s row %d: width %d -> %d", sz[0], sz[1], name, r, len(c), len(a))
			continue
		}
		for x := range a {
			if a[x] != c[x] && !(r == b.row && x >= b.col && x < b.col+width) {
				t.Errorf("%dx%d %s: the bar changed cell (%d,%d) %q -> %q outside its own %d cells", sz[0], sz[1], name, x, r, c[x], a[x], width)
				return
			}
		}
	}
	// Positive control: the bar's own cells really did differ.
	diff := 0
	a, c := []rune(wl[b.row]), []rune(ol[b.row])
	for x := b.col; x < b.col+width && x < len(a); x++ {
		if a[x] != c[x] {
			diff++
		}
	}
	if diff == 0 {
		t.Errorf("%dx%d %s: suppressing the bar changed nothing, the diff instrument is blind", sz[0], sz[1], name)
	}
}

// Proximity at 2 km: the bar and the drawn 1 km ring agree.
func TestScaleBarProximityAgreesWithTheRing(t *testing.T) {
	for _, sz := range scaleBarMapSizes() {
		w := proximityWorld(t, orbital.Vec3{X: 2000})
		v := newProximityTestView(t, sz[0], sz[1])
		w.ViewMode = sim.ViewProximity
		frame := v.Render(w, 0, sz[0], sz[1])
		b, ok := findScaleBar(frame)
		if !ok {
			t.Fatalf("%dx%d: no scale bar in the Proximity view at 2 km", sz[0], sz[1])
		}
		st, _ := w.ProximityState()
		if !v.proximityRingVisible(st, 1000) {
			t.Fatalf("%dx%d: the 1 km ring is not drawn at 2 km, the comparison has nothing to agree with", sz[0], sz[1])
		}
		ringPx := 1000 * v.canvas.Scale()
		if b.label != "1 km" {
			t.Errorf("%dx%d: bar reads %q at 2 km, want the drawn ring's 1 km", sz[0], sz[1], b.label)
		}
		if math.Abs(float64(b.cells)*2-ringPx) > 2 {
			t.Errorf("%dx%d: the 1 km bar is %d cells (%d px), the 1 km ring is %.1f px", sz[0], sz[1], b.cells, b.cells*2, ringPx)
		}
		if got, ok := barAgreesWithCanvas(v.canvas, b); !ok {
			t.Errorf("%dx%d: bar %d cells vs canvas %.1f", sz[0], sz[1], b.cells, got)
		}
		lines := strings.Split(stripANSIForBar(frame), "\n")
		if !strings.Contains(lines[b.row], proximityHintStripText) {
			t.Errorf("%dx%d: the hint strip is clipped or covered on the bar's row: %q", sz[0], sz[1], lines[b.row])
		}
	}
}

// Proximity far away: the picture is the same, the bar is the only scale.
func TestScaleBarProximityFarTarget(t *testing.T) {
	for _, sz := range scaleBarMapSizes() {
		var labels []string
		for _, rangeM := range []float64{2000, 360e6} {
			w := proximityWorld(t, orbital.Vec3{X: rangeM})
			v := newProximityTestView(t, sz[0], sz[1])
			w.ViewMode = sim.ViewProximity
			b, ok := findScaleBar(v.Render(w, 0, sz[0], sz[1]))
			if !ok {
				t.Fatalf("%dx%d range %.0f: no scale bar", sz[0], sz[1], rangeM)
			}
			if got, ok := barAgreesWithCanvas(v.canvas, b); !ok {
				t.Errorf("%dx%d range %.0f: bar %d cells = %s, canvas %.1f", sz[0], sz[1], rangeM, b.cells, b.label, got)
			}
			labels = append(labels, b.label)
		}
		if labels[0] == labels[1] || !strings.HasSuffix(labels[1], "Mm") {
			t.Errorf("%dx%d: 2 km reads %q and 360 Mm reads %q, want different bars and Mm for the far target", sz[0], sz[1], labels[0], labels[1])
		}
	}
}

// Proximity zoom changes the label.
func TestScaleBarProximityFollowsZoom(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 2000})
	v := newProximityTestView(t, 140, 40)
	w.ViewMode = sim.ViewProximity
	before, _ := findScaleBar(v.Render(w, 0, 140, 40))
	for i := 0; i < 6; i++ {
		v.ZoomIn()
	}
	after, ok := findScaleBar(v.Render(w, 0, 140, 40))
	if !ok || after.label == before.label || after.metres >= before.metres {
		t.Errorf("zoom in: %q -> %q, want a smaller bar", before.label, after.label)
	}
}

func TestScaleBarProximityOnlyTouchesItsOwnCells(t *testing.T) {
	for _, sz := range scaleBarMapSizes() {
		for _, rangeM := range []float64{2000, 360e6} {
			w := proximityWorld(t, orbital.Vec3{X: rangeM})
			w.ViewMode = sim.ViewProximity
			v := newProximityTestView(t, sz[0], sz[1])
			with := stripANSIForBar(v.Render(w, 0, sz[0], sz[1]))
			scaleBarOff = true
			v2 := newProximityTestView(t, sz[0], sz[1])
			without := stripANSIForBar(v2.Render(w, 0, sz[0], sz[1]))
			scaleBarOff = false
			assertOnlyBarDiffers(t, sz, "Proximity", with, without)
		}
	}
}

// The launch view's bar is honest to the altitude-derived chase-cam scale.
func TestScaleBarOnTheLaunchView(t *testing.T) {
	for _, sz := range scaleBarMapSizes() {
		w, _ := spawnSaturnVOnPad(t)
		th := launchThemeForTest()
		v := NewLaunchView(th, NewOrbitView(th))
		pad, ok := findScaleBar(v.Render(w, sz[0], sz[1]))
		if !ok {
			t.Fatalf("%dx%d: no scale bar on the launch view", sz[0], sz[1])
		}
		if got, ok := barAgreesWithCanvas(v.canvas, pad); !ok {
			t.Errorf("%dx%d pad: bar %d cells = %s, canvas %.1f", sz[0], sz[1], pad.cells, pad.label, got)
		}
		// Pinned zoom (the +/- path): a 50 m/px chase cam reads a bigger bar.
		w.LaunchZoom = 50
		far, ok := findScaleBar(v.Render(w, sz[0], sz[1]))
		if !ok || far.metres <= pad.metres {
			t.Fatalf("%dx%d: zoomed out the bar went %q -> %q, want bigger", sz[0], sz[1], pad.label, far.label)
		}
		if got, ok := barAgreesWithCanvas(v.canvas, far); !ok {
			t.Errorf("%dx%d zoomed out: bar %d cells = %s, canvas %.1f", sz[0], sz[1], far.cells, far.label, got)
		}
	}
}

func TestScaleBarLaunchOnlyTouchesItsOwnCells(t *testing.T) {
	for _, sz := range scaleBarMapSizes() {
		w, _ := spawnSaturnVOnPad(t)
		th := launchThemeForTest()
		with := stripANSIForBar(NewLaunchView(th, NewOrbitView(th)).Render(w, sz[0], sz[1]))
		scaleBarOff = true
		without := stripANSIForBar(NewLaunchView(th, NewOrbitView(th)).Render(w, sz[0], sz[1]))
		scaleBarOff = false
		assertOnlyBarDiffers(t, sz, "launch", with, without)
	}
}

// Projection honesty: the tilted view is the one that foreshortens (the
// vertical axis shrinks by cos of the tilt), so check it at several tilts
// and in the pad fallback basis. The horizontal axis keeps the full scale at
// the focus depth, which is what the bar measures.
func TestScaleBarTiltedProjectionIsMeasuredOnTheHorizontalAxis(t *testing.T) {
	for _, theta := range []float64{0, 30, 60, 85} {
		w, _ := sim.NewWorld()
		w.ViewMode = sim.ViewTilted
		w.ViewTilt.Theta = theta
		v := NewOrbitView(plainTheme())
		v.Resize(140, 40)
		b, ok := findScaleBar(v.Render(w, 0, 140, 40))
		if !ok {
			t.Fatalf("tilt %g: no bar", theta)
		}
		if got, ok := barAgreesWithCanvas(v.canvas, b); !ok {
			t.Errorf("tilt %g: bar %d cells = %s, canvas draws %.1f", theta, b.cells, b.label, got)
		}
		if n := v.canvas.Basis().X.Norm(); n < 0.999999 || n > 1.000001 {
			t.Errorf("tilt %g: horizontal basis has length %g, the one-scale premise fails", theta, n)
		}
	}
	// The pad (landed) uses TiltedWorldBasis.
	w, _ := spawnSaturnVOnPad(t)
	w.ViewMode = sim.ViewTilted
	v := NewOrbitView(plainTheme())
	v.Resize(140, 40)
	b, ok := findScaleBar(v.Render(w, 0, 140, 40))
	if !ok {
		t.Fatal("landed tilted view: no bar")
	}
	if got, ok := barAgreesWithCanvas(v.canvas, b); !ok {
		t.Errorf("landed tilted: bar %d cells = %s, canvas %.1f", b.cells, b.label, got)
	}
}

// A long target name fills the Proximity label row; the bar must step up a
// row rather than land on the label or strip (the strip itself clips at this
// name length on main; the bar must add nothing to that), and still be whole.
func TestScaleBarProximityLongNameKeepsTheStrip(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 2000})
	w.Crafts[1].Name = "Apollo Lunar Excursion Module Stack 12"
	v := newProximityTestView(t, 140, 40)
	w.ViewMode = sim.ViewProximity
	frame := v.Render(w, 0, 140, 40)
	b, ok := findScaleBar(frame)
	if !ok {
		t.Fatal("no bar with a long target name")
	}
	last := strings.Split(stripANSIForBar(frame), "\n")
	if b.row == len(last)-2 {
		t.Errorf("the bar sits on the full label row (row %d) instead of stepping up", b.row)
	}
	scaleBarOff = true
	v2 := newProximityTestView(t, 140, 40)
	without := stripANSIForBar(v2.Render(w, 0, 140, 40))
	scaleBarOff = false
	assertOnlyBarDiffers(t, [2]int{140, 40}, "Proximity long name", stripANSIForBar(frame), without)
}

// With real colour on, the launch row's styled bar is still whole (the HUD
// strip's width is measured without the escapes).
func TestScaleBarLaunchSurvivesColour(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	th := launchThemeForTest()
	th.Primary = lipgloss.NewStyle().Foreground(lipgloss.Color("#88CCFF"))
	w, _ := spawnSaturnVOnPad(t)
	frame := NewLaunchView(th, NewOrbitView(th)).Render(w, 140, 40)
	if !strings.Contains(frame, "\x1b[") {
		t.Fatal("colour is not on, the test would be vacuous")
	}
	if _, ok := findScaleBar(frame); !ok {
		t.Errorf("styled launch frame lost its bar: %q", strings.Split(frame, "\n")[38])
	}
}

// Review #629 L3: the cell after the bar's label is canvas, not blank. Over a
// planet the disk's dots filled it and read as part of the label
// (`├───────────┤ 50 km⣿`). Saturate the canvas with ink, paint the bar, and
// read the cell after the label out of the frame the player gets.
func TestScaleBarLabelEndsInABlankCell(t *testing.T) {
	cases := []struct {
		name         string
		fbRow, fbCol int
		stripEnd     int
	}{
		{"last row, flush right", -1, -1, 0},
		{"fallback cell, right-aligned", 2, -1, 1000},
		{"fallback cell, left (Proximity)", 2, 0, 1000},
	}
	for _, sz := range scaleBarMapSizes() {
		for _, tc := range cases {
			w, _ := sim.NewWorld()
			v := NewOrbitView(plainTheme())
			v.Resize(sz[0], sz[1])
			v.Render(w, 0, sz[0], sz[1])
			inked := func(paint bool) string {
				// Render with no bar of its own, so the only bar is the one
				// painted below.
				scaleBarOff = true
				defer func() { scaleBarOff = false }()
				v.Render(w, 0, sz[0], sz[1])
				// Ink every pixel, then paint the bar over it.
				for y := 0; y < v.canvas.Rows()*4; y++ {
					for x := 0; x < v.canvas.Cols()*2; x++ {
						v.canvas.PlotColoredTagged(v.canvas.Unproject(x, y), widgets.CellTag{})
					}
				}
				if paint {
					scaleBarOff = false
					v.paintScaleBar(tc.stripEnd, tc.fbRow, tc.fbCol)
				}
				return stripANSIForBar(v.canvas.String())
			}
			with := inked(true)
			b, ok := findScaleBar(with)
			if !ok {
				t.Errorf("%dx%d %s: no bar painted", sz[0], sz[1], tc.name)
				continue
			}
			after := b.col + b.cells + 2 + utf8.RuneCountInString(b.label)
			row := []rune(strings.Split(with, "\n")[b.row])
			if after < len(row) && row[after] != ' ' {
				t.Errorf("%dx%d %s: the cell after the label (%d,%d) is %q, want a blank", sz[0], sz[1], tc.name, after, b.row, row[after])
			}
			// Positive control: with the bar off, that cell is ink.
			bare := []rune(strings.Split(inked(false), "\n")[b.row])
			if after < len(bare) && bare[after] == ' ' {
				t.Errorf("%dx%d %s: the cell is blank even without the bar, the saturation instrument is blind", sz[0], sz[1], tc.name)
			}
		}
	}
}
