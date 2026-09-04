package screens

import (
	"strings"
	"testing"
)

// TestVABFooterWrapsAndKeepsSaveOpenAt104Columns is the #373 repro for the
// VAB's second footer row: at 104 columns the un-wrapped 132-char string
// ("[+/−] qty  ...  [s] save  [o] open  [esc] back") ran off the pane and
// was cut off mid-token, hiding "[s] save" and "[o] open" entirely — the
// only on-screen way to learn how to keep a build (features-discoverability
// -22-vab-104x24.txt). The fix wraps the footer to as many rows as the
// width needs instead of truncating.
func TestVABFooterWrapsAndKeepsSaveOpenAt104Columns(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	out := v.Render(104)

	for _, ln := range strings.Split(out, "\n") {
		if len([]rune(ln)) > 104 {
			t.Errorf("rendered line exceeds 104 columns (%d): %q", len([]rune(ln)), ln)
		}
	}
	if !strings.Contains(out, "[s] save") {
		t.Errorf("footer is missing \"[s] save\" at 104 columns:\n%s", out)
	}
	if !strings.Contains(out, "[o] open") {
		t.Errorf("footer is missing \"[o] open\" at 104 columns:\n%s", out)
	}
	if !strings.Contains(out, "[esc] back") {
		t.Errorf("footer is missing \"[esc] back\" at 104 columns:\n%s", out)
	}
}

// TestVABFooterFitsOnOneRowWhenWide confirms the wrap is a real word-wrap,
// not an unconditional split: at a comfortably wide terminal both footer
// hint rows stay on one physical line each (2 lines total), matching the
// pre-#373 layout.
func TestVABFooterFitsOnOneRowWhenWide(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	out := v.Render(160)
	lines := strings.Split(out, "\n")

	footerRows := 0
	for _, ln := range lines {
		if strings.Contains(ln, "[tab] column") || strings.Contains(ln, "[+/") {
			footerRows++
		}
	}
	if footerRows != 2 {
		t.Errorf("got %d footer key-hint rows at width 160, want 2 (one per hint row):\n%s", footerRows, out)
	}
}

// TestWrapFooterHintsKeepsHintsWhole checks the wrap unit directly: a hint
// ("[key] description") must never be split across two lines — only
// gaps between hints are wrap points.
func TestWrapFooterHintsKeepsHintsWhole(t *testing.T) {
	s := "[a] add  [b] bravo  [c] charlie  [d] delta"
	for _, w := range []int{1, 5, 8, 12, 20, 100} {
		lines := wrapFooterHints(s, w)
		joined := strings.Join(lines, "  ")
		if joined != s {
			t.Errorf("wrap at width %d dropped or reordered hints: got %q, want %q", w, joined, s)
		}
		for _, ln := range lines {
			if ln == "" {
				t.Errorf("wrap at width %d produced an empty line", w)
			}
		}
	}
}

func TestWrapFooterHintsEmptyInput(t *testing.T) {
	if got := wrapFooterHints("", 80); got != nil {
		t.Errorf("wrapFooterHints(\"\", 80) = %v, want nil", got)
	}
	if got := wrapFooterHints("hi", 0); got != nil {
		t.Errorf("wrapFooterHints with w<=0 = %v, want nil", got)
	}
}
