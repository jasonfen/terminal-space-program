// Package screens — the descent corridor's predict-on-change cache
// (issue #377): PredictPoweredStop / PredictBurnAt must not re-run every
// render frame (a 1.6 km/s stop is ~800 integrator sub-steps, and the
// burn-at search multiplies that by roughly another order of magnitude).
// Mirrors orbit_predict_cache_test.go / orbit_soipass_test.go's shape.

package screens

import (
	"testing"

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
	if v.descentStopCacheComputes != 1 {
		t.Errorf("descentStopCacheComputes = %d after 5 idle renders, want 1 (predict-on-change cache)", v.descentStopCacheComputes)
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
	if v.descentStopCacheComputes != 1 {
		t.Fatalf("setup: descentStopCacheComputes = %d after first render, want 1", v.descentStopCacheComputes)
	}
	if !v.descentStopCache.has {
		t.Fatal("setup: cache did not populate on the first render")
	}

	// Sabotage: corrupt the stored key's clock bucket so it can no
	// longer match a freshly-built key, WITHOUT touching the craft or the
	// world clock at all.
	v.descentStopCache.key.clockBucket = -1

	v.Render(w, 200, 60)
	if v.descentStopCacheComputes != 2 {
		t.Errorf("descentStopCacheComputes = %d after a sabotaged-key render, want 2 (the cache must have missed)", v.descentStopCacheComputes)
	}

	// And confirm the cache is genuinely usable again afterwards — the
	// sabotage was a one-off corruption, not a permanent break.
	v.Render(w, 200, 60)
	if v.descentStopCacheComputes != 2 {
		t.Errorf("descentStopCacheComputes = %d after re-rendering post-sabotage, want 2 (should hit again)", v.descentStopCacheComputes)
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
	if v.descentStopCacheComputes != 1 {
		t.Fatalf("setup: descentStopCacheComputes = %d, want 1", v.descentStopCacheComputes)
	}

	c := w.ActiveCraft()
	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100}

	v.Render(w, 200, 60)
	if v.descentStopCacheComputes != 2 {
		t.Errorf("descentStopCacheComputes = %d after ActiveBurn was set, want 2 (midBurn must bust the key)", v.descentStopCacheComputes)
	}
}
