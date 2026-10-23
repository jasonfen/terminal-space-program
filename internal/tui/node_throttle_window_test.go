package tui

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// #748: a node planted at a throttle below 1 burns proportionally slower, so
// its window must be sized at that throttle. Sized at full throttle, the
// window closed with about half the Δv still owed.
func TestNodeAtHalfThrottleDeliversItsDeltaV(t *testing.T) {
	for _, th := range []float64{1, 0.5} {
		a, err := New(nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		a.Update(screens.BurnExecutedMsg{
			EditingIdx:  -1,
			Mode:        spacecraft.BurnPrograde,
			DV:          200,
			Throttle:    th,
			TriggerTime: a.world.Clock.SimTime.Add(10 * time.Minute),
		})
		if n := len(a.world.ActiveCraft().Nodes); n != 1 {
			t.Fatalf("throttle %v: %d nodes planted, want 1", th, n)
		}
		owed, fired := 0.0, false
		for i := 0; i < 200000; i++ {
			a.world.Tick()
			ab := a.world.ActiveCraft().ActiveBurn
			if ab != nil {
				fired, owed = true, ab.DVRemaining
			} else if fired {
				break
			}
		}
		if !fired {
			t.Fatalf("throttle %v: node never fired", th)
		}
		if owed > 1 {
			t.Errorf("throttle %v: burn ended with %.1f of 200 m/s still owed", th, owed)
		}
	}
}
