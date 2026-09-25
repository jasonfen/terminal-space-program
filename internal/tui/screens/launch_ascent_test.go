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
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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

// canvasCellRuneAt returns the single rune at (row, col) of a rendered
// canvas string (ANSI stripped first), for tests that need to read a
// specific screen cell rather than search a whole line.
func canvasCellRuneAt(t *testing.T, canvasStr string, row, col int) rune {
	t.Helper()
	lines := strings.Split(stripANSI(canvasStr), "\n")
	if row < 0 || row >= len(lines) {
		t.Fatalf("row %d out of range (canvas has %d rows)", row, len(lines))
	}
	runes := []rune(lines[row])
	if col < 0 || col >= len(runes) {
		t.Fatalf("col %d out of range (row %d has %d cols)", col, row, len(runes))
	}
	return runes[col]
}

// TestDrawAscentAirScaleMarksCurrentAndMaxQ (ADR 0051 slice 4 item 3, the
// retired ATMOSPHERE box's own line-based test ported to the canvas):
// the vessel's current band carries the craft glyph, the peak-Q band
// carries the max-Q glyph, the top and bottom bands carry their edge
// glyphs, and every other row stays a bare tick.
func TestDrawAscentAirScaleMarksCurrentAndMaxQ(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	v.Resize(120, 40)
	v.canvas.Clear()
	qb := sim.AscentQBand{
		AtmosphereDepthM: 150_000,
		CurrentAltM:      125_000,
		CurrentQPa:       1234.5,
		MaxQAltM:         25_000,
		MaxQPa:           45_678,
		HasMaxQ:          true,
	}
	const col, topRow = 50, 1
	v.drawAscentAirScale(qb, col, topRow, 0)
	out := v.canvas.String()

	curRow := qBandRowIndex(qb.CurrentAltM, qb.AtmosphereDepthM, airScaleRows)
	maxRow := qBandRowIndex(qb.MaxQAltM, qb.AtmosphereDepthM, airScaleRows)
	if curRow == maxRow {
		t.Fatalf("setup: current row and max-Q row coincide (%d); pick inputs that separate them", curRow)
	}
	if got := canvasCellRuneAt(t, out, topRow+curRow, col); string(got) != ascentQBandCraftGlyph {
		t.Errorf("current-altitude row %d, col %d = %q, want the craft glyph %q", curRow, col, string(got), ascentQBandCraftGlyph)
	}
	if got := canvasCellRuneAt(t, out, topRow+maxRow, col); string(got) != ascentQBandMaxQGlyph {
		t.Errorf("max-Q row %d, col %d = %q, want the max-Q glyph %q", maxRow, col, string(got), ascentQBandMaxQGlyph)
	}
	if got := canvasCellRuneAt(t, out, topRow, col); got != '┬' {
		t.Errorf("top row, col %d = %q, want the top-of-air glyph ┬", col, string(got))
	}
	if got := canvasCellRuneAt(t, out, topRow+airScaleRows-1, col); got != '┴' {
		t.Errorf("bottom row, col %d = %q, want the ground glyph ┴", col, string(got))
	}
	for i := 1; i < airScaleRows-1; i++ {
		if i == curRow || i == maxRow {
			continue
		}
		if got := canvasCellRuneAt(t, out, topRow+i, col); string(got) != ascentQBandTickGlyph {
			t.Errorf("row %d, col %d = %q, want the bare tick %q", i, col, string(got), ascentQBandTickGlyph)
		}
	}
}

// TestDrawAscentAirScaleOmitsMaxQBeforeMeasured: a fresh session that
// hasn't ratcheted a peak yet must not fabricate one at the ground.
func TestDrawAscentAirScaleOmitsMaxQBeforeMeasured(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	v.Resize(120, 40)
	v.canvas.Clear()
	qb := sim.AscentQBand{AtmosphereDepthM: 150_000, CurrentAltM: 1_000, CurrentQPa: 10, HasMaxQ: false}
	v.drawAscentAirScale(qb, 50, 1, 0)
	out := v.canvas.String()
	if strings.Contains(stripANSI(out), ascentQBandMaxQGlyph) {
		t.Errorf("scale mentions max Q before any peak was measured:\n%s", out)
	}
}

