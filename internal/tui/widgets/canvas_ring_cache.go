package widgets

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// #369: after #367/#368 memoized orbit-LINE geometry (canvas_curve_cache.go),
// the prod host-seat profile that originally flagged that cost (#367's
// issue body, detached 200×50 tmux) ALSO named
// Canvas.RingTiltedOutlineTagged among the hot symbols — a different draw
// primitive (tilted ring-band outlines: Saturn's C/B/A/F bands), out of
// scope for #368's cache, which covers only the 4 DrawEllipseClassTagged
// call sites in orbit.go.
//
// MEASURED before writing this file (per the issue's instruction — see the
// PR body for the full profile/benchmark numbers): a Saturn-focused idle
// render (screens.BenchmarkOrbitViewRenderIdleRinged — the default
// BenchmarkOrbitViewRenderIdle scene never brings a ringed body into view
// at all, so it can't see this cost) ran ~2.03ms/frame on pre-#369 main vs
// ~1.76ms for the same canvas with no ring bands in view —
// RingTiltedOutlineTagged alone was ~3.3% of total cpu samples and ~24% of
// cumulative time under (*OrbitView).Render in a `pprof -focus` breakdown.
// Not negligible when a ringed body actually fills the view (worst case:
// up to 64 concentric outlines per band × 4 bands, each up to
// 4×(pxW+pxH) samples, every one of 20 ticks/second) — this earns the
// same treatment #367 gave orbit lines.
//
// Same discipline as the curve cache: memoize the drawn ring's
// projected pixel list, keyed on everything that can change it — the
// ring's own embedding (center, e1/e2 basis, radius) AND the canvas's own
// view state (scale, basis, camera center, pixel dimensions), for the
// exact #363-review reason curveCacheKey documents (a cache keyed on the
// subject alone serves a stale frame the instant the camera moves).

// ringCacheEntry holds the last recorded pixel list for one ring outline
// (identified by curveID, an opaque string the caller mints — orbit.go
// mints one per body+band+concentric-outline-index) and the key it was
// recorded for. Reuses curveCache's canvasPoint (int32 x, y) — same
// "retained size tight" reasoning applies here.
type ringCacheEntry struct {
	key    ringCacheKey
	points []canvasPoint
}

// ringCacheCap bounds ringCache the same LRU way curveCacheCap bounds
// curveCache. Sized to the deterministic worst case rather than an
// estimate: BodyRingBands' only ringed body today (Saturn) has 4 bands,
// each capped at 64 concentric outlines (orbit.go's per-band `n`, capped
// at 64) — 256 distinct curveIDs, all stable strings ("ring:saturn:B:i")
// that don't churn the way vessel/ghost curveIDs can. A future second
// ringed body would need this raised; 256 leaves headroom without it.
const ringCacheCap = 256

// ringCacheKey fingerprints everything a ring outline's projected pixel
// list depends on. Mirrors curveCacheKey's shape and camera-relative
// centering exactly (see its doc comment for why relCenter is measured
// from c.centerW rather than in absolute world coordinates).
type ringCacheKey struct {
	relCenterXQ, relCenterYQ, relCenterZQ int64
	// e1Q / e2Q are the ring-plane basis vectors' raw components,
	// quantized by ringBasisQuantum (angle-equivalent to
	// curveShapeQuantum, not curveBasisQuantum — these are WORLD-space
	// unit vectors scaled by rMeters, not canvas-basis vectors scaled by
	// a pixel distance, so the error budget has to scale off the ring's
	// OWN pixel radius the same way curveShapeQuantum derives an orbit's
	// element quantum off its apoapsis).
	e1Q, e2Q [3]int64
	rQ       int64
	// View state (CRITICAL — see curveCacheKey's identical field for the
	// #363 review finding this protects against).
	scaleLogQ int64
	basisQ    [6]int64
	pxW, pxH  int
}

// ringBasisQuantum derives the angle-equivalent quantum for e1/e2's raw
// components from the ring's own on-screen radius, exactly the way
// curveShapeQuantum derives an orbit's shape quantum from its apoapsis: a
// basis error of δ radians moves a point at the ring's pixel radius
// (rMeters·scale) by ≈ rMeters·scale·δ screen pixels, so bounding that
// under arcFlatTolerancePx needs δ ≤ arcFlatTolerancePx / (rMeters·scale)
// — precisely curveShapeQuantum's formula with rMeters standing in for
// the orbit's apoapsis.
func ringBasisQuantum(scale, rMeters float64) float64 {
	return curveShapeQuantum(scale, rMeters)
}

