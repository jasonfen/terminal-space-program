package render

import (
	"math"
	"math/rand"
	"testing"
)

// Real Sol radii / distances in meters — the eclipse-oracle anchor.
// If the cone math is miscalibrated these constants make lunar
// eclipses either constant or impossible, so this is the guard the
// plan flagged as mandatory-first.
const (
	sunRm       = 6.957e8  // 695,700 km
	earthRm     = 6.371e6  // 6,371 km
	moonRm      = 1.7374e6 // 1,737.4 km
	earthSunDm  = 1.496e11 // 1 AU
	earthMoonDm = 3.844e8  // 384,400 km
)

// TestEclipseOracleLunar is the calibration anchor: with real Sol
// radii, the Moon directly anti-solar of Earth at its true distance
// must fall in total umbra (→ umbraFloor); the Moon beside Earth (new
// moon / not behind it) must be fully lit (→ 1).
func TestEclipseOracleLunar(t *testing.T) {
	earth := Vec3{X: earthSunDm}
	// Full lunar eclipse: Moon on the Sun→Earth axis, beyond Earth.
	moonEclipsed := Vec3{X: earthSunDm + earthMoonDm}
	if f := EclipseFactor(moonEclipsed, earth, moonRm, earthRm, sunRm); math.Abs(f-umbraFloor) > 1e-9 {
		t.Errorf("total lunar eclipse factor = %v, want umbraFloor %v", f, umbraFloor)
	}
	// Moon to the side of Earth (perpendicular to the Sun line) —
	// it is level with Earth, not behind it: no eclipse.
	moonBeside := Vec3{X: earthSunDm, Y: earthMoonDm}
	if f := EclipseFactor(moonBeside, earth, moonRm, earthRm, sunRm); f != 1 {
		t.Errorf("new-moon (beside) factor = %v, want 1", f)
	}
	// Moon behind Earth but far off the shadow axis (well outside
	// the penumbra): no eclipse.
	moonOffAxis := Vec3{X: earthSunDm + earthMoonDm, Y: 5e7}
	if f := EclipseFactor(moonOffAxis, earth, moonRm, earthRm, sunRm); f != 1 {
		t.Errorf("off-axis Moon factor = %v, want 1", f)
	}
}

func TestEclipseFactorSunwardNoEclipse(t *testing.T) {
	occ := Vec3{X: earthSunDm}
	// Body between Sun and occluder (sunward) — cannot be shadowed.
	body := Vec3{X: earthSunDm - 1e8}
	if f := EclipseFactor(body, occ, moonRm, earthRm, sunRm); f != 1 {
		t.Errorf("sunward body factor = %v, want 1", f)
	}
}

func TestEclipseFactorDegenerateOccluder(t *testing.T) {
	// Occluder at the Sun (origin) — e.g. a planet whose "parent"
	// is the star. Must never spuriously eclipse.
	if f := EclipseFactor(Vec3{X: earthSunDm}, Vec3{}, earthRm, sunRm, sunRm); f != 1 {
		t.Errorf("occluder-at-sun factor = %v, want 1", f)
	}
}

func TestEclipseFactorPenumbraMonotonic(t *testing.T) {
	// Sweep the miss distance outward at the Moon's axial depth: the
	// factor must rise monotonically from umbraFloor (deep umbra) to
	// 1 (clear of the penumbra), all within range.
	earth := Vec3{X: earthSunDm}
	prev := -1.0
	for mKm := 0.0; mKm <= 12000; mKm += 200 {
		body := Vec3{X: earthSunDm + earthMoonDm, Y: mKm * 1000}
		f := EclipseFactor(body, earth, moonRm, earthRm, sunRm)
		if f < umbraFloor-1e-9 || f > 1+1e-9 {
			t.Fatalf("factor out of range at miss=%.0fkm: %v", mKm, f)
		}
		if f < prev-1e-9 {
			t.Fatalf("factor not monotonic at miss=%.0fkm: %v < %v", mKm, f, prev)
		}
		prev = f
	}
	if prev < 1-1e-9 {
		t.Errorf("factor never reached full light (max %v)", prev)
	}
}

func TestEclipseFactorRangeFuzz(t *testing.T) {
	earth := Vec3{X: earthSunDm}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 1000; i++ {
		body := Vec3{
			X: earthSunDm + (rng.Float64()-0.3)*2*earthMoonDm,
			Y: (rng.Float64() - 0.5) * 4e7,
			Z: (rng.Float64() - 0.5) * 4e7,
		}
		f := EclipseFactor(body, earth, moonRm, earthRm, sunRm)
		if f < umbraFloor-1e-9 || f > 1+1e-9 {
			t.Fatalf("factor out of [%v,1]: %v at %+v", umbraFloor, f, body)
		}
	}
}
