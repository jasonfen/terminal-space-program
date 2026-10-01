package planner

import (
	"errors"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// RendezvousOrbit (ADR 0045 §2, #398) picks which craft's orbit the
// rendezvous point lives on. Whoever HOLDS never burns — their
// future position is confined to their own unchanged orbit, so the
// rendezvous point necessarily lies on the holder's path. The Rendezvous
// Planner solves for the burn the MOVER needs to arrive there.
type RendezvousOrbit int

const (
	// RendezvousCrossing — "the crossing" (G4 Q3, #416): the mover coasts
	// to the geometric intersection of the two coplanar orbits, burns the
	// same tangential lap-time change there, and meets N laps later. The
	// row's TBurn is the coast. (PR #412's version solved at the crossing
	// but planted at "now + lead" and missed by megametres; the row now
	// carries its burn epoch and the plant reads it.)
	RendezvousCrossing RendezvousOrbit = iota
	// RendezvousTheirOrbit — "their orbit": the target holds, the active
	// craft (A) is the mover and burns to arrive wherever the target's
	// own unchanged orbit carries it.
	RendezvousTheirOrbit
	// RendezvousYourOrbit — "your orbit": the active craft holds, the
	// TARGET (B) is the mover — the returned ladder's rows describe a
	// burn for the *partner*, not the active craft. This slice computes
	// it; planting it on a remote craft is out of scope (S6/S7 own
	// delivery).
	RendezvousYourOrbit
)

// String labels match the ADR 0045 vocabulary ("their orbit" / "your
// orbit" / "the crossing") verbatim so HUD callers (S6) don't need a
// parallel naming table.
func (m RendezvousOrbit) String() string {
	switch m {
	case RendezvousCrossing:
		return "the crossing"
	case RendezvousTheirOrbit:
		return "their orbit"
	case RendezvousYourOrbit:
		return "your orbit"
	}
	return "?"
}

// RendezvousBurnOption is one row of the Lap Ladder: the single Rendezvous
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
// ("no rendezvous solution" | "burn drops periapsis unsafely" |
// "unaffordable"). The row is still returned, not dropped — ADR 0045
// §2: "unaffordable rows are shown but not plantable, so the trade
// stays visible."
type RendezvousBurnOption struct {
	Laps int // the ladder row's lap count

	Ok     bool
	Reason string

	DV      float64      // scalar Δv magnitude, m/s
	BurnDir orbital.Vec3 // unit thrust direction, mover's frame (== stateA/stateB's frame)

	// TBurn is seconds from now until the burn fires (G4 Q2, #418/#416): the
	// lead time every row carries, or the coast to the crossing. TArrival
	// is seconds from now until the rendezvous (the ladder's "wait"), so
	// the flight AFTER the burn is TArrival - TBurn (FlightSec), which is
	// what a planted node stores as RendezvousArrivalSec.
	TBurn    float64
	TArrival float64

	// AchievableCA / ArrivalSpeed come from an independent analytic
	// (Kepler) propagation of BOTH the post-burn mover and the
	// (unburned) holder out to TArrival — not read off the closed-form
	// solve directly — so a derivation bug shows up as a nonzero
	// AchievableCA rather than being silently trusted away.
	// rendezvousLadderCore gates Ok on this being within
	// rendezvousAchievableCATolFrac of r0mag — a row whose own propagated
	// check doesn't land close is never Ok=true (see that constant's
	// doc comment).
	AchievableCA float64 // m, separation at the propagated rendezvous time
	ArrivalSpeed float64 // m/s, |v_rel| at that same instant
}

// FlightSec is the time from the burn to the rendezvous (TArrival - TBurn),
// the number a planted node carries as RendezvousArrivalSec.
func (o RendezvousBurnOption) FlightSec() float64 { return o.TArrival - o.TBurn }

// RendezvousLadder is the result of RecommendRendezvousLadder: every row the
// solver produced for one RendezvousOrbit, plus which of the two input
// states (A or B) is the mover — the row a plant-side caller must burn.
type RendezvousLadder struct {
	Place    RendezvousOrbit
	MoverIsA bool // true: stateA (typically the active craft) burns; false: stateB (the partner) does
	Rows     []RendezvousBurnOption

	// SolvedAt is the sim-clock instant the rows were solved at; every
	// row's TBurn / TArrival count from it. The planner has no clock, so the
	// sim layer stamps it. A plant reads SolvedAt + TBurn as the burn epoch,
	// so the row you read is the burn you get however far the clock moved
	// since (#418).
	SolvedAt time.Time
}

// rendezvousCandidateLaps is the fixed lap-count ladder every RendezvousOrbit
// scans. Short list per ADR 0045 §2's own example (2 / 5 / 20); a
// couple of extra rungs give S6's picker room to show a smoother
// wait-vs-cost curve.
var rendezvousCandidateLaps = []int{2, 3, 5, 10, 20}

// rendezvousPlaneTolDeg is the coplanar tolerance the Rendezvous Planner
// refuses beyond, naming [I] (PlanVesselPlaneMatch, ADR 0045 S4/#397)
// as the remedy. Mirrors hohmannInclTolDeg's precedent
// (internal/sim/hohmann_guard.go) — ~1° is already a large miss at
// any orbital distance, and the Rendezvous Planner's tangential solve
// (rendezvousLadderCore) assumes a coplanar geometry; this slice
// explicitly narrows scope to coplanar (#398 "out of scope: the plane
// rung").
const rendezvousPlaneTolDeg = 1.0

// rendezvousAchievableCATolFrac gates Ok on the row's own AchievableCA
// (review finding: "Ok never consults AchievableCA"). rendezvousLadderCore
// times the holder with Kepler's equation (exact for a same-orbit
// holder, circular or eccentric; #413) but still only aims at r0's
// direction, so a holder on a DIFFERENT orbit that crosses r0's radius
// elsewhere can pass every other gate (r0 in [holderPeri, holderApo], periapsis
// safety, affordability) while the burn it actually prices misses the
// holder by a large fraction of the orbit. AchievableCA is already the
// row's own propagate-and-check number (both craft advanced via
// physics.KeplerStep to TArrival); this constant is what turns that
// number into a gate instead of a display-only field.
//
// 0.1% of r0mag is generous next to the near-machine-precision residual
// a genuinely circular holder produces (TestRecommendRendezvousLadder_
// TheirOrbit_PhaseOffsetConverges asserts <5 km on a 6.87e6 m orbit,
// several orders of magnitude under this bound) but far tighter than
// the miss the pre-#413 uniform-sweep holder model produced on an
// eccentric orbit: measured on a perigee 6771 km / apogee 12000 km orbit
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
// treats as "this is a rendezvous" (RendezvousArrivalSec/AchievableCA flow
// straight through PlanRendezvousBurn to the planted node), a tens-of-km
// miss shouldn't read as one. 0.1% (≈6,871 m at that same radius)
// clears every one of those four measured values while still passing
// every row this package's own tests expect to succeed — none of them
// exercise an eccentric HOLDER on a geometry meant to stay Ok=true
// (the only eccentric-holder case, TestRendezvousLadderEccentricHolderSolves,
// now expects Ok rows whose re-propagated miss sits inside this bound).
const rendezvousAchievableCATolFrac = 0.001

var (
	// ErrRendezvousPlaneMismatch: the two orbital planes differ by more
	// than rendezvousPlaneTolDeg. The Rendezvous Planner assumes coplanar
	// geometry (ADR 0045 S5 scope) — [I] (PlanVesselPlaneMatch) is the
	// tool that rotates into the target's plane; named here so the
	// caller's refusal text can point at it directly, matching K's own
	// "your planes differ — match theirs [I] first" doctrine.
	ErrRendezvousPlaneMismatch = errors.New("rendezvous: orbital planes differ — match theirs [I] first")
	// ErrRendezvousNoCrossing: the two orbits share no single crossing
	// point: they never meet (nested or concentric) or coincide (every
	// point is shared). The caller's remedy is "their orbit" or "your
	// orbit", which don't depend on a crossing existing.
	ErrRendezvousNoCrossing = errors.New("rendezvous: these orbits have no single crossing point, try \"their orbit\" or \"your orbit\"")
	// ErrRendezvousSizeMismatch: the mover's current orbital radius never
	// falls within the holder's own [periapsis, apoapsis] band, so
	// there is no point on the holder's UNCHANGED orbit the tangential
	// "return to my own current position" model (see rendezvousLadderCore)
	// can aim at. A KNOWN, DOCUMENTED simplification of this slice
	// (see rendezvousLadderCore's doc comment) rather than the fully
	// general "the same burn also carries you to the other altitude"
	// solve ADR 0045 §2 describes — that combined reshape+phase case is
	// a natural follow-on, not required for #398's acceptance criteria
	// (all of which use matched or near-matched orbit sizes). [H]/[m]
	// remain the tools for a genuine size mismatch.
	ErrRendezvousSizeMismatch = errors.New("radius outside target's apsides: plan a transfer [H] first")
	// errRendezvousInvalidInput: non-positive mu or search horizon —
	// mirrors RecommendRendezvousNudge's "horizon too short" input
	// guard.
	errRendezvousInvalidInput = errors.New("rendezvous: invalid input (mu or horizon)")
)

// RecommendRendezvousLadder is the Rendezvous Planner's solver (ADR 0045 §2,
// #398): given the active craft's state (stateA) and the target's
// state (stateB) in a common primary-relative frame, produce the Lap
// Ladder for the chosen RendezvousOrbit.
//
// stateA/stateB must already be in the same frame — same convention as
// NextClosestApproach / RecommendRendezvousNudge; the sim layer
// resolves cross-primary conversion before calling in
// (TargetStateRelativeToActivePrimary).
//
// crossingSearchHorizon is only validated (> 0) for RendezvousCrossing; the
// crossing itself is a closed-form orbit intersection, not a search. It must
// still be the shared flat horizon (ADR 0045 S1 / #394), never a private
// constant. It does not bound the ladder's own arrival times, which are
// driven by lap count (ADR 0045 §2: "the 4h window bounds a search, not a
// plan").
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
// non-coplanar geometry, or a crossing Place whose orbits share no single
// crossing point (ErrRendezvousNoCrossing). Per-row gate failures
// (unaffordable, unsafe
// periapsis, no rendezvous solution for a given lap count) are reported IN
// the ladder's Rows, Ok=false — visible, not hidden (ADR 0045 §2).
func RecommendRendezvousLadder(
	stateA, stateB orbital.Vec3State,
	primary bodies.CelestialBody,
	mu float64,
	place RendezvousOrbit,
	crossingSearchHorizon, moverRemainingDV float64,
) (RendezvousLadder, error) {
	return RecommendRendezvousLadderAfter(stateA, stateB, primary, mu, place, crossingSearchHorizon, moverRemainingDV, 0)
}

// RecommendRendezvousLadderAfter is RecommendRendezvousLadder with a lead
// time (G4 Q1/Q2, #418): leadSec > 0 solves for a burn leadSec from now.
func RecommendRendezvousLadderAfter(
	stateA, stateB orbital.Vec3State,
	primary bodies.CelestialBody,
	mu float64,
	place RendezvousOrbit,
	crossingSearchHorizon, moverRemainingDV, leadSec float64,
) (RendezvousLadder, error) {
	if mu <= 0 {
		return RendezvousLadder{}, errRendezvousInvalidInput
	}
	if !rendezvousCoplanar(stateA, stateB) {
		return RendezvousLadder{}, ErrRendezvousPlaneMismatch
	}

	if leadSec < 0 {
		leadSec = 0
	}
	switch place {
	case RendezvousTheirOrbit:
		rows, err := rendezvousLadderAt(stateA, stateB, primary, mu, moverRemainingDV, leadSec)
		if err != nil {
			return RendezvousLadder{}, err
		}
		return RendezvousLadder{Place: place, MoverIsA: true, Rows: rows}, nil
	case RendezvousYourOrbit:
		rows, err := rendezvousLadderAt(stateB, stateA, primary, mu, moverRemainingDV, leadSec)
		if err != nil {
			return RendezvousLadder{}, err
		}
		return RendezvousLadder{Place: place, MoverIsA: false, Rows: rows}, nil
	case RendezvousCrossing:
		// G4 Q3 (#416): coast to the geometric intersection of the two
		// coplanar orbits (not NextClosestApproach), burn there, meet N
		// laps later. leadSec is the minimum coast.
		if crossingSearchHorizon <= 0 {
			return RendezvousLadder{}, errRendezvousInvalidInput
		}
		tBurn, err := rendezvousCrossingBurnTime(stateA, stateB, mu, leadSec)
		if err != nil {
			return RendezvousLadder{}, err
		}
		rows, err := rendezvousLadderAt(stateA, stateB, primary, mu, moverRemainingDV, tBurn)
		if err != nil {
			return RendezvousLadder{}, err
		}
		return RendezvousLadder{Place: place, MoverIsA: true, Rows: rows}, nil
	}
	return RendezvousLadder{}, errRendezvousInvalidInput
}

// rendezvousLadderAt solves the ladder for a burn tBurn seconds from now:
// both vessels coast (Kepler, no burn) to the burn epoch, the tangential
// solve runs there, and the rows are stamped with TBurn and re-based so
// TArrival counts from now (G4 Q2). tBurn == 0 is the old "burn now".
func rendezvousLadderAt(mover, holder orbital.Vec3State, primary bodies.CelestialBody, mu, moverRemainingDV, tBurn float64) ([]RendezvousBurnOption, error) {
	if tBurn > 0 {
		m, mok := physics.KeplerStep(physics.StateVector{R: mover.R, V: mover.V}, mu, tBurn)
		h, hok := physics.KeplerStep(physics.StateVector{R: holder.R, V: holder.V}, mu, tBurn)
		if !mok || !hok {
			return nil, errRendezvousInvalidInput
		}
		mover = orbital.Vec3State{R: m.R, V: m.V}
		holder = orbital.Vec3State{R: h.R, V: h.V}
	}
	rows, err := rendezvousLadderCore(mover, holder, primary, mu, moverRemainingDV)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].TBurn = tBurn
		if rows[i].TArrival > 0 {
			rows[i].TArrival += tBurn
		}
	}
	return rows, nil
}

