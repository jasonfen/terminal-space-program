package screens

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #221 part 2: the spawn form flags posOrbit presets that fall in the
// degraded comms band, from the injected per-primary sampler — the form
// itself never derives coverage.

// bandStub returns a canned coverage per altitude (km-keyed), defaulting
// to full coverage, and records the antenna range of the last call.
func bandStub(byAltKm map[int]float64) func(string, float64, float64) (float64, bool) {
	return func(bodyID string, altM, antennaRangeM float64) (float64, bool) {
		if cov, ok := byAltKm[int(altM/1000)]; ok {
			return cov, true
		}
		return 1.0, true
	}
}

// bandTestBodies is a single bare body with no mass, radius or atmosphere —
// deliberately minimal, these tests only care about comms-band wording, not
// physics. Under ADR 0044's Orbit Band rules a body this bare floors at
// exactly OrbitFloorMarginM (25km, an airless-body cutoff of 0) with no
// ceiling (SOIRadius needs a SemimajorAxis this fixture doesn't set), so
// every altitude these tests commit (500km, 5000km) sits comfortably
// in-band — no sim.ClampToOrbitBand note fires, and the comms assertions
// below still mean exactly what they meant before S4.
func bandTestBodies() []bodies.CelestialBody {
	return []bodies.CelestialBody{{ID: "earth", Name: "Earth"}}
}

func resetWithBand(s *SpawnCraft, cov func(string, float64, float64) (float64, bool)) {
	s.Reset(bandTestBodies(), "earth", nil, "", cov)
}

func TestSpawnFormFlagsDegradedBand(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	resetWithBand(s, bandStub(map[int]float64{5000: 0.61}))

	setAltitude(t, s, 5000)
	out := s.Render(80)
	if !strings.Contains(out, "degraded comms band") || !strings.Contains(out, "relays advised") {
		t.Errorf("a degraded preset must be flagged with the relay advice:\n%s", out)
	}

	setAltitude(t, s, 500)
	if out := s.Render(80); strings.Contains(out, "degraded comms band") {
		t.Errorf("a full-coverage preset must not be flagged:\n%s", out)
	}
}

func TestSpawnFormOutOfReachWording(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	resetWithBand(s, bandStub(map[int]float64{5000: 0}))
	setAltitude(t, s, 5000)
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
	setAltitude(t, s, 5000)
	if out := s.Render(80); strings.Contains(out, "degraded comms band") {
		t.Errorf("no sampler injected → no claim made:\n%s", out)
	}
}

// Review fixes (v0.32 batch): the warning must describe the craft being
// spawned — crewed loadouts are never comms-gated so they get no
// warning, and the sampler receives the selected stack's best antenna
// so a relay craft isn't warned off the constellation-building spawn.

func TestSpawnFormCrewedLoadoutNeverWarned(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	resetWithBand(s, bandStub(map[int]float64{5000: 0.5}))
	selectCustom(s)
	s.customStages = []spacecraft.Stage{{Name: "Pod", CommandSource: spacecraft.CommandCrewed}}
	setAltitude(t, s, 5000)
	if out := s.Render(80); strings.Contains(out, "degraded comms band") || strings.Contains(out, "out of network reach") {
		t.Errorf("a crewed craft is never comms-gated — no warning applies:\n%s", out)
	}
}

func TestSpawnFormPassesSelectedAntennaToSampler(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	var gotRange float64
	s.Reset(bandTestBodies(), "earth", nil, "", func(bodyID string, altM, antennaRangeM float64) (float64, bool) {
		gotRange = antennaRangeM
		return 1.0, true
	})
	selectCustom(s)
	s.customStages = []spacecraft.Stage{{
		Name: "Bus", CommandSource: spacecraft.CommandProbe,
		AntennaKind: spacecraft.AntennaRelay, AntennaRangeM: spacecraft.AntennaRangeRelayCislunar,
	}}
	setAltitude(t, s, 5000)
	_ = s.Render(80)
	if gotRange != spacecraft.AntennaRangeRelayCislunar {
		t.Errorf("sampler must receive the selected stack's antenna range; got %g", gotRange)
	}
}

// setAltitude commits a typed altitude (km) via the real edit-box path
// (ADR 0044 / S4) — HandleKey drives fieldIdx to ALTITUDE, opens the box,
// types each digit, then commits, exercising the same state machine a
// player uses rather than poking a removed ladder index directly.
func setAltitude(t *testing.T, s *SpawnCraft, km int) {
	t.Helper()
	s.fieldIdx = 3
	if got := s.HandleKey("enter"); got != SpawnActionNone {
		t.Fatalf("opening the altitude box returned %v, want SpawnActionNone", got)
	}
	for _, d := range strconv.Itoa(km) {
		s.HandleKey(string(d))
	}
	if got := s.HandleKey("enter"); got != SpawnActionNone {
		t.Fatalf("committing the altitude box returned %v, want SpawnActionNone", got)
	}
}
