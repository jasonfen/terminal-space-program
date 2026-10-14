package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
)

// ADR 0053 slice F: the DOCKING page and the docking text added to other F1
// pages are read at the 140x40 floor, where a row is 2 + 20 + text cells and
// the box takes 138 and a row at 138 touches the border, so these
// rows stay at 136 or under. A longer row is ellipsised mid-sentence, or touches the
// border, so these rows (written fresh for docking) must show in full.
func TestHelpDockingRowsFitDesignSizeUncut(t *testing.T) {
	h := NewHelp(chipTestTheme())
	touched := map[string]map[string]bool{
		"CAMERA & VIEW": {"o": true},
		"TIME & WARP":   {".": true, "warp caps": true},
		"VESSEL":        {"U": true, "U (cross-player)": true},
	}
	checked := 0
	for _, s := range helpSections {
		all := strings.HasPrefix(s.header, "DOCKING")
		for _, r := range s.rows {
			if !all && !touched[s.header][r[0]] {
				continue
			}
			checked++
			line := ansi.Strip(h.renderRows(keylayout.QWERTY, [][2]string{r})[0])
			if w := len([]rune(line)); w > 136 {
				t.Errorf("%s / %q is %d cells, want at most 136 (a row at 138 touches the right border):\n%s", s.header, r[0], w, line)
			}
		}
	}
	if checked < 12 {
		t.Errorf("checked only %d rows: the guard lost its rows (DOCKING page missing?)", checked)
	}
}
