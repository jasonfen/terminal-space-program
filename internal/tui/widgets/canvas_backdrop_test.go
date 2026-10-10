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
