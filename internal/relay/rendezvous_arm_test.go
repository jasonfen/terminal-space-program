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

	// B Engages a Rendezvous Warp toward A with an 8 km committed approach.
	const committedCA = 8000.0
	if !wB.EngageRendezvousWarp(ownerA, "alice", tau, committedCA) {
		t.Fatal("B failed to engage")
	}
	NewReporter(store, ownerB).Tick(wB, time.Now())

	// A adapts B's report — arm-toward-viewer + τ + committed CA survive.
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
	if peers[0].RendezvousCA != committedCA {
		t.Errorf("RendezvousCA = %v, want committed %v on the wire", peers[0].RendezvousCA, committedCA)
	}

	// A adopts the initiator's τ + CA and Engages back → mutual couple 50 km apart.
	wA.EngageRendezvousWarp(ownerB, "bob", peers[0].RendezvousTau, peers[0].RendezvousCA)
	res := wA.ComputeCoWarp(peers, nil)
	if !res.State.Coupled {
		t.Error("mutual arm did not couple across the seam at 50 km")
	}
}

// A re-commit toward the SAME partner with a new τ forces a report
// immediately (v0.29 review) — the partner must never adopt a stale τ
// off a heartbeat-delayed report. A pause flip force-reports too (the
// hold-the-leader keys on it).
func TestReporterForcesOnTauAndPauseChange(t *testing.T) {
	store := NewStore()
	w := newWorld(t)
	const owner = "SHA256:bob"
	rep := NewReporter(store, owner)
	tau1 := w.Clock.SimTime.Add(48 * time.Hour)
	w.EngageRendezvousWarp("SHA256:alice", "alice", tau1, 0)
	rep.Tick(w, time.Now())

	// Same partner, new τ — nothing else changed; must still report.
	tau2 := w.Clock.SimTime.Add(60 * time.Hour)
	w.EngageRendezvousWarp("SHA256:alice", "alice", tau2, 0)
	rep.Tick(w, time.Now())
	if got := store.Snapshot("")[0].RendezvousTau; !got.Equal(tau2) {
		t.Errorf("reported τ = %v after re-commit, want %v (stale-τ split-brain)", got, tau2)
	}

	// Pause flip rides the wire promptly.
	w.Clock.Paused = true
	rep.Tick(w, time.Now())
	if !store.Snapshot("")[0].Paused {
		t.Error("pause flip did not force a report — the partner's hold would lag the heartbeat")
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
	wB.EngageRendezvousWarp("SHA256:carol", "carol", wA.Clock.SimTime.Add(time.Hour), 0) // toward someone else
	NewReporter(store, ownerB).Tick(wB, time.Now())

	peers := CoWarpPeersFrom(wA, store.Snapshot(ownerA), handles, ownerA)
	if len(peers) == 1 && peers[0].ArmedTowardViewer {
		t.Error("ArmedTowardViewer set for an arm aimed at a third player")
	}
}
