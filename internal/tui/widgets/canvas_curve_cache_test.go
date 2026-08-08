package widgets

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// forceANSIColor makes lipgloss emit real ANSI color in a test binary (no
// TTY) instead of stripping it. Uses t.Setenv("CLICOLOR_FORCE", "1"), the
// convention every other color-sensitive test in THIS package already
// uses (canvas_test.go, canvas_string_coalesce_test.go,
// lighting_pipeline_test.go) — deliberately NOT the
// lipgloss.SetColorProfile approach screens/orbit_disk_cache_pan_test.go
// uses: SetColorProfile explicitly overrides lipgloss's lazy env-based
// detection for the rest of the process, which is exactly what that
// screens-package test needs (its own sync.Once ordering problem is
// documented there) but would instead BREAK every t.Setenv-based test
// in this package that runs afterward, since the sync.Once would already
// be pinned by the time they set CLICOLOR_FORCE (verified: swapping this
// in caused TestColoredDiskEmitsAnsiOnRender and three sibling tests to
// fail). Same env var, same package convention, no cross-file footgun.
func forceANSIColor(t *testing.T) {
	t.Helper()
	t.Setenv("CLICOLOR_FORCE", "1")
}

// testEllipse is a modest inclined, eccentric orbit sized to fill a chunk
// of a small test canvas — big enough that flattening actually subdivides,
// small enough the test stays fast.
func testEllipse() orbital.Elements {
	return orbital.Elements{A: 30, E: 0.4, I: 0.3, Omega: 0.5, Arg: 0.8}
}

func newCurveCacheTestCanvas() *Canvas {
	c := NewCanvas(20, 10) // 40x40 px
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	return c
}

// TestDrawEllipseClassCachedTaggedMatchesUncached confirms a cache MISS
// (first draw) produces byte-identical canvas output to the uncached
// DrawEllipseClassTagged — the cache must never change what gets drawn,
// only whether it's recomputed.
func TestDrawEllipseClassCachedTaggedMatchesUncached(t *testing.T) {
	forceANSIColor(t)
	el := testEllipse()
	tag := CellTag{Color: "#00FF00", Owner: "test"}

	cached := newCurveCacheTestCanvas()
	cached.DrawEllipseClassCachedTagged("curve-1", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, tag)

	uncached := newCurveCacheTestCanvas()
	uncached.DrawEllipseClassTagged(el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, tag)

	if got, want := cached.String(), uncached.String(); got != want {
		t.Errorf("cached miss diverges from uncached draw\ngot:  %q\nwant: %q", got, want)
	}
}

// TestDrawEllipseClassCachedTaggedHitsOnRepeat confirms an identical
// second call is a cache hit (no recompute) and still renders the same
// picture.
func TestDrawEllipseClassCachedTaggedHitsOnRepeat(t *testing.T) {
	forceANSIColor(t)
	el := testEllipse()
	tag := CellTag{Color: "#00FF00", Owner: "test"}

	c := newCurveCacheTestCanvas()
	c.DrawEllipseClassCachedTagged("curve-1", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, tag)
	first := c.String()
	computesBefore := c.curveCacheComputes

	c.Clear()
	c.DrawEllipseClassCachedTagged("curve-1", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, tag)
	second := c.String()

	if c.curveCacheComputes != computesBefore {
		t.Fatalf("expected the identical second call to be a cache HIT, got a recompute (computes %d -> %d)", computesBefore, c.curveCacheComputes)
	}
	if c.curveCacheHits == 0 {
		t.Error("expected curveCacheHits to be nonzero after an identical repeat call")
	}
	if first != second {
		t.Errorf("cache-hit render diverges from the original draw\nfirst:  %q\nsecond: %q", first, second)
	}
}

