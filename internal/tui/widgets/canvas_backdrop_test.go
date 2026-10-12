package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// An orbit line drawn across a planet's disk must show (Jason 2026-10-10:
// "the target vessel orbit is having trouble rendering against a planet
// backdrop ... across other views too"). Each braille cell takes one
// colour by majority; the disk fills all 8 dots of a cell and a line adds
// 1-3, so the planet won nearly every cell the line crossed. Backdrop
// dots now yield: a cell holding any non-backdrop ink shows only that ink,
// in its colour.
func TestLineAcrossBackdropDiskWinsItsCells(t *testing.T) {
	forceANSIColor(t)

	const planet, line = lipgloss.Color("#2060C0"), lipgloss.Color("#3DDC84")
	c := NewCanvas(40, 20) // 80 x 80 px
	c.SetScale(1)
	c.FillColoredDiskTagged(orbital.Vec3{}, 30, CellTag{Color: planet, BodyID: "earth", Backdrop: true})
	c.PlotDenseLineColored(orbital.Vec3{X: -36, Y: 1}, orbital.Vec3{X: 36, Y: 1}, line, 1)

	if got := c.CountColor(line); got < 25 {
		t.Errorf("line resolves to its colour in %d cells, want the ~30 it crosses (the disk outvoted it)", got)
	}
	// The cells it crosses inside the disk show the line's dots, not a full
	// planet block: a crisp line, not a band.
	plain := strings.Split(ansi.Strip(c.String()), "\n")
	full := 0
	for _, row := range plain {
		for _, r := range row {
			if r == '⣿' {
				full++
			}
		}
	}
	lineCells := 0
	for y := range plain {
		for x, r := range []rune(plain[y]) {
			if r != '⣿' && r != ' ' && r >= 0x2800 && r <= 0x28FF && x > 8 && x < 32 && y > 4 && y < 16 {
				lineCells++
			}
		}
	}
	if full == 0 {
		t.Fatal("setup: the disk drew no full cells")
	}
	if lineCells < 15 {
		t.Errorf("only %d partial cells inside the disk; the line's cells should show just its dots", lineCells)
	}
	// Without a line the disk keeps its colour everywhere.
	d := NewCanvas(40, 20)
	d.SetScale(1)
	d.FillColoredDiskTagged(orbital.Vec3{}, 30, CellTag{Color: planet, BodyID: "earth", Backdrop: true})
	if d.CountColor(line) != 0 || d.CountColor(planet) == 0 {
		t.Errorf("a bare disk changed colour: planet %d cells, line %d", d.CountColor(planet), d.CountColor(line))
	}
}

// litDots rebuilds the lit pixel grid from String()'s braille runes.
func litDots(t *testing.T, c *Canvas) map[[2]int]bool {
	t.Helper()
	lit := map[[2]int]bool{}
	for y, row := range strings.Split(ansi.Strip(c.String()), "\n") {
		for x, r := range []rune(row) {
			if r < 0x2800 || r > 0x28FF {
				continue
			}
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					if (r-0x2800)&brailleDotBit[dy][dx] != 0 {
						lit[[2]int{x*2 + dx, y*4 + dy}] = true
					}
				}
			}
		}
	}
	return lit
}

