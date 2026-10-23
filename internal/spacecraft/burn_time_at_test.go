package spacecraft

import "testing"

// #748: half throttle doubles the engine-on time; 0 and 1 both mean full.
func TestBurnTimeForDVAtScalesByThrottle(t *testing.T) {
	c := NewFromLoadout(LoadoutSaturnVID)
	full := c.BurnTimeForDV(200)
	if full <= 0 {
		t.Fatalf("setup: full-throttle burn time %v", full)
	}
	if got := c.BurnTimeForDVAt(200, 0.5); got != 2*full {
		t.Errorf("throttle 0.5: %v, want %v", got, 2*full)
	}
	for _, th := range []float64{0, 1} {
		if got := c.BurnTimeForDVAt(200, th); got != full {
			t.Errorf("throttle %v: %v, want %v", th, got, full)
		}
	}
}
