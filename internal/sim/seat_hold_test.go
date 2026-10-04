package sim

import (
	"testing"
	"time"
)

// TestMutualArmWaitsForSeatHold (B11 review L-mp): a partner's mutual arm
// must not unpause a clock this seat's pause menu holds; the unpause is
// recorded for the UI to apply on release.
func TestMutualArmWaitsForSeatHold(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	w.Clock.Paused = true
	w.SeatHold = true
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.AutoWarp == nil || !w.AutoWarp.Rendezvous {
		t.Fatalf("the coast should still be engaged: %+v", w.AutoWarp)
	}
	if !w.Clock.Paused {
		t.Error("mutual arm unpaused the clock under the seat's pause menu")
	}
	if !w.SeatHoldUnpause {
		t.Error("the deferred unpause was not recorded")
	}
}

// TestSyncWarpWaitsForSeatHold: same rule for Sync.
func TestSyncWarpWaitsForSeatHold(t *testing.T) {
	w, _, _ := anchorWorld(t)
	w.Clock.Paused = true
	w.SeatHold = true
	if !w.EngageSyncWarp(w.Clock.SimTime.Add(time.Hour), "o", "h") {
		t.Fatal("sync refused")
	}
	if !w.Clock.Paused || !w.SeatHoldUnpause {
		t.Errorf("sync under a seat hold: paused=%v deferred=%v, want true/true", w.Clock.Paused, w.SeatHoldUnpause)
	}
}
