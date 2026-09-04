package widgets

import "fmt"

// WindowLine is one line of content in a scrollable list panel: either a
// pinnable section/category header or an ordinary content row (a
// selectable item, a blank spacer, a description — anything the caller
// wants to keep in the scroll).
//
// #373: the spawn form and Settings screen each grew a selectable list
// (the CRAFT TYPE catalog, the chip-toggle rows) tall enough to scroll the
// cursor, the screen title, or the footer off the top of the terminal with
// no windowing at all. This is the shared fix (ADR 0046's Consequences
// section): a list panel windows itself around the cursor instead of
// emitting every line.
type WindowLine struct {
	Text     string
	IsHeader bool
}

// LineKind classifies one line of a Window result.
type LineKind int

const (
	// LineContent is a line taken directly from the input, at
	// RenderedLine.Index.
	LineContent LineKind = iota
	// LinePinnedHeader is a section header (IsHeader==true in the input, at
	// RenderedLine.Index) that has itself scrolled out of the visible
	// window and is pinned as an extra line above it, so the section a
	// player has scrolled into never loses its label.
	LinePinnedHeader
	// LineMoreAbove is a "▲ N more" marker; RenderedLine.Count is N.
	LineMoreAbove
	// LineMoreBelow is a "▼ N more" marker; RenderedLine.Count is N.
	LineMoreBelow
)

// RenderedLine is one line of a Window result.
type RenderedLine struct {
	Text  string
	Kind  LineKind
	Index int // input index into the lines Window was called with; valid for LineContent / LinePinnedHeader
	Count int // hidden-line count; valid for LineMoreAbove / LineMoreBelow
}

// Bounds returns the [start, end) window into an n-item list that keeps
// cursor visible, sized to at most rowBudget items — centered on cursor
// where the list is long enough to need windowing, clamped to the list's
// ends otherwise. rowBudget <= 0, or a rowBudget that already covers the
// whole list, returns the full range (0, n). cursor is clamped into
// [0, n) before computing the window, so any cursor value is safe to pass.
func Bounds(n, cursor, rowBudget int) (start, end int) {
	if n <= 0 {
		return 0, 0
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= n {
		cursor = n - 1
	}
	if rowBudget <= 0 || rowBudget >= n {
		return 0, n
	}
	start = cursor - rowBudget/2
	if start < 0 {
		start = 0
	}
	end = start + rowBudget
	if end > n {
		end = n
		start = end - rowBudget
	}
	if start < 0 {
		start = 0
	}
	return start, end
}

// Window computes a scrollable list panel's visible content: the
// [start, end) slice of lines from Bounds, a leading "▲ N more" marker
// when rows are hidden above, a trailing "▼ N more" marker when rows are
// hidden below, and — when the header that owns the first visible line has
// itself scrolled out of view — that header pinned as an extra leading
// line.
//
// cursor is an index into lines (need not itself be a header line); it is
// clamped the same way Bounds clamps it. An empty lines returns nil.
func Window(lines []WindowLine, cursor, rowBudget int) []RenderedLine {
	n := len(lines)
	if n == 0 {
		return nil
	}
	start, end := Bounds(n, cursor, rowBudget)

	var out []RenderedLine
	if hdrIdx := ownerHeaderIndex(lines, start); hdrIdx >= 0 && hdrIdx < start {
		out = append(out, RenderedLine{Text: lines[hdrIdx].Text, Kind: LinePinnedHeader, Index: hdrIdx})
	}
	if start > 0 {
		out = append(out, RenderedLine{Text: fmt.Sprintf("▲ %d more", start), Kind: LineMoreAbove, Count: start})
	}
	for i := start; i < end; i++ {
		out = append(out, RenderedLine{Text: lines[i].Text, Kind: LineContent, Index: i})
	}
	if end < n {
		out = append(out, RenderedLine{Text: fmt.Sprintf("▼ %d more", n-end), Kind: LineMoreBelow, Count: n - end})
	}
	return out
}

// ownerHeaderIndex returns the index of the nearest header line at or
// before i, or -1 when no header precedes it.
func ownerHeaderIndex(lines []WindowLine, i int) int {
	for j := i; j >= 0; j-- {
		if lines[j].IsHeader {
			return j
		}
	}
	return -1
}

// Texts extracts the rendered text of each line, in order — for a caller
// (like a plain list with no click targets) that only needs the finished
// lines and not the Kind/Index detail Window otherwise carries.
func Texts(rendered []RenderedLine) []string {
	out := make([]string, len(rendered))
	for i, r := range rendered {
		out[i] = r.Text
	}
	return out
}
