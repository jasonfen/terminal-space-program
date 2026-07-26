package sim

import (
	"testing"
	"time"
)

// #253: Away is a state, not a moment — a partner asleep for hours while
// the Reprieve keeps their session flying. The flight view must learn it
// from live state (mirrored here each drive tick), not from the 6 s
// SessionEventWentQuiet chip that expired an hour ago.
func TestDriveRendezvousWarpMirrorsPartnerAway(t *testing.T) {
	w, primary, st := anchorWorld(t)
	w.EngageRendezvousWarp("SHA256:gern", "gern", st.Add(72*time.Hour), 0)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.Away = true

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("coast did not engage on the mutual arm")
	}
	if !w.RendezvousPartnerAway {
		t.Error("armed partner's Away not mirrored onto the world slate")
	}

	// The partner returns: the standing state drops the same tick.
	peer.Away = false
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousPartnerAway {
		t.Error("slate still marks the partner away after they returned")
	}

	// Cancel clears the coast; the away mirror must not outlive the arm.
	peer.Away = true
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	w.DisengageRendezvousWarp()
	w.DriveRendezvousWarp(nil)
	if w.RendezvousPartnerAway {
		t.Error("away mirror survived the arm being released")
	}
}
