package orbital

import "math"

// Vec3 is a 3-element inertial-frame vector (meters or m/s).
type Vec3 struct{ X, Y, Z float64 }

// Add returns a + b.
func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Sub returns a − b.
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// Scale returns a * s.
func (a Vec3) Scale(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

// Norm returns |a|.
func (a Vec3) Norm() float64 { return math.Sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z) }

// PerifocalToInertial converts perifocal (p, q, w) → inertial (X, Y, Z) via
// the standard 3-1-3 Euler sequence (Ω, i, ω). Perifocal frame: p along
// periapsis, q 90° ahead in the orbital plane, w = p × q.
func PerifocalToInertial(p, q float64, el Elements) Vec3 {
	cO, sO := math.Cos(el.Omega), math.Sin(el.Omega)
	cw, sw := math.Cos(el.Arg), math.Sin(el.Arg)
	ci, si := math.Cos(el.I), math.Sin(el.I)

	// Rotation matrix columns (perifocal basis in inertial coords).
	// See Vallado §2.6 or Curtis §4.4.
	px := cO*cw - sO*sw*ci
	py := sO*cw + cO*sw*ci
	pz := sw * si
	qx := -cO*sw - sO*cw*ci
	qy := -sO*sw + cO*cw*ci
	qz := cw * si

	return Vec3{
		X: p*px + q*qx,
		Y: p*py + q*qy,
		Z: p*pz + q*qz,
	}
}

// PositionAtTrueAnomaly computes the inertial position for a body with
// given orbital elements and true anomaly ν. r = a(1-e²)/(1+e·cos ν).
func PositionAtTrueAnomaly(el Elements, nu float64) Vec3 {
	if el.A == 0 {
		return Vec3{}
	}
	p := el.A * (1 - el.E*el.E)
	if p == 0 {
		return Vec3{}
	}
	r := p / (1 + el.E*math.Cos(nu))
	pf := r * math.Cos(nu)
	qf := r * math.Sin(nu)
	return PerifocalToInertial(pf, qf, el)
}

// VelocityAtTrueAnomaly computes the inertial velocity vector at true anomaly
// ν, given the central body's gravitational parameter mu (m³/s²). Returns
// zero vector for degenerate (a=0) orbits (e.g. the system primary itself).
func VelocityAtTrueAnomaly(el Elements, nu, mu float64) Vec3 {
	if el.A == 0 || mu == 0 {
		return Vec3{}
	}
	p := el.A * (1 - el.E*el.E)
	if p <= 0 {
		return Vec3{}
	}
	h := math.Sqrt(mu * p) // specific angular momentum
	vp := -mu / h * math.Sin(nu)
	vq := mu / h * (el.E + math.Cos(nu))
	return PerifocalToInertial(vp, vq, el)
}

// ToPlanetCentric rebases an inertial position r_inertial into a frame
// centered on the primary. This is subtraction in the same inertial basis
// (no rotation) — our planets are on fixed Keplerian tracks and don't
// define their own rotating frames in v0.1.
func ToPlanetCentric(rInertial, primaryInertial Vec3) Vec3 {
	return rInertial.Sub(primaryInertial)
}

// FromPlanetCentric is the inverse of ToPlanetCentric.
func FromPlanetCentric(rLocal, primaryInertial Vec3) Vec3 {
	return rLocal.Add(primaryInertial)
}
