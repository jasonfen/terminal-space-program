package tui

import (
	"math"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// advanceSimSeconds ticks the REAL update path (a.Update(sim.TickMsg)) until
// the sim clock has moved at least secs, at whatever warp the app is on.
func advanceSimSeconds(t *testing.T, a *App, secs float64) {
	t.Helper()
	start := a.world.Clock.SimTime
	for i := 0; i < 2_000_000; i++ {
		if a.world.Clock.SimTime.Sub(start).Seconds() >= secs {
			return
		}
		a.Update(sim.TickMsg(time.Now()))
	}
	t.Fatalf("clock never advanced %.0f s", secs)
}

// G4 Q1b (#418): opening the picker drops warp to 1x (the clock keeps
// running; no pause).
func TestRendezvousPicker_OpenDropsWarpToRealtime(t *testing.T) {
	a := rendezvousPickerPhaseMismatchApp(t)
	a.world.Clock.WarpIdx = 3 // 1000x coasting
	pressRune(a, 'K')
	if !a.orbitView.RendezvousPickerOpen() {
		t.Fatal("picker did not open")
	}
	if a.world.Clock.WarpIdx != 0 {
		t.Errorf("WarpIdx = %d after K, want 0 (1x)", a.world.Clock.WarpIdx)
	}
	if a.world.Clock.Paused {
		t.Errorf("picker must not pause the clock (Q1b: no pause state)")
	}
}

// G4 Q1/Q2 (#418), through the real key handlers: read a row, let the clock
// run, press Enter. The planted node is the row that was read: its burn
// epoch (solve time + the row's TBurn), size and flight time, and flying it
// through the Engage commit path closes the gap.
func TestRendezvousPickerEnter_AfterClockMoved_PlantsTheRowRead(t *testing.T) {
	a := rendezvousPickerPhaseMismatchApp(t)
	c := a.world.ActiveCraft()
	pressRune(a, 'K')
	if !a.orbitView.RendezvousPickerOpen() {
		t.Fatal("picker did not open")
	}
	row, ok := a.orbitView.RendezvousPickerSelectedRow()
	if !ok || !row.Ok {
		t.Fatalf("no plantable selected row: %+v ok=%v", row, ok)
	}
	solvedAt := a.orbitView.RendezvousPickerLadder().SolvedAt
	if solvedAt.IsZero() {
		t.Fatal("ladder carries no SolvedAt")
	}
	if row.TBurn < 299 {
		t.Fatalf("row TBurn=%.1f, want the ~5 min default lead", row.TBurn)
	}

	// The pilot reads for a minute and a half (the picker kept the clock
	// running at 1x; a few ticks at higher warp stand in for slow reading).
	a.world.Clock.WarpIdx = 2
	advanceSimSeconds(t, a, 90)
	a.world.Clock.WarpIdx = 0

	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(c.Nodes) != 1 {
		t.Fatalf("Enter planted %d nodes, want 1", len(c.Nodes))
	}
	n := c.Nodes[0]
	wantTrigger := solvedAt.Add(time.Duration(row.TBurn * float64(time.Second)))
	if d := n.TriggerTime.Sub(wantTrigger); d > time.Millisecond || d < -time.Millisecond {
		t.Errorf("node fires at %v, row read said %v (off by %v)", n.TriggerTime, wantTrigger, d)
	}
	if math.Abs(n.DV-row.DV) > 1e-6 {
		t.Errorf("node DV %.3f, row read said %.3f", n.DV, row.DV)
	}
	if math.Abs(n.RendezvousArrivalSec-row.FlightSec()) > 1e-6 {
		t.Errorf("node RendezvousArrivalSec %.1f, row flight %.1f (counts from the burn)", n.RendezvousArrivalSec, row.FlightSec())
	}
	if plan, ok := a.world.RendezvousCommitWithPlan(); !ok || plan.CommittedCA > 7_000 {
		t.Errorf("flown CA through the commit path: %.0f m (ok=%v), want small", plan.CommittedCA, ok)
	}
}

// A row whose burn epoch has passed refuses honestly and plants nothing; the
// picker stays open so the pilot can re-read.
func TestRendezvousPickerEnter_ExpiredRow_RefusesPlantsNothing(t *testing.T) {
	a := rendezvousPickerPhaseMismatchApp(t)
	c := a.world.ActiveCraft()
	pressRune(a, 'K')
	a.world.Clock.WarpIdx = 3
	advanceSimSeconds(t, a, 400) // past the 300 s lead
	a.world.Clock.WarpIdx = 0

	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(c.Nodes) != 0 {
		t.Fatalf("expired row planted %d nodes, want 0", len(c.Nodes))
	}
	if !a.orbitView.RendezvousPickerOpen() {
		t.Errorf("picker closed on an expired row; it should stay open")
	}
}
