package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// UX cycle 3, slice B9 (#506, grill G7): the wide map ("g", System-wide
// focus) names the star and planets, draws the star at its honest size, and
// lets each planet wear its own glyph instead of its moons'.

// wideMapRender builds the named system, focuses the whole system (what "g"
// does: World.ResetFocus) in the default Tilted view, and renders one frame
// at the given terminal size through the production Render path.
func wideMapRender(t *testing.T, system string, cols, rows int) (*OrbitView, *sim.World, string) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for i := 0; i < len(w.Systems) && w.Systems[w.SystemIdx].Name != system; i++ {
		w.CycleSystem()
	}
	if w.Systems[w.SystemIdx].Name != system {
		t.Fatalf("could not browse to %s (stuck on %q)", system, w.Systems[w.SystemIdx].Name)
	}
	w.ViewMode = sim.ViewTilted
	w.ResetFocus()
	v := NewOrbitView(Theme{
		Primary: lipgloss.NewStyle(), Warning: lipgloss.NewStyle(),
		Dim: lipgloss.NewStyle(), HUDBox: lipgloss.NewStyle(),
	})
	v.Resize(cols, rows)
	return v, w, v.Render(w, 0, cols, rows)
}

// canvasRuneAt reads the rune at canvas cell (col, row) out of a rendered
// frame: line 0 is the title bar, line 1 the panel's top border, and each
// canvas line starts after the panel's left border cell.
func canvasRuneAt(out string, col, row int) rune {
	lines := strings.Split(stripANSI(out), "\n")
	if 2+row >= len(lines) {
		return 0
	}
	rs := []rune(lines[2+row])
	if 1+col >= len(rs) {
		return 0
	}
	return rs[1+col]
}

// bodyCell is the canvas cell a named body projects to.
func bodyCell(t *testing.T, v *OrbitView, w *sim.World, name string) (int, int) {
	t.Helper()
	for _, b := range w.System().Bodies {
		if b.EnglishName == name {
			px, py, ok := v.canvas.Project(w.BodyPosition(b))
			if !ok {
				t.Fatalf("%s is off the canvas at the g fit", name)
			}
			return px / 2, py / 4
		}
	}
	t.Fatalf("no body %q in %s", name, w.System().Name)
	return 0, 0
}

// TestWideMapPlanetGlyphsNotOverwrittenByMoons: at the "g" fit a planet's
// moons project to the planet's own cell (their orbits are well under 1 px),
// and the moons draw after their planet, so the last glyph write used to win
// and Jupiter read as a hollow moon circle. Each planet must read as itself:
// Jupiter (gas giant) a fisheye, Mars (terrestrial) a solid dot.
func TestWideMapPlanetGlyphsNotOverwrittenByMoons(t *testing.T) {
	v, w, out := wideMapRender(t, "Sol", 140, 40)
	for _, c := range []struct {
		name string
		want rune
	}{{"Jupiter", '◉'}, {"Saturn", '◉'}, {"Mars", '●'}} {
		col, row := bodyCell(t, v, w, c.name)
		if got := canvasRuneAt(out, col, row); got != c.want {
			t.Errorf("%s cell (%d,%d) reads %q, want %q (a moon's glyph overwrote the planet's)", c.name, col, row, got, c.want)
		}
	}
}

// coronaOnScreen reports whether any corona-coloured ink is in the frame.
// Needs a forced colour profile: under `go test` lipgloss is colourless and
// the check would be vacuous.
func coronaOnScreen(out string) bool {
	return strings.Contains(out, "38;2;255;224;112") // render.ColorSunCorona #FFE070
}

func forceTrueColor(t *testing.T) {
	t.Helper()
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })
}

// TestWideMapStarDrawnAtHonestSize: at the "g" fit the Sun's true radius is
// a hundredth of a pixel, so it draws at the 2 px floor, not the old 6 px
// star fallback that swallowed Mercury..Mars (Earth's whole orbit is ~2 px
// across), and no corona ring is drawn over the inner orbits.
func TestWideMapStarDrawnAtHonestSize(t *testing.T) {
	forceTrueColor(t)
	v, w, out := wideMapRender(t, "Sol", 140, 40)
	sun := w.System().Bodies[0]
	if got := mapBodyPixelRadius(sun, true, v.canvas.Scale(), 1000); got > 2 {
		t.Errorf("Sun draws %d px at the g fit, want <= 2", got)
	}
	if coronaOnScreen(out) {
		t.Error("corona ink on screen at the g fit while Mercury..Mars orbit inside it")
	}
}

// TestWideMapZoomInNeverBringsCoronaOverOrbits: stepping the player's zoom in
// from the g fit, a corona ring may appear only once its outer ring is
// clear of the innermost orbit's periapsis on screen. Walks the zoom through
// the real Render so the radius and the gate both come from production.
func TestWideMapZoomInNeverBringsCoronaOverOrbits(t *testing.T) {
	forceTrueColor(t)
	v, w, _ := wideMapRender(t, "Sol", 140, 40)
	sun := w.System().Bodies[0]
	mercury := w.System().Bodies[1]
	peri := mercury.SemimajorAxisMeters() * (1 - mercury.Eccentricity)
	sawCorona := false
	for step := 0; step < 40; step++ {
		out := v.Render(w, 0, 140, 40)
		scale := v.canvas.Scale()
		r := mapBodyPixelRadius(sun, true, scale, v.canvas.Cols()*2+v.canvas.Rows()*4)
		if coronaOnScreen(out) {
			sawCorona = true
			if outer, inner := int(float64(r)*1.8), peri*scale; float64(outer) >= inner {
				t.Fatalf("zoom step %d: corona ring (outer %d px) drawn while Mercury's periapsis is %.1f px out", step, outer, inner)
			}
		}
		v.ZoomIn()
	}
	// Positive control: the detector must be able to see a corona at all, or
	// the loop above proves nothing.
	if !sawCorona {
		t.Error("no corona seen at any zoom step; the detector or the gate is wrong")
	}
}
