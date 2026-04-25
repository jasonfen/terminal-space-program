package planner

import (
	"errors"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TransferLeg names which end of a TransferPlan a node belongs to —
// helpful for HUDs and logging that want to show "departure" vs
// "arrival" without having to derive it from PrimaryID.
type TransferLeg int

const (
	LegDeparture TransferLeg = iota
	LegArrival
)

// TransferNode is a planner-layer description of a single burn that the
// sim layer will turn into a sim.ManeuverNode. We keep it free of any
// sim-package dependencies so planner stays a pure math/algorithms
// surface — sim.PlanTransfer adapts these into sim.ManeuverNodes.
type TransferNode struct {
	Leg          TransferLeg
	PrimaryID    string        // body whose frame the burn was planned in
	DV           float64       // m/s, magnitude
	OffsetTime   time.Duration // time after PlanTransfer returns when this fires
	IsRetrograde bool          // true → retrograde mode; false → prograde
}

// TransferPlan is the two-burn output of an auto-plant transfer.
// Departure fires at a parking-orbit periapsis around the origin
// primary; Arrival fires at the destination's SOI/circular-capture
// radius after the transfer ellipse coast.
type TransferPlan struct {
	Departure  TransferNode
	Arrival    TransferNode
	TransferDt time.Duration // coast time (Departure → Arrival)
}

// PlanHohmannTransfer constructs a Hohmann-style transfer plan from a
// circular parking orbit at radius rPark around a departure planet
// (helios distance rDeparture, gravitational parameter muDeparture) to
// a circular capture orbit at radius rCapture around a destination
// planet (helios distance rArrival, gravitational parameter
// muDestination), all heliocentric distances in the system primary's
// frame (mu = muSun).
//
// Result Δv magnitudes are the patched-conic Hohmann values:
//
//	departure: v∞_dep = sqrt(µ_sun · (2/r_dep − 1/a_t)) − v_dep_orbit
//	         Δv_dep = EscapeBurnDeltaV(v∞_dep, µ_planet, r_park)
//	arrival:   v∞_arr = v_arr_orbit − sqrt(µ_sun · (2/r_arr − 1/a_t))
//	         Δv_arr = CaptureBurnDeltaV(|v∞_arr|, µ_dest, r_capture)
//
// PrimaryIDs are set so the sim layer can render frame-aware glyphs
// and the planner UI can label the legs. Phasing is *not* accounted
// for — both burns assume the destination planet is at the right place
// at the right time, which the v0.3.1 sandbox doesn't enforce. A
// porkchop-plot screen (deferred to v0.3.2) is the natural next step.
func PlanHohmannTransfer(
	muSun float64,
	rDeparture, rArrival float64,
	muDeparture, rPark float64, departureID string,
	muDestination, rCapture float64, destinationID string,
) (TransferPlan, error) {
	if muSun <= 0 || rDeparture <= 0 || rArrival <= 0 {
		return TransferPlan{}, errors.New("planhohmann: heliocentric inputs must be positive")
	}
	if rDeparture == rArrival {
		return TransferPlan{}, errors.New("planhohmann: departure and arrival radii are equal — no transfer")
	}

	aT := (rDeparture + rArrival) / 2
	vDepOrbit := math.Sqrt(muSun / rDeparture)
	vArrOrbit := math.Sqrt(muSun / rArrival)
	vTransAtDep := math.Sqrt(muSun * (2/rDeparture - 1/aT))
	vTransAtArr := math.Sqrt(muSun * (2/rArrival - 1/aT))

	vInfDep := vTransAtDep - vDepOrbit
	vInfArr := vArrOrbit - vTransAtArr

	dvDep, err := EscapeBurnDeltaV(math.Abs(vInfDep), muDeparture, rPark)
	if err != nil {
		return TransferPlan{}, err
	}
	dvArr, err := CaptureBurnDeltaV(math.Abs(vInfArr), muDestination, rCapture)
	if err != nil {
		return TransferPlan{}, err
	}

	transferTime := math.Pi * math.Sqrt(aT*aT*aT/muSun)

	// Outbound (rArr > rDep): departure raises apoapsis (prograde),
	// arrival drops periapsis (retrograde — relative velocity points
	// against Mars's motion at intercept). Inbound: opposite.
	outbound := rArrival > rDeparture
	return TransferPlan{
		Departure: TransferNode{
			Leg:          LegDeparture,
			PrimaryID:    departureID,
			DV:           dvDep,
			OffsetTime:   0,
			IsRetrograde: !outbound,
		},
		Arrival: TransferNode{
			Leg:          LegArrival,
			PrimaryID:    destinationID,
			DV:           dvArr,
			OffsetTime:   time.Duration(transferTime * float64(time.Second)),
			IsRetrograde: outbound,
		},
		TransferDt: time.Duration(transferTime * float64(time.Second)),
	}, nil
}

// PlanIntraPrimaryHohmann constructs a Hohmann transfer for the case
// where craft and target both orbit the same primary (e.g. craft in
// LEO around Earth → Luna also around Earth). Pre-v0.5.7 PlanTransfer
// assumed craft and target both heliocentric; for moon targets it
// computed nonsense (Luna's parent-relative semimajor used as a
// heliocentric distance).
//
// Inputs:
//   - mu: GM of the shared primary (e.g. Earth's GM).
//   - rDeparture: craft's |R| in primary's frame (e.g. LEO radius
//     ≈ 6571 km for 200 km altitude).
//   - rArrival: target's semimajor axis around primary (e.g. Luna's
//     384 399 km).
//   - departureID: identifier of the primary (for ManeuverNode
//     PrimaryID labeling).
//   - muTarget, rCapture, targetID: target body's GM, capture-orbit
//     radius around target, and ID. Used for the SOI-entry braking
//     burn — closing speed at rendezvous becomes v∞ relative to
//     target, which CaptureBurnDeltaV converts to a circular-orbit
//     insertion Δv.
//
// Departure burn: prograde Δv at periapsis raising apoapsis to
// rArrival. Arrival burn: capture into target SOI at rArrival.
//
// Phasing not enforced — same caveat as PlanHohmannTransfer.
func PlanIntraPrimaryHohmann(
	mu float64,
	rDeparture, rArrival float64,
	departureID string,
	muTarget, rCapture float64,
	targetID string,
) (TransferPlan, error) {
	if mu <= 0 || rDeparture <= 0 || rArrival <= 0 {
		return TransferPlan{}, errors.New("planintra: mu and radii must be positive")
	}
	if rDeparture == rArrival {
		return TransferPlan{}, errors.New("planintra: departure and arrival radii are equal — no transfer")
	}

	aT := (rDeparture + rArrival) / 2
	vDepCirc := math.Sqrt(mu / rDeparture)
	vArrCirc := math.Sqrt(mu / rArrival)
	vTransAtDep := math.Sqrt(mu * (2/rDeparture - 1/aT))
	vTransAtArr := math.Sqrt(mu * (2/rArrival - 1/aT))

	dvDep := vTransAtDep - vDepCirc
	// Closing speed at rendezvous: target moves at vArrCirc, craft
	// arrives with vTransAtArr. Outbound: target faster, craft is
	// slower → target catches craft from "behind" relative to
	// rendezvous geometry, vInf = vArrCirc - vTransAtArr.
	vInfArr := math.Abs(vArrCirc - vTransAtArr)
	dvArr, err := CaptureBurnDeltaV(vInfArr, muTarget, rCapture)
	if err != nil {
		return TransferPlan{}, err
	}

	// Coast time = half the transfer ellipse's period.
	transferTime := math.Pi * math.Sqrt(aT*aT*aT/mu)

	outbound := rArrival > rDeparture
	return TransferPlan{
		Departure: TransferNode{
			Leg:          LegDeparture,
			PrimaryID:    departureID,
			DV:           math.Abs(dvDep),
			OffsetTime:   0,
			IsRetrograde: !outbound,
		},
		Arrival: TransferNode{
			Leg:          LegArrival,
			PrimaryID:    targetID,
			DV:           dvArr,
			OffsetTime:   time.Duration(transferTime * float64(time.Second)),
			IsRetrograde: outbound,
		},
		TransferDt: time.Duration(transferTime * float64(time.Second)),
	}, nil
}

// PlanLambertTransfer builds a two-burn transfer for an arbitrary
// (departure-time, time-of-flight) pair using a single-rev Lambert
// solve for the heliocentric coast. Unlike PlanHohmannTransfer which
// assumes 180° opposition geometry, this supports off-Hohmann launch
// windows — the same geometry the porkchop grid scores, so Enter-to-
// plant from the porkchop cursor is a direct call-through.
//
// Inputs: heliocentric state of departure body at t_dep, heliocentric
// state of arrival body at t_dep + tof, transfer TOF in seconds, plus
// the parking / capture orbit parameters used for the patched-conic
// Δv identity (matching PlanHohmannTransfer + PorkchopGrid).
//
// depOffset is the wall-clock delay from "now" (sim-time at planning)
// until the departure burn; it becomes the Departure node's OffsetTime.
// The Arrival node's OffsetTime is depOffset + tof.
//
// Retrograde flags follow the same outbound/inbound rule as
// PlanHohmannTransfer: outbound (|rArr| > |rDep|) gets a prograde
// departure + retrograde arrival; inbound flips both. Lambert
// geometry varies more than Hohmann's 180° opposition, but the
// radius-based sign captures the common case well enough for the
// porkchop cursor and we can revisit if off-Hohmann arrivals need
// a sharper rule.
func PlanLambertTransfer(
	muSun float64,
	rDep, vDepBody orbital.Vec3,
	rArr, vArrBody orbital.Vec3,
	tof float64,
	muDeparture, rPark float64, departureID string,
	muDestination, rCapture float64, destinationID string,
	depOffset time.Duration,
) (TransferPlan, error) {
	if muSun <= 0 || tof <= 0 {
		return TransferPlan{}, errors.New("planlambert: muSun and tof must be > 0")
	}
	v1, v2, err := LambertSolve(rDep, rArr, tof, muSun)
	if err != nil {
		return TransferPlan{}, err
	}
	vInfDep := v1.Sub(vDepBody)
	vInfArr := v2.Sub(vArrBody)
	dvDep, err := EscapeBurnDeltaV(vInfDep.Norm(), muDeparture, rPark)
	if err != nil {
		return TransferPlan{}, err
	}
	dvArr, err := CaptureBurnDeltaV(vInfArr.Norm(), muDestination, rCapture)
	if err != nil {
		return TransferPlan{}, err
	}
	outbound := rArr.Norm() > rDep.Norm()
	depRetro := !outbound
	arrRetro := outbound
	return TransferPlan{
		Departure: TransferNode{
			Leg:          LegDeparture,
			PrimaryID:    departureID,
			DV:           dvDep,
			OffsetTime:   depOffset,
			IsRetrograde: depRetro,
		},
		Arrival: TransferNode{
			Leg:          LegArrival,
			PrimaryID:    destinationID,
			DV:           dvArr,
			OffsetTime:   depOffset + time.Duration(tof*float64(time.Second)),
			IsRetrograde: arrRetro,
		},
		TransferDt: time.Duration(tof * float64(time.Second)),
	}, nil
}
