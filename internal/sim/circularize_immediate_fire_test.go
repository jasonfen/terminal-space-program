// Regression for the v0.11 playtest report: pressing `C` while a
// manual burn was active (engine ignited via `b`, throttle dropped
// to 0%) appeared to make the engine fire immediately rather than
// wait for apoapsis. These three tests pin down the sim-layer
// contract end-to-end and confirm the reported behaviour does not
// match the actual sim state:
//
//   1. After plant + one Tick, ActiveBurn must remain nil and the
//      planted node must have a TriggerTime well in the future
//      (past the half-period mark for a periapsis-departure orbit).
//   2. Across a 200-tick coast (warp 100×) the planted node stays
//      dormant — ActiveBurn nil, fuel constant.
//   3. Coasting all the way through apoapsis fires the planted burn
//      (fuel decreases).
//
// All three pass at v0.11.0 Slice 1.7: the sim plants, resolves, and
// fires correctly. The original perception report likely traced to
// the HUD throttle indicator showing the planted node's 100% throttle
// at fire time even though the player's manual setting was 0%, OR
// the player was closer to apo than they realised. Either way the
// sim-side path is verified.
package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func TestCircularizeWithManualBurnZeroThrottleDoesNotFireImmediately(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	const (
		periAlt = 200e3  // 200 km — just above atmosphere cutoff
		apoAlt  = 1000e3 // 1000 km — comfortably above
	)
	rPeri := primaryR + periAlt
	rApo := primaryR + apoAlt
	a := (rPeri + rApo) / 2
	vPeri := math.Sqrt(mu * (2/rPeri - 1/a))
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	c.State.R = frame.ToWorld(orbital.Vec3{X: rPeri})
	c.State.V = frame.ToWorld(orbital.Vec3{Y: vPeri})

	// Player has the engine "armed" via `b` but throttle at 0% (idle).
	// StartManualBurn gates on EffectiveThrottle > 0, so simulate the
	// observed runtime state directly: ManualBurn non-nil, throttle 0.
	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}
	c.Throttle = 0

	if _, err := w.PlanCircularizeAtApoapsis(); err != nil {
		t.Fatalf("PlanCircularizeAtApoapsis: %v", err)
	}

	w.Tick()

	if c.ActiveBurn != nil {
		t.Fatalf("ActiveBurn = %+v on tick after plant, want nil — burn must wait for apoapsis", c.ActiveBurn)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1 (planted circularize)", len(c.Nodes))
	}
	n := c.Nodes[0]
	if !n.IsResolved() {
		t.Fatalf("node not resolved after one tick — resolver should have set TriggerTime")
	}
	// Expected dt-to-apo at periapsis = half the orbital period.
	period := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	wantTriggerAfter := w.Clock.SimTime.Add(time.Duration((period/2 - 1) * float64(time.Second)))
	if !n.TriggerTime.After(wantTriggerAfter) {
		t.Errorf("TriggerTime = %v (= now + %.0fs), want strictly after now + %.0fs (≈ half-period)",
			n.TriggerTime,
			n.TriggerTime.Sub(w.Clock.SimTime).Seconds(),
			period/2-1)
	}
}

// And under the same setup, no thrust should be applied across the
// coast — fuel mass must stay constant until BurnStart, even though
// ManualBurn is non-nil. If this fails the planted node is firing
// thrust through the executor or the integrator is misreading
// ManualBurn+throttle-0 as "go".
func TestCircularizeCoastDoesNotConsumeFuel(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	const (
		periAlt = 200e3
		apoAlt  = 1000e3
	)
	rPeri := primaryR + periAlt
	rApo := primaryR + apoAlt
	a := (rPeri + rApo) / 2
	vPeri := math.Sqrt(mu * (2/rPeri - 1/a))
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	c.State.R = frame.ToWorld(orbital.Vec3{X: rPeri})
	c.State.V = frame.ToWorld(orbital.Vec3{Y: vPeri})

	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}
	c.Throttle = 0

	if _, err := w.PlanCircularizeAtApoapsis(); err != nil {
		t.Fatalf("PlanCircularizeAtApoapsis: %v", err)
	}
	fuelBefore := c.Fuel

	// Advance several minutes of sim time. Half-period for this orbit
	// is ~30 min, so we stay well clear of apo and the planted burn
	// must remain dormant. Crank warp up so we don't iterate thousands
	// of ticks (warp clamp leaves the future trigger alone).
	for w.Clock.Warp() < 100 {
		w.Clock.WarpUp()
	}
	for i := 0; i < 200; i++ {
		w.Tick()
	}

	if c.ActiveBurn != nil {
		t.Fatalf("ActiveBurn = %+v during coast, want nil", c.ActiveBurn)
	}
	if fuelLost := fuelBefore - c.Fuel; fuelLost > 1e-6 {
		t.Errorf("fuel lost during coast = %.3f kg, want 0 (no thrust at 0%% throttle)", fuelLost)
	}
}

// And eventually — coasting all the way to apoapsis — the planted
// burn DOES fire. Pins the positive case so the "doesn't fire
// immediately" guard above can't accidentally over-clamp into "never
// fires." Fuel must decrease once BurnStart passes.
func TestCircularizeFiresAtApoapsis(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	const (
		periAlt = 200e3
		apoAlt  = 1000e3
	)
	rPeri := primaryR + periAlt
	rApo := primaryR + apoAlt
	a := (rPeri + rApo) / 2
	vPeri := math.Sqrt(mu * (2/rPeri - 1/a))
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	c.State.R = frame.ToWorld(orbital.Vec3{X: rPeri})
	c.State.V = frame.ToWorld(orbital.Vec3{Y: vPeri})

	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}
	c.Throttle = 0

	if _, err := w.PlanCircularizeAtApoapsis(); err != nil {
		t.Fatalf("PlanCircularizeAtApoapsis: %v", err)
	}
	fuelBefore := c.Fuel

	// Coast past apoapsis. Half-period for this orbit:
	period := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	target := w.Clock.SimTime.Add(time.Duration((period/2 + 60) * float64(time.Second)))
	for w.Clock.Warp() < 100 {
		w.Clock.WarpUp()
	}
	// Warp clamp shrinks simDelta as TriggerTime approaches so we
	// need enough iterations to actually reach apo. 50000 is plenty
	// for a 48-min half-period at base step 0.05 s.
	for i := 0; i < 50000 && w.Clock.SimTime.Before(target); i++ {
		w.Tick()
	}
	if fuelBefore-c.Fuel <= 0 {
		t.Errorf("fuel unchanged across apoapsis coast — planted burn did not fire (fuel: %.1f → %.1f)", fuelBefore, c.Fuel)
	}
}
