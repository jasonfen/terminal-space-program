package sim

import (
	"testing"
	"time"
)

// TestRendezvousArmTracksPrevCommittedCAOnWaypointAdvance — ADR 0039 S3 /
// #281: the RENDEZVOUS chip needs to tell a converging waypoint sequence
// from a diverging one — a question the existing degrade watchdog can't
// answer, because its own baseline re-bases with the drift it's supposed
// to catch (RendezvousArm.degradeBaseCA's own doc comment: "the baseline
// moves with the divergence it should be flagging"). PrevCommittedCA
// captures the waypoint-before-last's committed CA at the moment
// resolveRendezvousWaypoint advances to a new one, so the chip can
// compare "was it better or worse last time" without touching the
// degrade lifetime rules at all.
func TestRendezvousArmTracksPrevCommittedCAOnWaypointAdvance(t *testing.T) {
	wa, _, sta := anchorWorld(t)
	wb, _, _ := anchorWorld(t)
	aheadAlongTrack(t, wb)
	tau := sta.Add(time.Hour)
	wa.EngageRendezvousWarp("SHA256:b", "b", tau, 5.4e6)
	wb.EngageRendezvousWarp("SHA256:a", "a", tau, 5.4e6)

	effA, effB := wa.EffectiveWarp(), wb.EffectiveWarp()
	prevA, prevB := map[string]bool{}, map[string]bool{}
	step := func() {
		pa := crossPeer(wb, "SHA256:b", "b", effB)
		pb := crossPeer(wa, "SHA256:a", "a", effA)
		wa.DriveRendezvousWarp(pa)
		wb.DriveRendezvousWarp(pb)
		ra, rb := wa.ComputeCoWarp(pa, prevA), wb.ComputeCoWarp(pb, prevB)
		wa.CoWarp, wb.CoWarp = ra.State, rb.State
		prevA, prevB = ra.CoupledOwners, rb.CoupledOwners
		effA, effB = wa.EffectiveWarp(), wb.EffectiveWarp()
	}
	step()
	if !wa.rendezvousWarpEngaged() {
		t.Fatal("precondition: shared coast engaged")
	}
	if wa.RendezvousArm.PrevCommittedCASet {
		t.Fatal("precondition: no waypoint advance has happened yet")
	}

	firstCA := wa.RendezvousArm.CommittedCA

	// Reach the committed encounter far outside couple range — advances
	// the waypoint rather than clearing the arm (mirrors
	// TestRendezvousArmSurvivesTauOutsideCoupleRange).
	wa.Clock.SimTime, wb.Clock.SimTime = tau, tau
	step()

	if wa.RendezvousArm == nil {
		t.Fatal("arm cleared at τ outside couple range")
	}
	if !wa.RendezvousArm.PrevCommittedCASet {
		t.Fatal("PrevCommittedCASet not raised after the first waypoint advance")
	}
	if wa.RendezvousArm.PrevCommittedCA != firstCA {
		t.Errorf("PrevCommittedCA = %.0f, want the pre-advance CommittedCA %.0f", wa.RendezvousArm.PrevCommittedCA, firstCA)
	}
}

// TestRendezvousArmTracksPrevCommittedCAOnMinTauAdoption — batch-review
// follow-up (PR #319): the min-τ adoption path in DriveRendezvousWarp
// (partner.RendezvousTau wins, ~auto_warp.go:534) overwrites CommittedCA
// exactly like resolveRendezvousWaypoint's own-derivation advance does,
// but — unlike that site — did not stamp PrevCommittedCA/
// PrevCommittedCASet before this fix. The codebase already treats
// adoption as a new waypoint everywhere else (the adjacent
// "degradeBaseSet = false // a new waypoint means a new baseline"), so
// the trend must too: leaving PrevCommittedCA stale here means the row
// can compare across TWO waypoint transitions instead of one, rendering
// "↘ shrinking" when the most recent transition actually grew — an
// actively misleading readout in the one feature whose whole job is
// telling the pilot honestly whether they are converging.
func TestRendezvousArmTracksPrevCommittedCAOnMinTauAdoption(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 900)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.RendezvousTau = tau
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}
	if w.RendezvousArm.PrevCommittedCASet {
		t.Fatal("precondition: no waypoint transition has happened yet")
	}
	preAdoptionCA := w.RendezvousArm.CommittedCA

	// Partner's earlier τ, past the min lead → adopted (mirrors
	// TestRendezvousMinTauAdoptionRespectsMinLead).
	adopt := st.Add(time.Minute)
	peer.RendezvousTau = adopt
	peer.RendezvousCA = 12_345
	w.DriveRendezvousWarp([]CoWarpPeer{peer})

	if !w.RendezvousArm.Tau.Equal(adopt) {
		t.Fatalf("precondition: adoption did not fire — arm.Tau = %v, want %v", w.RendezvousArm.Tau, adopt)
	}
	if w.RendezvousArm.CommittedCA != 12_345 {
		t.Fatalf("precondition: CommittedCA = %.0f, want the adopted 12345", w.RendezvousArm.CommittedCA)
	}
	if !w.RendezvousArm.PrevCommittedCASet {
		t.Fatal("PrevCommittedCASet not raised on min-τ adoption")
	}
	if w.RendezvousArm.PrevCommittedCA != preAdoptionCA {
		t.Errorf("PrevCommittedCA = %.0f, want the pre-adoption CommittedCA %.0f", w.RendezvousArm.PrevCommittedCA, preAdoptionCA)
	}
}
