package orbital

import "math"

// TrueAnomalyAt returns the true anomaly ν (radians, in (−π, π]) of an in-plane
// position r on the conic with elements el: it projects r onto the perifocal
// basis (x̂ toward periapsis, ŷ 90° prograde) and takes the angle. Any
// out-of-plane component of r is dropped. Used to read the ν of a known point
// so an analytic arc can be resampled between two endpoints — the SOI-pass
// arc's entry and exit (ADR 0023).
func TrueAnomalyAt(el Elements, r Vec3) float64 {
	xHat, yHat := PerifocalBasis(el)
	return math.Atan2(r.Dot(yHat), r.Dot(xHat))
}

// SampleConicArc returns n+1 positions along the conic `el` from true anomaly
// nuStart to nuEnd, spaced uniformly in ECCENTRIC anomaly — the hyperbolic
// eccentric anomaly H for e>1, the elliptic E for e<1. Even eccentric-anomaly
// steps concentrate points at periapsis, the highest-curvature part of the
// arc, so a hyperbolic flyby renders as a smooth curve instead of the few
// straight chords a uniform-time sampling leaves at a fast perilune (ADR 0023,
// the faceted-perilune fix). Each point is computed by PositionAtTrueAnomaly,
// so it shares the perifocal frame of el (the same body-relative frame the
// caller's elements were derived in). Returns nil for a degenerate conic
// (a==0, near-parabolic e≈1, n<1, or a point that hits the position guard) so
// the caller can keep its integrated arc.
func SampleConicArc(el Elements, nuStart, nuEnd float64, n int) []Vec3 {
	if n < 1 || el.A == 0 || math.Abs(el.E-1) < 1e-6 {
		return nil
	}
	eaStart := eccentricAnomalyFromTrue(nuStart, el.E)
	eaEnd := eccentricAnomalyFromTrue(nuEnd, el.E)
	if math.IsNaN(eaStart) || math.IsNaN(eaEnd) ||
		math.IsInf(eaStart, 0) || math.IsInf(eaEnd, 0) {
		return nil
	}
	pts := make([]Vec3, 0, n+1)
	for k := 0; k <= n; k++ {
		ea := eaStart + (eaEnd-eaStart)*float64(k)/float64(n)
		p := PositionAtTrueAnomaly(el, trueFromEccentricAnomaly(ea, el.E))
		if p == (Vec3{}) {
			return nil // hit PositionAtTrueAnomaly's degenerate guard — bail
		}
		pts = append(pts, p)
	}
	return pts
}

// eccentricAnomalyFromTrue maps true anomaly ν to eccentric anomaly: the
// elliptic E for e<1 (the numerically stable half-angle atan2 form) or the
// hyperbolic H for e>1 (tanh(H/2) = √((e−1)/(e+1))·tan(ν/2)).
func eccentricAnomalyFromTrue(nu, e float64) float64 {
	if e < 1 {
		return 2 * math.Atan2(math.Sqrt(1-e)*math.Sin(nu/2), math.Sqrt(1+e)*math.Cos(nu/2))
	}
	return 2 * math.Atanh(math.Sqrt((e-1)/(e+1))*math.Tan(nu/2))
}

// trueFromEccentricAnomaly inverts eccentricAnomalyFromTrue: the elliptic E
// back to ν via TrueAnomaly for e<1, or the hyperbolic H via
// tan(ν/2) = √((e+1)/(e−1))·tanh(H/2) for e>1.
func trueFromEccentricAnomaly(ea, e float64) float64 {
	if e < 1 {
		return TrueAnomaly(ea, e)
	}
	return 2 * math.Atan(math.Sqrt((e+1)/(e-1))*math.Tanh(ea/2))
}
