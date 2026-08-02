package serve

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The reset epoch is the frontier: a persisted payload ahead of the
// host's clock wins, and every reset payload lands exactly on it —
// no player needs a Sync after a reset.
func TestResetFleetEpochIsStoredFrontier(t *testing.T) {
	srv := newOfflineServer(t)

	// Enroll a guest whose payload is 30 days ahead of the host.
	inv, err := srv.store.MintInvite("veteran")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if _, err := srv.store.Enroll(inv.Code, "SHA256:veteran", "veteran"); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	hostClock := w.Clock.SimTime
	ahead := hostClock.Add(30 * 24 * time.Hour)
	w.Clock.SimTime = ahead
	if err := srv.store.SavePlayer("SHA256:veteran", w); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	entries, epoch, err := srv.ResetFleet(hostClock)
	if err != nil {
		t.Fatalf("ResetFleet: %v", err)
	}
	if !epoch.Equal(ahead) {
		t.Errorf("epoch = %v, want stored frontier %v", epoch, ahead)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (host + veteran)", len(entries))
	}
	got, err := srv.store.LoadPlayer("SHA256:veteran")
	if err != nil {
		t.Fatalf("LoadPlayer after reset: %v", err)
	}
	if !got.Clock.SimTime.Equal(epoch) {
		t.Errorf("veteran SimTime = %v, want epoch %v", got.Clock.SimTime, epoch)
	}
}

// A host clock ahead of every stored payload holds the frontier itself.
func TestResetFleetEpochFloorsAtHostClock(t *testing.T) {
	srv := newOfflineServer(t)
	hostClock := time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	_, epoch, err := srv.ResetFleet(hostClock)
	if err != nil {
		t.Fatalf("ResetFleet: %v", err)
	}
	if !epoch.Equal(hostClock) {
		t.Errorf("epoch = %v, want host clock %v", epoch, hostClock)
	}
}