// TestDrawAscentAirScaleTracksAltitude (ADR 0051 slice 4 item 3): a
// higher current altitude must put the craft glyph on a LOWER row index
// (row 0 is the top of the air), so the marker climbs the scale as the
// vessel climbs. Proven red first by inverting qBandRowIndex's fraction
// (see the item's vault log for the sabotage transcript).
func TestDrawAscentAirScaleTracksAltitude(t *testing.T) {
	rowFor := func(altM float64) int {
		v := NewLaunchView(launchThemeForTest(), nil)
		v.Resize(120, 40)
		v.canvas.Clear()
		v.drawAscentAirScale(sim.AscentQBand{AtmosphereDepthM: 150_000, CurrentAltM: altM}, 50, 1, 0)
		out := v.canvas.String()
		for i := 0; i < airScaleRows; i++ {
			if canvasCellRuneAt(t, out, 1+i, 50) == []rune(ascentQBandCraftGlyph)[0] {
				return i
			}
		}
		t.Fatalf("craft glyph not found on the scale for altitude %.0f", altM)
		return -1
	}
	low := rowFor(20_000)
	high := rowFor(100_000)
	if high >= low {
		t.Errorf("100 km put the marker on row %d, want a row above 20 km's row %d (lower index = higher on screen)", high, low)
	}
}

// TestLaunchViewAscentInstrumentsAt80x24: the ascent half has to survive
// the smallest supported terminal: the attitude stubs land near the
// sprite at 80×24, not only at roomy dev-window sizes.
// Rendered at the Design Size (ADR 0046/0051): ADR 0051's eight
// instrument boxes are much larger than the VESSEL/ATTITUDE/etc. chips
// they replace and are never dropped (Core priority). ADR 0051 slice 4
// item 1 retires the ATMOSPHERE box outright (decision 8 draws the same
// reading into the horizon picture instead, item 3), so this no longer
// checks for it as text; item 3 is what re-adds an ascent-only assertion
// for the picture-drawn air scale. 140x40 is the one canvas the Design
// Size floor actually guarantees room at.
func TestLaunchViewAscentInstrumentsAt80x24(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := ascendingCraftWorld(t, "earth", 20_000, 300, orbital.Vec3{X: 1})

	out := v.Render(w, DesignWidth, DesignHeight)
	if strings.Contains(stripANSI(out), "ATMOSPHERE") {
		t.Errorf("ascending render still shows the retired ATMOSPHERE box:\n%s", out)
	}
	if rows := len(strings.Split(out, "\n")); rows > DesignHeight {
		t.Errorf("render is %d rows tall, want <= %d", rows, DesignHeight)
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

// TestLaunchViewShowsTargetDuringEarthAscent (ADR 0051 slice 4 item 1):
// the LAUNCH view used to append its own DESCENT CORRIDOR and ATMOSPHERE
// boxes on top of the shared chips assembleChips already returns. During
// an Earth ascent with the navball actually showing (NavballSubObserver
// resolving, as it does the instant a real craft has a defined attitude)
// and a target set, the extra ATMOSPHERE box pushed the right column's
// budget (NAVIGATION 10 + TARGET 7 = 17 of 17, no slack) over the edge;
// the drop phase then took the stub's own row from TARGET too (Core
// tier, but still droppable when the stub itself doesn't fit), hiding
// both behind "▸ +2 hidden"
// (ux-reviews/20260913-readout-overlap/adr0051-slice2b/04-burn-map.txt).
// Retiring both appends must bring TARGET back at the Design Size.
func TestLaunchViewShowsTargetDuringEarthAscent(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w, c := spawnSaturnVOnPad(t)
	c.Landed = false
	c.CurrentAttitudeDir = orbital.Vec3{X: 1}
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + 20_000)
	c.State.V = rHat.Scale(300)
	c.State.M = c.TotalMass()

	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" || b.ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx <= 0 {
		t.Fatalf("moon not found in default system")
	}
	w.SetTargetBody(moonIdx)
	if _, _, ok := w.NavballSubObserver(); !ok {
		t.Fatal("setup: expected the navball to resolve so its rows are reserved, matching the real overflow")
	}

	out := stripANSI(v.Render(w, DesignWidth, DesignHeight))
	if !strings.Contains(out, "TARGET") {
		t.Errorf("LAUNCH view during an Earth ascent with a target set is missing the TARGET box:\n%s", out)
	}
	if strings.Contains(out, "hidden") {
		t.Errorf("LAUNCH view during an Earth ascent shows a hidden-chip stub:\n%s", out)
	}
}

