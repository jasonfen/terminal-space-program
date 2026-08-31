package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// ADR 0045 S7 (#400): Engage becomes the agreement to meet. These tests
// cover the "agreed, no plan yet" state — RendezvousArm with a zero Tau
// — directly at the sim layer: it must not expire on its own (unlike a
// real, bounded τ), it must not start an Auto-Warp coast (there is
// nothing to chase), and it must still end on an explicit cancel from
// either side.

// TestEngageRendezvousWarpAs_ZeroTauFormsUnplannedAgreement is the #400
// acceptance test: "Engage succeeds with no encounter inside 4h and no
// planted node." At this layer that is EngageRendezvousWarpAs accepting
// a zero (unset) tau — app.go's SessionCmdRendezvous handler is what
// actually computes and passes that zero tau via RendezvousCommitWithPlan,
// covered end-to-end by the tui-layer tests
// (TestEngageRendezvousNoEncounterFormsUnplannedAgreement,
// TestEngageRendezvousSmallLagFormsUnplannedAgreement).
func TestEngageRendezvousWarpAs_ZeroTauFormsUnplannedAgreement(t *testing.T) {
	w, _, _ := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, true) {
		t.Fatal("Engage with a zero tau should succeed (agreed, no plan yet)")
	}
	if w.RendezvousArm == nil {
		t.Fatal("RendezvousArm not set")
	}
	if !w.RendezvousArm.Tau.IsZero() {
		t.Errorf("Tau = %v, want zero", w.RendezvousArm.Tau)
	}
	if !w.RendezvousUnplanned() {
		t.Error("RendezvousUnplanned() = false, want true")
	}
	if w.RendezvousApproachPhase() {
		t.Error("RendezvousApproachPhase() = true — the unplanned state must never set Approach, it never demoted from a coast")
	}
}

// TestUnplannedAgreement_DoesNotExpireAcrossTicks: a real committed τ
// eventually passes and the arm-expiry check drops it (v0.29 review);
// an unplanned arm has no τ to pass and must survive indefinitely —
// only an explicit cancel or a partner retract while mutual ends it.
// Drives several ticks with no partner present at all (the ordinary
// "still waiting to be joined" case) and confirms the arm survives.
func TestUnplannedAgreement_DoesNotExpireAcrossTicks(t *testing.T) {
	w, _, _ := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, true) {
		t.Fatal("Engage should succeed")
	}
	for i := 0; i < 5; i++ {
		w.Clock.SimTime = w.Clock.SimTime.Add(time.Hour)
		w.DriveRendezvousWarp(nil)
		if w.RendezvousArm == nil {
			t.Fatalf("arm expired after %d hour(s) of no partner report — an unplanned arm has no time bound", i+1)
		}
	}
}

// TestUnplannedAgreement_MutualDoesNotStartCoast: even once the partner
// has Engaged back (mutual), a zero-Tau arm must not start the Auto-Warp
// driver — there is nothing to chase. The pair still couples via
// ComputeCoWarp's `armed` branch (proven separately, see
// TestComputeCoWarp_UnplannedMutualArmCouples), but no AutoWarp target is
// ever created for it.
func TestUnplannedAgreement_MutualDoesNotStartCoast(t *testing.T) {
	w, primary, st := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, true) {
		t.Fatal("Engage should succeed")
	}
	peer := peerAt(w, primary, st, 3, orbital.Vec3{X: 50_000}, orbital.Vec3{}, "gern")
	peer.ArmedTowardViewer = true

	w.DriveRendezvousWarp([]CoWarpPeer{peer})

	if w.rendezvousWarpEngaged() {
		t.Error("Auto-Warp engaged for an unplanned mutual arm — there is no τ to chase")
	}
	if !w.RendezvousMutualUnplanned {
		t.Error("RendezvousMutualUnplanned = false, want true once the partner has armed back")
	}
	if w.RendezvousArm == nil {
		t.Fatal("arm cleared unexpectedly")
	}
}

