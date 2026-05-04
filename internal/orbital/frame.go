package orbital

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

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

// Cross returns a × b.
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}

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

// BodyFrame is an orthonormal basis (Ex, Ey, Ez) expressed in world
// inertial coordinates. Ez is the body's spin axis; (Ex, Ey) span the
// equatorial plane. ToWorld rotates a vector from the body-equatorial
// basis into the world basis; FromWorld is the inverse.
//
// v0.8.6+: lets the orbital, sim, and planner layers express elements
// (i, Ω, ω) relative to the primary body's equator — the convention
// used by every operational mission planner. A 0° inclination Earth
// orbit lies in Earth's equatorial plane (passes over the equator).
// Heliocentric orbits use the ecliptic-aligned IdentityFrame instead;
// see ReferenceFrameForPrimary.
type BodyFrame struct {
	Ex, Ey, Ez Vec3
}

// IdentityFrame is the world inertial frame: Ex=+X, Ey=+Y, Ez=+Z.
// Used as the orbital reference frame for heliocentric orbits, where
// the ecliptic plane and the world XY plane coincide by construction.
func IdentityFrame() BodyFrame {
	return BodyFrame{
		Ex: Vec3{X: 1},
		Ey: Vec3{Y: 1},
		Ez: Vec3{Z: 1},
	}
}

// BodyEquatorialFrame returns the inertial frame whose Z-axis is
// b's spin axis. Mirrors render.BodyRotationAxisWorld /
// BodyRingBasisWorld so renderer and orbital math agree on which
// way the body is tilted.
//
// Construction:
//   - Ez = (sin t·cos a, sin t·sin a, cos t) — body spin axis,
//     where t = AxialTilt, a = AxialAzimuth (both in degrees).
//   - Ex = projection of world +X onto the equatorial plane,
//     normalised. Falls back to projection of world +Y when +X is
//     near-parallel to Ez (high-tilt + azimuth-0 body).
//   - Ey = Ez × Ex, completing a right-handed orthonormal basis.
//
// At AxialTilt = 0 this collapses to IdentityFrame.
func BodyEquatorialFrame(b bodies.CelestialBody) BodyFrame {
	tiltRad := b.AxialTilt * math.Pi / 180.0
	azRad := b.AxialAzimuth * math.Pi / 180.0
	sinT := math.Sin(tiltRad)
	cosT := math.Cos(tiltRad)
	ez := Vec3{X: sinT * math.Cos(azRad), Y: sinT * math.Sin(azRad), Z: cosT}

	ref := Vec3{X: 1}
	if math.Abs(ez.X*ref.X+ez.Y*ref.Y+ez.Z*ref.Z) > 0.999 {
		ref = Vec3{Y: 1}
	}
	exRaw := ref.Sub(ez.Scale(ez.X*ref.X + ez.Y*ref.Y + ez.Z*ref.Z))
	exNorm := exRaw.Norm()
	if exNorm < 1e-12 {
		return IdentityFrame()
	}
	ex := exRaw.Scale(1 / exNorm)
	ey := ez.Cross(ex)
	return BodyFrame{Ex: ex, Ey: ey, Ez: ez}
}

// ReferenceFrameForPrimary returns the orbital reference frame for
// orbits about the given primary. Heliocentric orbits (primary is the
// Sun) use the ecliptic = IdentityFrame, matching the astronomical
// convention that planet inclinations are quoted relative to the
// ecliptic. Body-bound orbits use the primary's equatorial frame,
// matching the operational mission-planning convention (ECI for Earth,
// MCI for Mars, etc.).
func ReferenceFrameForPrimary(primary bodies.CelestialBody) BodyFrame {
	if primary.ID == "sun" {
		return IdentityFrame()
	}
	return BodyEquatorialFrame(primary)
}

// ToWorld rotates v from the body-equatorial basis to the world
// inertial basis: v_world = vx·Ex + vy·Ey + vz·Ez.
func (f BodyFrame) ToWorld(v Vec3) Vec3 {
	return Vec3{
		X: v.X*f.Ex.X + v.Y*f.Ey.X + v.Z*f.Ez.X,
		Y: v.X*f.Ex.Y + v.Y*f.Ey.Y + v.Z*f.Ez.Y,
		Z: v.X*f.Ex.Z + v.Y*f.Ey.Z + v.Z*f.Ez.Z,
	}
}

// FromWorld rotates v from the world basis into this frame: it's the
// transpose of ToWorld (v_frame = (Ex·v, Ey·v, Ez·v)).
func (f BodyFrame) FromWorld(v Vec3) Vec3 {
	return Vec3{
		X: f.Ex.X*v.X + f.Ex.Y*v.Y + f.Ex.Z*v.Z,
		Y: f.Ey.X*v.X + f.Ey.Y*v.Y + f.Ey.Z*v.Z,
		Z: f.Ez.X*v.X + f.Ez.Y*v.Y + f.Ez.Z*v.Z,
	}
}
