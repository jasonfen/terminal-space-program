package orbital

// LVLH (local-vertical / local-horizontal) is the frame proximity
// operations are flown in: it rides with the target vehicle rather than
// with the stars, so a steady approach reads as a steady picture. The
// world-frame OrbitView cannot hold that picture still — at close range
// both vehicles sweep the canvas at orbital speed while the metres
// between them stay sub-pixel — which is what ADR 0043's Proximity View
// exists to fix.
//
// This file is the frame math only: the basis and its degenerate cases.
// Who is at the centre, what gets drawn, and how the canvas is fitted are
// the sim and screen layers' business (internal/sim/proximity.go,
// internal/tui/screens/orbit_proximity.go).

// LVLHFrame is an orthonormal basis expressed in the same inertial axes
// as the (r, v) it was built from — the primary-relative frame, which
// shares its axes with the world frame (ToPlanetCentric is a pure
// translation), so the vectors can be handed straight to a canvas basis
// without a second rotation.
//
// The three axes, in the NASA rendezvous vocabulary a pilot already has
// words for:
//
//   - RadialOut (r̂) — the "R-bar" axis, pointing away from the primary.
//     Its negative points at the primary, which is why the screen puts
//     it DOWN: down is toward the planet, the way it looks out a window.
//   - AlongTrack — the "V-bar" axis: the target's velocity with the
//     radial component projected out, so it is exactly in-plane and
//     exactly perpendicular to RadialOut. Screen-right. Approaching from
//     behind therefore slides in from the left.
//   - CrossTrack (ĥ) — the orbit normal, completing the right-handed set
//     (RadialOut × AlongTrack = CrossTrack). With along-track drawn
//     screen-right and radial-out screen-up, ĥ points INTO the screen:
//     the camera sits on the −ĥ side looking along the orbit normal,
//     which is exactly where the standard V-bar / R-bar rendezvous
//     diagram puts the reader. This slice draws nothing along it, but the
//     depth sign is what a later out-of-plane cue would read.
//
// The basis is a function of the target's CURRENT state alone, so a
// caller that rebuilds it every frame gets a frame that rotates with the
// target's orbit — which is the point: relative drift then curves the way
// the physics curves, instead of unrolling into an inertial spiral.
type LVLHFrame struct {
	AlongTrack Vec3
	RadialOut  Vec3
	CrossTrack Vec3
}

// LVLHBasis builds the target-centred LVLH frame from the target's
// primary-relative position and velocity.
//
// ok=false for the two degenerate states where the frame is genuinely
// undefined rather than merely awkward: a zero position vector (the
// target is AT the primary's centre — no local vertical), and a velocity
// parallel to the position vector (a purely radial fall, or a zero
// velocity — no local horizontal). Callers render a refusal instead of a
// frame; silently substituting world axes would draw a picture whose
// axis labels lie, which is worse than saying so.
func LVLHBasis(r, v Vec3) (LVLHFrame, bool) {
	rn := r.Norm()
	if rn == 0 {
		return LVLHFrame{}, false
	}
	radialOut := r.Scale(1 / rn)

	// h = r × v is the orbit normal; its magnitude is zero exactly when v
	// is parallel to r (including v = 0), which is the radial-fall case.
	h := r.Cross(v)
	hn := h.Norm()
	if hn == 0 {
		return LVLHFrame{}, false
	}
	crossTrack := h.Scale(1 / hn)

	// ĥ × r̂ is v̂ with the radial component removed and re-normalised in
	// one step — both inputs are unit and orthogonal, so the cross product
	// is already unit. Taking it this way rather than projecting v
	// explicitly keeps the three axes exactly orthonormal at FP precision,
	// which matters because the canvas projects onto (AlongTrack,
	// RadialOut) and a non-orthogonal pair would shear the picture.
	alongTrack := crossTrack.Cross(radialOut)

	return LVLHFrame{
		AlongTrack: alongTrack,
		RadialOut:  radialOut,
		CrossTrack: crossTrack,
	}, true
}

// ToFrame expresses a world-axes vector in LVLH components:
// (along-track, radial-out, cross-track). The inverse of the basis
// combination — useful to a readout that wants to say "200 m behind,
// 30 m below" rather than plot a point.
func (f LVLHFrame) ToFrame(v Vec3) Vec3 {
	return Vec3{
		X: f.AlongTrack.Dot(v),
		Y: f.RadialOut.Dot(v),
		Z: f.CrossTrack.Dot(v),
	}
}
