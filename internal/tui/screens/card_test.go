package screens

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func testRows() []CardRow {
	return []CardRow{
		{ID: "a", Label: "do it", Key: "y", Letters: []string{"y", "Y"}},
		{ID: "b", Label: "do it hard", Key: "", Letters: nil},
		{ID: "out", Label: "cancel", Key: "n  esc", Letters: []string{"n", "N", "esc"}, Out: true},
	}
}

func TestCardHeightAndWidth(t *testing.T) {
	th := sessionTheme()
	for _, tc := range []struct {
		name  string
		note  string
		rows  []CardRow
		wantH int
	}{
		{"three rows no note", "", testRows(), 9},
		{"two rows no note", "", testRows()[:1:1], 9},
		{"two rows one note line", "short note", append(testRows()[:1:1], testRows()[2]), 9},
		{"three rows two note lines", "a note that is long enough to wrap onto a second line here", testRows(), 11},
		{"note cut to two lines", strings.Repeat("word ", 40), testRows(), 11},
	} {
		c := NewCard(th, "Title?", tc.note, tc.rows, 0)
		lines := strings.Split(c.Render(), "\n")
		if len(lines) != tc.wantH || c.Height() != tc.wantH {
			t.Errorf("%s: %d lines, Height %d, want %d:\n%s", tc.name, len(lines), c.Height(), tc.wantH, c.Render())
		}
		for i, ln := range lines {
			if w := lipgloss.Width(ln); w != CardW {
				t.Errorf("%s: line %d is %d wide: %q", tc.name, i, w, ln)
			}
		}
		// The second step keeps the height.
		c.Pick(0)
		if got := len(strings.Split(c.Render(), "\n")); got != tc.wantH {
			t.Errorf("%s: second step is %d lines, want %d", tc.name, got, tc.wantH)
		}
	}
}

// HitRow must agree with where Render drew the rows: for every visible row,
// the line its label is on, read back from the rendered text, hits that row.
func TestCardHitRowMatchesRender(t *testing.T) {
	th := sessionTheme()
	for _, note := range []string{"", "one line", "a note that is long enough to wrap onto a second line here"} {
		c := NewCard(th, "Title?", note, testRows(), 0)
		for step := 0; step < 2; step++ {
			lines := strings.Split(ansi.Strip(c.Render()), "\n")
			for vi, r := range c.visible() {
				want := -1
				for li, ln := range lines {
					if li > 1 && strings.Contains(ln, r.Label) {
						want = li
						break
					}
				}
				if want < 0 {
					t.Fatalf("note %q step %d: label %q not drawn", note, step, r.Label)
				}
				got, ok := c.HitRow(5, want)
				if !ok || got != vi {
					t.Errorf("note %q step %d: HitRow on the line of %q = %d,%v want %d", note, step, r.Label, got, ok, vi)
				}
				if _, ok := c.HitRow(0, want); ok {
					t.Errorf("a click on the border column hit %q", r.Label)
				}
				if _, ok := c.HitRow(CardW-1, want); ok {
					t.Errorf("a click on the right border hit %q", r.Label)
				}
			}
			if _, ok := c.HitRow(5, 0); ok {
				t.Errorf("a click on the top edge hit a row")
			}
			if _, ok := c.HitRow(5, len(lines)-1); ok {
				t.Errorf("a click on the bottom edge hit a row")
			}
			c.Pick(0)
		}
	}
}

