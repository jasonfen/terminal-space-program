package spacecraft

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
)

// BurnDirection returns the unit thrust direction for the active
// craft given a burn mode, taking into account:
//
//   - Surface-relative modes (BurnSurfacePrograde / Retrograde)
//     which need the craft's primary spin axis (ω) to compute the
//     surface-relative velocity v - ω × r. Pre-launch (zero
//     surface velocity) returns the zero vector; the caller
//     interprets that as "no defined direction" — the burn is a
//     no-op until the craft is moving relative to the ground.
//   - HeadingTrim (v0.42+) — the player-commanded launch azimuth,
//     applied first as a rotation about local up so a non-default
//     heading redirects which vertical plane the mode's natural
//     direction (and PitchTrim below) lands in.
//   - PitchTrim (v0.9.2+) — a player-set ± rotation about the
//     local-north axis applied on top of the mode's natural
//     direction, for ascent gravity-turn manual flight.
//
// Live-craft call sites (RCS pulse, manual burn, ActiveBurn fire)
// use this method instead of the bare DirectionUnit so surface
// modes + trim feed through. Predictor / speculative call sites
// without a *Spacecraft in scope keep using DirectionUnit; surface
// modes there return zero (degraded — predictor doesn't simulate
// future v_surface).
//
// v0.9.2+. v0.9.3+: target-relative modes added; this wrapper passes
// zero target state, so the four target modes degrade to no-op here —
// callers with a target use BurnDirectionWithTarget.
func (s *Spacecraft) BurnDirection(mode BurnMode) orbital.Vec3 {
	return s.BurnDirectionWithTarget(mode, orbital.Vec3{}, orbital.Vec3{})
}

// BurnDirectionWithTarget is BurnDirection with a target snapshot in
// the same frame as Spacecraft.State (primary-relative when both
// share a primary, fully inertial otherwise — caller resolves the
// frame via World.targetStateRelativeToActivePrimary).
//
// The four target-relative modes (BurnTargetPrograde / Retrograde /
// BurnTarget / AntiTarget) consume (rT, vT); other modes ignore it
// and behave identically to BurnDirection.
//
// v0.9.3+.
func (s *Spacecraft) BurnDirectionWithTarget(mode BurnMode, rT, vT orbital.Vec3) orbital.Vec3 {
	// Body's tilted spin axis — shared with the launchpad spawn
	// frame, the landed integrator, and physics.AtmosphereOmega
	// (v0.11.2+ unification, ADR 0003). One ω across the codebase.
	omegaR := render.BodySpinOmegaWorld(s.Primary)
	omega := orbital.Vec3{X: omegaR.X, Y: omegaR.Y, Z: omegaR.Z}
	axisR := render.BodyRotationAxisWorld(s.Primary)
	spinAxis := orbital.Vec3{X: axisR.X, Y: axisR.Y, Z: axisR.Z}

	var dir orbital.Vec3
	switch mode {
	case BurnSurfacePrograde, BurnSurfaceRetrograde:
		vSurf := s.State.V.Sub(omega.Cross(s.State.R))
		n := vSurf.Norm()
		if n == 0 {
			return orbital.Vec3{}
		}
		dir = vSurf.Scale(1 / n)
		if mode == BurnSurfaceRetrograde {
			dir = dir.Scale(-1)
		}
	case BurnTargetPrograde, BurnTargetRetrograde, BurnTarget, BurnAntiTarget:
		dir = DirectionUnitTarget(mode, s.State.R, s.State.V, rT, vT)
	default:
		dir = DirectionUnit(mode, s.State.R, s.State.V)
	}
	// Heading before pitch (ADR 0049 decision 8/9): heading rotates the
	// horizontal component of dir about local up onto the commanded
	// heading first, so pitch trim's rotation about local north then
	// tilts within THAT heading's vertical plane rather than always
	// within the due-east one. Swapping this order silently pins every
	// ascent back to due-east steering regardless of the commanded
	// heading — a later refactor must preserve it.
	if s.HeadingTrim != 0 {
		dir = ApplyHeadingTrim(dir, s.State.R, spinAxis, s.HeadingTrim)
	}
	if s.PitchTrim != 0 {
		dir = ApplyPitchTrim(dir, s.State.R, spinAxis, s.PitchTrim)
	}
	return dir
}

