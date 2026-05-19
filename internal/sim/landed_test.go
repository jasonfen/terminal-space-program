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

// TestLandedNoseTracksLocalUpThroughWarp — regression for the v0.10.0
// slew bug the player hit: warping on the pad (watching inclination)
// then pressing `b` would not lift off. Cause: the slew integrator is
// skipped while Landed, so CurrentAttitudeDir stayed frozen at the
// spawn-time radial-out vector while the craft co-rotated; on liftoff
// the engine thrust followed that stale (now sub-horizon) nose.
// Fix: integrateLanded keeps CurrentAttitudeDir synced to the
// commanded direction so the nose co-rotates with the pad.
func TestLandedNoseTracksLocalUpThroughWarp(t *testing.T) {
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
	if !c.Landed {
		t.Fatal("setup: launchpad spawn should set Landed=true")
	}
	spawnNose := c.CurrentAttitudeDir
	if a := slewAngle(spawnNose, c.State.R.Unit()); a > 1e-3 {
		t.Fatalf("setup: spawn nose not radial-out (%.4f rad off)", a)
	}

	// Warp on the pad: the body rotates, local up moves.
	w.Clock.SimTime = w.Clock.SimTime.Add(6 * time.Hour)
	integrateLanded(w, c, time.Hour)
	upNow := c.State.R.Unit()
	if a := slewAngle(spawnNose, upNow); a < 5*math.Pi/180 {
		t.Fatalf("setup: body did not rotate enough to exercise the bug (%.2f°)",
			a*180/math.Pi)
	}
	// The nose must track the *current* local up, not the stale
	// spawn vector.
	if a := slewAngle(c.CurrentAttitudeDir, upNow); a > 1e-3 {
		t.Errorf("landed nose did not track local up through warp: %.3f° off",
			a*180/math.Pi)
	}

	// Liftoff: with the nose correct the craft must actually rise.
	alt0 := c.Altitude()
	w.StartManualBurn()
	for i := 0; i < 60; i++ { // ~3 s at 1× / 50 ms base step
		w.Tick()
	}
	if c.Altitude() <= alt0+1.0 {
		t.Errorf("craft did not lift off the pad: altitude %.2f → %.2f m",
			alt0, c.Altitude())
	}
	if vUp := c.State.V.Dot(c.State.R.Unit()); vUp <= 0 {
		t.Errorf("post-ignition vertical velocity not positive: %.3f m/s", vUp)
	}
}
