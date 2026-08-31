package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestUnplannedAgreement_AccepterAdoptsInitiatorsLaterPlan is finding 1
// (batch review, HIGH): an accepter who joins a zero-τ ("agreed, no plan
// yet") agreement was stuck permanently — refreshRendezvousInvite never
// surfaces a fresh invite while w.RendezvousArm is non-nil, and nothing
// on the accepter's side ever writes arm.Tau once the arm exists
// (PlanMeetingBurn only touches the arm-holder's own arm). So when the
// INITIATOR later plants a Meeting Burn and re-Engages — the exact flow
// the chip itself prompts for ("no plan yet — pick a Meeting Place [K],
// then Engage to commit") — the accepter's own arm never adopted it and
// the coast never started for them.
//
// This drives the accepter's World through DriveRendezvousWarp with a
// peer report that now carries a real, future RendezvousTau (mirroring
// the initiator's post-replan relay report) and asserts the accepter's
// own arm adopts it and the shared coast starts.
func TestUnplannedAgreement_AccepterAdoptsInitiatorsLaterPlan(t *testing.T) {
	w, primary, st := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, false) {
		t.Fatal("Engage (as accepter) should succeed")
	}
	if w.RendezvousArm.Initiator {
		t.Fatal("precondition: accepter seat")
	}

	// First tick: partner armed back, but still no plan on either side —
	// must not start the coast (mirrors TestUnplannedAgreement_MutualDoesNotStartCoast).
	peer := peerAt(w, primary, st, 3, orbital.Vec3{X: 50_000}, orbital.Vec3{}, "gern")
	peer.ArmedTowardViewer = true
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast must not start before a plan lands")
	}

	// The initiator plants a Meeting Burn and re-Engages: their relayed
	// report now carries a real, future τ + committed CA + a Meeting
	// Place. Nothing about the ACCEPTER's own seat or arm identity
	// changes — only the partner's report.
	committedTau := w.Clock.SimTime.Add(3 * time.Hour)
	peer.RendezvousTau = committedTau
	peer.RendezvousCA = 750
	peer.RendezvousMeetingPlace = "LEO node"
	peer.RendezvousMeetingLaps = 2

	w.DriveRendezvousWarp([]CoWarpPeer{peer})

	if !w.rendezvousWarpEngaged() {
		t.Fatal("accepter never adopted the initiator's later plan — still stuck unplanned")
	}
	if w.RendezvousArm == nil {
		t.Fatal("arm cleared unexpectedly")
	}
	if !w.RendezvousArm.Tau.Equal(committedTau) {
		t.Errorf("arm.Tau = %v, want adopted %v", w.RendezvousArm.Tau, committedTau)
	}
	if w.RendezvousArm.CommittedCA != 750 {
		t.Errorf("arm.CommittedCA = %v, want 750 (adopted from partner)", w.RendezvousArm.CommittedCA)
	}
	if w.RendezvousArm.MeetingPlaceLabel != "LEO node" || w.RendezvousArm.MeetingLaps != 2 {
		t.Errorf("Meeting Place not adopted: label=%q laps=%d", w.RendezvousArm.MeetingPlaceLabel, w.RendezvousArm.MeetingLaps)
	}
	if w.RendezvousArm.Initiator {
		t.Error("adoption flipped the seat — accepter must stay accepter (ADR 0037 seat split)")
	}
	if w.AutoWarp == nil || !w.AutoWarp.Rendezvous || !w.AutoWarp.T.Equal(committedTau) {
		t.Fatalf("driver not started on the adopted τ: AutoWarp=%+v", w.AutoWarp)
	}
	if w.RendezvousUnplanned() {
		t.Error("RendezvousUnplanned() still true after adopting a real plan")
	}
}

// TestUnplannedAgreement_AccepterIgnoresSubMinLeadPartnerTau mirrors the
// min-τ adoption block's own min-lead floor (#252 review, finding 4):
// a partner τ inside rendezvousWaypointMinLead of now must not be
// adopted — it would be crossed on the very next tick, forcing an
// immediate spurious waypoint resolution.
func TestUnplannedAgreement_AccepterIgnoresSubMinLeadPartnerTau(t *testing.T) {
	w, primary, st := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, false) {
		t.Fatal("Engage (as accepter) should succeed")
	}
	peer := peerAt(w, primary, st, 3, orbital.Vec3{X: 50_000}, orbital.Vec3{}, "gern")
	peer.ArmedTowardViewer = true
	peer.RendezvousTau = w.Clock.SimTime.Add(1 * time.Second) // inside rendezvousWaypointMinLead (5s)

	w.DriveRendezvousWarp([]CoWarpPeer{peer})

	if w.rendezvousWarpEngaged() {
		t.Error("adopted a sub-min-lead partner τ — should have waited")
	}
	if !w.RendezvousArm.Tau.IsZero() {
		t.Errorf("arm.Tau = %v, want still zero", w.RendezvousArm.Tau)
	}
}
