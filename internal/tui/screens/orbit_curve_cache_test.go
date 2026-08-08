package screens

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestOrbitViewCurveCacheHitsAtIdleAndBustsOnCameraChange is the #367
// end-to-end regression test the issue calls for: drive the real
// OrbitView.Render path (not a synthetic Canvas scenario) through an idle
// repeat, then a pan, a zoom, and a focus switch, and assert the curve
// cache behaves — hits when nothing that feeds it changed, misses when the
// camera moves. This exercises the actual screens/orbit.go wiring (the
// four DrawEllipseClassCachedTagged call sites and the curveID each
// passes), which the widgets-package unit tests (canvas_curve_cache_test.go)
// can't reach on their own.
func TestOrbitViewCurveCacheHitsAtIdleAndBustsOnCameraChange(t *testing.T) {
	v := NewOrbitView(plainTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	v.Render(w, 0, 120, 40) // warm the Framing Event / zoom fit
	_, computesAfterWarm := v.canvas.CurveCacheStats()
	if computesAfterWarm == 0 {
		t.Fatal("precondition: warm-up render recorded zero curve-cache computes — nothing drew an ellipse, test scenario is broken")
	}

	// Idle repeat: identical world, identical camera — every curve drawn
	// on the warm-up should still be in the cache.
	v.Render(w, 0, 120, 40)
	hits, computes := v.canvas.CurveCacheStats()
	if computes != computesAfterWarm {
		t.Errorf("idle repeat re-flattened %d curve(s) that should have hit (computes %d -> %d)", computes-computesAfterWarm, computesAfterWarm, computes)
	}
	if hits == 0 {
		t.Error("idle repeat recorded zero curve-cache hits")
	}

	// Pan: the camera center moves, nothing else does. Every on-canvas
	// curve's key must change (relOffset/relBody are camera-relative).
	v.PanRight()
	v.Render(w, 0, 120, 40)
	_, computesAfterPan := v.canvas.CurveCacheStats()
	if computesAfterPan == computes {
		t.Error("PanRight produced zero curve-cache misses — the cache didn't notice the camera moved")
	}

	// Idle again after the pan settles: should go back to all-hits.
	v.Render(w, 0, 120, 40)
	hitsBeforeSteady, _ := v.canvas.CurveCacheStats()
	v.Render(w, 0, 120, 40)
	hitsAfterSteady, computesAfterSteady := v.canvas.CurveCacheStats()
	if computesAfterSteady != 0 && computesAfterSteady != computesAfterPan {
		// (computesAfterPan carries forward; a further miss here would be
		// a bug — the panned camera is now steady.)
		t.Errorf("post-pan steady state kept missing (computes %d -> %d)", computesAfterPan, computesAfterSteady)
	}
	if hitsAfterSteady <= hitsBeforeSteady {
		t.Error("post-pan steady state produced no hits — cache never recovered after the pan")
	}

	// Zoom busts too.
	computesBeforeZoom := computesAfterSteady
	v.ZoomIn()
	v.Render(w, 0, 120, 40)
	_, computesAfterZoom := v.canvas.CurveCacheStats()
	if computesAfterZoom == computesBeforeZoom {
		t.Error("ZoomIn produced zero curve-cache misses")
	}

	// Focus switch (body -> craft), a Framing Event: re-fits scale and
	// re-centers, both of which must bust every curve's key.
	if w.ActiveCraft() == nil {
		t.Skip("no active craft in default world — can't exercise the focus-switch leg")
	}
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: 0}
	v.Render(w, 0, 120, 40)
	_, computesBeforeSwitch := v.canvas.CurveCacheStats()

	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v.Render(w, 0, 120, 40)
	_, computesAfterSwitch := v.canvas.CurveCacheStats()
	if computesAfterSwitch == computesBeforeSwitch {
		t.Error("focus switch (body -> craft) produced zero curve-cache misses")
	}
}

