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
//   - craftAngleNow / targetAngleNow: current angular positions of
//     craft and target around the shared primary (radians, atan2 of
//     position-vector y, x in the primary's frame). Used for phase-
//     corrected launch-window timing — the departure burn fires when
//     target leads craft by (π − n_target · T_transfer), so craft
//     arrives at apoapsis when target is also there. v0.5.9+.
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
// minLeadSeconds (v0.5.11+): the planner pads the wait time τ by
// integer synodic periods until τ ≥ minLeadSeconds. Callers pass
// half the expected burn duration so the live integrator can center
// the finite burn on the planner's intended firing point without the
// trigger time falling in the past. Pass 0 to skip padding.
func PlanIntraPrimaryHohmann(
	mu float64,
	rDeparture, rArrival float64,
	craftAngleNow, targetAngleNow float64,
	minLeadSeconds float64,
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
	vInfArr := math.Abs(vArrCirc - vTransAtArr)
	dvArr, err := CaptureBurnDeltaV(vInfArr, muTarget, rCapture)
	if err != nil {
		return TransferPlan{}, err
	}

	// Coast time = half the transfer ellipse's period.
	transferTime := math.Pi * math.Sqrt(aT*aT*aT/mu)

	// Phase-corrected launch window. Mean motions:
	//   n_craft  = √(µ / r_dep³)   (parking orbit)
	//   n_target = √(µ / r_arr³)   (target's circular orbit)
	// At burn time, target should lead craft by:
	//   Δθ_required = π − n_target · T_transfer   (mod 2π)
	// Current phase difference: Δθ_now = θ_target − θ_craft (mod 2π)
	// Phase difference evolves as Δθ(t) = Δθ_now + (n_target − n_craft)·t.
	// Solve for smallest τ ≥ 0 such that Δθ(τ) ≡ Δθ_required (mod 2π).
	nCraft := math.Sqrt(mu / (rDeparture * rDeparture * rDeparture))
	nTarget := math.Sqrt(mu / (rArrival * rArrival * rArrival))
	requiredDelta := wrapTau(math.Pi - nTarget*transferTime)
	currentDelta := wrapTau(targetAngleNow - craftAngleNow)
	relativeRate := nTarget - nCraft // negative when craft is faster (LEO is)
	deltaToWait := requiredDelta - currentDelta
	if relativeRate > 0 {
		// Δθ increases over time — wait until next forward arrival at requiredDelta.
		for deltaToWait < 0 {
			deltaToWait += 2 * math.Pi
		}
	} else if relativeRate < 0 {
		// Δθ decreases over time — wait until next backward arrival at requiredDelta.
		for deltaToWait > 0 {
			deltaToWait -= 2 * math.Pi
		}
	}
	var waitTime float64
	if relativeRate != 0 {
		waitTime = deltaToWait / relativeRate
	}
	if waitTime < 0 {
		waitTime = 0
	}

	// Pad waitTime by integer synodic periods until it covers the
	// caller's minLeadSeconds. Synodic period = 2π / |relativeRate|;
	// each whole synodic period is another valid launch window with
	// the same phase geometry, so phasing remains correct.
	if minLeadSeconds > 0 && relativeRate != 0 {
		synodic := 2 * math.Pi / math.Abs(relativeRate)
		for waitTime < minLeadSeconds {
			waitTime += synodic
		}
	}

	outbound := rArrival > rDeparture
	depOffset := time.Duration(waitTime * float64(time.Second))
	return TransferPlan{
		Departure: TransferNode{
			Leg:          LegDeparture,
			PrimaryID:    departureID,
			DV:           math.Abs(dvDep),
			OffsetTime:   depOffset,
			IsRetrograde: !outbound,
		},
		Arrival: TransferNode{
			Leg:          LegArrival,
			PrimaryID:    targetID,
			DV:           dvArr,
			OffsetTime:   depOffset + time.Duration(transferTime*float64(time.Second)),
			IsRetrograde: outbound,
		},
		TransferDt: time.Duration(transferTime * float64(time.Second)),
	}, nil
}

// wrapTau normalises an angle to [0, 2π).
func wrapTau(a float64) float64 {
	const tau = 2 * math.Pi
	a = math.Mod(a, tau)
	if a < 0 {
		a += tau
	}
	return a
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
	retrograde bool,
) (TransferPlan, error) {
	if muSun <= 0 || tof <= 0 {
		return TransferPlan{}, errors.New("planlambert: muSun and tof must be > 0")
	}
	v1, v2, err := LambertSolve(rDep, rArr, tof, muSun, retrograde)
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