// A solid line over a body wins the cells it touches and leaves the rest of
// the planet alone (GM Q5 "a", #580; Jason 2026-10-10: "a would be best").
// v0.51.3 also cleared every planet dot touching a line dot, a one-dot dark
// moat that stair-stepped along the cell grid and read as a jagged trench.
// The knockout stays (TestLineAcrossBackdropDiskWinsItsCells); the moat is
// gone: a planet dot next to the line, in a cell the line does not touch,
// stays lit.
func TestSolidOrbitOverBodyHasNoMoat(t *testing.T) {
	const planet, line = lipgloss.Color("#E8D940"), lipgloss.Color("#FFD93D") // Io under your orbit
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.FillColoredDiskTagged(orbital.Vec3{}, 30, CellTag{Color: planet, BodyID: "io", Backdrop: true})
	c.PlotDenseLineColored(orbital.Vec3{X: -36, Y: 1}, orbital.Vec3{X: 36, Y: 1}, line, 1)

	fore := map[[2]int]bool{}
	foreCells := map[[2]int]bool{}
	c.pixelTags.each(func(px, py int, tag CellTag) {
		if !tag.Backdrop {
			fore[[2]int{px, py}] = true
			foreCells[[2]int{px / 2, py / 4}] = true
		}
	})
	if len(fore) < 50 {
		t.Fatalf("setup: the line drew %d pixels", len(fore))
	}
	lit := litDots(t, c)
	beside, litBeside := 0, 0
	for p := range fore {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				q := [2]int{p[0] + dx, p[1] + dy}
				if fore[q] || foreCells[[2]int{q[0] / 2, q[1] / 4}] {
					continue // the line's own pixel, or a cell the knockout owns
				}
				if tag, ok := c.pixelTags.get(q[0], q[1]); ok && tag.Backdrop {
					beside++
					if lit[q] {
						litBeside++
					}
				}
			}
		}
	}
	// Instrument: there are planet dots beside the line in untouched cells.
	if beside < 20 {
		t.Fatalf("setup: only %d planet dots sit beside the line outside its cells", beside)
	}
	if litBeside != beside {
		t.Errorf("%d of %d planet dots beside the line, in cells it does not touch, are dark: that is the moat", beside-litBeside, beside)
	}
	// The knockout stays: a cell the line touches shows the line's dots only.
	for cell := range foreCells {
		for dy := 0; dy < 4; dy++ {
			for dx := 0; dx < 2; dx++ {
				q := [2]int{cell[0]*2 + dx, cell[1]*4 + dy}
				if lit[q] && !fore[q] {
					t.Fatalf("cell %v still shows a planet dot at %v: the knockout is gone", cell, q)
				}
			}
		}
	}
}

// colorTally picks the same winner as pickDominantColor (count, then the
// colour string), including ties and a cell using all 8 slots.
func TestColorTallyMatchesPickDominantColor(t *testing.T) {
	for _, seq := range [][]lipgloss.Color{
		{"#B", "#A", "#B", "#A"},
		{"#1", "#2", "#3", "#4", "#5", "#5", "#6", "#7"},
		{"#9", "#9", "#1", "#2", "#3", "#4", "#4", "#4"},
		{"#E", "#D", "#C", "#B", "#A", "#A", "#E", "#E"},
	} {
		palette := []lipgloss.Color{""}
		ids := map[lipgloss.Color]uint16{}
		var tally colorTally
		counts := map[lipgloss.Color]int{}
		for _, c := range seq {
			id, ok := ids[c]
			if !ok {
				id = uint16(len(palette))
				palette = append(palette, c)
				ids[c] = id
			}
			tally.add(id)
			counts[c]++
		}
		if got, want := palette[tally.dominant(palette)], pickDominantColor(counts); got != want {
			t.Errorf("%v: tally picks %q, pickDominantColor %q", seq, got, want)
		}
	}
}

// resolveCellInk's scratch lives on the canvas and is reused every frame;
// a frame drawn after a busy one must render exactly as on a fresh canvas
// (release zeroes only what the last frame wrote).
func TestCellInkScratchDoesNotLeakBetweenFrames(t *testing.T) {
	drawQuiet := func(c *Canvas) {
		c.PlotColored(orbital.Vec3{X: 5, Y: 5}, "#FF0000")
	}
	fresh := NewCanvas(40, 20)
	fresh.SetScale(1)
	drawQuiet(fresh)
	want := fresh.String()

	c := NewCanvas(40, 20)
	c.SetScale(1)
	for i := 0; i < 3; i++ {
		c.Clear()
		c.FillColoredDiskTagged(orbital.Vec3{}, 30, CellTag{Color: "#2060C0", Backdrop: true})
		c.PlotDenseLineColored(orbital.Vec3{X: -36, Y: 1}, orbital.Vec3{X: 36, Y: 1}, "#3DDC84", 1)
		_ = c.String()
		c.Clear()
		drawQuiet(c)
		if got := c.String(); got != want {
			t.Fatalf("round %d: a quiet frame after a busy one rendered differently from a fresh canvas (stale scratch)", i)
		}
		if n := c.CountColor("#3DDC84"); n != 0 {
			t.Fatalf("round %d: quiet frame counts %d cells of the busy frame's green", i, n)
		}
	}
}

