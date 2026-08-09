package widgets

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// testRing is a modest tilted ring embedding, sized to land real pixels on
// a small test canvas — mirrors testEllipse()'s role in
// canvas_curve_cache_test.go.
func testRingEmbedding() (center, e1, e2 orbital.Vec3, rMeters float64) {
	e1 = orbital.Vec3{X: 1}
	e2 = orbital.Vec3{Y: 0.8, Z: 0.6} // tilted, not orthogonal-to-basis-plane
	return orbital.Vec3{}, e1, e2, 15
}

func newRingCacheTestCanvas() *Canvas {
	c := NewCanvas(20, 10) // 40x40 px
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	return c
}

// TestRingTiltedOutlineCachedTaggedMatchesUncached confirms a cache MISS
// (first draw) produces byte-identical canvas output to the uncached
// RingTiltedOutlineTagged.
func TestRingTiltedOutlineCachedTaggedMatchesUncached(t *testing.T) {
	forceANSIColor(t)
	center, e1, e2, r := testRingEmbedding()
	tag := CellTag{Color: "#00FF00"}

	cached := newRingCacheTestCanvas()
	cached.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r, tag)

	uncached := newRingCacheTestCanvas()
	uncached.RingTiltedOutlineTagged(center, e1, e2, r, tag)

	if got, want := cached.String(), uncached.String(); got != want {
		t.Errorf("cached miss diverges from uncached draw\ngot:  %q\nwant: %q", got, want)
	}
}

// TestRingTiltedOutlineCachedTaggedHitsOnRepeat confirms an identical
// second call is a cache hit (no recompute) and still renders the same
// picture.
func TestRingTiltedOutlineCachedTaggedHitsOnRepeat(t *testing.T) {
	forceANSIColor(t)
	center, e1, e2, r := testRingEmbedding()
	tag := CellTag{Color: "#00FF00"}

	c := newRingCacheTestCanvas()
	c.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r, tag)
	first := c.String()
	computesBefore := c.ringCacheComputes

	c.Clear()
	c.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r, tag)
	second := c.String()

	if c.ringCacheComputes != computesBefore {
		t.Fatalf("expected the identical second call to be a cache HIT, got a recompute (computes %d -> %d)", computesBefore, c.ringCacheComputes)
	}
	if c.ringCacheHits == 0 {
		t.Error("expected ringCacheHits to be nonzero after an identical repeat call")
	}
	if first != second {
		t.Errorf("cache-hit render diverges from the original draw\nfirst:  %q\nsecond: %q", first, second)
	}
}

// TestRingTiltedOutlineCachedTaggedColorOnlyChangeStillHits confirms the
// cache is keyed on GEOMETRY, not color.
func TestRingTiltedOutlineCachedTaggedColorOnlyChangeStillHits(t *testing.T) {
	forceANSIColor(t)
	center, e1, e2, r := testRingEmbedding()

	c := newRingCacheTestCanvas()
	c.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r, CellTag{Color: "#00FF00"})
	computesBefore := c.ringCacheComputes

	c.Clear()
	c.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r, CellTag{Color: "#FF0000"})
	recolored := c.String()

	if c.ringCacheComputes != computesBefore {
		t.Fatalf("a color-only change should reuse cached geometry (HIT), got a recompute (computes %d -> %d)", computesBefore, c.ringCacheComputes)
	}

	fresh := newRingCacheTestCanvas()
	fresh.RingTiltedOutlineTagged(center, e1, e2, r, CellTag{Color: "#FF0000"})
	want := fresh.String()

	if recolored != want {
		t.Errorf("recolored cache-hit render diverges from an uncached reference in the new color\ngot:  %q\nwant: %q", recolored, want)
	}
}

// TestRingTiltedOutlineCachedTaggedEmptyCurveIDNeverCaches confirms an
// empty curveID falls through to the plain uncached path and never
// touches ringCache.
func TestRingTiltedOutlineCachedTaggedEmptyCurveIDNeverCaches(t *testing.T) {
	c := newRingCacheTestCanvas()
	center, e1, e2, r := testRingEmbedding()
	c.RingTiltedOutlineCachedTagged("", center, e1, e2, r, CellTag{Color: "#00FF00"})
	if len(c.ringCache) != 0 {
		t.Errorf("empty curveID populated ringCache (%d entries), want 0", len(c.ringCache))
	}
}

// TestRingTiltedOutlineCachedTaggedBustsOnViewChange is the ring-cache
// analog of TestDrawEllipseClassCachedTaggedBustsOnViewChange: pan, zoom,
// rebasis, and resize must all bust the geometry cache. Each case asserts
// BOTH a recompute happened AND the new frame matches a from-scratch
// (never-cached) reference — a cache that merely detects a miss but still
// blits the OLD geometry would pass a hit/miss-counter-only test while
// still being visibly broken.
//
// Non-vacuousness check performed manually (not committed): temporarily
// hard-coding scaleLogQ/basisQ/pxW/pxH out of ringCacheKeyFor made every
// sub-test in this function fail exactly as expected (recompute count
// stayed flat instead of increasing) before the fix was reverted —
// confirming the assertions actually exercise the view-state coverage
// rather than passing regardless of what the key contains.
func TestRingTiltedOutlineCachedTaggedBustsOnViewChange(t *testing.T) {
	forceANSIColor(t)
	center, e1, e2, r := testRingEmbedding()

	cases := []struct {
		name  string
		setup func(c *Canvas)
	}{
		{"pan", func(c *Canvas) { c.Center(orbital.Vec3{X: 5, Y: -3}) }},
		{"zoom", func(c *Canvas) { c.SetScale(2) }},
		{"rebasis", func(c *Canvas) { c.SetBasis(Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Z: 1}}) }},
		{"resize", func(c *Canvas) { c.Resize(30, 15) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newRingCacheTestCanvas()
			c.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r, CellTag{Color: "#00FF00"})
			computesBefore := c.ringCacheComputes

			c.Clear()
			tc.setup(c)
			c.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r, CellTag{Color: "#00FF00"})
			got := c.String()

			if c.ringCacheComputes == computesBefore {
				t.Fatalf("%s: expected a cache MISS (recompute) after the view changed, got a HIT", tc.name)
			}

			fresh := newRingCacheTestCanvas()
			tc.setup(fresh)
			fresh.RingTiltedOutlineTagged(center, e1, e2, r, CellTag{Color: "#00FF00"})
			want := fresh.String()

			if got != want {
				t.Errorf("%s: post-change cache-miss render diverges from an uncached reference\ngot:  %q\nwant: %q", tc.name, got, want)
			}
		})
	}
}

