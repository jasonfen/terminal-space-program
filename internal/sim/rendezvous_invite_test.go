package sim

import (
	"testing"
	"time"
)

// The invite slate (v0.29 S2): a peer armed toward an unarmed viewer
// surfaces as World.RendezvousInvite — the orbit HUD's persistent join
// prompt — and clears when the viewer arms back (mutual), the peer
// retracts, or the committed τ passes.
func TestRendezvousInviteSlate(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.RendezvousTau = tau
	peer.RendezvousCA = 1234

	// Armed peer, unarmed viewer → invite with the peer's committed τ+CA.
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	inv := w.RendezvousInvite
	if inv == nil {
		t.Fatal("no invite from an armed peer")
	}
	if inv.Owner != peer.Owner || inv.Handle != "gern" || !inv.Tau.Equal(tau) || inv.CA != 1234 {
		t.Errorf("invite = %+v, want gern's committed τ+CA", inv)
	}

	// Viewer Engages back (mutual arm) → the prompt clears.
	w.EngageRendezvousWarp(peer.Owner, "gern", tau, 1234)
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousInvite != nil {
		t.Error("invite survived the viewer's own arm (mutual — nothing to respond to)")
	}

	// Partner retracts after the mutual coast → arm clears AND no invite.
	peer.ArmedTowardViewer = false
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousInvite != nil {
		t.Error("invite shown from a peer that is no longer armed")
	}
}

// A stale invite — committed τ at or behind the viewer's clock — is not
// surfaced: the encounter is gone and Engage would refuse it anyway.
func TestRendezvousInviteIgnoresPastTau(t *testing.T) {
	w, primary, st := anchorWorld(t)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.RendezvousTau = st.Add(-time.Minute)

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousInvite != nil {
		t.Errorf("invite surfaced for a past τ: %+v", w.RendezvousInvite)
	}
}

// RendezvousWarpEngaged is the exported engaged-state accessor the HUD
// reads (unexported rendezvousWarpEngaged stays the sim-internal form).
func TestRendezvousWarpEngagedExported(t *testing.T) {
	w, primary, st := anchorWorld(t)
	if w.RendezvousWarpEngaged() {
		t.Fatal("engaged before any arm")
	}
	w.EngageRendezvousWarp("SHA256:gern", "gern", st.Add(72*time.Hour), 0)
	w.DriveRendezvousWarp([]CoWarpPeer{armPeer(w, primary, st, 50, "gern")})
	if !w.RendezvousWarpEngaged() {
		t.Error("not engaged after the mutual arm started the coast")
	}
}

// RendezvousCommit finds the encounter the initiator commits to: with a
// bound relative target it returns a forward τ and a finite approach —
// from the K-nudge advisory when it has a burn to offer, else the
// current-course closest approach. Without a target it reports ok=false.
func TestRendezvousCommit(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	tau, ca, ok := w.RendezvousCommit()
	if !ok {
		t.Fatal("no commit for a bound LEO target pair")
	}
	if !tau.After(w.Clock.SimTime) {
		t.Errorf("committed τ %v not after SimTime %v", tau, w.Clock.SimTime)
	}
	if ca < 0 {
		t.Errorf("committed CA = %v, want ≥ 0", ca)
	}

	w.ClearTarget()
	if _, _, ok := w.RendezvousCommit(); ok {
		t.Error("commit succeeded with no relative target")
	}
}
