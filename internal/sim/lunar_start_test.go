package sim

import (
	"sort"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestAdjustStartForLunarTransferWindow: the computed start lands the split
// departure burn ~lead away (not the ~10 days out J2000 yields), and the
// number the search models matches what PlanTransfer actually plants.
func TestAdjustStartForLunarTransferWindow(t *testing.T) {
	w := mustWorld(t)
	moonIdx := moonIndex(w)
	if moonIdx < 0 {
		t.Skip("Moon missing from Sol")
	}

	const lead = 4 * time.Hour
	if !w.AdjustStartForLunarTransferWindow(lead) {
		t.Fatalf("AdjustStartForLunarTransferWindow returned false")
	}
	if w.Clock.SimTime.Equal(bodies.J2000) {
		t.Fatalf("clock still at J2000 after adjustment")
	}
	if !w.Clock.RotationTime.Equal(w.Clock.SimTime) {
		t.Errorf("RotationTime %v not synced to SimTime %v",
			w.Clock.RotationTime, w.Clock.SimTime)
	}

	plan, err := w.PlanTransfer(moonIdx)
	if err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	if w.LastTransfer.Strategy != "split" {
		t.Fatalf("strategy = %q, want split", w.LastTransfer.Strategy)
	}
	// The achievable departure is quantized to the parking period. Since
	// #566 (Jason, 2026-10-07, option S) the split may slip its departure by
	// whole parking orbits so the arrival lines up with the Moon's plane,
	// which from the 51.6 deg seed can pull it EARLIER than the lead (never
	// before the burn can start). The pin is therefore a ceiling and a floor
	// of "the burn is still ahead of us", not "within 95 m of 4 h".
	// See TestLunarTransferWaitDistribution for the measured spread.
	got := plan.Departure.OffsetTime
	if got <= 0 || got > lead+95*time.Minute {
		t.Errorf("departure offset = %v, want in (0, %v]", got, lead+95*time.Minute)
	}
}

// TestLunarTransferWaitDistribution measures the wait to the Moon-transfer
// burn from the default start (AdjustStartForLunarTransferWindow, 4 h lead)
// across 20 start times over a lunar month, from the 51.6 deg seed and an
// equatorial LEO, and pins that departure slipping never makes the wait
// worse than the old bound (#566, option S). The raw (unadjusted) wait is
// bounded by the node phasing, about half a lunar month, unchanged by the slip.
func TestLunarTransferWaitDistribution(t *testing.T) {
	for _, eq := range []bool{true, false} {
		var adj, raw []float64
		for i := 0; i < 20; i++ {
			off := time.Duration(float64(i) * 27.3 / 20 * 24 * float64(time.Hour))
			w := mustWorld(t)
			if eq {
				makeActiveEquatorial(w)
			}
			w.Clock.SimTime = w.Clock.SimTime.Add(off)
			idx := moonIndex(w)
			p, err := w.PlanTransfer(idx)
			if err != nil {
				t.Fatalf("PlanTransfer raw: %v", err)
			}
			raw = append(raw, p.Departure.OffsetTime.Hours())

			w2 := mustWorld(t)
			if eq {
				makeActiveEquatorial(w2)
			}
			w2.Clock.SimTime = w2.Clock.SimTime.Add(off)
			w2.AdjustStartForLunarTransferWindow(DefaultLunarTransferLead)
			p2, err := w2.PlanTransfer(moonIndex(w2))
			if err != nil {
				t.Fatalf("PlanTransfer adjusted: %v", err)
			}
			adj = append(adj, p2.Departure.OffsetTime.Hours())
		}
		sort.Float64s(adj)
		sort.Float64s(raw)
		t.Logf("equatorial=%v wait after default start (h): min %.2f median %.2f max %.2f", eq, adj[0], adj[len(adj)/2], adj[len(adj)-1])
		t.Logf("equatorial=%v raw wait from arbitrary starts (h): min %.1f median %.1f max %.1f", eq, raw[0], raw[len(raw)/2], raw[len(raw)-1])
		if adj[0] <= 0 || adj[len(adj)-1] > (DefaultLunarTransferLead+95*time.Minute).Hours() {
			t.Errorf("equatorial=%v default-start wait %.2f..%.2f h outside (0, 5.6]", eq, adj[0], adj[len(adj)-1])
		}
		if raw[len(raw)-1] > 15*24 {
			t.Errorf("equatorial=%v raw wait max %.0f h exceeds the half-month phasing bound", eq, raw[len(raw)-1])
		}
	}
}

// TestAdjustStartForLunarTransferWindowDeterministic: the search is a pure
// function of the spawn state, so repeated calls land the same start.
func TestAdjustStartForLunarTransferWindowDeterministic(t *testing.T) {
	w1 := mustWorld(t)
	if moonIndex(w1) < 0 {
		t.Skip("Moon missing from Sol")
	}
	w2 := mustWorld(t)

	if !w1.AdjustStartForLunarTransferWindow(4*time.Hour) ||
		!w2.AdjustStartForLunarTransferWindow(4*time.Hour) {
		t.Fatalf("adjustment returned false")
	}
	if !w1.Clock.SimTime.Equal(w2.Clock.SimTime) {
		t.Errorf("non-deterministic start: %v vs %v",
			w1.Clock.SimTime, w2.Clock.SimTime)
	}
}
