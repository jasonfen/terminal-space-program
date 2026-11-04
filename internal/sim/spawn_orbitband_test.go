package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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

// TestSpawnCraftEmptyBandErrorHasNoRedundantPrefix — review finding #4:
// the Empty-band error must be exactly noOrbitNote's sentence, with no
// "spawn: " prefix layered on top of it. app.go wraps this error in its
// own "spawn failed: %v", so a "spawn: " prefix here would render as the
// doubled "spawn failed: spawn: Mars owns everything...".
func TestSpawnCraftEmptyBandErrorHasNoRedundantPrefix(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys, phobos := findBodyIn(t, w.Systems, "Sol", "phobos")

	_, spawnErr := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "phobos",
		AltitudeM:    50_000,
	})
	if spawnErr == nil {
		t.Fatalf("SpawnCraft at phobos succeeded, want an error")
	}
	want := noOrbitNote(sys, phobos)
	if spawnErr.Error() != want {
		t.Errorf("SpawnCraft error = %q, want exactly noOrbitNote's sentence %q (no spawn: prefix)", spawnErr.Error(), want)
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

// presetMeanAnomalyDeg is c's mean anomaly about its primary, degrees,
// read in the primary's equatorial frame.
func presetMeanAnomalyDeg(c *spacecraft.Spacecraft) float64 {
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	r, v := frame.FromWorld(c.State.R), frame.FromWorld(c.State.V)
	el := orbital.ElementsFromState(r, v, c.Primary.GravitationalParameter())
	px, py := orbital.PerifocalBasis(el)
	nu := math.Atan2(r.Dot(py), r.Dot(px))
	ea := 2 * math.Atan2(math.Sqrt(1-el.E)*math.Sin(nu/2), math.Sqrt(1+el.E)*math.Cos(nu/2))
	return math.Mod(ea-el.E*math.Sin(ea)+4*math.Pi, 2*math.Pi) * 180 / math.Pi
}

func frozenPreset(t *testing.T, w *World) OrbitPreset {
	t.Helper()
	sys := w.System()
	p, ok := OrbitPresetByKey(sys, *sys.FindBody("moon"), "frozen")
	if !ok {
		t.Fatal("no frozen preset at the Moon")
	}
	return p
}

// ADR 0065 guard 4: the Moon's frozen spawn reads Ap 17,409 / Pe 1,748 /
// 57.00 deg / 30 h in the Moon's equatorial frame, in RingState's plane.
func TestSpawnPresetElements(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	p := frozenPreset(t, w)
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "moon", Preset: &p})
	if err != nil {
		t.Fatal(err)
	}
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	mu := c.Primary.GravitationalParameter()
	el := orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
	rad := c.Primary.RadiusMeters()
	if got := (el.Apoapsis() - rad) / 1e3; math.Abs(got-17409) > 1 {
		t.Errorf("Ap = %.1f km, want 17409", got)
	}
	if got := (el.Periapsis() - rad) / 1e3; math.Abs(got-1748) > 1 {
		t.Errorf("Pe = %.1f km, want 1748", got)
	}
	if got := el.I * 180 / math.Pi; math.Abs(got-57) > 0.005 {
		t.Errorf("incl = %.3f, want 57.00", got)
	}
	if got := orbital.PeriodFromA(el.A, mu); math.Abs(got-30*3600) > 100 {
		t.Errorf("period = %.0f s, want 30 h (108000) within the ADR's 108,046", got)
	}
	h := c.State.R.Cross(c.State.V).Unit()
	rs, vs := spacecraft.RingState(frame, rad+1e6, 1, 90, 57)
	if d := h.Dot(rs.Cross(vs).Unit()); d < math.Cos(0.01*math.Pi/180) {
		t.Errorf("plane normal off RingState's 57 deg plane: dot = %v", d)
	}
}

// ADR 0065 guard 5: five spawns slot 180/108/36/324/252, a sixth wraps to
// 180; a 100 km circle and a 10 deg tilted copy of the ellipse do not count.
func TestSpawnPresetSlotting(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	p := frozenPreset(t, w)
	spec := SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "moon", Preset: &p}
	// Neighbours that must not shift the slot.
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "moon", AltitudeM: 100e3}); err != nil {
		t.Fatal(err)
	}
	// Same a but round (only the e test excludes it), same e but 10 % wider
	// (only the a test), and the ellipse tilted 10 deg (only the plane test).
	sys := w.System()
	moon := sys.FindBody("moon")
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "moon", AltitudeM: p.AM - moon.RadiusMeters(), Inclination: p.InclDeg}); err != nil {
		t.Fatal(err)
	}
	wider, tilted := p, p
	wider.AM *= 1.1
	tilted.InclDeg = 67
	for _, nb := range []OrbitPreset{wider, tilted} {
		nb := nb
		if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "moon", Preset: &nb}); err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range []float64{180, 108, 36, 324, 252, 180} {
		c, err := w.SpawnCraft(spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := presetMeanAnomalyDeg(c); math.Abs(got-want) > 0.01 {
			t.Errorf("spawn %d: mean anomaly %.3f, want %v", i+1, got, want)
		}
	}
	// F2: a spawn made later lands a fifth of a lap behind the last vessel's
	// anomaly now, not at an absolute slot. 24 h of flight, then 0 h again
	// (the back-to-back equal case, already covered above, repeated after).
	mu := w.Crafts[len(w.Crafts)-1].Primary.GravitationalParameter()
	for i, dt := range []float64{24 * 3600, 0} {
		last := w.Crafts[len(w.Crafts)-1]
		st, ok := physics.KeplerStep(last.State, mu, dt)
		if !ok {
			t.Fatal("KeplerStep failed")
		}
		last.State = st
		wantM := math.Mod(presetMeanAnomalyDeg(last)-72+360, 360)
		c, err := w.SpawnCraft(spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := presetMeanAnomalyDeg(c); math.Abs(got-wantM) > 0.01 {
			t.Errorf("timed spawn %d (dt %v s): mean anomaly %.3f, want %.3f", i+1, dt, got, wantM)
		}
	}
}
