package render

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// GG grill mock (#779), NOT FOR MERGE: the navball's solid look. GGNav
// mirrors the canvas mock's mode (0 dots, 1 or 2 fine blocks, 3 coarse);
// the F8 handler keeps the two in step.
var GGNav = func() int {
	switch os.Getenv("TSP_GLYPHS") {
	case "body", "all":
		return 1
	case "coarse":
		return 3
	}
	return 0
}()

// ggNavRune is the block character for a braille-bit mask (see the canvas
// mock's ggRune: octant, or the three-row sextant in the coarse look).
func ggNavRune(mask rune) rune {
	if GGNav != 3 {
		return ggOctantRune[mask&0xFF]
	}
	v := 0
	for sx := 0; sx < 2; sx++ {
		lit := func(sy int) bool { return mask&brailleBitForDot[sx][sy] != 0 }
		if lit(0) {
			v |= 1 << sx
		}
		if lit(1) || lit(2) {
			v |= 4 << sx
		}
		if lit(3) {
			v |= 16 << sx
		}
	}
	switch v {
	case 0:
		return ' '
	case 63:
		return 0x2588
	case 21:
		return 0x258C
	case 42:
		return 0x2590
	}
	r := rune(0x1FB00 + v - 1)
	if v > 21 {
		r--
	}
	if v > 42 {
		r--
	}
	return r
}

// ggNavCell is one solid navball cell. in/sky/grid are braille-bit masks of
// the cell's in-disk, sky and grid-line dots. bg is the colour an overlay
// glyph (rung, reticle, marker) should sit on, "" for a rim cell.
//
//   - Rim cell (part outside the ball): the ball's exact outline in one
//     colour, the darker edge tint when 3 dots or fewer are inside.
//   - Sky and ground in one cell: sky pixels on ground, so the horizon is
//     the sharp edge between two colours, pixel for pixel.
//   - Grid dots: a line one pixel wide in the grid tint over the hemisphere.
//   - Otherwise a full square of the hemisphere's colour.
func ggNavCell(in, sky, grid rune, skyCount, groundCount int) (out string, bg lipgloss.Color) {
	hemi, edge, gridC := ColorNavballGround, ColorNavballGroundEdge, ColorNavballGroundGrid
	if skyCount >= groundCount {
		hemi, edge, gridC = ColorNavballSky, ColorNavballSkyEdge, ColorNavballSkyGrid
	}
	st := lipgloss.NewStyle()
	switch {
	case in != 0xFF:
		c := hemi
		if skyCount+groundCount <= 3 {
			c = edge
		}
		return st.Foreground(c).Render(string(ggNavRune(in))), ""
	case skyCount > 0 && groundCount > 0:
		return st.Foreground(ColorNavballSky).Background(ColorNavballGround).Render(string(ggNavRune(sky))), hemi
	case grid != 0:
		return st.Foreground(gridC).Background(hemi).Render(string(ggNavRune(grid))), hemi
	}
	return st.Foreground(hemi).Render("█"), hemi
}
