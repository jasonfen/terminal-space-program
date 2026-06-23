package physics

import "github.com/jasonfen/terminal-space-program/internal/orbital"

// Line-of-sight occlusion (v0.23 / ADR 0027, CommNet cycle 2). The one
// net-new physics primitive the comms subsystem needs: does a straight
// segment between two antennas pass through a body? Pure geometry,
// stateless, frame-agnostic — the caller supplies segment endpoints and
// body centres in one consistent frame (the active system's world frame).
// No atmosphere refraction and no minimum-elevation margin (ADR 0027 §6).

// OccluderBody is a body that can block a sightline: a sphere with a
// world-frame centre and radius. The caller builds these per tick from
// each body's current position + mean radius.
type OccluderBody struct {
	Center orbital.Vec3
	Radius float64
}

// RaySphereIntersect reports whether the segment [p, q] passes through the
// INTERIOR of the sphere centred at centre with radius r — i.e. the body
// blocks the sightline between p and q.
//
// "Interior" is deliberate (strict): an endpoint sitting exactly on the
// surface (a ground station at body radius) does NOT count as blocked, so
// a station with a target above its horizon keeps line of sight. The body
// blocks only when the segment actually dips inside it: an endpoint is
// strictly inside, or the perpendicular foot falls between the endpoints
// at a distance strictly less than r. A grazing tangent (foot distance
// exactly r) is treated as clear.
func RaySphereIntersect(p, q, centre orbital.Vec3, r float64) bool {
	if r <= 0 {
		return false
	}
	r2 := r * r
	// Either endpoint strictly inside the sphere → blocked (buried antenna).
	if pc := p.Sub(centre); pc.Dot(pc) < r2 {
		return true
	}
	if qc := q.Sub(centre); qc.Dot(qc) < r2 {
		return true
	}
	d := q.Sub(p)
	len2 := d.Dot(d)
	if len2 == 0 {
		return false // degenerate segment, both endpoints outside
	}
	// Project the centre onto the segment line; t is the foot parameter.
	t := centre.Sub(p).Dot(d) / len2
	if t <= 0 || t >= 1 {
		return false // closest approach is at an endpoint (both outside) → body is behind it
	}
	foot := p.Add(d.Scale(t))
	fc := foot.Sub(centre)
	return fc.Dot(fc) < r2
}

// SegmentOccludedByBody reports whether the segment [a, b] is blocked by
// any body in occluders (using each body's current position + radius). A
// link is occluded as soon as one body crosses it.
func SegmentOccludedByBody(a, b orbital.Vec3, occluders []OccluderBody) bool {
	for _, o := range occluders {
		if RaySphereIntersect(a, b, o.Center, o.Radius) {
			return true
		}
	}
	return false
}
