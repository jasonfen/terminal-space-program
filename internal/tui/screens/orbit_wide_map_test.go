package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
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

// wideMapFixtures are the four maps the grill's acceptance names.
var wideMapFixtures = []struct {
	system     string
	cols, rows int
}{
	{"Sol", 140, 40}, {"Sol", 181, 49}, {"Lumen", 140, 40}, {"Lumen", 181, 49},
}

func labelNames(v *OrbitView) []string {
	var out []string
	for _, l := range v.nameLabels {
		out = append(out, l.Name)
	}
	return out
}

// TestWideMapLabelsNeverOverlap: every placed name occupies cells no other
// name, body disk, instrument box, navball panel or the bottom row occupies,
// and it survives into the finished frame intact (nothing composited later
// covers it). The disk/marker half reads the canvas as it stood BEFORE the
// names were stamped, by re-rendering with names suppressed (declutter is not
// used, so the seam is a second view with the pass disabled via its target).
func TestWideMapLabelsNeverOverlap(t *testing.T) {
	for _, fx := range wideMapFixtures {
		v, w, out := wideMapRender(t, fx.system, fx.cols, fx.rows)
		if len(v.nameLabels) == 0 {
			t.Fatalf("%s %dx%d: no names placed, the overlap check would be vacuous", fx.system, fx.cols, fx.rows)
		}
		cRows := v.canvas.Rows()
		seen := map[[2]int]string{}
		lines := strings.Split(stripANSI(out), "\n")
		for _, l := range v.nameLabels {
			n := len([]rune(l.Name))
			if l.Row >= cRows-1 {
				t.Errorf("%s %dx%d: %s on the bottom row %d", fx.system, fx.cols, fx.rows, l.Name, l.Row)
			}
			// Intact in the finished frame: boxes/navball composited after
			// the canvas would have overwritten part of it.
			rs := []rune(lines[2+l.Row])
			if got := string(rs[1+l.Col : 1+l.Col+n]); got != l.Name {
				t.Errorf("%s %dx%d: %s at (%d,%d) reads %q in the finished frame (covered by a box or marker)", fx.system, fx.cols, fx.rows, l.Name, l.Col, l.Row, got)
			}
			for _, o := range v.nameLabels {
				if o.Name != l.Name && o.Row >= l.Row-1 && o.Row <= l.Row+1 &&
					o.Col <= l.Col+n && l.Col <= o.Col+len([]rune(o.Name)) {
					t.Errorf("%s %dx%d: %s and %s touch (names keep a cell between them)", fx.system, fx.cols, fx.rows, l.Name, o.Name)
				}
			}
			for c := l.Col; c < l.Col+n; c++ {
				k := [2]int{c, l.Row}
				if prev, dup := seen[k]; dup {
					t.Errorf("%s %dx%d: %s and %s share cell %v", fx.system, fx.cols, fx.rows, prev, l.Name, k)
				}
				seen[k] = l.Name
				// chipRects are screen cells (canvas col+1, row+2).
				for _, r := range v.chipRects {
					if c+1 >= r.colStart && c+1 <= r.colEnd && l.Row+2 >= r.rowStart && l.Row+2 <= r.rowEnd {
						t.Errorf("%s %dx%d: %s overlaps instrument box %q", fx.system, fx.cols, fx.rows, l.Name, r.id)
					}
				}
			}
		}
		// Body disks: no name covers the cell any body projects to.
		for _, b := range w.System().Bodies {
			px, py, ok := v.canvas.Project(w.BodyPosition(b))
			if !ok {
				continue
			}
			if who, hit := seen[[2]int{px / 2, py / 4}]; hit {
				t.Errorf("%s %dx%d: name %s covers %s's cell", fx.system, fx.cols, fx.rows, who, b.EnglishName)
			}
		}
	}
}

// TestWideMapLabelCounts pins how many bodies are named on each fixture,
// measured on this branch (not copied from the grill mocks): the star and
// planets only, never a moon, and the ones the grill expects to read are
// always among them.
func TestWideMapLabelCounts(t *testing.T) {
	cases := []struct {
		system     string
		cols, rows int
		want       int
		mustHave   []string
	}{
		{"Sol", 140, 40, 6, []string{"Sun", "Mars", "Jupiter", "Saturn", "Uranus"}},
		{"Sol", 181, 49, 7, []string{"Sun", "Jupiter", "Saturn", "Uranus", "Neptune"}},
		{"Lumen", 140, 40, 7, []string{"Lumen", "Kern", "Cache"}},
		{"Lumen", 181, 49, 7, []string{"Lumen", "Rust", "Cache"}},
	}
	for _, c := range cases {
		v, w, _ := wideMapRender(t, c.system, c.cols, c.rows)
		got := labelNames(v)
		if len(got) != c.want {
			t.Errorf("%s %dx%d: %d names %v, want %d", c.system, c.cols, c.rows, len(got), got, c.want)
		}
		have := map[string]bool{}
		for _, n := range got {
			have[n] = true
		}
		for _, n := range c.mustHave {
			if !have[n] {
				t.Errorf("%s %dx%d: %s is not named (placed %v, dropped %v)", c.system, c.cols, c.rows, n, got, v.nameDropped)
			}
		}
		for _, b := range w.System().Bodies {
			if b.BodyType == "Moon" && have[b.EnglishName] {
				t.Errorf("%s %dx%d: moon %s is named at system zoom", c.system, c.cols, c.rows, b.EnglishName)
			}
		}
	}
}

