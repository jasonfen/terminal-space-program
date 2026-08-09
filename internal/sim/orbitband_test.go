package sim

import (
	"fmt"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// loadCatalog loads the embedded systems catalog once per call. Tests
// that need it multiple times should call it once and reuse the slice.
func loadOrbitBandCatalog(t *testing.T) []bodies.System {
	t.Helper()
	systems, warnings, err := bodies.LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("LoadAllWithWarnings: %v", err)
	}
	for _, w := range warnings {
		t.Fatalf("unexpected catalog load warning: %v", w)
	}
	return systems
}

// findBody returns the System and CelestialBody for id within the
// system named sysName, failing the test if either is missing.
func findBodyIn(t *testing.T, systems []bodies.System, sysName, id string) (bodies.System, bodies.CelestialBody) {
	t.Helper()
	for _, sys := range systems {
		if sys.Name != sysName {
			continue
		}
		for _, b := range sys.Bodies {
			if b.ID == id {
				return sys, b
			}
		}
		t.Fatalf("body %q not found in system %q", id, sysName)
	}
	t.Fatalf("system %q not found", sysName)
	return bodies.System{}, bodies.CelestialBody{}
}

func TestOrbitBandFor_FloorFromAtmosphereCutoffPlusMargin(t *testing.T) {
	systems := loadOrbitBandCatalog(t)

	cases := []struct{ sysName, id string }{
		{"Sol", "earth"},
		{"Sol", "mars"},
		{"Sol", "moon"}, // airless: no Atmosphere block
	}
	for _, c := range cases {
		sys, b := findBodyIn(t, systems, c.sysName, c.id)
		cutoff := 0.0
		if b.Atmosphere != nil {
			cutoff = b.Atmosphere.CutoffAltitude
		}
		want := cutoff + OrbitFloorMarginM
		got := OrbitBandFor(sys, b).FloorM
		if got != want {
			t.Errorf("%s floor = %.1f, want %.1f (cutoff=%.1f)", b.Name, got, want, cutoff)
		}
	}
}

func TestOrbitBandFor_StarFloorsAtStandOffAndHasNoCeiling(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	sys, sun := findBodyIn(t, systems, "Sol", "sun")

	if sun.BodyType != "Star" {
		t.Fatalf("expected sun.BodyType == Star, got %q", sun.BodyType)
	}

	band := OrbitBandFor(sys, sun)
	if want := sun.OrbitStandOffM(); band.FloorM != want {
		t.Errorf("sun floor = %.1f, want OrbitStandOffM() = %.1f", band.FloorM, want)
	}
	if band.HasCeiling {
		t.Errorf("sun band has a ceiling (%.1f); a star orbits nothing, want HasCeiling=false", band.CeilingM)
	}
	if band.Empty {
		t.Errorf("sun band reports Empty; a star always has a floor and no ceiling to fall below")
	}
}

func TestOrbitBandFor_CeilingIsTwoThirdsToSOI(t *testing.T) {
	systems := loadOrbitBandCatalog(t)

	// Moon: exercises a real parent lookup (ParentID == "earth").
	solSys, moon := findBodyIn(t, systems, "Sol", "moon")
	_, earth := findBodyIn(t, systems, "Sol", "earth")
	wantMoonCeiling := (2.0 / 3.0) * (physics.SOIRadius(moon, earth) - moon.RadiusMeters())
	gotMoon := OrbitBandFor(solSys, moon).CeilingM
	if math.Abs(gotMoon-wantMoonCeiling) > 1.0 {
		t.Errorf("moon ceiling = %.3f, want %.3f (hand-derived vs. earth)", gotMoon, wantMoonCeiling)
	}

	// Mars: ParentID is empty (planets aren't tagged with a parent in
	// the catalog JSON), so this exercises the ParentOf -> Bodies[0]
	// fallback that resolves a planet's ceiling against its star.
	_, sun := findBodyIn(t, systems, "Sol", "sun")
	_, mars := findBodyIn(t, systems, "Sol", "mars")
	if mars.ParentID != "" {
		t.Fatalf("expected mars.ParentID == \"\" (fallback precondition), got %q", mars.ParentID)
	}
	wantMarsCeiling := (2.0 / 3.0) * (physics.SOIRadius(mars, sun) - mars.RadiusMeters())
	gotMars := OrbitBandFor(solSys, mars).CeilingM
	if math.Abs(gotMars-wantMarsCeiling) > 1.0 {
		t.Errorf("mars ceiling = %.3f, want %.3f (hand-derived vs. sun)", gotMars, wantMarsCeiling)
	}
	if !OrbitBandFor(solSys, mars).HasCeiling {
		t.Errorf("mars band reports HasCeiling=false")
	}
}

