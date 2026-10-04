package screens

import (
	"os"
	"strings"
	"testing"
)

// TestChromeRulesAreInF1AndControlsDoc (B11 / G9 Q3, Q6, Q7): the back rule,
// the pause card that really pauses, and the orientation cue are player-
// facing behaviour, so F1 (the source of truth) and docs/controls.md (its
// mirror) must both state them.
func TestChromeRulesAreInF1AndControlsDoc(t *testing.T) {
	var f1 strings.Builder
	for _, sec := range helpSections {
		f1.WriteString(sec.header + "\n")
		for _, row := range sec.rows {
			f1.WriteString(row[0] + " " + row[1] + "\n")
		}
	}
	raw, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatalf("read controls.md: %v", err)
	}
	for name, text := range map[string]string{"F1": f1.String(), "docs/controls.md": string(raw)} {
		for _, want := range []string{
			"returns to the menu",  // Q3: menu-opened screens go back to the menu
			"returns to the map",   // Q3: key-opened screens go back to the map
			"PAUSED",               // Q6: the menu stops the clock and the bar says so
			"N ⊙",                  // Q7: the cue vocabulary
			"open your orbit ring", // Q7 follow-up
			"67° open",             // Q7 follow-up
			"as it was",            // Q6: closing hands the clock back
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not state %q", name, want)
			}
		}
	}
}
