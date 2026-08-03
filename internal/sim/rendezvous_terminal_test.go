package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// armedApproachWorld drives a mutual arm through its τ handoff and
// returns the world sitting in the demoted terminal phase (ADR 0037 S1),
// plus the partner peer so the caller can keep feeding it.
func armedApproachWorld(t *testing.T, initiator bool) (*World, CoWarpPeer) {
	t.Helper()
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", tau, 6000, initiator) {
		t.Fatal("precondition: arm engaged")
	}
	// 6 km out at ~594 m/s relative — the live playtest shape (#299/#302):
	// inside the 35 km range gate, well over the 100 m/s velocity term.
	near := peerAt(w, primary, st, 50, orbital.Vec3{X: 6000}, orbital.Vec3{X: 594}, "gern")
	near.ArmedTowardViewer = true
	near.RendezvousTau = tau
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}
	w.Clock.WarpIdx = 5
	w.Clock.SimTime = tau
	near.SubspaceTime = tau
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	return w, near
}

// The τ handoff demotes the agreement instead of ending it (ADR 0037 S1):
// #299's release of the driver and the drop to 1× are kept exactly, but
// the mutual intent survives so the pair stays time-locked through the
// terminal phase's braking burns and waits (#302).
func TestTauHandoffDemotesAgreementInsteadOfEndingIt(t *testing.T) {
	w, _ := armedApproachWorld(t, true)

	if w.rendezvousWarpEngaged() {
		t.Error("coast still engaged at τ inside couple range — the ship must be handed back (#299)")
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("did not drop to 1× at the handoff: WarpIdx = %d", w.Clock.WarpIdx)
	}
	if w.LastRendezvousArrival == nil {
		t.Fatal("arrival not recorded for the chip")
	}
	arm := w.RendezvousArm
	if arm == nil {
		t.Fatal("agreement ended at the handoff — ADR 0037 S1 demotes it, it does not end it")
	}
	if !arm.Approach {
		t.Error("agreement survived but was not demoted to the approach phase")
	}
	if !w.RendezvousApproachPhase() {
		t.Error("RendezvousApproachPhase() false in the terminal phase")
	}
}

// The demoted agreement keeps the pair warp-coupled with no distance
// tripwire (ADR 0037 S1): a pilot who swings 100 km wide — past both the
// 35 km couple gate and the 42 km hysteresis band — is still
// rendezvousing, and the lock holds while they fly back.
func TestApproachAgreementHoldsCoupleBeyondHysteresisBand(t *testing.T) {
	w, near := armedApproachWorld(t, true)
	// 100 km out, fast: outside every proximity gate.
	near.Crafts[0].R = w.ActiveCraft().State.R.Add(orbital.Vec3{X: 100_000})
	near.Crafts[0].V = w.ActiveCraft().State.V.Add(orbital.Vec3{X: 594})
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if w.RendezvousArm == nil {
		t.Fatal("agreement dropped on distance — there is no distance tripwire (ADR 0037 S1)")
	}
	res := w.ComputeCoWarp([]CoWarpPeer{near}, map[string]bool{"SHA256:gern": true})
	if !res.State.Coupled {
		t.Error("pair uncoupled 100 km out — the standing agreement, not range, holds the lock")
	}
}

// Either side's explicit cancel ends the demoted agreement — the retract
// travels the wire, so the surviving side sees the partner's arm vanish
// while their report is still present and releases in turn.
func TestApproachAgreementEndsOnPartnerRetract(t *testing.T) {
	w, near := armedApproachWorld(t, true)
	near.ArmedTowardViewer = false // they pressed [/]
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if w.RendezvousArm != nil {
		t.Error("partner's retract did not end the demoted agreement")
	}
}

// A whole-peer dropout gets the same grace the coast gives it: idle
// through a transient gap, release once the peer has been absent longer
// than the same-subspace window (the lifetime rules carry over, S1).
func TestApproachAgreementHoldsThroughPeerDropoutThenReleases(t *testing.T) {
	w, _ := armedApproachWorld(t, true)
	w.DriveRendezvousWarp(nil)
	if w.RendezvousArm == nil {
		t.Fatal("a one-tick peer dropout destroyed the agreement — every other gap is held through")
	}
	w.Clock.SimTime = w.Clock.SimTime.Add(time.Duration(coWarpSubspaceToleranceSec+1) * time.Second)
	w.DriveRendezvousWarp(nil)
	if w.RendezvousArm != nil {
		t.Error("agreement outlived a peer absent past the tolerance window")
	}
}

// Docking is the other end condition (ADR 0037 S1): once the pair are one
// stack there is no rendezvous left to hold.
func TestApproachAgreementEndsOnDock(t *testing.T) {
	w, _ := armedApproachWorld(t, true)
	if w.EndRendezvousOnDock("SHA256:someone-else") {
		t.Error("a dock with a third party ended the agreement")
	}
	if w.RendezvousArm == nil {
		t.Fatal("agreement ended by an unrelated dock")
	}
	if !w.EndRendezvousOnDock("SHA256:gern") {
		t.Fatal("docking with the partner did not end the agreement")
	}
	if w.RendezvousArm != nil {
		t.Error("agreement survived the dock")
	}
}

// The demoted phase must not restart the coast: the driver stays released
// so the pilot can brake at closest approach (#299's whole point), and the
// armed-waiting surfaces must not re-appear behind it.
func TestApproachPhaseDoesNotRestartTheCoast(t *testing.T) {
	w, near := armedApproachWorld(t, true)
	for i := 0; i < 3; i++ {
		w.Clock.SimTime = w.Clock.SimTime.Add(time.Second)
		near.SubspaceTime = w.Clock.SimTime
		w.DriveRendezvousWarp([]CoWarpPeer{near})
	}
	if w.rendezvousWarpEngaged() {
		t.Error("the terminal phase restarted the shared coast")
	}
	if w.RendezvousWait.Reason != RendezvousWaitNone {
		t.Errorf("terminal phase classified as armed-waiting: %v", w.RendezvousWait.Reason)
	}
}

// An away partner still shows the standing away line through the terminal
// phase — the lifetime rules carry over unchanged (S1).
func TestApproachPhaseMirrorsPartnerAway(t *testing.T) {
	w, near := armedApproachWorld(t, true)
	near.Away = true
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if !w.RendezvousPartnerAway {
		t.Error("away partner not mirrored onto the slate in the terminal phase")
	}
}
