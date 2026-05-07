package spacecraft

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
)

const g0 = 9.80665

// EngineMode selects which propulsion system the manual-flight path
// drives: the main engine (high-thrust, fuel) or the monopropellant
// RCS thrusters (low-thrust, monoprop, pulse-fired). v0.8.0+.
//
// Planted maneuver nodes always use the main engine; EngineMode only
// gates the live manual-flight inputs (attitude keys + b).
type EngineMode int

const (
	EngineMain EngineMode = iota
	EngineRCS
)

// String returns the HUD label for the engine mode.
func (e EngineMode) String() string {
	switch e {
	case EngineMain:
		return "main"
	case EngineRCS:
		return "rcs"
	}
	return "?"
}

// RCSDvQuantum is the per-pulse Δv applied when an attitude key fires
// in RCS mode. Sized at 0.1 m/s per scoping decision #7 — small enough
// for sub-m/s precision proximity work, large enough that a held key
// at terminal-default ~5 Hz key-repeat delivers usable corrections in
// a few seconds. v0.8.0+.
const RCSDvQuantum = 0.1

// ApplyRCSPulse delivers one RCSDvQuantum of Δv in the given burn
// direction, debiting monoprop via the rocket equation against the
// RCSIsp engine. No-op if monoprop is empty or RCSThrust / RCSIsp are
// unconfigured (legacy save with zero RCS fields, mid-load before the
// loader populates defaults). v0.8.0+.
//
// Target-relative modes degrade to no-op (zero direction) without a
// resolved target snapshot — callers with a target use
// ApplyRCSPulseWithTarget. v0.9.3+.
func (s *Spacecraft) ApplyRCSPulse(mode BurnMode) bool {
	return s.ApplyRCSPulseWithTarget(mode, orbital.Vec3{}, orbital.Vec3{})
}

// ApplyRCSPulseWithTarget is ApplyRCSPulse with a resolved target
// snapshot in the same frame as Spacecraft.State (primary-relative
// when both share a primary, fully inertial otherwise — caller
// resolves via World.targetStateRelativeToActivePrimary). The four
// target-relative modes use the snapshot to compute direction; other
// modes ignore it. v0.9.3+.
func (s *Spacecraft) ApplyRCSPulseWithTarget(mode BurnMode, rT, vT orbital.Vec3) bool {
	if s.RCSIsp <= 0 || s.RCSThrust <= 0 || s.Monoprop <= 0 {
		return false
	}
	// v0.9.2+: route through BurnDirection so surface modes + pitch
	// trim feed through. Non-surface modes degenerate to the same
	// result as DirectionUnit + zero trim.
	dir := s.BurnDirectionWithTarget(mode, rT, vT)
	if dir.Norm() == 0 {
		return false
	}
	dv := RCSDvQuantum
	// Rocket equation against monoprop pool only — main fuel
	// untouched. m0 = TotalMass; m1 = m0 · exp(-Δv / (Isp·g0)).
	m0 := s.TotalMass()
	if m0 <= 0 {
		return false
	}
	massFrac := 1.0 - math.Exp(-dv/(s.RCSIsp*g0))
	monoBurned := m0 * massFrac
	// v0.9.1+: route through BurnMonoprop so Stages[0].MonopropMass
	// is the authoritative debit + SyncFields refreshes the flat
	// shadow Monoprop field.
	s.BurnMonoprop(monoBurned)
	mAfter := s.TotalMass()
	s.State = physics.StepRCSPulse(s.State, dir, dv, mAfter)
	return true
}

// BurnMode enumerates the six direction modes from plan §Phase 2.
type BurnMode int

