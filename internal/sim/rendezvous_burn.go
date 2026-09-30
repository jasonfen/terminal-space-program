package sim

import (
	"errors"

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
	ErrRendezvousNoCrossing    = transferError("no natural encounter to meet at — try \"their orbit\" or \"your orbit\"")
	// ErrRendezvousCrossingNotImplemented: "the crossing" has no working
	// solver (review round 2 revert — see planner.
	// ErrRendezvousCrossingNotImplemented's doc comment for why PR #412's
	// attempt was pulled rather than fixed forward). RecommendRendezvousLadder
	// and PlanRendezvousBurn both refuse before computing or planting
	// anything for this Place.
	ErrRendezvousCrossingNotImplemented = transferError("\"the crossing\" isn't implemented yet — try \"their orbit\" or \"your orbit\"")
	ErrRendezvousSizeMismatch           = transferError("radius outside target's apsides: plan a transfer [H] first")
	ErrRendezvousUnaffordable           = transferError("rendezvous burn exceeds remaining Δv budget")
	ErrRendezvousNoSolution             = transferError("no rendezvous solution on this lap count")
	ErrRendezvousNoSuchLap              = transferError("no such lap count on the ladder")
)

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
	case errors.Is(err, planner.ErrRendezvousCrossingNotImplemented):
		return ErrRendezvousCrossingNotImplemented
	case errors.Is(err, planner.ErrRendezvousSizeMismatch):
		return ErrRendezvousSizeMismatch
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
// primary. The search horizon passed to the planner is
// rendezvousCommitHorizonSec — ADR 0045 S1's single flat 4h window,
// used here ONLY for RendezvousCrossing's existence check (does a natural
// crossing exist at all — see planner.ErrRendezvousCrossingNotImplemented,
// this Place always refuses regardless of the answer), never as a
// private constant (see planner.RecommendRendezvousLadder's own doc
// comment).
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

	ladder, err := planner.RecommendRendezvousLadder(stateA, stateB, active.Primary, mu, place, rendezvousCommitHorizonSec, moverRemainingDV)
	if err != nil {
		return planner.RendezvousLadder{}, rendezvousStructuralErr(err)
	}
	return ladder, nil
}

// rendezvousMoverRemainingDV resolves whose Δv budget gates a ladder's
// affordability column: the active craft burns for RendezvousTheirOrbit
// (RendezvousCrossing never reaches an affordability check — it always
// refuses structurally, see planner.ErrRendezvousCrossingNotImplemented —
// but takes the same branch here since it isn't RendezvousYourOrbit); the
// TARGET burns for RendezvousYourOrbit. A local
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
// actually planted on the active craft (RendezvousTheirOrbit — the only
// Place that currently reaches a plant; RendezvousCrossing always refuses
// before getting here); ForActive=false means the row describes a burn
// for the PARTNER (RendezvousYourOrbit) — nothing is planted here, since
// this session has no authority to queue a node on another player's
// (or another local craft's) Nodes slate. S6/S7 own how that gets
// communicated/delivered; this slice only computes it.
type RendezvousBurnPlan struct {
	planner.RendezvousBurnOption
	ForActive bool
}

