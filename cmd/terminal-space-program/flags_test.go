package main

import (
	"math"
	"testing"
)

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