const (
	BurnPrograde BurnMode = iota
	BurnRetrograde
	BurnNormalPlus    // orbit normal (+h direction)
	BurnNormalMinus   // orbit normal (-h direction)
	BurnRadialOut     // away from primary
	BurnRadialIn      // toward primary

	// Surface-relative modes (v0.9.2+) — live SAS only, not planted-
	// node modes. Direction = ±(v_surface).Unit() where v_surface =
	// v - ω × r is the craft's velocity relative to the rotating
	// atmosphere. Useful for ascent gravity-turn flight: once the
	// craft has eastward velocity, BurnSurfacePrograde tracks it
	// even as the velocity vector pitches over from gravity drag,
	// and the autopilot rides the curving trajectory cleanly.
	//
	// Pre-launch (zero velocity) the surface direction is undefined;
	// BurnDirection returns the zero vector so the burn is a no-op.
	// The player nudges off the pad with pitch trim (BurnRadialOut +
	// trim east) and switches to BurnSurfacePrograde once velocity
	// is established.
	//
	// Not in AllBurnModes — surface modes don't appear in the m
	// planner's mode cycle, because planted nodes can't predict
	// future v_surface usefully.
	BurnSurfacePrograde
	BurnSurfaceRetrograde

	// Target-relative modes (v0.9.3+). Direction depends on the
	// active *and* target craft states in the same frame:
	//
	//   BurnTargetPrograde   = unit(v_target − v_active)
	//   BurnTargetRetrograde = unit(v_active − v_target)
	//   BurnTarget           = unit(r_target − r_active)
	//   BurnAntiTarget       = unit(r_active − r_target)
	//
	// The velocity-relative pair is the primary tool for the manual
	// rendezvous loop — hold target-prograde to close v_rel during
	// approach, flip target-retrograde at closest approach to null
	// v_rel. The position-relative pair is for sub-m/s proximity-ops
	// nudges after v_rel is nulled.
	//
	// All four require World.Target.Kind == TargetCraft. Without a
	// craft target, DirectionUnitTarget returns the zero vector and
	// the burn is a no-op (the live-closure path captures the world's
	// resolved target state once per step; the planted-node path
	// resolves at fire-time via ManeuverNode.TargetCraftIdx).
	BurnTargetPrograde
	BurnTargetRetrograde
	BurnTarget
	BurnAntiTarget
)

// String is the label shown in the maneuver planner / HUD.
func (m BurnMode) String() string {
	switch m {
	case BurnPrograde:
		return "Prograde"
	case BurnRetrograde:
		return "Retrograde"
	case BurnNormalPlus:
		return "Normal+"
	case BurnNormalMinus:
		return "Normal-"
	case BurnRadialOut:
		return "Radial+"
	case BurnRadialIn:
		return "Radial-"
	case BurnSurfacePrograde:
		return "Surface Prograde"
	case BurnSurfaceRetrograde:
		return "Surface Retrograde"
	case BurnTargetPrograde:
		return "Target Prograde"
	case BurnTargetRetrograde:
		return "Target Retrograde"
	case BurnTarget:
		return "Target"
	case BurnAntiTarget:
		return "Anti-Target"
	}
	return "?"
}

// AllBurnModes is the cycle order for the planner UI. v0.9.3+: the
// four target-relative modes append after the body-frame six. Surface
// modes stay out — planted nodes can't predict future v_surface.
//
// The maneuver form skips target-relative entries when
// World.Target.Kind != TargetCraft (no defined direction without a
// craft target).
var AllBurnModes = []BurnMode{
	BurnPrograde,
	BurnRetrograde,
	BurnNormalPlus,
	BurnNormalMinus,
	BurnRadialOut,
	BurnRadialIn,
	BurnTargetPrograde,
	BurnTargetRetrograde,
	BurnTarget,
	BurnAntiTarget,
}

// DirectionUnit returns a unit vector for the given burn mode given the
// craft's current (r, v) — primary-relative. Returns the zero vector if
// r or v is degenerate (can't define the frame).
//
// Target-relative modes (BurnTargetPrograde / Retrograde / BurnTarget /
// AntiTarget) are not handled here — they require target craft state.
// Callers with a target use DirectionUnitTarget; pure-function callers
// without target state in scope (predictor, AllBurnModes preview math
// when no target is set) get the zero vector + degraded behaviour.
func DirectionUnit(mode BurnMode, r, v orbital.Vec3) orbital.Vec3 {
	vMag := v.Norm()
	rMag := r.Norm()
	if vMag == 0 || rMag == 0 {
		return orbital.Vec3{}
	}
	prograde := v.Scale(1 / vMag)
	radialOut := r.Scale(1 / rMag)
	// Normal = r × v (specific angular momentum direction).
	h := cross(r, v)
	hMag := h.Norm()
	var normal orbital.Vec3
	if hMag > 0 {
		normal = h.Scale(1 / hMag)
	}
	switch mode {
	case BurnPrograde:
		return prograde
	case BurnRetrograde:
		return prograde.Scale(-1)
	case BurnNormalPlus:
		return normal
	case BurnNormalMinus:
		return normal.Scale(-1)
	case BurnRadialOut:
		return radialOut
	case BurnRadialIn:
		return radialOut.Scale(-1)
	}
	return orbital.Vec3{}
}

