package widgets

import (
	"github.com/charmbracelet/lipgloss"
)

// brailleDotBit is the braille-pattern bit for a pixel at (dx, dy) inside
// its 2x4 cell, the same layout drawille sets.
var brailleDotBit = [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

// colorTally counts one cell's pixels per colour by palette id. A braille
// cell has 8 dots, so 8 slots always suffice: no map, no spill.
type colorTally struct {
	n   uint8
	id  [8]uint16
	cnt [8]uint8
}

func (t *colorTally) add(id uint16) {
	for i := uint8(0); i < t.n; i++ {
		if t.id[i] == id {
			t.cnt[i]++
			return
		}
	}
	if t.n < uint8(len(t.id)) {
		t.id[t.n], t.cnt[t.n] = id, 1
		t.n++
	}
}

// dominant is pickDominantColor over the tally: highest count, ties broken
// on the colour string so the choice is stable frame to frame.
func (t *colorTally) dominant(palette []lipgloss.Color) uint16 {
	var best uint16
	var bestN uint8
	for i := uint8(0); i < t.n; i++ {
		id, n := t.id[i], t.cnt[i]
		if n > bestN || (n == bestN && string(palette[id]) < string(palette[best])) {
			best, bestN = id, n
		}
	}
	return best
}

// cellInk is one terminal cell's resolved ink: color (a palette id, 0 =
// unstyled) and knockout (the cell shows only foreDots, replacing its own
// braille rune). all/fore/foreDots/backdrop are working state.
type cellInk struct {
	all, fore colorTally
	color     uint16
	foreDots  uint8
	backdrop  bool
	knockout  bool
}

// inkScratch is resolveCellInk's working set, kept on the Canvas and reused
// every frame so String() allocates nothing per cell. A per-cell map
// version (and then a sync.Pool, which GC empties) cost ~5% of an idle map
// frame. A Canvas is single-owner already (every draw mutates it), so the
// scratch needs no lock.
type inkScratch struct {
	cells     []cellInk        // cols*rows, zero except at touched
	touched   []int32          // cell indices written this call
	palette   []lipgloss.Color // palette id -> colour; id 0 = ""
	tagColor  []uint16         // pixel tag index (0-based) -> palette id
	tagBack   []bool           // pixel tag index (0-based) -> Backdrop
	tagStrong []bool           // pixel tag index (0-based) -> solid ink: wins over Backdrop
	paletteOf map[lipgloss.Color]uint16
}

// colorAt is the resolved colour of the cell at idx, "" when unstyled.
func (s *inkScratch) colorAt(idx int) lipgloss.Color { return s.palette[s.cells[idx].color] }

// runeAt applies the cell's knockout to ch, the cell's own rune.
func (s *inkScratch) runeAt(idx int, ch rune) rune {
	if ci := &s.cells[idx]; ci.knockout {
		return 0x2800 | rune(ci.foreDots)
	}
	return ch
}

// release zeroes what this call wrote, ready for the next frame.
func (s *inkScratch) release() {
	for _, idx := range s.touched {
		s.cells[idx] = cellInk{}
	}
	s.touched = s.touched[:0]
}

// resolveCellInk is the one per-cell colour rule String() and CountColor
// share. A cell takes the majority colour of its tagged pixels, ties broken
// on the colour string, except where Backdrop ink (a body disk, a horizon
// band, Scenery) meets solid ink (a live orbit, a vessel, a marker; not
// Sparse dotted or dashed ink, which keeps the plain majority):
//   - a cell holding both shows only the other ink, its majority colour and
//     its own dots (knockout), so a line across a planet stays a line
//     rather than losing the vote 8 to 2, on a body of any colour or
//     texture: yellow over Io, green over Kern or Earth's land.
//
// There is no moat: v0.51.3 also cleared every backdrop dot touching a dot
// of other ink, a one-dot dark border that stair-stepped along the cell
// grid and read as a jagged trench (Jason 2026-10-10: variants b, c, d
// rejected, "a would be best"; GM Q5, #580). The planet keeps every cell and
// dot the line does not touch.
//
// The caller must release() the result before the next call.
func (c *Canvas) resolveCellInk() *inkScratch {
	s := &c.ink
	g := &c.pixelTags
	if n := c.cols * c.rows; len(s.cells) != n {
		s.cells = make([]cellInk, n)
		s.touched = s.touched[:0]
	}

	// Palette: one id per distinct colour in this frame's tag table, so the
	// per-pixel work below is array indexing, never a string compare.
	if s.paletteOf == nil {
		s.paletteOf = make(map[lipgloss.Color]uint16)
	}
	clear(s.paletteOf)
	s.palette = append(s.palette[:0], "")
	s.tagColor, s.tagBack, s.tagStrong = s.tagColor[:0], s.tagBack[:0], s.tagStrong[:0]
	for _, tag := range g.tags {
		var id uint16
		if tag.Color != "" {
			var ok bool
			if id, ok = s.paletteOf[tag.Color]; !ok {
				id = uint16(len(s.palette))
				s.palette = append(s.palette, tag.Color)
				s.paletteOf[tag.Color] = id
			}
		}
		s.tagColor = append(s.tagColor, id)
		s.tagBack = append(s.tagBack, tag.Backdrop)
		s.tagStrong = append(s.tagStrong, !tag.Backdrop && !tag.Sparse && tag.Color != "")
	}

	// Tally every cell.
	for _, pi := range g.touched {
		t := g.tagIdx[pi] - 1
		if t < 0 || s.tagColor[t] == 0 {
			continue
		}
		px, py := int(pi)%g.w, int(pi)/g.w
		cx, cy := px/2, py/4
		if cx >= c.cols || cy >= c.rows {
			continue
		}
		idx := cy*c.cols + cx
		ci := &s.cells[idx]
		if ci.all.n == 0 {
			s.touched = append(s.touched, int32(idx))
		}
		bit := uint8(brailleDotBit[py%4][px%2])
		id := s.tagColor[t]
		if s.tagBack[t] {
			ci.backdrop = true
			ci.all.add(id)
			continue
		}
		ci.all.add(id)
		if !s.tagStrong[t] {
			continue // Sparse: votes with everything else, never knocks out
		}
		ci.fore.add(id)
		ci.foreDots |= bit
	}

	for _, idx := range s.touched {
		ci := &s.cells[idx]
		switch {
		case ci.fore.n > 0 && ci.backdrop:
			ci.color = ci.fore.dominant(s.palette)
			ci.knockout = true
		case ci.all.n > 0:
			ci.color = ci.all.dominant(s.palette)
		}
	}
	return s
}

// CountColorIn is CountColor limited to the cells in the inclusive cell
// rectangle [col0, col1] x [row0, row1]: how much of a colour shows on a
// given patch of the canvas (a planet's disk), not anywhere on it.
func (c *Canvas) CountColorIn(color lipgloss.Color, col0, row0, col1, row1 int) int {
	ink := c.resolveCellInk()
	defer ink.release()
	n := 0
	for _, idx := range ink.touched {
		col, row := int(idx)%c.cols, int(idx)/c.cols
		if col < col0 || col > col1 || row < row0 || row > row1 {
			continue
		}
		if ink.colorAt(int(idx)) == color {
			n++
		}
	}
	return n
}
