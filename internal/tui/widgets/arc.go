package widgets

// Adaptive arc-length sampling for trajectory curves (ADR 0042 §3).
//
// Every curve the orbit map draws — the active vessel's ellipse, other
// vessels' and ghosts' ellipses, body orbits, node legs, encounter arcs,
// the SOI ring — now goes through one sampling policy:
//
//  1. FLATTEN. The curve is subdivided into chords that, once projected,
//     are straight to within arcFlatTolerancePx. A chord that passes that
//     test IS the curve on screen at the current zoom, so nothing is lost
//     by inking it as a straight run. Subdivision is adaptive and
//     view-pruned: a sub-span whose projected bounding box misses the
//     canvas is dropped in O(1), so a curve that is mostly (or entirely)
//     off-screen costs almost nothing, and the work that IS done lands on
//     the visible arc.
//
//  2. INK. Each surviving chord is walked in PIXELS, one dot every
//     `spacingPx`, with the dash phase carried ACROSS chord boundaries.
//     The cadence is therefore an arc-length parameter measured on screen,
//     not a sample-index stride — constant dot spacing at any zoom (ADR
//     0023 C, generalised from foreign-SOI legs to every curve), and the
//     parameter a future dashed / dotted line vocabulary needs is already
//     the one being counted.
//
// Before this, ellipses were fixed 360-sample stride-dot loops: zoom in and
// consecutive samples spread apart until the ellipse read as scattered dust
// rather than a line.