// DirectionUnitTarget returns the unit thrust direction for the four
// target-relative modes (v0.9.3+) given the active and target craft
// states in the SAME frame. Both states should be primary-relative
// when the two craft share a primary, or fully inertial when they
// don't — the world layer (World.targetStateRelativeToActivePrimary)
// handles that conversion.
//
// Non-target modes fall through to DirectionUnit(mode, rA, vA), so
// this is safe to call from any thrust-direction site that wants
// uniform mode handling.
//
// Returns the zero vector when the relative quantity is degenerate
// (identical positions for BurnTarget / AntiTarget; identical
// velocities for BurnTargetPrograde / Retrograde) or when called
// with zero rT, vT (no target resolved — caller passes zeros so the
// closure still constructs but the burn no-ops).
func DirectionUnitTarget(mode BurnMode, rA, vA, rT, vT orbital.Vec3) orbital.Vec3 {
	var d orbital.Vec3
	switch mode {
	case BurnTargetPrograde:
		d = vT.Sub(vA)
	case BurnTargetRetrograde:
		d = vA.Sub(vT)
	case BurnTarget:
		d = rT.Sub(rA)
	case BurnAntiTarget:
		d = rA.Sub(rT)
	default:
		return DirectionUnit(mode, rA, vA)
	}
	n := d.Norm()
	if n == 0 {
		return orbital.Vec3{}
	}
	return d.Scale(1 / n)
}

// ApplyImpulsive adds a delta-v of magnitude dv m/s in the given direction
// mode, instantly. Fuel is deducted using the rocket equation as a proxy
// (Isp·g·ln(m0/m1) for the actually-burned Δv); in v0.1 we approximate with
// a linear consumption rate — plan §MVP defers true rocket-eq to v0.2.
//
// Target-relative modes degrade to no-op without a target snapshot;
// callers with a target use ApplyImpulsiveWithTarget. v0.9.3+.
func (s *Spacecraft) ApplyImpulsive(mode BurnMode, dv float64) {
	s.ApplyImpulsiveWithTarget(mode, dv, orbital.Vec3{}, orbital.Vec3{})
}

// ApplyImpulsiveWithTarget is ApplyImpulsive with a target snapshot
// in the same frame as Spacecraft.State. Used by the planted-node
// fire path (sim/maneuver.go) for target-relative impulsive nodes.
// v0.9.3+.
func (s *Spacecraft) ApplyImpulsiveWithTarget(mode BurnMode, dv float64, rT, vT orbital.Vec3) {
	// v0.9.2+: route through BurnDirection so surface modes + trim feed through.
	dir := s.BurnDirectionWithTarget(mode, rT, vT)
	if dir.Norm() == 0 || dv == 0 {
		return
	}
	s.State.V = s.State.V.Add(dir.Scale(dv))
	s.consumeFuel(math.Abs(dv))
}

// consumeFuel deducts fuel assuming the rocket equation. fuelUsed =
// m0 · (1 − exp(−dv / (Isp·g0))). Caps at available fuel. v0.9.1+:
// routes the debit through BurnFuel so Stages[0].FuelMass (source
// of truth) decrements + SyncFields keeps the flat shadow field
// coherent.
func (s *Spacecraft) consumeFuel(dvUsed float64) {
	if s.Isp <= 0 {
		return
	}
	mass0 := s.TotalMass()
	massFrac := 1.0 - math.Exp(-dvUsed/(s.Isp*g0))
	fuelBurned := mass0 * massFrac
	s.BurnFuel(fuelBurned)
	s.State.M = s.TotalMass()
}

