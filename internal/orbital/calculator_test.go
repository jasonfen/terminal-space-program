package orbital

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestSolarSystemCalculatorAtJ2000(t *testing.T) {
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var sol bodies.System
	for _, s := range systems {
		if s.Name == "Sol" {
			sol = s
			break
		}
	}
	calc := ForSystem(sol, bodies.J2000)
	if calc.GetSystemType() != SystemTypeSolar {
		t.Fatalf("wrong calculator type: %s", calc.GetSystemType())
	}
	earth := sol.FindBody("Earth")
	m := calc.CalculateMeanAnomaly(*earth, bodies.J2000)
	want := 357.5291 * math.Pi / 180.0
	if math.Abs(m-want) > 1e-10 {
		t.Errorf("Earth M at J2000: got %.10f, want %.10f", m, want)
	}
}

func TestSolarSystemCalculatorProgresses(t *testing.T) {
	systems, _ := bodies.LoadAll()
	sol := systems[0]
	earth := sol.FindBody("Earth")
	calc := ForSystem(sol, bodies.J2000)
	// After one sidereal year Earth's mean anomaly should return to starting value (mod 2π).
	then := bodies.J2000.Add(time.Duration(earth.SideralOrbit*24) * time.Hour)
	m0 := calc.CalculateMeanAnomaly(*earth, bodies.J2000)
	m1 := calc.CalculateMeanAnomaly(*earth, then)
	diff := math.Mod(math.Abs(m0-m1), 2*math.Pi)
	if diff > 1e-3 && math.Abs(diff-2*math.Pi) > 1e-3 {
		t.Errorf("Earth M after 1 sidereal year: |Δ| mod 2π = %.6f (expected ≈0)", diff)
	}
}

func TestGenericCalculatorForExoplanet(t *testing.T) {
	systems, _ := bodies.LoadAll()
	var trappist bodies.System
	for _, s := range systems {
		if s.Name == "TRAPPIST-1" {
			trappist = s
			break
		}
	}
	calc := ForSystem(trappist, bodies.J2000)
	if calc.GetSystemType() != SystemTypeGeneric {
		t.Errorf("TRAPPIST-1 expected Generic, got %s", calc.GetSystemType())
	}
	// Should produce a finite, stable value.
	b := trappist.FindBody("TRAPPIST-1e")
	m := calc.CalculateMeanAnomaly(*b, bodies.J2000)
	if math.IsNaN(m) || math.IsInf(m, 0) {
		t.Errorf("TRAPPIST-1e M not finite: %v", m)
	}
}
