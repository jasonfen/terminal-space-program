package widgets

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// #367: after #363/#364 memoized the textured-disk raster and coalesced
// Canvas.String()'s per-cell styling, the dominant remaining idle-CPU cost
// on the orbit screen is orbit-LINE geometry — flattenEllipse's recursive
// chord subdivision (math.sin/cos via PositionAtTrueAnomaly) plus the
// walkPixelSegment dot-cadence walk, re-run for every drawn ellipse (every
// body in the system, the active vessel, every other vessel, every MP
// ghost) on every one of 20 ticks/second, even though at a steady warp the
// picture is as static frame to frame as the disk texture was.
//
// This is the disk cache's predict-on-change discipline (ADR 0017,
// screens/orbit_disk_cache.go) applied to the render side rather than the
// physics side: cache the FLATTENED + PROJECTED pixel list a curve drawer
// actually produced, keyed on everything that can change that list, and
// replay the cached pixels on a key hit instead of re-flattening.
//
// #363's pre-merge review caught a real bug in the disk cache from exactly
// this kind of shortcut: a cache keyed on the subject (body/orbit) alone,
// without the screen-projection state (pan, zoom, canvas size, view
// direction), served a stale frame the instant the camera moved. This
// cache's key is built entirely from Canvas's own view state (c.basis,
// c.scale, c.centerW, c.pxW, c.pxH) plus the curve's own shape — see
// curveCacheKeyFor — specifically so a pan/zoom/resize/rebasis busts it
// the same way an orbital-element change does. orbit_curve_cache_test.go
// (screens package) drives OrbitView.Render through a pan + zoom + focus
// switch and asserts the frame is never a stale cache hit.

// curveCacheEntry holds the last recorded pixel list for one curve
// (identified by curveID, an opaque string the caller mints — mirrors
// diskRasterCache using a body's ID as its map key) and the key it was
// recorded for.
type curveCacheEntry struct {
	key    curveCacheKey
	points []canvasPoint
}

// canvasPoint is one plotted pixel. int32 (not int) keeps a cached curve's
// retained size tight — the #363 review's "budget or shrink" lesson on
// diskOffsetCache — well within range for any realistic canvas (pxW/pxH
// are terminal-cell counts times 2/4, nowhere near 2^31).
type canvasPoint struct{ x, y int32 }

// curveCacheCap bounds curveCache to this many distinct curveIDs
// (LRU-evicted beyond it), the same discipline #363's review added to
// diskOffsetCache. Body count is fixed per system (Lumen's largest is 17),
// but vessel/ghost IDs can churn over a long-running --serve process as
// players connect and disconnect; capping keeps that churn from being a
// slow per-process leak instead of turning it into one.
const curveCacheCap = 64

// curveCacheKey fingerprints everything a curve's flattened+projected pixel
// list depends on: the orbit's shape and embedding (quantized against
// numerical jitter — a coasting vessel's live elements drift by
// floating-point noise every physics tick even absent thrust) and the
// canvas's own projection state (quantized against the same idle drift,
// but exact — not quantized — where the value is already discrete: pixel
// dimensions, span/spacing/occlusion-radius parameters). See
// curveShapeQuantum / curvePositionQuantum for the quantum derivation.
type curveCacheKey struct {
	aQ, eQ, iQ, omegaQ, argQ int64
	// relOffset / relBody are offset and bodyPos measured from the
	// canvas's OWN camera center (c.centerW), not in absolute world
	// coordinates. Two frames where offset and centerW moved by the same
	// delta (offset stays fixed on screen — nothing to redraw) hit the
	// cache; a real pan (only centerW moves) changes the delta and
	// correctly misses. This also means centerW never needs its own key
	// field — its effect on the projection is entirely captured here.
	relOffsetXQ, relOffsetYQ, relOffsetZQ int64
	relBodyXQ, relBodyYQ, relBodyZQ       int64
	minSpans                              int
	nearPx, farPx                         int
	bodyPxR                               int
	// View state (CRITICAL — #363's review finding: a render cache keyed
	// on the subject alone, without what the camera is doing, serves
	// stale pixels the instant the camera moves). scaleLogQ is quantized
	// on a log scale so the same relative precision holds across scale's
	// huge dynamic range (system view to surface view); basisQ quantizes
	// the raw unit-vector components at a fixed epsilon, tight enough at
	// any realistic canvas size (see curveBasisQuantum's derivation).
	scaleLogQ int64
	basisQ    [6]int64
	pxW, pxH  int
}

const (
	// curveScaleLogQuantum / curveBasisQuantum bound the projection-state
	// quantization error to sub-pixel at any on-canvas point. A basis
	// rotation of δ radians (from quantizing a unit-vector component at
	// epsilon ε, δ ≈ ε for small ε) or a relative scale change of δ moves
	// a point at pixel-distance R from the camera center by ≈ R·δ. Bounding
	// that under arcFlatTolerancePx (0.5px) at a generously large R of
	// 2000px (far past any real terminal's pixel diagonal) needs δ ≤
	// 0.5/2000 = 2.5e-4; both constants sit an order of magnitude under
	// that for margin.
	curveScaleLogQuantum = 2e-5
	curveBasisQuantum    = 2e-5
)

