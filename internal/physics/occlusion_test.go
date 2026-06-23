package physics

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// A unit-ish sphere at the origin, radius 100, for the geometry cases.
func TestRaySphereIntersect(t *testing.T) {
	c := orbital.Vec3{}
	const r = 100
	cases := []struct {
		name string
		p, q orbital.Vec3
		want bool
	}{
		// Horizontal line at y=150, well above the radius-100 sphere — the
		// perpendicular foot is interior to the segment but 150 > r.
		{"clear above", orbital.Vec3{X: -200, Y: 150}, orbital.Vec3{X: 200, Y: 150}, false},
		// Straight through the centre.
		{"through centre", orbital.Vec3{X: -200}, orbital.Vec3{X: 200}, true},
		// Station on the surface, target directly overhead (radial) — the
		// foot is the station endpoint, so NOT occluded by its own body.
		{"radial overhead from surface", orbital.Vec3{X: 100}, orbital.Vec3{X: 300}, false},
		// Two antipodal surface points — the chord dips through the body.
		{"antipodal surface points", orbital.Vec3{X: 100}, orbital.Vec3{X: -100}, true},
		// Grazing tangent: perpendicular distance exactly r → NOT occluded
		// (strict interior test; no elevation margin per ADR 0027 §6).
		{"grazing tangent", orbital.Vec3{X: -200, Y: 100}, orbital.Vec3{X: 200, Y: 100}, false},
		// An endpoint buried inside the sphere.
		{"endpoint inside", orbital.Vec3{X: 50}, orbital.Vec3{X: 300}, true},
		// Both endpoints outside and on the same side — body is behind them.
		{"both outside same side", orbital.Vec3{X: 200}, orbital.Vec3{X: 200, Y: 200}, false},
		// Degenerate (zero-length) segment outside the sphere.
		{"degenerate outside", orbital.Vec3{X: 200}, orbital.Vec3{X: 200}, false},
	}
	for _, tc := range cases {
		if got := RaySphereIntersect(tc.p, tc.q, c, r); got != tc.want {
			t.Errorf("%s: RaySphereIntersect = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A zero / negative radius never occludes.
func TestRaySphereIntersectZeroRadius(t *testing.T) {
	if RaySphereIntersect(orbital.Vec3{X: -1}, orbital.Vec3{X: 1}, orbital.Vec3{}, 0) {
		t.Error("zero-radius sphere should never intersect")
	}
}

func TestSegmentOccludedByBody(t *testing.T) {
	a := orbital.Vec3{X: -500}
	b := orbital.Vec3{X: 500}
	// No body in the way.
	far := []OccluderBody{{Center: orbital.Vec3{Y: 1000}, Radius: 100}}
	if SegmentOccludedByBody(a, b, far) {
		t.Error("a body far off the segment should not occlude")
	}
	// One of several bodies sits across the segment.
	mixed := []OccluderBody{
		{Center: orbital.Vec3{Y: 1000}, Radius: 100}, // off to the side
		{Center: orbital.Vec3{}, Radius: 100},        // on the segment → blocks
	}
	if !SegmentOccludedByBody(a, b, mixed) {
		t.Error("a body across the segment should occlude")
	}
	// Empty occluder set never occludes.
	if SegmentOccludedByBody(a, b, nil) {
		t.Error("no occluders should never occlude")
	}
}

// TestRaySphereSurfaceTolerance (v0.22.1): a ground station's surface point is
// computed as radius×dir, where dir carries ~1e-12 of normalization error, so
// its distance from centre straddles the radius by microns. A strict interior
// test flickered such a station into "buried inside the body" on alternating
// ticks, self-occluding it (and every craft relying on it). The endpoint test
// now tolerates a point essentially on the surface, while still catching a
// genuinely sub-surface antenna.
func TestRaySphereSurfaceTolerance(t *testing.T) {
	c := orbital.Vec3{}
	const r = 6.371e6 // Earth-ish radius (metres), where the bug showed up
	up := orbital.Vec3{X: r + 1e6} // a craft 1000 km up — the link's other end

	// A surface point a hair *inside* r (well within the surface skin) is a
	// station on the ground, not a buried antenna → not occluded.
	nearSurface := orbital.Vec3{X: r * (1 - 1e-11)} // ~64 µm inside
	if RaySphereIntersect(nearSurface, up, c, r) {
		t.Error("a point a hair inside the surface (a ground station) must not be occluded")
	}
	// A point genuinely below the surface (10 m down) is still buried.
	deep := orbital.Vec3{X: r - 10}
	if !RaySphereIntersect(deep, up, c, r) {
		t.Error("a point well below the surface must be occluded")
	}
}
