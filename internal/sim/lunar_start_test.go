package sim

import (
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
	// The achievable departure is quantized to the parking period, so allow
	// one such period of slack around the 4 h target.
	got := plan.Departure.OffsetTime
	if got < lead-95*time.Minute || got > lead+95*time.Minute {
		t.Errorf("departure offset = %v, want within 95m of %v", got, lead)
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
