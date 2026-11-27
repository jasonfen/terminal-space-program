package widgets

import (
	"math/bits"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// GG grill mock (#779), NOT FOR MERGE. GGMode picks how a canvas cell is
// drawn: 0 braille (today), 1 solid pixels on bodies (cells holding part
// of a filled disk become Unicode 16 octants, two colours a cell; every
// other cell stays braille), 2 solid pixels everywhere. TSP_GLYPHS=
// braille|body|all sets the start mode; F8 cycles it in the running game.
var GGMode = func() int {
	switch os.Getenv("TSP_GLYPHS") {
	case "body":
		return 1
	case "all":
		return 2
	case "coarse":
		return 3
	}
	return 0
}()

// GGModeName names the look.
func GGModeName() string {
	return [...]string{"dots", "solid, fine", "solid everything", "solid, coarse"}[GGMode]
}

// ggShowTag turns the title-bar tag on: when the mock was started with
// TSP_GLYPHS set, or once F8 has been pressed. Off otherwise, so a plain
// run (and every test) keeps main's exact title bar.
var ggShowTag = os.Getenv("TSP_GLYPHS") != ""

// GGTitleTag is the title bar's " [F8 look: ...]" tag, or "".
func GGTitleTag() string {
	if !ggShowTag {
		return ""
	}
	return " [F8 look: " + GGModeName() + "]"
}

// GGCycle is F8: dots -> solid bodies in octants (fine, 2x4 a cell) ->
// solid bodies in sextants (coarse, 2x3 a cell, drawn by nearly every
// current terminal) -> dots. "Solid everything" (mode 2) was dropped by
// Q1 and is reachable only with TSP_GLYPHS=all.
func GGCycle() {
	ggShowTag = true
	switch GGMode {
	case 0:
		GGMode = 1
	case 1:
		GGMode = 3
	default:
		GGMode = 0
	}
}

// ggRune is the block character for a braille-bit mask of lit pixels: the
// exact octant, or in the coarse look the sextant whose three rows are
// pixel row 0, rows 1 or 2, and row 3.
func ggRune(mask uint8) rune {
	if GGMode != 3 {
		return octantRune[mask]
	}
	v := 0
	for dx := 0; dx < 2; dx++ {
		col := func(dy int) bool { return mask&uint8(brailleDotBit[dy][dx]) != 0 }
		if col(0) {
			v |= 1 << dx
		}
		if col(1) || col(2) {
			v |= 4 << dx
		}
		if col(3) {
			v |= 16 << dx
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

// ggCell resolves one cell under the mock. ok false means "draw it as
// braille, the way main does". fg/bg "" mean terminal default.
//
// A cell shows two colours at most: the lit octants (foreground) and the
// rest (background). Each pixel is one of: filled area (a disk; `under`
// remembers it even where ink was drawn on top), solid ink (a live orbit),
// soft ink (dotted or dashed lines, rings, an atmosphere ring), or empty.
//
//   - Solid ink over a body: the line's own pixels in its colour, the body
//     behind as the background. No knockout, no trench.
//   - Body only, cell fully inside the disk: its two commonest texture
//     colours, pixel for pixel. A ring or atmosphere line there shows as
//     the second colour.
//   - A limb cell (part body, part space): the body's exact outline in one
//     colour; soft ink in that cell is dropped (three things, two colours).
//   - A line through a limb cell: the line's pixels; the body fills the
//     rest of the cell if it has the majority of what is left, else it is
//     dropped from that cell.
//   - Dotted and dashed ink over a body yields to it, as on main (GM Q6).
func (c *Canvas) ggCell(s *inkScratch, cx, cy int) (r rune, fg, bg lipgloss.Color, ok bool) {
	lit := c.dc.cells[cy*c.dc.stride()+cx]
	if lit == 0 {
		return 0, "", "", false
	}
	g := &c.pixelTags
	var areaMask, strongMask, lineMask, softMask uint8
	var area, strong, line, soft colorTally
	var pxID [8]uint16
	var pxBit [8]uint8
	n := 0
	for dy := 0; dy < 4; dy++ {
		for dx := 0; dx < 2; dx++ {
			bit := uint8(brailleDotBit[dy][dx])
			if lit&bit == 0 {
				continue
			}
			px, py := cx*2+dx, cy*4+dy
			if px >= g.w || py >= g.h {
				softMask |= bit
				soft.add(0)
				continue
			}
			pi := py*g.w + px
			if u := g.under[pi] - 1; u >= 0 && int(u) < len(s.tagColor) {
				areaMask |= bit
				area.add(s.tagColor[u])
				pxID[n], pxBit[n] = s.tagColor[u], bit
				n++
			}
			t := g.tagIdx[pi] - 1
			if t < 0 || int(t) >= len(s.tagColor) {
				if g.under[pi] == 0 {
					softMask |= bit
					soft.add(0)
				}
				continue
			}
			if g.under[pi] == g.tagIdx[pi] {
				continue // the area itself is the top ink here
			}
			switch {
			case s.tagStrong[t]:
				strongMask |= bit
				strong.add(s.tagColor[t])
			case s.tagBack[t] && !g.tags[t].Sparse:
				lineMask |= bit
				line.add(s.tagColor[t])
			default:
				softMask |= bit
				soft.add(s.tagColor[t])
			}
		}
	}
	if areaMask == 0 {
		if GGMode != 2 {
			return 0, "", "", false
		}
		// No body here: every lit pixel a solid square, coloured as main
		// colours the cell (its knockout rule included).
		idx := cy*c.cols + cx
		return octantRune[uint8(s.runeAt(idx, 0x2800+rune(lit))-0x2800)], s.colorAt(idx), "", true
	}
	a, b := ggTopTwo(&area, s.palette)
	empty := ^(areaMask | strongMask)
	full := areaMask|strongMask == 0xFF
	if strongMask != 0 {
		fg = s.palette[strong.dominant(s.palette)]
		if full || bits.OnesCount8(areaMask&^strongMask) >= bits.OnesCount8(empty) {
			bg = s.palette[a]
		}
		return ggRune(strongMask), fg, bg, true
	}
	if !full {
		return ggRune(areaMask), s.palette[a], "", true
	}
	if lineMask != 0 {
		return ggRune(lineMask), s.palette[line.dominant(s.palette)], s.palette[a], true
	}
	if b == 0 {
		return ggRune(0xFF), s.palette[a], "", true
	}
	var maskA uint8
	for i := 0; i < n; i++ {
		if pxID[i] == a {
			maskA |= pxBit[i]
		}
	}
	return ggRune(maskA), s.palette[a], s.palette[b], true
}

// ggTopTwo is the two most frequent colours of a tally (b is 0 if there is
// only one), ties broken on the colour string like dominant.
func ggTopTwo(t *colorTally, palette []lipgloss.Color) (a, b uint16) {
	a = t.dominant(palette)
	var bn uint8
	for i := uint8(0); i < t.n; i++ {
		id, n := t.id[i], t.cnt[i]
		if id == a {
			continue
		}
		if n > bn || (n == bn && string(palette[id]) < string(palette[b])) {
			b, bn = id, n
		}
	}
	return a, b
}
