package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
	if !strings.Contains(p.TitleContext(), "Mars") {
		t.Errorf("Title Row context didn't mention target name 'Mars': %q", p.TitleContext())
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

// TestPorkchopHitCellLandsOnTheDrawnCell (B11 / G9 Q4): the grid now sits in
// a frame and a box, which moved every cell. The selected cell is drawn as
// "█" (the cursor); the click position of that glyph, read from the actual
// render, must hit-test to the selection.
func TestPorkchopHitCellLandsOnTheDrawnCell(t *testing.T) {
	// The cursor is the only Warning-coloured cell; under go test lipgloss
	// is colourless unless the profile is forced.
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })
	p := NewPorkchop(Theme{Warning: lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAF00"))})
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	marsIdx := -1
	for i, b := range w.System().Bodies {
		if b.EnglishName == "Mars" {
			marsIdx = i
		}
	}
	if marsIdx < 0 {
		t.Skip("Mars not in Sol system")
	}
	p.Load(w, marsIdx)
	p.SetSelection(7, 3)
	cursor := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAF00")).Render("█")
	found := false
	for row, ln := range strings.Split(p.Render(w, 140, 39), "\n") {
		i := strings.Index(ln, cursor)
		if i < 0 || !strings.Contains(ln, "tof ") {
			continue
		}
		found = true
		col := lipgloss.Width(ln[:i])
		dep, tof, ok := p.HitCell(col, row)
		if !ok || dep != 7 || tof != 3 {
			t.Errorf("click on the drawn cursor cell (col %d, row %d) = (%d,%d,%v), want (7,3,true)", col, row, dep, tof, ok)
		}
	}
	if !found {
		t.Fatal("no cursor cell found in the render")
	}
}

// B11 review H1: the plot options left the Title Row (it overflowed 140
// columns), so the body must still say what the grid is scoring.
func TestPorkchopBodyStillNamesTheScoredOptions(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	p := NewPorkchop(Theme{Title: lipgloss.NewStyle(), Footer: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(), Alert: lipgloss.NewStyle(), Dim: lipgloss.NewStyle(), Primary: lipgloss.NewStyle()})
	p.Load(w, 4)
	out := p.Render(w, 140, 38)
	if !strings.Contains(out, "rev=0 prograde") {
		t.Errorf("porkchop body does not name the scored options:\n%s", out)
	}
}
