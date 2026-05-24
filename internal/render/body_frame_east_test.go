package render

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func vecClose(a, b Vec3, tol float64) bool {
	return math.Abs(a.X-b.X) < tol && math.Abs(a.Y-b.Y) < tol && math.Abs(a.Z-b.Z) < tol
}

// At the equator of a non-tilted body, the local east at the (R, 0, 0)
// point points in world +Y (right-hand rule, spin axis = +Z).
func TestBodyFrameEastEquatorZeroTilt(t *testing.T) {
	b := bodies.CelestialBody{AxialTilt: 0, AxialAzimuth: 0}
	r := Vec3{1, 0, 0}
	east := BodyFrameEast(b, r)
	want := Vec3{0, 1, 0}
	if !vecClose(east, want, 1e-12) {
		t.Errorf("east = %+v, want %+v", east, want)
	}
}

// East is always perpendicular to local-up (r̂) and is a unit vector,
// for any point off the spin axis. Sample across latitudes on a
// tilted body to flush out frame-mixing bugs.
func TestBodyFrameEastIsUnitAndPerpendicularToLocalUp(t *testing.T) {
	b := bodies.CelestialBody{AxialTilt: 23.44, AxialAzimuth: 0}
	for _, latDeg := range []float64{0, 30, -45, 60, 80} {
		phi := latDeg * math.Pi / 180.0
		r := Vec3{math.Cos(phi), 0, math.Sin(phi)}
		east := BodyFrameEast(b, r)
		mag := math.Sqrt(dot(east, east))
		if math.Abs(mag-1) > 1e-12 {
			t.Errorf("lat=%g: |east| = %g, want 1", latDeg, mag)
		}
		if perp := dot(east, normalize(r)); math.Abs(perp) > 1e-12 {
			t.Errorf("lat=%g: east·r̂ = %g, want 0", latDeg, perp)
		}
	}
}

// At the exact pole the canonical cross product collapses. Fallback
// must return a finite unit vector still perpendicular to local-up
// — non-degenerate, even if the direction is no longer the "true"
// ground east (no such direction exists at the pole).
func TestBodyFrameEastAtNorthPoleFallsBack(t *testing.T) {
	b := bodies.CelestialBody{AxialTilt: 0, AxialAzimuth: 0}
	r := Vec3{0, 0, 1} // exact +Z, parallel to spin axis
	east := BodyFrameEast(b, r)
	mag := math.Sqrt(dot(east, east))
	if math.IsNaN(mag) || math.IsInf(mag, 0) {
		t.Fatalf("east magnitude non-finite: %g", mag)
	}
	if math.Abs(mag-1) > 1e-12 {
		t.Errorf("|east| = %g, want 1", mag)
	}
	if perp := dot(east, normalize(r)); math.Abs(perp) > 1e-12 {
		t.Errorf("east·r̂ = %g, want 0", perp)
	}
}

