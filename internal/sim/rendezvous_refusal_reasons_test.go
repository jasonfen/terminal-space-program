package sim

import (
	"errors"
	"testing"
)

// TestRendezvousReasonToErr_S1DistinctReasons — #278: before this mapping
// existed, PlanRendezvousNudge collapsed every non-"docked" planner Reason
// into the single ErrRendezvousNoImprovement ("no useful nudge in range"),
// so a genuinely actionable "burn too large" refusal read identically to
// "no improvement available" (geometry is already optimal) and to a
// mismatched-shape refusal. This slice (S1) gives each of those a
// DISTINCT sentinel with its own remedy text. The four inner Lambert-
// failure tags ("no lambert convergence", "degenerate axes", "horizon too
// short", "ca-verify failed") are deliberately NOT split out yet — they
// still fall back to ErrRendezvousNoImprovement here; S2 gives them their
// own shared phasing-coach wording.
func TestRendezvousReasonToErr_S1DistinctReasons(t *testing.T) {
	cases := []struct {
		reason string
		want   error
	}{
		{"docked", ErrRendezvousAlreadyDocked},
		{"orbit shape mismatch", ErrRendezvousShapeMismatch},
		{"burn too large — use H/I/m", ErrRendezvousBurnTooLarge},
		{"burn drops periapsis unsafely", ErrRendezvousUnsafePeriapsis},
		{"no improvement available", ErrRendezvousNoImprovement},
	}
	for _, c := range cases {
		got := rendezvousReasonToErr(c.reason)
		if !errors.Is(got, c.want) {
			t.Errorf("reason %q → %v, want %v", c.reason, got, c.want)
		}
	}

	// The four distinct reasons must not all collapse onto the SAME
	// error as each other (that was the #278 bug) — check pairwise
	// inequality among the ones this slice separates.
	distinct := []error{ErrRendezvousShapeMismatch, ErrRendezvousBurnTooLarge, ErrRendezvousUnsafePeriapsis, ErrRendezvousNoImprovement}
	for i := range distinct {
		for j := range distinct {
			if i == j {
				continue
			}
			if errors.Is(distinct[i], distinct[j]) {
				t.Errorf("distinct reasons %d and %d map to the same error", i, j)
			}
		}
	}
}
