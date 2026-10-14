package widgets

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// trueColour forces 24-bit output for the test: lipgloss renders no escapes
// under `go test` otherwise and every colour/background assertion would be
// vacuous.
func trueColour(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// noBackdropScene is a deterministic frame that exercises every String()
// branch that exists without a background layer: a backdrop disk, solid and
// sparse lines, a pinned-colour label, a plain overlay glyph and untagged
// dots.
func noBackdropScene() *Canvas {
	c := NewCanvas(40, 12)
	c.SetScale(1)
	c.Clear()
	c.FillColoredDiskTagged(orbital.Vec3{X: -10}, 9, CellTag{Color: "#6B8E4E", BodyID: "x", Backdrop: true})
	c.PlotDenseLineColored(orbital.Vec3{X: -30, Y: 3}, orbital.Vec3{X: 30, Y: -5}, "#E8C547", 1)
	c.PlotDenseLineColored(orbital.Vec3{X: -30, Y: -12}, orbital.Vec3{X: 30, Y: 12}, "#3DDC84", 3)
	c.PlotColoredTagged(orbital.Vec3{X: 20, Y: 15}, CellTag{Color: "#FF0000", IsVessel: true})
	c.SetCellLabelColored(2, 1, "label", lipgloss.Color("#9DC8FF"))
	c.SetCellOverlay(orbital.Vec3{X: 14, Y: 0}, '@')
	c.dc.Set(3, 40)
	return c
}

// TestNoBackdropCanvasIsByteIdenticalToBeforeTheBackdropLayer pins the exact
// bytes a canvas with no background set produced BEFORE #639 added the layer
// (hash taken from main at 1c8b97c). Every other screen draws on this canvas;
// none may gain or lose a byte.
func TestNoBackdropCanvasIsByteIdenticalToBeforeTheBackdropLayer(t *testing.T) {
	trueColour(t)
	out := noBackdropScene().String()
	if len(out) < 200 {
		t.Fatalf("scene rendered almost nothing (%d bytes); fixture is broken", len(out))
	}
	sum := sha256.Sum256([]byte(out))
	got := hex.EncodeToString(sum[:])
	const want = "9f874cbc34de95b047c5883dff9482feaf91c38506f8a54a43090d8047e17068"
	if got != want {
		t.Errorf("no-backdrop output changed: sha256 %s, want %s\n%q", got, want, out)
	}
}

// opens counts SGR open sequences (everything but the reset) in s.
func opens(s string) int {
	return strings.Count(s, "\x1b[") - strings.Count(s, "\x1b[0m")
}

const (
	bgA    = lipgloss.Color("#102030") // 16;32;48
	bgB    = lipgloss.Color("#304050") // 48;64;80
	sgrBgA = "48;2;16;32;48"
	sgrBgB = "48;2;48;64;80"
)

func TestBackgroundOnlyCellRendersASpaceWithThatBackground(t *testing.T) {
	trueColour(t)
	c := NewCanvas(10, 5)
	c.Clear()
	c.SetCellBackground(3, 2, bgA)
	out := c.String()
	if !strings.Contains(out, sgrBgA+"m \x1b[0m") {
		t.Errorf("a bg-only cell should render as a space inside a 48;2 sequence, got %q", out)
	}
	if got := c.CellBackground(3, 2); got != bgA {
		t.Errorf("CellBackground = %q, want %q", got, bgA)
	}
	if got := c.CellBackground(4, 2); got != "" {
		t.Errorf("neighbour has bg %q, want none", got)
	}
}

func TestDotsDrawOverTheirCellBackgroundInOneSequence(t *testing.T) {
	trueColour(t)
	c := NewCanvas(10, 5)
	c.SetScale(1)
	c.Clear()
	c.SetCellBackground(5, 2, bgA)
	c.PlotColoredTagged(orbital.Vec3{}, CellTag{Color: "#FF0000", IsVessel: true}) // centre pixel -> cell (5,2)
	out := c.String()
	i := strings.Index(out, sgrBgA)
	if i < 0 || !strings.Contains(out, "38;2;255;0;0") {
		t.Fatalf("want fg red and bg A both present, got %q", out)
	}
	open := out[strings.LastIndex(out[:i], "\x1b["):]
	open = open[:strings.Index(open, "m")+1]
	if !strings.Contains(open, "38;2;255;0;0") || !strings.Contains(open, sgrBgA) {
		t.Errorf("fg and bg should share one open sequence, got %q", open)
	}
}

func TestRunsCoalesceOnForegroundAndBackgroundTogether(t *testing.T) {
	trueColour(t)
	build := func(second lipgloss.Color) string {
		c := NewCanvas(10, 5)
		c.SetScale(1)
		c.Clear()
		// Two adjacent cells, same red dot colour, bg per arg.
		c.SetCellBackground(4, 2, bgA)
		c.SetCellBackground(5, 2, second)
		c.PlotColoredTagged(orbital.Vec3{X: -1}, CellTag{Color: "#FF0000", IsVessel: true}) // cell 4
		c.PlotColoredTagged(orbital.Vec3{X: 1}, CellTag{Color: "#FF0000", IsVessel: true})  // cell 5
		return c.String()
	}
	if n := opens(build(bgA)); n != 1 {
		t.Errorf("same fg and same bg should coalesce into 1 run, got %d opens", n)
	}
	if n := opens(build(bgB)); n != 2 {
		t.Errorf("same fg but different bg must split into 2 runs, got %d opens", n)
	}
}

func TestOverlayGlyphKeepsItsCellBackground(t *testing.T) {
	trueColour(t)
	c := NewCanvas(20, 5)
	c.Clear()
	for x := 0; x < 20; x++ {
		c.SetCellBackground(x, 1, bgA)
	}
	c.SetCellLabelColored(2, 1, "tower", lipgloss.Color("#E8E8E8"))
	out := c.String()
	if !strings.Contains(out, "48;2;16;32;48") || !strings.Contains(out, "tower") {
		t.Fatalf("label missing: %q", out)
	}
	i := strings.Index(out, "tower")
	open := out[strings.LastIndex(out[:i], "\x1b["):i]
	if !strings.Contains(open, sgrBgA) || !strings.Contains(open, "38;2;232;232;232") {
		t.Errorf("label run should carry its fg and the cell bg, got %q", open)
	}
}

func TestClearResetsTheBackgroundLayer(t *testing.T) {
	trueColour(t)
	c := NewCanvas(10, 5)
	c.Clear()
	c.SetCellBackground(1, 1, bgA)
	c.Clear()
	if got := c.CellBackground(1, 1); got != "" {
		t.Errorf("bg survived Clear: %q", got)
	}
	if out := c.String(); strings.Contains(out, "48;") {
		t.Errorf("bg escape survived Clear: %q", out)
	}
}

// The layer is paint, not ink: it must never vote in a cell's colour or
// answer a hit test.
func TestBackgroundNeverVotesOrHits(t *testing.T) {
	trueColour(t)
	scene := func(withBG bool) *Canvas {
		c := noBackdropScene()
		if withBG {
			for y := 0; y < 12; y++ {
				for x := 0; x < 40; x++ {
					c.SetCellBackground(x, y, bgB)
				}
			}
		}
		return c
	}
	a, b := scene(false), scene(true)
	for _, col := range []lipgloss.Color{"#6B8E4E", "#E8C547", "#3DDC84", "#FF0000", bgB} {
		if x, y := a.CountColor(col), b.CountColor(col); x != y {
			t.Errorf("CountColor(%s) changed with a bg layer: %d -> %d", col, x, y)
		}
	}
	for y := 0; y < 12; y++ {
		for x := 0; x < 40; x++ {
			if a.HitAt(x, y) != b.HitAt(x, y) {
				t.Fatalf("HitAt(%d,%d) changed with a bg layer", x, y)
			}
		}
	}
}

// backdropCanvas is 40x20 cells (80x80 px), scale 1, with the camera sitting
// `above` pixels over the top of a sphere of radius r: horizon at pixel row
// 40+above in the middle column, curving away at the sides.
func backdropCanvas(r, above float64) *Canvas {
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.Center(orbital.Vec3{Y: r + above})
	c.Clear()
	return c
}

var testRamp = []lipgloss.Color{"#0000F0", "#0000A0", "#000050"} // horizon -> top

func TestBackdropSkyFillsToTheTopAndGroundToTheBottom(t *testing.T) {
	c := backdropCanvas(1000, 8) // flat-ish horizon at px row 48 -> cell row 12
	c.FillHorizonBackdrop(orbital.Vec3{}, 1000, "#203018", testRamp)
	for x := 0; x < 40; x++ {
		if got := c.CellBackground(x, 0); got != testRamp[2] {
			t.Fatalf("top row col %d bg = %q, want the ramp's top colour %q", x, got, testRamp[2])
		}
		if got := c.CellBackground(x, 19); got != "#203018" {
			t.Fatalf("bottom row col %d bg = %q, want ground", x, got)
		}
	}
	skyRows := 0
	for y := 0; y < 20; y++ {
		if c.CellBackground(20, y) != "#203018" {
			skyRows++
		}
	}
	if skyRows != 12 {
		t.Errorf("sky spans %d cell rows above the horizon, want 12 (all the way up)", skyRows)
	}
	// The sky is a gradient: horizon end is the ramp's first colour.
	if got := c.CellBackground(20, 11); got != testRamp[0] && got != testRamp[1] {
		t.Errorf("cell just above the horizon = %q, want near the horizon colour", got)
	}
}

func TestBackdropHorizonCurvesAndColumnsOffTheDomeAreAllSky(t *testing.T) {
	c := backdropCanvas(30, 5) // dome: centre col horizon at px 45, edges lower
	c.FillHorizonBackdrop(orbital.Vec3{}, 30, "#203018", testRamp)
	// Row 14 (mid px 58): ground under the dome's crown, still sky near its flank.
	if c.CellBackground(20, 14) != "#203018" {
		t.Errorf("crown column at row 14 should be ground, got %q", c.CellBackground(20, 14))
	}
	if g := c.CellBackground(6, 14); g == "#203018" || g == "" {
		t.Errorf("flank column at row 14 should still be sky (horizon curves down), got %q", g)
	}
	// Row 16 (mid px 66): the flank has now crossed its horizon.
	if c.CellBackground(6, 16) != "#203018" {
		t.Errorf("flank column at row 16 should be ground, got %q", c.CellBackground(6, 16))
	}
	// Off the dome entirely: space-side, all sky to the bottom.
	if g := c.CellBackground(0, 19); g == "#203018" || g == "" {
		t.Errorf("column off the sphere should be sky at the bottom, got %q", g)
	}
}

func TestBackdropWithoutARampLeavesTheSkyBlack(t *testing.T) {
	c := backdropCanvas(1000, 8)
	c.FillHorizonBackdrop(orbital.Vec3{}, 1000, "#203018", nil)
	if got := c.CellBackground(20, 0); got != "" {
		t.Errorf("no ramp should mean no sky bg, got %q", got)
	}
	if got := c.CellBackground(20, 19); got != "#203018" {
		t.Errorf("ground still fills, got %q", got)
	}
}

// styledRunBG must emit exactly what lipgloss renders for the same
// foreground/background pair, on a cold and a warm cache.
func TestStyledRunBGMatchesLipgloss(t *testing.T) {
	trueColour(t)
	var s inkScratch
	for _, tc := range []struct {
		fg lipgloss.TerminalColor
		bg lipgloss.Color
	}{{nil, bgA}, {lipgloss.Color("#FF0000"), bgA}, {lipgloss.Color("#FF0000"), ""}, {lipgloss.Color("#FF0000"), bgB}} {
		st := lipgloss.NewStyle()
		if tc.fg != nil {
			st = st.Foreground(tc.fg)
		}
		if tc.bg != "" {
			st = st.Background(tc.bg)
		}
		want := st.Render("ab")
		for pass := 0; pass < 2; pass++ {
			if got := s.styledRunBG(tc.fg, tc.bg, "ab"); got != want {
				t.Errorf("styledRunBG(%v,%q) pass %d = %q, lipgloss %q", tc.fg, tc.bg, pass, got, want)
			}
		}
	}
}
