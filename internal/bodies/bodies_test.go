package bodies

import (
	"math"
	"testing"
)

func TestLoadAllSystems(t *testing.T) {
	systems, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(systems) < 4 {
		t.Fatalf("expected >=4 systems, got %d", len(systems))
	}
	if systems[0].Name != "Sol" {
		t.Errorf("expected Sol first, got %q", systems[0].Name)
	}
	names := map[string]bool{}
	for _, s := range systems {
		names[s.Name] = true
	}
	for _, want := range []string{"Sol", "Alpha Centauri", "TRAPPIST-1", "Kepler-452"} {
		if !names[want] {
			t.Errorf("missing system %q", want)
		}
	}
}

func TestSolEarthValues(t *testing.T) {
	systems, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var sol *System
	for i := range systems {
		if systems[i].Name == "Sol" {
			sol = &systems[i]
			break
		}
	}
	if sol == nil {
		t.Fatal("Sol not found")
	}
	earth := sol.FindBody("Earth")
	if earth == nil {
		t.Fatal("Earth not found in Sol")
	}
	// Earth semimajor axis ≈ 1 AU ± 0.1%.
	earthAMeters := earth.SemimajorAxisMeters()
	if d := math.Abs(earthAMeters-AU) / AU; d > 0.001 {
		t.Errorf("Earth semimajor axis %.3e m deviates from 1 AU by %.4f", earthAMeters, d)
	}
	// Earth mass ≈ 5.972e24 kg ± 0.1%.
	if d := math.Abs(earth.MassKg()-5.972e24) / 5.972e24; d > 0.001 {
		t.Errorf("Earth mass %.3e kg deviates from 5.972e24 by %.4f", earth.MassKg(), d)
	}
}

func TestGravitationalParameter(t *testing.T) {
	systems, _ := LoadAll()
	earth := systems[0].FindBody("Earth")
	if earth == nil {
		t.Fatal("Earth not found")
	}
	// Standard gravitational parameter of Earth: 3.986e14 m^3/s^2 ± 0.1%.
	mu := earth.GravitationalParameter()
	want := 3.986004418e14
	if d := math.Abs(mu-want) / want; d > 0.001 {
		t.Errorf("Earth GM %.3e deviates from %.3e by %.4f", mu, want, d)
	}
}
