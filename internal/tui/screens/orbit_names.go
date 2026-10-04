package screens

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Names on the wide map (UX cycle 3 slice B9, #506, grill G7 Q2 + Q4;
// ADR 0041 §1 amendment 2026-10-04).
//
// The star and planets carry a dim one-word name while the camera is
// System-wide ("g"), moons never at that zoom (they fold into their
// planet's cell, a moon label would only mislabel the planet), and the
// Target body keeps its name at every zoom. A name goes beside its body,
// right then left then above then below, and only where every cell is
// clear: other names, body disks, markers and vessels, the instrument
// boxes, the navball, and the bottom row (the view label and Hint Strip
// live there). When nothing fits the body stays unnamed and `j` Inspect
// still answers for it. A wrong or clipped name is worse than none.

// nameLabel is one name placed on the canvas this frame.
type nameLabel struct {
	Name     string
	Col, Row int
}

// nameCandidate is a body that wants a name, in priority order.
type nameCandidate struct {
	idx  int
	r    int // drawn disk radius, px
	px   int // projected pixel position
	py   int
	name string
}

// cellRect is an inclusive canvas-cell rectangle.
type cellRect struct{ c0, c1, r0, r1 int }

func (r cellRect) contains(col, row int) bool {
	return col >= r.c0 && col <= r.c1 && row >= r.r0 && row <= r.r1
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// wideMapNameCandidates lists the bodies that want a name this frame:
// the Target body first (any zoom), then, at System-wide focus, the star
// and planets in catalog order. Only bodies that project onto the canvas.
func (v *OrbitView) wideMapNameCandidates(w *sim.World, scale float64, canvasReach int) []nameCandidate {
	sys := w.System()
	want := make([]int, 0, len(sys.Bodies))
	targetIdx := -1
	if w.Target.Kind == sim.TargetBody && w.Target.BodyIdx > 0 && w.Target.BodyIdx < len(sys.Bodies) {
		targetIdx = w.Target.BodyIdx
		// A targeted moon that folds into its planet's dot (its own disk is
		// hidden under the planet's glyph) cannot carry a name of its own
		// there: "Moon" beside Earth's dot mislabels Earth, and it used to
		// take Earth's name spot (wave C review MEDIUM 52). The planet whose
		// dot it is takes the Target's slot instead; the TARGET chip names
		// the moon.
		if tb := sys.Bodies[targetIdx]; tb.BodyType == "Moon" && foldsIntoParent(&sys, tb, w.BodyPosition(tb), w, scale) {
			for i, b := range sys.Bodies {
				if b.ID == tb.ParentID {
					targetIdx = i
					break
				}
			}
		}
		want = append(want, targetIdx)
	}
	if w.Focus.Kind == sim.FocusSystem {
		for i, b := range sys.Bodies {
			if b.BodyType == "Moon" || i == targetIdx {
				continue
			}
			want = append(want, i)
		}
	}
	var out []nameCandidate
	for _, i := range want {
		b := sys.Bodies[i]
		px, py, ok := v.canvas.Project(w.BodyPosition(b))
		if !ok {
			continue
		}
		out = append(out, nameCandidate{
			idx: i, r: mapBodyPixelRadius(b, i == 0, scale, canvasReach),
			px: px, py: py, name: b.EnglishName,
		})
	}
	return out
}

// chipLayoutKey hashes everything the chip layout reads: the canvas size,
// the navball reservation, and each chip's shape (id, corner, tier, line
// widths of the full and compact forms). Bay chips also hash their text,
// since the bay wraps by word. Chip text that only changes digits (a clock,
// an altitude) keeps the same key, so the replay runs on a layout change,
// not on every frame. FNV-1a, inline, allocation-free.
func chipLayoutKey(chips []builtChip, cCols, cRows, navballReserved int) uint64 {
	h := uint64(14695981039346656037)
	mix := func(n uint64) {
		h ^= n
		h *= 1099511628211
	}
	mixStr := func(s string) {
		for i := 0; i < len(s); i++ {
			mix(uint64(s[i]))
		}
		mix(0xff)
	}
	mix(uint64(cCols))
	mix(uint64(cRows))
	mix(uint64(navballReserved))
	for _, c := range chips {
		mixStr(string(c.id))
		mix(uint64(c.corner))
		mix(uint64(c.tier))
		if c.neverShrink {
			mix(1)
		}
		mix(uint64(len(c.lines)))
		for _, ln := range c.lines {
			mix(uint64(ansi.StringWidth(ln)))
			if c.corner == cornerBay {
				mixStr(ln)
			}
		}
		mix(uint64(len(c.compact)) + 1<<32)
		for _, ln := range c.compact {
			mix(uint64(ansi.StringWidth(ln)))
		}
	}
	return h
}

// blockedByInstruments returns the canvas-cell rectangles the HUD will
// cover after the canvas string is built: every chip box and the navball
// panel. Chips are composited from strings after the canvas is drawn, so
// the layout is replayed here onto a blank canvas (same chips, same sizes,
// zero screen offset) to learn where they land. composeChips resets and
// refills v.chipRects; the real composition later in Render does it again.
// The rectangles are cached on chipLayoutKey (wave C review, LOW 53): the
// replay was +0.28 ms and +9.1k allocations per frame at "g".
func (v *OrbitView) blockedByInstruments(w *sim.World, chips []builtChip, cCols, cRows int) []cellRect {
	navballReserved := v.navballReservedRows(w, cCols, cRows)
	key := chipLayoutKey(chips, cCols, cRows, navballReserved)
	if v.blockedCacheOK && v.blockedCacheKey == key && v.blockedCacheDecl == v.declutter {
		return v.blockedCache
	}
	v.nameReplays++
	var out []cellRect
	blank := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", cCols)+"\n", cRows), "\n")
	composed := v.composeChips(blank, cCols, cRows, navballReserved, 0, 0, chips)
	for _, r := range v.chipRects {
		out = append(out, cellRect{r.colStart, r.colEnd, r.rowStart, r.rowEnd})
	}
	// Hidden Stubs and the bay's fold line have no rect; ink the dry
	// composition left outside every box is blocked too, one rect per
	// contiguous run.
	boxes := len(out)
	inBox := func(col, row int) bool {
		for _, r := range out[:boxes] {
			if r.contains(col, row) {
				return true
			}
		}
		return false
	}
	for row, ln := range strings.Split(composed, "\n") {
		run := -1
		for col, ch := range []rune(ansi.Strip(ln)) {
			ink := ch != ' ' && !inBox(col, row)
			switch {
			case ink && run < 0:
				run = col
			case !ink && run >= 0:
				out = append(out, cellRect{run, col - 1, row, row})
				run = -1
			}
		}
		if run >= 0 {
			out = append(out, cellRect{run, cCols - 1, row, row})
		}
	}
	if !v.declutter && navballReserved > chipStubHeight {
		g := navballGeometry(cCols, cRows)
		out = append(out, cellRect{cCols - g.panelW, cCols - 1, cRows - g.panelH - 1, cRows - 2})
	}
	v.blockedCache, v.blockedCacheKey, v.blockedCacheDecl, v.blockedCacheOK = out, key, v.declutter, true
	return out
}

// paintBodyNames stamps the names onto the canvas, after every map layer
// and before the canvas string is built. It records what it placed in
// v.nameLabels (tests and captures read it).
func (v *OrbitView) paintBodyNames(w *sim.World, chips []builtChip, scale float64, canvasReach int) {
	v.nameLabels = v.nameLabels[:0]
	v.nameDropped = v.nameDropped[:0]
	if v.declutter {
		return // F2 clears standing overlays; names are standing ink
	}
	cands := v.wideMapNameCandidates(w, scale, canvasReach)
	if len(cands) == 0 {
		return
	}
	cCols, cRows := v.canvas.Cols(), v.canvas.Rows()
	blocked := v.blockedByInstruments(w, chips, cCols, cRows)
	sys := w.System()
	placed := map[[2]int]bool{}

	free := func(col, row, n int) bool {
		if row < 0 || row >= cRows-1 { // never the bottom row
			return false
		}
		for c := col; c < col+n; c++ {
			if c < 0 || c >= cCols || v.canvas.CellOccupied(c, row) {
				return false
			}
			// Names keep one empty cell between them (diagonals too): two
			// names touching read as one, or as the wrong body's.
			for dr := -1; dr <= 1; dr++ {
				for dc := -1; dc <= 1; dc++ {
					if placed[[2]int{c + dc, row + dr}] {
						return false
					}
				}
			}
			for _, r := range blocked {
				if r.contains(c, row) {
					return false
				}
			}
		}
		return true
	}
	// bodyCell maps each non-moon body's own cell to its index, so a name is
	// never placed flush against a DIFFERENT body (it would read as that
	// body's name: "Sun" abutting Earth's marker).
	bodyCell := map[[2]int][]int{}
	for i, b := range sys.Bodies {
		if b.BodyType == "Moon" {
			continue
		}
		if px, py, ok := v.canvas.Project(w.BodyPosition(b)); ok {
			k := [2]int{px / 2, py / 4}
			bodyCell[k] = append(bodyCell[k], i)
		}
	}
	touchesOther := func(self, col, row int) bool {
		for _, i := range bodyCell[[2]int{col, row}] {
			if i != self {
				return true
			}
		}
		return false
	}
	for _, cd := range cands {
		n := lipgloss.Width(cd.name)
		cx := cd.px / 2
		row := cd.py / 4
		// near is the cell just on the body's side of a side-by-side name
		// (-1 for above/below, where a crowd of bodies shares the row).
		type spot struct{ col, row, nearCol, nearRow int }
		right := floorDiv(cd.px+cd.r, 2) + 1
		left := floorDiv(cd.px-cd.r, 2) - n
		above := floorDiv(cd.py-cd.r, 4) - 1
		below := floorDiv(cd.py+cd.r, 4) + 1
		spots := []spot{
			{right, row, right - 1, row}, // right
			{left, row, left + n, row},   // left
			{cx - n/2, above, -1, -1},    // above
			{cx - n/2, below, -1, -1},    // below
		}
		ok := false
		for _, s := range spots {
			if !free(s.col, s.row, n) || touchesOther(cd.idx, s.nearCol, s.nearRow) {
				continue
			}
			v.canvas.SetCellLabelColored(s.col, s.row, cd.name, dimBodyColor(sys.Bodies[cd.idx]))
			for c := s.col; c < s.col+n; c++ {
				placed[[2]int{c, s.row}] = true
			}
			v.nameLabels = append(v.nameLabels, nameLabel{cd.name, s.col, s.row})
			ok = true
			break
		}
		if !ok {
			v.nameDropped = append(v.nameDropped, cd.name)
		}
	}
}

// dimBodyColor is the body's palette colour pulled 30% toward the dim
// grey, so a name reads as the body's own and stays quieter than any
// instrument text (clarity 60: no third grey). Non-hex colours pass
// through unchanged.
func dimBodyColor(b bodies.CelestialBody) lipgloss.TerminalColor {
	c := render.ColorFor(b)
	s := string(c)
	if len(s) != 7 || s[0] != '#' {
		return c
	}
	var ch [3]int
	for i := 0; i < 3; i++ {
		x, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return c
		}
		ch[i] = int(float64(x)*0.7 + 95*0.3)
	}
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", ch[0], ch[1], ch[2]))
}