// BurnDirectionPlaneAware resolves a burn direction like
// BurnDirectionWithTarget, additionally handling BurnPlaneChange via
// the supplied signed plane-change angle (radians). planeRad is
// ignored for every other mode. The planted-node and active-burn
// paths use this wrapper because the rotation angle rides on the
// ManeuverNode / ActiveBurn — a BurnMode alone can't decode it.
//
// v0.10.4+.
func (s *Spacecraft) BurnDirectionPlaneAware(mode BurnMode, rT, vT orbital.Vec3, planeRad float64) orbital.Vec3 {
	if mode == BurnPlaneChange {
		return planeChangeDirection(s.State.R, s.State.V, planeRad)
	}
	return s.BurnDirectionWithTarget(mode, rT, vT)
}

// BurnDirectionForBurn resolves a planted / in-flight burn's unit thrust
// direction, additionally handling BurnVector (v0.12.x+) via the captured
// fixed-inertial burnDir — a direction a BurnMode alone can't decode, just
// like BurnPlaneChange's planeRad. BurnVector ignores (rT, vT, planeRad)
// and craft state; every other mode delegates to BurnDirectionPlaneAware.
// The firing/slew path (sim) uses this wrapper because the captured vector
// rides on the ManeuverNode / ActiveBurn.
//
// v0.12.x+.
func (s *Spacecraft) BurnDirectionForBurn(mode BurnMode, rT, vT orbital.Vec3, planeRad float64, burnDir orbital.Vec3) orbital.Vec3 {
	if mode == BurnVector {
		return burnDir.Unit()
	}
	return s.BurnDirectionPlaneAware(mode, rT, vT, planeRad)
}

// localHorizonFrame builds the (east, up, north) local frame at
// position r on a body spinning about spinAxis. Shared by
// ApplyPitchTrim and ApplyHeadingTrim so the two trims always agree on
// which way is east.
//
// Frame:
//
//	up    = r̂                          (local vertical)
//	east  = unit(spinAxis × up)         (local east on the body)
//	north = up × east                   (right-handed local frame)
//
// ok is false at r == 0 (no defined position) or at a pole (where
// spinAxis × up vanishes and east is undefined) — callers no-op the
// trim in that case rather than divide by zero.
//
// spinAxis is the body's true spin axis in world coordinates (tilted
// per AxialTilt + AxialAzimuth, matching render.BodyRotationAxisWorld).
// Pass orbital.Vec3{Z: 1} for an un-tilted body to get the legacy
// pre-v0.9.4 behaviour.
func localHorizonFrame(r, spinAxis orbital.Vec3) (east, up, north orbital.Vec3, ok bool) {
	rN := r.Norm()
	if rN == 0 {
		return orbital.Vec3{}, orbital.Vec3{}, orbital.Vec3{}, false
	}
	up = r.Scale(1 / rN)
	// east = spinAxis × up, normalised. Falls back to the Z-aligned
	// approximation if the caller passed a zero spin axis (e.g. a
	// body with no rotation period).
	axis := spinAxis
	if axis.Norm() == 0 {
		axis = orbital.Vec3{Z: 1}
	}
	east = axis.Cross(up)
	eN := east.Norm()
	if eN == 0 {
		// Pole — no defined east.
		return orbital.Vec3{}, orbital.Vec3{}, orbital.Vec3{}, false
	}
	east = east.Scale(1 / eN)
	north = up.Cross(east)
	return east, up, north, true
}

