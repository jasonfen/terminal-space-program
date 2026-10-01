package tui

import (
	"math"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// rendezvousPickerSizesApp: you at 500 km, partner at 700 km, both circular
// and coplanar, partner phaseDeg ahead (G4 Q4). Built through the spawn path
// plus a state rewrite, as the sibling helpers do.
func rendezvousPickerSizesApp(t *testing.T, phaseDeg float64) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 700e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	active, target := a.world.Crafts[0], a.world.Crafts[1]
	mu := active.Primary.GravitationalParameter()
	rp := active.Primary.RadiusMeters()
	axis := active.State.R.Cross(active.State.V).Unit()
	rhat := active.State.R.Unit()
	that := axis.Cross(rhat).Unit()
	rm, rh := rp+500e3, rp+700e3
	active.State.R, active.State.V = rhat.Scale(rm), that.Scale(math.Sqrt(mu/rm))
	ph := phaseDeg * math.Pi / 180
	target.State.R = rotateAboutAxis(rhat, axis, ph).Scale(rh)
	target.State.V = rotateAboutAxis(that, axis, ph).Scale(math.Sqrt(mu / rh))
	target.Primary = active.Primary
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)
	return a
}

// #407 through the real K key: a 500/700 km pair opens the Rendezvous Planner
// with rows, not the old `radius outside target's apsides: plan a transfer
// [H] first` refusal, and Enter plants the row read.
func TestRendezvousK_DifferentSizes_ShowsRowsNotRefusal(t *testing.T) {
	a := rendezvousPickerSizesApp(t, 200)
	pressRune(a, 'K')
	if !a.orbitView.RendezvousPickerOpen() {
		t.Fatalf("K did not open the picker")
	}
	ladder := a.orbitView.RendezvousPickerLadder()
	if len(ladder.Rows) == 0 {
		t.Fatalf("picker open with no rows")
	}
	row, ok := a.orbitView.RendezvousPickerSelectedRow()
	if !ok || !row.Ok {
		t.Fatalf("picker opened without a plantable selected row: %+v ok=%v rows=%+v", row, ok, ladder.Rows)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	c := a.world.ActiveCraft()
	if len(c.Nodes) != 1 {
		t.Fatalf("Enter planted %d nodes, want 1", len(c.Nodes))
	}
	if math.Abs(c.Nodes[0].DV-row.DV) > 1e-6 {
		t.Errorf("node DV %.3f, row read said %.3f", c.Nodes[0].DV, row.DV)
	}
}
