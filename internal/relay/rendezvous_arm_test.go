package relay

import (
	"testing"
	"time"
)

// The Rendezvous Warp arm crosses the reporting seam: B Engages toward A,
// B's report carries the intent, and CoWarpPeersFrom turns "report names
// the viewer" into ArmedTowardViewer + the committed τ. With A armed back,
// the pair couples 50 km apart — proof the arm, not proximity, coupled it.
func TestRendezvousArmThroughSeam(t *testing.T) {
	store := NewStore()
	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime // same subspace
	// 50 km apart — far outside the proximity gate.
	wB.ActiveCraft().State.R.X += 50_000

	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	handles := map[string]string{ownerB: "bob", ownerA: "alice"}
	tau := wA.Clock.SimTime.Add(72 * time.Hour)

	// B Engages a Rendezvous Warp toward A.
	if !wB.EngageRendezvousWarp(ownerA, tau) {
		t.Fatal("B failed to engage")
	}
	NewReporter(store, ownerB).Tick(wB, time.Now())

	// A adapts B's report — the arm-toward-viewer + τ survive the wire.
	peers := CoWarpPeersFrom(wA, store.Snapshot(ownerA), handles, ownerA)
	if len(peers) != 1 {
		t.Fatalf("got %d peers, want 1", len(peers))
	}
	if !peers[0].ArmedTowardViewer {
		t.Error("ArmedTowardViewer not set from a report naming the viewer")
	}
	if !peers[0].RendezvousTau.Equal(tau) {
		t.Errorf("RendezvousTau = %v, want committed τ %v", peers[0].RendezvousTau, tau)
	}

	// A Engages back → mutual arm couples them 50 km apart.
	wA.EngageRendezvousWarp(ownerB, tau)
	res := wA.ComputeCoWarp(peers, nil)
	if !res.State.Coupled {
		t.Error("mutual arm did not couple across the seam at 50 km")
	}
}

// A report that does NOT name the viewer leaves ArmedTowardViewer false
// (an arm aimed at a third player must not couple to us).
func TestRendezvousArmNotForViewer(t *testing.T) {
	store := NewStore()
	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime

	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	handles := map[string]string{ownerB: "bob"}
	wB.EngageRendezvousWarp("SHA256:carol", wA.Clock.SimTime.Add(time.Hour)) // toward someone else
	NewReporter(store, ownerB).Tick(wB, time.Now())

	peers := CoWarpPeersFrom(wA, store.Snapshot(ownerA), handles, ownerA)
	if len(peers) == 1 && peers[0].ArmedTowardViewer {
		t.Error("ArmedTowardViewer set for an arm aimed at a third player")
	}
}
