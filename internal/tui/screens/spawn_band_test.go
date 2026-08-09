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

// TestClampAndCommsWarningBothRenderAtLowCeilingBody — review finding #2.
// A body whose Orbit Band ceiling sits below the 500km Reset default opens
// ALREADY clamped (Enceladus's ceiling is ~157km; Lumen's Mote is ~75km),
// so the clamp note (↳) is non-empty from the very first render — before
// this fix that permanently hid the comms warning (⚠), a standing fact
// about the spawn, not feedback about a keypress. Both must render, on the
// real first frame after Reset, at a REAL body (not the bare bandTestBodies
// fixture, whose Orbit Band never clamps the 500km default).
func TestClampAndCommsWarningBothRenderAtLowCeilingBody(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	systems := loadRealSystems(t)
	sys := findRealBody(t, systems, "Sol", "enceladus")
	// Coverage stub: degraded regardless of altitude, so the warning is
	// live at whatever altitude Enceladus's ceiling clamps the 500km
	// default down to.
	degraded := func(bodyID string, altM, antennaRangeM float64) (float64, bool) { return 0.5, true }
	s.Reset(sys.Bodies, "enceladus", nil, "", degraded)

	// An uncrewed craft with an antenna, so bandWarning has something to
	// say (crewed craft are never comms-gated).
	selectCustom(s)
	s.customStages = []spacecraft.Stage{{
		Name: "Bus", CommandSource: spacecraft.CommandProbe,
		AntennaKind: spacecraft.AntennaRelay, AntennaRangeM: spacecraft.AntennaRangeRelayCislunar,
	}}

	if s.altNote == "" {
		t.Fatalf("setup: Enceladus's Orbit Band did not clamp the 500km Reset default — test premise broken")
	}

	out := s.Render(80) // the very first render — no arrow keys, no re-focus
	clampIdx := strings.Index(out, "↳")
	warnIdx := strings.Index(out, "⚠")
	if clampIdx < 0 {
		t.Errorf("clamp note (↳) missing from the first render at a low-ceiling body:\n%s", out)
	}
	if warnIdx < 0 {
		t.Errorf("comms warning (⚠) missing from the first render at a low-ceiling body — it must not be hidden behind the clamp note:\n%s", out)
	}
	if clampIdx >= 0 && warnIdx >= 0 && clampIdx > warnIdx {
		t.Errorf("comms warning rendered BEFORE the clamp note; want clamp first:\n%s", out)
	}
}

// setAltitude commits a typed altitude (km) via the real edit-box path
// (ADR 0044 / S4) — HandleKey drives fieldIdx to ALTITUDE, opens the box,
// types each digit, then commits, exercising the same state machine a
// player uses rather than poking a removed ladder index directly.
func setAltitude(t *testing.T, s *SpawnCraft, km int) {
	t.Helper()
	// Tab first, the way a player who has just committed one altitude gets
	// back to the field: leaving the box arms "Enter now launches", and any
	// non-Enter key disarms it. Without this a second setAltitude in the
	// same test would launch the form instead of reopening the box.
	s.HandleKey("tab")
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
