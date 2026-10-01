package sim

import (
	"errors"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Rendezvous Planner sim-layer entry points (ADR 0045 §2, S5/#398).
// RecommendRendezvousLadder is the read-only preview S6's picker chip
// will drive; PlanRendezvousBurn commits one of its rows. Both mirror the
// RecommendedRendezvousBurn / PlanRendezvousNudge shape (rendezvous.go)
// — gather primary-relative states, hand off to the planner package,
// map its Reason/error vocabulary onto sim-layer sentinels the HUD can
// switch on.

// Rendezvous Planner refusal sentinels. Distinct from the K/Nudge family
// (ErrRendezvous*) even where the underlying gate is the literal same
// function (ErrRendezvousUnsafePeriapsis is reused directly for the
// periapsis gate — same physical failure, same remedy) — a caller
// wiring up S6's picker needs to tell "no target" apart from "this
// Rendezvous Orbit has no solution" apart from "this specific lap row is
// unaffordable".
var (
	ErrRendezvousPlaneMismatch = transferError("your planes differ — match theirs [I] first")
	ErrRendezvousNoCrossing    = transferError("these orbits have no single crossing point, try \"their orbit\" or \"your orbit\"")
	ErrRendezvousUnaffordable  = transferError("rendezvous burn exceeds remaining Δv budget")
	ErrRendezvousNoSolution    = transferError("no rendezvous solution on this lap count")
	ErrRendezvousNoSuchLap     = transferError("no such lap count on the ladder")
	ErrRendezvousRowExpired    = transferError("that burn time has passed, reopen the plan [K]")
)

// rendezvousPlanLeadSec is the default lead time a Rendezvous Planner row
// aims at (G4 Q1, #418): rows burn this far ahead so the row stays valid
// while the pilot reads it, and Enter plants the row they read.
const rendezvousPlanLeadSec = 300.0

// rendezvousStructuralErr maps a structural (whole-ladder) error from
// planner.RecommendRendezvousLadder onto a sim-layer sentinel. Anything
// unrecognised (a planner-internal invalid-input guard, not meant to
// surface to a player) falls back to ErrRendezvousNoImprovement —
// the same "nothing useful available" bucket K's own unmapped reasons
// use (rendezvousReasonToErr's default case).
func rendezvousStructuralErr(err error) error {
	switch {
	case errors.Is(err, planner.ErrRendezvousPlaneMismatch):
		return ErrRendezvousPlaneMismatch
	case errors.Is(err, planner.ErrRendezvousNoCrossing):
		return ErrRendezvousNoCrossing
	case errors.Is(err, planner.ErrRendezvousShapeMismatch):
		return ErrRendezvousShapeMismatch
	default:
		return ErrRendezvousNoImprovement
	}
}

// rendezvousRowReasonToErr maps a RendezvousBurnOption's Reason (populated
// when Ok=false) onto a sim-layer sentinel, mirroring
// rendezvousReasonToErr's per-reason mapping for K's advisory.
func rendezvousRowReasonToErr(reason string) error {
	switch reason {
	case "unaffordable":
		return ErrRendezvousUnaffordable
	case "burn drops periapsis unsafely":
		// Reused verbatim: same gate, same physical failure, same
		// remedy text as K's own unsafe-periapsis refusal.
		return ErrRendezvousUnsafePeriapsis
	default: // "no rendezvous solution"
		return ErrRendezvousNoSolution
	}
}

// RecommendRendezvousLadder gathers the active craft + target state and
// hands off to planner.RecommendRendezvousLadder for the given
// RendezvousOrbit. Read-only — S6's picker chip calls this to render
// rows; nothing is planted.
//
// Same gates as RecommendedRendezvousBurn / PlanRendezvousNudge: an
// active craft, a bound relative target (craft or ghost), same
// primary. Rows aim rendezvousPlanLeadSec ahead (G4 Q1) and the ladder is
// stamped with the sim-clock instant it was solved at (SolvedAt). The
// horizon passed to the planner is rendezvousCommitHorizonSec, only
// validated there (see planner.RecommendRendezvousLadderAfter).
func (w *World) RecommendRendezvousLadder(place planner.RendezvousOrbit) (planner.RendezvousLadder, error) {
	active := w.ActiveCraft()
	if active == nil {
		return planner.RendezvousLadder{}, ErrRendezvousNoCraft
	}
	if !w.HasRelativeTarget() {
		return planner.RendezvousLadder{}, ErrRendezvousNoTarget
	}
	targetPrimary, ok := w.rendezvousTargetPrimary()
	if !ok {
		return planner.RendezvousLadder{}, ErrRendezvousNoTarget
	}
	if targetPrimary.EnglishName != active.Primary.EnglishName {
		return planner.RendezvousLadder{}, ErrRendezvousDifferentPrimaries
	}
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return planner.RendezvousLadder{}, ErrRendezvousNoTarget
	}
	mu := active.Primary.GravitationalParameter()
	if mu <= 0 {
		return planner.RendezvousLadder{}, ErrRendezvousNoTarget
	}

	stateA := orbital.Vec3State{R: active.State.R, V: active.State.V}
	stateB := orbital.Vec3State{R: rT, V: vT}
	moverRemainingDV := w.rendezvousMoverRemainingDV(place, active)

	ladder, err := planner.RecommendRendezvousLadderAfter(stateA, stateB, active.Primary, mu, place, rendezvousCommitHorizonSec, moverRemainingDV, rendezvousPlanLeadSec)
	if err != nil {
		return planner.RendezvousLadder{}, rendezvousStructuralErr(err)
	}
	ladder.SolvedAt = w.Clock.SimTime
	return ladder, nil
}

// rendezvousMoverRemainingDV resolves whose Δv budget gates a ladder's
// affordability column: the active craft burns for RendezvousTheirOrbit
// (as does RendezvousCrossing); the TARGET burns for RendezvousYourOrbit. A local
// craft target's budget is directly readable; a remote ghost's is not
// (this player's session has no visibility into another player's
// Spacecraft.Stages) — planner.RecommendRendezvousLadder's own
// convention treats <= 0 as "unknown, report every row affordable"
// (mirrors PreviewBurnState's "fuelDv > 0 && ..." pattern,
// internal/sim/maneuver.go), same as it would for any other unknown
// budget.
func (w *World) rendezvousMoverRemainingDV(place planner.RendezvousOrbit, active *spacecraft.Spacecraft) float64 {
	if place != planner.RendezvousYourOrbit {
		return active.RemainingDeltaV()
	}
	if w.Target.Kind == TargetCraft {
		if t, _, ok := w.craftByID(w.Target.CraftID); ok {
			return t.RemainingDeltaV()
		}
	}
	return -1
}

// RendezvousBurnPlan is PlanRendezvousBurn's result: the chosen Lap Ladder row
// plus which craft it applies to. ForActive=true means the node was
// actually planted on the active craft (RendezvousTheirOrbit and RendezvousCrossing); ForActive=false means the row describes a burn
// for the PARTNER (RendezvousYourOrbit) — nothing is planted here, since
// this session has no authority to queue a node on another player's
// (or another local craft's) Nodes slate. S6/S7 own how that gets
// communicated/delivered; this slice only computes it.
type RendezvousBurnPlan struct {
	planner.RendezvousBurnOption
	ForActive bool
}

// PlanRendezvousBurn commits the Lap Ladder row with the given lap count
// for the given RendezvousOrbit, from a ladder solved right now. It is the
// single-keystroke shape (gather state, ask the planner, plant a node);
// the picker instead plants the ladder the pilot actually read via
// PlanRendezvousFromLadder.
func (w *World) PlanRendezvousBurn(place planner.RendezvousOrbit, laps int) (*RendezvousBurnPlan, error) {
	ladder, err := w.RecommendRendezvousLadder(place)
	if err != nil {
		return nil, err
	}
	return w.PlanRendezvousFromLadder(place, ladder, laps)
}

// PlanRendezvousFromLadder plants exactly the row the pilot read (G4 Q1/Q2,
// #418/#416). The row carries its own burn epoch (ladder.SolvedAt + TBurn),
// burn size and flight time, all solved from the vessels' states AT that
// epoch (Kepler coast, no burn), so nothing is re-solved here: the clock
// may have run since the ladder was drawn and the plant is unchanged. The
// node's RendezvousArrivalSec is the row's FlightSec (seconds from the
// BURN), the single meaning Engage's commit path reads. A row whose burn
// epoch is closer than the slew lead has expired and refuses
// (ErrRendezvousRowExpired).
//
// The direction is re-derived at fire time from the mover's own velocity
// (BurnPrograde/BurnRetrograde), the sign taken from the row's tangential
// BurnDir against the mover's velocity at the burn epoch.
//
// A second call replaces the craft's own previously-planted, still
// unfired Rendezvous Burn (AdvisoryKeyRendezvousBurn) rather than stacking
// a stale duplicate behind it (#293).
func (w *World) PlanRendezvousFromLadder(place planner.RendezvousOrbit, ladder planner.RendezvousLadder, laps int) (*RendezvousBurnPlan, error) {
	active := w.ActiveCraft()
	if active == nil {
		return nil, ErrRendezvousNoCraft
	}
	var row planner.RendezvousBurnOption
	found := false
	for _, r := range ladder.Rows {
		if r.Laps == laps {
			row, found = r, true
			break
		}
	}
	if !found {
		return nil, ErrRendezvousNoSuchLap
	}
	if !row.Ok {
		return nil, rendezvousRowReasonToErr(row.Reason)
	}

	if !ladder.MoverIsA {
		// "your orbit": the PARTNER is the mover. Nothing to plant on
		// this session's own craft — return the computed plan so a
		// caller can surface/relay it (S6/S7 own that delivery).
		return &RendezvousBurnPlan{RendezvousBurnOption: row, ForActive: false}, nil
	}

	solvedAt := ladder.SolvedAt
	if solvedAt.IsZero() {
		solvedAt = w.Clock.SimTime
	}
	triggerTime := solvedAt.Add(time.Duration(row.TBurn * float64(time.Second)))
	untilBurn := triggerTime.Sub(w.Clock.SimTime)
	if untilBurn < w.rendezvousLeadBuffer(active, row.BurnDir) {
		return nil, ErrRendezvousRowExpired
	}

	mu := active.Primary.GravitationalParameter()
	moverAtTrigger, mok := physics.KeplerStep(active.State, mu, untilBurn.Seconds())
	if !mok {
		return nil, ErrRendezvousNoImprovement
	}
	mode := spacecraft.BurnPrograde
	if row.BurnDir.Dot(moverAtTrigger.V.Unit()) < 0 {
		mode = spacecraft.BurnRetrograde
	}

	w.replaceAdvisoryNode(active, AdvisoryKeyRendezvousBurn)

	node := ManeuverNode{
		Mode:     mode,
		DV:       row.DV,
		Duration: active.BurnTimeForDV(row.DV),
		Event:    spacecraft.TriggerAbsolute,

		TriggerTime:      triggerTime,
		PrimaryID:        active.Primary.ID,
		Throttle:         1.0,
		TargetCraftID:    w.Target.CraftID,
		TargetGhostOwner: w.Target.GhostOwner,
		AdvisoryKey:      AdvisoryKeyRendezvousBurn,
		// ADR 0045 S7 (#400): the row's flight time FROM THE BURN, so a
		// later Engage commits to TriggerTime + this (see
		// rendezvousCommitFromPlantedBurnNode). Row.TArrival counts from
		// solve time; FlightSec is the one number that counts from the
		// burn (#416 Contradiction 5).
		RendezvousArrivalSec: row.FlightSec(),
		RendezvousOrbitLabel: place.String(),
		RendezvousLaps:       laps,
	}
	w.PlanNode(node)
	return &RendezvousBurnPlan{RendezvousBurnOption: row, ForActive: true}, nil
}
