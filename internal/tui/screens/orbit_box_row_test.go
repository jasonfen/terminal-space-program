// ADR 0051 slice 2a: chipRow2, the two-quantities-per-row helper decision
// 4 requires ("altitude:  0 m               vert:   0.00 m/s"). Sabotage-
// first: these pin the behaviour a naive %-Ns-padded implementation gets
// wrong (byte-vs-display-cell width with multibyte labels/values).

package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// testRowCols is a generous, box-agnostic column set for these
// mechanism-level tests (they exercise chipRow2/chipRow3 directly, not
// any specific instrument box's own calibrated boxCols), sized wide
// enough to fit every literal used in this file without triggering the
// overflow-push clamp.
var testRowCols = boxCols{label2: 34, gap2: 10, label3: 52, gap3: 6}

// TestChipRow2AlignsSecondLabelAcrossVaryingFirstValues: the whole point
// of a fixed second-label column is that two rows with very different
// first-value widths still start their second label at the SAME screen
// column, so a box's second cells read as one vertical column of labels
// rather than drifting per row. This is the actual regression the
// coordinator found in the live captures (label2 following "row + two
// spaces" instead of a fixed column): sabotage-checked below by
// reverting to that shape and confirming this goes red.
func TestChipRow2AlignsSecondLabelAcrossVaryingFirstValues(t *testing.T) {
	short := chipRow2(testRowCols, "TWR:", "1.23", "mode:", "main")
	long := chipRow2(testRowCols, "Δv:", "7502 / 12014 m/s", "Δv→circ:", "7502 m/s  9m45s")
	shortCol := strings.Index(short, "mode:")
	longCol := strings.Index(long, "Δv→circ:")
	if shortCol < 0 || longCol < 0 {
		t.Fatalf("second label missing: short=%q long=%q", short, long)
	}
	shortColW := lipgloss.Width(short[:shortCol])
	longColW := lipgloss.Width(long[:longCol])
	if shortColW != longColW {
		t.Errorf("second label lands at different columns depending on the first value's width: %q (col %d) vs %q (col %d) — the second label must be pinned to a fixed column, not follow the first value plus two spaces",
			short, shortColW, long, longColW)
	}

	noSecond := chipRow2(testRowCols, "TWR:", "1.23", "", "")
	if strings.Contains(noSecond, "mode:") {
		t.Fatalf("row with no second cell should not carry one: %q", noSecond)
	}
}

// TestChipRow2SecondLabelColumnIsDisplayWidthAware: the label "Δv:" is 2
// display cells wide (Δ + v) but 3 bytes wide (Δ is a 2-byte UTF-8
// rune). A byte-counted %-Ns pad would put the second label one column
// further right than a lipgloss.Width-based pad for this row versus an
// all-ASCII row of otherwise identical visible length. This test proves
// the helper uses display width, not byte length, by checking two rows
// whose first cells have equal DISPLAY width but different BYTE length
// land their second label at the exact same column.
func TestChipRow2SecondLabelColumnIsDisplayWidthAware(t *testing.T) {
	asciiRow := chipRow2(testRowCols, "ee:", "12", "mode:", "main") // "ee:" = 3 bytes, 3 cells
	deltaRow := chipRow2(testRowCols, "Δv:", "12", "mode:", "main") // "Δv:" = 4 bytes, 3 cells
	asciiCol := strings.Index(asciiRow, "mode:")
	deltaCol := strings.Index(deltaRow, "mode:")
	if asciiCol < 0 || deltaCol < 0 {
		t.Fatalf("second label missing: ascii=%q delta=%q", asciiRow, deltaRow)
	}
	// The byte index differs (Δv: costs one extra byte) but the DISPLAY
	// column must match: measure via lipgloss.Width on the prefix before
	// "mode:" in each row.
	asciiPrefixWidth := lipgloss.Width(asciiRow[:asciiCol])
	deltaPrefixWidth := lipgloss.Width(deltaRow[:deltaCol])
	if asciiPrefixWidth != deltaPrefixWidth {
		t.Errorf("second label lands at different DISPLAY columns: ascii prefix width %d, delta prefix width %d (ascii=%q delta=%q)",
			asciiPrefixWidth, deltaPrefixWidth, asciiRow, deltaRow)
	}
}

// TestChipRow3AlignsThirdLabelAcrossVaryingFirstAndSecondValues: the
// same fixed-column guarantee chipRow2 gives its second cell must hold
// for chipRow3's third cell too, regardless of how wide the first OR
// second cells are in a given row (TARGET's range/closing/rel and
// Ap/Pe/incl rows, NAVIGATION's depart/e/dir row).
func TestChipRow3AlignsThirdLabelAcrossVaryingFirstAndSecondValues(t *testing.T) {
	short := chipRow3(testRowCols, "Pe:", "—", "lead:", "—", "dir:", "prograde")
	long := chipRow3(testRowCols, "depart:", "49.42° (best 5.17°)", "e:", "0.9973", "dir:", "retrograde")
	shortCol := strings.Index(short, "dir:")
	longCol := strings.Index(long, "dir:")
	if shortCol < 0 || longCol < 0 {
		t.Fatalf("third label missing: short=%q long=%q", short, long)
	}
	shortColW := lipgloss.Width(short[:shortCol])
	longColW := lipgloss.Width(long[:longCol])
	if shortColW != longColW {
		t.Errorf("third label lands at different columns depending on the first/second values' width: %q (col %d) vs %q (col %d)",
			short, shortColW, long, longColW)
	}
}

// TestChipRow2EmptyLabelReturnsSingleCell: a dash-only row (no second
// quantity) renders exactly like chipRowAt's single-value form, no
// trailing padding or stray second column, since most NAVIGATION rows on
// the pad are single dashes today and must not gain visible width.
func TestChipRow2EmptyLabelReturnsSingleCell(t *testing.T) {
	got := chipRow2(testRowCols, "Ap:", "—", "", "")
	want := chipRowAt("Ap:", "—", boxValueCol)
	if got != want {
		t.Errorf("chipRow2 with empty label2 = %q, want %q (chipRowAt's own form)", got, want)
	}
}