// TestLaunchViewAirScaleDoesNotHideTarget (ADR 0051 slice 4 item 3): the
// air scale paints directly onto the canvas before composeChips runs,
// entirely outside its side-budget accounting, so it must not cost the
// right column any budget the way the retired ATMOSPHERE box did (item
// 1's own regression, reproduced with the identical setup here). TARGET
// must stay visible, and the scale's own top/bottom edges must appear.
func TestLaunchViewAirScaleDoesNotHideTarget(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w, c := spawnSaturnVOnPad(t)
	c.Landed = false
	c.CurrentAttitudeDir = orbital.Vec3{X: 1}
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + 20_000)
	c.State.V = rHat.Scale(300)
	c.State.M = c.TotalMass()

	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" || b.ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx <= 0 {
		t.Fatalf("moon not found in default system")
	}
	w.SetTargetBody(moonIdx)
	if _, _, ok := w.NavballSubObserver(); !ok {
		t.Fatal("setup: expected the navball to resolve so its rows are reserved")
	}

	out := stripANSI(v.Render(w, DesignWidth, DesignHeight))
	if !strings.Contains(out, "TARGET") {
		t.Errorf("air scale cost TARGET its box:\n%s", out)
	}
	if strings.Contains(out, "hidden") {
		t.Errorf("air scale triggered a hidden-chip stub:\n%s", out)
	}
	if !strings.Contains(out, "┬") || !strings.Contains(out, "┴") {
		t.Errorf("air scale did not draw its top/bottom edges:\n%s", out)
	}
}

// TestAirScaleColumnBoundClearsNavigationAndTarget (ADR 0051 slice 4
// item 3): the bound must sit strictly left of BOTH boxes' left edges,
// whichever is wider this frame, with at least one column of clearance
// (the ADR audit's own "one column clear").
func TestAirScaleColumnBoundClearsNavigationAndTarget(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w, c := spawnSaturnVOnPad(t)
	c.Landed = false
	c.CurrentAttitudeDir = orbital.Vec3{X: 1}
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + 20_000)
	c.State.V = rHat.Scale(300)
	c.State.M = c.TotalMass()
	v.Resize(DesignWidth, DesignHeight)

	cCols := v.canvas.Cols()
	bound := v.airScaleColumnBound(w, cCols)

	navLines := v.hudSource.buildNavigationBox(w)
	_, navW := padChipBlock(navLines)
	navLeftEdge := cCols - (navW + 2)

	targetLines := v.hudSource.buildTargetBox(w)
	_, targetW := padChipBlock(targetLines)
	targetLeftEdge := cCols - (targetW + 2)

	if bound >= navLeftEdge {
		t.Errorf("bound %d does not clear NAVIGATION's left edge %d (width %d)", bound, navLeftEdge, navW+2)
	}
	if bound >= targetLeftEdge {
		t.Errorf("bound %d does not clear TARGET's left edge %d (width %d)", bound, targetLeftEdge, targetW+2)
	}

	// The left side (review finding 4, 2026-09-25): this test's own name
	// claims both boxes, but until now only ever measured the right;
	// nothing checked that a label could clear PROPELLANT/GUIDANCE on the
	// left, which is exactly the side the defect painted over.
	leftBound := v.airScaleLabelLeftBound(w)

	engineLines := v.hudSource.buildEngineBox(w)
	_, engineW := padChipBlock(engineLines)
	engineRightEdge := engineW + 2

	propLines := v.hudSource.buildPropellantBox(w)
	_, propW := padChipBlock(propLines)
	propRightEdge := propW + 2

	guidanceLines := v.hudSource.buildGuidanceBox(w)
	_, guidanceW := padChipBlock(guidanceLines)
	guidanceRightEdge := guidanceW + 2

	if leftBound <= engineRightEdge {
		t.Errorf("label left bound %d does not clear ENGINE's right edge %d (width %d)", leftBound, engineRightEdge, engineW+2)
	}
	if leftBound <= propRightEdge {
		t.Errorf("label left bound %d does not clear PROPELLANT's right edge %d (width %d)", leftBound, propRightEdge, propW+2)
	}
	if leftBound <= guidanceRightEdge {
		t.Errorf("label left bound %d does not clear GUIDANCE's right edge %d (width %d)", leftBound, guidanceRightEdge, guidanceW+2)
	}
}

// ascentTrendFixture returns a Saturn V pad-spawned craft moved to a 20
// km ascent, 300 m/s straight up (satisfies AscentCueFor's climb-rate
// gate, no orbital-element requirement of its own) PLUS a 7000 m/s
// tangential component, so the state carries real, nonzero angular
// momentum and craftLiveElements/Apoapsis is actually defined and
// movable (a purely radial velocity, TestAirScaleColumnBoundClearsNavigationAndTarget's
// own fixture, is a degenerate zero-angular-momentum trajectory whose
// apoapsis reads "-" and never trends).
func ascentTrendFixture(t *testing.T) (*sim.World, *spacecraft.Spacecraft) {
	t.Helper()
	w, c := spawnSaturnVOnPad(t)
	c.Landed = false
	c.CurrentAttitudeDir = orbital.Vec3{X: 1}
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	tHat := rHat.Cross(orbital.Vec3{Z: 1}).Unit()
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + 20_000)
	c.State.V = rHat.Scale(300).Add(tHat.Scale(7000))
	c.State.M = c.TotalMass()
	return w, c
}

