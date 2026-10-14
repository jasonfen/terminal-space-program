package orbital

import "math"

// PortAxisRadialDeg is how close to radial a port axis may point before
// "up across the port face" switches from RadialOut to AlongTrack (ADR
// 0053 decision 7): RadialOut projected off an axis it nearly coincides
// with is numerical noise.
const PortAxisRadialDeg = 10.0

// PortFaceBasis is the pair of directions across a port's face, in world
// axes: up/down and left/right as a pilot looking at the port sees them.
// axis is the unit vector the port faces out along (the target's nose
// direction for its port); f is the target's LVLH frame.
//
//	up    = RadialOut with its axis component removed, normalised
//	        (AlongTrack instead when the axis is within 10 degrees of radial)
//	right = perpendicular to both axis and up, signed to agree with
//	        CrossTrack
//
// ADR 0053 decision 7 words "right" as CrossTrack with the axis component
// removed. That agrees with this whenever the axis lies in the
// radial/along-track plane (TestPortFaceBasisVsADRWording measures the
// gap elsewhere), and this stays orthogonal to up when it does not, which
// matters because the off-axis readout splits one offset onto the two
// directions and the PORT-frame keys push along them. One function serves
// both (slice A's readout, slice C's keys). ok is false only for a zero
// axis.
func PortFaceBasis(axis Vec3, f LVLHFrame) (up, right Vec3, ok bool) {
	a := axis.Unit()
	if a.Norm() == 0 {
		return Vec3{}, Vec3{}, false
	}
	seed := f.RadialOut
	if math.Abs(a.Dot(f.RadialOut)) > math.Cos(PortAxisRadialDeg*math.Pi/180) {
		seed = f.AlongTrack
	}
	up = seed.Sub(a.Scale(seed.Dot(a))).Unit()
	right = up.Cross(a).Unit()
	if right.Dot(f.CrossTrack) < 0 {
		right = right.Scale(-1)
	}
	return up, right, true
}
