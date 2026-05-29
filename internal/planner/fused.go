package planner

import (
	"errors"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// PlanIntraPrimaryFused builds a combined plane-shift + Hohmann transfer
// for the intra-primary case (craft + target share a primary, e.g. a LEO
// craft → Luna, both around Earth) using a single-revolution Lambert
// solve from the craft's actual departure state to the target's actual
// arrival position. Unlike PlanIntraPrimaryHohmann — which feeds the
// craft's |R| in as a *circular* parking radius and has no plane-change
// term — the returned departure velocity v1 connects the two points on a
// Keplerian arc regardless of nodes, so it inherently carries
// eccentricity, the apsis raise, AND any plane change together. The
// departure leg is therefore a full 3D BurnVector (Δv = v1 − vDep), not a
// prograde scalar. "Eccentric-aware departure" and "plane change" are not
// separate deliverables here — they fall out of the solve (ADR 0005).
//
// All states are in the shared primary's (inertially-oriented) frame:
//   - rDep, vDep: craft state at the departure epoch (now + depOffset).
//   - rArr, vArr: target state at the arrival epoch (now + depOffset + tof).
//
// The caller (sim layer) propagates the craft (analytic Kepler) and the
// target (ephemeris) to those epochs and seeds tof + depOffset from the
// shared intra-primary phasing (intraPrimaryPhasing).
//
// The arrival leg is the SOI-capture braking burn: the hyperbolic excess
// |v2 − vArr| converted to a circular-insertion Δv via CaptureBurnDeltaV,
// planted as a retrograde scalar at the target — matching
// PlanIntraPrimaryHohmann's arrival. The departure carries the geometry;
// the arrival just brakes into the target's SOI.
//
// v0.12.x+.
func PlanIntraPrimaryFused(
	mu float64,
	rDep, vDep orbital.Vec3,
	rArr, vArr orbital.Vec3,
	tof float64,
	depOffset time.Duration,
	departureID string,
	muTarget, rCapture float64,
	targetID string,
) (TransferPlan, error) {
	if mu <= 0 || tof <= 0 {
		return TransferPlan{}, errors.New("planfused: mu and tof must be > 0")
	}
	// Prograde short-way single-rev solve, matching PlanLambertTransfer's
	// convention for the heliocentric path. The Hohmann-seeded tof keeps
	// the transfer angle near a half-ellipse, where the short branch is
	// the intended trans-target arc.
	v1, v2, err := LambertSolve(rDep, rArr, tof, mu, false)
	if err != nil {
		return TransferPlan{}, err
	}
	depDV := v1.Sub(vDep)
	dvDep := depDV.Norm()
	if dvDep == 0 || math.IsNaN(dvDep) {
		return TransferPlan{}, errors.New("planfused: degenerate departure Δv")
	}
	vInfArr := v2.Sub(vArr).Norm()
	dvArr, err := CaptureBurnDeltaV(vInfArr, muTarget, rCapture)
	if err != nil {
		return TransferPlan{}, err
	}
	return TransferPlan{
		Departure: TransferNode{
			Leg:        LegDeparture,
			PrimaryID:  departureID,
			DV:         dvDep,
			OffsetTime: depOffset,
			BurnDir:    depDV.Scale(1 / dvDep),
		},
		Arrival: TransferNode{
			Leg:          LegArrival,
			PrimaryID:    targetID,
			DV:           dvArr,
			OffsetTime:   depOffset + time.Duration(tof*float64(time.Second)),
			IsRetrograde: true,
		},
		TransferDt: time.Duration(tof * float64(time.Second)),
	}, nil
}
