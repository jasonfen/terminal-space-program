package widgets

import (
	"sort"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// runsAndGaps takes the x-coordinates of tagged pixels along a single
// horizontal row (sorted ascending, no duplicates) and returns the length of
// each contiguous run of set pixels and each gap between runs — the shape a
// dash pattern (contiguous "on" pixels separated by "off" pixels) leaves
// behind, as opposed to dotted's isolated single-pixel runs or solid's one
// single run covering everything.
func runsAndGaps(xs []int) (runs, gaps []int) {
	if len(xs) == 0 {
		return nil, nil
	}
	xs = append([]int(nil), xs...)
	sort.Ints(xs)
	runStart := xs[0]
	prev := xs[0]
	for i := 1; i < len(xs); i++ {
		if xs[i] == prev+1 {
			prev = xs[i]
			continue
		}
		runs = append(runs, prev-runStart+1)
		gaps = append(gaps, xs[i]-prev-1)
		runStart = xs[i]
		prev = xs[i]
	}
	runs = append(runs, prev-runStart+1)
	return runs, gaps
}

// classChordXs draws a long horizontal world-space chord through a class's
// polyline path and returns the tagged x-coordinates on the row it lands on
// — the fixture every pattern-per-class test below measures.
func classChordXs(t *testing.T, class LineClass, scale float64) (xs []int, row int) {
	t.Helper()
	color := lipgloss.Color("#33AAFF")
	c := NewCanvas(200, 60) // 400 x 240 px
	c.SetScale(scale)
	c.Center(orbital.Vec3{})
	c.Clear()
	half := 150.0 / scale
	c.PlotPolylineClass([]orbital.Vec3{{X: -half}, {X: half}}, color, class)
	px := taggedPixels(c, color)
	if len(px) == 0 {
		t.Fatalf("class %d chord inked nothing at scale %v", class, scale)
	}
	row = px[0][1]
	for _, p := range px {
		if p[1] != row {
			t.Fatalf("chord ink spans more than one row (%d and %d) — not a straight horizontal test chord", row, p[1])
		}
		xs = append(xs, p[0])
	}
	return xs, row
}

// TestClassRealIsContiguous: ClassReal (a live orbit — "SOLID bright" /
// "SOLID dim") inks every pixel along its run, i.e. exactly one run with no
// internal gaps — the "contiguous pixel run" ADR 0041 §2 describes.
func TestClassRealIsContiguous(t *testing.T) {
	xs, _ := classChordXs(t, ClassReal, 1)
	runs, gaps := runsAndGaps(xs)
	if len(runs) != 1 {
		t.Fatalf("ClassReal chord has %d runs (%v) with gaps %v — want one contiguous run", len(runs), runs, gaps)
	}
	if want := len(xs); runs[0] != want {
		t.Errorf("ClassReal run length %d, want %d (every pixel of the chord)", runs[0], want)
	}
}

// TestClassPlannedHasDashPattern: ClassPlanned (a plan's before-it-fires
// consequence — node legs, predictions, encounter arcs) inks a genuine dash:
// repeated runs of classPlannedDashPx pixels separated by gaps of
// classPlannedGapPx pixels. This is the pattern that didn't exist anywhere
// in the codebase before ADR 0041 — every "dashed" call site was really
// sparse single-pixel dotting.
func TestClassPlannedHasDashPattern(t *testing.T) {
	xs, _ := classChordXs(t, ClassPlanned, 1)
	runs, gaps := runsAndGaps(xs)
	if len(runs) < 5 {
		t.Fatalf("ClassPlanned chord has only %d runs — want a repeated dash pattern", len(runs))
	}
	// Endpoints can clip a run short; check the interior only.
	for i := 1; i < len(runs)-1; i++ {
		if runs[i] != classPlannedDashPx {
			t.Errorf("dash run %d has length %d, want %d", i, runs[i], classPlannedDashPx)
		}
	}
	for i, g := range gaps {
		if g != classPlannedGapPx {
			t.Errorf("gap %d has length %d, want %d", i, g, classPlannedGapPx)
		}
	}
}

// TestClassSceneryIsSparseDots: ClassScenery (backdrop — body orbits, the
// SOI Ring) inks isolated single-pixel dots, sparser than a dash: every run
// has length 1, separated by classSceneryNearSpacingPx-1 px gaps.
func TestClassSceneryIsSparseDots(t *testing.T) {
	xs, _ := classChordXs(t, ClassScenery, 1)
	runs, gaps := runsAndGaps(xs)
	if len(runs) < 5 {
		t.Fatalf("ClassScenery chord has only %d runs — want repeated sparse dots", len(runs))
	}
	for i, r := range runs {
		if r != 1 {
			t.Errorf("scenery run %d has length %d, want 1 (a lone dot)", i, r)
		}
	}
	wantGap := classSceneryNearSpacingPx - 1
	for i, g := range gaps {
		if g != wantGap {
			t.Errorf("scenery gap %d has length %d, want %d", i, g, wantGap)
		}
	}
}

// TestClassInkDensityOrdering: the three classes' ink density over an
// identical chord orders SOLID > DASHED > DOTTED — the class vocabulary's
// whole premise is that this ordering is learnable at a glance regardless of
// color.
func TestClassInkDensityOrdering(t *testing.T) {
	real, _ := classChordXs(t, ClassReal, 1)
	planned, _ := classChordXs(t, ClassPlanned, 1)
	scenery, _ := classChordXs(t, ClassScenery, 1)
	if !(len(real) > len(planned) && len(planned) > len(scenery)) {
		t.Errorf("ink counts real=%d planned=%d scenery=%d — want real > planned > scenery",
			len(real), len(planned), len(scenery))
	}
}

// TestDashCadenceZoomInvariant: the dash on/off run lengths are a canvas-
// pixel quantity, not a world-space or sample-index one, so the same class
// chord dashes identically at 1x, 40x and 1000x zoom (mirroring
// TestEllipseDotSpacingIsZoomInvariant for the new dash pattern).
func TestDashCadenceZoomInvariant(t *testing.T) {
	for _, scale := range []float64{1, 40, 1000} {
		xs, _ := classChordXs(t, ClassPlanned, scale)
		runs, gaps := runsAndGaps(xs)
		if len(runs) < 5 {
			t.Fatalf("scale %v: only %d dash runs — cadence not holding at this zoom", scale, len(runs))
		}
		for i := 1; i < len(runs)-1; i++ {
			if runs[i] != classPlannedDashPx {
				t.Errorf("scale %v: dash run %d has length %d, want %d", scale, i, runs[i], classPlannedDashPx)
			}
		}
		for i, g := range gaps {
			if g != classPlannedGapPx {
				t.Errorf("scale %v: gap %d has length %d, want %d", scale, i, g, classPlannedGapPx)
			}
		}
	}
}

// TestPlotPolylineClassCarriesPhaseAcrossPoints: a many-point polyline (the
// realistic shape of a node leg or encounter arc, sampled at sub-dash
// spacing) must still show the dash cadence rather than restarting the
// on/off cycle — and therefore reading solid — at every sample, mirroring
// TestPlotDensePolylineCarriesDashPhase for the new dash primitive.
func TestPlotPolylineClassCarriesPhaseAcrossPoints(t *testing.T) {
	color := lipgloss.Color("#33AAFF")
	c := NewCanvas(60, 30) // 120 x 120 px
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()

	pts := make([]orbital.Vec3, 0, 81)
	for i := -40; i <= 40; i++ {
		pts = append(pts, orbital.Vec3{X: float64(i)})
	}
	c.PlotPolylineClass(pts, color, ClassPlanned)

	xs := []int{}
	for _, p := range taggedPixels(c, color) {
		xs = append(xs, p[0])
	}
	runs, _ := runsAndGaps(xs)
	if len(runs) < 5 {
		t.Fatalf("only %d dash runs over an 81-sample polyline — phase not carrying across points", len(runs))
	}
	for i := 1; i < len(runs)-1; i++ {
		if runs[i] != classPlannedDashPx {
			t.Errorf("dash run %d has length %d, want %d — a per-sample phase restart would read solid", i, runs[i], classPlannedDashPx)
		}
	}
}

// TestDrawEllipseClassRealMatchesFarSideDashedAtSpacingOne: DrawEllipseClass
// is a thin dispatcher over the already-tested drawEllipseAdaptive engine —
// this pins the dispatch itself: ClassReal must draw identically to calling
// DrawEllipseOffsetFarSideDashed at the documented Real near-spacing (1px,
// genuinely contiguous), not merely "close to it."
func TestDrawEllipseClassRealMatchesFarSideDashedAtSpacingOne(t *testing.T) {
	el := orbital.Elements{A: 5e6, E: 0.2}
	color := lipgloss.Color("#33AAFF")

	viaClass := NewCanvas(80, 24)
	viaClass.FitTo(el.A * 1.3)
	viaClass.Clear()
	viaClass.DrawEllipseClass(el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, color)

	viaSpacing := NewCanvas(80, 24)
	viaSpacing.FitTo(el.A * 1.3)
	viaSpacing.Clear()
	viaSpacing.DrawEllipseOffsetFarSideDashed(el, orbital.Vec3{}, 360, classRealNearSpacingPx, orbital.Vec3{}, 0, color)

	a, b := taggedPixels(viaClass, color), taggedPixels(viaSpacing, color)
	if len(a) == 0 || len(b) == 0 {
		t.Fatalf("no ink drawn: DrawEllipseClass=%d DrawEllipseOffsetFarSideDashed=%d", len(a), len(b))
	}
	if len(a) != len(b) {
		t.Errorf("DrawEllipseClass(ClassReal) inked %d pixels, DrawEllipseOffsetFarSideDashed(spacing=%d) inked %d — dispatch drifted from its documented mapping",
			len(a), classRealNearSpacingPx, len(b))
	}
}
