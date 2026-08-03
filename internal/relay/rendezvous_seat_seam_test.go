package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The seat and the seat's published rate must cross the wire (ADR 0037
// §2's "the role must be unambiguous under reconnect"): the reporter
// stamps them onto the report and CoWarpPeersFrom carries them onto the
// peer the rate rule reads. Without this seam both sides sit in the
// terminal phase with unresolved seats and silently keep min-wins.
func TestSeatAndRateCrossTheWire(t *testing.T) {
	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime // same subspace

	// alice initiated and is flying the pair's clock at 1000×.
	tau := wA.Clock.SimTime.Add(time.Hour)
	if !wA.EngageRendezvousWarpAs(ownerB, "bob", tau, 8000, true) {
		t.Fatal("precondition: alice armed as initiator")
	}
	wA.RendezvousArm.Approach = true
	wA.Clock.WarpIdx = 3

	store := NewStore()
	NewReporter(store, ownerA).Tick(wA, time.Now())
	reports := store.Snapshot(ownerB)
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want alice's", len(reports))
	}
	if !reports[0].RendezvousInitiator {
		t.Error("initiator seat did not reach the wire")
	}
	if reports[0].RendezvousRate != 1000 {
		t.Errorf("published rate = %v, want alice's selected 1000×", reports[0].RendezvousRate)
	}

	live := map[string]bool{ownerA: true}
	peers := CoWarpPeersFrom(wB, reports, map[string]string{ownerA: "alice"}, ownerB, live, nil)
	if len(peers) != 1 {
		t.Fatalf("peers = %d, want alice", len(peers))
	}
	if !peers[0].ArmedTowardViewer {
		t.Fatal("precondition: alice's arm names bob")
	}
	if !peers[0].RendezvousInitiator || peers[0].RendezvousRate != 1000 {
		t.Errorf("seat/rate lost at the peer seam: initiator=%v rate=%v",
			peers[0].RendezvousInitiator, peers[0].RendezvousRate)
	}

	// bob joins as copilot and lands in the terminal phase: his clock
	// becomes alice's, not his own 1× selection.
	if !wB.EngageRendezvousWarpAs(ownerA, "alice", tau, 8000, false) {
		t.Fatal("bob could not join")
	}
	wB.RendezvousArm.Approach = true
	wB.DriveRendezvousWarp(peers)
	if got := wB.RendezvousRate.Seat; got != sim.RendezvousSeatCopilot {
		t.Fatalf("bob's seat = %v, want copilot", got)
	}
	if got := wB.EffectiveWarp(); got != 1000 {
		t.Errorf("bob's effective warp = %v, want alice's 1000×", got)
	}
}

// A peer from before ADR 0037 publishes neither bit; the pair must fall
// back to min-wins rather than one side assuming command.
func TestPreSeatPeerLeavesSeatsUnresolved(t *testing.T) {
	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	wB := newWorld(t)
	tau := wB.Clock.SimTime.Add(time.Hour)
	wB.EngageRendezvousWarpAs(ownerA, "alice", tau, 8000, false)
	wB.RendezvousArm.Approach = true

	peers := []sim.CoWarpPeer{{
		Owner: ownerA, Handle: "alice", SubspaceTime: wB.Clock.SimTime,
		EffWarp: 10, ArmedTowardViewer: true, RendezvousTau: tau,
		Crafts: []sim.CoWarpCraft{{
			Primary: wB.ActiveCraft().Primary.ID,
			R:       wB.ActiveCraft().State.R,
			V:       wB.ActiveCraft().State.V,
		}},
	}}
	wB.DriveRendezvousWarp(peers)
	if got := wB.RendezvousRate.Seat; got != sim.RendezvousSeatNone {
		t.Errorf("seat = %v, want none against a peer that publishes no seat", got)
	}
	res := wB.ComputeCoWarp(peers, map[string]bool{ownerA: true})
	if res.State.MinWarp != 10 {
		t.Errorf("MinWarp = %v, want min-wins over the peer's reported 10×", res.State.MinWarp)
	}
}
