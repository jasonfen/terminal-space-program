package relay

import (
	"testing"
	"time"
)

// TestRendezvousMeetingPlaceThroughSeam is the ADR 0045 S7 (#400)
// acceptance test: "the accepter's chip names the Meeting Place and the
// initiator's selection, and the accepter cannot change it." Exercises
// the full wire path — SetRendezvousMeeting on the initiator's arm,
// through the reporter, through CoWarpPeersFrom, into the accepter's
// invite, and finally adopted verbatim onto the accepter's OWN arm on
// join — mirroring TestRendezvousArmThroughSeam's shape for Tau/CA.
func TestRendezvousMeetingPlaceThroughSeam(t *testing.T) {
	store := NewStore()
	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime // same subspace

	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	handles := map[string]string{ownerB: "bob", ownerA: "alice"}
	tau := wA.Clock.SimTime.Add(8 * time.Hour)
	const committedCA = 5000.0
	const place, laps = "their orbit", 5

	// B Engages toward A and stamps the Meeting Place it committed from —
	// exactly the sequence app.go's SessionCmdRendezvous handler runs
	// (EngageRendezvousWarpAs then SetRendezvousMeeting).
	if !wB.EngageRendezvousWarp(ownerA, "alice", tau, committedCA) {
		t.Fatal("B failed to engage")
	}
	wB.SetRendezvousMeeting(place, laps)
	NewReporter(store, ownerB).Tick(wB, time.Now())

	// A adapts B's report — the Meeting Place rides alongside τ/CA.
	peers := CoWarpPeersFrom(wA, store.Snapshot(ownerA), handles, ownerA, live(ownerB), nil)
	if len(peers) != 1 {
		t.Fatalf("got %d peers, want 1", len(peers))
	}
	if peers[0].RendezvousMeetingPlace != place {
		t.Errorf("RendezvousMeetingPlace = %q, want %q", peers[0].RendezvousMeetingPlace, place)
	}
	if peers[0].RendezvousMeetingLaps != laps {
		t.Errorf("RendezvousMeetingLaps = %d, want %d", peers[0].RendezvousMeetingLaps, laps)
	}

	// A's own invite slate carries it too (refreshRendezvousInvite reads
	// straight off the peer set built above).
	wA.DriveRendezvousWarp(peers)
	inv := wA.RendezvousInvite
	if inv == nil {
		t.Fatal("A has no invite from B despite a live armed report")
	}
	if inv.MeetingPlaceLabel != place || inv.MeetingLaps != laps {
		t.Errorf("invite Meeting Place = %q/%d, want %q/%d", inv.MeetingPlaceLabel, inv.MeetingLaps, place, laps)
	}

	// A joins, adopting the Meeting Place verbatim (mirrors app.go's [y]
	// handler: EngageRendezvousWarpAs then SetRendezvousMeeting(inv...)).
	if !wA.EngageRendezvousWarpAs(inv.Owner, inv.Handle, inv.Tau, inv.CA, false) {
		t.Fatal("A failed to join")
	}
	wA.SetRendezvousMeeting(inv.MeetingPlaceLabel, inv.MeetingLaps)
	if wA.RendezvousArm.MeetingPlaceLabel != place || wA.RendezvousArm.MeetingLaps != laps {
		t.Errorf("A's arm Meeting Place = %q/%d after join, want %q/%d",
			wA.RendezvousArm.MeetingPlaceLabel, wA.RendezvousArm.MeetingLaps, place, laps)
	}

	// "The accepter cannot change it": nothing in the picker/planner path
	// (PlanMeetingBurn, PlanRendezvousNudge) ever touches RendezvousArm —
	// only SetRendezvousMeeting does, and A's only call to it was the join
	// above. Simulate A independently running their own Meeting Planner
	// (as a copilot idly exploring the tool) and confirm the arm's Place
	// is untouched by it.
	if _, err := wA.PlanMeetingBurn(0 /* MeetingCrossing */, 2); err == nil {
		// Whether or not this particular plan succeeds on A's fixture
		// geometry is irrelevant to the property under test.
		_ = err
	}
	if wA.RendezvousArm.MeetingPlaceLabel != place || wA.RendezvousArm.MeetingLaps != laps {
		t.Errorf("A's arm Meeting Place changed after A ran the Meeting Planner locally: got %q/%d, want unchanged %q/%d",
			wA.RendezvousArm.MeetingPlaceLabel, wA.RendezvousArm.MeetingLaps, place, laps)
	}
}
