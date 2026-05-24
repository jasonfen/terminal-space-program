// Surface-frame east helper for the ViewLaunch chase-cam projection
// (v0.11.0+). The launch view's projection-plane horizontal axis
// prefers the projection of the commanded attitude onto the local-
// horizontal plane, but falls back to body-frame east when the
// commanded direction is near-vertical (rocket sitting on the pad,
// just-after-liftoff, or any moment with attitude ~aligned to
// local-up).

package render

import (
	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// BodyFrameEast returns the local east unit vector in the world
// inertial frame at the surface point given by `r` (position vector
// from body centre; magnitude irrelevant — only direction matters).
//
// East is defined as the unit vector ẑ_body × r̂, where ẑ_body is the
// body's tilted spin axis (`BodyRotationAxisWorld(b)`). This gives a
// vector tangent to the surface, perpendicular to local-up, pointing
// in the prograde (eastward) direction of rotation.
//
// Pole-on degenerate guard (per v0.11 Slice 1 grill resolution): when
// the cross product magnitude shrinks below 1e-9 (caller is within
// ~57 nrad of the spin axis), falls back to projecting world +X onto
// the local-horizontal plane (the plane perpendicular to r̂). This is
// non-degenerate, deterministic, and safe — though world +X is
// inertial rather than body-fixed, so the fallback rotates relative
// to the ground at the body's spin rate. Acceptable for the pole-
// launch case Slice 1 does not optimise.
func BodyFrameEast(b bodies.CelestialBody, r Vec3) Vec3 {
	rHat := normalize(r)
	axis := BodyRotationAxisWorld(b)
	east := cross(axis, rHat)
	if dot(east, east) < 1e-18 {
		// Pole-on fallback: project world +X onto the local-horizontal
		// plane (perpendicular to r̂). If the projection itself
		// degenerates (caller passed r exactly parallel to +X), fall
		// back to world +Y projected the same way — one of the two
		// must survive in 3D.
		ref := Vec3{1, 0, 0}
		proj := add(ref, scale(rHat, -dot(ref, rHat)))
		if dot(proj, proj) < 1e-18 {
			ref = Vec3{0, 1, 0}
			proj = add(ref, scale(rHat, -dot(ref, rHat)))
		}
		return normalize(proj)
	}
	return normalize(east)
}
