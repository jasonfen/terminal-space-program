package spacecraft

import (
	"math"
	"testing"
)

// Phase 0 is byte-identical placement to the historical NewInLEO seed —
// the fleet reset's slot 0 matches what a fresh enrollment spawns.
func TestNewInLEOAtPhaseZeroMatchesNewInLEO(t *testing.T) {
	earth := testEarth()
	a, b := NewInLEO(earth), NewInLEOAtPhase(earth, 0)
	if a.State.R != b.State.R || a.State.V != b.State.V {
		t.Errorf("phase 0 state diverges from NewInLEO: R %v vs %v, V %v vs %v",
			a.State.R, b.State.R, a.State.V, b.State.V)
	}
	if b.LoadoutID != LoadoutSIVB1ID {
		t.Errorf("LoadoutID = %q, want %q", b.LoadoutID, LoadoutSIVB1ID)
	}
}

// Every phase sits on the same 500x500 circular orbit: same radius,
// same speed, velocity perpendicular to radius, and the orbit plane
// (angular-momentum direction) identical to the phase-0 seed's.
func TestNewInLEOAtPhaseSameCircularOrbit(t *testing.T) {
	earth := testEarth()
	seed := NewInLEO(earth)
	wantR := earth.RadiusMeters() + 500e3
	wantV := math.Sqrt(earth.GravitationalParameter() / wantR)
	seedH := seed.State.R.Cross(seed.State.V).Unit()

	for _, phase := range []float64{0, 45, 90, 120, 180, 270, 359} {
		c := NewInLEOAtPhase(earth, phase)
		r, v := c.State.R.Norm(), c.State.V.Norm()
		if math.Abs(r-wantR) > 1 { // 1 m tolerance
			t.Errorf("phase %.0f: |R| = %f, want %f", phase, r, wantR)
		}
		if math.Abs(v-wantV) > 1e-6 {
			t.Errorf("phase %.0f: |V| = %f, want %f", phase, v, wantV)
		}
		if dot := math.Abs(c.State.R.Unit().Dot(c.State.V.Unit())); dot > 1e-9 {
			t.Errorf("phase %.0f: R·V = %g, want ~0 (circular)", phase, dot)
		}
		h := c.State.R.Cross(c.State.V).Unit()
		if h.Sub(seedH).Norm() > 1e-9 {
			t.Errorf("phase %.0f: orbit plane normal %v differs from seed %v", phase, h, seedH)
		}
	}
}

// The phase angle actually separates craft: the chord between phase 0
// and phase th matches 2r·sin(th/2).
func TestNewInLEOAtPhaseSeparation(t *testing.T) {
	earth := testEarth()
	r := earth.RadiusMeters() + 500e3
	a := NewInLEOAtPhase(earth, 0)
	b := NewInLEOAtPhase(earth, 90)
	want := 2 * r * math.Sin(45*math.Pi/180)
	if got := a.State.R.Sub(b.State.R).Norm(); math.Abs(got-want) > 1 {
		t.Errorf("chord 0°→90° = %f, want %f", got, want)
	}
}
