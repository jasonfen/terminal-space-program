package spacecraft

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

const g0 = 9.80665

// BurnMode enumerates the six direction modes from plan §Phase 2.
type BurnMode int

const (
	BurnPrograde BurnMode = iota
	BurnRetrograde
	BurnNormalPlus    // orbit normal (+h direction)
	BurnNormalMinus   // orbit normal (-h direction)
	BurnRadialOut     // away from primary
	BurnRadialIn      // toward primary
)

// String is the label shown in the maneuver planner.
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
	}
	return "?"
}

// AllBurnModes is the cycle order for the planner UI.
var AllBurnModes = []BurnMode{
	BurnPrograde,
	BurnRetrograde,
	BurnNormalPlus,
	BurnNormalMinus,
	BurnRadialOut,
	BurnRadialIn,
}

// DirectionUnit returns a unit vector for the given burn mode given the
// craft's current (r, v) — primary-relative. Returns the zero vector if
// r or v is degenerate (can't define the frame).
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

// ApplyImpulsive adds a delta-v of magnitude dv m/s in the given direction
// mode, instantly. Fuel is deducted using the rocket equation as a proxy
// (Isp·g·ln(m0/m1) for the actually-burned Δv); in v0.1 we approximate with
// a linear consumption rate — plan §MVP defers true rocket-eq to v0.2.
func (s *Spacecraft) ApplyImpulsive(mode BurnMode, dv float64) {
	dir := DirectionUnit(mode, s.State.R, s.State.V)
	if dir.Norm() == 0 || dv == 0 {
		return
	}
	s.State.V = s.State.V.Add(dir.Scale(dv))
	s.consumeFuel(math.Abs(dv))
}

// consumeFuel deducts fuel assuming the rocket equation. fuelUsed =
// m0 · (1 − exp(−dv / (Isp·g0))). Caps at available fuel.
func (s *Spacecraft) consumeFuel(dvUsed float64) {
	if s.Isp <= 0 {
		return
	}
	mass0 := s.TotalMass()
	massFrac := 1.0 - math.Exp(-dvUsed/(s.Isp*g0))
	fuelBurned := mass0 * massFrac
	if fuelBurned > s.Fuel {
		fuelBurned = s.Fuel
	}
	s.Fuel -= fuelBurned
	s.State.M = s.TotalMass()
}

// RemainingDeltaV estimates how much more Δv the current fuel supports,
// from the rocket equation: Δv = Isp·g0·ln(m0/m_dry).
func (s *Spacecraft) RemainingDeltaV() float64 {
	if s.DryMass == 0 || s.TotalMass() == 0 {
		return 0
	}
	return s.Isp * g0 * math.Log(s.TotalMass()/s.DryMass)
}

// MassFlowRate returns the propellant mass-flow magnitude (kg/s) at the
// configured thrust level: ṁ = Thrust/(Isp·g0). Zero if thrust or Isp is
// zero. Used by the finite-burn integrator to step mass per sub-step.
func (s *Spacecraft) MassFlowRate() float64 {
	if s.Thrust <= 0 || s.Isp <= 0 {
		return 0
	}
	return s.Thrust / (s.Isp * g0)
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

// ThrustAccelFn returns an RK4-compatible accel closure that adds engine
// thrust along the given burn-mode direction on top of two-body gravity.
// Direction is recomputed each sub-step from live (r, v), so prograde
// follows the rotating velocity frame — the expected UX for held-prograde
// burns.
//
// Mass is held constant for the closure (the integrator treats the
// sub-step as ~constant-mass); the caller updates fuel via MassFlowRate
// after the StepRK4 call. Thrust is gated to zero if fuel is empty.
func (s *Spacecraft) ThrustAccelFn(mode BurnMode, mu float64) func(r, v orbital.Vec3, t float64) orbital.Vec3 {
	mass := s.TotalMass()
	thrust := s.Thrust
	if s.Fuel <= 0 {
		thrust = 0
	}
	return func(r, v orbital.Vec3, _ float64) orbital.Vec3 {
		gravity := physics.Accel(r, mu)
		if thrust == 0 || mass == 0 {
			return gravity
		}
		dir := DirectionUnit(mode, r, v)
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
