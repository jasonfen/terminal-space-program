package sim

import (
	"errors"

	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// ADR 0045 S6 (#399): K's modal decision. Before this slice K either
// planted the trim-rung nudge (PlanRendezvousNudge) or refused outright —
// a phase-mismatched pair that Δv couldn't nudge its way out of dead-ended
// at a refusal naming a tool (the Meeting Planner) the pilot had no key to
// reach (#277). This file is the orchestration that makes K modal: still
// plant directly when the trim rung works, open the Meeting Planner picker
// instead of refusing when it doesn't.

// rendezvousKDefaultPlace is the MeetingPlace the picker opens on — "their
// orbit" (ADR 0045 §2's own mockup): the active craft is the mover, which
// matches K's existing "I burn, they hold" trim-rung intuition more
// closely than either alternative.
const rendezvousKDefaultPlace = planner.MeetingTheirOrbit

// RendezvousKOutcome is PlanRendezvousOrOpenMeeting's result. Exactly one
// of Planted / OpenPicker is meaningful on success (err == nil); a non-nil
// err means K refused outright and neither rung fired.
type RendezvousKOutcome struct {
	// Planted is set when the trim rung (PlanRendezvousNudge) found a
	// plantable nudge directly — a close, near-matched pair. The node is
	// already on the active craft's Nodes slate.
	Planted *planner.RendezvousAdvisory

	// OpenPicker is true when the trim rung had nothing plantable but the
	// pair is coplanar, so the meeting rung opens instead of refusing.
	// Place/Ladder/LadderErr are the picker's initial state (the default
	// Place); LadderErr carries a per-place structural refusal
	// (ErrMeetingSizeMismatch / ErrMeetingNoCrossing, #407) the picker
	// itself must show rather than render a blank/broken chip — nothing is
	// planted yet, the picker is read-only until Enter, and ←/→ can still
	// try a different Place.
	OpenPicker bool
	Place      planner.MeetingPlace
	Ladder     planner.MeetingLadder
	LadderErr  error
}

// rendezvousKStructuralRefusal reports whether err means there is nothing
// for EITHER rung to work with, so K refuses outright rather than opening
// the picker: no craft, no target bound, a different primary, or already
// inside DOCK READY range. The Meeting Planner shares the identical first
// three gates (it would just repeat the same refusal, less directly) and
// has no "already docked" gate of its own to repeat the fourth — a docked
// pair has nowhere to "meet".
func rendezvousKStructuralRefusal(err error) bool {
	switch {
	case errors.Is(err, ErrRendezvousNoCraft),
		errors.Is(err, ErrRendezvousNoTarget),
		errors.Is(err, ErrRendezvousDifferentPrimaries),
		errors.Is(err, ErrRendezvousAlreadyDocked):
		return true
	}
	return false
}

// PlanRendezvousOrOpenMeeting is K's modal decision (ADR 0045 S6, #399): a
// close, near-matched pair still plants the trim-rung nudge directly,
// exactly as PlanRendezvousNudge always has; a pair too far apart in phase
// no longer refuses — it opens the Meeting Planner picker instead, closing
// #277's dead end by construction (K can no longer point at a tool the
// pilot has to go find on their own).
//
// K walks exactly two rungs — meeting and trim (ADR 0045 §2) — never a
// plane change. The plane-mismatch check runs FIRST, ahead of even
// attempting the trim-rung plant, so a diverged-plane pair is named
// ("your planes differ — match theirs [I] first") and nothing is ever
// planted — not even the trim rung's own axis-projection nudge, which can
// otherwise pick a Normal± axis and quietly re-shape the plane as a side
// effect of "improving" CA (see RecommendRendezvousNudge's InclinedTarget
// case: a 2° mismatch already fires the Normal± branch). [I]
// (PlanVesselPlaneMatch, #397) is the tool built for that job; K names it
// instead of doing a smaller, less deliberate version of the same thing.
//
// The plane check reuses RecommendMeetingLadder's own coplanar gate — the
// same physical check the Meeting Planner needs anyway (ErrMeetingPlaneMismatch),
// not a second implementation of it — computed once, for the default
// Place, since coplanarity doesn't depend on which of the three Places is
// chosen.
func (w *World) PlanRendezvousOrOpenMeeting() (RendezvousKOutcome, error) {
	ladder, lerr := w.RecommendMeetingLadder(rendezvousKDefaultPlace)
	switch {
	case errors.Is(lerr, ErrRendezvousNoCraft), errors.Is(lerr, ErrRendezvousNoTarget), errors.Is(lerr, ErrRendezvousDifferentPrimaries):
		// Nothing for either rung to work with — same refusal
		// PlanRendezvousNudge would give for the identical gate.
		return RendezvousKOutcome{}, lerr
	case errors.Is(lerr, ErrMeetingPlaneMismatch):
		return RendezvousKOutcome{}, lerr
	}

	adv, err := w.PlanRendezvousNudge()
	if err == nil {
		return RendezvousKOutcome{Planted: adv}, nil
	}
	if rendezvousKStructuralRefusal(err) {
		return RendezvousKOutcome{}, err
	}
	// Phase mismatch, shape mismatch, burn-too-large, or unsafe-periapsis:
	// the trim rung has nothing to plant, but the pair is coplanar (the
	// gate above already ruled out a plane mismatch) — open the meeting
	// rung instead of refusing.
	return RendezvousKOutcome{OpenPicker: true, Place: rendezvousKDefaultPlace, Ladder: ladder, LadderErr: lerr}, nil
}
