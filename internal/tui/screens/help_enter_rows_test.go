package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
)

// GK Q6 (#589): F1 says what Enter does on every screen that uses it, and
// no key label overflows F1's 20-cell key column or cuts its row at 140x40.

func TestHelpKeyLabelsFitTheKeyColumnAndRowsFitDesignSize(t *testing.T) {
	// Every label fits the 20-cell key column.
	for _, s := range helpSections {
		for _, r := range s.rows {
			if w := lipgloss.Width(r[0]); w > 20 {
				t.Errorf("%s: key label %q is %d cells, the key column is 20", s.header, r[0], w)
			}
		}
	}
	// The rows this slice owns are never cut at 140x40 (2 + 20 + text <= 138).
	owned := map[string]bool{
		"SPAWN FORM (n)": true, "VEHICLE ASSEMBLY (VAB)": true,
		"SAVES (menu → Save / Load Game)": true,
	}
	touched := map[string]bool{"H": true, "enter (any screen)": true, "F1 / ?": true, "F1 (in a box)": true,
		"in Settings": true, "in Keyboard layout": true, "enter (invite box)": true, "x (what it does)": true,
		"asks first": true, "d": true, "; (self-switch)": true, "; (to a target)": true, "h": true, "i / r / x": true, "F4": true, "F2": true}
	h := NewHelp(chipTestTheme())
	for _, s := range helpSections {
		for _, r := range s.rows {
			if !owned[s.header] && !touched[r[0]] {
				continue
			}
			line := ansi.Strip(h.renderRows(keylayout.QWERTY, [][2]string{r})[0])
			if w := lipgloss.Width(line); w > 138 {
				t.Errorf("%s / %q is %d cells, want at most 138:\n%s", s.header, r[0], w, line)
			}
			if strings.Contains(r[1], "—") {
				t.Errorf("%s / %q uses an em dash", s.header, r[0])
			}
		}
	}
}
