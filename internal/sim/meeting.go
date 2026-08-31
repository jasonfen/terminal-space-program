package sim

import (
	"errors"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Meeting Planner sim-layer entry points (ADR 0045 §2, S5/#398).
// RecommendMeetingLadder is the read-only preview S6's picker chip
// will drive; PlanMeetingBurn commits one of its rows. Both mirror the
// RecommendedRendezvousBurn / PlanRendezvousNudge shape (rendezvous.go)
// — gather primary-relative states, hand off to the planner package,
// map its Reason/error vocabulary onto sim-layer sentinels the HUD can
// switch on.

// Meeting Planner refusal sentinels. Distinct from the K/Nudge family
// (ErrRendezvous*) even where the underlying gate is the literal same
// function (ErrRendezvousUnsafePeriapsis is reused directly for the
// periapsis gate — same physical failure, same remedy) — a caller
// wiring up S6's picker needs to tell "no target" apart from "this
// Meeting Place has no solution" apart from "this specific lap row is
// unaffordable".
var (
	ErrMeetingPlaneMismatch = transferError("your planes differ — match theirs [I] first")
	ErrMeetingNoCrossing    = transferError("no natural encounter to meet at — try \"their orbit\" or \"your orbit\"")
	ErrMeetingSizeMismatch  = transferError("orbits differ too much in size for this Meeting Place — plan a transfer [H] first")
	ErrMeetingUnaffordable  = transferError("meeting burn exceeds remaining Δv budget")
	ErrMeetingNoSolution    = transferError("no meeting solution on this lap count")
	ErrMeetingNoSuchLap     = transferError("no such lap count on the ladder")
)

// meetingStructuralErr maps a structural (whole-ladder) error from
// planner.RecommendMeetingLadder onto a sim-layer sentinel. Anything
// unrecognised (a planner-internal invalid-input guard, not meant to
// surface to a player) falls back to ErrRendezvousNoImprovement —
// the same "nothing useful available" bucket K's own unmapped reasons
// use (rendezvousReasonToErr's default case).
func meetingStructuralErr(err error) error {
	switch {
	case errors.Is(err, planner.ErrMeetingPlaneMismatch):
		return ErrMeetingPlaneMismatch
	case errors.Is(err, planner.ErrMeetingNoCrossing):
		return ErrMeetingNoCrossing
	case errors.Is(err, planner.ErrMeetingSizeMismatch):
		return ErrMeetingSizeMismatch
	default:
		return ErrRendezvousNoImprovement
	}
}

// meetingRowReasonToErr maps a MeetingBurnOption's Reason (populated
// when Ok=false) onto a sim-layer sentinel, mirroring
// rendezvousReasonToErr's per-reason mapping for K's advisory.
func meetingRowReasonToErr(reason string) error {
	switch reason {
	case "unaffordable":
		return ErrMeetingUnaffordable
	case "burn drops periapsis unsafely":
		// Reused verbatim: same gate, same physical failure, same
		// remedy text as K's own unsafe-periapsis refusal.
		return ErrRendezvousUnsafePeriapsis
	default: // "no meeting solution"
		return ErrMeetingNoSolution
	}
}

// RecommendMeetingLadder gathers the active craft + target state and
// hands off to planner.RecommendMeetingLadder for the given
// MeetingPlace. Read-only — S6's picker chip calls this to render
// rows; nothing is planted.
//
// Same gates as RecommendedRendezvousBurn / PlanRendezvousNudge: an
// active craft, a bound relative target (craft or ghost), same
// primary. The search horizon passed to the planner is
// rendezvousCommitHorizonSec — ADR 0045 S1's single flat 4h window,
// used here ONLY for MeetingCrossing's natural-encounter anchor
// search, never as a private constant (see
// planner.RecommendMeetingLadder's own doc comment).
func (w *World) RecommendMeetingLadder(place planner.MeetingPlace) (planner.MeetingLadder, error) {
	active := w.ActiveCraft()
	if active == nil {
		return planner.MeetingLadder{}, ErrRendezvousNoCraft
	}
	if !w.HasRelativeTarget() {
		return planner.MeetingLadder{}, ErrRendezvousNoTarget
	}
	targetPrimary, ok := w.rendezvousTargetPrimary()
	if !ok {
		return planner.MeetingLadder{}, ErrRendezvousNoTarget
	}
	if targetPrimary.EnglishName != active.Primary.EnglishName {
		return planner.MeetingLadder{}, ErrRendezvousDifferentPrimaries
	}
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return planner.MeetingLadder{}, ErrRendezvousNoTarget
	}
	mu := active.Primary.GravitationalParameter()
	if mu <= 0 {
		return planner.MeetingLadder{}, ErrRendezvousNoTarget
	}

	stateA := orbital.Vec3State{R: active.State.R, V: active.State.V}
	stateB := orbital.Vec3State{R: rT, V: vT}
	moverRemainingDV := w.meetingMoverRemainingDV(place, active)

	ladder, err := planner.RecommendMeetingLadder(stateA, stateB, active.Primary, mu, place, rendezvousCommitHorizonSec, moverRemainingDV)
	if err != nil {
		return planner.MeetingLadder{}, meetingStructuralErr(err)
	}
	return ladder, nil
}

// meetingMoverRemainingDV resolves whose Δv budget gates a ladder's
// affordability column: the active craft burns for MeetingTheirOrbit
// and MeetingCrossing; the TARGET burns for MeetingYourOrbit. A local
// craft target's budget is directly readable; a remote ghost's is not
// (this player's session has no visibility into another player's
// Spacecraft.Stages) — planner.RecommendMeetingLadder's own
// convention treats <= 0 as "unknown, report every row affordable"
// (mirrors PreviewBurnState's "fuelDv > 0 && ..." pattern,
// internal/sim/maneuver.go), same as it would for any other unknown
// budget.
func (w *World) meetingMoverRemainingDV(place planner.MeetingPlace, active *spacecraft.Spacecraft) float64 {
	if place != planner.MeetingYourOrbit {
		return active.RemainingDeltaV()
	}
	if w.Target.Kind == TargetCraft {
		if t, _, ok := w.craftByID(w.Target.CraftID); ok {
			return t.RemainingDeltaV()
		}
	}
	return -1
}

// MeetingPlan is PlanMeetingBurn's result: the chosen Lap Ladder row
// plus which craft it applies to. ForActive=true means the node was
// actually planted on the active craft (MeetingTheirOrbit /
// MeetingCrossing); ForActive=false means the row describes a burn
// for the PARTNER (MeetingYourOrbit) — nothing is planted here, since
// this session has no authority to queue a node on another player's
// (or another local craft's) Nodes slate. S6/S7 own how that gets
// communicated/delivered; this slice only computes it.
type MeetingPlan struct {
	planner.MeetingBurnOption
	ForActive bool
}

// PlanMeetingBurn commits the Lap Ladder row with the given lap count
// for the given MeetingPlace. Mirrors PlanRendezvousNudge /
// PlanVesselPlaneMatch's single-keystroke-planter shape: gather state,
// ask the planner, plant a BurnVector node (the same arbitrary-3D-
// direction mechanism the fused-Lambert departure planter uses,
// spacecraft.BurnVector — a Meeting Burn is a raw tangential Δv, not
// one of the eight discrete prograde/retrograde/normal/radial axes).
//
// A second call replaces the craft's own previously-planted, still
// unfired Meeting Burn (AdvisoryKeyMeetingBurn) rather than stacking a
// stale duplicate behind it — same "replace, don't stack" rule as K's
// nudge and C's circularize (#293).
func (w *World) PlanMeetingBurn(place planner.MeetingPlace, laps int) (*MeetingPlan, error) {
	active := w.ActiveCraft()
	if active == nil {
		return nil, ErrRendezvousNoCraft
	}

	ladder, err := w.RecommendMeetingLadder(place)
	if err != nil {
		return nil, err
	}

	var row planner.MeetingBurnOption
	found := false
	for _, r := range ladder.Rows {
		if r.Laps == laps {
			row, found = r, true
			break
		}
	}
	if !found {
		return nil, ErrMeetingNoSuchLap
	}
	if !row.Ok {
		return nil, meetingRowReasonToErr(row.Reason)
	}

	if !ladder.MoverIsA {
		// "your orbit": the PARTNER is the mover. Nothing to plant on
		// this session's own craft — return the computed plan so a
		// caller can surface/relay it (S6/S7 own that delivery).
		return &MeetingPlan{MeetingBurnOption: row, ForActive: false}, nil
	}

	leadBuffer := w.rendezvousLeadBuffer(active, row.BurnDir)

	// #293 precedent: a second press replaces its own previous unfired
	// Meeting Burn instead of stacking behind it — every ladder row is
	// computed from the craft's CURRENT orbit, so a stale queued node
	// would fire against an orbit it was never computed for.
	w.replaceAdvisoryNode(active, AdvisoryKeyMeetingBurn)

	node := ManeuverNode{
		Mode:             spacecraft.BurnVector,
		DV:               row.DV,
		Duration:         active.BurnTimeForDV(row.DV),
		Event:            spacecraft.TriggerAbsolute,
		TriggerTime:      w.Clock.SimTime.Add(leadBuffer),
		PrimaryID:        active.Primary.ID,
		Throttle:         1.0,
		TargetCraftID:    w.Target.CraftID,
		TargetGhostOwner: w.Target.GhostOwner,
		AdvisoryKey:      AdvisoryKeyMeetingBurn,
		BurnDirUnit:      row.BurnDir,
	}
	w.PlanNode(node)
	return &MeetingPlan{MeetingBurnOption: row, ForActive: true}, nil
}
