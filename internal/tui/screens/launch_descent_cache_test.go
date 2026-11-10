// Package screens — the descent corridor's predict-on-change cache
// (issue #377): PredictPoweredStop / PredictBurnAt must not re-run every
// render frame (a 1.6 km/s stop is ~800 integrator sub-steps, and the
// burn-at search multiplies that by roughly another order of magnitude).
// Mirrors orbit_predict_cache_test.go / orbit_soipass_test.go's shape.
// The cache itself lives on the shared *OrbitView* (ADR 0051 slice 1),
// reached here via LaunchView's hudSource pointer.

package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestDescentStopCacheHoldsAcrossIdleFrames: re-rendering a descending
// craft repeatedly with nothing advanced (no tick, same clock, same
// state) recomputes the stop forecast only once.
func TestDescentStopCacheHoldsAcrossIdleFrames(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := descendingMoonCraft(t, 20_000, 120)

	for i := 0; i < 5; i++ {
		v.Render(w, 200, 60)
	}
	if v.hudSource.descentStopCacheComputes != 1 {
		t.Errorf("descentStopCacheComputes = %d after 5 idle renders, want 1 (predict-on-change cache)", v.hudSource.descentStopCacheComputes)
	}
}

// TestDescentStopCacheBustsOnSabotagedKey is the #369-lesson test: a
// cache that always returns its stored data regardless of the key would
// pass TestDescentStopCacheHoldsAcrossIdleFrames vacuously (nothing
// there proves the key comparison is what's gating the hit — it could
// just always hit). Sabotage the STORED key after a real compute, with
// the craft's actual state left untouched, and confirm the very next
// render recomputes anyway: the only way that happens is if
// cachedDescentStop is genuinely comparing the freshly-built key against
// what's stored, not returning cached data unconditionally.
func TestDescentStopCacheBustsOnSabotagedKey(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := descendingMoonCraft(t, 20_000, 120)

	v.Render(w, 200, 60)
	if v.hudSource.descentStopCacheComputes != 1 {
		t.Fatalf("setup: descentStopCacheComputes = %d after first render, want 1", v.hudSource.descentStopCacheComputes)
	}
	if !v.hudSource.descentStopCache.has {
		t.Fatal("setup: cache did not populate on the first render")
	}

	// Sabotage: corrupt the stored key's clock bucket so it can no
	// longer match a freshly-built key, WITHOUT touching the craft or the
	// world clock at all.
	v.hudSource.descentStopCache.key.clockBucket = -1

	v.Render(w, 200, 60)
	if v.hudSource.descentStopCacheComputes != 2 {
		t.Errorf("descentStopCacheComputes = %d after a sabotaged-key render, want 2 (the cache must have missed)", v.hudSource.descentStopCacheComputes)
	}

	// And confirm the cache is genuinely usable again afterwards — the
	// sabotage was a one-off corruption, not a permanent break.
	v.Render(w, 200, 60)
	if v.hudSource.descentStopCacheComputes != 2 {
		t.Errorf("descentStopCacheComputes = %d after re-rendering post-sabotage, want 2 (should hit again)", v.hudSource.descentStopCacheComputes)
	}
}

// TestDescentStopCacheBustsOnBurnStart: the moment a burn starts, the
// cache key's midBurn field flips, so the very next render recomputes —
// required for `burn at` to disappear immediately (issue #377 §2) rather
// than waiting for velocity to drift past the position/velocity quanta.
func TestDescentStopCacheBustsOnBurnStart(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := descendingMoonCraft(t, 20_000, 120)

	v.Render(w, 200, 60)
	if v.hudSource.descentStopCacheComputes != 1 {
		t.Fatalf("setup: descentStopCacheComputes = %d, want 1", v.hudSource.descentStopCacheComputes)
	}

	c := w.ActiveCraft()
	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100}

	v.Render(w, 200, 60)
	if v.hudSource.descentStopCacheComputes != 2 {
		t.Errorf("descentStopCacheComputes = %d after ActiveBurn was set, want 2 (midBurn must bust the key)", v.hudSource.descentStopCacheComputes)
	}
}

// marsEntryCraft is a Ground-Station Lander on a 150 km Mars orbit with its
// periapsis 5 km under the ground: an entry coast. air=false strips the air.
func marsEntryCraft(t *testing.T, air bool) (*sim.World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: "Ground-Station-Lander", ParentBodyID: "mars", AltitudeM: 150e3})
	if err != nil {
		t.Fatal(err)
	}
	if !air {
		c.Primary.Atmosphere = nil
	}
	mu, rp := c.Primary.GravitationalParameter(), c.Primary.RadiusMeters()
	r := c.State.R.Norm()
	c.State.V = c.State.V.Unit().Scale(math.Sqrt(2 * mu * (rp - 5000) / (r * (r + rp - 5000))))
	c.State.M = c.TotalMass()
	return w, c
}

// ADR 0069 guard 2: while the cue is ahead on a body with air, stop: is the
// brake lit at the cue (it stops, margin >= 0), not the lit-now answer; with no
// air it stays the lit-now forecast, equal to the plain PredictPoweredStop.
func TestStopReadsTheBrakeAtTheCueOnAir(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, c := marsEntryCraft(t, true)
	dat := v.cachedDescentStop(w, c)
	if !dat.hasBurnAt || dat.burnAt.InSec <= 0 {
		t.Fatalf("no cue ahead: has=%v in=%v", dat.hasBurnAt, dat.burnAt.InSec)
	}
	if !dat.stopOK || dat.stop.Outcome != sim.StopStopped || dat.stop.MarginM < 0 {
		t.Errorf("stop: with the cue ahead = ok %v %v margin %.0f, want the cue's brake: stopped, margin >= 0", dat.stopOK, dat.stop.Outcome, dat.stop.MarginM)
	}
	lit, litOK := sim.PredictPoweredStop(c, sim.DescentPredictHorizon)
	if litOK && lit.Outcome == sim.StopStopped {
		t.Errorf("fixture does not separate the two: lit-now also stops (margin %.0f)", lit.MarginM)
	}
	t.Logf("Mars entry coast: cue %.1f km T-%.0f s, stop: margin %.0f m; lit-now reads %v", dat.burnAt.AltitudeM/1000, dat.burnAt.InSec, dat.stop.MarginM, lit.Outcome)

	// no air: the lit-now forecast, bit for bit (no cue is found there at all)
	w2, c2 := marsEntryCraft(t, false)
	v2 := NewOrbitView(launchThemeForTest())
	dat2 := v2.cachedDescentStop(w2, c2)
	want, wantOK := sim.PredictPoweredStop(c2, sim.DescentPredictHorizon)
	if dat2.stop != want || dat2.stopOK != wantOK {
		t.Errorf("airless stop: = %+v ok=%v, want the lit-now forecast %+v ok=%v", dat2.stop, dat2.stopOK, want, wantOK)
	}
}