// Dotted and dashed ink (a Planned leg, the CommNet beam) does not win its
// cells over a body: knocked out, the gaps between its dots
// turned the dark channel into a zig-zag that read as a strange wavy line
// across the planet (Jason 2026-10-10, the CommNet beam). It keeps the old
// majority vote, so over a disk it yields as it did before v0.51.3.
func TestSparseLineOverBackdropYields(t *testing.T) {
	const planet, beam = lipgloss.Color("#2060C0"), lipgloss.Color("#34E2D0")
	draw := func(step int) *Canvas {
		c := NewCanvas(40, 20)
		c.SetScale(1)
		c.FillColoredDiskTagged(orbital.Vec3{}, 30, CellTag{Color: planet, Backdrop: true})
		c.PlotDenseLineColored(orbital.Vec3{X: -36, Y: 1}, orbital.Vec3{X: 36, Y: 1}, beam, step)
		return c
	}
	bare := NewCanvas(40, 20)
	bare.SetScale(1)
	bare.FillColoredDiskTagged(orbital.Vec3{}, 30, CellTag{Color: planet, Backdrop: true})
	wantDots := len(litDots(t, bare))

	dotted := draw(3)
	if n := dotted.CountColor(beam); n > 8 {
		t.Errorf("a dotted line takes %d cells over the disk, want it to yield (only the few cells off the disk's edge)", n)
	}
	if got := len(litDots(t, dotted)); got < wantDots {
		t.Errorf("a dotted line cleared planet dots (%d lit, the bare disk has %d): sparse ink must not knock out", got, wantDots)
	}
	// The same line drawn solid still wins its cells.
	if solid := draw(1); solid.CountColor(beam) < 25 {
		t.Errorf("a solid line takes only %d cells over the disk; solid ink should still win", solid.CountColor(beam))
	}
}

// The far (behind-the-plane) half of a Real ellipse is dotted ink at
// classRealFarSpacingPx, so it must yield over a Backdrop the way every
// other dotted line does: no knockout (review #582 M1). The near
// half of the same orbit is solid and keeps winning. Drawn both fresh and
// through the curve cache, whose replay used to carry one tag for the
// whole curve.
func TestRealEllipseFarHalfYieldsOverBackdrop(t *testing.T) {
	const planet, orbit = lipgloss.Color("#2060C0"), lipgloss.Color("#FFD93C")
	// Omega=Arg=0: the orbit's -y half is behind the basis plane (far,
	// dotted) and its +y half in front of it (near, solid).
	el := orbital.Elements{A: 30, E: 0, I: 0.5}
	const diskY, diskR = 26.0, 6.0
	for _, cached := range []bool{false, true} {
		for _, side := range []struct {
			name string
			y    float64
		}{{"near", diskY}, {"far", -diskY}} {
			scene := func(withDisk bool) *Canvas {
				c := NewCanvas(40, 20)
				c.SetScale(1)
				c.Center(orbital.Vec3{})
				if withDisk {
					c.FillColoredDiskTagged(orbital.Vec3{Y: side.y}, diskR, CellTag{Color: planet, Backdrop: true})
				}
				tag := CellTag{Color: orbit}
				if cached {
					// Twice: the first call is a miss that draws fresh; the
					// second is the hit that replays the recorded pixels.
					c.DrawEllipseClassCachedTagged("t", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, tag)
					c.DrawEllipseClassCachedTagged("t", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, tag)
					if c.curveCacheHits != 1 {
						t.Fatalf("second cached draw was not a cache hit (hits %d)", c.curveCacheHits)
					}
				} else {
					c.DrawEllipseClassTagged(el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, tag)
				}
				return c
			}
			with, without := scene(true), scene(false)
			// Cells of the orbit that the disk took: ~0 for ink that wins.
			ceded := without.CountColor(orbit) - with.CountColor(orbit)
			cx, cy := with.projectPx(orbital.Vec3{Y: side.y})
			inDisk := func(c *Canvas) int {
				n := 0
				for p := range litDots(t, c) {
					dx, dy := float64(p[0])-cx, float64(p[1])-cy
					if dx*dx+dy*dy <= diskR*diskR {
						n++
					}
				}
				return n
			}
			bare := NewCanvas(40, 20)
			bare.SetScale(1)
			bare.Center(orbital.Vec3{})
			bare.FillColoredDiskTagged(orbital.Vec3{Y: side.y}, diskR, CellTag{Color: planet, Backdrop: true})
			wantDots := inDisk(bare)
			if side.name == "near" {
				// Instrument check: solid ink over the same kind of disk wins.
				if ceded > 2 {
					t.Errorf("cached=%v near half ceded %d cells to the disk, want it to win", cached, ceded)
				}
				continue
			}
			if ceded < 6 {
				t.Errorf("cached=%v far half ceded only %d cells to the disk, want it to yield like any dotted line", cached, ceded)
			}
			if got := inDisk(with); got < wantDots {
				t.Errorf("cached=%v far half cleared planet dots (%d lit in the disk, bare %d): it knocks out", cached, got, wantDots)
			}
		}
	}
}

