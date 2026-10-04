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