import (
	"math"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

const (
	// arcFlatTolerancePx is how far (canvas pixels) a chord may depart
	// from the arc it stands in for. Half a braille pixel is below what
	// the renderer can express, so a chord inside it is visually exact.
	arcFlatTolerancePx = 0.5

	// arcCoarseSpans is the first-pass division of a closed curve. Each
	// span is then flattened adaptively (or pruned). It has to be fine
	// enough that a span's arc can't sneak across the canvas while its
	// endpoints and midpoint all sit outside the pruning box — see
	// arcPruneChordFrac.
	arcCoarseSpans = 256

	// arcMaxSpans caps the coarse pass so a caller-supplied minimum can't
	// blow up the per-frame loop.
	arcMaxSpans = 2048

	// arcMaxDepth bounds the subdivision of one coarse span. Chord
	// deviation falls ~4× per level, so 12 levels absorb a 16-million-fold
	// zoom-in past the point where the coarse span was already flat.
	arcMaxDepth = 12

	// arcPruneMarginPx / arcPruneChordFrac inflate the pruning box around a
	// span's {start, mid, end} projected points. Stepping in ECCENTRIC
	// anomaly bounds the tangent turn over a span at ΔE·(a/b), so the arc
	// bulges past the box by at most ~ΔE·(a/b)/8 of the chord — under 3 %
	// for e ≤ 0.995 at arcCoarseSpans, and the midpoint is already in the
	// box. A quarter-chord margin is an order of magnitude of headroom;
	// over-accepting only costs one extra flatness test.
	arcPruneMarginPx  = 2.0
	arcPruneChordFrac = 0.25
)

// projectPx is Project without the integer rounding or the on-canvas
// verdict: the raw pixel coordinate of a world point. Adaptive flattening
// works in floats so a span far off-canvas is measured and pruned before
// anything is rounded into an int.
func (c *Canvas) projectPx(w orbital.Vec3) (float64, float64) {
	rel := w.Sub(c.centerW)
	relX := rel.X*c.basis.X.X + rel.Y*c.basis.X.Y + rel.Z*c.basis.X.Z
	relY := rel.X*c.basis.Y.X + rel.Y*c.basis.Y.Y + rel.Z*c.basis.Y.Z
	return relX*c.scale + float64(c.pxW/2), float64(c.pxH/2) - relY*c.scale
}

// arcChord is one flattened piece of a curve: the world endpoints plus the
// projected pixel coordinates the flattener already computed for them.
type arcChord struct {
	a, b   orbital.Vec3
	ax, ay float64
	bx, by float64
}

// flattenEllipse walks an ellipse and hands `emit` a run of chords whose
// projected form is flat to within arcFlatTolerancePx, skipping spans that
// can't reach the canvas. Sampling steps in eccentric anomaly, not true
// anomaly: uniform ν crowds an eccentric orbit's samples at periapsis and
// starves apoapsis (where the tangent turns 1/(1−e) times faster), while
// uniform E spreads them by arc length within a factor of a/b. It is the
// same parameterisation orbital.SampleConicArc uses for the ADR 0023
// foreign-SOI redraw — this is that idea generalised to every curve.
//
// minSpans lets a caller keep a floor on the coarse pass (the historical
// fixed sample counts); the adaptive subdivision supplies everything above.
func (c *Canvas) flattenEllipse(el orbital.Elements, offset orbital.Vec3, minSpans int, emit func(arcChord)) {
	if !(el.A > 0) || math.IsNaN(el.A) || math.IsInf(el.A, 0) ||
		el.E < 0 || el.E >= 1 || math.IsNaN(el.E) {
		return
	}
	spans := arcCoarseSpans
	if minSpans > spans {
		spans = minSpans
	}
	if spans > arcMaxSpans {
		spans = arcMaxSpans
	}
	at := func(ea float64) orbital.Vec3 {
		return offset.Add(orbital.PositionAtTrueAnomaly(el, orbital.TrueAnomaly(ea, el.E)))
	}
	p0 := at(0)
	x0, y0 := c.projectPx(p0)
	for i := 0; i < spans; i++ {
		ea1 := 2 * math.Pi * float64(i+1) / float64(spans)
		p1 := at(ea1)
		x1, y1 := c.projectPx(p1)
		ea0 := 2 * math.Pi * float64(i) / float64(spans)
		c.flattenSpan(at, ea0, ea1, p0, x0, y0, p1, x1, y1, 0, emit)
		p0, x0, y0 = p1, x1, y1
	}
}

// flattenSpan is the recursive half of flattenEllipse: prune, test for
// flatness, else split at the midpoint. The midpoint is projected once and
// serves both the pruning box and the flatness test.
func (c *Canvas) flattenSpan(
	at func(float64) orbital.Vec3,
	u0, u1 float64,
	p0 orbital.Vec3, x0, y0 float64,
	p1 orbital.Vec3, x1, y1 float64,
	depth int,
	emit func(arcChord),
) {
	um := 0.5 * (u0 + u1)
	pm := at(um)
	xm, ym := c.projectPx(pm)
	if !c.spanReachesCanvas(x0, y0, x1, y1, xm, ym) {
		return
	}
	if depth >= arcMaxDepth || pointSegDistSq(xm, ym, x0, y0, x1, y1) <= arcFlatTolerancePx*arcFlatTolerancePx {
		emit(arcChord{a: p0, b: p1, ax: x0, ay: y0, bx: x1, by: y1})
		return
	}
	c.flattenSpan(at, u0, um, p0, x0, y0, pm, xm, ym, depth+1, emit)
	c.flattenSpan(at, um, u1, pm, xm, ym, p1, x1, y1, depth+1, emit)
}

// spanReachesCanvas reports whether the arc through the three projected
// points could touch the canvas. The box around the points is inflated by
// arcPruneMarginPx + arcPruneChordFrac × chord so a span that bulges toward
// the view isn't dropped; see the constants' doc comment for the bound.
func (c *Canvas) spanReachesCanvas(x0, y0, x1, y1, xm, ym float64) bool {
	minX, maxX := minMax3(x0, x1, xm)
	minY, maxY := minMax3(y0, y1, ym)
	margin := arcPruneMarginPx + arcPruneChordFrac*math.Hypot(x1-x0, y1-y0)
	return maxX+margin >= 0 && minX-margin <= float64(c.pxW-1) &&
		maxY+margin >= 0 && minY-margin <= float64(c.pxH-1)
}

func minMax3(a, b, m float64) (float64, float64) {
	lo, hi := a, a
	if b < lo {
		lo = b
	}
	if b > hi {
		hi = b
	}
	if m < lo {
		lo = m
	}
	if m > hi {
		hi = m
	}
	return lo, hi
}

// pointSegDistSq is the squared distance from (px, py) to the segment
// (ax, ay)–(bx, by) — the flatness measure (the arc's sagitta, sampled at
// its midpoint).
func pointSegDistSq(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	den := dx*dx + dy*dy
	if den == 0 {
		return (px-ax)*(px-ax) + (py-ay)*(py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / den
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	ex, ey := px-(ax+t*dx), py-(ay+t*dy)
	return ex*ex + ey*ey
}

// drawEllipseAdaptive is the shared body of the ellipse draw helpers.
// Chords on the far side of the basis plane through bodyPos ink at
// farSpacingPx (the same-hue depth stipple documented in
// internal/render/palette.go); near-side chords ink at nearSpacingPx.
// bodyPxR > 0 additionally cuts far-side pixels that land inside the
// body's projected disk, so the disk reads as opaque and the orbit visibly
// passes behind it. The cut is per-PIXEL rather than per-sample, so the
// occlusion gap tracks the disk edge exactly however long the chords get.
func (c *Canvas) drawEllipseAdaptive(
	el orbital.Elements,
	offset orbital.Vec3,
	minSpans int,
	nearSpacingPx, farSpacingPx int,
	bodyPos orbital.Vec3,
	bodyPxR int,
	color lipgloss.Color,
) {
	c.drawEllipseAdaptiveTagged(el, offset, minSpans, nearSpacingPx, farSpacingPx, bodyPos, bodyPxR, CellTag{Color: color})
}

// drawEllipseAdaptiveTagged is drawEllipseAdaptive carrying the full
// CellTag (colour + Inspect Owner, ADR 0041 §3) onto every pixel of the
// curve, so an orbit line is hit-testable back to the entity that owns it.
func (c *Canvas) drawEllipseAdaptiveTagged(
	el orbital.Elements,
	offset orbital.Vec3,
	minSpans int,
	nearSpacingPx, farSpacingPx int,
	bodyPos orbital.Vec3,
	bodyPxR int,
	tag CellTag,
) {
	c.drawEllipseAdaptiveTaggedRecording(el, offset, minSpans, nearSpacingPx, farSpacingPx, bodyPos, bodyPxR, tag, nil)
}

// drawEllipseAdaptiveTaggedRecording is drawEllipseAdaptiveTagged with an
// optional pixel recorder (#367): every pixel actually plotted is also
// handed to record (nil ⇒ identical to drawEllipseAdaptiveTagged). The
// curve-geometry cache (canvas_curve_cache.go) calls this directly on a
// cache miss to capture the flattened+walked output for replay, so
// caching costs nothing beyond the draw that already had to happen.
func (c *Canvas) drawEllipseAdaptiveTaggedRecording(
	el orbital.Elements,
	offset orbital.Vec3,
	minSpans int,
	nearSpacingPx, farSpacingPx int,
	bodyPos orbital.Vec3,
	bodyPxR int,
	tag CellTag,
	record func(px, py int),
) {
	if nearSpacingPx < 1 {
		nearSpacingPx = 1
	}
	if farSpacingPx < 1 {
		farSpacingPx = 1
	}
	depthAxis := c.basis.DepthAxis()
	bodyX, bodyY := c.projectPx(bodyPos)
	// "Behind" needs a floor: an orbit lying IN the basis plane (any
	// coplanar view — an equatorial orbit under the perifocal basis, the
	// commonest case there is) has depth ≈ 0, and floating-point noise then
	// flips chords between the near and far cadence at random, thinning the
	// curve to roughly the average of the two. Anything less than half a
	// pixel behind the plane is not behind it.
	behindThreshold := 0.0
	if c.scale > 0 {
		behindThreshold = -0.5 / c.scale
	}
	// One dash phase for the whole curve: the cadence has to survive chord
	// boundaries or a finely flattened curve inks solid (every chord would
	// restart at phase 0).
	nearPhase, farPhase := 0.0, 0.0
	c.flattenEllipse(el, offset, minSpans, func(ch arcChord) {
		mid := ch.a.Add(ch.b).Scale(0.5).Sub(bodyPos)
		depth := mid.X*depthAxis.X + mid.Y*depthAxis.Y + mid.Z*depthAxis.Z
		if depth < behindThreshold {
			farPhase = c.walkPixelSegment(ch.ax, ch.ay, ch.bx, ch.by, tag, farSpacingPx, farPhase,
				bodyX, bodyY, float64(bodyPxR), record)
			return
		}
		nearPhase = c.walkPixelSegment(ch.ax, ch.ay, ch.bx, ch.by, tag, nearSpacingPx, nearPhase, 0, 0, 0, record)
	})
}

// visibleAngleWindow returns the angular window (radians, measured from the
// +x pixel axis, y down) outside which no point of the circle of radius
// pxRadius centred at (cx, cy) can land on the canvas. ok=false means the
// circle misses the canvas entirely.
//
// This is what keeps a screen-space ring honest at extreme zoom: sampling
// the whole circle at a fixed dot spacing costs O(radius), so the shipped
// primitives capped the sample count instead and the visible arc went
// sparse — the ring dissolved exactly when zoomed in. Restricting the sweep
// to the wedge the canvas subtends makes the cost O(visible arc) instead,
// and the dots stay at their commanded spacing at any radius.
func visibleAngleWindow(cx, cy float64, pxRadius float64, w, h int) (start, span float64, ok bool) {
	maxX, maxY := float64(w-1), float64(h-1)
	if cx >= 0 && cx <= maxX && cy >= 0 && cy <= maxY {
		// Centre inside the canvas: every direction can matter, but a
		// radius past the far corner puts the whole circle outside.
		far := math.Max(math.Hypot(cx, cy), math.Hypot(maxX-cx, maxY-cy))
		far = math.Max(far, math.Max(math.Hypot(maxX-cx, cy), math.Hypot(cx, maxY-cy)))
		if pxRadius > far {
			return 0, 0, false
		}
		return 0, 2 * math.Pi, true
	}
	// Centre outside: the canvas rectangle subtends less than π, so the
	// corner angles unwrapped around the rectangle's centre bound it.
	ref := math.Atan2((maxY/2)-cy, (maxX/2)-cx)
	lo, hi := 0.0, 0.0
	corners := [4][2]float64{{0, 0}, {maxX, 0}, {0, maxY}, {maxX, maxY}}
	farthest := 0.0
	for i, p := range corners {
		d := math.Hypot(p[0]-cx, p[1]-cy)
		if d > farthest {
			farthest = d
		}
		a := wrapToRef(math.Atan2(p[1]-cy, p[0]-cx), ref)
		if i == 0 || a < lo {
			lo = a
		}
		if i == 0 || a > hi {
			hi = a
		}
	}
	// Nearest point of the rectangle to the centre (clamped projection).
	nx := math.Min(math.Max(cx, 0), maxX)
	ny := math.Min(math.Max(cy, 0), maxY)
	near := math.Hypot(nx-cx, ny-cy)
	if pxRadius < near || pxRadius > farthest {
		return 0, 0, false
	}
	return lo, hi - lo, true
}

// wrapToRef shifts angle a by whole turns until it lies within π of ref.
func wrapToRef(a, ref float64) float64 {
	for a-ref > math.Pi {
		a -= 2 * math.Pi
	}
	for ref-a > math.Pi {
		a += 2 * math.Pi
	}
	return a
}
