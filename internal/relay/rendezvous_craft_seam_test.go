package relay

import (
	"testing"
	"time"
)

// #295, partner side across the seam: the initiator's acting craft has
// to reach the responder's join prompt. The wrong-vessel arm that
// motivated this was only catchable from the responder's seat, and only
// via an implausible closest approach — the vessel name is the signal
// that should have been there.
//
// The arm always acts through the reporter's active craft, so the name
// rides the #288 marker rather than a second wire field.
func TestInviteCarriesTheInitiatorsCraftAcrossTheSeam(t *testing.T) {
	store := NewStore()
	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime // same subspace: a joinable invite
	wB.ActiveCraft().State.R.X += 50_000
	wB.ActiveCraft().Name = "Relay Tug-1"

	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	handles := map[string]string{ownerA: "alice", ownerB: "bob"}
	if !wB.EngageRendezvousWarp(ownerA, "alice", wA.Clock.SimTime.Add(72*time.Hour), 8000) {
		t.Fatal("B failed to engage")
	}
	NewReporter(store, ownerB).Tick(wB, time.Now())

	peers := CoWarpPeersFrom(wA, store.Snapshot(ownerA), handles, ownerA, live(ownerB), nil)
	if len(peers) != 1 {
		t.Fatalf("got %d peers, want 1", len(peers))
	}
	if peers[0].ActiveCraftName != "Relay Tug-1" {
		t.Errorf("peer ActiveCraftName = %q, want the vessel B armed", peers[0].ActiveCraftName)
	}

	// Through DriveRendezvousWarp onto A's invite slate — what the prompt reads.
	wA.DriveRendezvousWarp(peers)
	inv := wA.RendezvousInvite
	if inv == nil {
		t.Fatal("no invite raised from an armed peer")
	}
	if inv.CraftName != "Relay Tug-1" {
		t.Errorf("invite CraftName = %q, want the initiator's acting craft", inv.CraftName)
	}
}