// curvePositionQuantum is the world-meter quantum for a screen-position
// input (relOffset / relBody / the 'a' shape term): the world distance
// that maps to arcFlatTolerancePx screen pixels at the canvas's current
// scale. Mirrors diskRasterQuantDeg's derivation (screens/orbit_disk_cache.go)
// — sub-pixel at the current zoom, recomputed every call since scale
// changes on every zoom step. Degenerate scale (≤0, non-finite — the
// canvas has nothing meaningful projected) falls back to a quantum of 1,
// which just means such frames don't get to share a cache bucket; they
// don't draw anything sensible to cache correctness over either.
func curvePositionQuantum(scale float64) float64 {
	if !(scale > 0) || math.IsInf(scale, 0) {
		return 1
	}
	return arcFlatTolerancePx / scale
}

// curveShapeQuantum derives the eccentricity / angle quantum from the
// orbit's own on-screen size (its apoapsis in pixels) rather than a fixed
// constant: the same absolute angular error sweeps a much smaller arc on a
// tiny, zoomed-out orbit than on one filling the canvas, so a single fixed
// quantum would either be needlessly tight (wasted cache misses) at small
// scale or unsafe (visible drift on a cache hit) at large scale. Mirrors
// diskRasterQuantDeg's "arc_px ≈ pxRadius × quantum(rad)" derivation with
// the orbit's apoapsis standing in for the disk's pixel radius.
func curveShapeQuantum(scale, apoapsisM float64) float64 {
	pxRadius := math.Abs(apoapsisM) * scale
	if !(pxRadius > 1) || math.IsInf(pxRadius, 0) {
		pxRadius = 1
	}
	return arcFlatTolerancePx / pxRadius
}

// quantize rounds x to integer multiples of step, returning 0 for a
// non-finite x (keeps the cache key deterministic and comparable) or a
// non-positive step (avoids a divide-by-zero/NaN cascade; callers only
// ever pass a derived-positive step, but a defensive floor costs nothing).
func quantize(x, step float64) int64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || !(step > 0) {
		return 0
	}
	return int64(math.Round(x / step))
}

// curveCacheKeyFor builds the cache key for a curve as drawn RIGHT NOW —
// this canvas's current view state plus the curve's own shape/embedding.
func (c *Canvas) curveCacheKeyFor(el orbital.Elements, offset orbital.Vec3, minSpans, nearPx, farPx int, bodyPos orbital.Vec3, bodyPxR int) curveCacheKey {
	posQ := curvePositionQuantum(c.scale)
	shapeQ := curveShapeQuantum(c.scale, el.Apoapsis())
	relOffset := offset.Sub(c.centerW)
	relBody := bodyPos.Sub(c.centerW)
	return curveCacheKey{
		aQ:          quantize(el.A, posQ),
		eQ:          quantize(el.E, shapeQ),
		iQ:          quantize(el.I, shapeQ),
		omegaQ:      quantize(el.Omega, shapeQ),
		argQ:        quantize(el.Arg, shapeQ),
		relOffsetXQ: quantize(relOffset.X, posQ),
		relOffsetYQ: quantize(relOffset.Y, posQ),
		relOffsetZQ: quantize(relOffset.Z, posQ),
		relBodyXQ:   quantize(relBody.X, posQ),
		relBodyYQ:   quantize(relBody.Y, posQ),
		relBodyZQ:   quantize(relBody.Z, posQ),
		minSpans:    minSpans,
		nearPx:      nearPx,
		farPx:       farPx,
		bodyPxR:     bodyPxR,
		scaleLogQ:   quantize(math.Log(c.scale), curveScaleLogQuantum),
		basisQ: [6]int64{
			quantize(c.basis.X.X, curveBasisQuantum), quantize(c.basis.X.Y, curveBasisQuantum), quantize(c.basis.X.Z, curveBasisQuantum),
			quantize(c.basis.Y.X, curveBasisQuantum), quantize(c.basis.Y.Y, curveBasisQuantum), quantize(c.basis.Y.Z, curveBasisQuantum),
		},
		pxW: c.pxW,
		pxH: c.pxH,
	}
}

