package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// plantLongRow plants a long-wait row and returns the world, craft and node.
func plantLongRow(t *testing.T) (*World, *spacecraft.Spacecraft, ManeuverNode) {
	t.Helper()
	w := rendezvousSizesWorld(t, 500e3, 700e3, 200)
	ladder, err := w.RecommendRendezvousLadder(planner.RendezvousTheirOrbit)
	if err != nil {
		t.Fatal(err)
	}
	laps := 0
	for _, r := range ladder.Rows {
		if r.Ok && r.TArrival > 5*3600 {
			laps = r.Laps
			break
		}
	}
	if laps == 0 {
		t.Fatal("no long-wait row")
	}
	if _, err := w.PlanRendezvousFromLadder(planner.RendezvousTheirOrbit, ladder, laps); err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	return w, c, c.Nodes[0]
}

// tickPlanUntil ticks (warping far from t0, 1x near it) until cond holds.
func tickPlanUntil(t *testing.T, w *World, trigger time.Time, cond func() bool) {
	t.Helper()
	for i := 0; i < 400_000; i++ {
		if cond() {
			return
		}
		toTrig := trigger.Sub(w.Clock.SimTime)
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
	t.Fatal("condition never reached")
}

// firedAndDone flies the planted rendezvous burn to completion.
func firedAndDone(t *testing.T) (*World, *spacecraft.Spacecraft) {
	w, c, n := plantLongRow(t)
	tickPlanUntil(t, w, n.TriggerTime, func() bool {
		return len(c.Nodes) == 0 && c.ActiveBurn == nil && !w.Clock.SimTime.Before(n.TriggerTime)
	})
	if _, _, ok := w.RendezvousPlanReading(); !ok {
		t.Fatal("plan reading missing after a clean rendezvous burn")
	}
	return w, c
}

// Review follow-up: any other thrust after the burn makes the plan stale.
func TestRendezvousPlanReading_ManualBurnAfterFireWithdraws(t *testing.T) {
	w, c := firedAndDone(t)
	c.Throttle = 1
	w.StartManualBurn()
	if c.ManualBurn == nil {
		t.Fatal("fixture: manual burn did not start")
	}
	if _, _, ok := w.RendezvousPlanReading(); ok {
		t.Error("plan still read after a manual burn started")
	}
	if c.RendezvousPlan != nil {
		t.Error("stored plan not withdrawn")
	}
}

func TestRendezvousPlanReading_SecondNodeBurnWithdraws(t *testing.T) {
	w, c := firedAndDone(t)
	trig := w.Clock.SimTime.Add(60 * time.Second)
	w.PlanNode(ManeuverNode{Mode: spacecraft.BurnPrograde, DV: 30, Duration: c.BurnTimeForDV(30),
		Event: spacecraft.TriggerAbsolute, TriggerTime: trig, PrimaryID: c.Primary.ID, Throttle: 1})
	tickPlanUntil(t, w, trig, func() bool { return !w.Clock.SimTime.Before(trig.Add(time.Second)) })
	if _, _, ok := w.RendezvousPlanReading(); ok {
		t.Error("plan still read after another node's burn started")
	}
}

func TestRendezvousPlanReading_CutShortBurnWithdraws(t *testing.T) {
	w, c, n := plantLongRow(t)
	tickPlanUntil(t, w, n.TriggerTime, func() bool { return c.ActiveBurn != nil })
	if _, _, ok := w.RendezvousPlanReading(); !ok {
		t.Fatal("no reading while the rendezvous burn is in flight")
	}
	// The burn window ends with Δv still owed.
	c.ActiveBurn.EndTime = w.Clock.SimTime
	if c.ActiveBurn.DVRemaining < 5 {
		t.Fatalf("fixture: only %.2f m/s left", c.ActiveBurn.DVRemaining)
	}
	w.Clock.WarpIdx = 0
	w.Tick()
	if c.ActiveBurn != nil {
		t.Fatal("fixture: burn did not end")
	}
	if _, _, ok := w.RendezvousPlanReading(); ok {
		t.Error("plan still read after the burn ended short of its Δv")
	}
}

// A planted node that disappears at/after its trigger WITHOUT firing (refused
// or dropped) is not a flown plan.
func TestRendezvousPlanReading_NodeDroppedAtFireWithdraws(t *testing.T) {
	w, c, n := plantLongRow(t)
	w.Clock.SimTime = n.TriggerTime.Add(time.Second)
	c.Nodes = nil // dropped without ever firing
	if _, _, ok := w.RendezvousPlanReading(); ok {
		t.Error("plan read for a node that never fired")
	}
}