// TestWideMapNamesOnlyAtSystemFocusExceptTarget: with the camera on a body
// the standing names go away; the Target body keeps its name at every zoom.
func TestWideMapNamesOnlyAtSystemFocusExceptTarget(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.ViewMode = sim.ViewTilted
	earthIdx, moonIdx := -1, -1
	for i, b := range w.System().Bodies {
		switch b.EnglishName {
		case "Earth":
			earthIdx = i
		case "Moon":
			moonIdx = i
		}
	}
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: earthIdx}
	v := NewOrbitView(plainTheme())
	v.Resize(140, 40)
	v.Render(w, 0, 140, 40)
	if len(v.nameLabels) != 0 {
		t.Errorf("focused on Earth with no target: names %v, want none", labelNames(v))
	}
	w.SetTargetBody(moonIdx)
	// Pull back until the Moon is on the canvas so the check can see it.
	named := false
	for i := 0; i < 30 && !named; i++ {
		v.Render(w, 0, 140, 40)
		for _, n := range labelNames(v) {
			named = named || n == "Moon"
			if n != "Moon" {
				t.Errorf("only the Target is named off System focus, got %q", n)
			}
		}
		v.ZoomOut()
	}
	if !named {
		t.Errorf("targeted Moon never named while on the canvas (dropped: %v)", v.nameDropped)
	}
}

// TestWideMapNamesHiddenByDeclutter: F2 clears standing overlays, names too.
func TestWideMapNamesHiddenByDeclutter(t *testing.T) {
	v, w, _ := wideMapRender(t, "Sol", 140, 40)
	if len(v.nameLabels) == 0 {
		t.Fatal("precondition: names expected without declutter")
	}
	v.declutter = true
	v.Render(w, 0, 140, 40)
	if len(v.nameLabels) != 0 {
		t.Errorf("declutter on: names %v, want none", labelNames(v))
	}
}

// TestWideMapNameColourIsDimBodyColour: a name is drawn in its body's
// palette colour pulled toward grey, not the terminal default and not a
// third grey. Needs a forced colour profile.
func TestWideMapNameColourIsDimBodyColour(t *testing.T) {
	forceTrueColor(t)
	v, w, out := wideMapRender(t, "Sol", 140, 40)
	_ = v
	for _, b := range w.System().Bodies {
		if b.EnglishName != "Jupiter" {
			continue
		}
		// Independent of dimBodyColor: 70% of the palette colour + 30% of #5F.
		var r, g, bl int
		if _, err := fmt.Sscanf(string(render.ColorFor(b)), "#%02X%02X%02X", &r, &g, &bl); err != nil {
			t.Fatalf("palette colour %q: %v", render.ColorFor(b), err)
		}
		r, g, bl = int(float64(r)*0.7+28.5), int(float64(g)*0.7+28.5), int(float64(bl)*0.7+28.5)
		hex := fmt.Sprintf("#%02X%02X%02X", r, g, bl)
		want := fmt.Sprintf("38;2;%d;%d;%d", r, g, bl)
		// The frame styles cell by cell, so each letter carries its own
		// colour escape: every letter of the name must be in the dim colour.
		for _, ch := range "Jupiter" {
			if !strings.Contains(out, want+"m"+string(ch)) {
				t.Errorf("Jupiter's %q is not drawn in %s (%s)", ch, hex, want)
			}
		}
	}
}

// TestWideMapNeverNamesOnTheBottomRow: the bottom canvas row carries the view
// label and Hint Strip. Pan the system so bodies sweep across it and check no
// name ever lands there, with a positive control that a body really did sit
// on that row (otherwise the sweep proves nothing).
func TestWideMapNeverNamesOnTheBottomRow(t *testing.T) {
	v, w, _ := wideMapRender(t, "Sol", 140, 40)
	last := v.canvas.Rows() - 1
	sawBodyOnBottom := false
	// Pan both ways: X slides the Tilted map vertically, Y horizontally. The
	// Hint Strip fills the left of the bottom row, so the control needs a
	// body on the bottom row in the free stretch to its right.
	for _, ky := range []int{20, 40, 60} {
		for k := -90; k <= -20; k++ {
			v.panOffset = orbital.Vec3{X: float64(k) * 1.5e11, Y: float64(-ky) * 1.5e11}
			v.Render(w, 0, 140, 40)
			for _, b := range w.System().Bodies {
				if b.BodyType == "Moon" {
					continue
				}
				if px, py, ok := v.canvas.Project(w.BodyPosition(b)); ok && py/4 == last && px/2 > 100 {
					sawBodyOnBottom = true
				}
			}
			for _, l := range v.nameLabels {
				if l.Row == last {
					t.Fatalf("pan (%d,%d): %s named on the bottom row", k, ky, l.Name)
				}
			}
		}
	}
	if !sawBodyOnBottom {
		t.Error("positive control: no body ever sat on the bottom row during the sweep")
	}
}
