package planner

import (
	"errors"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// MeetingPlace (ADR 0045 §2, #398) picks which craft's orbit the
// rendezvous meeting point lives on. Whoever HOLDS never burns — their
// future position is confined to their own unchanged orbit, so the
// meeting point necessarily lies on the holder's path. The Meeting
// Planner solves for the burn the MOVER needs to arrive there.
type MeetingPlace int

const (
	// MeetingCrossing — "the crossing": NOT YET IMPLEMENTED. Intended to
	// anchor at wherever the two CURRENT, unburned courses already come
	// closest within the shared search horizon (the same one the
	// TARGET chip / Engage use, ADR 0045 S1) — "an existing
	// intersection" — cheapest when a close approach already exists.
	// PR #412 tried to build this (Kepler-propagate both craft to the
	// crossing instant tCA, then solve there) and shipped it broken:
	// the returned burn is only correct AT tCA, but the caller always
	// plants the node to fire at "soon" (TriggerTime = now + a slew
	// lead), not at tCA, so the planted burn routinely missed by
	// megametres (review round 2). Reverted rather than fixed forward —
	// see ErrMeetingCrossingNotImplemented. RecommendMeetingLadder
	// always refuses for this Place; "their orbit" / "your orbit" are
	// the two working Places.
	MeetingCrossing MeetingPlace = iota
	// MeetingTheirOrbit — "their orbit": the target holds, the active
	// craft (A) is the mover and burns to arrive wherever the target's
	// own unchanged orbit carries it.
	MeetingTheirOrbit
	// MeetingYourOrbit — "your orbit": the active craft holds, the
	// TARGET (B) is the mover — the returned ladder's rows describe a
	// burn for the *partner*, not the active craft. This slice computes
	// it; planting it on a remote craft is out of scope (S6/S7 own
	// delivery).
	MeetingYourOrbit
)

// String labels match the ADR 0045 vocabulary ("their orbit" / "your
// orbit" / "the crossing") verbatim so HUD callers (S6) don't need a
// parallel naming table.
func (m MeetingPlace) String() string {
	switch m {
	case MeetingCrossing:
		return "the crossing"
	case MeetingTheirOrbit:
		return "their orbit"
	case MeetingYourOrbit:
		return "your orbit"
	}
	return "?"
}

// MeetingBurnOption is one row of the Lap Ladder: the single Meeting
// Burn a chosen lap count plants, generalising RendezvousAdvisory's
// Ok/Reason/ArrivalSpeed shape (ADR 0039 S1) to a multi-row picker.
//
// Ok=true: DV / BurnDir / AchievableCA / TArrival / ArrivalSpeed are
// populated and a plant-side caller can build a BurnVector maneuver
// node directly (see spacecraft.BurnVector — the same mechanism the
// fused-Lambert departure planter uses for an arbitrary 3D Δv the
// discrete axis modes can't express).
//
// Ok=false: Reason carries the gate that fired
// ("no meeting solution" | "burn drops periapsis unsafely" |
// "unaffordable"). The row is still returned, not dropped — ADR 0045
// §2: "unaffordable rows are shown but not plantable, so the trade
// stays visible."
type MeetingBurnOption struct {
	Laps int // the ladder row's lap count

	Ok     bool
	Reason string

	DV      float64      // scalar Δv magnitude, m/s
	BurnDir orbital.Vec3 // unit thrust direction, mover's frame (== stateA/stateB's frame)

	TArrival float64 // s from now until the meeting time (the ladder's "wait")

	// AchievableCA / ArrivalSpeed come from an independent analytic
	// (Kepler) propagation of BOTH the post-burn mover and the
	// (unburned) holder out to TArrival — not read off the closed-form
	// solve directly — so a derivation bug shows up as a nonzero
	// AchievableCA rather than being silently trusted away.
	// meetingLadderCore gates Ok on this being within
	// meetingAchievableCATolFrac of r0mag — a row whose own propagated
	// check doesn't land close is never Ok=true (see that constant's
	// doc comment).
	AchievableCA float64 // m, separation at the propagated meeting time
	ArrivalSpeed float64 // m/s, |v_rel| at that same instant
}

// MeetingLadder is the result of RecommendMeetingLadder: every row the
// solver produced for one MeetingPlace, plus which of the two input
// states (A or B) is the mover — the row a plant-side caller must burn.
type MeetingLadder struct {
	Place    MeetingPlace
	MoverIsA bool // true: stateA (typically the active craft) burns; false: stateB (the partner) does
	Rows     []MeetingBurnOption
}

// meetingCandidateLaps is the fixed lap-count ladder every MeetingPlace
// scans. Short list per ADR 0045 §2's own example (2 / 5 / 20); a
// couple of extra rungs give S6's picker room to show a smoother
// wait-vs-cost curve.
var meetingCandidateLaps = []int{2, 3, 5, 10, 20}

// meetingPlaneTolDeg is the coplanar tolerance the Meeting Planner
// refuses beyond, naming [I] (PlanVesselPlaneMatch, ADR 0045 S4/#397)
// as the remedy. Mirrors hohmannInclTolDeg's precedent
// (internal/sim/hohmann_guard.go) — ~1° is already a large miss at
// any orbital distance, and the Meeting Planner's tangential solve
// (meetingLadderCore) assumes a coplanar geometry; this slice
// explicitly narrows scope to coplanar (#398 "out of scope: the plane
// rung").
const meetingPlaneTolDeg = 1.0

// meetingAchievableCATolFrac gates Ok on the row's own AchievableCA
// (review finding: "Ok never consults AchievableCA"). meetingLadderCore
// models the holder as sweeping at a uniform angular rate to derive t0
// — exact for a circular holder, a first-order approximation for an
// eccentric one (see meetingLadderCore's doc comment) — so a row can
// pass every other gate (r0 in [holderPeri, holderApo], periapsis
// safety, affordability) while the burn it actually prices misses the
// holder by a large fraction of the orbit. AchievableCA is already the
// row's own propagate-and-check number (both craft advanced via
// physics.KeplerStep to TArrival); this constant is what turns that
// number into a gate instead of a display-only field.
//
// 0.1% of r0mag is generous next to the near-machine-precision residual
// a genuinely circular holder produces (TestRecommendMeetingLadder_
// TheirOrbit_PhaseOffsetConverges asserts <5 km on a 6.87e6 m orbit,
// several orders of magnitude under this bound) but far tighter than
// the miss an eccentric holder's uniform-sweep approximation actually
// produces: measured on a perigee 6771 km / apogee 12000 km orbit
// (e≈0.28) with two co-orbital craft 1/6 period apart, every row's
// AchievableCA came out ≈6,563,744 m against r0mag≈6.77e6 m — 97% of
// r0mag, not a rounding error.
//
// Was 1% (review finding, LOW): that admitted Ok=true rows whose own
// propagated AchievableCA ran into the tens of kilometres — measured
// (500 km LEO calibration radius, r0mag≈6.871e6 m, a mildly eccentric
// holder e in [0.002, 0.01] a small fraction of a period ahead of the
// mover) Ok=true rows at 12,717 / 14,959 / 26,142 / 49,422 m, all under
// the old 1% (≈68,710 m) bound. Since Ok is what an Engage commit
// treats as "this is a meeting" (MeetingArrivalSec/AchievableCA flow
// straight through PlanMeetingBurn to the planted node), a tens-of-km
// miss shouldn't read as one. 0.1% (≈6,871 m at that same radius)
// clears every one of those four measured values while still passing
// every row this package's own tests expect to succeed — none of them
// exercise an eccentric HOLDER on a geometry meant to stay Ok=true
// (the only eccentric-holder case, TestRecommendMeetingLadder_
// EccentricHolder_UnachievableRefused, expects total refusal at a
// ~97%-of-r0mag miss, far outside either bound).
const meetingAchievableCATolFrac = 0.001

var (
	// ErrMeetingPlaneMismatch: the two orbital planes differ by more
	// than meetingPlaneTolDeg. The Meeting Planner assumes coplanar
	// geometry (ADR 0045 S5 scope) — [I] (PlanVesselPlaneMatch) is the
	// tool that rotates into the target's plane; named here so the
	// caller's refusal text can point at it directly, matching K's own
	// "your planes differ — match theirs [I] first" doctrine.
	ErrMeetingPlaneMismatch = errors.New("meeting: orbital planes differ — match theirs [I] first")
	// ErrMeetingNoCrossing: MeetingCrossing found no closest approach
	// on the current, unburned courses within the shared search
	// horizon — there is no "existing intersection" to be cheap about.
	// The caller's remedy is to pick "their orbit" or "your orbit"
	// instead, which don't depend on one already existing.
	ErrMeetingNoCrossing = errors.New("meeting: no natural encounter within the search horizon — try \"their orbit\" or \"your orbit\"")
	// ErrMeetingCrossingNotImplemented: MeetingCrossing found a natural
	// crossing (NextClosestApproach converged within
	// crossingSearchHorizon — see ErrMeetingNoCrossing for when it
	// doesn't) but this build has no solver for it. PR #412 tried to
	// anchor meetingLadderCore's tangential solve at the crossing
	// instant tCA (Kepler-propagate both craft there, then solve as if
	// "now" were tCA); the row it returned was only correct for a burn
	// executed AT tCA, but internal/sim.PlanMeetingBurn always plants
	// the resulting node to fire at TriggerTime = now + a slew lead —
	// not at tCA — so the planted burn described a course correction
	// for a position/velocity the mover was never at when the node
	// actually fired. Measured (review round 2, two-craft LEO fixture,
	// four configs): advertised AchievableCA in the 15,000-26,000 m
	// range while the burn actually planted (propagated the same way
	// Engage does, via rendezvousCommitFromPlantedMeetingNode) missed
	// by 1.4-16 million metres. Reverted rather than fixed forward — a
	// correct version needs TriggerTime itself to mean "fire at the
	// crossing", which is a bigger change than this patch makes. Until
	// that lands, "the crossing" refuses outright rather than silently
	// falling back to duplicating MeetingTheirOrbit (the pre-#412
	// behavior a previous review flagged as an inert decoy) — a picker
	// walking here sees a plain refusal, never a plantable row that
	// lies about where it lands.
	ErrMeetingCrossingNotImplemented = errors.New("meeting: \"the crossing\" isn't implemented yet — try \"their orbit\" or \"your orbit\"")
	// ErrMeetingSizeMismatch: the mover's current orbital radius never
	// falls within the holder's own [periapsis, apoapsis] band, so
	// there is no point on the holder's UNCHANGED orbit the tangential
	// "return to my own current position" model (see meetingLadderCore)
	// can aim at. A KNOWN, DOCUMENTED simplification of this slice
	// (see meetingLadderCore's doc comment) rather than the fully
	// general "the same burn also carries you to the other altitude"
	// solve ADR 0045 §2 describes — that combined reshape+phase case is
	// a natural follow-on, not required for #398's acceptance criteria
	// (all of which use matched or near-matched orbit sizes). [H]/[m]
	// remain the tools for a genuine size mismatch.
	ErrMeetingSizeMismatch = errors.New("meeting: orbits differ too much in size for this Meeting Place — plan a transfer [H] first")
	// errMeetingInvalidInput: non-positive mu or search horizon —
	// mirrors RecommendRendezvousNudge's "horizon too short" input
	// guard.
	errMeetingInvalidInput = errors.New("meeting: invalid input (mu or horizon)")
)

// RecommendMeetingLadder is the Meeting Planner's solver (ADR 0045 §2,
// #398): given the active craft's state (stateA) and the target's
// state (stateB) in a common primary-relative frame, produce the Lap
// Ladder for the chosen MeetingPlace.
//
// stateA/stateB must already be in the same frame — same convention as
// NextClosestApproach / RecommendRendezvousNudge; the sim layer
// resolves cross-primary conversion before calling in
// (TargetStateRelativeToActivePrimary).
//
// crossingSearchHorizon bounds ONLY the MeetingCrossing existence check
// (NextClosestApproach on the current, unburned courses, used solely
// to tell ErrMeetingNoCrossing apart from ErrMeetingCrossingNotImplemented
// — MeetingCrossing has no working solver yet, see that sentinel's doc
// comment) — this must be the same flat search horizon every other
// rendezvous surface uses (ADR 0045 S1 / #394, the sim layer's
// rendezvousCommitHorizonSec), never a private constant. It does not
// bound the ladder's own arrival times for the two working Places,
// which are driven by lap count and can run well past it — that is the
// entire point of this tool (ADR 0045 §2: "the 4h window bounds a
// search, not a plan").
//
// moverRemainingDV is the mover's Spacecraft.RemainingDeltaV() (ADR
// 0045 §2's affordability check). A NEGATIVE value means unknown
// (e.g. a remote ghost's fuel state is unobservable) — every row is
// then reported affordable. This is deliberately NOT the "<= 0" idiom
// PreviewBurnState's "fuelDv > 0 && ..." uses (internal/sim/
// maneuver.go) — there, a craft that has genuinely run dry reads
// RemainingDeltaV()==0, and this affordability check must still gate
// on that (every priced row unaffordable), not treat it as "unknown,
// let everything through".
//
// Returns a non-nil error for structural refusals: bad input,
// non-coplanar geometry, or MeetingCrossing (always — see
// ErrMeetingCrossingNotImplemented, this Place has no working solver).
// Per-row gate failures on the two working Places (unaffordable, unsafe
// periapsis, no meeting solution for a given lap count) are reported IN
// the ladder's Rows, Ok=false — visible, not hidden (ADR 0045 §2).
func RecommendMeetingLadder(
	stateA, stateB orbital.Vec3State,
	primary bodies.CelestialBody,
	mu float64,
	place MeetingPlace,
	crossingSearchHorizon, moverRemainingDV float64,
) (MeetingLadder, error) {
	if mu <= 0 {
		return MeetingLadder{}, errMeetingInvalidInput
	}
	if !meetingCoplanar(stateA, stateB) {
		return MeetingLadder{}, ErrMeetingPlaneMismatch
	}

	switch place {
	case MeetingTheirOrbit:
		rows, err := meetingLadderCore(stateA, stateB, primary, mu, moverRemainingDV)
		if err != nil {
			return MeetingLadder{}, err
		}
		return MeetingLadder{Place: place, MoverIsA: true, Rows: rows}, nil
	case MeetingYourOrbit:
		rows, err := meetingLadderCore(stateB, stateA, primary, mu, moverRemainingDV)
		if err != nil {
			return MeetingLadder{}, err
		}
		return MeetingLadder{Place: place, MoverIsA: false, Rows: rows}, nil
	case MeetingCrossing:
		// Regression revert (review round 2): PR #412 replaced this
		// existence-check-only refusal with an attempt to Kepler-
		// propagate both craft to the crossing instant tCA and solve
		// there. That attempt is gone — see ErrMeetingCrossingNotImplemented's
		// doc comment for the measured failure and why this wasn't
		// fixed forward. The existence check stays (it's cheap, and it
		// lets the refusal distinguish "no natural crossing exists" from
		// "one exists but this build can't solve it") but its result is
		// never fed into meetingLadderCore — there is no solve to feed
		// it into. This intentionally reproduces this Place's pre-#412
		// behavior MINUS the decoy: instead of silently running
		// MeetingTheirOrbit's solve and calling it "the crossing" (a
		// previous review's own "MeetingCrossing is inert" finding),
		// it refuses outright and says so.
		if crossingSearchHorizon <= 0 {
			return MeetingLadder{}, errMeetingInvalidInput
		}
		if _, _, _, err := NextClosestApproach(stateA, stateB, primary, mu, crossingSearchHorizon); err != nil {
			return MeetingLadder{}, ErrMeetingNoCrossing
		}
		return MeetingLadder{}, ErrMeetingCrossingNotImplemented
	}
	return MeetingLadder{}, errMeetingInvalidInput
}

// meetingCoplanar reports whether stateA/stateB's orbital planes agree
// within meetingPlaneTolDeg. Degenerate (zero angular-momentum) states
// are treated as coplanar — no false refusal on a state the caller
// couldn't have derived a plane from anyway; meetingLadderCore's own
// input guards (r0mag/v0mag/pHolder/nHmag checks) fail such a state on
// its own merits downstream.
func meetingCoplanar(stateA, stateB orbital.Vec3State) bool {
	hA := stateA.R.Cross(stateA.V)
	hB := stateB.R.Cross(stateB.V)
	ma, mb := hA.Norm(), hB.Norm()
	if ma == 0 || mb == 0 {
		return true
	}
	cosAngle := hA.Dot(hB) / (ma * mb)
	if cosAngle > 1 {
		cosAngle = 1
	} else if cosAngle < -1 {
		cosAngle = -1
	}
	angleDeg := math.Acos(cosAngle) * 180 / math.Pi
	return angleDeg <= meetingPlaneTolDeg
}

// meetingLadderCore is the shared solve behind every MeetingPlace: mover
// burns TANGENTIALLY (prograde/retrograde — along its own current
// velocity direction, magnitude only) at its CURRENT position, holder
// never changes. Each candidate lap count picks a different post-burn
// period, hence a different Δv.
//
// The model: a tangential burn at position r0 leaves r0 ON the new
// orbit (any Keplerian orbit revisits every one of its points every
// period, apsis or not), so the mover returns to r0 EXACTLY at
// t = N·P'(Δv) for any lap count N. The holder, meanwhile, is treated
// as sweeping its current orbit at a fixed angular rate (exact for a
// circular holder; a first-order approximation for a mildly eccentric
// one — this slice's fixtures and acceptance criteria are all
// matched/near-matched circular-ish orbits, so this isn't exercised
// off that domain) and reaches r0's angular position at
// t = t0 + m·P_holder for m = 0, 1, 2, .... Solving
// N·P'(Δv) = t0 + m·P_holder for Δv (monotonic in Δv — bisection, see
// solveTangentialSpeedForPeriod) gives the single burn that lands
// BOTH craft at r0 simultaneously. Exactly two natural m are tried per
// N — the bracketing pair either side of "P' unchanged" (ADR 0045 §2's
// "direction is derived, not chosen: take whichever needs the smaller
// period change") — and whichever clears the safety gate with the
// smaller |Δv| wins; a geometry where BOTH brackets are unsafe is
// rejected rather than searched around ("fall back to the other when
// blocked" — not "search until something works").
//
// This DELIBERATELY does not implement the fully general "if the
// orbits differ, the same burn also carries you to the other
// altitude" case ADR 0045 §2 describes for arbitrary size mismatches
// — see ErrMeetingSizeMismatch. r0 must fall within the holder's own
// [periapsis, apoapsis] band (trivially true for same-radius circular
// orbits, and true near a genuine "crossing") for this model to have
// anywhere to aim.
func meetingLadderCore(moverState, holderState orbital.Vec3State, primary bodies.CelestialBody, mu float64, moverRemainingDV float64) ([]MeetingBurnOption, error) {
	r0 := moverState.R
	v0 := moverState.V
	r0mag := r0.Norm()
	v0mag := v0.Norm()
	if r0mag <= 0 || v0mag <= 0 {
		return nil, errMeetingInvalidInput
	}
	v0hat := v0.Scale(1 / v0mag)

	pMoverOrig := orbitalPeriod(physics.StateVector{R: r0, V: v0}, mu)
	if math.IsInf(pMoverOrig, 0) || math.IsNaN(pMoverOrig) || pMoverOrig <= 0 {
		return nil, errMeetingInvalidInput
	}

	pHolder := orbitalPeriod(physics.StateVector{R: holderState.R, V: holderState.V}, mu)
	if math.IsInf(pHolder, 0) || math.IsNaN(pHolder) || pHolder <= 0 {
		return nil, errMeetingInvalidInput
	}

	hEl := orbital.ElementsFromState(holderState.R, holderState.V, mu)
	if hEl.A <= 0 || hEl.E >= 1 {
		return nil, errMeetingInvalidInput
	}
	holderPeri := hEl.Periapsis()
	holderApo := hEl.A * (1 + hEl.E)
	// Tolerance: floating point can put an EXACTLY-matched-radius r0
	// a few ULPs outside [holderPeri, holderApo] (the calibration
	// scenario's whole point — same-radius circular orbits).
	const reachTol = 1.0 // 1 m
	if r0mag < holderPeri-reachTol || r0mag > holderApo+reachTol {
		return nil, ErrMeetingSizeMismatch
	}

	// t0: time (from now) until the holder's angular position first
	// coincides with r0's, sweeping in the holder's own rotation sense.
	nH := holderState.R.Cross(holderState.V)
	nHmag := nH.Norm()
	if nHmag == 0 {
		return nil, errMeetingInvalidInput
	}
	nHhat := nH.Scale(1 / nHmag)
	rHmag := holderState.R.Norm()
	if rHmag <= 0 {
		return nil, errMeetingInvalidInput
	}
	cosPhi := holderState.R.Dot(r0) / (rHmag * r0mag)
	if cosPhi > 1 {
		cosPhi = 1
	} else if cosPhi < -1 {
		cosPhi = -1
	}
	sinPhi := holderState.R.Cross(r0).Dot(nHhat) / (rHmag * r0mag)
	dPhi0 := math.Atan2(sinPhi, cosPhi)
	if dPhi0 < 0 {
		dPhi0 += 2 * math.Pi
	}
	t0 := dPhi0 / (2 * math.Pi) * pHolder

	rows := make([]MeetingBurnOption, 0, len(meetingCandidateLaps))
	for _, n := range meetingCandidateLaps {
		// Exactly two natural candidates per ADR 0045 §2 ("direction is
		// derived, not chosen: ... take whichever ... needs the smaller
		// period change. Fall back to the other when blocked"): mRaw is
		// the real-valued m that would make P' exactly equal
		// pMoverOrig's own cadence; its floor is the "catch up by
		// dropping" bracket (shorter T, smaller/negative Δv) and its
		// ceiling is "fall back by raising" (longer T, larger/positive
		// Δv) — the two period changes bracketing zero. Trying more than
		// these two would let the ladder route around a genuinely unsafe
		// geometry by always finding some safe far-flung alternative,
		// which defeats the safety gate's purpose rather than
		// demonstrating "fall back to the other when blocked".
		mRaw := (float64(n)*pMoverOrig - t0) / pHolder
		mFloor := math.Floor(mRaw)
		var best MeetingBurnOption
		var bestSafeFlag, bestAchievableFlag bool
		bestFound := false
		var bestGood MeetingBurnOption
		bestGoodFound := false
		for _, m := range [...]float64{mFloor, mFloor + 1} {
			if m < 0 {
				continue
			}
			Ttarget := t0 + m*pHolder
			if Ttarget <= 0 {
				continue
			}
			Pprime := Ttarget / float64(n)
			vPrime, vok := solveTangentialSpeedForPeriod(Pprime, r0mag, mu)
			if !vok {
				continue
			}
			dv := vPrime - v0mag
			newV := v0hat.Scale(vPrime)

			moverSV, mok := physics.KeplerStep(physics.StateVector{R: r0, V: newV}, mu, Ttarget)
			holderSV, hok := physics.KeplerStep(physics.StateVector{R: holderState.R, V: holderState.V}, mu, Ttarget)
			if !mok || !hok {
				continue
			}
			achievedCA := moverSV.R.Sub(holderSV.R).Norm()
			vRelAtT := moverSV.V.Sub(holderSV.V)

			cand := MeetingBurnOption{
				Laps:         n,
				DV:           math.Abs(dv),
				BurnDir:      v0hat.Scale(math.Copysign(1, dv)),
				TArrival:     Ttarget,
				AchievableCA: achievedCA,
				ArrivalSpeed: vRelAtT.Norm(),
			}
			safe := orbitSafetyGate(r0, v0, r0, newV, primary, mu)
			achievable := cand.AchievableCA <= meetingAchievableCATolFrac*r0mag

			if !bestFound || cand.DV < best.DV {
				best, bestFound = cand, true
				bestSafeFlag, bestAchievableFlag = safe, achievable
			}
			if safe && achievable && (!bestGoodFound || cand.DV < bestGood.DV) {
				bestGood, bestGoodFound = cand, true
			}
		}
		if !bestFound {
			rows = append(rows, MeetingBurnOption{Laps: n, Ok: false, Reason: "no meeting solution"})
			continue
		}

		chosen := best
		chosenSafe := bestSafeFlag
		chosenAchievable := bestAchievableFlag
		if bestGoodFound {
			chosen = bestGood
			chosenSafe, chosenAchievable = true, true
		}

		affordable := moverRemainingDV < 0 || chosen.DV <= moverRemainingDV
		switch {
		case !chosenSafe:
			chosen.Reason = "burn drops periapsis unsafely"
		case !chosenAchievable:
			// The row's own propagate-and-check (AchievableCA) landed
			// outside meetingAchievableCATolFrac of r0mag — the
			// uniform-angular-rate holder model (see this function's
			// doc comment) doesn't hold for this geometry, so the
			// solved burn doesn't actually deliver a meeting. Same
			// reason text as the "no bracket converged at all" case
			// above: both mean "this lap count has no usable meeting
			// solution", just discovered at a different stage.
			chosen.Reason = "no meeting solution"
		case !affordable:
			chosen.Reason = "unaffordable"
		default:
			chosen.Ok = true
		}
		rows = append(rows, chosen)
	}
	return rows, nil
}

// tangentialPeriodForSpeed returns the orbital period of a Keplerian
// orbit whose energy comes from speed vPrime at radius r0mag (tangent
// burn — the direction doesn't affect the resulting scalar energy).
// ok=false for a hyperbolic/parabolic result (vPrime at or above local
// escape velocity).
func tangentialPeriodForSpeed(vPrime, r0mag, mu float64) (period float64, ok bool) {
	denom := 2/r0mag - vPrime*vPrime/mu
	if denom <= 0 {
		return 0, false
	}
	a := 1 / denom
	if a <= 0 || math.IsInf(a, 0) || math.IsNaN(a) {
		return 0, false
	}
	return 2 * math.Pi * math.Sqrt(a*a*a/mu), true
}

// solveTangentialSpeedForPeriod inverts tangentialPeriodForSpeed via
// bisection: tangentialPeriodForSpeed is strictly increasing in vPrime
// over (0, vEscape), so a fixed-iteration bisection converges to
// near-machine precision.
func solveTangentialSpeedForPeriod(targetPeriod, r0mag, mu float64) (vPrime float64, ok bool) {
	if targetPeriod <= 0 || r0mag <= 0 || mu <= 0 {
		return 0, false
	}
	vEscape := math.Sqrt(2 * mu / r0mag)
	lo, hi := 1e-6*vEscape, vEscape*(1-1e-12)
	pLo, loOk := tangentialPeriodForSpeed(lo, r0mag, mu)
	if !loOk || pLo > targetPeriod {
		// Even the slowest sampled speed already exceeds the target
		// period (an extremely short target period) — no solution in
		// the elliptic domain this model covers.
		return 0, false
	}
	for i := 0; i < 100; i++ {
		mid := (lo + hi) / 2
		p, pok := tangentialPeriodForSpeed(mid, r0mag, mu)
		if !pok {
			hi = mid
			continue
		}
		if p < targetPeriod {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2, true
}

// orbitSafetyGate reuses RecommendRendezvousNudge's v0.10.3+ step-6
// periapsis check VERBATIM (ADR 0045 §2 explicitly calls for this):
// reject a burn whose post-burn periapsis either falls below the
// primary's surface+50 km floor, or drops more than 100 km from the
// pre-burn periapsis. Same physical failure mode either caller can hit
// — a phasing burn that deorbits the chaser — so the gate is shared
// rather than re-derived. preR/preV is the state BEFORE the burn (for
// the pre-burn periapsis reference); postR/postV is the state
// immediately after (same position, burned velocity, for an impulsive
// tangential-style Δv).
func orbitSafetyGate(preR, preV, postR, postV orbital.Vec3, primary bodies.CelestialBody, mu float64) bool {
	prePeri := orbital.ElementsFromState(preR, preV, mu).Periapsis()
	postPeri := orbital.ElementsFromState(postR, postV, mu).Periapsis()
	periSurfaceFloor := primary.RadiusMeters() + 50_000.0
	periDropLimit := prePeri - 100_000.0
	if primary.RadiusMeters() > 0 && postPeri < periSurfaceFloor {
		return false
	}
	if postPeri < periDropLimit {
		return false
	}
	return true
}
