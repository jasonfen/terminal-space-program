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

// Merge guard (#253 × #252): the away mirror must run on EVERY drive tick
// with a matched partner — including the τ-crossing and the past-τ idle
// where waypoint derivation fails and driveRendezvousCoast returns early
// each tick. That idle stretch is strongly correlated with the partner
// being away (an asleep partner can't burn the next encounter into
// existence), so a mirror placed below the τ-reached early return would
// leave the flag cleared-and-never-set for the whole stretch and silently
// blank the standing away line in exactly its motivating scenario. The
// peer sits 100 km out on a plain radial offset: outside the couple gate,
// and a shape whose closest approach is at t=0, so rendezvousNextWaypoint
// can never derive an advance — the coast idles at the passed τ, retrying.
func TestRendezvousAwayLineSurvivesTauCrossing(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 50_000)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.Away = true

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: mutual arm did not engage the coast")
	}
	if !w.RendezvousPartnerAway {
		t.Fatal("precondition: Away not mirrored pre-τ")
	}

	// The crossing tick: both clocks at τ, still 100 km apart — the
	// waypoint-resolution path runs (and its derivation fails → idle).
	w.Clock.SimTime = tau
	peer.SubspaceTime = tau
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousArm == nil || !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: standing intent did not survive the τ crossing")
	}
	if !w.RendezvousPartnerAway {
		t.Error("away line lost on the τ-crossing tick (#253 re-opened)")
	}

	// Subsequent idle ticks: derivation keeps failing, the coast idles at
	// the passed τ every tick — the standing state must persist throughout.
	for i := 0; i < 3; i++ {
		w.Clock.SimTime = w.Clock.SimTime.Add(time.Minute)
		peer.SubspaceTime = w.Clock.SimTime
		w.DriveRendezvousWarp([]CoWarpPeer{peer})
		if !w.RendezvousPartnerAway {
			t.Fatalf("away line lost on past-τ idle tick %d", i+1)
		}
	}
}
