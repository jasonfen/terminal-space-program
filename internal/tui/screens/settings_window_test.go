package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
)

// TestSettingsFitsAndKeepsCursorVisibleAt104x24 is the #373 repro: at
// 104x24 the pre-fix Render emitted the title, the "chips" section header,
// and all three sections' rows unconditionally (33 lines), so after two
// Down presses the cursor scrolled off the top of the pane with no title,
// no header, and zero ">" markers anywhere in the capture
// (features-discoverability-19/20-settings-104x24.txt).
func TestSettingsFitsAndKeepsCursorVisibleAt104x24(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	s.HandleKey("down")
	s.HandleKey("down")

	out := s.Render(settings.Default(), 104, 24)
	lines := strings.Split(out, "\n")
	if len(lines) > 24 {
		t.Errorf("rendered %d lines, want <= 24 (terminal height):\n%s", len(lines), out)
	}
	if !strings.Contains(out, "settings") {
		t.Errorf("title missing from rendered output:\n%s", out)
	}
	if !strings.Contains(out, "chips") {
		t.Errorf("the active section header (\"chips\") is missing from rendered output:\n%s", out)
	}
	if !strings.Contains(out, "> ") {
		t.Errorf("cursor marker (\"> \") missing from rendered output:\n%s", out)
	}
	if !strings.Contains(out, settings.AllChips[2].Label()) {
		t.Errorf("the highlighted chip (%q) is missing from rendered output:\n%s", settings.AllChips[2].Label(), out)
	}
	if !strings.Contains(out, "[esc] back") {
		t.Errorf("footer missing from rendered output:\n%s", out)
	}
}

// TestSettingsWindowActuallyHidesRowsAtFloor confirms the body is really
// windowed (not just short) at the floor: far fewer chip rows render than
// settings.AllChips holds.
func TestSettingsWindowActuallyHidesRowsAtFloor(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	out := s.Render(settings.Default(), 104, 24)
	shown := 0
	for _, c := range settings.AllChips {
		if strings.Contains(out, c.Label()) {
			shown++
		}
	}
	if shown >= len(settings.AllChips) {
		t.Errorf("windowed render shows %d of %d chip labels, want far fewer:\n%s", shown, len(settings.AllChips), out)
	}
}

// TestSettingsWindowFollowsCursorIntoGameplaySection checks the pinned
// section header tracks the cursor across sections: once the cursor walks
// past the chip rows onto the gameplay toggles, the "gameplay" header
// (not "chips") must be the one visible.
func TestSettingsWindowFollowsCursorIntoGameplaySection(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	for i := 0; i < len(settings.AllChips); i++ {
		s.HandleKey("down")
	}
	out := s.Render(settings.Default(), 104, 24)
	if !strings.Contains(out, "gameplay") {
		t.Errorf("expected the \"gameplay\" section header pinned once the cursor reaches the gameplay rows:\n%s", out)
	}
	if !strings.Contains(out, "Tutorial") {
		t.Errorf("cursor row (Tutorial) missing from rendered output:\n%s", out)
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