// ringCacheKeyFor builds the cache key for a ring outline as drawn RIGHT
// NOW — this canvas's current view state plus the ring's own embedding.
func (c *Canvas) ringCacheKeyFor(center, e1, e2 orbital.Vec3, rMeters float64) ringCacheKey {
	posQ := curvePositionQuantum(c.scale)
	basisQ := ringBasisQuantum(c.scale, rMeters)
	relCenter := center.Sub(c.centerW)
	return ringCacheKey{
		relCenterXQ: quantize(relCenter.X, posQ),
		relCenterYQ: quantize(relCenter.Y, posQ),
		relCenterZQ: quantize(relCenter.Z, posQ),
		e1Q: [3]int64{
			quantize(e1.X, basisQ), quantize(e1.Y, basisQ), quantize(e1.Z, basisQ),
		},
		e2Q: [3]int64{
			quantize(e2.X, basisQ), quantize(e2.Y, basisQ), quantize(e2.Z, basisQ),
		},
		rQ:        quantize(rMeters, posQ),
		scaleLogQ: quantize(math.Log(c.scale), curveScaleLogQuantum),
		basisQ: [6]int64{
			quantize(c.basis.X.X, curveBasisQuantum), quantize(c.basis.X.Y, curveBasisQuantum), quantize(c.basis.X.Z, curveBasisQuantum),
			quantize(c.basis.Y.X, curveBasisQuantum), quantize(c.basis.Y.Y, curveBasisQuantum), quantize(c.basis.Y.Z, curveBasisQuantum),
		},
		pxW: c.pxW,
		pxH: c.pxH,
	}
}

// RingTiltedOutlineCachedTagged is RingTiltedOutlineTagged with the #369
// ring-geometry cache: on a key hit it replays the pixel list a prior
// identical-key call produced instead of resampling+reprojecting the
// ring. curveID is an opaque per-outline identity the caller mints
// (screens/orbit.go uses "ring:<bodyID>:<bandIdx>:<i>" — the ring bands
// aren't Inspect-owned the way orbit lines are, so unlike
// DrawEllipseClassCachedTagged this isn't reusing an existing Inspect
// owner key, just minting a stable cache identity) — kept as its own
// parameter for the same reason DrawEllipseClassCachedTagged's is: an
// empty curveID never caches, so unrelated anonymous callers can't
// collide on a shared "" bucket.
//
// Same precision contract as DrawEllipseClassCachedTagged: a HIT is
// sub-pixel-equivalent to a fresh draw (within arcFlatTolerancePx), not
// necessarily byte-identical, because the key quantizes its inputs — see
// ringCacheKeyFor / ringBasisQuantum. A genuine MISS always recomputes
// exactly.
func (c *Canvas) RingTiltedOutlineCachedTagged(curveID string, center, e1, e2 orbital.Vec3, rMeters float64, tag CellTag) {
	if curveID == "" {
		c.RingTiltedOutlineTagged(center, e1, e2, rMeters, tag)
		return
	}
	key := c.ringCacheKeyFor(center, e1, e2, rMeters)
	if c.ringCache == nil {
		c.ringCache = make(map[string]*ringCacheEntry)
	}
	if entry, ok := c.ringCache[curveID]; ok && entry.key == key {
		c.ringCacheHits++
		c.touchRingCacheID(curveID)
		c.blitCurvePoints(entry.points, tag)
		return
	}
	var points []canvasPoint
	c.ringTiltedOutlineTaggedRecording(center, e1, e2, rMeters, tag, func(px, py int) {
		points = append(points, canvasPoint{int32(px), int32(py)})
	})
	c.ringCache[curveID] = &ringCacheEntry{key: key, points: points}
	c.ringCacheComputes++
	c.touchRingCacheID(curveID)
	c.evictOldRingCache()
}

// touchRingCacheID moves curveID to the most-recently-used end of
// ringCacheOrder — same O(cap) LRU as touchCurveCacheID.
func (c *Canvas) touchRingCacheID(curveID string) {
	for i, id := range c.ringCacheOrder {
		if id == curveID {
			c.ringCacheOrder = append(c.ringCacheOrder[:i], c.ringCacheOrder[i+1:]...)
			break
		}
	}
	c.ringCacheOrder = append(c.ringCacheOrder, curveID)
}

// evictOldRingCache drops the least-recently-used curveIDs once
// ringCache exceeds ringCacheCap.
func (c *Canvas) evictOldRingCache() {
	for len(c.ringCacheOrder) > ringCacheCap {
		oldest := c.ringCacheOrder[0]
		c.ringCacheOrder = c.ringCacheOrder[1:]
		delete(c.ringCache, oldest)
	}
}

// RingCacheStats reports the #369 ring-geometry cache's cumulative
// hit/recompute counts — CurveCacheStats' sibling, same test-hook role
// for screens-package end-to-end tests that can't reach Canvas's private
// counters directly.
func (c *Canvas) RingCacheStats() (hits, computes int) {
	return c.ringCacheHits, c.ringCacheComputes
}

// ResetRingCache drops every cached ring entry, forcing the next draw of
// each ring outline to recompute from scratch. Mirrors ResetCurveCache's
// role and the same "drop immediately before the one render under
// comparison" usage discipline — see its doc comment.
func (c *Canvas) ResetRingCache() {
	c.ringCache = nil
	c.ringCacheOrder = nil
}
