package main

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// loadFlagsTestCatalog loads the embedded systems catalog once per call,
// for tests that need real bodies to check the ADR 0044 §5 Orbit Band
// clamp report.
func loadFlagsTestCatalog(t *testing.T) []bodies.System {
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

func TestParseDistanceM(t *testing.T) {
	cases := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{"400km", 400_000, false},
		{"400000m", 400_000, false},
		{"400", 400_000, false}, // bare → km
		{"35786km", 35_786_000, false},
		{" 100 km ", 100_000, false},
		{"-5km", 0, true},
		{"abc", 0, true},
		{"", 0, true},
	}
	for _, c := range cases {
		got, err := parseDistanceM(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseDistanceM(%q): want error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDistanceM(%q): unexpected error %v", c.in, err)
			continue
		}
		if math.Abs(got-c.want) > 1e-6 {
			t.Errorf("parseDistanceM(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBuildScenarioNoFlags(t *testing.T) {
	s, err := buildScenario(rawFlags{set: map[string]bool{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != nil {
		t.Errorf("no flags should yield nil scenario (default start), got %+v", s)
	}
}

func TestBuildScenarioOrbital(t *testing.T) {
	s, err := buildScenario(rawFlags{
		system:      "Lumen",
		body:        "kernel",
		altitude:    "400km",
		inclination: 30,
		loadout:     "Kern-Stack",
		set:         map[string]bool{"altitude": true, "inclination": true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected a scenario")
	}
	if s.Surface {
		t.Error("orbital flags should not set Surface")
	}
	if s.SystemName != "Lumen" || s.BodyID != "kernel" || s.Loadout != "Kern-Stack" {
		t.Errorf("system/body/loadout not threaded: %+v", s)
	}
	if math.Abs(s.AltitudeM-400_000) > 1e-6 || s.InclDeg != 30 {
		t.Errorf("altitude/incl wrong: alt=%v incl=%v", s.AltitudeM, s.InclDeg)
	}
}

func TestBuildScenarioLaunchSite(t *testing.T) {
	s, err := buildScenario(rawFlags{
		launchSite: "KSC",
		loadout:    "Saturn-V",
		set:        map[string]bool{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.Surface {
		t.Error("--launch-site should set Surface")
	}
	if math.Abs(s.LatDeg-28.6083) > 1e-3 {
		t.Errorf("KSC latitude not resolved: %v", s.LatDeg)
	}
}

func TestBuildScenarioLaunchpadDefaultsKSC(t *testing.T) {
	s, err := buildScenario(rawFlags{launchpad: true, set: map[string]bool{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.Surface || math.Abs(s.LatDeg-28.6083) > 1e-3 {
		t.Errorf("bare --launchpad should default to KSC, got Surface=%v lat=%v", s.Surface, s.LatDeg)
	}
}

func TestBuildScenarioNumericSite(t *testing.T) {
	s, err := buildScenario(rawFlags{
		lat: 0, lon: 0,
		set: map[string]bool{"lat": true, "lon": true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.Surface || s.LatDeg != 0 || s.LonDeg != 0 {
		t.Errorf("explicit --lat 0 --lon 0 should be an equatorial surface spawn, got %+v", s)
	}
}

func TestBuildScenarioConflict(t *testing.T) {
	_, err := buildScenario(rawFlags{
		altitude:  "400km",
		launchpad: true,
		set:       map[string]bool{"altitude": true},
	})
	if err == nil {
		t.Error("orbital + surface flags should conflict")
	}
}

func TestBuildScenarioSiteVsLatLonConflict(t *testing.T) {
	_, err := buildScenario(rawFlags{
		launchSite: "KSC",
		lat:        10,
		set:        map[string]bool{"lat": true},
	})
	if err == nil {
		t.Error("--launch-site + --lat should conflict")
	}
}

func TestBuildScenarioUnknownSite(t *testing.T) {
	_, err := buildScenario(rawFlags{launchSite: "Nowhere", set: map[string]bool{}})
	if err == nil {
		t.Error("unknown launch site should error")
	}
}

// TestStartAltitudeClampNoteReportsRaise — "--altitude 60km" at Earth
// (ADR 0044 §5) must report the same raise-to-the-floor SpawnCraft will
// actually apply.
func TestStartAltitudeClampNoteReportsRaise(t *testing.T) {
	systems := loadFlagsTestCatalog(t)
	s := &sim.StartScenario{BodyID: "earth", AltitudeM: 60_000}
	note := startAltitudeClampNote(systems, s)
	if note == "" {
		t.Fatal("expected a clamp note for a below-floor Earth altitude, got empty")
	}
	// Review finding #3: the note states both ends with units — on the CLI
	// path this note is the ONLY output telling the player where they
	// actually launched, so "raised from 60" (no unit, no landed value) is
	// not good enough.
	want := "raised from 60 km to 175 km — Earth's air reaches 150 km"
	if note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
}

// TestStartAltitudeClampNoteReportsLower mirrors the ceiling side at the
// Moon.
func TestStartAltitudeClampNoteReportsLower(t *testing.T) {
	systems := loadFlagsTestCatalog(t)
	s := &sim.StartScenario{BodyID: "moon", AltitudeM: 90_000_000}
	note := startAltitudeClampNote(systems, s)
	if note == "" {
		t.Fatal("expected a clamp note for an above-ceiling Moon altitude, got empty")
	}
	if got, want := note[:len("lowered from 90,000 km to")], "lowered from 90,000 km to"; got != want {
		t.Errorf("note = %q, want prefix %q", note, want)
	}
}

// TestStartAltitudeClampNoteEmptyForInBand — the common case: a
// requested altitude already inside the band reports nothing.
func TestStartAltitudeClampNoteEmptyForInBand(t *testing.T) {
	systems := loadFlagsTestCatalog(t)
	s := &sim.StartScenario{BodyID: "earth", AltitudeM: 400_000}
	if note := startAltitudeClampNote(systems, s); note != "" {
		t.Errorf("note = %q, want empty (in-band altitude)", note)
	}
}

// TestStartAltitudeClampNoteEmptyForSurface — a surface (launchpad)
// scenario never clamps; the helper must not even try to resolve a body
// for it.
func TestStartAltitudeClampNoteEmptyForSurface(t *testing.T) {
	systems := loadFlagsTestCatalog(t)
	s := &sim.StartScenario{BodyID: "earth", Surface: true}
	if note := startAltitudeClampNote(systems, s); note != "" {
		t.Errorf("note = %q, want empty (surface scenario)", note)
	}
}

// TestStartAltitudeClampNoteEmptyForEmptyBand — "--orbit phobos" must not
// print a garbled "altitude <note>" line here; SpawnCraft's own refusal
// (surfaced through ApplyStartScenario -> tui.New -> main's normal error
// path) is the single message this scenario should ever produce.
func TestStartAltitudeClampNoteEmptyForEmptyBand(t *testing.T) {
	systems := loadFlagsTestCatalog(t)
	s := &sim.StartScenario{BodyID: "phobos", AltitudeM: 50_000}
	if note := startAltitudeClampNote(systems, s); note != "" {
		t.Errorf("note = %q, want empty (Empty band is a refusal, not a report-here move)", note)
	}
}

// TestStartAltitudeClampNoteEmptyForNilScenario — the default-start path
// (no scenario flags) must not panic or resolve anything.
func TestStartAltitudeClampNoteEmptyForNilScenario(t *testing.T) {
	systems := loadFlagsTestCatalog(t)
	if note := startAltitudeClampNote(systems, nil); note != "" {
		t.Errorf("note = %q, want empty (nil scenario)", note)
	}
}
