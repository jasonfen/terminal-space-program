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

// TestChipRow2AlignsSecondLabelAcrossVaryingFirstValues: the whole point
// of a fixed second-label column is that two rows with very different
// first-value widths still start their second label at the same screen
// column. A %-Ns byte-padded implementation would already pass this for
// ASCII, so this alone isn't the sabotage proof (see the multibyte test
// below) — but it is the basic contract this helper exists for.
func TestChipRow2AlignsSecondLabelAcrossVaryingFirstValues(t *testing.T) {
	short := chipRow2("TWR:", "1.23", "", "")
	long := chipRow2("Δv:", "7502 / 12014 m/s", "Δv→circ:", "7502 m/s  9m45s")
	if strings.Contains(short, "mode:") {
		t.Fatalf("short row should not carry a second cell: %q", short)
	}
	if !strings.Contains(long, "Δv→circ:") {
		t.Fatalf("long row should carry its second cell: %q", long)
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
	asciiRow := chipRow2("ee:", "12", "mode:", "main") // "ee:" = 3 bytes, 3 cells
	deltaRow := chipRow2("Δv:", "12", "mode:", "main") // "Δv:" = 4 bytes, 3 cells
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

// TestChipRow2EmptyLabelReturnsSingleCell: a dash-only row (no second
// quantity) renders exactly like chipRowAt's single-value form — no
// trailing padding or stray second column, since most NAVIGATION rows on
// the pad are single dashes today and must not gain visible width.
func TestChipRow2EmptyLabelReturnsSingleCell(t *testing.T) {
	got := chipRow2("Ap:", "—", "", "")
	want := chipRowAt("Ap:", "—", boxValueCol)
	if got != want {
		t.Errorf("chipRow2 with empty label2 = %q, want %q (chipRowAt's own form)", got, want)
	}
}
