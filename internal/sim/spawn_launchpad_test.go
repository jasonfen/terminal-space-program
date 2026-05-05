package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/render"
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

// TestSpawnLaunchpadAlignsWithTexture — regression for the v0.9.2
// playtest bug: spawning at "Cape Canaveral" didn't visually line
// up with Florida on the rendered Earth because the spawn used a
// different coordinate system (world +Z spin, Unix epoch) than the
// renderer (tilted axis, J2000 epoch, per-body texture offset).
// After the fix, the spawn point fed back through the renderer's
// SubObserverPointDeg should reconstruct the same (lat, lon).
func TestSpawnLaunchpadAlignsWithTexture(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	const (
		wantLat = 28.6083
		wantLon = -80.604
	)
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        wantLat,
		LongitudeOffset: wantLon,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	// Reverse-project R through the renderer to recover (lat, lon).
	camDir := render.Vec3{X: c.State.R.X, Y: c.State.R.Y, Z: c.State.R.Z}
	gotLat, gotLon := render.SubObserverPointDeg(c.Primary, w.Clock.SimTime, camDir, render.Vec3{})
	if math.Abs(gotLat-wantLat) > 0.01 {
		t.Errorf("recovered lat: got %.4f, want %.4f", gotLat, wantLat)
	}
	if math.Abs(gotLon-wantLon) > 0.01 {
		t.Errorf("recovered lon: got %.4f, want %.4f", gotLon, wantLon)
	}
}

// Note: TestSpawnLaunchpadKSCLatitude and
// TestSpawnLaunchpadCapeCanaveralLongitudePinned (pre-fix-3) both
// asserted Z = R · sin(lat) — true only when the spin axis is world
// +Z. v0.9.2 fix-3 swapped the spawn over to the renderer's tilted
// spin axis (so spawn lines up with the texture's continents); the
// flat-Z assertion no longer holds. Both tests are subsumed by
// TestSpawnLaunchpadAlignsWithTexture above, which round-trips the
// spawn position through the renderer and confirms the (lat, lon)
// reconstructs correctly.
