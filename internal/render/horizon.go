// Horizon-curve math for the ViewLaunch chase-cam scene (v0.11.0+).
//
// The horizon is the visible boundary of a body's silhouette from the
// camera's viewpoint. For a sphere of radius R observed from distance
// d (measured to the body centre), the tangent cone from the camera
// has half-angle θ where sin(θ) = R/d. The silhouette in 3D is a
// circle of radius
//
//	r_sil = R · cos(θ) = R · √(d² - R²) / d
//
// sitting in the plane perpendicular to the camera-look direction at
// the locus of tangent points. Projected into the camera's projection
// plane (with origin at the camera, y along local-up, body centre at
// (0, -d)), the silhouette traces a circle of radius r_sil centred at
// (0, -d) — the upper half of which is the visible horizon arc and
// the lower half is occluded.

package render

import "math"

// HorizonSilhouetteRadius returns the apparent silhouette radius (in
// metres) of a body of radius `bodyRadius` as seen from a camera at
// `cameraDist` metres from the body centre. Returns 0 when the camera
// is inside or on the body's surface (d ≤ R).
func HorizonSilhouetteRadius(bodyRadius, cameraDist float64) float64 {
	if cameraDist <= bodyRadius {
		return 0
	}
	return bodyRadius * math.Sqrt(cameraDist*cameraDist-bodyRadius*bodyRadius) / cameraDist
}

// HorizonPoint is a single sample on the horizon curve in
// projection-plane metres. Origin is the camera (vessel) position;
// +x is the projection-plane horizontal axis (h_axis); +y is local-up
// (away from body centre).
type HorizonPoint struct{ X, Y float64 }

// HorizonCurve samples N points along the visible upper arc of the
// body's silhouette circle, evenly spaced in angle from the leftmost
// "shoulder" through the apex to the rightmost "shoulder". The circle
// sits in projection-plane coordinates centred at (0, -cameraDist)
// with radius `HorizonSilhouetteRadius(bodyRadius, cameraDist)`.
//
// Returns nil when the camera is inside the body or n < 2.
func HorizonCurve(bodyRadius, cameraDist float64, n int) []HorizonPoint {
	if n < 2 {
		return nil
	}
	r := HorizonSilhouetteRadius(bodyRadius, cameraDist)
	if r == 0 {
		return nil
	}
	pts := make([]HorizonPoint, n)
	for i := 0; i < n; i++ {
		theta := -math.Pi/2 + math.Pi*float64(i)/float64(n-1)
		pts[i] = HorizonPoint{
			X: r * math.Sin(theta),
			Y: -cameraDist + r*math.Cos(theta),
		}
	}
	return pts
}
