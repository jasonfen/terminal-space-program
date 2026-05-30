// Package screens — v0.12 Slice 3 CHUTE HUD tests (ADR 0008).
//
// Descent under a parachute happens in OrbitView (ADR 0004 deferred a
// dedicated ViewLanding), so the HUD is the player's only window onto
// the chute: the CHUTE block surfaces the deploy state (STOWED / ARMED
// / DEPLOYED) and the descent rate so the player can watch terminal
// velocity settle under V_CRIT before contact.

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// placeCapsuleOnEarth parks a re-entry capsule descending toward Earth
// at the given altitude with a downward (radially-inward) velocity.
func placeCapsuleOnEarth(t *testing.T, w *sim.World, altM, vDown float64) *spacecraft.Spacecraft {
	t.Helper()
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutCapsuleID)
	// Reuse the default active craft's Earth primary.
	c.Primary = w.ActiveCraft().Primary
	c.State = physics.StateVector{
		R: orbital.Vec3{X: c.Primary.RadiusMeters() + altM},
		V: orbital.Vec3{X: -vDown},
		M: c.TotalMass(),
	}
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	return c
}

// TestShouldShowChuteHUDForCapsule — a chute-bearing capsule in flight
// shows the block; a Landed or Crashed one, or a non-chute craft, does
// not.
func TestShouldShowChuteHUDForCapsule(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := placeCapsuleOnEarth(t, w, 8_000, 50)
	if !shouldShowChuteHUD(c) {
		t.Errorf("capsule in flight: shouldShowChuteHUD=false, want true")
	}
	c.Landed = true
	if shouldShowChuteHUD(c) {
		t.Errorf("Landed capsule: shouldShowChuteHUD=true, want false")
	}
	c.Landed = false
	c.Crashed = true
	if shouldShowChuteHUD(c) {
		t.Errorf("Crashed capsule: shouldShowChuteHUD=true, want false")
	}
	// A craft without the capability never shows the block.
	c.Crashed = false
	c.HasParachute = false
	if shouldShowChuteHUD(c) {
		t.Errorf("non-chute craft: shouldShowChuteHUD=true, want false")
	}
}

// TestChuteHUDRendersStateAndDescentRate — the rendered block carries
// the CHUTE header, the deploy-state word, and a descent-rate readout.
func TestChuteHUDRendersStateAndDescentRate(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := placeCapsuleOnEarth(t, w, 6_000, 7) // 7 m/s descent, under V_CRIT
	c.ChuteState = spacecraft.ChuteDeployed
	view := NewOrbitView(descentHUDTheme())
	out := view.Render(w, 0, 200, 60)
	if !strings.Contains(out, "CHUTE") {
		t.Errorf("expected CHUTE section header; got:\n%s", out)
	}
	if !strings.Contains(out, "DEPLOYED") {
		t.Errorf("expected DEPLOYED state word in CHUTE block; got:\n%s", out)
	}
	if !strings.Contains(out, "descent rate:") {
		t.Errorf("expected descent-rate readout; got:\n%s", out)
	}
}

// TestChuteHUDAlertsAboveVCrit — a chute craft still descending faster
// than V_CRIT gets a crash-on-contact alert so the player knows the
// canopy hasn't bled off enough speed yet.
func TestChuteHUDAlertsAboveVCrit(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := placeCapsuleOnEarth(t, w, 6_000, 60) // 60 m/s > V_CRIT
	c.ChuteState = spacecraft.ChuteArmed
	view := NewOrbitView(descentHUDTheme())
	out := view.Render(w, 0, 200, 60)
	if !strings.Contains(out, "ARMED") {
		t.Errorf("expected ARMED state word; got:\n%s", out)
	}
	if !strings.Contains(out, "CRASH on contact") {
		t.Errorf("expected crash-on-contact alert above V_CRIT; got:\n%s", out)
	}
}
