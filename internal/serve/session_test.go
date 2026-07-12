package serve

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ansiRE strips CSI, OSC, charset-select, and lone Fe escape sequences
// so tests can assert on the plain text of rendered frames.
var ansiRE = regexp.MustCompile(`\x1b(\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|\([AB012]|[@-Z\\-_])`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// Two session apps in one process are fully independent Worlds:
// warping one leaves the other's clock untouched. This is the headless
// base of the S1 acceptance — the ssh smoke test drives the same
// divergence through real connections.
func TestSessionsRunIndependentWorlds(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	newApp := func() tea.Model {
		t.Helper()
		m, _, err := newSessionApp()
		if err != nil {
			t.Fatalf("newSessionApp: %v", err)
		}
		return m
	}
	a, b := newApp(), newApp()
	if a == b {
		t.Fatal("expected distinct models per session")
	}

	size := tea.WindowSizeMsg{Width: 140, Height: 45}
	a, _ = a.Update(size)
	b, _ = b.Update(size)

	// One warp-up on A only, then tick both.
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	now := time.Now()
	a, _ = a.Update(sim.TickMsg(now))
	b, _ = b.Update(sim.TickMsg(now))

	av, bv := stripANSI(a.View()), stripANSI(b.View())
	if !strings.Contains(av, "warp 10x") {
		t.Errorf("session A: expected 'warp 10x' in frame after WarpUp, got clock chip:\n%s", firstLines(av, 3))
	}
	if strings.Contains(bv, "warp 10x") {
		t.Errorf("session B: warp leaked across sessions:\n%s", firstLines(bv, 3))
	}
	if !strings.Contains(bv, "warp 1x") {
		t.Errorf("session B: expected untouched 'warp 1x' chip:\n%s", firstLines(bv, 3))
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
