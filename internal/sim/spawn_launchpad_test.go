package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSpawnLaunchpadAtAltitudeZero — a launchpad spawn at the
// default latitude must put the craft at altitude 0 (|R| within a
// few metres of the primary's mean radius — surface co-rotation
// uses an exact spherical model).
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
	rNorm := c.State.R.Sub(w.BodyPosition(c.Primary)).Norm()
	if math.Abs(rNorm-primaryR) > 1.0 {
		t.Errorf("launchpad |R - primary_pos| = %.3f m, want %.3f m (within 1 m)",
			rNorm, primaryR)
	}
}

// TestSpawnLaunchpadCoRotatesWithSurface — the spawn velocity
// minus the body's heliocentric velocity should equal the surface
// co-rotation contribution ω×r. At KSC (28.6°N) on Earth, that's
// ~407 m/s (= 465 m/s · cos(28.6°) at the equator scaled by
// the latitude cosine). Sub-meter tolerance is fine.
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
	bodyVel := w.bodyInertialVelocity(c.Primary)
	surfaceV := c.State.V.Sub(bodyVel)
	primaryR := c.Primary.RadiusMeters()
	// At the equator on Earth (sidereal period ~23.93h), surface
	// speed is 2π · R / T ≈ 465 m/s.
	periodSec := c.Primary.SideralRotation * 3600
	wantSpeed := 2 * math.Pi * primaryR / periodSec
	gotSpeed := surfaceV.Norm()
	if math.Abs(gotSpeed-wantSpeed) > 1.0 {
		t.Errorf("equatorial co-rotation speed: got %.1f m/s, want %.1f m/s",
			gotSpeed, wantSpeed)
	}
}

// TestSpawnLaunchpadKSCLatitude — spawn at 28.6°N (the form's
// default) puts the craft on a circle at the right Z offset.
// Confirms latitude is interpreted as documented (degrees north,
// trigonometric sin/cos in the body equatorial frame).
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
	bodyPos := w.BodyPosition(c.Primary)
	rRel := c.State.R.Sub(bodyPos)
	primaryR := c.Primary.RadiusMeters()
	// sin(28.6°) ≈ 0.479; Z component of rRel = R · sin(lat).
	wantZ := primaryR * math.Sin(28.6*math.Pi/180)
	if math.Abs(rRel.Z-wantZ) > 1.0 {
		t.Errorf("Z offset for 28.6° N: got %.0f m, want %.0f m (≈ R · sin 28.6°)",
			rRel.Z, wantZ)
	}
}
