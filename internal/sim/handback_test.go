package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSafeHandbackLeavesNothingArmed (#303): a returned craft must not wear
// the other pilot's flight state. The measured failure was a craft coming back
// at 10% throttle on RCS holding Target Retrograde, which the pilot had left
// at 100%/main/prograde — "ignite main and burn" became a silent no-op metres
// from a stack.
func TestSafeHandbackLeavesNothingArmed(t *testing.T) {
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Throttle = 0.1
	c.EngineMode = spacecraft.EngineRCS
	c.AttitudeMode = spacecraft.BurnTargetRetrograde
	c.RCSFineLevel = 2
	c.ManualBurn = &spacecraft.ManualBurn{}

	SafeHandback(c)

	if c.Throttle != 0 {
		t.Errorf("throttle = %v, want 0", c.Throttle)
	}
	if c.EngineMode != spacecraft.EngineMain {
		t.Errorf("engine mode = %v, want main", c.EngineMode)
	}
	if c.AttitudeMode != spacecraft.BurnPrograde {
		t.Errorf("attitude hold = %v, want the neutral default", c.AttitudeMode)
	}
	if c.RCSFineLevel != 0 {
		t.Errorf("RCS fine level = %v, want coarse", c.RCSFineLevel)
	}
	if c.ManualBurn != nil || c.ActiveBurn != nil {
		t.Errorf("a burn survived the handback")
	}
	SafeHandback(nil) // must not panic
}

// TestSeparationPushClearsBothDockingGates (#304): the cross-player return
// path had no push at all, so the pair silently re-fused nine seconds after
// undocking. The push has to beat the proximity gate AND the closing-rate
// gate, or near-identical orbits wander back into the box.
func TestSeparationPushClearsBothDockingGates(t *testing.T) {
	w := testWorld(t)
	c := w.ActiveCraft()
	r0, v0 := c.State.R, c.State.V

	SeparationPush(c)

	if got := c.State.R.Sub(r0).Norm(); got <= DockingDistM {
		t.Errorf("separation = %v m, want more than the %v m proximity gate", got, DockingDistM)
	}
	if got := c.State.V.Sub(v0).Norm(); got <= DockingVMS {
		t.Errorf("closing rate = %v m/s, want more than the %v m/s gate", got, DockingVMS)
	}
	SeparationPush(nil) // must not panic
}

// TestPlaceAcrossSubspaceGapCoastsBothWays: a craft handed across a subspace
// gap is propagated to the RECEIVER's sim-time, forward or backward, and a
// round trip returns it where it started. Zero gap is a no-op — a live
// handback at the seam must land exactly at the seam.
func TestPlaceAcrossSubspaceGapCoastsBothWays(t *testing.T) {
	w := testWorld(t)
	c := w.ActiveCraft()
	r0, v0 := c.State.R, c.State.V

	if !PlaceAcrossSubspaceGap(c, 0) {
		t.Fatalf("zero-gap placement failed")
	}
	if c.State.R != r0 || c.State.V != v0 {
		t.Errorf("zero gap moved the craft: %v/%v != %v/%v", c.State.R, c.State.V, r0, v0)
	}

	if !PlaceAcrossSubspaceGap(c, 600) {
		t.Fatalf("forward placement failed")
	}
	if c.State.R.Sub(r0).Norm() < 1000 {
		t.Errorf("ten minutes of coasting moved the craft only %v m", c.State.R.Sub(r0).Norm())
	}
	if !PlaceAcrossSubspaceGap(c, -600) {
		t.Fatalf("backward placement failed")
	}
	if c.State.R.Sub(r0).Norm() > 1 {
		t.Errorf("round trip landed %v m from the start", c.State.R.Sub(r0).Norm())
	}
}

// TestGuestReleaseRefusalIsExhaustive: the owner seat's release predicate
// covers every no it can give, each naming the situation and — where there is
// one — the way out. UndockRefusal's discipline (#308), applied to the verb
// ADR 0040 §3 introduces.
func TestGuestReleaseRefusalIsExhaustive(t *testing.T) {
	w := testWorld(t)
	if why := w.GuestReleaseRefusal(-1); why == "" {
		t.Errorf("no vessel selected: want a refusal")
	}
	if why := w.GuestReleaseRefusal(0); why == "" {
		t.Errorf("a plain craft carries nobody: want a refusal")
	}

	// A real cross-player stack, guest components on top: allowed.
	guest := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	guest.Primary = w.ActiveCraft().Primary
	guest.State = w.ActiveCraft().State
	guest.ID = 501
	w.AdoptCraft(guest, false)
	if _, idx, ok := w.DockGuestCraft(0, guest, "SHA256:gern"); !ok {
		t.Fatalf("DockGuestCraft refused")
	} else if why := w.GuestReleaseRefusal(idx); why != "" {
		t.Errorf("a top-anchored guest block refused: %q", why)
	}
}

func testWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}
