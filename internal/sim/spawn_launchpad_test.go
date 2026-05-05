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
// fix-4 playtest bug: spawning at "Cape Canaveral" didn't visually
// line up with Florida on the rendered Earth.
//
// The spawn now uses Snyder forward orthographic projection (the
// inverse of the texture pipeline's projectPixelToLatLon) at
// ViewTop's sub-observer point. Verifying alignment: the spawn's
// world R, projected through ViewTop's canvas basis, gives screen
// (nx, ny) which when fed through the texture pipeline's inverse
// returns the original (lat, lon). We verify this by replicating
// Snyder forward+inverse manually and asserting (nx, ny, z) round-
// trip back to (lat, lon).
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
	primaryR := c.Primary.RadiusMeters()
	// |R| should equal the primary's mean radius (altitude 0).
	if math.Abs(c.State.R.Norm()-primaryR) > 1.0 {
		t.Errorf("|R| = %.0f, want %.0f", c.State.R.Norm(), primaryR)
	}
	// For ViewTop, canvas X = world+X, canvas Y = world+Y, depth = world+Z.
	// Screen (nx, ny) for the spawn = (R.X / radius, R.Y / radius).
	subLatDeg, subLonDeg := render.SubObserverPointDeg(c.Primary, w.Clock.SimTime, render.CameraDirTop, render.Vec3{})
	nx := c.State.R.X / primaryR
	ny := c.State.R.Y / primaryR
	z := c.State.R.Z / primaryR
	// Snyder inverse should reconstruct (wantLat, wantLon) from (nx, ny, z).
	phi0 := subLatDeg * math.Pi / 180
	lam0 := subLonDeg * math.Pi / 180
	sP0, cP0 := math.Sin(phi0), math.Cos(phi0)
	sL0, cL0 := math.Sin(lam0), math.Cos(lam0)
	bodyZ := sP0*z + cP0*ny
	bodyX := cP0*cL0*z - sL0*nx - sP0*cL0*ny
	bodyY := cP0*sL0*z + cL0*nx - sP0*sL0*ny
	gotLat := math.Asin(bodyZ) * 180 / math.Pi
	gotLon := math.Atan2(bodyY, bodyX) * 180 / math.Pi
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