func TestCardTwoStepPick(t *testing.T) {
	c := NewCard(sessionTheme(), "T?", "", testRows(), 0)
	if row, done := c.Enter(); done || row != 0 || !c.Confirming() {
		t.Fatalf("enter on an action row: row %d done %v confirming %v, want 0 false true", row, done, c.Confirming())
	}
	if c.Choice() != 2 {
		t.Errorf("the second step must sit on the way out (row 2), is on %d", c.Choice())
	}
	if row, done := c.Enter(); !done || row != 2 {
		t.Errorf("enter on the way out: row %d done %v, want 2 true", row, done)
	}

	// A letterless row has no letter on the first step, but its second step is
	// the deliberate one: y confirms it, like every card (F4).
	c = NewCard(sessionTheme(), "T?", "", testRows(), 0)
	c.Down()
	if _, done := c.Enter(); done {
		t.Fatal("the letterless action row acted at once")
	}
	if row, done, ok := c.Letter("y"); !ok || !done || row != 1 {
		t.Errorf("y on the letterless row's confirm step: row %d done %v ok %v, want 1 true true", row, done, ok)
	}
	c = NewCard(sessionTheme(), "T?", "", testRows(), 0)
	c.Down()
	c.Enter()
	c.Up()
	if row, done := c.Enter(); !done || row != 1 {
		t.Errorf("enter on 'yes, do it hard': row %d done %v, want 1 true", row, done)
	}

	// F4: the second step's keys do not depend on the row picked. After an n
	// pick, n is the way out and y confirms.
	for _, pick := range []string{"y", "n"} {
		rows := []CardRow{
			{ID: "a", Label: "save", Key: "y", Letters: []string{"y", "Y"}},
			{ID: "b", Label: "discard", Key: "n", Letters: []string{"n", "N"}},
			{ID: "out", Label: "stay", Key: "esc", Letters: []string{"esc"}, Out: true},
		}
		c = NewCard(sessionTheme(), "T?", "", rows, 0)
		want := map[string]int{"y": 0, "n": 1}[pick]
		if row, done, ok := c.Letter(pick); !ok || done || row != want {
			t.Fatalf("first %s: row %d done %v ok %v", pick, row, done, ok)
		}
		out := ansi.Strip(c.Render())
		if !strings.Contains(out, "yes, "+rows[want].Label) || !strings.Contains(out, "n  esc") || !regexp.MustCompile(`yes, \w+ +y\b`).MatchString(out) {
			t.Errorf("confirm step after %s lacks the y / n  esc key column:\n%s", pick, out)
		}
		if row, done, ok := c.Letter("n"); !ok || !done || row != 2 {
			t.Errorf("%s then n: row %d done %v ok %v, want the way out (2) at once", pick, row, done, ok)
		}
		c = NewCard(sessionTheme(), "T?", "", rows, 0)
		c.Letter(pick)
		if row, done, ok := c.Letter("y"); !ok || !done || row != want {
			t.Errorf("%s then y: row %d done %v ok %v, want %d true true", pick, row, done, ok, want)
		}
	}

	// The way out acts at once by letter, from the first step.
	c = NewCard(sessionTheme(), "T?", "", testRows(), 0)
	if row, done, ok := c.Letter("esc"); !ok || !done || row != 2 {
		t.Errorf("esc: row %d done %v ok %v, want 2 true true", row, done, ok)
	}
	// y, y acts; an unknown key matches nothing.
	c = NewCard(sessionTheme(), "T?", "", testRows(), 0)
	if _, done, ok := c.Letter("y"); !ok || done {
		t.Errorf("first y: done %v ok %v, want false true", done, ok)
	}
	if row, done, ok := c.Letter("y"); !ok || !done || row != 0 {
		t.Errorf("second y: row %d done %v ok %v", row, done, ok)
	}
	if _, _, ok := c.Letter("q"); ok {
		t.Error("q matched a row")
	}
}

func TestCardUpDownWrap(t *testing.T) {
	c := NewCard(sessionTheme(), "T?", "", testRows(), 0)
	c.Up()
	if c.Choice() != 2 {
		t.Errorf("up from the top = %d, want 2", c.Choice())
	}
	c.Down()
	if c.Choice() != 0 {
		t.Errorf("down from the bottom = %d, want 0", c.Choice())
	}
}

func TestCardOpensOnTheRowItIsGiven(t *testing.T) {
	c := NewCard(sessionTheme(), "T?", "", testRows(), 2)
	if c.Choice() != 2 || !strings.Contains(ansi.Strip(c.Render()), "▸ cancel") {
		t.Errorf("a card opened on row 2 sits on %d:\n%s", c.Choice(), ansi.Strip(c.Render()))
	}
}

