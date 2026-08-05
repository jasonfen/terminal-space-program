// Package screens — ascent-half render tests for the surface view (issue
// #348 §3 / ADR 0043): the nose-vs-prograde vector stubs, the dashed
// path ahead of a climbing vessel, and the ATMOSPHERE chip's vertical
// Q-band scale. Mirrors launch_descent_test.go's structure for the
// descent half (PR #354).

package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// ascendingCraftWorld parks the world's active craft `altM` above
// `bodyID`'s surface, climbing straight up (+X, radial) at `climbMps`,
// nose pointed in `nose`. A simplified (non-co-rotating) construction —
// good enough for render smoke tests; the exact coincide/diverge
// geometry is already pinned at the sim layer
// (internal/sim/ascent_test.go).
func ascendingCraftWorld(t *testing.T, bodyID string, altM, climbMps float64, nose orbital.Vec3) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active craft")
	}
	for _, b := range w.System().Bodies {
		if b.ID == bodyID {
			c.Primary = b
		}
	}
	c.Landed = false
	c.Crashed = false
	rHat := orbital.Vec3{X: 1}
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + altM)
	c.State.V = rHat.Scale(climbMps)
	c.State.M = c.TotalMass()
	c.CurrentAttitudeDir = nose
	return w
}

// TestAscentArcIsPlannedDashed: the predicted path ahead of a climbing
// vessel is a PLAN, so it must ink at ClassPlanned's dash cadence (ADR
// 0041 §2 / PR #353), matching the descent arc's own test
// (TestDescentArcIsPlannedDashed).
func TestAscentArcIsPlannedDashed(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	v.Resize(120, 40)
	v.canvas.Clear()
	v.canvas.SetScale(1) // 1 px per metre
	v.canvas.Center(orbital.Vec3{})

	pts := []orbital.Vec3{{X: -100}, {X: 100}}
	arc := sim.AscentPath{Path: pts}
	v.drawAscentArc(orbital.Vec3{}, arc)
	live := countBrailleDots(v.canvas.String())
	if live == 0 {
		t.Fatal("ascent arc inked nothing")
	}

	measure := func(class widgets.LineClass) int {
		c := widgets.NewCanvas(v.canvas.Cols(), v.canvas.Rows())
		c.SetScale(1)
		c.Center(orbital.Vec3{})
		c.Clear()
		c.PlotPolylineClass(pts, render.ColorPlannedNode, class)
		return countBrailleDots(c.String())
	}
	planned, solid := measure(widgets.ClassPlanned), measure(widgets.ClassReal)
	if live != planned {
		t.Errorf("arc inked %d dots, want ClassPlanned's %d", live, planned)
	}
	if planned >= solid {
		t.Errorf("ClassPlanned inked %d dots vs ClassReal's %d — the dash pattern is not thinning the line", planned, solid)
	}
}

// TestAscentArcClipsAtViewEdge: a predicted path running far past the
// visible canvas (e.g. an ascent still climbing toward a distant
// apoapsis) must not panic and must still ink the portion that's
// actually on-screen — the canvas's own segment clipping
// (clipSegmentToCanvas) is what issue #348 §3 relies on for "clip
// gracefully" rather than any special-case logic in drawAscentArc.
func TestAscentArcClipsAtViewEdge(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	v.Resize(120, 40)
	v.canvas.Clear()
	v.canvas.SetScale(1)
	v.canvas.Center(orbital.Vec3{})

	// The canvas is a few hundred pixels wide at scale 1; this run goes
	// four orders of magnitude past that.
	pts := []orbital.Vec3{{X: 0}, {X: 50}, {X: 1_000_000}}
	arc := sim.AscentPath{Path: pts}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("drawAscentArc panicked on an off-canvas path: %v", r)
		}
	}()
	v.drawAscentArc(orbital.Vec3{}, arc)
	if n := countBrailleDots(v.canvas.String()); n == 0 {
		t.Error("off-canvas-bound arc inked nothing near the sprite, want the on-screen portion drawn")
	}
}

