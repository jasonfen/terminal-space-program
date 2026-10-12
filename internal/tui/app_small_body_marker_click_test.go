package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// moonNodeApp is the everyday lunar-transfer picture at the Design Size: Earth
// in focus, the view zoomed out until the Moon is a couple of dots, and a
// planted node (the capture node) whose true point is the Moon's own cell, so
// its Δ steps off the Moon.
// The vessel sits on a transfer ellipse whose apoapsis is the Moon's
// distance, and the node is dated at that apoapsis. It returns the app with
// the frame rendered, the screen cells of the Moon's disk, and the cell the
// node's glyph was drawn in.
func moonNodeApp(t *testing.T) (a *App, moonCells [][2]int, glyphCol, glyphRow int) {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	w := a.world
	var earth, moon int
	for i, b := range w.System().Bodies {
		switch b.ID {
		case "earth":
			earth = i
		case "moon":
			moon = i
		}
	}
	eb, mb := w.System().Bodies[earth], w.System().Bodies[moon]
	rel := w.BodyPosition(mb).Sub(w.BodyPosition(eb))
	rel.Z = 0
	dir := rel.Unit()
	mu := eb.GravitationalParameter()
	rp, ra := 6.6e6, rel.Norm()
	sma := (rp + ra) / 2
	vp := math.Sqrt(mu * (2/rp - 1/sma))
	c := w.ActiveCraft()
	c.State.R = dir.Scale(-rp)
	c.State.V = orbital.Vec3{X: -dir.Y, Y: dir.X}.Scale(-vp)
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: earth}
	w.ViewMode = sim.ViewTop
	half := time.Duration(math.Pi * math.Sqrt(sma*sma*sma/mu) * float64(time.Second))
	w.PlanNode(spacecraft.ManeuverNode{TriggerTime: w.Clock.SimTime.Add(half), Mode: spacecraft.BurnPrograde, DV: 10, Duration: time.Second})
	for _, mode := range sim.ProjectionViewModes {
		w.ViewMode = mode
		for i := 0; i < 20; i++ {
			if g, ok := tryMoonNodeFrame(a); ok {
				return a, g.moonCells, g.col, g.row
			}
			a.orbitView.ZoomOut()
		}
	}
	t.Fatal("setup: no frame has the Moon with a node's Δ stepped off beside it, clear of the chips")
	return
}

type moonNodeFrame struct {
	moonCells [][2]int
	col, row  int
}

// tryMoonNodeFrame renders the frame and, when the Moon is on it with the
// node's Δ drawn in a cell beside the Moon's, and no chip or navball control
// within a cell of either (they take clicks first), returns the cells.
func tryMoonNodeFrame(a *App) (moonNodeFrame, bool) {
	{
		a.View()
		var moonCells [][2]int
		for r := 2; r < a.height-1; r++ {
			for col := 1; col < a.width-1; col++ {
				// Chips are opaque and take clicks before the canvas: a Moon
				// cell under one is not a click on the canvas at all.
				if a.orbitView.HitAt(col, r).BodyID == "moon" {
					moonCells = append(moonCells, [2]int{col, r})
				}
			}
		}
		clear := func(c, r int) bool {
			for dr := -1; dr <= 1; dr++ {
				for dc := -1; dc <= 1; dc++ {
					if _, on := a.orbitView.HitChip(c+dc, r+dr); on {
						return false
					}
					if _, on := a.orbitView.HitNavballControl(c+dc, r+dr); on {
						return false
					}
				}
			}
			return true
		}
		// The drawn Δ beside the Moon's cells (not on them: it stepped off).
		lines := strings.Split(ansi.Strip(a.View()), "\n")
		for _, mc := range moonCells {
			for dr := -2; dr <= 2; dr++ {
				for dc := -2; dc <= 2; dc++ {
					rr, cc := mc[1]+dr, mc[0]+dc
					if rr < 0 || rr >= len(lines) {
						continue
					}
					ln := []rune(lines[rr])
					if cc < 0 || cc >= len(ln) || ln[cc] != 'Δ' {
						continue
					}
					onMoon := false
					for _, m2 := range moonCells {
						onMoon = onMoon || m2 == [2]int{cc, rr}
					}
					if !onMoon && clear(cc, rr) && clear(mc[0], mc[1]) {
						return moonNodeFrame{moonCells, cc, rr}, true
					}
				}
			}
		}
	}
	return moonNodeFrame{}, false
}

// Review #629 M2. A marker that steps off a small body used to leave its
// click target behind: the drawn Δ did nothing, and clicking the Moon under
// it opened the maneuver planner. Now the drawn glyph is the node's click
// target, and the Moon's own cell focuses the Moon.
func TestSteppedOffMarkerClicksWhereItIsDrawn(t *testing.T) {
	a, _, gc, gr := moonNodeApp(t)
	a.Update(tea.MouseMsg{X: gc, Y: gr, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	// An empty-canvas click ALSO opens the planner (staging a fresh burn), so
	// the screen alone proves nothing: the planner must be editing the node.
	if a.active != screenManeuver {
		t.Fatalf("clicking the drawn Δ at (%d,%d) did not open the planner (screen %v)", gc, gr, a.active)
	}
	if got := a.maneuver.TitleContext(a.world); got != "editing node 1 of 1" {
		t.Errorf("clicking the drawn Δ opened the planner as %q, want it editing the node (a stray staged burn is the empty-canvas path)", got)
	}
}

func TestClickingTheSmallBodyUnderAMarkerFocusesTheBody(t *testing.T) {
	a, moonCells, _, _ := moonNodeApp(t)
	if len(moonCells) == 0 {
		t.Fatal("setup: no Moon cells")
	}
	for _, mc := range moonCells {
		a.active = screenOrbit
		a.selectedBody = -1
		a.Update(tea.MouseMsg{X: mc[0], Y: mc[1], Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if a.active != screenOrbit {
			t.Errorf("clicking the Moon's cell %v opened screen %v (the node's planner), want it to stay on the map", mc, a.active)
		}
		if a.selectedBody < 0 || a.world.System().Bodies[a.selectedBody].ID != "moon" {
			t.Errorf("clicking the Moon's cell %v did not select the Moon (selectedBody %d)", mc, a.selectedBody)
		}
		a.world.Clock.Paused = false
	}
}
