package spacecraft

import (
	"math"
	"testing"
	"time"
)

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

// #996: a burn the lit stage can't cover alone is timed stage by stage. The
// player decouples the dry stage and the next one carries on, so the plan sums
// each stage's own rocket-equation time.
//
// Fixture: core (1,000 dry, 200 fuel, 100 kN, Isp 300) under S2 (2,000 dry,
// 5,000 fuel, 50 kN, Isp 350).
//
//	core phase: m0 = 8,200, mf = 8,000, dv1 = 300 g0 ln(8200/8000), t1 = 200 kg / (100e3/(300 g0)) kg/s
//	S2 phase:   m0 = 7,000 (core dropped), t2 = (7000/mdot2)(1 - exp(-(dv-dv1)/(350 g0)))
func TestBurnTimeForDVCarriesAcrossStages(t *testing.T) {
	c := &Spacecraft{Stages: []Stage{
		{DryMass: 1000, FuelMass: 200, FuelCapacity: 200, Thrust: 100_000, Isp: 300},
		{DryMass: 2000, FuelMass: 5000, FuelCapacity: 5000, Thrust: 50_000, Isp: 350},
	}}
	c.SyncFields()
	dv1 := 300 * g0 * math.Log(8200.0/8000.0)
	t1 := 200 / (100_000.0 / (300 * g0))
	mdot2 := 50_000.0 / (350 * g0)
	tail := func(extra float64) float64 { return (7000 / mdot2) * (1 - math.Exp(-extra/(350*g0))) }
	secs := func(d time.Duration) float64 { return d.Seconds() }

	// Equal case: exactly the core's capacity, no carry, the old single-engine time.
	if got := secs(c.BurnTimeForDV(dv1)); math.Abs(got-t1) > 1e-6 {
		t.Errorf("dv == core capacity: %.6f s, want %.6f", got, t1)
	}
	// Just over: the core's time plus a sliver on S2.
	if got, want := secs(c.BurnTimeForDV(dv1+1)), t1+tail(1); math.Abs(got-want) > 1e-6 {
		t.Errorf("dv = capacity+1: %.6f s, want %.6f", got, want)
	}
	// FH circularise shape: a small core, a long S2 burn.
	extra := 4000.0
	got, want := secs(c.BurnTimeForDV(dv1+extra)), t1+tail(extra)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("dv = capacity+4000: %.3f s, want %.3f", got, want)
	}
	if got < 200 {
		t.Errorf("a ~4 km/s burn on a 50 kN stage reads %.1f s; the core alone cannot time it", got)
	}
}