// PlanRendezvousBurn commits the Lap Ladder row with the given lap count
// for the given RendezvousOrbit. Mirrors PlanRendezvousNudge /
// PlanVesselPlaneMatch's single-keystroke-planter shape: gather state,
// ask the planner, plant a maneuver node.
//
// The row returned by the preview ladder (w.RecommendRendezvousLadder) is
// solved as a tangential burn at the craft's position NOW — but the
// node it plants doesn't fire until leadBuffer later (the same slew-
// lead pattern PlanRendezvousNudge uses). Review finding 2: planting
// that row as a BurnVector node (a FROZEN inertial direction, see
// spacecraft.NodeBurnDirection) fires the frozen "tangential at now"
// direction at a position where it is no longer tangential, and
// RendezvousArrivalSec re-anchored by subtracting leadBuffer from
// TArrival doesn't land on the resulting closest approach either
// (measured: rendezvousTwoCraftWorld at 30° phase lag reported
// AchievableCA=0 while the planted node actually produced 974.9 m at
// the committed τ, true minimum 46.3 m ten seconds later).
//
// Fix: re-solve the ladder from the state BOTH craft will actually be
// at TriggerTime (Kepler-propagated forward by leadBuffer, no burn
// applied — the craft is coasting, not thrusting, until the node
// fires), then plant BurnPrograde/BurnRetrograde — re-derived at fire
// time from the craft's ACTUAL (r, v) via NodeBurnDirection/
// DirectionUnit, exactly like every other tangential-style node — so
// the direction is correct at the instant it actually applies.
// RendezvousArrivalSec is the fresh row's own TArrival directly: since
// that row was solved AT TriggerTime, TArrival is already "seconds
// from TriggerTime to the rendezvous" — no re-anchoring subtraction
// needed (see rendezvousCommitFromPlantedBurnNode, which reads
// RendezvousArrivalSec as exactly that).
//
// A second call replaces the craft's own previously-planted, still
// unfired Rendezvous Burn (AdvisoryKeyRendezvousBurn) rather than stacking a
// stale duplicate behind it — same "replace, don't stack" rule as K's
// nudge and C's circularize (#293).
func (w *World) PlanRendezvousBurn(place planner.RendezvousOrbit, laps int) (*RendezvousBurnPlan, error) {
	active := w.ActiveCraft()
	if active == nil {
		return nil, ErrRendezvousNoCraft
	}

	ladder, err := w.RecommendRendezvousLadder(place)
	if err != nil {
		return nil, err
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

	// leadBuffer only needs an approximate burn axis to size the slew
	// angle — the preview row's "tangential at now" direction is close
	// enough for that estimate even though it isn't used for the plant
	// itself below.
	leadBuffer := w.rendezvousLeadBuffer(active, row.BurnDir)
	triggerTime := w.Clock.SimTime.Add(leadBuffer)

	mu := active.Primary.GravitationalParameter()
	moverAtTrigger, mok := physics.KeplerStep(active.State, mu, leadBuffer.Seconds())
	if !mok {
		return nil, ErrRendezvousNoImprovement
	}
	rT, vT, tok := w.TargetStateRelativeToActivePrimary()
	if !tok {
		return nil, ErrRendezvousNoTarget
	}
	holderAtTrigger, hok := physics.KeplerStep(physics.StateVector{R: rT, V: vT}, mu, leadBuffer.Seconds())
	if !hok {
		return nil, ErrRendezvousNoImprovement
	}

	moverRemainingDV := w.rendezvousMoverRemainingDV(place, active)
	freshLadder, err := planner.RecommendRendezvousLadder(
		orbital.Vec3State{R: moverAtTrigger.R, V: moverAtTrigger.V},
		orbital.Vec3State{R: holderAtTrigger.R, V: holderAtTrigger.V},
		active.Primary, mu, place, rendezvousCommitHorizonSec, moverRemainingDV)
	if err != nil {
		return nil, rendezvousStructuralErr(err)
	}
	var freshRow planner.RendezvousBurnOption
	freshFound := false
	for _, r := range freshLadder.Rows {
		if r.Laps == laps {
			freshRow, freshFound = r, true
			break
		}
	}
	if !freshFound {
		return nil, ErrRendezvousNoSuchLap
	}
	if !freshRow.Ok {
		return nil, rendezvousRowReasonToErr(freshRow.Reason)
	}

	// The fresh row's BurnDir is ± the mover's OWN velocity unit at
	// TriggerTime (rendezvousLadderCore's tangential-burn construction) —
	// compare against it to pick prograde vs. retrograde rather than
	// carrying a frozen direction vector.
	mode := spacecraft.BurnPrograde
	if freshRow.BurnDir.Dot(moverAtTrigger.V.Unit()) < 0 {
		mode = spacecraft.BurnRetrograde
	}

	// #293 precedent: a second press replaces its own previous unfired
	// Rendezvous Burn instead of stacking behind it — every ladder row is
	// computed from the craft's CURRENT orbit, so a stale queued node
	// would fire against an orbit it was never computed for.
	w.replaceAdvisoryNode(active, AdvisoryKeyRendezvousBurn)

	node := ManeuverNode{
		Mode:     mode,
		DV:       freshRow.DV,
		Duration: active.BurnTimeForDV(freshRow.DV),
		Event:    spacecraft.TriggerAbsolute,

		TriggerTime:      triggerTime,
		PrimaryID:        active.Primary.ID,
		Throttle:         1.0,
		TargetCraftID:    w.Target.CraftID,
		TargetGhostOwner: w.Target.GhostOwner,
		AdvisoryKey:      AdvisoryKeyRendezvousBurn,
		// ADR 0045 S7 (#400): carry the plan's own arrival onto the node
		// so a later Engage can commit to it directly (see
		// rendezvousCommitFromPlantedBurnNode). freshRow was solved
		// FROM the state at TriggerTime, so its own TArrival already
		// means "seconds from TriggerTime to the rendezvous" — stored
		// as-is, no lead-buffer re-anchoring needed.
		RendezvousArrivalSec: freshRow.TArrival,
		RendezvousOrbitLabel: place.String(),
		RendezvousLaps:       laps,
	}
	w.PlanNode(node)
	return &RendezvousBurnPlan{RendezvousBurnOption: freshRow, ForActive: true}, nil
}
