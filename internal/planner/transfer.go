package planner

import (
	"errors"
	"math"
	"time"
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