// DrawEllipseClassCachedTagged is DrawEllipseClassTagged with the #367
// curve-geometry cache: on a key hit it replays the pixel list a prior
// identical-key call produced instead of re-flattening the ellipse and
// re-walking its chords. curveID is an opaque per-curve identity the
// caller mints (screens/orbit.go reuses the same Inspect owner key it
// already computes for tag.Owner) — kept as its own parameter rather than
// read off tag.Owner so the cache's identity contract doesn't silently
// depend on a field whose primary job is click hit-testing; an empty
// curveID never caches (a shared "" bucket across unrelated anonymous
// callers would serve one curve's geometry onto another's).
//
// Only geometry is cached, not color: tag is applied fresh on every call,
// hit or miss, so a color-only change (e.g. a target promotion) reuses the
// cached geometry rather than needlessly busting it.
//
// A cache HIT is sub-pixel-equivalent to a fresh draw, not necessarily
// byte-identical: the key quantizes its inputs to stay within
// arcFlatTolerancePx of a fresh recompute (see curvePositionQuantum /
// curveShapeQuantum), so during live ticking a hit can legitimately reuse
// geometry from up to one quantum step earlier — bounded, non-accumulating
// drift below what the renderer can express, not a correctness bug. A
// genuine MISS (a new key) always recomputes exactly, which is why the
// widgets-package tests assert byte-identical output there specifically.
func (c *Canvas) DrawEllipseClassCachedTagged(curveID string, el orbital.Elements, offset orbital.Vec3, minSpans int, class LineClass, bodyPos orbital.Vec3, bodyPxR int, tag CellTag) {
	if curveID == "" {
		c.DrawEllipseClassTagged(el, offset, minSpans, class, bodyPos, bodyPxR, tag)
		return
	}
	near, far := classEllipseSpacings(class)
	key := c.curveCacheKeyFor(el, offset, minSpans, near, far, bodyPos, bodyPxR)
	if c.curveCache == nil {
		c.curveCache = make(map[string]*curveCacheEntry)
	}
	if entry, ok := c.curveCache[curveID]; ok && entry.key == key {
		c.curveCacheHits++
		c.touchCurveCacheID(curveID)
		c.blitCurvePoints(entry.points, tag)
		return
	}
	var points []canvasPoint
	c.drawEllipseAdaptiveTaggedRecording(el, offset, minSpans, near, far, bodyPos, bodyPxR, tag, func(px, py int) {
		points = append(points, canvasPoint{int32(px), int32(py)})
	})
	c.curveCache[curveID] = &curveCacheEntry{key: key, points: points}
	c.curveCacheComputes++
	c.touchCurveCacheID(curveID)
	c.evictOldCurveCache()
}

// blitCurvePoints replays a cached pixel list: every point was already
// bounds- and occlusion-clipped when recorded (walkPixelSegment's plotPx
// only calls record for a pixel it actually set), so a hit is a flat loop
// with no flattening, no trig, no clipping math.
func (c *Canvas) blitCurvePoints(points []canvasPoint, tag CellTag) {
	tagged := tag != (CellTag{})
	if tagged && c.pixelTags == nil {
		c.pixelTags = make(map[[2]int]CellTag)
	}
	for _, p := range points {
		px, py := int(p.x), int(p.y)
		c.dc.Set(px, py)
		if tagged {
			c.pixelTags[[2]int{px, py}] = tag
		}
	}
}

// touchCurveCacheID moves curveID to the most-recently-used end of
// curveCacheOrder (appending it if new) — same O(cap) LRU as
// touchDiskOffsetRadius, fine at curveCacheCap's small bound.
func (c *Canvas) touchCurveCacheID(curveID string) {
	for i, id := range c.curveCacheOrder {
		if id == curveID {
			c.curveCacheOrder = append(c.curveCacheOrder[:i], c.curveCacheOrder[i+1:]...)
			break
		}
	}
	c.curveCacheOrder = append(c.curveCacheOrder, curveID)
}

// evictOldCurveCache drops the least-recently-used curveIDs once
// curveCache exceeds curveCacheCap.
func (c *Canvas) evictOldCurveCache() {
	for len(c.curveCacheOrder) > curveCacheCap {
		oldest := c.curveCacheOrder[0]
		c.curveCacheOrder = c.curveCacheOrder[1:]
		delete(c.curveCache, oldest)
	}
}

// CurveCacheStats reports the #367 curve-geometry cache's cumulative
// hit/recompute counts. Exported as a test hook: the cache lives on
// Canvas, but the call sites that exercise it end-to-end (screens/orbit.go)
// live in a different package, so screens/orbit_curve_cache_test.go needs
// this to assert an idle re-render hits and a pan/zoom/focus change misses.
func (c *Canvas) CurveCacheStats() (hits, computes int) {
	return c.curveCacheHits, c.curveCacheComputes
}

// ResetCurveCache drops every cached curve entry, forcing the next draw of
// each curve to recompute from scratch. Exported as a test hook: a
// cache-vs-ground-truth comparison needs a way to force a genuinely
// cache-free render on demand. screens/orbit_curve_cache_test.go's
// TestOrbitViewCurveCacheHitMatchesUncachedAfterPan calls this on the
// reference OrbitView's canvas immediately before the FINAL render under
// comparison (not earlier) — the reference still has to replay the exact
// same warm-up/pan sequence as the OrbitView under test so both land on the
// same camera state (ADR 0021's Framing Event only re-fits on Focus/
// ViewMode/System changes or a resize, so reordering the pans ahead of the
// warm-up render would fit a different — and wrong — baseScale). Dropping
// the cache only immediately before that last render isolates "did this
// specific render use a cached entry" without disturbing the camera framing
// both views need to share.
func (c *Canvas) ResetCurveCache() {
	c.curveCache = nil
	c.curveCacheOrder = nil
}
