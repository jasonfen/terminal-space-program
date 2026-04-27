package screens

import (
	"fmt"
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Porkchop renders a launch-window Δv heatmap for a Hohmann transfer
// to the target body — grid of departure-day × time-of-flight with
// cells filled by an intensity character reflecting total Δv.
//
// Simplistic ASCII render (no color) to stay portable across terminals
// without touching lipgloss color config. Darker/heavier glyph = lower
// total Δv ("cheaper" transfer). "·" marks infeasible / non-converged.
type Porkchop struct {
	theme        Theme
	targetIdx    int
	targetName   string
	depDays      []float64
	tofDays      []float64
	grid         [][]float64
	selDep       int
	selTof       int
	errMsg       string
	plantPending bool
}

// NewPorkchop constructs an empty screen. Call Load(world, targetIdx)
// before the first render to populate the grid.
func NewPorkchop(th Theme) *Porkchop {
	return &Porkchop{
		theme: th,
	}
}

// Load computes the porkchop grid for the given target body and caches
// it on the screen. Cheap to call once on open; not recomputed per
// frame since the grid doesn't change unless the craft's state or
// target shifts, and the player pauses on this screen to select a
// cell anyway.
func (p *Porkchop) Load(w *sim.World, targetIdx int) {
	p.targetIdx = targetIdx
	p.selDep, p.selTof = 0, 0
	sys := w.System()
	if targetIdx > 0 && targetIdx < len(sys.Bodies) {
		p.targetName = sys.Bodies[targetIdx].EnglishName
	}

	// Default grid: 0–365 dep days step 10, 100–400 tof days step 15.
	// Gives a 37 × 21 grid — readable in a typical 120-col terminal.
	p.depDays = linspace(0, 365, 37)
	p.tofDays = linspace(100, 400, 21)
	grid, err := w.PorkchopGrid(targetIdx, p.depDays, p.tofDays)
	if err != nil {
		p.errMsg = err.Error()
		p.grid = nil
		return
	}
	p.grid = grid
	p.errMsg = ""
	// Center cursor on the min-Δv cell if one exists.
	if d, t, _, ok := porkchopMin(grid); ok {
		p.selDep = d
		p.selTof = t
	}
}

// HandleKey routes arrow keys within the grid. Returns done=true on
// Esc (cancel) or Enter (plant → app reads PendingPlant()) to signal
// the app should return to orbit view. Enter also sets the plant
// flag, checked by the app immediately after done=true.
func (p *Porkchop) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "left":
		if p.selDep > 0 {
			p.selDep--
		}
	case "right":
		if p.selDep < len(p.depDays)-1 {
			p.selDep++
		}
	case "up":
		if p.selTof > 0 {
			p.selTof--
		}
	case "down":
		if p.selTof < len(p.tofDays)-1 {
			p.selTof++
		}
	case "enter":
		if p.grid != nil && p.selTof < len(p.grid) && p.selDep < len(p.grid[p.selTof]) &&
			!math.IsNaN(p.grid[p.selTof][p.selDep]) {
			p.plantPending = true
			return nil, true
		}
	case "esc":
		return nil, true
	}
	return nil, false
}

// PendingPlant returns the selected (targetIdx, depDay, tofDay) if the
// user pressed Enter on a feasible cell; ok=false if no plant is
// pending (Esc-close or infeasible cell). Consumes the pending flag —
// next call returns ok=false until the user plants again.
func (p *Porkchop) PendingPlant() (targetIdx int, depDay, tofDay float64, ok bool) {
	if !p.plantPending {
		return 0, 0, 0, false
	}
	p.plantPending = false
	return p.targetIdx, p.depDays[p.selDep], p.tofDays[p.selTof], true
}

// HitCell maps a screen-space (col, row) onto the porkchop grid's
// (depIdx, tofIdx). Title + blank line take rows 0 and 1; grid
// starts at row 2. Each row begins with "tof XXXd │" = 10 chars
// (gridLead) before the first cell. Returns ok=false when the
// click lands outside the grid's pixel rectangle. v0.6.4 mouse
// dispatch sets selDep / selTof from the result.
func (p *Porkchop) HitCell(col, row int) (depIdx, tofIdx int, ok bool) {
	const gridLead = 10 // matches Render's gridLead constant
	if p.grid == nil {
		return 0, 0, false
	}
	depIdx = col - gridLead
	tofIdx = row - 2
	if depIdx < 0 || depIdx >= len(p.depDays) {
		return 0, 0, false
	}
	if tofIdx < 0 || tofIdx >= len(p.tofDays) {
		return 0, 0, false
	}
	return depIdx, tofIdx, true
}

// SetSelection moves the cursor to (depIdx, tofIdx) — used by the
// v0.6.4 mouse dispatch on click. Bounds-checked; out-of-range
// indices are ignored.
func (p *Porkchop) SetSelection(depIdx, tofIdx int) {
	if depIdx < 0 || depIdx >= len(p.depDays) {
		return
	}
	if tofIdx < 0 || tofIdx >= len(p.tofDays) {
		return
	}
	p.selDep = depIdx
	p.selTof = tofIdx
}

