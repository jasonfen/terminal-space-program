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
		Dim:     lipgloss.NewStyle(),
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

// TestPorkchopOptionsSubmenu: `o` opens the transfer-options sub-menu;
// n/r/b inside flip nRev / retrograde / longBranch; enter/o/esc closes
// the menu. Verifies key dispatch + visible state changes; does not
// re-solve the grid (no world).
func TestPorkchopOptionsSubmenu(t *testing.T) {
	p := NewPorkchop(Theme{Dim: lipgloss.NewStyle(), Warning: lipgloss.NewStyle()})
	p.depDays = []float64{0}
	p.tofDays = []float64{100}
	p.grid = [][]float64{{1000}}

	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	p.HandleKey(key("o"))
	if !p.optsOpen {
		t.Fatal("`o` should open the options sub-menu")
	}
	p.HandleKey(key("n"))
	if p.opts.NRev != 1 {
		t.Errorf("`n` did not advance NRev: got %d, want 1", p.opts.NRev)
	}
	p.HandleKey(key("r"))
	if !p.opts.Retrograde {
		t.Error("`r` did not toggle Retrograde on")
	}
	p.HandleKey(key("b"))
	if !p.opts.LongBranch {
		t.Error("`b` did not toggle LongBranch on")
	}
	// nRev wraps: 1 → 2 → 3 → 0.
	for i := 0; i < porkchopMaxNRev; i++ {
		p.HandleKey(key("n"))
	}
	if p.opts.NRev != 0 {
		t.Errorf("nRev did not wrap back to 0 at %d cycles past 0: got %d", porkchopMaxNRev, p.opts.NRev)
	}
	// `o` closes the menu (and would re-solve the grid if world were set).
	p.HandleKey(key("o"))
	if p.optsOpen {
		t.Error("`o` should close the options sub-menu")
	}
}

// TestPorkchopPendingPlantCarriesOptions: a plant from within the
// options-driven state surfaces those options to the caller so
// PlanTransferAt uses the same Lambert params the cell was scored at.
func TestPorkchopPendingPlantCarriesOptions(t *testing.T) {
	p := NewPorkchop(Theme{Warning: lipgloss.NewStyle()})
	p.depDays = []float64{0}
	p.tofDays = []float64{200}
	p.grid = [][]float64{{1500}}
	p.opts = sim.TransferOptions{NRev: 2, Retrograde: true, LongBranch: true}
	p.targetIdx = 4

	enterKey := tea.KeyMsg{Type: tea.KeyEnter}
	if _, done := p.HandleKey(enterKey); !done {
		t.Fatal("Enter on a feasible cell should signal done=true")
	}
	tgt, depD, tofD, opts, ok := p.PendingPlant()
	if !ok {
		t.Fatal("PendingPlant ok=false after Enter on feasible cell")
	}
	if tgt != 4 || depD != 0 || tofD != 200 {
		t.Errorf("plant target/cell mismatch: tgt=%d dep=%v tof=%v", tgt, depD, tofD)
	}
	if opts.NRev != 2 || !opts.Retrograde || !opts.LongBranch {
		t.Errorf("plant did not carry opts forward: got %+v", opts)
	}
}