// TestUnplannedAgreement_SelfCancelEnds is the #400 acceptance test's
// self-cancel half: "a test that cancel still ends the agreement from
// either side." DisengageRendezvousWarp (the [/] key's action on both
// the initiator and accepter side — app.go routes both through it) must
// clear an unplanned arm exactly like a planned one.
func TestUnplannedAgreement_SelfCancelEnds(t *testing.T) {
	w, _, _ := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, true) {
		t.Fatal("Engage should succeed")
	}
	w.DisengageRendezvousWarp()
	if w.RendezvousArm != nil {
		t.Error("cancel did not clear an unplanned arm")
	}
	if w.RendezvousUnplanned() {
		t.Error("RendezvousUnplanned() still true after cancel")
	}
}

// TestUnplannedAgreement_AccepterSelfCancelEnds mirrors the test above
// from the ACCEPTER's seat (initiator=false, the [y]-join path) — the
// same [/] cancel action, the other seat.
func TestUnplannedAgreement_AccepterSelfCancelEnds(t *testing.T) {
	w, _, _ := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, false) {
		t.Fatal("Engage (as accepter) should succeed")
	}
	if w.RendezvousArm.Initiator {
		t.Fatal("precondition: accepter seat")
	}
	w.DisengageRendezvousWarp()
	if w.RendezvousArm != nil {
		t.Error("cancel did not clear an unplanned accepter arm")
	}
}

// TestComputeCoWarp_UnplannedMutualArmCouples pins ADR 0045 S7 (#400)'s
// documented scope decision, load-bearing for the ComputeCoWarp
// clamp-exemption ordering the PR review flags as hand-verified-once
// (v0.33): a MUTUAL unplanned arm still couples via the `armed` branch
// (unconditional on Tau — this is unchanged, pre-existing behaviour),
// but it does NOT claim the #248 clamp exemption (that stays keyed on
// rendezvousWarpEngaged/rendezvousSeatWith, both false here since the
// coast never started and Approach was never set) — ordinary min-wins
// applies until a plan lands. A regression that accidentally widened the
// exemption to cover the unplanned state would flip this test's MinWarp
// assertion from "capped at the peer's EffWarp" to "0 / uncapped".
func TestComputeCoWarp_UnplannedMutualArmCouples(t *testing.T) {
	w, primary, st := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, true) {
		t.Fatal("Engage should succeed")
	}
	peer := peerAt(w, primary, st, 3, orbital.Vec3{X: 50_000}, orbital.Vec3{}, "gern")
	peer.ArmedTowardViewer = true

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, nil)
	if !res.State.Coupled {
		t.Fatal("mutual unplanned arm did not couple — the `armed` branch should couple regardless of Tau")
	}
	if res.State.MinWarp != peer.EffWarp {
		t.Errorf("MinWarp = %v, want %v (peer's EffWarp) — an unplanned mutual arm unexpectedly claimed the #248 clamp exemption", res.State.MinWarp, peer.EffWarp)
	}
}

// TestRefreshRendezvousDegrade_UnplannedNeverTrips verifies watch-point
// 3 rather than assuming it (ADR 0045 S7, #400): the degrade watchdog
// (refreshRendezvousDegrade) gates on rendezvousWarpEngaged() — the
// AutoWarp coast actually running — which an unplanned arm never
// reaches (TestUnplannedAgreement_MutualDoesNotStartCoast). It must stay
// silent across ticks even while mutually armed.
func TestRefreshRendezvousDegrade_UnplannedNeverTrips(t *testing.T) {
	w, primary, st := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, true) {
		t.Fatal("Engage should succeed")
	}
	peer := peerAt(w, primary, st, 3, orbital.Vec3{X: 50_000}, orbital.Vec3{}, "gern")
	peer.ArmedTowardViewer = true

	for i := 0; i < 5; i++ {
		w.DriveRendezvousWarp([]CoWarpPeer{peer})
	}
	if w.RendezvousDegraded {
		t.Error("RendezvousDegraded = true for an unplanned agreement — there is no τ to predict an approach at")
	}
	if w.RendezvousApproachM != 0 {
		t.Errorf("RendezvousApproachM = %v, want 0 (never computed while unplanned)", w.RendezvousApproachM)
	}
	if !w.RendezvousUnplanned() {
		t.Fatal("precondition: still unplanned after driving")
	}
}
