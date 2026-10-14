package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
	"github.com/jasonfen/terminal-space-program/internal/sim"
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

// The numbers the DOCKING page and docs/controls.md quote are the sim's own
// constants, not copies: a retuned gate or magnet must go red here until the
// words are updated (the ADR's own text was wrong in several places, so the
// docs are checked against the code, not the ADR).
func TestHelpDockingPageQuotesTheSimConstants(t *testing.T) {
	var page helpSection
	for _, s := range helpSections {
		if strings.HasPrefix(s.header, "DOCKING") {
			page = s
		}
	}
	if len(page.rows) == 0 {
		t.Fatal("no DOCKING page")
	}
	row := func(token string) string {
		for _, r := range page.rows {
			if r[0] == token {
				return r[1]
			}
		}
		t.Fatalf("DOCKING page has no %q row", token)
		return ""
	}
	gate := row("the gate")
	for _, want := range []string{
		fmt.Sprintf("%.1f m", sim.DockPortRangeM),
		fmt.Sprintf("%.1f m/s", sim.DockingVMS),
		fmt.Sprintf("%.0f°", sim.DockAlignDeg),
	} {
		if !strings.Contains(gate, want) {
			t.Errorf("gate row %q does not quote %q", gate, want)
		}
	}
	if want := fmt.Sprintf("%.0f-second", sim.DockMagnetDuration.Seconds()); !strings.Contains(row("magnet"), "five-second") || sim.DockMagnetDuration.Seconds() != 5 {
		t.Errorf("magnet row %q no longer matches DockMagnetDuration %v (want %s)", row("magnet"), sim.DockMagnetDuration, want)
	}
	if want := fmt.Sprintf("%.2f m/s", sim.UndockSpringMS); !strings.Contains(row("U"), want) {
		t.Errorf("U row %q does not quote the Spring Release %s", row("U"), want)
	}
	if want := fmt.Sprintf("re-approach under %.2f m/s", sim.DockingVMS); !strings.Contains(row("bump (caption)"), want) {
		t.Errorf("bump caption row %q does not quote %q", row("bump (caption)"), want)
	}
}
