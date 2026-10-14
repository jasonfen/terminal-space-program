package screens

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Issue #639: the launch view's sky and ground are a tinted backdrop over the
// WHOLE canvas (a per-cell background), with every dot drawn on top.

func backdropTrueColour(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func hexRGB(t *testing.T, c lipgloss.Color) (r, g, b int) {
	t.Helper()
	var ok bool
	if r, g, b, ok = parseHexColor(string(c)); !ok {
		t.Fatalf("not a #RRGGBB colour: %q", c)
	}
	return
}

func brightness(t *testing.T, c lipgloss.Color) int {
	r, g, b := hexRGB(t, c)
	return r + g + b
}

// renderedLaunch renders the launch view and returns it for canvas inspection.
func renderedLaunch(t *testing.T, w *sim.World) *LaunchView {
	t.Helper()
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	v.Render(w, DesignWidth, DesignHeight)
	return v
}

func earthGroundTint(t *testing.T, w *sim.World) lipgloss.Color {
	t.Helper()
	return darkenHex(w.ActiveCraft().Primary.SurfaceColorHex(), groundTintDarkenFrac)
}

// horizonRow is the first ground-tinted cell row down the middle column.
func horizonRow(v *LaunchView, ground lipgloss.Color) int {
	mid := v.canvas.Cols() / 2
	for r := 0; r < v.canvas.Rows(); r++ {
		if v.canvas.CellBackground(mid, r) == ground {
			return r
		}
	}
	return -1
}

func TestLaunchSkyFillsFromTheHorizonToTheTopOfTheCanvas(t *testing.T) {
	w, _ := spawnSaturnVOnPad(t)
	v := renderedLaunch(t, w)
	ground := earthGroundTint(t, w)
	h := horizonRow(v, ground)
	if h < 8 {
		t.Fatalf("horizon at cell row %d; want well below the old 3-cell sky band", h)
	}
	for col := 0; col < v.canvas.Cols(); col++ {
		for r := 0; r < h-1; r++ { // the row touching the horizon may straddle it
			if bg := v.canvas.CellBackground(col, r); bg == "" || bg == ground {
				t.Fatalf("cell (%d,%d) above the horizon (row %d) has bg %q, want sky tint", col, r, h, bg)
			}
		}
	}
}

func TestLaunchGroundFillsFromTheHorizonToTheBottomOfTheCanvas(t *testing.T) {
	w, _ := spawnSaturnVOnPad(t)
	v := renderedLaunch(t, w)
	ground := earthGroundTint(t, w)
	h := horizonRow(v, ground)
	last := v.canvas.Rows() - 1
	for col := 0; col < v.canvas.Cols(); col++ {
		for r := h + 1; r <= last; r++ {
			if bg := v.canvas.CellBackground(col, r); bg != ground {
				t.Fatalf("cell (%d,%d) below the horizon has bg %q, want ground %q (old code stopped 2.5 cells down)", col, r, bg, ground)
			}
		}
	}
}

func TestLaunchSkyIsDeeperOverheadThanAtTheHorizon(t *testing.T) {
	w, _ := spawnSaturnVOnPad(t)
	v := renderedLaunch(t, w)
	h := horizonRow(v, earthGroundTint(t, w))
	mid := v.canvas.Cols() / 2
	near, top := v.canvas.CellBackground(mid, h-1), v.canvas.CellBackground(mid, 0)
	if brightness(t, top) >= brightness(t, near) {
		t.Errorf("top of the sky %s is not darker than the horizon haze %s", top, near)
	}
	if r, _, b := hexRGB(t, top); b <= r {
		t.Errorf("top of the sky %s should still read blue", top)
	}
}

func TestLaunchSkyFadesToBlackWithAltitudeAndIsGoneAtTheCutoff(t *testing.T) {
	prev := 1 << 30
	for _, alt := range []float64{200, 30_000, 75_000, 120_000} {
		w := ascendingCraftWorld(t, "earth", alt, 0, orbital.Vec3{X: 1})
		v := renderedLaunch(t, w)
		bg := v.canvas.CellBackground(v.canvas.Cols()/2, 0)
		b := 0
		if bg != "" {
			b = brightness(t, bg)
		}
		if b >= prev {
			t.Errorf("at %.0f m the overhead sky (%q, brightness %d) is not darker than at the previous altitude (%d)", alt, bg, b, prev)
		}
		prev = b
	}
	// Past the cutoff (150 km): space is black, no sky tint on any cell above the planet.
	w := ascendingCraftWorld(t, "earth", 160_000, 0, orbital.Vec3{X: 1})
	v := renderedLaunch(t, w)
	ground := earthGroundTint(t, w)
	for col := 0; col < v.canvas.Cols(); col++ {
		for r := 0; r < v.canvas.Rows(); r++ {
			if bg := v.canvas.CellBackground(col, r); bg != "" && bg != ground {
				t.Fatalf("cell (%d,%d) at 160 km has sky tint %q; the sky must be black past the cutoff", col, r, bg)
			}
		}
	}
}

func TestLaunchAirlessBodyHasGroundButNoSky(t *testing.T) {
	w := ascendingCraftWorld(t, "moon", 100, 0, orbital.Vec3{X: 1})
	v := renderedLaunch(t, w)
	ground := earthGroundTint(t, w)
	for col := 0; col < v.canvas.Cols(); col++ {
		if bg := v.canvas.CellBackground(col, 0); bg != "" {
			t.Fatalf("airless body has sky tint %q at the top of column %d", bg, col)
		}
	}
	if bg := v.canvas.CellBackground(v.canvas.Cols()/2, v.canvas.Rows()-1); bg != ground {
		t.Errorf("airless pad bottom cell bg = %q, want ground %q", bg, ground)
	}
}

var sgrOpenRE = regexp.MustCompile("\x1b\\[([0-9;]*)m([^\x1b]*)")

// arcCellsOverSky counts the cells rendered with the arc colour as their
// foreground and a sky (not ground, not empty) background.
func arcCellsOverSky(out string, arc, ground lipgloss.Color) int {
	rgb := func(c lipgloss.Color) (int, int, int) {
		r, _ := strconv.ParseInt(string(c)[1:3], 16, 32)
		g, _ := strconv.ParseInt(string(c)[3:5], 16, 32)
		b, _ := strconv.ParseInt(string(c)[5:7], 16, 32)
		return int(r), int(g), int(b)
	}
	ar, ag, ab := rgb(arc)
	gr, gg, gb := rgb(ground)
	fg := fmt.Sprintf("38;2;%d;%d;%d", ar, ag, ab)
	gbg := fmt.Sprintf("48;2;%d;%d;%d", gr, gg, gb)
	n := 0
	for _, m := range sgrOpenRE.FindAllStringSubmatch(out, -1) {
		if strings.Contains(m[1], fg) && strings.Contains(m[1], "48;2;") && !strings.Contains(m[1], gbg) {
			n += len([]rune(m[2]))
		}
	}
	return n
}

// The reason for the tint rather than a dot fill (#639): a dashed ascent arc
// keeps its own cells over the sky. With the sky as dots the majority rule
// handed those cells to the sky.
func TestAscentArcStaysVisibleInItsOwnColourOverTheSky(t *testing.T) {
	backdropTrueColour(t)
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	v.Resize(DesignWidth, DesignHeight)
	var earth bodies.CelestialBody
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range w.System().Bodies {
		if b.ID == "earth" {
			earth = b
		}
	}
	const mPerPx = 50.0
	v.canvas.Clear()
	v.canvas.SetScale(1 / mPerPx)
	v.canvas.Center(orbital.Vec3{Y: earth.RadiusMeters() + 2000})
	v.drawHorizonAndFill(earth, orbital.Vec3{}, 2000)
	var path []orbital.Vec3
	for i := 0; i <= 60; i++ {
		f := float64(i) / 60
		path = append(path, orbital.Vec3{X: 300 * f * mPerPx, Y: earth.RadiusMeters() + 2000 + 70*f*mPerPx*4})
	}
	v.drawAscentArc(orbital.Vec3{}, sim.AscentPath{Path: path})
	ground := darkenHex(earth.SurfaceColorHex(), groundTintDarkenFrac)
	if n := arcCellsOverSky(v.canvas.String(), render.ColorPredictedPath, ground); n < 20 {
		t.Errorf("only %d cells show the arc in its colour over the sky, want it drawn across the climb", n)
	}
}
