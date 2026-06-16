package orbital

import (
	"math"
	"testing"
)

// TestTrueAnomalyAtRoundTripsPosition: ν read back from a position on the
// conic reproduces the ν that generated it, for both an ellipse and a
// hyperbola at a few inclinations.
func TestTrueAnomalyAtRoundTripsPosition(t *testing.T) {
	cases := []Elements{
		{A: 7e6, E: 0.3, I: 0, Omega: 0, Arg: 0},
		{A: 8e6, E: 0.6, I: 0.4, Omega: 1.1, Arg: 2.0},
		{A: -1e7, E: 1.5, I: 0.3, Omega: 0.7, Arg: 1.3}, // hyperbola, a<0
		{A: -2e7, E: 2.4, I: 1.2, Omega: 2.5, Arg: 0.2},
	}
	for _, el := range cases {
		for _, nu := range []float64{-1.0, -0.3, 0, 0.5, 1.2} {
			p := PositionAtTrueAnomaly(el, nu)
			if p == (Vec3{}) {
				t.Fatalf("PositionAtTrueAnomaly(%+v, %.2f) degenerate", el, nu)
			}
			got := TrueAnomalyAt(el, p)
			if math.Abs(got-nu) > 1e-9 {
				t.Errorf("TrueAnomalyAt round-trip (e=%.1f, ν=%.2f) = %.6f, want %.6f", el.E, nu, got, nu)
			}
		}
	}
}

// TestSampleConicArcEndpointsAndCount: the sampler returns n+1 points whose
// first and last sit exactly on the requested true anomalies.
func TestSampleConicArcEndpointsAndCount(t *testing.T) {
	el := Elements{A: -1.2e7, E: 1.6, I: 0.5, Omega: 0.3, Arg: 1.0}
	nuIn, nuOut := -0.8, 0.8
	pts := SampleConicArc(el, nuIn, nuOut, 96)
	if len(pts) != 97 {
		t.Fatalf("got %d points, want 97", len(pts))
	}
	if got := TrueAnomalyAt(el, pts[0]); math.Abs(got-nuIn) > 1e-9 {
		t.Errorf("first point ν = %.6f, want %.6f", got, nuIn)
	}
	if got := TrueAnomalyAt(el, pts[len(pts)-1]); math.Abs(got-nuOut) > 1e-9 {
		t.Errorf("last point ν = %.6f, want %.6f", got, nuOut)
	}
}

// TestSampleConicArcDenseAtPeriapsis: eccentric-anomaly spacing puts the
// shortest chords at periapsis (ν=0) and longer ones toward the SOI edge, so
// the high-curvature bottom of a hyperbola is the best-resolved part — the
// opposite of equal-time sampling, which starves a fast perilune.
func TestSampleConicArcDenseAtPeriapsis(t *testing.T) {
	el := Elements{A: -1e7, E: 2.0} // equatorial hyperbola, periapsis on +x
	pts := SampleConicArc(el, -1.0, 1.0, 40)
	if len(pts) < 5 {
		t.Fatalf("too few points: %d", len(pts))
	}
	// Chord straddling periapsis (around the middle index) vs a chord near the
	// outbound edge (near the end).
	mid := len(pts) / 2
	periChord := pts[mid].Sub(pts[mid-1]).Norm()
	edgeChord := pts[len(pts)-1].Sub(pts[len(pts)-2]).Norm()
	if periChord >= edgeChord {
		t.Errorf("periapsis chord %.0f m not shorter than edge chord %.0f m — not dense at periapsis", periChord, edgeChord)
	}
}

// TestSampleConicArcEllipse: works for a bound orbit too (the in-SOI escaping
// case can be elliptic), with even-eccentric-anomaly spacing.
func TestSampleConicArcEllipse(t *testing.T) {
	el := Elements{A: 9e6, E: 0.4, I: 0.6, Omega: 1.0, Arg: 0.5}
	pts := SampleConicArc(el, -0.9, 1.1, 32)
	if len(pts) != 33 {
		t.Fatalf("got %d points, want 33", len(pts))
	}
	for i, p := range pts {
		if p == (Vec3{}) {
			t.Fatalf("point %d is zero — degenerate", i)
		}
	}
}

// TestSecsFromPeriapsisMatchesHyperbolicTOF: the signed time-from-periapsis
// equals −TimeToPeriapsisHyperbolic for a state built at that ν (the analytic
// arc relies on this to clock each sample's true inertial position).
func TestSecsFromPeriapsisMatchesHyperbolicTOF(t *testing.T) {
	const mu = 4.9e12
	el := Elements{A: -1.5e7, E: 1.8, I: 0.4, Omega: 0.6, Arg: 0.9}
	for _, nu := range []float64{-1.0, -0.4, 0.0, 0.7, 1.1} {
		r := PositionAtTrueAnomaly(el, nu)
		v := VelocityAtTrueAnomaly(el, nu, mu)
		tToPeri, ok := TimeToPeriapsisHyperbolic(Vec3State{R: r, V: v}, mu)
		if !ok {
			t.Fatalf("TimeToPeriapsisHyperbolic failed at ν=%.2f", nu)
		}
		got := SecsFromPeriapsisAt(el, nu, mu)
		want := -tToPeri // time-to-peri is negative once past; since-peri flips it
		if math.Abs(got-want) > 1e-2*math.Max(1, math.Abs(want)) {
			t.Errorf("ν=%.2f: SecsFromPeriapsisAt=%.3fs, want %.3fs", nu, got, want)
		}
	}
}

// TestSampleConicArcDegenerate: parabolic / zero-a / n<1 bail to nil so the
// caller keeps its integrated arc.
func TestSampleConicArcDegenerate(t *testing.T) {
	if pts := SampleConicArc(Elements{A: 1e7, E: 1.0}, -0.5, 0.5, 10); pts != nil {
		t.Errorf("parabolic e=1 should return nil, got %d points", len(pts))
	}
	if pts := SampleConicArc(Elements{A: 0, E: 0.5}, -0.5, 0.5, 10); pts != nil {
		t.Errorf("a=0 should return nil, got %d points", len(pts))
	}
	if pts := SampleConicArc(Elements{A: -1e7, E: 1.5}, -0.5, 0.5, 0); pts != nil {
		t.Errorf("n<1 should return nil, got %d points", len(pts))
	}
}
