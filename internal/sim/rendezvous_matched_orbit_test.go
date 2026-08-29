package sim

import "testing"

// TestRendezvousCommitMatchedOrbitsRefusesPhantomNudge (#276): two craft
// on the exact same circular orbit, differing only by a small phase lag,
// have zero relative drift — their separation on the CURRENT courses is
// constant for all time, so no future closest approach exists to commit
// to. The K-nudge advisory, however, DOES find a real burn here (a small
// phasing nudge would close the gap) — rendezvousSmallLagWorld exists
// specifically to guarantee that precondition (see
// TestPlanRendezvousNudge_PlantsOneNode).
//
// Before the fix, RendezvousCommit tried the advisory first and adopted
// its post-burn τ/CA even though PlanRendezvousNudge (K) was never
// called and no burn was ever made — so Engage silently armed toward an
// encounter the player was not actually flying toward. Engage must
// commit only to an encounter reachable on the CURRENT courses; if only
// a nudge would create one, it must refuse (the same refusal the
// current-course fallback already produces) rather than borrow the
// advisory's unfired result.
func TestRendezvousCommitMatchedOrbitsRefusesPhantomNudge(t *testing.T) {
	w := rendezvousSmallLagWorld(t)

	// Precondition: confirm the advisory finds a real plantable nudge for
	// this geometry — otherwise this test would trivially pass for the
	// wrong reason (no advisory to wrongly prefer in the first place).
	adv, aok := w.RecommendedRendezvousBurn()
	if !aok || !adv.Ok {
		t.Skipf("this geometry yields no advisory nudge (ok=%v adv.Ok=%v); not exercising the phantom-commit path", aok, adv.Ok)
	}

	if _, _, ok := w.RendezvousCommit(); ok {
		t.Error("RendezvousCommit committed to an encounter with no burn fired — " +
			"matched orbits have zero relative drift and can never close on the current courses (#276)")
	}
}
