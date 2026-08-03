package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// riderViewTheme mirrors chipTestTheme but is exported-local to this file
// (kept separate from the other render smoke tests so it's obvious this
// one is meant to be read, not just asserted against).
func riderViewTheme() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

// TestDockGuestRenderLooksRight is a "render and actually look at it" check
// (ADR 0038 S4): a real full-frame Render call with a DockGuest + ghost
// fixture, live and empty-seat, printed via -v so a mangled column (a
// width-2 glyph, a broken border, an overlapping chip) is visible to a
// human reviewer rather than only passing a substring assertion. Also
// pins the structural invariants a substring check could miss: the DOCKED
// block and the VESSEL/ORBIT panels must all appear, and the canvas must
// not blow past its requested width (padChipBlock/overlayStyledBlock
// mismatches show up as a wildly long line).
func TestDockGuestRenderLooksRight(t *testing.T) {
	v := NewOrbitView(riderViewTheme())
	v.Resize(200, 60)

	for _, tc := range []struct {
		name   string
		online bool
	}{
		{"live-owner", true},
		{"empty-seat", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := dockGuestStackGhostWorld(t)
			w.Session = &sim.SessionInfo{Players: []sim.SessionPlayer{
				{Fingerprint: w.DockGuest.OwnerFP, Handle: w.DockGuest.OwnerHandle, Online: tc.online},
			}}

			out := v.Render(w, 0, 200, 60)
			t.Logf("=== DockGuest render (%s) ===\n%s", tc.name, out)

			for _, want := range []string{"DOCKED", "riding in bob's stack", "VESSEL", "bob's stack"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s render missing %q", tc.name, want)
				}
			}
			if tc.online {
				if !strings.Contains(out, "[J] request control") || !strings.Contains(out, "[u] ask to undock") {
					t.Errorf("%s render missing the live-owner exits", tc.name)
				}
			} else {
				if !strings.Contains(out, "take the stick") {
					t.Errorf("%s render missing the empty-seat exit", tc.name)
				}
			}

			for i, line := range strings.Split(out, "\n") {
				if width := lipgloss.Width(line); width > 220 {
					t.Errorf("%s render line %d is %d cells wide (canvas requested at 200) — a chip likely overran: %q", tc.name, i, width, line)
				}
			}
		})
	}
}
