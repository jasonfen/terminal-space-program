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

// A line over a body sits in a dark moat (Jason 2026-10-10: "can it be
// smart about background colors like on Io"): no backdrop dot touching
// one of the line's dots stays lit, so the line reads on a planet of any
// colour or texture (yellow over Io, green over Kern or Earth's land)
// without changing colour. The moat is one dot wide; backdrop further out
// is untouched.
func TestLineOverBackdropSitsInADarkMoat(t *testing.T) {
	const planet, line = lipgloss.Color("#E8D940"), lipgloss.Color("#FFD93D") // Io under your orbit
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.FillColoredDiskTagged(orbital.Vec3{}, 30, CellTag{Color: planet, BodyID: "io", Backdrop: true})
	c.PlotDenseLineColored(orbital.Vec3{X: -36, Y: 1}, orbital.Vec3{X: 36, Y: 1}, line, 1)

	fore := map[[2]int]bool{}
	c.pixelTags.each(func(px, py int, tag CellTag) {
		if !tag.Backdrop {
			fore[[2]int{px, py}] = true
		}
	})
	if len(fore) < 50 {
		t.Fatalf("setup: the line drew %d pixels", len(fore))
	}
	lit := litDots(t, c)
	touching, moatEdge := 0, 0
	for p := range fore {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				q := [2]int{p[0] + dx, p[1] + dy}
				if fore[q] {
					continue
				}
				if tag, ok := c.pixelTags.get(q[0], q[1]); ok && tag.Backdrop && lit[q] {
					touching++
				}
			}
		}
		// Two dots out is beyond the moat: the planet is still there.
		if q := [2]int{p[0], p[1] + 2}; !fore[q] && lit[q] {
			moatEdge++
		}
	}
	if touching != 0 {
		t.Errorf("%d planet dots still lit right beside the line; want a one-dot dark moat", touching)
	}
	if moatEdge == 0 {
		t.Error("no planet dots two out from the line: the moat is wider than one dot, or the disk vanished")
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
// cells over a body or cut a moat: knocked out, the gaps between its dots
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
		t.Errorf("a dotted line cleared planet dots (%d lit, the bare disk has %d): no moat for sparse ink", got, wantDots)
	}
	// The same line drawn solid still wins and cuts its moat.
	if solid := draw(1); solid.CountColor(beam) < 25 {
		t.Errorf("a solid line takes only %d cells over the disk; solid ink should still win", solid.CountColor(beam))
	}
}