// TestRingTiltedOutlineCachedTaggedBustsOnEmbeddingChange confirms a real
// (well past quantization) change to the ring's own embedding — center,
// basis, or radius — busts the cache too.
func TestRingTiltedOutlineCachedTaggedBustsOnEmbeddingChange(t *testing.T) {
	forceANSIColor(t)
	center, e1, e2, r := testRingEmbedding()

	cases := []struct {
		name string
		draw func(c *Canvas)
	}{
		{"center", func(c *Canvas) {
			c.RingTiltedOutlineCachedTagged("ring-1", orbital.Vec3{X: 8}, e1, e2, r, CellTag{Color: "#00FF00"})
		}},
		{"radius", func(c *Canvas) {
			c.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r*1.5, CellTag{Color: "#00FF00"})
		}},
		{"e1", func(c *Canvas) {
			c.RingTiltedOutlineCachedTagged("ring-1", center, orbital.Vec3{Y: 1}, e2, r, CellTag{Color: "#00FF00"})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newRingCacheTestCanvas()
			c.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r, CellTag{Color: "#00FF00"})
			computesBefore := c.ringCacheComputes

			c.Clear()
			tc.draw(c)
			got := c.String()

			if c.ringCacheComputes == computesBefore {
				t.Fatalf("%s: expected a cache MISS after the embedding changed, got a HIT", tc.name)
			}
			_ = got
		})
	}
}

// TestRingTiltedOutlineCachedTaggedToleratesSubPixelJitter confirms the
// quantization half of the contract: a change far below
// curvePositionQuantum/ringBasisQuantum at this canvas's scale must still
// be a cache HIT.
//
// Non-vacuousness check performed manually (not committed): tightening
// ringBasisQuantum's formula (dropping the arcFlatTolerancePx numerator
// to an unreasonably small constant) made this test fail as expected
// before the change was reverted — confirming the quantum is actually
// gating the hit, not a check that would pass unconditionally.
func TestRingTiltedOutlineCachedTaggedToleratesSubPixelJitter(t *testing.T) {
	center, e1, e2, r := testRingEmbedding()
	e1Jittered := e1
	e1Jittered.Y += 1e-9 // far below ringBasisQuantum at this canvas's scale

	c := newRingCacheTestCanvas()
	c.RingTiltedOutlineCachedTagged("ring-1", center, e1, e2, r, CellTag{Color: "#00FF00"})
	computesBefore := c.ringCacheComputes

	c.Clear()
	c.RingTiltedOutlineCachedTagged("ring-1", center, e1Jittered, e2, r, CellTag{Color: "#00FF00"})

	if c.ringCacheComputes != computesBefore {
		t.Errorf("sub-pixel e1 jitter busted the cache (computes %d -> %d) — quantum is too tight", computesBefore, c.ringCacheComputes)
	}
}

// TestRingTiltedOutlineCachedTaggedBoundedAcrossManyCurveIDs sweeps far
// more distinct curveIDs than ringCacheCap through the same canvas — a
// deeply-zoomed ring system's full band×outline sweep, worst case — and
// asserts the cache never exceeds its bound, and that eviction is LRU
// (not just a bound).
func TestRingTiltedOutlineCachedTaggedBoundedAcrossManyCurveIDs(t *testing.T) {
	c := newRingCacheTestCanvas()
	center, e1, e2, r := testRingEmbedding()
	curveIDAt := func(i int) string {
		return "ring-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
	}
	const n = ringCacheCap * 2
	for i := 0; i < n; i++ {
		id := curveIDAt(i)
		c.RingTiltedOutlineCachedTagged(id, center, e1, e2, r, CellTag{Color: "#00FF00"})
		if len(c.ringCache) > ringCacheCap {
			t.Fatalf("after %d curveIDs: ringCache holds %d entries, want <= %d (cap)", i+1, len(c.ringCache), ringCacheCap)
		}
	}
	if got := len(c.ringCache); got != ringCacheCap {
		t.Errorf("after sweeping %d curveIDs, ringCache = %d entries, want exactly the cap (%d)", n, got, ringCacheCap)
	}
	mostRecent := curveIDAt(n - 1)
	leastRecent := curveIDAt(0)
	if _, ok := c.ringCache[mostRecent]; !ok {
		t.Errorf("most-recently-drawn curveID %q was evicted — eviction isn't LRU", mostRecent)
	}
	if _, ok := c.ringCache[leastRecent]; ok {
		t.Errorf("least-recently-drawn curveID %q is still resident after the sweep — eviction isn't LRU", leastRecent)
	}
}
