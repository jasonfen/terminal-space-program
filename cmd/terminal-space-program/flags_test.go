package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/save"
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

// TestStartAltitudeClampNoteEmpty: every case that must report nothing.
func TestStartAltitudeClampNoteEmpty(t *testing.T) {
	systems := loadFlagsTestCatalog(t)
	for _, c := range []struct {
		name string
		s    *sim.StartScenario
	}{
		// The common case: a requested altitude already inside the band.
		{"in band", &sim.StartScenario{BodyID: "earth", AltitudeM: 400_000}},
		// A surface (launchpad) scenario never clamps; the helper must not
		// even try to resolve a body for it.
		{"surface", &sim.StartScenario{BodyID: "earth", Surface: true}},
		// "--orbit phobos" must not print a garbled "altitude <note>" line:
		// SpawnCraft's own refusal (surfaced through ApplyStartScenario ->
		// tui.New -> main's normal error path) is the single message this
		// scenario should ever produce. Empty band is a refusal, not a
		// report-here move.
		{"empty band", &sim.StartScenario{BodyID: "phobos", AltitudeM: 50_000}},
		// The default-start path (no scenario flags) must not panic or
		// resolve anything.
		{"nil scenario", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if note := startAltitudeClampNote(systems, c.s); note != "" {
				t.Errorf("note = %q, want empty", note)
			}
		})
	}
}

// TestBareLaunchpadOnLumenIsEquatorial (#461): the CLI's bare --launchpad
// uses the same per-system default site as the spawn form.
func TestBareLaunchpadOnLumenIsEquatorial(t *testing.T) {
	s, err := buildScenario(rawFlags{launchpad: true, system: "Lumen", set: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Surface || s.LatDeg != 0 {
		t.Errorf("bare --launchpad --system Lumen: Surface=%v lat=%v, want equator", s.Surface, s.LatDeg)
	}
}

// ADR 0065 guard 6: "--altitude frozen" at the Moon spawns the preset; at
// Earth it is refused with a message naming the body (main exits 2).
func TestAltitudeFrozenFlag(t *testing.T) {
	systems := loadFlagsTestCatalog(t)
	raw := rawFlags{body: "moon", altitude: "frozen", set: map[string]bool{"altitude": true}}
	s, err := buildScenario(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.PresetKey != "frozen" || s.AltitudeM != 0 {
		t.Errorf("scenario = %+v, want PresetKey frozen and no circle altitude", s)
	}
	if err := checkStartPreset(systems, s); err != nil {
		t.Errorf("Moon: %v", err)
	}
	if note := startAltitudeClampNote(systems, s); note != "" {
		t.Errorf("preset scenario got an Orbit Band clamp note %q", note)
	}
	s.BodyID = "earth"
	err = checkStartPreset(systems, s)
	if err == nil || err.Error() != `no preset "frozen" at Earth` {
		t.Errorf("Earth: err = %v, want no preset \"frozen\" at Earth", err)
	}
}

func TestValidateLoad(t *testing.T) {
	cases := []struct {
		name                  string
		load                  string
		hasScenario, resetFlt bool
		wantErr               string
	}{
		{"no --load", "", true, true, ""}, // other validators own those flags
		{"load alone", "x", false, false, ""},
		{"load with spawn flags", "x", true, false, "spawn flags"},
		{"load with reset-fleet", "x", false, true, "--reset-fleet"},
	}
	for _, c := range cases {
		err := validateLoad(c.load, c.hasScenario, c.resetFlt)
		if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
}

func TestResolveLoad(t *testing.T) {
	infos := []save.SaveInfo{
		{ID: "a.json", Lane: save.LaneAutosave},
		{ID: "n1.json", Lane: save.LaneNamed, Meta: save.Meta{Name: "Dock test"}},
		{ID: "n2.json", Lane: save.LaneNamed, Meta: save.Meta{Name: "Dock test"}},
		{ID: "n3.json", Lane: save.LaneNamed, Meta: save.Meta{Name: "Landing"}},
		{ID: "bad.json", Lane: save.LaneNamed, Unreadable: true, Note: "newer version"},
	}
	file := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		arg, wantID, wantPath, wantErr string
	}{
		{"Landing", "n3.json", "", ""},
		{"[AUTOSAVE 1]", "a.json", "", ""},
		{file, "", file, ""},
		{"Dock test", "", "", "matches 2 saves"}, // n1.json named in the message
		{"landing", "", "", "saves are:"},        // names are exact
		{"nope", "", "", "unreadable (newer version)"},
		{filepath.Dir(file), "", "", "no such save or file"}, // a directory is not a save
	}
	for _, c := range cases {
		id, path, err := resolveLoad(c.arg, infos)
		if c.wantErr == "" {
			if err != nil || id != c.wantID || path != c.wantPath {
				t.Errorf("%q: got (%q,%q,%v), want (%q,%q)", c.arg, id, path, err, c.wantID, c.wantPath)
			}
		} else if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%q: err = %v, want %q", c.arg, err, c.wantErr)
		}
	}
	if _, _, err := resolveLoad("x", nil); err == nil || !strings.Contains(err.Error(), "no saves") {
		t.Errorf("empty dir: err = %v", err)
	}
}
