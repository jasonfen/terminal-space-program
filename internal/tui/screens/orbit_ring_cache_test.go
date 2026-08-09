package screens

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// saturnFocusedWorld returns a World focused on Saturn, zoomed so its ring
// bands actually fill the view — the only scenario that exercises the
// #369 ring-geometry cache end to end (the default NewWorld() scene never
// brings a ringed body into frame; see
// BenchmarkOrbitViewRenderIdleRinged's doc comment).
func saturnFocusedWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	satIdx := -1
	for i, b := range w.System().Bodies {
		if b.ID == "saturn" {
			satIdx = i
		}
	}
	if satIdx < 0 {
		t.Fatal("saturn not found in the default system — test scenario is broken")
	}
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: satIdx}
	return w
}

// TestOrbitViewRingCacheHitsAtIdleAndBustsOnCameraChange is the #369
// end-to-end regression test mirroring
// TestOrbitViewCurveCacheHitsAtIdleAndBustsOnCameraChange: drive the real
// OrbitView.Render path (not a synthetic Canvas scenario), Saturn-focused
// so its ring bands are actually on screen, through an idle repeat then a
// pan and a zoom, and assert the ring cache hits when nothing that feeds
// it changed and misses when the camera moves.
func TestOrbitViewRingCacheHitsAtIdleAndBustsOnCameraChange(t *testing.T) {
	v := NewOrbitView(plainTheme())
	w := saturnFocusedWorld(t)

	v.Render(w, 0, 120, 40) // warm the Framing Event / zoom fit
	_, computesAfterWarm := v.canvas.RingCacheStats()
	if computesAfterWarm == 0 {
		t.Fatal("precondition: warm-up render recorded zero ring-cache computes — Saturn's rings never drew, test scenario is broken")
	}

	// Idle repeat: identical world, identical camera — every ring outline
	// drawn on the warm-up should still be in the cache.
	v.Render(w, 0, 120, 40)
	hits, computes := v.canvas.RingCacheStats()
	if computes != computesAfterWarm {
		t.Errorf("idle repeat recomputed %d ring outline(s) that should have hit (computes %d -> %d)", computes-computesAfterWarm, computesAfterWarm, computes)
	}
	if hits == 0 {
		t.Error("idle repeat recorded zero ring-cache hits")
	}

	// Pan busts.
	v.PanRight()
	v.Render(w, 0, 120, 40)
	_, computesAfterPan := v.canvas.RingCacheStats()
	if computesAfterPan == computes {
		t.Error("PanRight produced zero ring-cache misses — the cache didn't notice the camera moved")
	}

	// Idle again after the pan settles: should go back to all-hits.
	v.Render(w, 0, 120, 40)
	hitsBeforeSteady, _ := v.canvas.RingCacheStats()
	v.Render(w, 0, 120, 40)
	hitsAfterSteady, computesAfterSteady := v.canvas.RingCacheStats()
	if computesAfterSteady != computesAfterPan {
		t.Errorf("post-pan steady state kept missing (computes %d -> %d)", computesAfterPan, computesAfterSteady)
	}
	if hitsAfterSteady <= hitsBeforeSteady {
		t.Error("post-pan steady state produced no hits — cache never recovered after the pan")
	}

	// Zoom busts too.
	computesBeforeZoom := computesAfterSteady
	v.ZoomIn()
	v.Render(w, 0, 120, 40)
	_, computesAfterZoom := v.canvas.RingCacheStats()
	if computesAfterZoom == computesBeforeZoom {
		t.Error("ZoomIn produced zero ring-cache misses")
	}
}

// TestOrbitViewRingCacheHitMatchesUncachedAfterPan is the ring-cache
// analog of TestOrbitViewCurveCacheHitMatchesUncachedAfterPan: a render
// that MAY be using warm ring-cache entries after a pan must match the
// same frame rendered with the cache forced empty.
//
// Same review-driven shape as the curve-cache version: freshV replays the
// identical warm -> pan -> pan sequence so its camera framing matches v's,
// then ResetRingCache() forces ONLY the final render under comparison to
// be genuinely cache-free — resetting earlier would let the two renders'
// Framing Events diverge (ADR 0021 only re-fits on Focus/ViewMode/System
// changes or a resize).
func TestOrbitViewRingCacheHitMatchesUncachedAfterPan(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	v := NewOrbitView(plainTheme())
	w := saturnFocusedWorld(t)
	v.Render(w, 0, 120, 40)
	v.PanRight()
	v.Render(w, 0, 120, 40)
	v.PanDown()
	got := v.Render(w, 0, 120, 40)

	freshV := NewOrbitView(plainTheme())
	freshW := saturnFocusedWorld(t)
	freshV.Render(freshW, 0, 120, 40)
	freshV.PanRight()
	freshV.Render(freshW, 0, 120, 40)
	freshV.PanDown()
	freshV.canvas.ResetRingCache()
	want := freshV.Render(freshW, 0, 120, 40)

	if got != want {
		t.Errorf("panned render diverges from a fresh reference\ngot:  %q\nwant: %q", got, want)
	}
	if !containsANSI(want) {
		t.Fatal("test setup broken: expected lipgloss.SetColorProfile(termenv.TrueColor) to produce ANSI-colored output")
	}
}
