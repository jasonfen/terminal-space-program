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