func TestOrbitBandFor_EmptyBandsAreExactlyPhobosAndDeimos(t *testing.T) {
	systems := loadOrbitBandCatalog(t)

	var empty []string
	for _, sys := range systems {
		for _, b := range sys.Bodies {
			band := OrbitBandFor(sys, b)
			if band.Empty {
				empty = append(empty, sys.Name+"/"+b.ID)
			}
		}
	}

	want := map[string]bool{"Sol/phobos": true, "Sol/deimos": true}
	if len(empty) != len(want) {
		t.Fatalf("empty-band bodies = %v, want exactly %v", empty, want)
	}
	for _, id := range empty {
		if !want[id] {
			t.Errorf("unexpected empty-band body %s", id)
		}
	}
}

func TestOrbitBandFor_PhobosAndDeimosWhyEmpty(t *testing.T) {
	systems := loadOrbitBandCatalog(t)

	sys, phobos := findBodyIn(t, systems, "Sol", "phobos")
	pband := OrbitBandFor(sys, phobos)
	if pband.FloorM != OrbitFloorMarginM {
		t.Errorf("phobos floor = %.1f, want flat margin %.1f (airless)", pband.FloorM, OrbitFloorMarginM)
	}
	if pband.CeilingM >= pband.FloorM {
		t.Errorf("phobos ceiling %.3f should be below its floor %.3f (SOI < own radius)", pband.CeilingM, pband.FloorM)
	}
	if !pband.Empty {
		t.Errorf("phobos band should be Empty")
	}

	_, deimos := findBodyIn(t, systems, "Sol", "deimos")
	dband := OrbitBandFor(sys, deimos)
	if dband.CeilingM <= 0 {
		t.Errorf("deimos ceiling = %.3f, want a small positive value (real ~2km band, eaten by the flat floor)", dband.CeilingM)
	}
	if dband.CeilingM >= dband.FloorM {
		t.Errorf("deimos ceiling %.3f should be below its 25km floor %.3f", dband.CeilingM, dband.FloorM)
	}
	if !dband.Empty {
		t.Errorf("deimos band should be Empty")
	}
}

func TestOrbitStops_EmptyBandReturnsNil(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	sys, phobos := findBodyIn(t, systems, "Sol", "phobos")
	if got := OrbitStops(sys, phobos); got != nil {
		t.Errorf("OrbitStops(phobos) = %v, want nil (empty band)", got)
	}
}

func TestOrbitStops_AscendingDedupedAndInBand(t *testing.T) {
	systems := loadOrbitBandCatalog(t)

	for _, c := range []struct{ sysName, id string }{
		{"Sol", "earth"},
		{"Sol", "moon"},
		{"Sol", "mars"},
		{"Lumen", "mote"},
	} {
		sys, b := findBodyIn(t, systems, c.sysName, c.id)
		band := OrbitBandFor(sys, b)
		stops := OrbitStops(sys, b)

		if len(stops) == 0 {
			t.Fatalf("%s: OrbitStops returned no stops for a non-empty band", b.Name)
		}
		for i, s := range stops {
			if s < band.FloorM-1 || (band.HasCeiling && s > band.CeilingM+1) {
				t.Errorf("%s: stop[%d] = %.1f out of band [%.1f, %.1f]", b.Name, i, s, band.FloorM, band.CeilingM)
			}
			if i > 0 && stops[i-1] >= s {
				t.Errorf("%s: stops not strictly ascending at index %d: %v", b.Name, i, stops)
			}
			if i > 0 && s-stops[i-1] < orbitDedupeToleranceM {
				t.Errorf("%s: stops[%d] and stops[%d] within dedupe tolerance: %v", b.Name, i-1, i, stops)
			}
		}
		if stops[0] != band.FloorM {
			t.Errorf("%s: first stop = %.1f, want floor %.1f", b.Name, stops[0], band.FloorM)
		}
		if band.HasCeiling && stops[len(stops)-1] != band.CeilingM {
			t.Errorf("%s: last stop = %.1f, want ceiling %.1f", b.Name, stops[len(stops)-1], band.CeilingM)
		}
	}
}

func TestOrbitStops_SynchronousPresentAtEarthAbsentAtMoon(t *testing.T) {
	systems := loadOrbitBandCatalog(t)

	solSys, earth := findBodyIn(t, systems, "Sol", "earth")
	earthSync := synchronousAltitudeM(earth)
	if earthSync <= 0 {
		t.Fatalf("expected a positive synchronous altitude for earth")
	}
	earthStops := OrbitStops(solSys, earth)
	if !containsWithin(earthStops, earthSync, 1.0) {
		t.Errorf("earth stops %v do not include synchronous altitude %.1f", earthStops, earthSync)
	}

	_, moon := findBodyIn(t, systems, "Sol", "moon")
	moonBand := OrbitBandFor(solSys, moon)
	moonSync := synchronousAltitudeM(moon)
	if moonSync <= moonBand.CeilingM {
		t.Fatalf("expected the moon's synchronous altitude (%.1f) to exceed its ceiling (%.1f) — this test's premise", moonSync, moonBand.CeilingM)
	}
	moonStops := OrbitStops(solSys, moon)
	if containsWithin(moonStops, moonSync, 1.0) {
		t.Errorf("moon stops %v unexpectedly include its out-of-band synchronous altitude %.1f", moonStops, moonSync)
	}
}

