package physics

import "github.com/jasonfen/terminal-space-program/internal/orbital"

// StepRCSPulse applies one RCS pulse to a state vector: a Δv of
// magnitude dvQuantum along the given unit direction, with mass
// updated to mAfter. No sub-stepping needed at the cm/s scale a pulse
// delivers — the velocity change is instantaneous from the
// integrator's perspective.
//
// Position is unchanged by the pulse itself (impulsive
// approximation); position evolves under the next gravity step. v0.8.0+.
//
// dirUnit must be a unit vector (caller is responsible — typically
// spacecraft.DirectionUnit). Caller passes mAfter = total mass after
// the monoprop debit so the returned state's M reflects post-pulse
// mass without StepRCSPulse needing to know about Isp / propellant.
func StepRCSPulse(s StateVector, dirUnit orbital.Vec3, dvQuantum, mAfter float64) StateVector {
	if dvQuantum == 0 {
		return s
	}
	return StateVector{
		R: s.R,
		V: s.V.Add(dirUnit.Scale(dvQuantum)),
		M: mAfter,
	}
}
