package screens

// One-line reason for a new test file: the card it covers is a new prod file.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/settings"
)

// Each row flips with its letter or with space/enter on the cursor; esc and y
// close; every rendered line is exactly the card width (glyph-free but boxed).
func TestSurfaceMarksCardKeysAndWidth(t *testing.T) {
	c := NewSurfaceMarksCard(Theme{})
	for _, r := range surfaceMarkRows {
		if chip, ok, _ := c.HandleKey(r.letter); !ok || chip != r.chip {
			t.Errorf("letter %q flipped (%q, %v), want %q", r.letter, chip, ok, r.chip)
		}
	}
	c.Open()
	if chip, ok, _ := c.HandleKey(" "); !ok || chip != settings.ChipMarksLanded {
		t.Errorf("space on row 0 flipped (%q, %v)", chip, ok)
	}
	c.HandleKey("down")
	if chip, _, _ := c.HandleKey("enter"); chip != settings.ChipMarksFlags {
		t.Errorf("enter after one down flipped %q, want flags", chip)
	}
	c.Open()
	c.HandleKey("up") // wraps to the last row
	if chip, _, _ := c.HandleKey(" "); chip != settings.ChipCoverageNow {
		t.Errorf("up from row 0 landed on %q, want coverage.now", chip)
	}
	for _, k := range []string{"esc", "f3"} {
		if _, ok, closed := c.HandleKey(k); ok || !closed {
			t.Errorf("%q: flip=%v closed=%v, want a close", k, ok, closed)
		}
	}
	out := c.Render(func(ch settings.Chip) bool { return ch != settings.ChipMarksWrecks })
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != CardW {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, CardW, ln)
		}
	}
	if !strings.Contains(out, "[ ] wrecks") || !strings.Contains(out, "[x] flags") {
		t.Errorf("render does not show wrecks off and flags on:\n%s", out)
	}
}

// Sites sharing a cell are one Inspect stop named `N sites · first …`; a lone
// site is named plainly (the equal case).
func TestSurfaceMarksInspectNamesACell(t *testing.T) {
	v := NewOrbitView(Theme{})
	v.Resize(200, 60)
	w := marksWorld(t, 60)
	// Everything within a hair of one point (a longitude that faces the camera).
	var names []string
	for _, lon := range []float64{0, 90, 180, -90} {
		for i := range w.Sites {
			w.Sites[i].LatDeg, w.Sites[i].LonDeg = 0, lon+float64(i)*1e-6
		}
		v.inspectables = nil
		marksFrame(v, w)
		names = nil
		for _, it := range v.inspectables {
			if it.ref.Kind == InspectSite {
				names = append(names, it.name)
			}
		}
		if len(names) > 0 {
			break
		}
	}
	if len(names) != 1 || !strings.HasPrefix(names[0], "60 sites · s1 ") {
		t.Fatalf("stack of 60 = %q, want one stop `60 sites · s1 …`", names)
	}
	w.Sites = w.Sites[:1]
	v.inspectables = nil
	marksFrame(v, w)
	got := ""
	for _, it := range v.inspectables {
		if it.ref.Kind == InspectSite {
			got = it.name
		}
	}
	if got != "s1" {
		t.Errorf("one site named %q, want plain s1", got)
	}
}
