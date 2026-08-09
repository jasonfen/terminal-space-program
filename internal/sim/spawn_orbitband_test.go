package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSpawnCraftClampsBelowFloorToFloor — ADR 0044 S3: an orbit-placement
// SpawnCraft call below the primary's Orbit Floor lands exactly at the
// floor (never refused), and the resulting orbital radius reflects it.
func TestSpawnCraftClampsBelowFloorToFloor(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	earth := *sys.FindBody("earth")
	band := OrbitBandFor(sys, earth)

	requested := band.FloorM - 100_000 // comfortably below Earth's floor
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		AltitudeM:    requested,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	gotAlt := c.State.R.Norm() - c.Primary.RadiusMeters()
	if math.Abs(gotAlt-band.FloorM) > 1.0 {
		t.Errorf("altitude = %.1f, want exactly the floor %.1f (requested %.1f)", gotAlt, band.FloorM, requested)
	}
}

// TestSpawnCraftClampsAboveCeilingToCeiling — the ceiling end of the same
// rule: a SpawnCraft call above the primary's Orbit Ceiling lands exactly
// at the ceiling. Uses the Moon, which (unlike Earth) has a ceiling well
// within reach of a plausible "too high" request.
func TestSpawnCraftClampsAboveCeilingToCeiling(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := *sys.FindBody("moon")
	band := OrbitBandFor(sys, moon)
	if !band.HasCeiling {
		t.Fatalf("test premise broken: moon band has no ceiling")
	}

	requested := band.CeilingM + 5_000_000 // comfortably above the Moon's ceiling
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "moon",
		AltitudeM:    requested,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	gotAlt := c.State.R.Norm() - c.Primary.RadiusMeters()
	if math.Abs(gotAlt-band.CeilingM) > 1.0 {
		t.Errorf("altitude = %.1f, want exactly the ceiling %.1f (requested %.1f)", gotAlt, band.CeilingM, requested)
	}
}

// TestSpawnCraftInBandAltitudeUntouched — the common case: a requested
// altitude that's already inside the band must not be nudged at all.
func TestSpawnCraftInBandAltitudeUntouched(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	const requested = 400_000.0 // well inside Earth's band
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		AltitudeM:    requested,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	gotAlt := c.State.R.Norm() - c.Primary.RadiusMeters()
	if math.Abs(gotAlt-requested) > 1.0 {
		t.Errorf("altitude = %.1f, want exactly the requested %.1f (in-band spawn must not be nudged)", gotAlt, requested)
	}
}

// TestSpawnCraftAtEmptyBandBodyErrorsWithoutMutatingSlate — Phobos has no
// legal orbit altitude at all (its SOI sits inside its own surface, ADR
// 0044 §6). SpawnCraft must refuse with a descriptive error rather than
// silently placing the craft somewhere else, and — critically — must not
// have appended a half-built craft to the slate before failing.
func TestSpawnCraftAtEmptyBandBodyErrorsWithoutMutatingSlate(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	before := len(w.Crafts)
	activeBefore := w.ActiveCraft()

	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "phobos",
		AltitudeM:    50_000,
	})
	if err == nil {
		t.Fatalf("SpawnCraft at phobos succeeded, want an error (no legal orbit altitude exists)")
	}
	if c != nil {
		t.Errorf("SpawnCraft returned a non-nil craft alongside an error: %+v", c)
	}
	if len(w.Crafts) != before {
		t.Errorf("slate length changed on a failed spawn: %d -> %d (half-spawn leaked in)", before, len(w.Crafts))
	}
	if w.ActiveCraft() != activeBefore {
		t.Errorf("active craft changed on a failed spawn")
	}
}

// TestSpawnLaunchpadIgnoresOrbitBand — regression guard: the Orbit Band
// clamp must live only in the orbit-placement branch. Phobos has an Empty
// band (no legal orbit at all), but a launchpad spawn is a surface spawn
// at altitude 0 by definition and must succeed there regardless.
func TestSpawnLaunchpadIgnoresOrbitBand(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "phobos",
		Launchpad:    true,
	})
	if err != nil {
		t.Fatalf("launchpad spawn at phobos (an Empty orbit band body) failed: %v", err)
	}
	if c.Primary.ID != "phobos" {
		t.Errorf("primary = %q, want phobos", c.Primary.ID)
	}
	if alt := c.State.R.Norm() - c.Primary.RadiusMeters(); math.Abs(alt) > 1.0 {
		t.Errorf("launchpad altitude = %.1f, want 0", alt)
	}
}

// TestSpawnAlongsideIgnoresOrbitBand — regression guard for the other
// exempt branch: Alongside clones the active craft's own state verbatim.
// A clamp leaking into this path would visibly displace the clone by
// kilometers; the ~25 m docking offset must be all that moves it.
func TestSpawnAlongsideIgnoresOrbitBand(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	if active == nil {
		t.Fatal("no active craft to clone")
	}
	wantR := active.State.R.Norm()

	c, err := w.SpawnCraft(SpawnSpec{Alongside: true})
	if err != nil {
		t.Fatalf("SpawnCraft(Alongside): %v", err)
	}
	gotR := c.State.R.Norm()
	if math.Abs(gotR-wantR) > 100 { // docking offset is 25 m; any clamp would move it by km
		t.Errorf("alongside spawn radius = %.1f, want ~%.1f (the active craft's own, unclamped)", gotR, wantR)
	}
}
