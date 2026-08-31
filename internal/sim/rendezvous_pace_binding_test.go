package sim

import (
	"testing"
	"time"
)

// pacedLeaderWorld returns a World with an engaged rendezvous coast
// (initiator seat, a far-future τ so nothing else — approach ramp, node
// ramp, burn cap — clamps the rate in these tests) so holdRendezvousLeader
// can be exercised directly against hand-built `ahead` values.
func pacedLeaderWorld(t *testing.T) *World {
	t.Helper()
	w := mustWorld(t)
	farFuture := w.Clock.SimTime.Add(200 * time.Hour)
	w.RendezvousArm = &RendezvousArm{
		TargetOwner: harnessOwnerB, Handle: "bob",
		Tau: farFuture, CommittedCA: 1000,
		Initiator: true, BrakeIdx: rendezvousFollowing,
	}
	w.AutoWarp = &AutoWarpTarget{
		T: farFuture, Rendezvous: true,
		RendezvousOwner: harnessOwnerB, RendezvousHandle: "bob",
	}
	w.Clock.Paused = false
	return w
}

// TestHoldRendezvousLeader_TinyAheadDoesNotPace is finding 3 (batch
// review, MEDIUM): relay reports are always at least one tick stale, so
// `ahead` is positive on essentially every coasting tick even when
// nothing is genuinely diverging. Before the fix, holdRendezvousLeader
// set RendezvousPaced whenever the ramp was merely "active" (ahead > 0),
// regardless of magnitude — a sliver of a tick's worth of staleness,
// well under rendezvousWaypointMinLead, must not stand up the
// "coasting Nx — paced to bob" chip line.
func TestHoldRendezvousLeader_TinyAheadDoesNotPace(t *testing.T) {
	w := pacedLeaderWorld(t)
	partner := &CoWarpPeer{
		Owner: harnessOwnerB, Handle: "bob",
		SubspaceTime: w.Clock.SimTime.Add(-50 * time.Millisecond), // one tick's worth of staleness
	}
	// Mirror driveRendezvousCoast's top-of-tick reset, which the real
	// caller always performs before holdRendezvousLeader runs.
	w.RendezvousHold, w.RendezvousPaced, w.RendezvousPaceWarp = false, false, 0

	w.holdRendezvousLeader(partner)

	if w.RendezvousPaced {
		t.Errorf("RendezvousPaced = true for a %v stale report — that's within rendezvousWaypointMinLead, ordinary staleness, not genuine divergence (RendezvousPaceWarp=%v)",
			w.Clock.SimTime.Sub(partner.SubspaceTime), w.RendezvousPaceWarp)
	}
}

// TestHoldRendezvousLeader_JustBelowAndAboveDeadband pins the exact
// boundary the finding 3 fix introduces: ahead == rendezvousWaypointMinLead
// must not pace (the guard is `ahead <= ...`), one second past it must.
func TestHoldRendezvousLeader_JustBelowAndAboveDeadband(t *testing.T) {
	w := pacedLeaderWorld(t)
	partner := &CoWarpPeer{Owner: harnessOwnerB, Handle: "bob"}

	partner.SubspaceTime = w.Clock.SimTime.Add(-rendezvousWaypointMinLead)
	w.RendezvousHold, w.RendezvousPaced, w.RendezvousPaceWarp = false, false, 0
	w.holdRendezvousLeader(partner)
	if w.RendezvousPaced {
		t.Error("RendezvousPaced = true exactly at the deadband — want false (guard is <=)")
	}

	partner.SubspaceTime = w.Clock.SimTime.Add(-rendezvousWaypointMinLead - time.Second)
	w.RendezvousHold, w.RendezvousPaced, w.RendezvousPaceWarp = false, false, 0
	w.holdRendezvousLeader(partner)
	if !w.RendezvousPaced {
		t.Error("RendezvousPaced = false one second past the deadband — want true")
	}
}

// TestHoldRendezvousLeader_LargeAheadPaces is the flip side: once the
// leader has genuinely pulled ahead — most of the way to
// rendezvousPaceCeilingSec — the ceiling is well below the unpaced rate
// and the flag must still stand up (the fix must narrow the flag, not
// disable pacing altogether).
func TestHoldRendezvousLeader_LargeAheadPaces(t *testing.T) {
	w := pacedLeaderWorld(t)
	partner := &CoWarpPeer{
		Owner: harnessOwnerB, Handle: "bob",
		SubspaceTime: w.Clock.SimTime.Add(-time.Duration(rendezvousPaceCeilingSec-1) * time.Second),
	}
	w.RendezvousHold, w.RendezvousPaced, w.RendezvousPaceWarp = false, false, 0

	w.holdRendezvousLeader(partner)

	if !w.RendezvousPaced {
		t.Fatal("RendezvousPaced = false for a leader nearly at the pace ceiling — genuine pacing must still report")
	}
	if w.RendezvousPaceWarp <= 0 {
		t.Errorf("RendezvousPaceWarp = %v, want > 0 while genuinely paced", w.RendezvousPaceWarp)
	}
}

// TestHoldRendezvousLeader_AtCeilingHoldsToZero: ahead beyond the ceiling
// still reports Paced (with PaceWarp effectively 0, the old freeze) —
// unaffected by the finding 3 fix, since 0 is always < any unpaced rate.
func TestHoldRendezvousLeader_AtCeilingHoldsToZero(t *testing.T) {
	w := pacedLeaderWorld(t)
	partner := &CoWarpPeer{
		Owner: harnessOwnerB, Handle: "bob",
		SubspaceTime: w.Clock.SimTime.Add(-time.Duration(rendezvousPaceCeilingSec+30) * time.Second),
	}
	w.RendezvousHold, w.RendezvousPaced, w.RendezvousPaceWarp = false, false, 0

	w.holdRendezvousLeader(partner)

	if !w.RendezvousPaced {
		t.Fatal("RendezvousPaced = false beyond the pace ceiling")
	}
	if w.RendezvousPaceWarp != 0 {
		t.Errorf("RendezvousPaceWarp = %v, want 0 beyond the ceiling", w.RendezvousPaceWarp)
	}
}
