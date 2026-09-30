package relay

import (
	"testing"
	"time"
)

// TestRendezvousOrbitThroughSeam is the ADR 0045 S7 (#400)
// acceptance test: "the accepter's chip names the Rendezvous Orbit and the
// initiator's selection, and the accepter cannot change it." Exercises
// the full wire path — SetRendezvousOrbit on the initiator's arm,
// through the reporter, through CoWarpPeersFrom, into the accepter's
// invite, and finally adopted verbatim onto the accepter's OWN arm on
// join — mirroring TestRendezvousArmThroughSeam's shape for Tau/CA.
func TestRendezvousOrbitThroughSeam(t *testing.T) {
	store := NewStore()
	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime // same subspace

	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	handles := map[string]string{ownerB: "bob", ownerA: "alice"}
	tau := wA.Clock.SimTime.Add(8 * time.Hour)
	const committedCA = 5000.0
	const place, laps = "their orbit", 5

	// B Engages toward A and stamps the Rendezvous Orbit it committed from —
	// exactly the sequence app.go's SessionCmdRendezvous handler runs
	// (EngageRendezvousWarpAs then SetRendezvousOrbit).
	if !wB.EngageRendezvousWarp(ownerA, "alice", tau, committedCA) {
		t.Fatal("B failed to engage")
	}
	wB.SetRendezvousOrbit(place, laps)
	NewReporter(store, ownerB).Tick(wB, time.Now())

	// A adapts B's report — the Rendezvous Orbit rides alongside τ/CA.
	peers := CoWarpPeersFrom(wA, store.Snapshot(ownerA), handles, ownerA, live(ownerB), nil)
	if len(peers) != 1 {
		t.Fatalf("got %d peers, want 1", len(peers))
	}
	if peers[0].RendezvousOrbit != place {
		t.Errorf("RendezvousOrbit = %q, want %q", peers[0].RendezvousOrbit, place)
	}
	if peers[0].RendezvousLaps != laps {
		t.Errorf("RendezvousLaps = %d, want %d", peers[0].RendezvousLaps, laps)
	}

	// A's own invite slate carries it too (refreshRendezvousInvite reads
	// straight off the peer set built above).
	wA.DriveRendezvousWarp(peers)
	inv := wA.RendezvousInvite
	if inv == nil {
		t.Fatal("A has no invite from B despite a live armed report")
	}
	if inv.RendezvousOrbitLabel != place || inv.RendezvousLaps != laps {
		t.Errorf("invite Rendezvous Orbit = %q/%d, want %q/%d", inv.RendezvousOrbitLabel, inv.RendezvousLaps, place, laps)
	}

	// A joins, adopting the Rendezvous Orbit verbatim (mirrors app.go's [y]
	// handler: EngageRendezvousWarpAs then SetRendezvousOrbit(inv...)).
	if !wA.EngageRendezvousWarpAs(inv.Owner, inv.Handle, inv.Tau, inv.CA, false) {
		t.Fatal("A failed to join")
	}
	wA.SetRendezvousOrbit(inv.RendezvousOrbitLabel, inv.RendezvousLaps)
	if wA.RendezvousArm.RendezvousOrbitLabel != place || wA.RendezvousArm.RendezvousLaps != laps {
		t.Errorf("A's arm Rendezvous Orbit = %q/%d after join, want %q/%d",
			wA.RendezvousArm.RendezvousOrbitLabel, wA.RendezvousArm.RendezvousLaps, place, laps)
	}

	// "The accepter cannot change it": nothing in the picker/planner path
	// (PlanRendezvousBurn, PlanRendezvousNudge) ever touches RendezvousArm —
	// only SetRendezvousOrbit does, and A's only call to it was the join
	// above. Simulate A independently running their own Rendezvous Planner
	// (as a copilot idly exploring the tool) and confirm the arm's Place
	// is untouched by it.
	if _, err := wA.PlanRendezvousBurn(0 /* RendezvousCrossing */, 2); err == nil {
		// Whether or not this particular plan succeeds on A's fixture
		// geometry is irrelevant to the property under test.
		_ = err
	}
	if wA.RendezvousArm.RendezvousOrbitLabel != place || wA.RendezvousArm.RendezvousLaps != laps {
		t.Errorf("A's arm Rendezvous Orbit changed after A ran the Rendezvous Planner locally: got %q/%d, want unchanged %q/%d",
			wA.RendezvousArm.RendezvousOrbitLabel, wA.RendezvousArm.RendezvousLaps, place, laps)
	}
}
