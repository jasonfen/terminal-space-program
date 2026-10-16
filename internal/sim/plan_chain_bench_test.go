package sim

import "testing"

// BenchmarkPlannedMoonTransferReadouts is one HUD frame's worth of the
// encounter sources on a planted Moon transfer: NAVIGATION's
// PredictedFinalOrbit, the FRAME TRANSITION box and the CAPTURE PREVIEW box.
func BenchmarkPlannedMoonTransferReadouts(b *testing.B) {
	w, _ := plannedMoonTransfer(b, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.PredictedFinalOrbit()
		w.NextFrameTransition()
		w.ArrivalCapturePreview()
	}
}
