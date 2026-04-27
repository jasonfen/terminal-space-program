package planner

import (
	"errors"
	"math"
	"time"
)

// PlanMoonEscape constructs a two-impulse plan to escape a moon's SOI
// and rejoin the parent body's frame. The departure burn is a prograde
// impulse at the moon-frame parking orbit periapsis sized so the
// transfer ellipse's apolune kisses the moon's sphere-of-influence
// boundary. When the craft reaches that apolune it crosses into the
// parent's frame (via the standard SOI-aware integrator) and inherits
// the moon's parent-relative velocity plus the small radial
// component left over from the SOI exit.
//
// The arrival node is a zero-Δv marker positioned in the parent's
// frame at the SOI-exit moment (depOffset + half-period). Its job is
// to surface the frame transition in the maneuver list / HUD; the
// player plants their own circularization burn manually after seeing
// the post-escape parent-frame trajectory.
//
// Inputs:
//   - muMoon: GM of the moon (e.g., Luna's GM).
//   - rPark:  craft's |R| in the moon's frame at plant time.
//   - rSOI:   moon's sphere-of-influence radius. Apolune target.
//   - minLeadSeconds: pad the departure offset so a centered finite
//     burn fits inside the wait window. Pass 0 to skip padding.
//   - moonID:    moon's body ID — populates the departure node's
//     PrimaryID so the HUD renders the burn in the moon's frame.
//   - parentID:  moon's parent ID — populates the arrival marker's
//     PrimaryID so the HUD knows the SOI-exit lives in the parent
//     frame.
//
// Phasing isn't enforced. v0.6.3 ships the simplest viable escape:
// burn at periapsis, drop into parent frame, manual finish. Future
// cycles can layer in a phasing model so the SOI-exit moment lands at
// the player's preferred parent-frame angle.
func PlanMoonEscape(
	muMoon, rPark, rSOI float64,
	minLeadSeconds float64,
	moonID, parentID string,
) (TransferPlan, error) {
	if muMoon <= 0 || rPark <= 0 || rSOI <= 0 {
		return TransferPlan{}, errors.New("planmoonescape: muMoon, rPark, rSOI must be positive")
	}
	if rSOI <= rPark {
		return TransferPlan{}, errors.New("planmoonescape: SOI radius must exceed parking radius")
	}

	aT := (rPark + rSOI) / 2
	vCirc := math.Sqrt(muMoon / rPark)
	vTransAtPeri := math.Sqrt(muMoon * (2/rPark - 1/aT))
	dvDep := vTransAtPeri - vCirc

	// Half-period of the bound transfer ellipse — periapsis to the
	// apolune that sits on the SOI boundary.
	transferTime := math.Pi * math.Sqrt(aT*aT*aT/muMoon)

	var depOffset time.Duration
	if minLeadSeconds > 0 {
		depOffset = time.Duration(minLeadSeconds * float64(time.Second))
	}

	return TransferPlan{
		Departure: TransferNode{
			Leg:          LegDeparture,
			PrimaryID:    moonID,
			DV:           dvDep,
			OffsetTime:   depOffset,
			IsRetrograde: false,
		},
		Arrival: TransferNode{
			Leg:          LegArrival,
			PrimaryID:    parentID,
			DV:           0,
			OffsetTime:   depOffset + time.Duration(transferTime*float64(time.Second)),
			IsRetrograde: false,
		},
		TransferDt: time.Duration(transferTime * float64(time.Second)),
	}, nil
}