// TestAscentAttitudeMarkersDrawBothColors: the nose and prograde stubs
// use the navball's own hues (ColorNavballMarkerNoseFront /
// ColorNavballMarkerPrograde) — when the two directions are distinct,
// both colors must land on the canvas.
func TestAscentAttitudeMarkersDrawBothColors(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	v.Resize(120, 40)
	v.canvas.Clear()
	v.canvas.SetScale(1)
	v.canvas.Center(orbital.Vec3{})

	vec := sim.AttitudeVectors{
		NoseDir:     orbital.Vec3{X: 1},
		ProgradeDir: orbital.Vec3{Y: 1},
	}
	v.drawAscentAttitudeMarkers(vec, orbital.Vec3{}, 1.0)
	if n := v.canvas.CountColor(render.ColorNavballMarkerNoseFront); n == 0 {
		t.Error("nose stub inked no ColorNavballMarkerNoseFront cells")
	}
	if n := v.canvas.CountColor(render.ColorNavballMarkerPrograde); n == 0 {
		t.Error("prograde stub inked no ColorNavballMarkerPrograde cells")
	}
}

// TestAscentAttitudeMarkersSkipUndefinedVectors: a zero-value
// AttitudeVectors (the AttitudeVectorsFor ok=false case) must not paint
// anything — there's no fabricated direction to draw a stub toward.
func TestAscentAttitudeMarkersSkipUndefinedVectors(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	v.Resize(120, 40)
	v.canvas.Clear()
	v.canvas.SetScale(1)
	v.canvas.Center(orbital.Vec3{})

	v.drawAscentAttitudeMarkers(sim.AttitudeVectors{}, orbital.Vec3{}, 1.0)
	if n := countBrailleDots(v.canvas.String()); n != 0 {
		t.Errorf("undefined attitude vectors inked %d dots, want 0", n)
	}
}

// TestQBandRowIndexEdgesAndClamp pins qBandRowIndex's mapping: the
// atmosphere's outer edge is row 0, the ground is the last row, and
// altitudes outside [0, cutoff] clamp rather than going out of range.
func TestQBandRowIndexEdgesAndClamp(t *testing.T) {
	const cutoff = 150_000.0
	const rows = 6
	cases := []struct {
		name string
		altM float64
		want int
	}{
		{"ground", 0, rows - 1},
		{"top of atmosphere", cutoff, 0},
		{"above the cutoff", cutoff * 2, 0},
		{"below ground", -1000, rows - 1},
		{"midpoint", cutoff / 2, rows / 2},
	}
	for _, c := range cases {
		if got := qBandRowIndex(c.altM, cutoff, rows); got != c.want {
			t.Errorf("%s: qBandRowIndex(%.0f, %.0f, %d) = %d, want %d", c.name, c.altM, cutoff, rows, got, c.want)
		}
	}
}

// TestAscentQBandLinesMarksCurrentAndMaxQ pins the ATMOSPHERE chip's
// rendered rows: the vessel's current band carries the craft glyph, the
// peak-Q band carries the max-Q glyph, and every other row is a bare
// tick — so a formatting change has to be deliberate, matching the
// descent corridor's own instrument-lines test.
func TestAscentQBandLinesMarksCurrentAndMaxQ(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	qb := sim.AscentQBand{
		AtmosphereDepthM: 150_000,
		CurrentAltM:      125_000, // row 1 of 6 (top-adjacent band)
		CurrentQPa:       1234.5,
		MaxQAltM:         25_000, // row 4 of 6
		MaxQPa:           45_678,
		HasMaxQ:          true,
	}
	got := v.ascentQBandLines(qb)
	if len(got) != ascentQBandRows+3 { // header + rows + Q + max Q
		t.Fatalf("got %d lines, want %d:\n%v", len(got), ascentQBandRows+3, got)
	}
	if !strings.Contains(got[0], "ATMOSPHERE") {
		t.Errorf("line 0 = %q, want the ATMOSPHERE header", got[0])
	}
	curRow := qBandRowIndex(qb.CurrentAltM, qb.AtmosphereDepthM, ascentQBandRows)
	maxRow := qBandRowIndex(qb.MaxQAltM, qb.AtmosphereDepthM, ascentQBandRows)
	if !strings.Contains(got[1+curRow], ascentQBandCraftGlyph) {
		t.Errorf("current-altitude row %d = %q, want the craft glyph %q", curRow, got[1+curRow], ascentQBandCraftGlyph)
	}
	if !strings.Contains(got[1+maxRow], ascentQBandMaxQGlyph) {
		t.Errorf("max-Q row %d = %q, want the max-Q glyph %q", maxRow, got[1+maxRow], ascentQBandMaxQGlyph)
	}
	if !strings.Contains(got[len(got)-2], "1.2") {
		// 1234.5 Pa → 1.2 kPa
		t.Errorf("Q row = %q, want the current Q value in kPa", got[len(got)-2])
	}
	if !strings.Contains(got[len(got)-1], "45.7") {
		// 45678 Pa → 45.7 kPa
		t.Errorf("max Q row = %q, want the max Q value in kPa", got[len(got)-1])
	}
}