// ApplyPitchTrim rotates dir about the local-north axis at position
// r by pitchRad (radians, positive = east). Used by BurnDirection to
// fold the player's pitch-trim setting into any burn mode's natural
// direction. Public so tests can exercise the rotation math directly.
//
// Rotation about north tilts the thrust vector east (+pitch) or west
// (-pitch) without changing the heading component. At the poles
// (where east is undefined) the rotation is a no-op — see
// localHorizonFrame.
//
// v0.9.2+. v0.9.4+: spin-axis param so the trim's east axis matches
// the launchpad spawn frame on tilted bodies (Earth: 23.5°).
func ApplyPitchTrim(dir, r, spinAxis orbital.Vec3, pitchRad float64) orbital.Vec3 {
	if pitchRad == 0 {
		return dir
	}
	east, up, north, ok := localHorizonFrame(r, spinAxis)
	if !ok {
		// No defined east (zero position, or a pole): the trim
		// silently no-ops rather than divide by zero. The player
		// won't be trimming a launch from the pole anyway.
		return dir
	}

	// Decompose dir into the (east, up, north) local frame.
	e := dir.X*east.X + dir.Y*east.Y + dir.Z*east.Z
	u := dir.X*up.X + dir.Y*up.Y + dir.Z*up.Z
	n := dir.X*north.X + dir.Y*north.Y + dir.Z*north.Z

	// Rotate (e, u) about north axis by pitchRad. Positive pitch
	// tilts the vector toward east.
	cosA, sinA := math.Cos(pitchRad), math.Sin(pitchRad)
	eNew := e*cosA + u*sinA
	uNew := -e*sinA + u*cosA

	return east.Scale(eNew).Add(up.Scale(uNew)).Add(north.Scale(n))
}

// ApplyHeadingTrim rotates dir about the local-up axis at position r
// by headingOffsetRad (radians, the commanded heading's offset from
// due east — positive offsets rotate toward south/west, negative
// toward north; see the derivation below). Sibling of ApplyPitchTrim:
// where pitch tilts a direction's vertical component, heading swings
// its horizontal component around to a different compass bearing
// without touching the up component. Public so tests can exercise the
// rotation math directly.
//
// Bearing convention (matching the pad's compass display): 000° =
// north, 090° = east, 180° = south, 270° = west, measured clockwise
// looking down on the body from above its spin axis. A direction at
// bearing β decomposes in the (east, north) plane as
// sinβ·east + cosβ·north. Starting from due east (β=090°, offset 0)
// and rotating by -offsetRad in the (east, north) plane lands exactly
// on sin(090°+offsetRad)·east + cos(090°+offsetRad)·north, i.e. on
// the bearing 090°+offsetRad — so offsetRad IS the desired bearing
// shift away from due east.
//
// At the poles (where east/north are undefined) the rotation is a
// no-op, the same guard ApplyPitchTrim uses via localHorizonFrame.
//
// v0.42+ (ADR 0049 decision 8, #453).
func ApplyHeadingTrim(dir, r, spinAxis orbital.Vec3, headingOffsetRad float64) orbital.Vec3 {
	if headingOffsetRad == 0 {
		return dir
	}
	east, up, north, ok := localHorizonFrame(r, spinAxis)
	if !ok {
		return dir
	}

	// Decompose dir into the (east, up, north) local frame.
	e := dir.X*east.X + dir.Y*east.Y + dir.Z*east.Z
	u := dir.X*up.X + dir.Y*up.Y + dir.Z*up.Z
	n := dir.X*north.X + dir.Y*north.Y + dir.Z*north.Z

	// Rotate (e, n) about the up axis by -headingOffsetRad (see the
	// doc comment for the sign derivation): the horizontal component
	// swings onto the commanded bearing while the up component is
	// left untouched.
	theta := -headingOffsetRad
	cosA, sinA := math.Cos(theta), math.Sin(theta)
	eNew := e*cosA - n*sinA
	nNew := e*sinA + n*cosA

	return east.Scale(eNew).Add(up.Scale(u)).Add(north.Scale(nNew))
}