// rendezvousCrossingBurnTime returns the seconds from now until the mover
// first reaches an intersection of the two coplanar orbits at or after
// minLead. In the orbital plane a conic is r = p / (1 + e.u) for the unit
// direction u, so equal radius at the same direction u means
// (pM*eH - pH*eM).u = pH - pM, solved in closed form for the (up to two)
// intersection directions. ErrRendezvousNoCrossing when the orbits never
// meet (concentric, nested) or coincide (every point is a crossing, so
// there is nothing to choose).
func rendezvousCrossingBurnTime(mover, holder orbital.Vec3State, mu, minLead float64) (float64, error) {
	hM := mover.R.Cross(mover.V)
	if hM.Norm() == 0 || holder.R.Cross(holder.V).Norm() == 0 {
		return 0, errRendezvousInvalidInput
	}
	hhat := hM.Unit()
	xhat := mover.R.Unit()
	yhat := hhat.Cross(xhat)

	conic := func(s orbital.Vec3State) (p, ex, ey float64) {
		h := s.R.Cross(s.V)
		p = h.Dot(h) / mu
		r := s.R.Norm()
		ev := s.R.Scale(s.V.Dot(s.V)/mu - 1/r).Sub(s.V.Scale(s.R.Dot(s.V) / mu))
		return p, ev.Dot(xhat), ev.Dot(yhat)
	}
	pM, eMx, eMy := conic(mover)
	pH, eHx, eHy := conic(holder)
	gx := pM*eHx - pH*eMx
	gy := pM*eHy - pH*eMy
	c := pH - pM
	gmag := math.Hypot(gx, gy)
	scale := math.Max(pM, pH)
	if gmag < 1e-9*scale || math.Abs(c) > gmag {
		return 0, ErrRendezvousNoCrossing
	}
	base := math.Atan2(gy, gx)
	off := math.Acos(c / gmag)

	elM := orbital.ElementsFromState(mover.R, mover.V, mu)
	pMover := orbitalPeriod(physics.StateVector{R: mover.R, V: mover.V}, mu)
	if elM.A <= 0 || elM.E >= 1 || pMover <= 0 || math.IsInf(pMover, 0) || math.IsNaN(pMover) {
		return 0, errRendezvousInvalidInput
	}
	best := math.Inf(1)
	for _, phi := range [...]float64{base + off, base - off} {
		dPhi := math.Mod(phi, 2*math.Pi)
		if dPhi < 0 {
			dPhi += 2 * math.Pi
		}
		t := holderTimeToPhase(mover, elM, mu, dPhi, pMover)
		for t < minLead {
			t += pMover
		}
		if t < best {
			best = t
		}
	}
	if math.IsInf(best, 1) {
		return 0, ErrRendezvousNoCrossing
	}
	return best, nil
}

