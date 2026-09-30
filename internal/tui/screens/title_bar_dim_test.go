package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// forceColour pins the lipgloss profile so style assertions are not
// vacuous under go test's non-TTY stdout.
func forceColour(t *testing.T) {
	t.Helper()
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })
}

// TestTitleBarClockNotDim: the clock and warp rate are read constantly,
// so they must not render in the Dim style (#498).
func TestTitleBarClockNotDim(t *testing.T) {
	forceColour(t)
	th := plainThemeColored()
	v := NewOrbitView(th)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	title := firstLine(v.Render(w, 0, 180, 40))
	chip := "T+" + w.Clock.SimTime.Format("2006-01-02") + "  " + warpField(w)
	if dim := th.Dim.Render(chip); strings.Contains(title, dim) {
		t.Errorf("clock+warp chip rendered in the Dim style: %q", title)
	}
	if !strings.Contains(title, th.Primary.Render(chip)) {
		t.Errorf("clock+warp chip not rendered in Primary: %q", title)
	}
}

// TestTitleBarWidthStableAcrossWarpSteps: changing warp must not shift
// the rest of the title bar (#499).
func TestTitleBarWidthStableAcrossWarpSteps(t *testing.T) {
	v := NewOrbitView(plainTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	seen := map[float64]bool{}
	want, wantBurn := -1, -1
	for i := 0; i < 12; i++ {
		title := firstLine(v.Render(w, 0, 180, 40))
		cur := w.Clock.Warp()
		if !seen[cur] {
			seen[cur] = true
			// Start of the clock chip (what sat left of the warp field
			// and slid when it grew) and of the element after it.
			col := lipgloss.Width(title[:strings.Index(title, "T+")])
			burn := lipgloss.Width(title[:strings.Index(title, "[»Burn]")])
			if want < 0 {
				want = col
				wantBurn = burn
			}
			if col != want || burn != wantBurn || v.burnColStart != wantBurn {
				t.Errorf("warp %vx: clock at col %d, [»Burn] at %d (hit-test %d); want %d and %d", cur, col, burn, v.burnColStart, want, wantBurn)
			}
		}
		w.Clock.WarpUp()
	}
	for _, need := range []float64{1, 10, 1000, 100000} {
		if !seen[need] {
			t.Errorf("warp step %vx never exercised", need)
		}
	}
}

// TestLaunchTitleWidthStableAcrossWarpSteps: same field on the pad.
func TestLaunchTitleWidthStableAcrossWarpSteps(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	want := -1
	for i := 0; i < 12; i++ {
		title := firstLine(v.Render(w, 140, 40))
		col := lipgloss.Width(title[:strings.Index(title, "warp ")])
		if want < 0 {
			want = col
		}
		if col != want {
			t.Errorf("warp %vx: warp field at col %d, want %d", w.Clock.Warp(), col, want)
		}
		w.Clock.WarpUp()
	}
}

// TestHelpMoreBelowCueNotDim: the scroll cue must not use the Footer
// (dimmest) style (#498).
func TestHelpMoreBelowCueNotDim(t *testing.T) {
	forceColour(t)
	th := plainThemeColored()
	th.Footer = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	h := NewHelp(th)
	_ = h.Render(100, 10, keylayout.QWERTY)
	foot := h.footer()
	if !strings.Contains(foot, "▼") {
		t.Fatalf("test setup: expected a more-below cue, got %q", foot)
	}
	if !strings.Contains(foot, th.Primary.Render("▼  ")) {
		t.Errorf("more-below cue not in Primary style: %q", foot)
	}
	if strings.Contains(foot, th.Footer.Render("▼")) {
		t.Errorf("more-below cue rendered in the dim Footer style: %q", foot)
	}
}
