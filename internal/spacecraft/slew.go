package spacecraft

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// slewAngleEps is the angular tolerance (radians) below which the nose
// is considered already aligned — snap to the commanded direction
// rather than emit a sub-nanoradian residual.
const slewAngleEps = 1e-9

// SlewRateRad returns the craft's attitude slew-rate cap in rad/s,
// falling back to DefaultSlewRateDegPerSec when the per-craft value is
// unset (zero). This is why legacy saves and bare Spacecraft{} test
// fixtures get a sane rate without a persisted field — the rate is a
// loadout/catalog property, re-applied on construction.
func (s *Spacecraft) SlewRateRad() float64 {
	deg := s.SlewRateDegPerSec
	if deg <= 0 {
		deg = DefaultSlewRateDegPerSec
	}
	return deg * math.Pi / 180
}

// SlewToward rotates CurrentAttitudeDir toward the commanded unit
// direction by at most SlewRate·dt radians, about their mutual
// perpendicular. dt is the warp-scaled sim-time elapsed this tick
// (constant angular velocity in sim-time — at very high warp dt is
// large enough that the slew completes in one tick, which is the
// accepted "effectively instant at high warp" behaviour).
//
// Guards:
//   - commanded ≈ 0 (undefined direction, e.g. pre-launch surface
//     mode): no-op, hold the current nose.
//   - CurrentAttitudeDir ≈ 0 (uninitialized: fresh spawn, legacy
//     save, never-ticked test craft): SNAP to commanded. This is the
//     load-bearing init guard — it prevents slewing from a garbage
//     vector and prevents a nose teleport on a pre-v0.10.0 reload.
//   - already within slewAngleEps, or the cap covers the whole
//     angle: snap exactly to commanded (clean convergence, no
//     residual jitter).
//   - antiparallel (180°, degenerate cross product): pick an
//     arbitrary unit perpendicular so the rotation axis is defined.
//
// v0.10.0+.
func (s *Spacecraft) SlewToward(commanded orbital.Vec3, dt float64) {
	cmd := commanded.Unit()
	if cmd == (orbital.Vec3{}) {
		return // undefined commanded direction — hold attitude
	}
	cur := s.CurrentAttitudeDir.Unit()
	if cur == (orbital.Vec3{}) {
		s.CurrentAttitudeDir = cmd // init snap
		return
	}

	cosA := cur.Dot(cmd)
	if cosA > 1 {
		cosA = 1
	} else if cosA < -1 {
		cosA = -1
	}
	ang := math.Acos(cosA)
	maxStep := s.SlewRateRad() * dt
	if ang <= slewAngleEps || ang <= maxStep {
		s.CurrentAttitudeDir = cmd // converged this tick
		return
	}

	axis := cur.Cross(cmd)
	if axis.Norm() == 0 {
		// Antiparallel: any perpendicular to cur works. Try cur×Z;
		// if cur ∥ Z, fall back to cur×X.
		axis = cur.Cross(orbital.Vec3{Z: 1})
		if axis.Norm() == 0 {
			axis = cur.Cross(orbital.Vec3{X: 1})
		}
	}
	s.CurrentAttitudeDir = orbital.Rotate(cur, axis.Unit(), maxStep).Unit()
}
