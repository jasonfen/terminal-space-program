package widgets

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// expandRunsToPerChar undoes the #364 run-length coalescing: every
// "open-code + N-rune content + \x1b[0m" group becomes N separate
// "open-code + 1 rune + \x1b[0m" groups, the exact shape String()
// produced before #364. Uncolored spans (no ESC) pass through
// unchanged. Used so the regression tests below can assert the
// coalesced output is byte-for-byte equivalent to the old per-cell
// output once un-batched — i.e. the SAME characters get the SAME
// color, just wrapped in fewer, larger escape sequences.
func expandRunsToPerChar(s string) string {
	const esc = "\x1b"
	const reset = "\x1b[0m"
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == esc[0] {
			j := i
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j >= len(s) {
				out.WriteString(s[i:])
				break
			}
			openCode := s[i : j+1]
			rest := s[j+1:]
			resetIdx := strings.Index(rest, reset)
			if resetIdx < 0 {
				out.WriteString(s[i:])
				break
			}
			content := rest[:resetIdx]
			for _, r := range content {
				out.WriteString(openCode)
				out.WriteRune(r)
				out.WriteString(reset)
			}
			i = j + 1 + resetIdx + len(reset)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		out.WriteRune(r)
		i += size
	}
	return out.String()
}

// referenceString reproduces the pre-#364 per-cell String() behavior —
// one independent lipgloss.NewStyle().Foreground(fg).Render(string(ch))
// call per colored, non-space cell, with no run-length coalescing — so
// the #364 optimization can be checked for byte-for-byte equivalence
// against exactly the code path it replaced.
func (c *Canvas) referenceString() string {
	rows := c.dc.Rows(0, 0, c.pxW, c.pxH)
	if len(c.pixelTags) == 0 && len(c.cellOverlays) == 0 {
		return c.joinRows(rows)
	}
	cellColor := make(map[[2]int]lipgloss.Color)
	cellCounts := make(map[[2]int]map[lipgloss.Color]int)
	for coord, tag := range c.pixelTags {
		if tag.Color == "" {
			continue
		}
		cellX, cellY := coord[0]/2, coord[1]/4
		key := [2]int{cellX, cellY}
		if cellCounts[key] == nil {
			cellCounts[key] = make(map[lipgloss.Color]int)
		}
		cellCounts[key][tag.Color]++
	}
	for key, counts := range cellCounts {
		cellColor[key] = pickDominantColor(counts)
	}
	var b strings.Builder
	for i := 0; i < c.rows; i++ {
		var line string
		if i < len(rows) {
			line = rows[i]
		}
		runes := []rune(line)
		for x := 0; x < c.cols; x++ {
			var ch rune = ' '
			if x < len(runes) {
				ch = runes[x]
			}
			if overlay, ok := c.cellOverlays[[2]int{x, i}]; ok {
				ch = overlay
			}
			color, hasColor := cellColor[[2]int{x, i}]
			var fg lipgloss.TerminalColor = color
			if oc, ok := c.cellOverlayColors[[2]int{x, i}]; ok {
				fg, hasColor = oc, true
			}
			if hasColor && ch != ' ' {
				b.WriteString(lipgloss.NewStyle().Foreground(fg).Render(string(ch)))
			} else {
				b.WriteRune(ch)
			}
		}
		if i < c.rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// TestStringCoalescedMatchesPerCellReference is the #364 regression
// guard: the run-length-coalesced String() must produce byte-for-byte
// identical output to the pre-#364 per-cell implementation for a scene
// with multiple distinctly-colored regions, gaps, and cell overlays —
// exactly the shapes a real orbit-screen frame paints (a filled body
// disk next to unrelated background, a differently-colored overlay
// label). Any divergence here is a real visual regression, not a
// harmless implementation-detail change.
func TestStringCoalescedMatchesPerCellReference(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1") // force lipgloss to emit ANSI in tests

	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.Center(orbital.Vec3{})

	// A filled "body" disk (long same-color runs).
	c.FillColoredDiskTagged(orbital.Vec3{X: -20}, 8, CellTag{Color: "#5FD7FF", BodyID: "test-body"})
	// A second, differently-colored disk overlapping the canvas edge
	// (forces off-canvas clipping + a second distinct color run).
	c.FillColoredDiskTagged(orbital.Vec3{X: 25, Y: 10}, 5, CellTag{Color: "#FF5F5F"})
	// A cell-overlay label in a third color, sitting partly on top of
	// untagged background.
	c.SetCellLabelColored(2, 2, "view: top", lipgloss.Color("#87FF87"))

	got := c.String()
	want := c.referenceString()
	// Un-batch got's coalesced runs back to one escape-sequence pair per
	// glyph — the exact shape the pre-#364 reference always produced —
	// so this compares "same character gets the same color" rather than
	// demanding an identical (and now pointlessly less efficient) byte
	// stream.
	if expanded := expandRunsToPerChar(got); expanded != want {
		t.Errorf("coalesced String() diverges from the per-cell reference once un-batched\ngot (expanded): %q\nwant:           %q", expanded, want)
	}
}

// TestStringCoalescedMatchesReferenceAcrossPalette exercises a range of
// small scenes (varying disk radius and color count) to catch a
// coalescing bug that a single fixed scene might not trigger — e.g. an
// off-by-one at a run boundary that only shows up at certain widths.
func TestStringCoalescedMatchesReferenceAcrossPalette(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")

	colors := []lipgloss.Color{"#FF0000", "#00FF00", "#0000FF", "#FFFF00"}
	for _, r := range []int{1, 3, 6, 10, 15} {
		for _, color := range colors {
			c := NewCanvas(30, 15)
			c.SetScale(1)
			c.Center(orbital.Vec3{})
			c.FillColoredDiskTagged(orbital.Vec3{}, r, CellTag{Color: color})
			got := c.String()
			want := c.referenceString()
			if expanded := expandRunsToPerChar(got); expanded != want {
				t.Errorf("r=%d color=%s: coalesced String() diverges from reference once un-batched\ngot (expanded): %q\nwant:           %q", r, color, expanded, want)
			}
		}
	}
}