// Scenery (body orbits, the SOI Ring) is Backdrop: it sits behind every
// live line and body (review #582 M2-S24). Two consequences are pinned,
// one per way of losing the tag: a moon's disk keeps its cells where the
// body's own orbit line crosses it, and a live solid line across a
// Scenery line knocks out the Scenery dots in the cells it shares rather than the
// two sharing the cell.
func TestSceneryIsBackdropUnderLiveInk(t *testing.T) {
	const moon, scenery, live = lipgloss.Color("#9AA0A6"), lipgloss.Color("#506070"), lipgloss.Color("#FFD93C")
	el := orbital.Elements{A: 20, E: 0}
	newCanvas := func() *Canvas {
		c := NewCanvas(40, 20)
		c.SetScale(1)
		c.Center(orbital.Vec3{})
		return c
	}
	drawScenery := func(c *Canvas) {
		c.DrawEllipseClassTagged(el, orbital.Vec3{}, 360, ClassScenery, orbital.Vec3{}, 0, CellTag{Color: scenery})
		c.PlotPolylineClassTagged([]orbital.Vec3{{X: -30, Y: 5}, {X: 30, Y: 5}}, CellTag{Color: scenery}, ClassScenery)
	}

	// 1. A moon sitting on its own orbit line keeps its disk.
	c := newCanvas()
	c.FillColoredDiskTagged(orbital.Vec3{X: 20}, 4, CellTag{Color: moon, Backdrop: true})
	drawScenery(c)
	if n := c.CountColor(moon); n < 2 {
		t.Errorf("moon disk keeps only %d cells under its own scenery orbit", n)
	}

	// 2. Live solid ink crossing Scenery knocks out the Scenery dots in its cells.
	only := func(draw func(*Canvas)) map[[2]int]bool {
		c := newCanvas()
		draw(c)
		return litDots(t, c)
	}
	drawLive := func(c *Canvas) {
		c.PlotDenseLineColored(orbital.Vec3{X: -30, Y: -3}, orbital.Vec3{X: 30, Y: 9}, live, 1)
	}
	a, b := only(drawScenery), only(drawLive)
	union := len(a)
	for p := range b {
		if !a[p] {
			union++
		}
	}
	both := newCanvas()
	drawScenery(both)
	drawLive(both)
	if got := len(litDots(t, both)); got >= union {
		t.Errorf("scenery under a live line lost no dots (%d lit, union %d): it is not Backdrop", got, union)
	}
}
