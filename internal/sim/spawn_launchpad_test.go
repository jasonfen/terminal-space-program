package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSpawnLaunchpadPrimaryStaysEarth — regression for the
// v0.9.2-fix-1 bug: pre-fix surfaceSpawnPosVel returned heliocentric
// coords into c.State.R, the SOI walker saw a craft at ~2× Earth's
// heliocentric distance, and re-parented it to Sun. Verify the
// spawned craft's primary stays as the requested body.
func TestSpawnLaunchpadPrimaryStaysEarth(t *testing.T) {
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
	if c.Primary.ID != "earth" {
		t.Errorf("primary after launchpad spawn: got %q, want %q", c.Primary.ID, "earth")
	}
}

// TestSpawnLaunchpadAtAltitudeZero — a launchpad spawn at the
// default latitude must put the craft at altitude 0. c.State.R is
// primary-relative; |R| should equal the primary's mean radius.
func TestSpawnLaunchpadAtAltitudeZero(t *testing.T) {
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
	primaryR := c.Primary.RadiusMeters()
	rNorm := c.State.R.Norm()
	if math.Abs(rNorm-primaryR) > 1.0 {
		t.Errorf("launchpad |R| = %.3f m, want %.3f m (within 1 m)", rNorm, primaryR)
	}
	// Altitude helper should also report ~0 m AGL.
	if alt := c.Altitude(); math.Abs(alt) > 1.0 {
		t.Errorf("c.Altitude() = %.3f m, want ≈ 0 (within 1 m)", alt)
	}
}

// TestSpawnLaunchpadCoRotatesWithSurface — the primary-relative
// velocity equals ω × r (surface co-rotation). At the equator on
// Earth, that's ~465 m/s (sidereal period ~23.93h, R ~6371 km).
func TestSpawnLaunchpadCoRotatesWithSurface(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     0, // equator — easiest case to sanity-check
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	primaryR := c.Primary.RadiusMeters()
	periodSec := c.Primary.SideralRotation * 3600
	wantSpeed := 2 * math.Pi * primaryR / periodSec
	gotSpeed := c.State.V.Norm()
	if math.Abs(gotSpeed-wantSpeed) > 1.0 {
		t.Errorf("equatorial co-rotation |V| (primary-relative): got %.1f m/s, want %.1f m/s",
			gotSpeed, wantSpeed)
	}
}

// TestSpawnLaunchpadKSCLatitude — spawn at 28.6°N puts the craft
// on a sphere at the right Z offset. Confirms latitude is
// interpreted as documented (degrees north, trigonometric sin/cos
// in the body equatorial frame).
func TestSpawnLaunchpadKSCLatitude(t *testing.T) {
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
	primaryR := c.Primary.RadiusMeters()
	// sin(28.6°) ≈ 0.479; Z component of c.State.R = R · sin(lat).
	wantZ := primaryR * math.Sin(28.6*math.Pi/180)
	if math.Abs(c.State.R.Z-wantZ) > 1.0 {
		t.Errorf("Z offset for 28.6° N: got %.0f m, want %.0f m (≈ R · sin 28.6°)",
			c.State.R.Z, wantZ)
	}
}
