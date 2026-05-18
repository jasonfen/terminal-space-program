package spacecraft

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
)

// RollStepDeg is the per-keypress commanded-roll adjustment (degrees)
// for the roll-left / roll-right keys. 15° matches the default slew
// rate's one-second budget so a tap visibly banks. v0.10.0+.
const RollStepDeg = 15.0

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

// headsUpReference returns the roll-zero "up" reference: the body's
// dorsal axis points as close to local vertical (radial-out, away
// from the primary) as the nose allows — i.e. heads-up / belly-down.
// When the nose is (anti)parallel to radial (pointing straight up on
// the pad), that projection collapses, so we fall back through the
// primary spin axis (≈ north), then world +Z, then +X. Returns a
// unit vector perpendicular to nose, or zero if nose is itself zero.
func (s *Spacecraft) headsUpReference(nose orbital.Vec3) orbital.Vec3 {
	axisR := render.BodyRotationAxisWorld(s.Primary)
	candidates := []orbital.Vec3{
		s.State.R,                            // radial-out (local vertical)
		{X: axisR.X, Y: axisR.Y, Z: axisR.Z}, // spin axis ≈ north
		{Z: 1},
		{X: 1},
	}
	for _, c := range candidates {
		perp := c.Sub(nose.Scale(nose.Dot(c)))
		if perp.Norm() > 1e-9 {
			return perp.Unit()
		}
	}
	return orbital.Vec3{}
}

// BodyFrame returns the craft's full orientation as an orthonormal
// triple: nose (lengthwise / thrust axis = CurrentAttitudeDir), up
// (dorsal), and right (starboard). up is the heads-up reference
// rotated about the nose by CurrentRollDeg, so a non-zero roll banks
// the frame; right = nose × up completes it. ok=false when the nose
// is uninitialised (zero) — callers fall back to the commanded
// direction or a static navball. v0.10.0+.
func (s *Spacecraft) BodyFrame() (nose, up, right orbital.Vec3, ok bool) {
	return s.BodyFrameFor(s.CurrentAttitudeDir, s.CurrentRollDeg)
}

// BodyFrameFor builds the body frame from an explicit nose direction
// and roll (degrees) rather than the stored CurrentAttitudeDir /
// CurrentRollDeg. The navball uses this so it can substitute the
// commanded nose/roll under InstantSAS or before the first slew tick.
// v0.10.0+.
func (s *Spacecraft) BodyFrameFor(noseDir orbital.Vec3, rollDeg float64) (nose, up, right orbital.Vec3, ok bool) {
	nose = noseDir.Unit()
	if nose == (orbital.Vec3{}) {
		return orbital.Vec3{}, orbital.Vec3{}, orbital.Vec3{}, false
	}
	up0 := s.headsUpReference(nose)
	if up0 == (orbital.Vec3{}) {
		return orbital.Vec3{}, orbital.Vec3{}, orbital.Vec3{}, false
	}
	up = orbital.Rotate(up0, nose, rollDeg*math.Pi/180).Unit()
	right = up.Cross(nose).Unit() // starboard; {nose,right,up} right-handed (nose×right=up)
	return nose, up, right, true
}

// wrapDeg180 normalises an angle to (-180, 180].
func wrapDeg180(d float64) float64 {
	d = math.Mod(d, 360)
	if d <= -180 {
		d += 360
	} else if d > 180 {
		d -= 360
	}
	return d
}

// RollToward rotates CurrentRollDeg toward CommandedRollDeg by at most
// SlewRate·dt degrees along the shortest signed path (wrapped to
// ±180). dt is the warp-scaled sim seconds for the tick — same rate
// budget as the attitude slew. v0.10.0+.
func (s *Spacecraft) RollToward(dt float64) {
	s.CommandedRollDeg = wrapDeg180(s.CommandedRollDeg)
	diff := wrapDeg180(s.CommandedRollDeg - wrapDeg180(s.CurrentRollDeg))
	if diff == 0 {
		s.CurrentRollDeg = s.CommandedRollDeg
		return
	}
	maxStep := s.SlewRateRad() * dt * 180 / math.Pi // deg this tick
	if math.Abs(diff) <= maxStep {
		s.CurrentRollDeg = s.CommandedRollDeg
		return
	}
	step := maxStep
	if diff < 0 {
		step = -maxStep
	}
	s.CurrentRollDeg = wrapDeg180(wrapDeg180(s.CurrentRollDeg) + step)
}
