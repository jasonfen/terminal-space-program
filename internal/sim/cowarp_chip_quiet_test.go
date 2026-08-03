package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// ADR 0037 §4: one chip per REAL change. The per-tick couple state inside
// a standing agreement is never chip-worthy — which erases #275's
// structural flap by construction rather than by tuning constants apart.
//
// The flap it kills: an engaged coast's leader-hold drives the partner's
// reported Effective warp to 0, the EffWarp>0 gate uncouples them
// ("released"), the hold lifts and they couple again — 852 coupled /
// released moments in one live session.
func TestAgreementCoupleStateNeverChips(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarpAs("SHA256:gern", "gern", tau, 6000, true)
	near := peerAt(w, primary, st, 50, orbital.Vec3{X: 6000}, orbital.Vec3{X: 594}, "gern")
	near.ArmedTowardViewer, near.RendezvousInitiator = true, false
	near.RendezvousTau = tau
	w.DriveRendezvousWarp([]CoWarpPeer{near})

	res := w.ComputeCoWarp([]CoWarpPeer{near}, nil)
	if !res.State.Coupled {
		t.Fatal("precondition: the agreement coupled the pair")
	}
	if len(res.NewlyCoupled) != 0 {
		t.Errorf("the agreement forming chipped a couple moment: %v — the arm's own moments already say it", res.NewlyCoupled)
	}

	// The partner's report goes to 0 (their leader-hold) and comes back:
	// the exact #275 flap. Neither tick may chip.
	prev := res.CoupledOwners
	near.EffWarp = 0
	res = w.ComputeCoWarp([]CoWarpPeer{near}, prev)
	if !res.State.Coupled {
		t.Error("a held partner's 0× report uncoupled the agreement — the flap's first half")
	}
	if len(res.Released) != 0 {
		t.Errorf("held partner chipped a release: %v", res.Released)
	}
	prev = res.CoupledOwners
	near.EffWarp = 50
	res = w.ComputeCoWarp([]CoWarpPeer{near}, prev)
	if len(res.NewlyCoupled) != 0 {
		t.Errorf("resuming partner chipped a couple: %v", res.NewlyCoupled)
	}
}

// A plain proximity lock still chips both ends — that IS a real change,
// and without an agreement it is the only announcement there is.
func TestPlainProximityLockStillChipsBothEnds(t *testing.T) {
	w, primary, st := anchorWorld(t)
	near := peerAt(w, primary, st, 50, orbital.Vec3{X: 5000}, orbital.Vec3{}, "bob")
	res := w.ComputeCoWarp([]CoWarpPeer{near}, nil)
	if len(res.NewlyCoupled) != 1 {
		t.Fatalf("NewlyCoupled = %v, want the couple moment", res.NewlyCoupled)
	}
	far := peerAt(w, primary, st, 50, orbital.Vec3{X: 500_000}, orbital.Vec3{}, "bob")
	res = w.ComputeCoWarp([]CoWarpPeer{far}, res.CoupledOwners)
	if len(res.Released) != 1 {
		t.Errorf("Released = %v, want the release moment", res.Released)
	}
}
