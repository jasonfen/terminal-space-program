package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

func TestNewInLEO(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)

	// ADR 0053 §4: the start vessel is Apollo TLI-1, the S-IVB with the
	// LM and CSM still on top (was the bare 51 720 kg S-IVB-1 before).
	if sc.Name != "Apollo TLI-1" || sc.LoadoutID != "Apollo-TLI" {
		t.Errorf("start vessel = %q / %q, want Apollo TLI-1 / Apollo-TLI", sc.Name, sc.LoadoutID)
	}
	if len(sc.Stages) != 5 {
		t.Errorf("start vessel has %d stages, want 5 (S-IVB, Descent, Ascent, SM, CM)", len(sc.Stages))
	}
	wantMass := 0.0
	for _, st := range sc.Stages {
		wantMass += st.DryMass + st.FuelMass + st.MonopropMass
	}
	if wantMass < 90000 || wantMass > 94000 {
		t.Errorf("summed stage mass = %v, want about 91.9 t", wantMass)
	}
	if math.Abs(sc.TotalMass()-wantMass) > 1e-6 {
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

// #566: the default vessel orbits at SeedInclinationDeg in Earth's
// EQUATORIAL frame, prograde, at every phase (one shared plane), so a
// KSC launch (28.6 N) can reach it.
func TestNewInLEOAtPhaseInclinationIsSeedInclinationPrograde(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	frame := orbital.ReferenceFrameForPrimary(*earth)
	for _, ph := range []float64{0, 45, 90, 180, 270} {
		sc := NewInLEOAtPhase(*earth, ph)
		el := orbital.ElementsFromStateInFrame(sc.State.R, sc.State.V, earth.GravitationalParameter(), frame)
		if got := el.I * 180 / math.Pi; math.Abs(got-SeedInclinationDeg) > 0.01 {
			t.Errorf("phase %v: inclination %.4f deg, want %.1f", ph, got, SeedInclinationDeg)
		}
		if el.E > 1e-6 || math.Abs(sc.Altitude()-500e3) > 1 {
			t.Errorf("phase %v: not a 500 km circle (e=%g alt=%.1f)", ph, el.E, sc.Altitude())
		}
	}
	if SeedInclinationDeg != 51.6 {
		t.Errorf("SeedInclinationDeg = %v, want 51.6 (issue #566)", SeedInclinationDeg)
	}
}
