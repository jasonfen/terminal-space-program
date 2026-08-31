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
// mismatched-shape refusal. rendezvousReasonToErr gives each of those a
// DISTINCT sentinel with its own remedy text. The four inner Lambert-
// failure tags ("no lambert convergence", "degenerate axes", "horizon too
// short", "ca-verify failed") are covered separately, by
// TestRendezvousReasonToErr_S2PhasingCoachBucket below — they share the
// ADR 0039 §2 phasing-coach wording (ErrRendezvousNoEncounter), not this
// slice's ErrRendezvousNoImprovement.
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

// TestRendezvousReasonToErr_S2PhasingCoachBucket — ADR 0039 S2 / #277:
// the four inner Lambert-lookahead dead ends share ONE remedy — none of
// them found a real encounter to score, so K has nothing more specific
// to say than "make a phasing burn" (rendezvousPhasingCoachMsg). PR #392
// review finding 1 later gave Engage's own refusal its wording back
// ("plant a rendezvous nudge [K] first", #276) now that a plant-then-
// Engage sequence actually arms once K finds something — so the two
// refusal chains no longer share identical text, but they still don't
// dead-end into each other: Engage points at K, and if K itself also
// finds nothing (a true matched-orbit stalemate) K names this same
// phasing-coach remedy instead of repeating Engage's line back
// unchanged.
func TestRendezvousReasonToErr_S2PhasingCoachBucket(t *testing.T) {
	for _, reason := range []string{
		"no lambert convergence", "degenerate axes", "horizon too short", "ca-verify failed",
	} {
		got := rendezvousReasonToErr(reason)
		if !errors.Is(got, ErrRendezvousNoEncounter) {
			t.Errorf("reason %q → %v, want ErrRendezvousNoEncounter", reason, got)
		}
	}
	// "no improvement available" is a DIFFERENT situation (the geometry
	// is already about as good as a nudge gets) and must stay out of the
	// phasing-coach bucket.
	if got := rendezvousReasonToErr("no improvement available"); errors.Is(got, ErrRendezvousNoEncounter) {
		t.Errorf("\"no improvement available\" wrongly joined the phasing-coach bucket: %v", got)
	}
}