// TestAscentQBandLinesOmitsMaxQBeforeMeasured: a fresh session that
// hasn't ratcheted a peak yet must not fabricate one at the ground.
func TestAscentQBandLinesOmitsMaxQBeforeMeasured(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	qb := sim.AscentQBand{AtmosphereDepthM: 150_000, CurrentAltM: 1_000, CurrentQPa: 10, HasMaxQ: false}
	got := v.ascentQBandLines(qb)
	for _, line := range got {
		if strings.Contains(line, ascentQBandMaxQGlyph) || strings.Contains(line, "max Q") {
			t.Errorf("line %q mentions max Q before any peak was measured", line)
		}
	}
}

// TestLaunchViewAscentInstrumentsAt80x24: the ascent half has to survive
// the smallest supported terminal — the ATMOSPHERE chip composites onto
// the canvas and the attitude stubs land near the sprite at 80×24, not
// only at roomy dev-window sizes.
func TestLaunchViewAscentInstrumentsAt80x24(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := ascendingCraftWorld(t, "earth", 20_000, 300, orbital.Vec3{X: 1})

	out := v.Render(w, 80, 24)
	if !strings.Contains(stripANSI(out), "ATMOSPHERE") {
		t.Errorf("80×24 ascending render is missing the ATMOSPHERE chip:\n%s", out)
	}
	if rows := len(strings.Split(out, "\n")); rows > 24 {
		t.Errorf("render is %d rows tall, want ≤ 24", rows)
	}
	if n := v.canvas.CountColor(render.ColorNavballMarkerNoseFront); n == 0 {
		t.Error("no nose-direction marker drawn during ascent")
	}
	if n := v.canvas.CountColor(render.ColorNavballMarkerPrograde); n == 0 {
		t.Error("no prograde marker drawn during ascent")
	}
}

// TestLaunchViewNoAscentInstrumentsOnDescentOrCoast: the ATMOSPHERE chip
// is gated on climbing, mirroring TestLaunchViewNoDescentInstrumentsOnAscent
// — a falling vessel or one coasting in a stable orbit gets none of the
// ascent cues, so the surface view never stacks both instrument sets.
func TestLaunchViewNoAscentInstrumentsOnDescentOrCoast(t *testing.T) {
	th := launchThemeForTest()

	// Falling: mirrors descendingMoonCraft's own falling case.
	fallingV := NewLaunchView(th, NewOrbitView(th))
	falling := descendingMoonCraft(t, 20_000, 120)
	fallingOut := stripANSI(fallingV.Render(falling, 120, 40))
	if strings.Contains(fallingOut, "ATMOSPHERE") {
		t.Errorf("falling vehicle rendered the ATMOSPHERE chip:\n%s", fallingOut)
	}

	// Coasting in a stable circular orbit: neither climbing nor falling.
	coastV := NewLaunchView(th, NewOrbitView(th))
	coast := ascendingCraftWorld(t, "earth", 300_000, 0, orbital.Vec3{X: 1})
	c := coast.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	coastOut := stripANSI(coastV.Render(coast, 120, 40))
	if strings.Contains(coastOut, "ATMOSPHERE") {
		t.Errorf("coasting vehicle rendered the ATMOSPHERE chip:\n%s", coastOut)
	}
	if strings.Contains(coastOut, "DESCENT CORRIDOR") {
		t.Errorf("coasting vehicle rendered the DESCENT CORRIDOR chip:\n%s", coastOut)
	}
}
