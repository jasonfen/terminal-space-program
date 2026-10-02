package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// Wave B review fix (REVIEW MEDIUM 2): after planting a Rendezvous Burn whose
// wait exceeds the 4 h search window, the TARGET chip must read the PLAN's
// arrival (TriggerTime + RendezvousArrivalSec) and its predicted separation,
// planted and after the burn has fired, until the arrival passes.
func TestRendezvousPlanReading_PlantedFiredAndExpired(t *testing.T) {
	w := rendezvousSizesWorld(t, 500e3, 700e3, 200)
	ladder, err := w.RecommendRendezvousLadder(planner.RendezvousTheirOrbit)
	if err != nil {
		t.Fatal(err)
	}
	// A row whose wait is beyond the 4 h closest-approach window.
	laps := 0
	for _, r := range ladder.Rows {
		if r.Ok && r.TArrival > 5*3600 {
			laps = r.Laps
			break
		}
	}
	if laps == 0 {
		t.Fatalf("fixture has no long-wait row: %+v", ladder.Rows)
	}
	if _, _, ok := w.RendezvousPlanReading(); ok {
		t.Fatal("reading present before any plan")
	}
	if _, err := w.PlanRendezvousFromLadder(planner.RendezvousTheirOrbit, ladder, laps); err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	n := c.Nodes[0]
	wantArr := n.TriggerTime.Add(time.Duration(n.RendezvousArrivalSec * float64(time.Second)))

	arr, sep, ok := w.RendezvousPlanReading()
	if !ok || !arr.Equal(wantArr) {
		t.Fatalf("planted: reading arrival %v ok=%v, want %v", arr, ok, wantArr)
	}
	if sep < 0 || sep > 1000 {
		t.Errorf("planted: predicted separation %.1f m, want the plan's small miss", sep)
	}

	// Fly through the burn with real ticks: the node is gone, the plan stays.
	for i := 0; i < 400_000 && (len(c.Nodes) > 0 || c.ActiveBurn != nil || w.Clock.SimTime.Before(n.TriggerTime)); i++ {
		toTrig := n.TriggerTime.Sub(w.Clock.SimTime)
		switch {
		case toTrig > 1200*time.Second:
			w.Clock.WarpIdx = 3
		case toTrig > 150*time.Second:
			w.Clock.WarpIdx = 2
		default:
			w.Clock.WarpIdx = 0
		}
		w.Tick()
	}
	if len(c.Nodes) != 0 || c.ActiveBurn != nil {
		t.Fatalf("burn never completed (nodes=%d burn=%v)", len(c.Nodes), c.ActiveBurn != nil)
	}
	arr, _, ok = w.RendezvousPlanReading()
	if !ok || !arr.Equal(wantArr) {
		t.Fatalf("after the burn: reading arrival %v ok=%v, want %v", arr, ok, wantArr)
	}

	// A different target: the plan is not about it.
	w2 := w.Target
	w.Target.CraftID++
	if _, _, ok := w.RendezvousPlanReading(); ok {
		t.Error("reading offered for a target the plan was not made against")
	}
	w.Target = w2

	// Past the arrival: gone.
	w.Clock.SimTime = wantArr.Add(time.Second)
	if _, _, ok := w.RendezvousPlanReading(); ok {
		t.Error("reading still offered after the planned arrival passed")
	}
}

// Deleting the planted node before it fires withdraws the plan.
func TestRendezvousPlanReading_DeletedNodeWithdrawsPlan(t *testing.T) {
	w := rendezvousSizesWorld(t, 500e3, 700e3, 200)
	if _, err := w.PlanRendezvousBurn(planner.RendezvousTheirOrbit, 2); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := w.RendezvousPlanReading(); !ok {
		t.Fatal("no reading after plant")
	}
	w.DeleteNode(0)
	if _, _, ok := w.RendezvousPlanReading(); ok {
		t.Error("reading survives deleting the unfired node")
	}
}