// TestDrawEllipseClassCachedTaggedColorOnlyChangeStillHits confirms the
// cache is keyed on GEOMETRY, not color: changing only tag.Color between
// two otherwise-identical calls still hits (reuses the flattened pixel
// list) but the rendered color reflects the new tag, not the stale one.
func TestDrawEllipseClassCachedTaggedColorOnlyChangeStillHits(t *testing.T) {
	forceANSIColor(t)
	el := testEllipse()

	c := newCurveCacheTestCanvas()
	c.DrawEllipseClassCachedTagged("curve-1", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})
	computesBefore := c.curveCacheComputes

	c.Clear()
	c.DrawEllipseClassCachedTagged("curve-1", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#FF0000", Owner: "test"})
	recolored := c.String()

	if c.curveCacheComputes != computesBefore {
		t.Fatalf("a color-only change should reuse cached geometry (HIT), got a recompute (computes %d -> %d)", computesBefore, c.curveCacheComputes)
	}

	fresh := newCurveCacheTestCanvas()
	fresh.DrawEllipseClassTagged(el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#FF0000", Owner: "test"})
	want := fresh.String()

	if recolored != want {
		t.Errorf("recolored cache-hit render diverges from an uncached reference in the new color\ngot:  %q\nwant: %q", recolored, want)
	}
}

// TestDrawEllipseClassCachedTaggedEmptyCurveIDNeverCaches confirms an
// empty curveID falls through to the plain uncached path and never
// touches curveCache — an empty-string map key shared by unrelated
// anonymous callers would silently serve one curve's geometry onto
// another's.
func TestDrawEllipseClassCachedTaggedEmptyCurveIDNeverCaches(t *testing.T) {
	c := newCurveCacheTestCanvas()
	el := testEllipse()
	c.DrawEllipseClassCachedTagged("", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00"})
	if len(c.curveCache) != 0 {
		t.Errorf("empty curveID populated curveCache (%d entries), want 0", len(c.curveCache))
	}
}

// curveScenario draws the test curve on a canvas configured by setup, and
// returns both the rendered string and the canvas for further cache
// introspection.
func curveScenario(t *testing.T, curveID string, el orbital.Elements, setup func(c *Canvas)) (*Canvas, string) {
	t.Helper()
	c := newCurveCacheTestCanvas()
	if setup != nil {
		setup(c)
	}
	c.DrawEllipseClassCachedTagged(curveID, el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})
	return c, c.String()
}

// TestDrawEllipseClassCachedTaggedBustsOnViewChange is the #367 stale-
// frame regression guard directly called for by the issue: pan, zoom,
// rebasis, and resize must all bust the geometry cache — #363's review
// finding was that a render cache keyed on the subject alone, without
// what the camera is doing, serves stale pixels the instant the camera
// moves. Each case asserts BOTH a recompute happened AND the new frame
// matches a from-scratch (never-cached) reference — a cache that merely
// detects a miss but still blits the OLD geometry would pass a
// hit/miss-counter-only test while still being visibly broken.
func TestDrawEllipseClassCachedTaggedBustsOnViewChange(t *testing.T) {
	forceANSIColor(t)
	el := testEllipse()

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
			// Warm the cache with the baseline view.
			c := newCurveCacheTestCanvas()
			c.DrawEllipseClassCachedTagged("curve-1", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})
			computesBefore := c.curveCacheComputes

			// Apply the view change and redraw the SAME curve.
			c.Clear()
			tc.setup(c)
			c.DrawEllipseClassCachedTagged("curve-1", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})
			got := c.String()

			if c.curveCacheComputes == computesBefore {
				t.Fatalf("%s: expected a cache MISS (recompute) after the view changed, got a HIT", tc.name)
			}

			// Reference: the same view, rendered on a fresh canvas that
			// never cached anything.
			fresh := newCurveCacheTestCanvas()
			tc.setup(fresh)
			fresh.DrawEllipseClassTagged(el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})
			want := fresh.String()

			if got != want {
				t.Errorf("%s: post-change cache-miss render diverges from an uncached reference\ngot:  %q\nwant: %q", tc.name, got, want)
			}
		})
	}
}

// TestDrawEllipseClassCachedTaggedBustsOnElementChange confirms a real
// (well past quantization) orbital-element change busts the cache too —
// the geometry side of what predictRenderKey already protects on the
// physics side.
func TestDrawEllipseClassCachedTaggedBustsOnElementChange(t *testing.T) {
	forceANSIColor(t)
	el1 := testEllipse()
	el2 := el1
	el2.A *= 1.5 // well beyond any sub-pixel quantum at this canvas's scale

	c := newCurveCacheTestCanvas()
	c.DrawEllipseClassCachedTagged("curve-1", el1, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})
	computesBefore := c.curveCacheComputes

	c.Clear()
	c.DrawEllipseClassCachedTagged("curve-1", el2, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})
	got := c.String()

	if c.curveCacheComputes == computesBefore {
		t.Fatal("expected a cache MISS after el.A changed by 50%, got a HIT")
	}

	fresh := newCurveCacheTestCanvas()
	fresh.DrawEllipseClassTagged(el2, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})
	if want := fresh.String(); got != want {
		t.Errorf("post-element-change render diverges from an uncached reference\ngot:  %q\nwant: %q", got, want)
	}
}

// TestDrawEllipseClassCachedTaggedToleratesSubPixelJitter confirms the
// OTHER half of the quantization contract: a coasting vessel's live
// elements drift by floating-point noise every physics tick even absent
// thrust (ElementsFromState recomputed from an integrator's R/V each
// frame) — far too small to move any pixel — and that must still be a
// cache HIT, or #367's whole premise (idle orbits are as static as the
// disk was) buys nothing.
func TestDrawEllipseClassCachedTaggedToleratesSubPixelJitter(t *testing.T) {
	el1 := testEllipse()
	el2 := el1
	el2.A += 1e-6 // far below curvePositionQuantum at this canvas's scale

	c := newCurveCacheTestCanvas()
	c.DrawEllipseClassCachedTagged("curve-1", el1, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})
	computesBefore := c.curveCacheComputes

	c.Clear()
	c.DrawEllipseClassCachedTagged("curve-1", el2, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: "test"})

	if c.curveCacheComputes != computesBefore {
		t.Errorf("sub-pixel element jitter (ΔA=1e-6) busted the cache (computes %d -> %d) — quantum is too tight", computesBefore, c.curveCacheComputes)
	}
}

// TestDrawEllipseClassCachedTaggedBoundedAcrossManyCurveIDs sweeps far
// more distinct curveIDs than curveCacheCap through the same canvas — a
// long-running --serve process with churning MP ghosts, worst case — and
// asserts the cache never exceeds its bound (same LRU discipline #363's
// review added to diskOffsetCache, applied here so per-vessel geometry
// entries can't accumulate as a slow per-process leak).
func TestDrawEllipseClassCachedTaggedBoundedAcrossManyCurveIDs(t *testing.T) {
	c := newCurveCacheTestCanvas()
	el := testEllipse()
	for i := 0; i < curveCacheCap*3; i++ {
		id := "curve-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		c.DrawEllipseClassCachedTagged(id, el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, CellTag{Color: "#00FF00", Owner: id})
		if len(c.curveCache) > curveCacheCap {
			t.Fatalf("after %d curveIDs: curveCache holds %d entries, want <= %d (cap)", i+1, len(c.curveCache), curveCacheCap)
		}
	}
	if got := len(c.curveCache); got != curveCacheCap {
		t.Errorf("after sweeping %d curveIDs, curveCache = %d entries, want exactly the cap (%d)", curveCacheCap*3, got, curveCacheCap)
	}
}
