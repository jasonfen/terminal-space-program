package render

import "math"

// Eclipse / shadow cones (v0.9.6 Phase B). When a body passes into
// the umbra/penumbra its parent casts away from the Sun, it dims
// globally on top of the per-pixel terminator — the "blood moon"
// lunar eclipse, and Galilean-moon eclipses behind Jupiter.
//
// Geometry, mirroring internal/physics/soi.go's distance/radius/ratio
// style (numbers in, number out — no spherical projection). The Sun
// is at the inertial origin (sol.json puts the star's
// semimajorAxis=0), so the shadow axis is just the Sun→occluder
// direction.
//
// For a spherical source (Sun, radius R_s) and spherical occluder
// (radius R_o, center at distance d), the umbra is the cone bounded
// by the external tangents; its radius shrinks linearly with axial
// distance behind the occluder and reaches zero at the umbra apex
// (distance R_o·d/(R_s−R_o) behind the occluder). The penumbra is
// bounded by the internal tangents and widens. At the occluder both
// equal R_o:
//
//	rUmbra(s)    = R_o − (s−d)·(R_s−R_o)/d   (clamped ≥ 0)
//	rPenumbra(s) = R_o + (s−d)·(R_s+R_o)/d
//
// With real Sol radii this yields ~4600 km Earth-umbra radius at
// Luna's distance — the physically correct total-lunar-eclipse
// geometry — so no fudge constant is needed; the eclipse-oracle test
// pins this.

// EclipseFactor returns a global dim factor in [umbraFloor, 1] for a
// body centered at bodyPos with radius bodyR, whose occluder (its
// parent / the larger sunward body) is centered at occPos with radius
// occR, with the Sun of radius sunR at the origin. 1 = fully lit;
// umbraFloor = deep total umbra; the penumbra band interpolates with
// a smoothstep. Degenerate inputs (occluder at the Sun, body sunward
// of the occluder) return 1 — no eclipse — so planets, whose
// "parent" is the star itself, are never spuriously shadowed.
func EclipseFactor(bodyPos, occPos Vec3, bodyR, occR, sunR float64) float64 {
	d := math.Sqrt(occPos.X*occPos.X + occPos.Y*occPos.Y + occPos.Z*occPos.Z)
	if d < 1e-6 || sunR <= occR || occR <= 0 {
		// Occluder at the Sun (e.g. a planet whose parent is the
		// star), or non-physical radii — cannot cast a shadow here.
		return 1
	}
	axis := normalize(occPos) // Sun → occluder, unit
	s := dot(bodyPos, axis)   // body's axial distance from the Sun
	if s <= d {
		// Body is level with or sunward of the occluder — it cannot
		// be in the occluder's shadow.
		return 1
	}
	behind := s - d
	// Perpendicular miss distance from the shadow axis.
	perp := Vec3{
		X: bodyPos.X - axis.X*s,
		Y: bodyPos.Y - axis.Y*s,
		Z: bodyPos.Z - axis.Z*s,
	}
	m := math.Sqrt(perp.X*perp.X + perp.Y*perp.Y + perp.Z*perp.Z)

	rUmbra := occR - behind*(sunR-occR)/d
	if rUmbra < 0 {
		rUmbra = 0 // past the umbra apex — only penumbra remains
	}
	rPenumbra := occR + behind*(sunR+occR)/d

	// Hard bounds (also the endpoints of the interpolation band):
	//   m + bodyR < rUmbra      → body wholly inside umbra
	//   m − bodyR > rPenumbra   → body wholly outside penumbra
	lo := rUmbra - bodyR
	hi := rPenumbra + bodyR
	if m <= lo {
		return umbraFloor
	}
	if m >= hi {
		return 1
	}
	// Body straddles the shadow boundary: ramp from full umbra
	// (umbraFloor) to fully lit (1) with a smoothstep on the miss
	// distance across the body's crossing band.
	t := (m - lo) / (hi - lo)
	smooth := t * t * (3 - 2*t)
	return umbraFloor + (1-umbraFloor)*smooth
}