// Render draws the grid + axes + selection readout.
func (p *Porkchop) Render(w *sim.World, cols, rows int) string {
	if p.errMsg != "" {
		return p.theme.Title.Render("porkchop plot") + "\n\n" +
			p.theme.Alert.Render("error: "+p.errMsg) + "\n\n" +
			p.theme.Footer.Render("[esc] back")
	}
	if p.grid == nil {
		return p.theme.Title.Render("porkchop plot") + "\n\n  (grid not loaded)"
	}

	minDv := math.Inf(1)
	maxDv := math.Inf(-1)
	for _, row := range p.grid {
		for _, v := range row {
			if math.IsNaN(v) {
				continue
			}
			if v < minDv {
				minDv = v
			}
			if v > maxDv {
				maxDv = v
			}
		}
	}

	var b strings.Builder
	title := fmt.Sprintf("porkchop plot — Earth → %s", p.targetName)
	b.WriteString(p.theme.Title.Render(title))
	b.WriteString("\n\n")

	// Grid lead-in width: "tof XXXd │" = 4 + 3 + 2 + 1 = 10 chars
	// (e.g. "tof 100d │"). v0.5.14 axis-label fix uses this constant
	// so the dep-day labels under the grid line up properly.
	const gridLead = 10

	// Grid body. Y axis = tof (top = short, bottom = long), X axis = dep.
	for j, tof := range p.tofDays {
		b.WriteString(fmt.Sprintf("tof %3.0fd │", tof))
		for i := range p.depDays {
			if i == p.selDep && j == p.selTof {
				b.WriteString(p.theme.Warning.Render("█"))
				continue
			}
			v := p.grid[j][i]
			b.WriteString(porkchopGlyph(v, minDv, maxDv))
		}
		b.WriteString("│\n")
	}

	// X axis tick line: "└" + dashes under each cell.
	b.WriteString(strings.Repeat(" ", gridLead-1))
	b.WriteString("└")
	b.WriteString(strings.Repeat("─", len(p.depDays)))
	b.WriteString("\n")

	// X axis labels (every 5th column). Each label is exactly 5 chars
	// wide so column alignment with the grid stays correct. Skip
	// labels that would overflow past the grid right edge.
	b.WriteString(strings.Repeat(" ", gridLead))
	for i := 0; i < len(p.depDays); i++ {
		if i%5 != 0 {
			continue
		}
		if i+5 > len(p.depDays) {
			// Final partial group — emit just the digits without padding.
			b.WriteString(fmt.Sprintf("%.0f", p.depDays[i]))
			break
		}
		b.WriteString(fmt.Sprintf("%-5.0f", p.depDays[i]))
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat(" ", gridLead))
	b.WriteString(p.theme.Dim.Render("dep day"))
	b.WriteString("\n\n")

	// Selection readout.
	selDv := p.grid[p.selTof][p.selDep]
	depD := p.depDays[p.selDep]
	tofD := p.tofDays[p.selTof]
	if math.IsNaN(selDv) {
		b.WriteString(fmt.Sprintf("selected: dep+%.0fd tof=%.0fd  Δv: (no solution)\n",
			depD, tofD))
	} else {
		b.WriteString(fmt.Sprintf("selected: dep+%.0fd tof=%.0fd  total Δv: %.2f km/s\n",
			depD, tofD, selDv/1000))
	}
	b.WriteString(fmt.Sprintf("min %.2f km/s  max %.2f km/s  legend: ",
		minDv/1000, maxDv/1000))
	for _, g := range porkchopLegendRamp {
		b.WriteString(g)
	}
	b.WriteString("  (darker = cheaper; · = no solution)\n\n")

	b.WriteString(p.theme.Footer.Render("[←/→] dep [↑/↓] tof [enter] plant [esc] back"))
	return b.String()
}

// porkchopLegendRamp is the intensity ramp from cheapest (left) to
// most expensive (right).
var porkchopLegendRamp = []string{"█", "▓", "▒", "░", " "}

// porkchopGlyph picks a ramp glyph for a Δv value normalised to the
// [min, max] range of the current grid. NaN → "·" (no solution).
func porkchopGlyph(v, min, max float64) string {
	if math.IsNaN(v) {
		return "·"
	}
	if max <= min {
		return porkchopLegendRamp[0]
	}
	t := (v - min) / (max - min)
	idx := int(t * float64(len(porkchopLegendRamp)))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(porkchopLegendRamp) {
		idx = len(porkchopLegendRamp) - 1
	}
	return porkchopLegendRamp[idx]
}

// porkchopMin is a local re-export of planner.PorkchopMinCell so the
// screen doesn't need to import the planner package directly.
func porkchopMin(grid [][]float64) (depIdx, tofIdx int, total float64, ok bool) {
	best := math.Inf(1)
	for j := range grid {
		for i, v := range grid[j] {
			if math.IsNaN(v) {
				continue
			}
			if v < best {
				best = v
				depIdx = i
				tofIdx = j
				ok = true
			}
		}
	}
	total = best
	return
}

// linspace returns `n` evenly-spaced values from start to end inclusive.
func linspace(start, end float64, n int) []float64 {
	if n < 2 {
		return []float64{start}
	}
	out := make([]float64, n)
	step := (end - start) / float64(n-1)
	for i := 0; i < n; i++ {
		out[i] = start + step*float64(i)
	}
	return out
}
