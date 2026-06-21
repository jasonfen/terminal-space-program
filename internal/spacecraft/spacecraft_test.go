package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestNewInLEO(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)

	// v0.5.13+ default: S-IVB-like 11 000 kg dry + 40 000 kg fuel.
	// v0.8.0+: + 720 kg monoprop tank (per DefaultRCSLoadout sizing,
	// targeting ~30 m/s of RCS Δv budget on the wet craft).
	const wantMass = 51720.0
	if sc.TotalMass() != wantMass {
		t.Errorf("total mass = %v, want %v", sc.TotalMass(), wantMass)
	}
	// Altitude should be ~500 km (v0.6.1+ default — was 200 km).
	alt := sc.Altitude()
	if math.Abs(alt-500e3) > 1 {
		t.Errorf("altitude = %.1f m, want ~500000", alt)
	}
	// Circular orbital speed at 500 km altitude: √(μ/(R_earth + 500 km)) ≈ 7613 m/s.
	v := sc.OrbitalSpeed()
	if math.Abs(v-7613) > 50 {
		t.Errorf("orbital speed = %.1f m/s, want ~7613 m/s", v)
	}
}

// TestSurfaceLatLon — touchdown coords win when set; otherwise the
// launchpad-spawn coords are used (the "when non-zero, read these instead"
// fallback shared by integrateLanded and the mission evaluator).
func TestSurfaceLatLon(t *testing.T) {
	s := &Spacecraft{LaunchLatDeg: 28.6, LaunchLonDeg: -80.6}
	// No touchdown coords → falls back to the launch site.
	if lat, lon := s.SurfaceLatLon(); lat != 28.6 || lon != -80.6 {
		t.Errorf("fallback: got (%v,%v), want (28.6,-80.6)", lat, lon)
	}
	// Touchdown coords set → they win.
	s.LandedLatDeg, s.LandedLonDeg = -5, 120
	if lat, lon := s.SurfaceLatLon(); lat != -5 || lon != 120 {
		t.Errorf("touchdown: got (%v,%v), want (-5,120)", lat, lon)
	}
}