// TestOrbitViewCurveCacheHitMatchesUncachedAfterPan is the same
// cache-vs-ground-truth check orbit_disk_cache_pan_test.go runs for the
// disk raster cache (TestOrbitRenderDiskCacheHitMatchesUncachedAfterPan),
// applied to orbit-line geometry: a render that MAY be using warm curve-
// cache entries after a pan must match the same frame rendered with the
// cache forced empty. Forces ANSI color for the same sync.Once test-
// ordering reason documented there.
//
// Review finding: an earlier version of this test replayed the identical
// warm -> pan -> pan -> render sequence on BOTH v and the "reference"
// freshV, so freshV's cache ended up exactly as warm as v's — the
// comparison proved the render is deterministic run-to-run, not that it's
// correct independent of what the cache did. The fix is
// freshV.canvas.ResetCurveCache() immediately before the final render:
// freshV still replays the same warm-up/pan sequence (so its camera
// framing matches v's — reordering the pans ahead of the warm-up render
// does NOT work, per ResetCurveCache's doc comment), but its cache is
// forced empty for the one render actually under comparison.
func TestOrbitViewCurveCacheHitMatchesUncachedAfterPan(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	newWorld := func(t *testing.T) *sim.World {
		t.Helper()
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatalf("NewWorld: %v", err)
		}
		return w
	}

	// One shared OrbitView, panned twice, so the second render is a
	// genuine cache MISS on an already-warm cache (not a cold first draw).
	v := NewOrbitView(plainTheme())
	w := newWorld(t)
	v.Render(w, 0, 120, 40)
	v.PanRight()
	v.Render(w, 0, 120, 40)
	v.PanDown()
	got := v.Render(w, 0, 120, 40)

	// Reference: replay the identical pan sequence on a fresh OrbitView +
	// World from scratch (cache never warm, so this is definitionally
	// correct — nothing to hit yet).
	freshV := NewOrbitView(plainTheme())
	freshW := newWorld(t)
	freshV.Render(freshW, 0, 120, 40)
	freshV.PanRight()
	freshV.Render(freshW, 0, 120, 40)
	freshV.PanDown()
	// Force the reference's LAST render to be genuinely cache-free —
	// otherwise freshV's cache is exactly as warm as v's at this point
	// (both replayed the identical sequence) and this stops being a
	// cached-vs-ground-truth check at all (review finding).
	freshV.canvas.ResetCurveCache()
	want := freshV.Render(freshW, 0, 120, 40)

	if got != want {
		t.Errorf("panned render diverges from a fresh reference\ngot:  %q\nwant: %q", got, want)
	}
	if !containsANSI(want) {
		t.Fatal("test setup broken: expected lipgloss.SetColorProfile(termenv.TrueColor) to produce ANSI-colored output")
	}
}

// TestOrbitViewCurveCacheHitBoundedDeviationDuringLiveTicking is the #367
// review's bounded-deviation regression test. A curve-cache HIT is
// sub-pixel-equivalent to a fresh draw, not byte-identical (see
// DrawEllipseClassCachedTagged's doc comment): the key quantizes its
// inputs to stay within arcFlatTolerancePx of a fresh recompute, so during
// LIVE ticking (physics actually advancing every frame, unlike the idle
// benchmark's frozen clock) a hit can legitimately reuse geometry from up
// to one quantum step earlier. That's bounded and non-accumulating —
// review-measured live at 1x warp: 140/200 frames differ, max 9 of 4800
// cells — but was previously undocumented and unguarded by any test. This
// ticks a live world for 200 real steps, rendering the SAME world state
// through a persistently-cached OrbitView and a reference OrbitView whose
// curve cache is forced empty before every render, and asserts the
// per-frame cell-diff count stays under a small cap: a future loosening of
// the quantization tolerance (or an outright key bug re-widening the
// staleness this cache exists to bound) shows up as a growing diff count
// instead of silently passing.
func TestOrbitViewCurveCacheHitBoundedDeviationDuringLiveTicking(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	cachedV := NewOrbitView(plainTheme())
	freshV := NewOrbitView(plainTheme())
	cachedV.Render(w, 0, 120, 40) // warm both once, identically, before ticking
	freshV.Render(w, 0, 120, 40)
	freshV.canvas.ResetCurveCache()

	const ticks = 200
	// Review-measured worst case at 1x warp was 9 of 4800 cells in any one
	// frame; capped with headroom so this test only fires on a real
	// regression (a wider quantum, or a key that stops covering something
	// it should), not on ordinary sub-pixel noise.
	const maxDiffCellsPerFrame = 40

	worst, framesDiffering := 0, 0
	for i := 0; i < ticks; i++ {
		w.Tick() // real physics step at whatever warp NewWorld defaults to
		cachedFrame := cachedV.Render(w, 0, 120, 40)
		freshFrame := freshV.Render(w, 0, 120, 40)
		freshV.canvas.ResetCurveCache() // next tick's freshFrame is cache-free too

		diff := diffCellCount(cachedFrame, freshFrame)
		if diff > 0 {
			framesDiffering++
		}
		if diff > worst {
			worst = diff
		}
		if diff > maxDiffCellsPerFrame {
			t.Errorf("tick %d: cached render differs from a cache-free reference in %d cells, want <= %d", i, diff, maxDiffCellsPerFrame)
		}
	}
	t.Logf("%d/%d frames differed from the cache-free reference; worst-case per-frame diff %d cells (cap %d)", framesDiffering, ticks, worst, maxDiffCellsPerFrame)
}

// diffCellCount counts the styled-cell positions where a and b differ.
// splitStyledCells (navball_panel.go) turns an ANSI-styled render string
// into one entry per rendered glyph (its active style plus its rune), so
// this counts glyphs that actually render differently — not raw bytes,
// which would overcount on an ANSI escape sequence's length varying
// between two colors with no visible difference in cell count.
func diffCellCount(a, b string) int {
	ca, cb := splitStyledCells(a), splitStyledCells(b)
	n := len(ca)
	if len(cb) < n {
		n = len(cb)
	}
	diff := 0
	for i := 0; i < n; i++ {
		if ca[i] != cb[i] {
			diff++
		}
	}
	if len(ca) > n {
		diff += len(ca) - n
	}
	if len(cb) > n {
		diff += len(cb) - n
	}
	return diff
}
