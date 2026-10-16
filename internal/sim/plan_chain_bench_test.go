package sim

import (
	"testing"
	"time"
)

// BenchmarkPlannedMoonTransferReadouts is one HUD frame's worth of the
// encounter sources (clock advanced each iteration, so every frame is cold) on a planted Moon transfer: NAVIGATION's
// PredictedFinalOrbit, the FRAME TRANSITION box and the CAPTURE PREVIEW box.
func BenchmarkPlannedMoonTransferReadouts(b *testing.B) {
	w, _ := plannedMoonTransfer(b, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A new frame: the clock has ticked, so no memo may be reused.
		w.Clock.SimTime = w.Clock.SimTime.Add(time.Millisecond)
		w.PredictedFinalOrbit()
		w.NextFrameTransition()
		w.ArrivalCapturePreview()
	}
}
