package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
)

// TestSettingsFitsAndKeepsCursorVisibleAt104x24 is the #373 repro, re-pinned
// for the framed two-column layout (B11 / G9 Q4): at the Playable Floor the
// whole screen (the frame, CHIPS, GAMEPLAY, SAVES, DISPLAY) fits the height
// the App gives it (24 rows minus the Title Row), nothing scrolls, and the
// cursor and the legend are on screen after two Down presses.
func TestSettingsFitsAndKeepsCursorVisibleAt104x24(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	s.HandleKey("down")
	s.HandleKey("down")

	out := s.Render(settings.Default(), 104, 23)
	lines := strings.Split(out, "\n")
	if len(lines) != 23 {
		t.Errorf("rendered %d lines, want exactly 23 (the framed block)", len(lines))
	}
	for _, want := range []string{"CHIPS", "GAMEPLAY", "SAVES", "DISPLAY", "▸ [x] " + settings.AllChips[2].Label(), "Empty readings", "[esc] back"} {
		if !strings.Contains(out, want) {
			t.Errorf("104x24 settings is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "more") {
		t.Errorf("104x24 settings still scrolls (a 'more' marker is drawn):\n%s", out)
	}
}

// TestSettingsRenderUnboundedHeightShowsWholeBody locks in the height<=0
// escape hatch: every existing settings test renders with height 0 and
// expects the full body (all chips + gameplay + saves rows) unwindowed.
func TestSettingsRenderUnboundedHeightShowsWholeBody(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	out := s.Render(settings.Default(), 80, 0)
	for _, c := range settings.AllChips {
		if !strings.Contains(out, c.Label()) {
			t.Errorf("chip %q missing from unbounded-height render", c.Label())
		}
	}
	if !strings.Contains(out, "Autosave interval") {
		t.Errorf("autosave row missing from unbounded-height render:\n%s", out)
	}
}

// TestSettingsWindowsChipsWhenTheyDoNotFit pins review #555 L3: formFrame
// drops body rows past its height without a word, so the CHIPS box lost
// rows silently once the list outgrew the box. Short of room the list must
// window around the cursor with "more" markers, keep the cursor row on
// screen, keep the height exact, and keep click targets on the rows drawn.
func TestSettingsWindowsChipsWhenTheyDoNotFit(t *testing.T) {
	chips := settings.AllChips
	last := chips[len(chips)-1]
	s := NewSettingsScreen(Theme{})
	for i := 0; i < len(chips)-1; i++ {
		s.HandleKey("down")
	}
	const h = 14
	out := stripANSI(s.Render(settings.Default(), 104, h))
	if n := len(strings.Split(out, "\n")); n != h {
		t.Errorf("rendered %d rows, want %d", n, h)
	}
	if !strings.Contains(out, "▸ [x] "+last.Label()) && !strings.Contains(out, "▸ [ ] "+last.Label()) {
		t.Errorf("cursor chip %q is not on screen:\n%s", last.Label(), out)
	}
	if !strings.Contains(out, "▲") || !strings.Contains(out, "more") {
		t.Errorf("rows above the window are hidden with no marker:\n%s", out)
	}
	// The click target of the drawn cursor row must toggle that chip: find
	// its screen row in the frame-relative output.
	rows := strings.Split(out, "\n")
	for y, ln := range rows {
		if strings.Contains(ln, "▸ ") && strings.Contains(ln, last.Label()) {
			act, c := s.HandleClick(frameInset+2, frameInset+y-frameInset)
			if act != SettingsActionToggle || c != last {
				t.Errorf("click on the drawn %q row gave %v %q", last.Label(), act, c)
			}
			return
		}
	}
	t.Errorf("cursor row not found:\n%s", out)
}
