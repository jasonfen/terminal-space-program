package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLandedCraftStaysOnSurfaceUnderWarp — regression for the
// v0.9.2 playtest bug. Pre-fix, a launchpad-spawned craft had
// V = ω × r and was integrated as a free orbital body; warp time
// and the integrator flew it along a fictitious orbit whose
// periapsis sat below Earth's center. With Landed=true the
// integrator bypasses gravity / drag / thrust and just rotates R
// about world +Z; |R| should remain at exactly the primary's
// mean radius after any amount of warp.
func TestLandedCraftStaysOnSurfaceUnderWarp(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("launchpad spawn should set Landed=true")
	}
	primaryR := c.Primary.RadiusMeters()
	startR := c.State.R.Norm()
	if math.Abs(startR-primaryR) > 1.0 {
		t.Fatalf("setup: |R| = %.0f, want %.0f", startR, primaryR)
	}

	// Advance sim time 6 hours and re-integrate. With Landed bypass,
	// R is regenerated from (LaunchLatDeg, LaunchLonDeg, simTime)
	// each tick, so |R| stays at primary radius regardless of how
	// far time advances.
	w.Clock.SimTime = w.Clock.SimTime.Add(6 * time.Hour)
	integrateLanded(w, c, time.Hour)
	endR := c.State.R.Norm()
	if math.Abs(endR-primaryR) > 1.0 {
		t.Errorf("after 6 hours of sim time, |R| = %.0f, want %.0f (within 1 m)",
			endR, primaryR)
	}
}

// TestLandedCraftStaysAtLaunchSiteUnderWarp — after a long warp
// window, the Landed craft's body-fixed (lat, lon) should be
// unchanged (= the launch site). World-frame R changes because the
// body has rotated, but the body-fixed coords are invariant.
func TestLandedCraftStaysAtLaunchSiteUnderWarp(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        28.6083,
		LongitudeOffset: -80.604,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if c.LaunchLatDeg != 28.6083 || c.LaunchLonDeg != -80.604 {
		t.Errorf("launch site stored on craft: got (%.4f, %.4f), want (28.6083, -80.604)",
			c.LaunchLatDeg, c.LaunchLonDeg)
	}
	// Advance sim time 12 hours; body has rotated ~half its sidereal
	// period. World R is different, but the body-fixed coords stay
	// at the launch site (the integrator regenerates from those).
	w.Clock.SimTime = w.Clock.SimTime.Add(12 * time.Hour)
	integrateLanded(w, c, time.Hour)
	if c.LaunchLatDeg != 28.6083 || c.LaunchLonDeg != -80.604 {
		t.Errorf("launch site mutated after 12h warp: (%.4f, %.4f)", c.LaunchLatDeg, c.LaunchLonDeg)
	}
	primaryR := c.Primary.RadiusMeters()
	if math.Abs(c.State.R.Norm()-primaryR) > 1.0 {
		t.Errorf("|R| = %.0f, want %.0f after 12h warp", c.State.R.Norm(), primaryR)
	}
}

// TestEngineIgnitionClearsLanded — pressing `b` to start a manual
// burn must clear the Landed flag so the integrator picks up
// normal physics on the next tick.
func TestEngineIgnitionClearsLanded(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	c := w.ActiveCraft()
	if !c.Landed {
		t.Fatal("setup: launchpad spawn should set Landed=true")
	}
	c.Throttle = 1.0
	c.AttitudeMode = spacecraft.BurnRadialOut
	w.StartManualBurn()
	if c.Landed {
		t.Error("StartManualBurn should clear Landed; got Landed=true")
	}
	if c.ManualBurn == nil {
		t.Error("StartManualBurn should engage ManualBurn")
	}
}
