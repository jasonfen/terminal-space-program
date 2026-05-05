package spacecraft

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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
// v0.9.2+.
func (s *Spacecraft) BurnDirection(mode BurnMode) orbital.Vec3 {
	var dir orbital.Vec3
	switch mode {
	case BurnSurfacePrograde, BurnSurfaceRetrograde:
		omega := physics.AtmosphereOmega(s.Primary)
		vSurf := s.State.V.Sub(omega.Cross(s.State.R))
		n := vSurf.Norm()
		if n == 0 {
			return orbital.Vec3{}
		}
		dir = vSurf.Scale(1 / n)
		if mode == BurnSurfaceRetrograde {
			dir = dir.Scale(-1)
		}
	default:
		dir = DirectionUnit(mode, s.State.R, s.State.V)
	}
	if s.PitchTrim != 0 {
		dir = ApplyPitchTrim(dir, s.State.R, s.PitchTrim)
	}
	return dir
}

// ApplyPitchTrim rotates dir about the local-north axis at position
// r by pitchRad (radians, positive = east). Used by BurnDirection to
// fold the player's pitch-trim setting into any burn mode's natural
// direction. Public so tests can exercise the rotation math directly.
//
// Frame:
//   up    = r̂                          (local vertical)
//   east  = unit(spinAxis × up)         (local east, with spin axis = +Z)
//   north = up × east                   (right-handed local frame)
//
// Rotation about north tilts the thrust vector east (+pitch) or west
// (-pitch) without changing the heading component. At the poles
// (where east is undefined) the rotation is a no-op.
//
// v0.9.2+.
func ApplyPitchTrim(dir, r orbital.Vec3, pitchRad float64) orbital.Vec3 {
	if pitchRad == 0 {
		return dir
	}
	rN := r.Norm()
	if rN == 0 {
		return dir
	}
	up := r.Scale(1 / rN)
	// east = Z × up (Z-aligned spin axis convention from physics.AtmosphereOmega).
	east := orbital.Vec3{X: -up.Y, Y: up.X, Z: 0}
	eN := east.Norm()
	if eN == 0 {
		// Pole — no defined east. Return dir unchanged so the trim
		// silently no-ops at high latitudes; the player won't be
		// trimming a launch from the pole anyway.
		return dir
	}
	east = east.Scale(1 / eN)
	north := up.Cross(east)

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

// PitchTrimStepRad is the per-keypress pitch trim adjustment in
// radians. v0.9.2.1+: 10° (= π/18). v0.9.2 shipped at 5° but
// playtest exposed that the user had to mash `>` 6+ times to get
// the gravity turn going on a Saturn V — Apollo's actual ascent
// program pitched 30–50° from vertical. Bump to 10° so 3 taps
// gives a reasonable initial pitch-over.
const PitchTrimStepRad = math.Pi / 18
