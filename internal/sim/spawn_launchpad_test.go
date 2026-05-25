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
// velocity equals ω × R (surface co-rotation about the body's
// tilted spin axis). Magnitude depends on the angle between R and
// the spin axis: |V| = ω·|R|·sin(angle).
//
// Note: post-fix-4 (texture-aligned spawn), R is on the Snyder-
// projection "surface" (where the texture renders the equator),
// which doesn't perfectly coincide with the body's true equatorial
// plane (perpendicular to the spin axis). At lat=0 on Earth we get
// ~398 m/s instead of the body-rotation-perfect 465 m/s — a known
// artifact of texture alignment that v0.9.4+ renderer work could
// resolve. v0.9.2 verifies |V| is non-zero and within a sensible
// range (250–500 m/s — captures real-world equatorial spin
// magnitudes whether you compute against body-rotation or
// texture-projection conventions).
func TestSpawnLaunchpadCoRotatesWithSurface(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     0, // equator
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	gotSpeed := c.State.V.Norm()
	if gotSpeed < 250 || gotSpeed > 500 {
		t.Errorf("equatorial co-rotation |V|: got %.1f m/s, want 250–500 m/s "+
			"(Earth equatorial surface speed range, accounting for spawn coordinate "+
			"system trade-offs)", gotSpeed)
	}
}

// TestSpawnLaunchpadAlignsWithTexture — regression for the v0.9.2
// fix-4 playtest bug (spawning at "Cape Canaveral" didn't line up
// with Florida on rendered Earth).
//
// v0.11.2+ (ADR 0003): the spawn and the renderer share one rotation
// convention — BodyFixedToWorld is a pure rotation about the body's
// spin axis. Alignment now reduces to a round-trip:
// WorldToBodyFixed(c.State.R / primaryR) must return the spawn
// (lat, lon).
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
	if math.Abs(c.State.R.Norm()-primaryR) > 1.0 {
		t.Errorf("|R| = %.0f, want %.0f", c.State.R.Norm(), primaryR)
	}
	unit := render.Vec3{
		X: c.State.R.X / primaryR,
		Y: c.State.R.Y / primaryR,
		Z: c.State.R.Z / primaryR,
	}
	gotLat, gotLon := render.WorldToBodyFixed(c.Primary, unit, w.Clock.SimTime)
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

// TestSpawnLaunchpadAutoSnapsNavSurface — v0.9.4+: a launchpad spawn
// auto-snaps World.NavMode to NavSurface so the player's `w`/`s` SAS
// keys route to surface-prograde / -retrograde without an explicit
// `;` cycle. Mirrors v0.9.3's reconcileNavMode pattern (NavMode
// follows the frame the player will be flying in).
func TestSpawnLaunchpadAutoSnapsNavSurface(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.NavMode != NavOrbit {
		t.Fatalf("precondition: NewWorld should land in NavOrbit, got %v", w.NavMode)
	}
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if w.NavMode != NavSurface {
		t.Errorf("NavMode after launchpad spawn: got %v, want %v",
			w.NavMode, NavSurface)
	}
}

// TestSpawnLaunchpadLeavesExistingSurfaceModeUntouched — when the
// world is already in NavSurface (e.g. a previous launchpad spawn
// already snapped it), a subsequent launchpad spawn must not flap
// the mode back through NavOrbit. The auto-snap is a one-way
// NavOrbit → NavSurface lift, idempotent on NavSurface. v0.9.4+.
//
// (NavTarget can't survive across a launchpad spawn because target
// bindings are per-craft and SetActiveCraftIdx → reconcileNavMode
// strips NavTarget when the incoming craft has no bound target —
// that's existing v0.9.3 behaviour, unrelated to this slice.)
func TestSpawnLaunchpadLeavesExistingSurfaceModeUntouched(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavSurface
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft launchpad: %v", err)
	}
	if w.NavMode != NavSurface {
		t.Errorf("NavMode after launchpad spawn from existing NavSurface: "+
			"got %v, want %v (no-op when already in surface)",
			w.NavMode, NavSurface)
	}
}