// HeadingOrbitNormal returns the (unnormalised) orbital-plane normal
// (the specific angular momentum direction r × v) an ascent launched
// NOW from position r at the given commanded-heading offset (the same
// offset-from-due-east ApplyHeadingTrim consumes) would produce. It
// does not consult a Spacecraft's actual State.V: a Landed craft's real
// velocity is always the due-east surface co-rotation velocity
// regardless of HeadingTrim (ApplyHeadingTrim only rotates a *burn*
// direction, never the pre-ignition landed state), so a caller that
// wants "the plane this pad's commanded heading would leave" must ask
// this function rather than read the state vector directly.
//
// Used for two HUD entry points (ADR 0049 decisions 9-11): the pad's
// `incl:` row (via HeadingInclinationDeg below) and `Δincl:` (dotting
// the returned normal against a target's own orbit normal). ok is
// false at the same degenerate cases ApplyHeadingTrim no-ops on (zero
// r, a pole, or a resulting h that collapses to zero).
//
// v0.42+ (ADR 0049 decisions 9-11).
func HeadingOrbitNormal(r, spinAxis orbital.Vec3, headingOffsetRad float64) (orbital.Vec3, bool) {
	east, _, _, ok := localHorizonFrame(r, spinAxis)
	if !ok {
		return orbital.Vec3{}, false
	}
	dir := ApplyHeadingTrim(east, r, spinAxis, headingOffsetRad)
	h := r.Cross(dir)
	if h.Norm() == 0 {
		return orbital.Vec3{}, false
	}
	return h, true
}

// HeadingInclinationDeg returns the inclination (degrees, unfolded over
// the full 0-180 range: 090° reads the pad's floor, 270° reads its
// retrograde complement) that an ascent launched now from r at the
// given commanded-heading offset would reach, measured against
// spinAxis the same general way internal/orbital.ElementsFromState
// derives inclination from a state vector (acos of the angular
// momentum's angle to the reference axis), not a re-derivation of the
// closed form cos(i) = sin(beta)*cos(phi) from its own components. See
// TestHeadingInclinationDegMatchesIdentity, which pins this function
// against that closed form independently.
//
// ok is false wherever HeadingOrbitNormal is (pole / zero r) or where
// spinAxis itself is undefined (a primary with no defined rotation
// axis); the caller (the pad's `incl:` row) should fall back to "—"
// rather than print a misleading number.
//
// v0.42+ (ADR 0049 decisions 9-10).
func HeadingInclinationDeg(r, spinAxis orbital.Vec3, headingOffsetRad float64) (float64, bool) {
	h, ok := HeadingOrbitNormal(r, spinAxis, headingOffsetRad)
	if !ok {
		return 0, false
	}
	axisNorm := spinAxis.Norm()
	if axisNorm == 0 {
		return 0, false
	}
	cosI := h.Dot(spinAxis) / (h.Norm() * axisNorm)
	if cosI > 1 {
		cosI = 1
	} else if cosI < -1 {
		cosI = -1
	}
	return math.Acos(cosI) * 180 / math.Pi, true
}

// PitchTrimStepRad is the per-keypress pitch trim adjustment in
// radians. v0.16: 5° (= π/36) — finer control for the gravity turn.
// History: v0.9.2 shipped at 5°, v0.9.2.1 bumped to 10° because a
// Saturn V's gravity turn needed too many `>` taps to get going; the
// 5° step is restored per playtest preference (the smaller stripped-back
// Lumen vehicles steer better with finer granularity, and held `>`
// ramps continuously at the terminal key-repeat rate for big pitch-overs).
const PitchTrimStepRad = math.Pi / 36

// HeadingTrimDueEastRad is the absolute compass bearing (000°=north,
// 090°=east, 180°=south, 270°=west) that a zero Spacecraft.HeadingTrim
// offset resolves to: due east. It is NOT the field's default value —
// HeadingTrim stores a signed offset from this bearing (zero = no
// trim, PitchTrim's own shape), so a fresh vessel needs no explicit
// initialisation. This constant exists for code that needs the
// resulting absolute bearing, i.e. HeadingTrimDueEastRad +
// Spacecraft.HeadingTrim (ADR 0049 decision 8).
const HeadingTrimDueEastRad = math.Pi / 2

// HeadingTrimStepRad is the per-keypress heading trim adjustment in
// radians, 5° (= π/36) — the same step size and idiom as
// PitchTrimStepRad (ADR 0049 decision 8/9: "`{` / `}` nudge the
// commanded heading ±5°... with the pitch-trim idiom").
const HeadingTrimStepRad = math.Pi / 36
