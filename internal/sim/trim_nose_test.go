package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestTrimNoseDownDegFollowsTheHold: the trim: rows read → as "down"
// against a prograde hold and "up" against a retrograde one, because →
// lowers a nose facing downrange and raises one facing back (Jason
// 2026-10-09: "up / down ... in relation to prograde").
func TestTrimNoseDownDegFollowsTheHold(t *testing.T) {
	w := testWorld(t)
	c := w.ActiveCraft()
	c.PitchTrim = 5 * math.Pi / 180
	for _, tc := range []struct {
		mode spacecraft.BurnMode
		want float64
	}{{spacecraft.BurnPrograde, 5}, {spacecraft.BurnRetrograde, -5}} {
		c.AttitudeMode = tc.mode
		if got := w.TrimNoseDownDeg(c); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%v hold, → 5°: TrimNoseDownDeg = %+.2f, want %+.0f", tc.mode, got, tc.want)
		}
	}
}