// RemainingDeltaV estimates how much more Δv the main engine's fuel
// supports via the rocket equation: Δv = Isp·g0·ln(m0/m_after_fuel).
// Monoprop is not burned through the main engine, so it counts as
// dead weight in the m_after_fuel term — the floor is dry+monoprop,
// not dry alone. v0.8.0+: pre-monoprop the floor was just DryMass.
func (s *Spacecraft) RemainingDeltaV() float64 {
	floor := s.DryMass + s.Monoprop
	if floor == 0 || s.TotalMass() == 0 {
		return 0
	}
	return s.Isp * g0 * math.Log(s.TotalMass()/floor)
}

// MassFlowRate returns the propellant mass-flow magnitude (kg/s) at
// the spacecraft's *live* throttle. The integrator's manual-burn
// path uses this; the planted-burn path uses MassFlowRateAt with the
// captured ActiveBurn.Throttle so adjusting the live throttle knob
// mid-coast doesn't slow a planted burn (v0.7.6+).
func (s *Spacecraft) MassFlowRate() float64 {
	return s.MassFlowRateAt(s.EffectiveThrottle())
}

// MassFlowRateAt returns the propellant mass-flow at an explicit
// throttle setting, clamped to [0, 1]. Used by the active-burn
// integrator path to honour the per-node throttle captured at
// burn-start. v0.7.6+.
func (s *Spacecraft) MassFlowRateAt(throttle float64) float64 {
	if s.Thrust <= 0 || s.Isp <= 0 {
		return 0
	}
	if throttle < 0 {
		throttle = 0
	} else if throttle > 1 {
		throttle = 1
	}
	if throttle == 0 {
		return 0
	}
	return s.Thrust * throttle / (s.Isp * g0)
}

