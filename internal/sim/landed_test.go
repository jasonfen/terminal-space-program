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

	// Run a long warp window — 6 hours at 1× sim time. With
	// gravity-only integration of a craft with V = ω×r, after
	// 6 hours the craft would be deep inside the primary on its
	// fictitious-orbit half-period. With Landed bypass, |R|
	// stays put.
	for i := 0; i < 6*3600; i++ {
		integrateLanded(c, time.Second)
	}
	endR := c.State.R.Norm()
	if math.Abs(endR-primaryR) > 1.0 {
		t.Errorf("after 6 hours of Landed integration, |R| = %.0f, want %.0f (within 1 m)",
			endR, primaryR)
	}
}

// TestLandedCraftCoRotatesUnderWarp — after one sidereal day,
// the Landed craft's R should have rotated 360° about Z (back
// to its starting longitude).
func TestLandedCraftCoRotatesUnderWarp(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     0, // equator — easiest to read X/Y
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	startR := c.State.R
	// One sidereal day = c.Primary.SideralRotation hours = 23.9345h.
	periodSec := c.Primary.SideralRotation * 3600
	integrateLanded(c, time.Duration(periodSec*float64(time.Second)))
	dx := c.State.R.X - startR.X
	dy := c.State.R.Y - startR.Y
	dz := c.State.R.Z - startR.Z
	// After exactly one rotation, R returns to starting position
	// (modulo float precision).
	if math.Abs(dx) > 1.0 || math.Abs(dy) > 1.0 || math.Abs(dz) > 1.0 {
		t.Errorf("after one sidereal day, R drift = (%.1f, %.1f, %.1f), want all ≈ 0", dx, dy, dz)
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