// rendezvousCoplanar reports whether stateA/stateB's orbital planes agree
// within rendezvousPlaneTolDeg. Degenerate (zero angular-momentum) states
// are treated as coplanar — no false refusal on a state the caller
// couldn't have derived a plane from anyway; rendezvousLadderCore's own
// input guards (r0mag/v0mag/pHolder/nHmag checks) fail such a state on
// its own merits downstream.
func rendezvousCoplanar(stateA, stateB orbital.Vec3State) bool {
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
	return angleDeg <= rendezvousPlaneTolDeg
}

// rendezvousLadderCore is the shared solve behind every RendezvousOrbit: mover
// burns TANGENTIALLY (prograde/retrograde — along its own current
// velocity direction, magnitude only) at its CURRENT position, holder
// never changes. Each candidate lap count picks a different post-burn
// period, hence a different Δv.
//
// The model: a tangential burn at position r0 leaves r0 ON the new
// orbit (any Keplerian orbit revisits every one of its points every
// period, apsis or not), so the mover returns to r0 EXACTLY at
// t = N·P'(Δv) for any lap count N. The holder, meanwhile, is treated
// as reaching r0's angular position after a Kepler time-of-flight t0
// (true to eccentric to mean anomaly, so an eccentric holder is timed
// exactly; #413) and every P_holder after that, i.e.
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
// — see ErrRendezvousSizeMismatch. r0 must fall within the holder's own
// [periapsis, apoapsis] band (trivially true for same-radius circular
// orbits, and true near a genuine "crossing") for this model to have
// anywhere to aim.
func rendezvousLadderCore(moverState, holderState orbital.Vec3State, primary bodies.CelestialBody, mu float64, moverRemainingDV float64) ([]RendezvousBurnOption, error) {
	r0 := moverState.R
	v0 := moverState.V
	r0mag := r0.Norm()
	v0mag := v0.Norm()
	if r0mag <= 0 || v0mag <= 0 {
		return nil, errRendezvousInvalidInput
	}
	v0hat := v0.Scale(1 / v0mag)

	pMoverOrig := orbitalPeriod(physics.StateVector{R: r0, V: v0}, mu)
	if math.IsInf(pMoverOrig, 0) || math.IsNaN(pMoverOrig) || pMoverOrig <= 0 {
		return nil, errRendezvousInvalidInput
	}

	pHolder := orbitalPeriod(physics.StateVector{R: holderState.R, V: holderState.V}, mu)
	if math.IsInf(pHolder, 0) || math.IsNaN(pHolder) || pHolder <= 0 {
		return nil, errRendezvousInvalidInput
	}

	hEl := orbital.ElementsFromState(holderState.R, holderState.V, mu)
	if hEl.A <= 0 || hEl.E >= 1 {
		return nil, errRendezvousInvalidInput
	}
	holderPeri := hEl.Periapsis()
	holderApo := hEl.A * (1 + hEl.E)
	// Tolerance: floating point can put an EXACTLY-matched-radius r0
	// a few ULPs outside [holderPeri, holderApo] (the calibration
	// scenario's whole point — same-radius circular orbits).
	const reachTol = 1.0 // 1 m
	if r0mag < holderPeri-reachTol || r0mag > holderApo+reachTol {
		return nil, ErrRendezvousSizeMismatch
	}

	// t0: time (from now) until the holder's angular position first
	// coincides with r0's, sweeping in the holder's own rotation sense.
	nH := holderState.R.Cross(holderState.V)
	nHmag := nH.Norm()
	if nHmag == 0 {
		return nil, errRendezvousInvalidInput
	}
	nHhat := nH.Scale(1 / nHmag)
	rHmag := holderState.R.Norm()
	if rHmag <= 0 {
		return nil, errRendezvousInvalidInput
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
	// Holder timing is a real Kepler time-of-flight (#413): the holder's
	// angle to r0 is a true-anomaly difference, so convert true anomaly
	// to eccentric to mean anomaly and divide by mean motion. A uniform
	// angular sweep (dPhi0/2π · P) is exact only for a circle; on an
	// eccentric holder it mis-times arrival by a large fraction of the
	// orbit. Below the circular threshold the true anomaly is undefined
	// and the sweep IS exact, so it stays as the fallback.
	t0 := holderTimeToPhase(holderState, hEl, mu, dPhi0, pHolder)

	rows := make([]RendezvousBurnOption, 0, len(rendezvousCandidateLaps))
	for _, n := range rendezvousCandidateLaps {
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
		var best RendezvousBurnOption
		var bestSafeFlag, bestAchievableFlag bool
		bestFound := false
		var bestGood RendezvousBurnOption
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

			cand := RendezvousBurnOption{
				Laps:         n,
				DV:           math.Abs(dv),
				BurnDir:      v0hat.Scale(math.Copysign(1, dv)),
				TArrival:     Ttarget,
				AchievableCA: achievedCA,
				ArrivalSpeed: vRelAtT.Norm(),
			}
			safe := orbitSafetyGate(r0, v0, r0, newV, primary, mu)
			achievable := cand.AchievableCA <= rendezvousAchievableCATolFrac*r0mag

			if !bestFound || cand.DV < best.DV {
				best, bestFound = cand, true
				bestSafeFlag, bestAchievableFlag = safe, achievable
			}
			if safe && achievable && (!bestGoodFound || cand.DV < bestGood.DV) {
				bestGood, bestGoodFound = cand, true
			}
		}
		if !bestFound {
			rows = append(rows, RendezvousBurnOption{Laps: n, Ok: false, Reason: "no rendezvous solution"})
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
			// outside rendezvousAchievableCATolFrac of r0mag — the
			// holder never passes through r0 at the aimed time (e.g. a
			// different orbit), so the
			// solved burn doesn't actually deliver a rendezvous. Same
			// reason text as the "no bracket converged at all" case
			// above: both mean "this lap count has no usable rendezvous
			// solution", just discovered at a different stage.
			chosen.Reason = "no rendezvous solution"
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

// rendezvousPhaseEps is the angular tolerance (rad) inside which the
// holder counts as already at r0. Float noise in the atan2 angle can land
// a co-located holder a hair either side of zero; the "hair behind" side
// normalises to ~2π, which would read as one full period of waiting.
const rendezvousPhaseEps = 1e-9

// holderTimeToPhase is the time for the holder to sweep dPhi0 (radians,
// in [0, 2π), in its own rotation sense) to reach r0. A phase within
// rendezvousPhaseEps of 0 or of 2π means "already there": zero, never a
// period. Eccentric holders use Kepler time-of-flight (#413); below the
// circular threshold the uniform sweep is exact.
func holderTimeToPhase(holderState orbital.Vec3State, hEl orbital.Elements, mu, dPhi0, pHolder float64) float64 {
	if dPhi0 < rendezvousPhaseEps || dPhi0 > 2*math.Pi-rendezvousPhaseEps {
		return 0
	}
	t0 := dPhi0 / (2 * math.Pi) * pHolder
	if hEl.E >= 1e-9 {
		nuNow := orbital.TrueAnomalyFromState(holderState.R, holderState.V, mu, hEl)
		nuAim := math.Mod(nuNow+dPhi0, 2*math.Pi)
		if dt := orbital.TimeToTrueAnomaly(nuNow, nuAim, hEl.A, hEl.E, mu); dt > 0 {
			t0 = dt
		}
	}
	return t0
}
