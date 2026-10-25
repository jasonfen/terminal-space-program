package widgets

import "strings"

// brailleGrid is the canvas's dot bitmap: one byte per terminal cell, bit
// layout of U+2800 braille. Replaces github.com/exrook/drawille-go (map of
// maps, one string concat per cell), same output byte for byte.
//
// The grid keeps one spare column and row beyond cols x rows. drawille's
// Rows(0, 0, pxW, pxH) returned that extra column, so the uncoloured
// String() path emits cols+1 runes per line (the last is usually U+2800);
// kept so output stays byte-identical. Stride is cols+1.
type brailleGrid struct {
	cols, rows int // visible cells; storage is (cols+1) x (rows+1)
	cells      []uint8
}

func (g *brailleGrid) stride() int { return g.cols + 1 }

// Dot bits for (x%2, y%4) within a cell.
var brailleBit = [4][2]uint8{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

func newBrailleGrid(cols, rows int) brailleGrid {
	return brailleGrid{cols: cols, rows: rows, cells: make([]uint8, (cols+1)*(rows+1))}
}

// resize keeps the overlapping cells, like the old unbounded map did.
func (g *brailleGrid) resize(cols, rows int) {
	if cols == g.cols && rows == g.rows {
		return
	}
	n := newBrailleGrid(cols, rows)
	for r := 0; r <= min(rows, g.rows); r++ {
		copy(n.cells[r*n.stride():r*n.stride()+min(cols, g.cols)+1], g.cells[r*g.stride():])
	}
	*g = n
}

func (g *brailleGrid) Clear() { clear(g.cells) }

// Set lights pixel (x, y). Cell index truncates toward zero and negative
// dot offsets wrap, exactly as drawille did (x=-1 lands in column 0);
// anything outside the grid is dropped.
func (g *brailleGrid) Set(x, y int) {
	cx, cy := x/2, y/4
	if cx < 0 || cx > g.cols || cy < 0 || cy > g.rows {
		return
	}
	dx, dy := x%2, y%4
	if x < 0 {
		dx = 1 + (x+1)%2
	}
	if y < 0 {
		dy = 3 + (y+1)%4
	}
	g.cells[cy*g.stride()+cx] |= brailleBit[dy][dx]
}

// Get reports whether pixel (x, y) is lit; outside the grid is false.
func (g *brailleGrid) Get(x, y int) bool {
	if x < 0 || y < 0 || x >= (g.cols+1)*2 || y >= (g.rows+1)*4 {
		return false
	}
	return g.cells[(y/4)*g.stride()+x/2]&brailleBit[y%4][x%2] != 0
}

// String renders rows of cols+1 braille runes joined by "\n" (no trailing one).
func (g *brailleGrid) String() string {
	var b strings.Builder
	b.Grow(g.rows * (g.stride()*3 + 1))
	for r := 0; r < g.rows; r++ {
		for _, v := range g.cells[r*g.stride() : (r+1)*g.stride()] {
			b.WriteRune(0x2800 + rune(v))
		}
		if r < g.rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
