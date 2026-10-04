package screens

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
)

// TestHelpIndexNumbersMatchDigitJump (review LOW 35): the number printed
// beside each index row is the only thing that tells the player which digit
// to press. For every digit key, the row that carries that number must name
// the page the digit opens.
func TestHelpIndexNumbersMatchDigitJump(t *testing.T) {
	h := NewHelp(chipTestTheme())
	rows := map[string]string{} // printed number -> the rest of the row
	for _, ln := range h.indexLines() {
		plain := strings.TrimSpace(strings.TrimPrefix(strings.TrimLeft(ansi.Strip(ln), " "), "▸"))
		f := strings.Fields(plain)
		if len(f) < 2 {
			continue
		}
		if _, err := strconv.Atoi(f[0]); err == nil {
			rows[f[0]] = strings.TrimSpace(strings.TrimPrefix(plain, f[0]))
		}
	}
	if len(rows) != helpPageCount() {
		t.Fatalf("index shows %d numbered rows, want %d", len(rows), helpPageCount())
	}
	for d := 1; d <= 9; d++ {
		g := NewHelp(chipTestTheme())
		g.HandleKey(helpKey(strconv.Itoa(d)))
		title := pageTitle(d - 1)
		if got := g.PositionLine(); !strings.HasPrefix(got, title+" ·") {
			t.Fatalf("digit %d opened %q, want page %q", d, got, title)
		}
		if row := rows[strconv.Itoa(d)]; !strings.HasPrefix(row, title) {
			t.Errorf("index row numbered %d reads %q, but digit %d opens %q", d, row, d, title)
		}
	}
}

// TestHelpIndexFooterReachesPagesBeyondNine (review LOW 36): pages 10 and up
// have no digit key, so the index footer must say how to reach them, and
// up/down + enter must actually open each one.
func TestHelpIndexFooterReachesPagesBeyondNine(t *testing.T) {
	h := NewHelp(chipTestTheme())
	out := ansi.Strip(h.Render(140, 40, keylayout.QWERTY))
	want := "pages 10-" + strconv.Itoa(helpPageCount())
	if !strings.Contains(out, want) {
		t.Errorf("index footer does not say how to reach pages 10+ (want %q):\n%s", want, out)
	}
	for n := 9; n < helpPageCount(); n++ {
		g := NewHelp(chipTestTheme())
		for i := 0; i < n; i++ {
			g.HandleKey(helpKey("down"))
		}
		g.HandleKey(helpKey("enter"))
		if got := g.PositionLine(); !strings.HasPrefix(got, pageTitle(n)+" ·") {
			t.Errorf("down x%d + enter opened %q, want %q", n, got, pageTitle(n))
		}
	}
}

// TestHelpPlayerTextHasNoEmDashInTouchedStrings (review LOW 37, 121): the
// sticky title and the pan rows carry no em dash.
func TestHelpPlayerTextHasNoEmDashInTouchedStrings(t *testing.T) {
	h := NewHelp(chipTestTheme())
	first := strings.SplitN(ansi.Strip(h.Render(140, 40, keylayout.QWERTY)), "\n", 2)[0]
	if strings.Contains(first, "—") {
		t.Errorf("help title has an em dash: %q", first)
	}
	for _, s := range helpSections {
		for _, r := range s.rows {
			if strings.HasPrefix(r[0], "shift+") && strings.Contains(r[1], "—") {
				t.Errorf("pan row %q has an em dash: %q", r[0], r[1])
			}
		}
	}
}

// TestHelpFirstFlightByHandRowIsTrueAtItsMoment (review LOW 38): the orbit
// start's `b` row used to say "light the engine until you pass 700 km", but
// a transfer burn from 500 km ends before altitude reaches 700 km (the
// vessel coasts up). The row must name the readout that ends the burn (the
// node row's remaining Δv) and keep 700 km as the Flight School finish line.
func TestHelpFirstFlightByHandRowIsTrueAtItsMoment(t *testing.T) {
	var row string
	for _, r := range firstFlight[0].rows {
		if r[0] == "b" {
			row = r[1]
		}
	}
	if row == "" {
		t.Fatal("ORBIT START has no b row")
	}
	if strings.Contains(row, "until you pass 700 km") {
		t.Errorf("b row still claims the engine burns until 700 km: %q", row)
	}
	if !strings.Contains(row, "Δv") {
		t.Errorf("b row does not name the Δv readout that ends the burn: %q", row)
	}
}

// TestHelpPageNamesItselfOnce pins review #555 L4: a page whose body header
// is the same word as the box title (GENERAL) opened with the word twice on
// consecutive rows. On every page the first body row must not repeat the
// box title.
func TestHelpPageNamesItselfOnce(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		for n := 0; n < helpPageCount(); n++ {
			h := NewHelp(chipTestTheme())
			h.OpenPage(n)
			rows := strings.Split(ansi.Strip(h.Render(sz[0], sz[1], keylayout.QWERTY)), "\n")
			// rows: frame top, box top, title, first body row
			title := strings.Trim(rows[3-1], "│ ")
			body := strings.Trim(rows[3], "│ ")
			if title == "" {
				t.Fatalf("page %d: title row not found:\n%s", n, strings.Join(rows[:5], "\n"))
			}
			if body == title {
				t.Errorf("%dx%d page %d: %q is written twice on consecutive rows", sz[0], sz[1], n, title)
			}
		}
	}
}
