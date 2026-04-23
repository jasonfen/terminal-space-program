package physics

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

func TestEarthSOIMatchesPublishedValue(t *testing.T) {
	systems, _ := bodies.LoadAll()
	sun := systems[0].FindBody("Sun")
	earth := systems[0].FindBody("Earth")
	soi := SOIRadius(*earth, *sun)
	// Published Earth SOI ≈ 9.29e8 m. Our formula uses masses from sol.json
	// and semimajor axis from sol.json, so result should be within 2%.
	want := 9.29e8
	if d := math.Abs(soi-want) / want; d > 0.02 {
		t.Errorf("Earth SOI = %.3e m, expected ~%.3e m (rel err %.3f)", soi, want, d)
	}
}

func TestFindPrimarySelectsEarthInsideSOI(t *testing.T) {
	systems, _ := bodies.LoadAll()
	sol := systems[0]
	earth := sol.FindBody("Earth")

	// Place Earth at a plausible inertial position, spacecraft just inside its SOI.
	earthPos := orbital.Vec3{X: bodies.AU}
	soi := SOIRadius(*earth, sol.Bodies[0])
	craft := earthPos.Add(orbital.Vec3{X: 0.5 * soi})

	positions := map[string]orbital.Vec3{earth.ID: earthPos}
	got := FindPrimary(sol, craft, positions)
	if got.Body.ID != earth.ID {
		t.Errorf("expected Earth as primary, got %s", got.Body.EnglishName)
	}
}

func TestFindPrimaryFallsBackToSun(t *testing.T) {
	systems, _ := bodies.LoadAll()
	sol := systems[0]

	// Spacecraft in free space between orbits.
	craft := orbital.Vec3{X: 1.5 * bodies.AU}
	positions := map[string]orbital.Vec3{}
	for _, b := range sol.Bodies {
		positions[b.ID] = orbital.Vec3{X: b.SemimajorAxisMeters()}
	}
	got := FindPrimary(sol, craft, positions)
	if got.Body.EnglishName != "Sun" {
		t.Errorf("expected Sun as fallback primary, got %s", got.Body.EnglishName)
	}
}

func TestRebaseRoundTrip(t *testing.T) {
	oldP := orbital.Vec3{X: bodies.AU}
	newP := orbital.Vec3{X: bodies.AU + 1e9}
	dv := orbital.Vec3{Y: 30e3} // 30 km/s (Earth orbital speed)

	s := StateVector{
		R: orbital.Vec3{X: 1e7, Y: 0, Z: 0},
		V: orbital.Vec3{Y: 7.78e3},
		M: 1000,
	}
	s2 := Rebase(s, oldP, newP, dv)
	// Inverse: swap primaries and negate dv.
	back := Rebase(s2, newP, oldP, dv.Scale(-1))

	dR := back.R.Sub(s.R).Norm()
	dV := back.V.Sub(s.V).Norm()
	// Floating point noise at AU-scale; use relative tolerances keyed to magnitudes.
	if dR > oldP.Norm()*1e-13 {
		t.Errorf("Rebase R round-trip: %.3e m (tol %.3e)", dR, oldP.Norm()*1e-13)
	}
	if dV > 1e-10 {
		t.Errorf("Rebase V round-trip: %.3e m/s", dV)
	}
}
