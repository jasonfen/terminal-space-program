package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestPorkchopLoadAndRender: smoke test covering load + render of the
// porkchop screen for an Earth → Mars window. Verifies the rendered
// output contains the target name and at least one glyph from the
// intensity ramp — i.e. some grid cell converged.
func TestPorkchopLoadAndRender(t *testing.T) {
	th := Theme{
		Title:   lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
	}
	p := NewPorkchop(th)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	marsIdx := -1
	for i, b := range w.System().Bodies {
		if b.EnglishName == "Mars" {
			marsIdx = i
			break
		}
	}
	if marsIdx < 0 {
		t.Skip("Mars not in Sol system — adjust if bodies changed")
	}

	p.Load(w, marsIdx)
	out := p.Render(w, 120, 40)
	if !strings.Contains(out, "Mars") {
		t.Errorf("render didn't mention target name 'Mars':\n%s", out)
	}
	hasGlyph := false
	for _, g := range porkchopLegendRamp[:4] { // skip trailing space
		if strings.Contains(out, g) {
			hasGlyph = true
			break
		}
	}
	if !hasGlyph {
		t.Errorf("render didn't include any non-blank intensity glyph (grid may be all-NaN):\n%s", out)
	}
}

// TestPorkchopHandleKeyCursorBounds: arrow keys move the cursor within
// grid bounds; pressing beyond the edge is a no-op, not a panic.
func TestPorkchopHandleKeyCursorBounds(t *testing.T) {
	p := NewPorkchop(Theme{})
	// Synthesise a tiny grid directly — avoids the cost of Lambert
	// solving for a UX test.
	p.depDays = []float64{0, 10}
	p.tofDays = []float64{100, 110}
	p.grid = [][]float64{{1000, 2000}, {3000, 4000}}
	p.selDep, p.selTof = 0, 0

	rightKey := tea.KeyMsg{Type: tea.KeyRight}
	p.HandleKey(rightKey)
	if p.selDep != 1 {
		t.Errorf("right: selDep=%d, want 1", p.selDep)
	}
	p.HandleKey(rightKey)
	if p.selDep != 1 {
		t.Errorf("right at right edge should clamp: selDep=%d, want 1", p.selDep)
	}
}