func TestOrbitStops_NarrowBandOffersFewerThanFive(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	sys, mote := findBodyIn(t, systems, "Lumen", "mote")

	band := OrbitBandFor(sys, mote)
	if band.Empty {
		t.Fatalf("expected mote to have a non-empty (if narrow) band")
	}
	stops := OrbitStops(sys, mote)
	if len(stops) >= 5 {
		t.Errorf("expected mote (a narrow-band moon) to offer fewer than five stops, got %d: %v", len(stops), stops)
	}
	if len(stops) == 0 {
		t.Errorf("expected mote to offer at least floor+ceiling")
	}
}

// TestClampToOrbitBand_RaisedNoteWording checks the exact player-facing
// sentence ClampToOrbitBand returns when it raises an altitude to the
// floor (ADR 0044 §4): what happened, where the number came from, and the
// physical fact (Earth's atmosphere cutoff) behind the limit.
func TestClampToOrbitBand_RaisedNoteWording(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	sys, earth := findBodyIn(t, systems, "Sol", "earth")
	if earth.Atmosphere == nil {
		t.Fatalf("test premise broken: earth has no Atmosphere block")
	}

	const requested = 60_000.0 // 60 km — below Earth's 175 km floor
	band := OrbitBandFor(sys, earth)
	got, note, ok := ClampToOrbitBand(sys, earth, requested)
	if !ok {
		t.Fatalf("ClampToOrbitBand(earth, 60km) ok=false, want true")
	}
	if got != band.FloorM {
		t.Errorf("clamped = %.1f, want floor %.1f", got, band.FloorM)
	}
	want := fmt.Sprintf("raised from %s — %s's air reaches %s km",
		commaKm(requested), earth.EnglishName, commaKm(earth.Atmosphere.CutoffAltitude))
	if note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
}

// TestClampToOrbitBand_LoweredNoteWording checks the ceiling-side wording:
// it cites the body's SOI ("grip"), not its (narrower) two-thirds ceiling.
func TestClampToOrbitBand_LoweredNoteWording(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	sys, moon := findBodyIn(t, systems, "Sol", "moon")
	_, earth := findBodyIn(t, systems, "Sol", "earth")

	const requested = 90_000_000.0 // 90,000 km — above the Moon's ceiling
	band := OrbitBandFor(sys, moon)
	got, note, ok := ClampToOrbitBand(sys, moon, requested)
	if !ok {
		t.Fatalf("ClampToOrbitBand(moon, 90000km) ok=false, want true")
	}
	if got != band.CeilingM {
		t.Errorf("clamped = %.1f, want ceiling %.1f", got, band.CeilingM)
	}
	soi := physics.SOIRadius(moon, earth)
	want := fmt.Sprintf("lowered from %s — %s's grip reaches %s km",
		commaKm(requested), moon.EnglishName, commaKm(soi))
	if note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
}

// TestClampToOrbitBand_InBandNoteEmpty checks the quiet path: an altitude
// already inside the band moves nothing and explains nothing.
func TestClampToOrbitBand_InBandNoteEmpty(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	sys, earth := findBodyIn(t, systems, "Sol", "earth")

	const requested = 400_000.0 // well inside Earth's band
	got, note, ok := ClampToOrbitBand(sys, earth, requested)
	if !ok {
		t.Fatalf("ClampToOrbitBand(earth, 400km) ok=false, want true")
	}
	if got != requested {
		t.Errorf("clamped = %.1f, want the untouched requested %.1f", got, requested)
	}
	if note != "" {
		t.Errorf("note = %q, want empty (nothing moved)", note)
	}
}

// TestClampToOrbitBand_NoOrbitNoteWording checks the Empty-band wording
// (ADR 0044 §6): it names the body that actually owns the space (Phobos's
// gravitational parent, Mars) and points at the launchpad. clampedM must
// come back unchanged — callers must not use it to place a craft.
func TestClampToOrbitBand_NoOrbitNoteWording(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	sys, phobos := findBodyIn(t, systems, "Sol", "phobos")
	_, mars := findBodyIn(t, systems, "Sol", "mars")

	const requested = 50_000.0
	got, note, ok := ClampToOrbitBand(sys, phobos, requested)
	if ok {
		t.Fatalf("ClampToOrbitBand(phobos) ok=true, want false (Empty band)")
	}
	if got != requested {
		t.Errorf("clampedM = %.1f, want the unchanged requested %.1f on a refused clamp", got, requested)
	}
	want := fmt.Sprintf("%s owns everything outside %s's surface — no orbit exists here; try the launchpad instead",
		mars.EnglishName, phobos.EnglishName)
	if note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
}

// containsWithin reports whether stops contains a value within tol of want.
func containsWithin(stops []float64, want, tol float64) bool {
	for _, s := range stops {
		if math.Abs(s-want) <= tol {
			return true
		}
	}
	return false
}
