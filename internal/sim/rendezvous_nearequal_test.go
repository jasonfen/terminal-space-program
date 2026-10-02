package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// Wave B review fix (REVIEW LOW 163): a holder 500 m above the mover (500 vs
// 500.5 km) sat outside the 1 m "same radius" band, so it took the
// different-sizes solve and the ladder jumped (20 laps: 14 m/s at an exact
// match, 113 m/s at +2 m). Near-equal circular orbits are the same size for
// the planner: same rows as the exact match, the miss shown honestly in CA.
func TestRendezvousNearEqualRadii_SameLadderAsExactMatch(t *testing.T) {
	exact, err := rendezvousSizesWorld(t, 500e3, 500e3, 40).RecommendRendezvousLadder(planner.RendezvousTheirOrbit)
	if err != nil {
		t.Fatalf("exact: %v", err)
	}
	for _, off := range []float64{2, 500} {
		near, err := rendezvousSizesWorld(t, 500e3, 500e3+off, 40).RecommendRendezvousLadder(planner.RendezvousTheirOrbit)
		if err != nil {
			t.Fatalf("+%.0f m: %v", off, err)
		}
		if len(near.Rows) != len(exact.Rows) {
			t.Fatalf("+%.0f m: %d rows vs %d", off, len(near.Rows), len(exact.Rows))
		}
		for i, r := range near.Rows {
			e := exact.Rows[i]
			if !r.Ok {
				t.Errorf("+%.0f m laps=%d refused: %s", off, r.Laps, r.Reason)
				continue
			}
			if math.Abs(r.DV-e.DV) > 0.02*e.DV+1 {
				t.Errorf("+%.0f m laps=%d: DV %.1f vs exact-match %.1f (a different solve took over)", off, r.Laps, r.DV, e.DV)
			}
			if r.AchievableCA > 1_000 {
				t.Errorf("+%.0f m laps=%d: advertised CA %.0f m, want the radial offset (<1 km)", off, r.Laps, r.AchievableCA)
			}
		}
	}
}
