package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Audit 2026-10-18 #5: touchdown or crash broke out of the sub-step loop but
// left the planted/manual burn alive. Every later tick returned early at
// Landed/Crashed, so the burn was never torn down: it pinned world warp at
// 10x (AnyCraftThrusting) and blocked StartManualBurn, so a lander could not
// lift off again.
func TestSurfaceArrivalEndsBurn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		soft    bool
		manual  bool
		nose    orbital.Vec3
		vDown   float64
		landed  bool
		crashed bool
	}{
		{"planted burn, soft landing", true, false, orbital.Vec3{X: 1}, 2, true, false},
		{"planted burn, crash", false, false, orbital.Vec3{X: 1}, 5, false, true},
		{"manual burn, soft landing", true, true, orbital.Vec3{X: 1}, 2, true, false},
		{"manual burn, crash", false, true, orbital.Vec3{X: 1}, 5, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, c := crashTestPrimaryFor(t)
			c.CanSoftLand = tc.soft
			c.Stages[0].CanSoftLand = tc.soft // the thrust sync re-reads it from the bottom stage
			setImpactState(c, tc.vDown, tc.nose)
			if tc.manual {
				c.AttitudeMode = spacecraft.BurnRadialOut
				c.ManualBurn = &spacecraft.ManualBurn{}
			} else {
				c.ActiveBurn = &spacecraft.ActiveBurn{
					Mode:        spacecraft.BurnRadialOut,
					DVRemaining: 500,
					EndTime:     w.Clock.SimTime.Add(10 * time.Minute),
					PrimaryID:   c.Primary.ID,
					Throttle:    1,
				}
			}
			for i := 0; i < 50; i++ {
				// 10 ms ticks: a full BaseStep of thrust would push a soft
				// touchdown past V_CRIT before contact.
				w.integrateOneCraft(c, 10*time.Millisecond)
			}
			if c.Landed != tc.landed || c.Crashed != tc.crashed {
				t.Fatalf("setup: Landed=%v Crashed=%v, want %v/%v", c.Landed, c.Crashed, tc.landed, tc.crashed)
			}
			if c.ActiveBurn != nil || c.ManualBurn != nil {
				t.Errorf("burn survived surface arrival: ActiveBurn=%v ManualBurn=%v", c.ActiveBurn != nil, c.ManualBurn != nil)
			}
			if w.AnyCraftThrusting() {
				t.Error("AnyCraftThrusting stays true on a landed/crashed vessel (pins warp at 10x)")
			}
		})
	}
}
