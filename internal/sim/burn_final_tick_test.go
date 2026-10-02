package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #537: the burn integrator used to thrust the whole last tick of a finite
// burn and clamp only the DVRemaining bookkeeping, over-delivering up to one
// tick of Δv (~1 m/s at 1x, ~10 m/s at 10x). The final tick must thrust only
// for the fraction that delivers DVRemaining, then coast.
//
// Measured against a twin world that never burns: the velocity difference
// after the burn is exactly the Δv the engine delivered (the twin shares
// gravity to within millimetres per second over a few seconds).
func deliveredDV(t *testing.T, warpIdx int, dv float64) float64 {
	t.Helper()
	burn := rendezvousSizesWorld(t, 500e3, 500e3, 90)
	twin := rendezvousSizesWorld(t, 500e3, 500e3, 90)
	c := burn.ActiveCraft()
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: dv,
		PlannedDV:   dv,
		EndTime:     burn.Clock.SimTime.Add(10 * time.Minute),
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
	}
	n := 0
	for ; n < 5000 && c.ActiveBurn != nil; n++ {
		burn.Clock.WarpIdx = warpIdx
		burn.Tick()
	}
	if c.ActiveBurn != nil {
		t.Fatalf("burn never finished in %d ticks", n)
	}
	// Coast a few more ticks so both worlds sit at the same instant.
	for i := 0; i < 3; i++ {
		burn.Clock.WarpIdx = warpIdx
		burn.Tick()
		n++
	}
	for i := 0; i < n; i++ {
		twin.Clock.WarpIdx = warpIdx
		twin.Tick()
	}
	if !burn.Clock.SimTime.Equal(twin.Clock.SimTime) {
		t.Fatalf("clocks differ: %v vs %v", burn.Clock.SimTime, twin.Clock.SimTime)
	}
	return burn.ActiveCraft().State.V.Sub(twin.ActiveCraft().State.V).Norm()
}

func TestPlantedBurnDeliversItsDV(t *testing.T) {
	const tol = 0.05 // m/s
	for _, tc := range []struct {
		name string
		idx  int
	}{{"1x", 0}, {"10x", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, dv := range []float64{123.456, 400.1} {
				got := deliveredDV(t, tc.idx, dv)
				if math.Abs(got-dv) > tol {
					t.Errorf("planned %.3f m/s, engine delivered %.3f m/s (off by %+.3f, want within %.2f)", dv, got, got-dv, tol)
				}
			}
		})
	}
}
