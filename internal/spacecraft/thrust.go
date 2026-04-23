package spacecraft

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

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
	const g0 = 9.80665
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
	const g0 = 9.80665
	if s.DryMass == 0 || s.TotalMass() == 0 {
		return 0
	}
	return s.Isp * g0 * math.Log(s.TotalMass()/s.DryMass)
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
