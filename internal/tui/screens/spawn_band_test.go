package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// #221 part 2: the spawn form flags posOrbit presets that fall in the
// degraded comms band, from the injected per-primary sampler — the form
// itself never derives coverage.

// bandStub returns a canned coverage per altitude (km-keyed), defaulting
// to full coverage.
func bandStub(byAltKm map[int]float64) func(string, float64) (float64, bool) {
	return func(bodyID string, altM float64) (float64, bool) {
		if cov, ok := byAltKm[int(altM/1000)]; ok {
			return cov, true
		}
		return 1.0, true
	}
}

func bandTestBodies() []bodies.CelestialBody {
	return []bodies.CelestialBody{{ID: "earth", Name: "Earth"}}
}

func resetWithBand(s *SpawnCraft, cov func(string, float64) (float64, bool)) {
	s.Reset(bandTestBodies(), "earth", nil, "", cov)
}

func TestSpawnFormFlagsDegradedBand(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	resetWithBand(s, bandStub(map[int]float64{5000: 0.61}))

	setAltPreset(t, s, 5000)
	out := s.Render(80)
	if !strings.Contains(out, "degraded comms band") || !strings.Contains(out, "relays advised") {
		t.Errorf("a degraded preset must be flagged with the relay advice:\n%s", out)
	}

	setAltPreset(t, s, 500)
	if out := s.Render(80); strings.Contains(out, "degraded comms band") {
		t.Errorf("a full-coverage preset must not be flagged:\n%s", out)
	}
}

func TestSpawnFormOutOfReachWording(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	resetWithBand(s, bandStub(map[int]float64{5000: 0}))
	setAltPreset(t, s, 5000)
	out := s.Render(80)
	if !strings.Contains(out, "out of network reach") {
		t.Errorf("zero coverage is the out-of-range case, not the band:\n%s", out)
	}
	if strings.Contains(out, "relays advised") {
		t.Errorf("zero coverage must not advise relays (bum-steer discipline):\n%s", out)
	}
}

func TestSpawnFormNoBandWarningWithoutSampler(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	setAltPreset(t, s, 5000)
	if out := s.Render(80); strings.Contains(out, "degraded comms band") {
		t.Errorf("no sampler injected → no claim made:\n%s", out)
	}
}

// setAltPreset moves the altitude cursor to the preset with the given
// km value.
func setAltPreset(t *testing.T, s *SpawnCraft, km int) {
	t.Helper()
	for i, v := range altitudePresets {
		if v == km {
			s.altIdx = i
			return
		}
	}
	t.Fatalf("no %d km preset", km)
}
