package sim

import (
	"testing"
	"time"
)

// Engage records the arm and is forward-only: an encounter at or behind
// SimTime is refused (the laggard Syncs forward; you never warp backward).
func TestEngageRendezvousWarpForwardOnly(t *testing.T) {
	w, _, st := anchorWorld(t)

	if w.EngageRendezvousWarp("SHA256:gern", st.Add(-time.Hour)) {
		t.Error("engaged toward a past encounter")
	}
	if w.RendezvousArm != nil {
		t.Error("arm set despite forward-only refusal")
	}
	if !w.EngageRendezvousWarp("SHA256:gern", st.Add(72*time.Hour)) {
		t.Fatal("refused a future encounter")
	}
	if w.RendezvousArm == nil || w.RendezvousArm.TargetOwner != "SHA256:gern" {
		t.Errorf("arm = %+v, want targeting gern", w.RendezvousArm)
	}
	// Engaging does NOT start the coast on its own — that waits for mutual.
	if w.AutoWarp != nil {
		t.Error("Auto-Warp started at engage (should wait for the partner)")
	}
}

// The shared coast starts only once the partner has Engaged back: armed
// but unpartnered holds warp unchanged (no solo drift).
func TestDriveRendezvousWarpWaitsForPartner(t *testing.T) {
	w, primary, st := anchorWorld(t)
	w.EngageRendezvousWarp("SHA256:gern", st.Add(72*time.Hour))
	peer := armPeer(w, primary, st, 50, "gern")
	peer.ArmedTowardViewer = false // partner hasn't Engaged yet

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.AutoWarp != nil {
		t.Error("coast started before the partner Engaged (solo drift)")
	}
}

// Both armed toward each other, same subspace → the shared Auto-Warp to
// the committed τ engages and unpauses.
func TestDriveRendezvousWarpStartsOnMutual(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", tau)
	w.Clock.Paused = true
	peer := armPeer(w, primary, st, 50, "gern") // ArmedTowardViewer = true

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.AutoWarp == nil || !w.AutoWarp.Rendezvous {
		t.Fatalf("shared coast not engaged on mutual arm: %+v", w.AutoWarp)
	}
	if !w.AutoWarp.T.Equal(tau) {
		t.Errorf("coast target = %v, want committed τ %v", w.AutoWarp.T, tau)
	}
	if w.Clock.Paused {
		t.Error("engaging the coast did not unpause")
	}
}

// Partner retracts (cancels) mid-coast → both release: the arm clears and
// the Auto-Warp drops.
func TestDriveRendezvousWarpCancelsOnRetract(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", tau)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer}) // engaged
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast should be engaged")
	}

	peer.ArmedTowardViewer = false // partner retracted
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.rendezvousWarpEngaged() {
		t.Error("coast survived the partner's retract")
	}
	if w.RendezvousArm != nil {
		t.Error("arm survived the partner's retract (both should release)")
	}
}

// Cancel from the viewer's side clears the arm and releases the coast
// without touching Selected Warp.
func TestDisengageRendezvousWarp(t *testing.T) {
	w, primary, st := anchorWorld(t)
	w.Clock.WarpIdx = 3
	w.EngageRendezvousWarp("SHA256:gern", st.Add(72*time.Hour))
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})

	w.DisengageRendezvousWarp()
	if w.RendezvousArm != nil || w.rendezvousWarpEngaged() {
		t.Error("cancel left arm/coast engaged")
	}
	if w.Clock.WarpIdx != 3 {
		t.Errorf("cancel touched Selected Warp: WarpIdx = %d, want 3", w.Clock.WarpIdx)
	}
}

// Arrival at τ hands off: drop to 1×, clear the arm (proximity co-warp
// takes over), and record the arrival for the S2 chip.
func TestRendezvousWarpArrivalHandsOff(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", tau)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})

	w.Clock.WarpIdx = 5
	w.Clock.SimTime = tau // reached the encounter
	w.resolveAutoWarp()

	if w.rendezvousWarpEngaged() {
		t.Error("coast still engaged past τ")
	}
	if w.RendezvousArm != nil {
		t.Error("arm not cleared at arrival (won't hand off to proximity)")
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("did not drop to 1× at arrival: WarpIdx = %d", w.Clock.WarpIdx)
	}
	if w.LastRendezvousArrival == nil || w.LastRendezvousArrival.Owner != "SHA256:gern" {
		t.Errorf("arrival not recorded for the chip: %+v", w.LastRendezvousArrival)
	}
}
