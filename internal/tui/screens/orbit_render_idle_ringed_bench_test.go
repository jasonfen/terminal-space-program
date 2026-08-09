package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// BenchmarkOrbitViewRenderIdleRinged is BenchmarkOrbitViewRenderIdle's #369
// sibling: same idle-frame shape (world clock frozen, warm once, render
// b.N times), but focused on Saturn instead of the default LEO seed craft.
//
// The default benchmark's scene (a craft in LEO around Earth) never brings
// a ringed body into view, so it can't see RingTiltedOutlineTagged's cost
// at all — Saturn is the ONLY body BodyRingBands recognizes today. This is
// the scene the #367 issue's prod profile actually needed (a save with a
// ringed body on screen) to justify the #369 ring-geometry cache
// (canvas_ring_cache.go): see the PR body for the measured before/after.
func BenchmarkOrbitViewRenderIdleRinged(b *testing.B) {
	v := NewOrbitView(plainTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		b.Fatalf("NewWorld: %v", err)
	}
	satIdx := -1
	for i, bd := range w.System().Bodies {
		if bd.ID == "saturn" {
			satIdx = i
		}
	}
	if satIdx < 0 {
		b.Fatal("saturn not found in the default system — benchmark scenario is broken")
	}
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: satIdx}
	v.Render(w, 0, 120, 40) // warm the Framing Event / zoom fit
	v.Render(w, 0, 120, 40) // warm the #369 ring cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Render(w, 0, 120, 40)
	}
}
