package screens

import (
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/settings"
)

// SurfaceMarksCard is the `y` card (ADR 0066 decision 6): five rows that
// filter what the orbit map draws. It filters the map only; a hidden kind
// stays in the `t` menu. The App owns the settings write.
type SurfaceMarksCard struct {
	theme  Theme
	cursor int
}

type surfaceMarkRow struct {
	chip   settings.Chip
	label  string
	letter string
}

var surfaceMarkRows = []surfaceMarkRow{
	{settings.ChipMarksLanded, "landed vessels", "v"},
	{settings.ChipMarksFlags, "flags", "f"},
	{settings.ChipMarksWrecks, "wrecks", "w"},
	{settings.ChipMarksPartners, "partners' marks", "p"},
	{settings.ChipCoverageNow, "coverage now", "c"},
}

func NewSurfaceMarksCard(th Theme) *SurfaceMarksCard { return &SurfaceMarksCard{theme: th} }

// Open puts the cursor back on the first row.
func (c *SurfaceMarksCard) Open() { c.cursor = 0 }

// HandleKey moves the cursor, and returns the chip to flip (ok) or close.
func (c *SurfaceMarksCard) HandleKey(k string) (flip settings.Chip, ok, closeCard bool) {
	switch k {
	case "esc", "f3":
		return "", false, true
	case "up":
		c.cursor = (c.cursor + len(surfaceMarkRows) - 1) % len(surfaceMarkRows)
	case "down":
		c.cursor = (c.cursor + 1) % len(surfaceMarkRows)
	case " ", "space", "enter":
		return surfaceMarkRows[c.cursor].chip, true, false
	default:
		for i, r := range surfaceMarkRows {
			if k == r.letter {
				c.cursor = i
				return r.chip, true, false
			}
		}
	}
	return "", false, false
}

// Render draws the card; on says whether a chip is currently shown.
func (c *SurfaceMarksCard) Render(on func(settings.Chip) bool) string {
	body := []string{""}
	for i, r := range surfaceMarkRows {
		cur, box := "  ", "[ ]"
		if on(r.chip) {
			box = "[x]"
		}
		label := padCells(r.label, 18)
		if i == c.cursor {
			cur = c.theme.Primary.Render("▸") + " "
			label = c.theme.Primary.Render(label)
		}
		body = append(body, "  "+cur+box+" "+label+c.theme.Dim.Render(r.letter))
	}
	body = append(body, "", "  "+c.theme.Footer.Render("[↑/↓] pick [space] toggle [esc]"))
	return strings.Join(formBox(c.theme, "  "+c.theme.Title.Render("SURFACE MARKS"), body, CardW), "\n")
}
