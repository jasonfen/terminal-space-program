package render

import (
	"math"
	"testing"
)

// HorizonSilhouetteRadius returns the apparent silhouette radius of a
// body of radius R as seen from camera distance d, using the locked
// formula R·√(d²-R²)/d. At d = 2R the silhouette is √3/2 · R.
func TestHorizonSilhouetteRadiusAtTwoBodyRadii(t *testing.T) {
	const R = 6371_000.0
	const d = 2 * R
	got := HorizonSilhouetteRadius(R, d)
	want := math.Sqrt(3) / 2 * R
	if math.Abs(got-want)/want > 1e-12 {
		t.Errorf("HorizonSilhouetteRadius(%g, %g) = %g, want %g", R, d, got, want)
	}
}

// At d ≤ R the camera is inside or on the surface; the silhouette
// degenerates. Return 0 rather than NaN / negative so callers can
// short-circuit cleanly.
func TestHorizonSilhouetteRadiusInsideBodyReturnsZero(t *testing.T) {
	const R = 6371_000.0
	if got := HorizonSilhouetteRadius(R, R); got != 0 {
		t.Errorf("at d = R: got %g, want 0", got)
	}
	if got := HorizonSilhouetteRadius(R, R*0.5); got != 0 {
		t.Errorf("inside body: got %g, want 0", got)
	}
}

// At very high altitude (d ≫ R) the silhouette radius approaches the
// body's actual radius R from below. Validates the asymptote.
func TestHorizonSilhouetteRadiusHighAltitudeApproachesR(t *testing.T) {
	const R = 6371_000.0
	const d = 1000 * R
	got := HorizonSilhouetteRadius(R, d)
	if got >= R || got/R < 0.9999 {
		t.Errorf("d=1000R: got %g (ratio %g), want ≈R from below", got, got/R)
	}
}

// HorizonCurve samples N points along the visible (upper) arc of the
// silhouette circle. Returns N points (not 2N — only the visible
// half), with origin at the camera, +y along local-up.
func TestHorizonCurvePointCount(t *testing.T) {
	const R = 6371_000.0
	const d = 2 * R
	pts := HorizonCurve(R, d, 33)
	if len(pts) != 33 {
		t.Errorf("len(pts) = %d, want 33", len(pts))
	}
}

// Each curve point lies on the silhouette circle: distance from circle
// centre (0, -d) equals r_sil within tight tolerance.
func TestHorizonCurvePointsLieOnSilhouetteCircle(t *testing.T) {
	const R = 6371_000.0
	const d = 3 * R
	r := HorizonSilhouetteRadius(R, d)
	pts := HorizonCurve(R, d, 32)
	for i, p := range pts {
		dx := p.X
		dy := p.Y - (-d)
		got := math.Sqrt(dx*dx + dy*dy)
		if math.Abs(got-r)/r > 1e-12 {
			t.Errorf("pt[%d] = (%g,%g): radius %g, want %g", i, p.X, p.Y, got, r)
		}
	}
}

// All curve points are on the visible upper arc — they sit at or
// above the circle's horizontal diameter (y ≥ -d), so the caller can
// flood-fill below the curve without worrying about the occluded arc.
func TestHorizonCurveIsUpperArcOnly(t *testing.T) {
	const R = 6371_000.0
	const d = 2.5 * R
	pts := HorizonCurve(R, d, 64)
	for i, p := range pts {
		if p.Y < -d-1e-6 {
			t.Errorf("pt[%d].Y = %g, want ≥ -d (%g) — visible arc only", i, p.Y, -d)
		}
	}
}

// Endpoints span the full width of the silhouette: leftmost point is
// at (-r, -d), rightmost at (+r, -d) (the horizon's edge against the
// body's "shoulders"). Validates the sampling covers the full arc.
func TestHorizonCurveSpansFullArc(t *testing.T) {
	const R = 6371_000.0
	const d = 2 * R
	r := HorizonSilhouetteRadius(R, d)
	pts := HorizonCurve(R, d, 33) // odd count so middle pt is the apex
	first, last := pts[0], pts[len(pts)-1]
	if math.Abs(first.X-(-r)) > 1e-6 || math.Abs(first.Y-(-d)) > 1e-6 {
		t.Errorf("first pt = (%g, %g), want (%g, %g)", first.X, first.Y, -r, -d)
	}
	if math.Abs(last.X-r) > 1e-6 || math.Abs(last.Y-(-d)) > 1e-6 {
		t.Errorf("last pt = (%g, %g), want (%g, %g)", last.X, last.Y, r, -d)
	}
}

// Camera inside the body: no horizon to draw. Returns nil so callers
// can skip the fill cleanly.
func TestHorizonCurveInsideBodyReturnsNil(t *testing.T) {
	const R = 6371_000.0
	if pts := HorizonCurve(R, R*0.5, 32); pts != nil {
		t.Errorf("inside body: got %d pts, want nil", len(pts))
	}
}