// BurnTimeForDV returns the engine-on duration required to deliver dv
// at the craft's current mass + thrust + Isp, using the rocket-equation
// form t = (m0/ṁ)·(1 − exp(−Δv/(Isp·g0))). Accounts for the mass loss
// during the burn — at high Δv-fraction of the budget, a constant-mass
// approximation underestimates the time by the integral of mass /
// thrust, which matters for low-TWR vessels burning a large share of
// their fuel.
//
// Returns 0 when no finite burn is possible: zero or non-positive Δv,
// no thrust, no Isp, or Δv exceeding what the available fuel can
// support (caller's exceeds-budget warning fires; the integrator caps
// delivery at fuel exhaustion regardless of the duration the form
// committed). v0.6.5+: replaces the prior UI-set duration field; the
// planner now derives this so the player only specifies Δv.
func (s *Spacecraft) BurnTimeForDV(dv float64) time.Duration {
	if dv <= 0 || s.Isp <= 0 || s.Thrust <= 0 {
		return 0
	}
	mDot := s.MassFlowRate()
	if mDot <= 0 {
		return 0
	}
	mass0 := s.TotalMass()
	if mass0 <= 0 {
		return 0
	}
	secs := (mass0 / mDot) * (1 - math.Exp(-dv/(s.Isp*g0)))
	if secs <= 0 || math.IsNaN(secs) || math.IsInf(secs, 0) {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// ThrustAccelFn returns an RK4-compatible accel closure that adds
// engine thrust along the given burn-mode direction on top of
// two-body gravity, using the spacecraft's live throttle. Routed
// through ThrustAccelFnAt so the manual-burn integrator path picks
// up live throttle adjustments. Direction is recomputed each
// sub-step from live (r, v), so prograde follows the rotating
// velocity frame — the expected UX for held-prograde burns.
//
// Mass is held constant for the closure (the integrator treats the
// sub-step as ~constant-mass); the caller updates fuel via
// MassFlowRate after the StepRK4 call. Thrust is gated to zero if
// fuel is empty.
func (s *Spacecraft) ThrustAccelFn(mode BurnMode, mu float64) func(r, v orbital.Vec3, t float64) orbital.Vec3 {
	return s.ThrustAccelFnAt(mode, mu, s.EffectiveThrottle())
}

// ThrustAccelFnAt is ThrustAccelFn but uses an explicit throttle —
// used by the active-burn path in sim/world.go to honour the
// per-node throttle captured on the ActiveBurn struct at fire-time.
// Decoupling from `Spacecraft.Throttle` means adjusting the live
// throttle knob mid-coast doesn't slow a planted burn. v0.7.6+.
//
// Wrapper: passes zero target state. Target-relative modes degrade
// to no-op without a snapshot. v0.9.3+: prefer
// ThrustAccelFnAtWithTarget when the caller has resolved target
// state.
func (s *Spacecraft) ThrustAccelFnAt(mode BurnMode, mu, throttle float64) func(r, v orbital.Vec3, t float64) orbital.Vec3 {
	return s.ThrustAccelFnAtWithTarget(mode, mu, throttle, orbital.Vec3{}, orbital.Vec3{})
}

// ThrustAccelFnAtWithTarget is ThrustAccelFnAt with a target-craft
// state snapshot captured at closure construction. The four target-
// relative modes (BurnTargetPrograde / Retrograde / BurnTarget /
// AntiTarget) resolve their direction against (rT, vT). The target
// moves during a sub-step but slowly relative to the per-step
// granularity, so freezing the snapshot per call is safe — the world
// layer reconstructs the closure each stepThrust pass with a fresh
// snapshot.
//
// Pass zero rT, vT when no craft target is set (target-relative modes
// degrade to no-op, non-target modes are unaffected). Both states
// must be in the same frame as the closure's incoming (r, v) — the
// world layer (targetStateRelativeToActivePrimary) handles cross-
// primary conversion before construction.
//
// v0.9.3+.
func (s *Spacecraft) ThrustAccelFnAtWithTarget(mode BurnMode, mu, throttle float64, rT, vT orbital.Vec3) func(r, v orbital.Vec3, t float64) orbital.Vec3 {
	mass := s.TotalMass()
	if throttle < 0 {
		throttle = 0
	} else if throttle > 1 {
		throttle = 1
	}
	thrust := s.Thrust * throttle
	if s.Fuel <= 0 {
		thrust = 0
	}
	// v0.9.2+: capture omega + pitch trim at closure construction so
	// the per-sub-step direction lookup can resolve surface modes
	// without re-touching the Spacecraft (the integrator only passes
	// (r, v, t) into the closure). The closure's mode + trim stay
	// fixed for the burn — the v0.9.2 plan didn't commit live trim
	// adjustments mid-burn (they only feed through to the next burn
	// engagement). v0.9.3+: target snapshot likewise captured here.
	omegaR := render.BodySpinOmegaWorld(s.Primary)
	omega := orbital.Vec3{X: omegaR.X, Y: omegaR.Y, Z: omegaR.Z}
	axisR := render.BodyRotationAxisWorld(s.Primary)
	spinAxis := orbital.Vec3{X: axisR.X, Y: axisR.Y, Z: axisR.Z}
	pitchTrim := s.PitchTrim
	return func(r, v orbital.Vec3, _ float64) orbital.Vec3 {
		gravity := physics.Accel(r, mu)
		if thrust == 0 || mass == 0 {
			return gravity
		}
		var dir orbital.Vec3
		switch mode {
		case BurnSurfacePrograde, BurnSurfaceRetrograde:
			vSurf := v.Sub(omega.Cross(r))
			n := vSurf.Norm()
			if n == 0 {
				return gravity
			}
			dir = vSurf.Scale(1 / n)
			if mode == BurnSurfaceRetrograde {
				dir = dir.Scale(-1)
			}
		case BurnTargetPrograde, BurnTargetRetrograde, BurnTarget, BurnAntiTarget:
			dir = DirectionUnitTarget(mode, r, v, rT, vT)
		default:
			dir = DirectionUnit(mode, r, v)
		}
		if pitchTrim != 0 {
			dir = ApplyPitchTrim(dir, r, spinAxis, pitchTrim)
		}
		if dir.Norm() == 0 {
			return gravity
		}
		return gravity.Add(dir.Scale(thrust / mass))
	}
}

// cross is the standard 3D cross product. Lifted here (rather than adding
// to orbital.Vec3) to keep orbital free of spacecraft-specific helpers.
func cross(a, b orbital.Vec3) orbital.Vec3 {
	return orbital.Vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}
