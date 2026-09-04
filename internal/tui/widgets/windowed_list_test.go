package widgets

import "testing"

func plainLines(n int) []WindowLine {
	lines := make([]WindowLine, n)
	for i := range lines {
		lines[i] = WindowLine{Text: itoa(i)}
	}
	return lines
}

func itoa(i int) string {
	// Avoid pulling in strconv just for test fixtures.
	digits := "0123456789"
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestBoundsCursorAtTop(t *testing.T) {
	start, end := Bounds(20, 0, 6)
	if start != 0 {
		t.Fatalf("start = %d, want 0", start)
	}
	if end-start != 6 {
		t.Fatalf("window size = %d, want 6", end-start)
	}
	if !(0 >= start && 0 < end) {
		t.Fatalf("cursor 0 not in [%d,%d)", start, end)
	}
}

func TestBoundsCursorInMiddle(t *testing.T) {
	start, end := Bounds(20, 10, 6)
	if end-start != 6 {
		t.Fatalf("window size = %d, want 6", end-start)
	}
	if !(10 >= start && 10 < end) {
		t.Fatalf("cursor 10 not in [%d,%d)", start, end)
	}
	// Centered: roughly equal slack on both sides.
	if start != 7 || end != 13 {
		t.Fatalf("got [%d,%d), want [7,13) for a centered cursor", start, end)
	}
}

func TestBoundsCursorAtBottom(t *testing.T) {
	start, end := Bounds(20, 19, 6)
	if end != 20 {
		t.Fatalf("end = %d, want 20", end)
	}
	if end-start != 6 {
		t.Fatalf("window size = %d, want 6", end-start)
	}
	if !(19 >= start && 19 < end) {
		t.Fatalf("cursor 19 not in [%d,%d)", start, end)
	}
}

func TestBoundsWindowLargerThanList(t *testing.T) {
	start, end := Bounds(5, 2, 50)
	if start != 0 || end != 5 {
		t.Fatalf("got [%d,%d), want [0,5) when the budget exceeds the list", start, end)
	}
}

func TestBoundsNonPositiveBudgetShowsEverything(t *testing.T) {
	start, end := Bounds(5, 2, 0)
	if start != 0 || end != 5 {
		t.Fatalf("got [%d,%d), want [0,5) for a non-positive budget", start, end)
	}
	start, end = Bounds(5, 2, -3)
	if start != 0 || end != 5 {
		t.Fatalf("got [%d,%d), want [0,5) for a negative budget", start, end)
	}
}

func TestWindowCursorAtTopShowsBelowMarkerOnly(t *testing.T) {
	rendered := Window(plainLines(20), 0, 6)
	if rendered[0].Kind != LineContent || rendered[0].Index != 0 {
		t.Fatalf("first rendered line = %+v, want the cursor's own row first", rendered[0])
	}
	last := rendered[len(rendered)-1]
	if last.Kind != LineMoreBelow {
		t.Fatalf("last line kind = %v, want LineMoreBelow", last.Kind)
	}
	for _, r := range rendered {
		if r.Kind == LineMoreAbove {
			t.Fatalf("unexpected LineMoreAbove when cursor is at the top: %+v", rendered)
		}
	}
}

func TestWindowCursorAtBottomShowsAboveMarkerOnly(t *testing.T) {
	rendered := Window(plainLines(20), 19, 6)
	first := rendered[0]
	if first.Kind != LineMoreAbove {
		t.Fatalf("first line kind = %v, want LineMoreAbove", first.Kind)
	}
	for _, r := range rendered {
		if r.Kind == LineMoreBelow {
			t.Fatalf("unexpected LineMoreBelow when cursor is at the bottom: %+v", rendered)
		}
	}
	// The cursor's own row must actually be present.
	found := false
	for _, r := range rendered {
		if r.Kind == LineContent && r.Index == 19 {
			found = true
		}
	}
	if !found {
		t.Fatalf("cursor row (index 19) missing from rendered output: %+v", rendered)
	}
}

func TestWindowMiddleShowsBothMarkers(t *testing.T) {
	rendered := Window(plainLines(20), 10, 6)
	if rendered[0].Kind != LineMoreAbove {
		t.Fatalf("first line kind = %v, want LineMoreAbove", rendered[0].Kind)
	}
	if rendered[len(rendered)-1].Kind != LineMoreBelow {
		t.Fatalf("last line kind = %v, want LineMoreBelow", rendered[len(rendered)-1].Kind)
	}
	found := false
	for _, r := range rendered {
		if r.Kind == LineContent && r.Index == 10 {
			found = true
		}
	}
	if !found {
		t.Fatalf("cursor row (index 10) missing from rendered output: %+v", rendered)
	}
}

func TestWindowLargerThanListReturnsEverythingUnwindowed(t *testing.T) {
	lines := plainLines(5)
	rendered := Window(lines, 2, 50)
	if len(rendered) != 5 {
		t.Fatalf("len(rendered) = %d, want 5 (no markers, nothing hidden)", len(rendered))
	}
	for i, r := range rendered {
		if r.Kind != LineContent || r.Index != i {
			t.Fatalf("rendered[%d] = %+v, want LineContent at index %d", i, r, i)
		}
	}
}

func TestWindowNonPositiveBudgetReturnsEverything(t *testing.T) {
	lines := plainLines(5)
	rendered := Window(lines, 2, 0)
	if len(rendered) != 5 {
		t.Fatalf("len(rendered) = %d, want 5 for a non-positive budget", len(rendered))
	}
}

// TestWindowPinsScrolledOffHeader is the header-pinning case: a category
// header several rows above the visible window must reappear as a pinned
// leading line once the window scrolls past it, so a player deep in a
// long section never loses the label for what they're looking at.
func TestWindowPinsScrolledOffHeader(t *testing.T) {
	lines := []WindowLine{
		{Text: "Header A", IsHeader: true},
		{Text: "a0"}, {Text: "a1"}, {Text: "a2"}, {Text: "a3"}, {Text: "a4"},
		{Text: "a5"}, {Text: "a6"}, {Text: "a7"}, {Text: "a8"}, {Text: "a9"},
	}
	// cursor deep into the "a" section, window small enough that index 0
	// (the header) scrolls out of view.
	rendered := Window(lines, 9, 3)
	if len(rendered) == 0 {
		t.Fatalf("got no rendered lines")
	}
	first := rendered[0]
	if first.Kind != LinePinnedHeader || first.Text != "Header A" {
		t.Fatalf("first line = %+v, want the pinned \"Header A\" header", first)
	}
}

// TestWindowNoPinWhenHeaderAlreadyVisible confirms the header is not
// duplicated when it's already inside the window (cursor near the top of
// its section).
func TestWindowNoPinWhenHeaderAlreadyVisible(t *testing.T) {
	lines := []WindowLine{
		{Text: "Header A", IsHeader: true},
		{Text: "a0"}, {Text: "a1"}, {Text: "a2"}, {Text: "a3"},
	}
	rendered := Window(lines, 1, 3)
	pins := 0
	for _, r := range rendered {
		if r.Kind == LinePinnedHeader {
			pins++
		}
	}
	if pins != 0 {
		t.Fatalf("got %d pinned headers, want 0 when the header is already in the window: %+v", pins, rendered)
	}
}

// TestWindowPinFollowsSectionCrossing checks that the pinned header tracks
// whichever section actually owns the top visible row, not just the first
// header in the list — the second section's header pins once the window
// crosses into it.
func TestWindowPinFollowsSectionCrossing(t *testing.T) {
	lines := []WindowLine{
		{Text: "Header A", IsHeader: true},
		{Text: "a0"}, {Text: "a1"},
		{Text: "Header B", IsHeader: true},
		{Text: "b0"}, {Text: "b1"}, {Text: "b2"}, {Text: "b3"}, {Text: "b4"},
	}
	// cursor on b3 (index 7), small window entirely inside section B but
	// past Header B's own line (index 3).
	rendered := Window(lines, 7, 2)
	if rendered[0].Kind != LinePinnedHeader || rendered[0].Text != "Header B" {
		t.Fatalf("first line = %+v, want the pinned \"Header B\" header", rendered[0])
	}
}

func TestTextsExtractsRenderedText(t *testing.T) {
	rendered := Window(plainLines(5), 2, 50)
	texts := Texts(rendered)
	want := []string{"0", "1", "2", "3", "4"}
	if len(texts) != len(want) {
		t.Fatalf("len(texts) = %d, want %d", len(texts), len(want))
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Fatalf("texts[%d] = %q, want %q", i, texts[i], want[i])
		}
	}
}

func TestWindowEmptyInput(t *testing.T) {
	if rendered := Window(nil, 0, 5); rendered != nil {
		t.Fatalf("Window(nil, ...) = %+v, want nil", rendered)
	}
}