// TestLaunchViewApTrendArrowAgreesWithMap (review finding 3, 2026-09-25):
// airScaleColumnBound (launch.go) measures NAVIGATION's width by calling
// v.hudSource.buildNavigationBox a second time per frame purely to
// measure it, and that call mutates the trend sampler
// (navigationApPeCells' v.ascentTrendCraft/ApoM/Time, orbit_box_navigation.go).
// composeChips then builds NAVIGATION again for real in the same frame,
// with dt = 0 against the measurement call's just-written timestamp, so
// the arrow the pilot actually sees is always empty during an Earth
// ascent even while apoapsis is genuinely climbing. The map builds
// NAVIGATION once per frame and is unaffected.
//
// Two frames one second apart with the velocity scaled 1.05 (mirrors the
// review's own probe R2) through two independent OrbitView/LaunchView
// pairs so each pipeline gets its own honest two-frame history: the
// map's build is the positive control (it must show the arrow, or the
// fixture itself is broken), and the LAUNCH view's real rendered output
// must show the same arrow for the same climbing instant.
func TestLaunchViewApTrendArrowAgreesWithMap(t *testing.T) {
	th := launchThemeForTest()

	// Map pipeline: one buildNavigationBox call per frame.
	mapHUD := NewOrbitView(th)
	wMap, cMap := ascentTrendFixture(t)
	mapHUD.buildNavigationBox(wMap) // frame 1, establishes the baseline
	wMap.Clock.SimTime = wMap.Clock.SimTime.Add(time.Second)
	cMap.State.V = cMap.State.V.Scale(1.05)
	mapOut := strings.Join(mapHUD.buildNavigationBox(wMap), "\n") // frame 2
	if !strings.Contains(mapOut, "↑") {
		t.Fatalf("setup: the map's own NAVIGATION box shows no climbing trend arrow for a growing apoapsis:\n%s", mapOut)
	}

	// LAUNCH pipeline: same two frames, through the real Render path,
	// which builds NAVIGATION twice internally each frame.
	launchHUD := NewOrbitView(th)
	v := NewLaunchView(th, launchHUD)
	wLaunch, cLaunch := ascentTrendFixture(t)
	v.Resize(DesignWidth, DesignHeight)
	v.Render(wLaunch, DesignWidth, DesignHeight) // frame 1, establishes the baseline
	wLaunch.Clock.SimTime = wLaunch.Clock.SimTime.Add(time.Second)
	cLaunch.State.V = cLaunch.State.V.Scale(1.05)
	launchOut := v.Render(wLaunch, DesignWidth, DesignHeight) // frame 2

	if !strings.Contains(launchOut, "↑") {
		t.Errorf("LAUNCH view shows no climbing trend arrow even though the map shows one for the same instant")
	}
}

// TestDrawAscentAirScaleLabelClearsLeftStack (review finding 4,
// 2026-09-25): airScaleColumnBound only ever cleared NAVIGATION and
// TARGET on the right; nothing clears PROPELLANT or GUIDANCE on the
// left, so a 17-cell max-Q label ("10.00 km (max Q) ") starting close to
// a narrow marker column can begin well inside the left stack's own
// boxes. The label must not paint left of leftBound; when the full label
// does not fit, it shortens to the bare "(max Q) " marker (the glyph
// already says max Q; the altitude number is in the F1 glossary) rather
// than spilling over.
func TestDrawAscentAirScaleLabelClearsLeftStack(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	v.Resize(120, 40)
	v.canvas.Clear()
	qb := sim.AscentQBand{
		AtmosphereDepthM: 150_000,
		CurrentAltM:      10_000,
		MaxQAltM:         10_000, // current == max-Q row: the worst-case combined label
		HasMaxQ:          true,
	}
	const markerCol = 60  // close enough to leftBound that the full 17-cell label would cross it
	const leftBound = 50
	v.drawAscentAirScale(qb, markerCol, 1, leftBound)

	isBlank := func(r rune) bool { return r == ' ' || r == '⠀' }

	row := 1 + qBandRowIndex(qb.CurrentAltM, qb.AtmosphereDepthM, airScaleRows)
	if got := canvasCellRuneAt(t, v.canvas.String(), row, leftBound-1); !isBlank(got) {
		t.Errorf("column %d (one left of leftBound %d) = %q, want blank: the label reached past the left bound", leftBound-1, leftBound, string(got))
	}
	// The shortened marker itself must still be present between the
	// bound and the marker column, or the fix dropped the label
	// entirely instead of shortening it.
	found := false
	for c := leftBound; c < markerCol; c++ {
		if !isBlank(canvasCellRuneAt(t, v.canvas.String(), row, c)) {
			found = true
			break
		}
	}
	if !found {
		t.Error("no label text at all between leftBound and markerCol, want the shortened (max Q) marker")
	}
}