// The ▸ row's label is the Primary colour and no other row's is. Forced
// profile: lipgloss renders no colour under go test, so without it this
// assertion would pass on any output.
func TestCardCursorRowIsPrimary(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	primary := lipgloss.NewStyle().Foreground(lipgloss.Color("#00D7FF"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#5F5F5F"))
	th := sessionTheme()
	th.Primary, th.Dim = primary, dim
	if primary.Render("x") == dim.Render("x") {
		t.Fatal("setup: colour is off")
	}
	c := NewCard(th, "T?", "", testRows(), 1)
	var onCursor, offCursor int
	for _, ln := range strings.Split(c.Render(), "\n") {
		for _, lbl := range []string{"do it hard", "cancel"} {
			if !strings.Contains(ansi.Strip(ln), lbl) {
				continue
			}
			if strings.Contains(ln, primary.Render(lbl)) {
				if lbl != "do it hard" {
					t.Errorf("the row %q is Primary but the ▸ is on row 1", lbl)
				}
				onCursor++
			} else {
				offCursor++
			}
		}
	}
	if onCursor != 1 || offCursor != 1 {
		t.Errorf("Primary labels = %d, other labels = %d, want 1 and 1", onCursor, offCursor)
	}
}

func TestQuitCardRows(t *testing.T) {
	th := sessionTheme()
	host := NewQuitCard(th, false)
	out := ansi.Strip(host.Render())
	for _, w := range []string{"Save before quitting?", "save and quit", "quit without saving", "stay", "▸ save and quit"} {
		if !strings.Contains(out, w) {
			t.Errorf("host quit card lacks %q:\n%s", w, out)
		}
	}
	guest := ansi.Strip(NewQuitCard(th, true).Render())
	if strings.Contains(guest, "without saving") || !strings.Contains(guest, "Your flight saves automatically.") {
		t.Errorf("guest quit card:\n%s", guest)
	}
	if _, _, ok := NewQuitCard(th, true).Letter("n"); ok {
		t.Error("n answers a guest's quit card")
	}
}

// A key column never touches the right edge, and a row with no key can run to
// the edge, so the longest verb (a destructive extra row, one to four words)
// reads whole in its second step.
func TestCardKeyColumnAndLongVerb(t *testing.T) {
	rows := []CardRow{
		{ID: "k", Label: "kick", Key: "y", Letters: []string{"y"}},
		{ID: "kd", Label: "kick and delete vessels"},
		{ID: "out", Label: "cancel", Key: "n  esc", Letters: []string{"n", "esc"}, Out: true},
	}
	c := NewCard(sessionTheme(), "Kick pilot2?", "", rows, 0)
	for step := 0; step < 2; step++ {
		for i, ln := range strings.Split(ansi.Strip(c.Render()), "\n") {
			if r := []rune(ln); len(r) == CardW && r[CardW-1] == '│' && i > 1 && i < 8 && r[CardW-2] != ' ' {
				t.Errorf("step %d line %d: text touches the right edge: %q", step, i, ln)
			}
		}
		if step == 0 {
			c.Down()
			c.Enter()
		}
	}
	if out := ansi.Strip(c.Render()); !strings.Contains(out, "yes, kick and delete vessels") {
		t.Errorf("the second step cut the long verb:\n%s", out)
	}
}

// A click points the ▸ at the clicked row (SetCursor) and then sends enter, so
// it answers through the same handler a key does.
func TestCardSetCursorPointsTheArrow(t *testing.T) {
	c := NewCard(sessionTheme(), "Q?", "", []CardRow{
		{ID: "a", Label: "a", Letters: []string{"y"}},
		{ID: "b", Label: "b"},
		{ID: "c", Label: "c", Out: true},
	}, 0)
	c.SetCursor(1)
	if got := c.Choice(); got != 1 {
		t.Fatalf("SetCursor(1): Choice = %d", got)
	}
	c.SetCursor(9) // out of range: ignored
	c.SetCursor(-1)
	if got := c.Choice(); got != 1 {
		t.Fatalf("out-of-range SetCursor moved the arrow to %d", got)
	}
	if row, done := c.Enter(); row != 1 || done {
		t.Errorf("enter on row b = (%d,%v), want the confirm step", row, done)
	}
}
