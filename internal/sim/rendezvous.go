package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Rendezvous advisory errors. Exported so app.go's status flash can
// switch on them via errors.Is, mirroring the PlanCircularize* family
// (see maneuver.go:1132). v0.10.2+.
//
// The reasons carry no "rendezvous:" prefix (#285): the flight view
// labels the refusal at the display layer, exactly as it does for
// `circularize:` and `save:`, so a prefix here rendered twice
// ("rendezvous: rendezvous: no vessel target") on every K refusal.
var (
	ErrRendezvousNoTarget           = transferError("no vessel target")
	ErrRendezvousDifferentPrimaries = transferError("target around a different primary")
	ErrRendezvousAlreadyDocked      = transferError("already in DOCK READY range")
	ErrRendezvousNoImprovement      = transferError("no useful nudge in range")
	ErrRendezvousNoCraft            = transferError("no active vessel")

	// ErrRendezvousShapeMismatch / ErrRendezvousBurnTooLarge /
	// ErrRendezvousUnsafePeriapsis (ADR 0039 S1, #278): distinct refusal
	// reasons that used to all collapse into ErrRendezvousNoImprovement.
	// Each names the actual planner gate that fired and, where one
	// exists, the remedy — so "the nudge would be expensive" no longer
	// reads identically to "rendezvous is impossible" or "the geometry is
	// already optimal". Wording for the burn-too-large case is #278's own
	// proposed text verbatim.
	//
	// ADR 0045 §2 / #398 proposed retiring ErrRendezvousShapeMismatch
	// alongside the planner-side gate it names, on the theory that the
	// Meeting Planner (meeting.go) now covers a shape-mismatched pair.
	// That removal did not ship (PR #405 review — see
	// RecommendRendezvousNudge's doc comment for why); this sentinel and
	// the gate behind it stay.
	ErrRendezvousShapeMismatch   = transferError("orbits differ in shape — circularize [C] or plan a transfer [H] first")
	ErrRendezvousBurnTooLarge    = transferError("nudge would exceed the burn ceiling — use the transfer planner [H/I/m]")
	ErrRendezvousUnsafePeriapsis = transferError("nudge would drop periapsis unsafely — plan a transfer instead [H/I/m]")

	// ErrRendezvousNoEncounter (ADR 0039 S2, #277): the shared
	// phasing-coach remedy for "no real encounter to score" — both K's
	// inner Lambert-lookahead dead ends and Engage's own commit-search
	// failure return this, in the doctrine's own words, so the two
	// refusal chains stop pointing at each other. Before this, `w` said
	// "plant a nudge [K] first" and K said "no useful nudge in range" —
	// each pointing at the other with no way out.
	ErrRendezvousNoEncounter = transferError(rendezvousPhasingCoachMsg)
)

// rendezvousPhasingCoachMsg is the doctrine-prescribed remedy surfaced
// whenever no real encounter can be found on the current courses: a
// matched-orbit stalemate (zero relative drift, an encounter can never
// form, #276) and a slow-converging geometry (a real encounter exists
// but isn't found/committable within the search window, #277) both get
// the same actionable words, deliberately with no computed numbers (ADR
// 0039 §2) — "bring the encounter to you" rather than either coasting
// toward a slow one or being told nothing at all.
const rendezvousPhasingCoachMsg = "no encounter on current courses — make a phasing burn (wide prograde or radial) and watch the CA shrink"

// rendezvousReasonToErr maps a planner.RendezvousAdvisory's Reason tag
// (populated when Ok=false) to the sim-layer sentinel PlanRendezvousNudge
// returns. #278: collapsing every reason into one string left the player
// unable to tell "spend more Δv than a nudge allows" from "this is
// already as good as it gets" from "this cannot ever converge". The four
// inner Lambert-lookahead dead ends ("no lambert convergence",
// "degenerate axes", "horizon too short", "ca-verify failed") share the
// ADR 0039 §2 phasing-coach bucket: none of them found a real encounter
// to score, so none of them has a more specific remedy than "make a
// phasing burn" — the same wording Engage's own no-encounter refusal
// uses (#277).
func rendezvousReasonToErr(reason string) error {
	switch reason {
	case "docked":
		return ErrRendezvousAlreadyDocked
	case "orbit shape mismatch":
		return ErrRendezvousShapeMismatch
	case "burn too large — use H/I/m":
		return ErrRendezvousBurnTooLarge
	case "burn drops periapsis unsafely":
		return ErrRendezvousUnsafePeriapsis
	case "no improvement available":
		return ErrRendezvousNoImprovement
	default: // "no lambert convergence", "degenerate axes", "horizon too short", "ca-verify failed"
		return ErrRendezvousNoEncounter
	}
}

// rendezvousAdvisoryCache stores the most recent recommendation so
// the per-frame TARGET HUD readout does not have to re-run the
// Lambert + NextClosestApproach pipeline every tick (~5 ms LEO).
// Sim-time-throttled: recompute when the active/target indices
// change OR when sim-time has advanced past
// rendezvousRecomputeInterval since the last computation.
//
// Sim-time (not real-time) is the throttle clock because at warp the
// trajectories change with sim-time — a 50× warp player wants a
// recompute every ~10 ms real-time, which sim-time naturally
// produces. At 1× normal play the cache hits ~10 frames in a row,
// which is the per-frame budget win we're after.
type rendezvousAdvisoryCache struct {
	lastSimTime time.Time
	activeIdx   int
	targetID    uint64 // stable Spacecraft.ID of the cached target (ADR 0012)
	// targetGhostOwner distinguishes a ghost target from a local craft
	// that happens to share the id (v0.28 S4): "" for a local craft, the
	// remote player's handle for a ghost. Part of the cache key so
	// switching between a local craft and a same-id ghost never serves a
	// stale advisory.
	targetGhostOwner string
	advisory         planner.RendezvousAdvisory
	ok               bool
	populated        bool
}

// rendezvousRecomputeInterval is the sim-time gap that forces a
// recompute. 500 ms balances stale-by-a-tick acceptability against
// per-frame CPU cost. State changes smoothly at this granularity.
const rendezvousRecomputeInterval = 500 * time.Millisecond

// rendezvousBurnLeadMin is the floor on the dynamic lead buffer
// PlanRendezvousNudge applies to TriggerTime. Keeps a minimum margin
// for v0.10.0 slew alignment even when CurrentAttitudeDir already
// happens to line up with the recommended axis (slewTime ≈ 0).
const rendezvousBurnLeadMin = 30 * time.Second

// rendezvousBurnLeadPad is added on top of the dynamic slew estimate
// so a NavMode toggle mid-coast or a small attitude drift before
// ignition does not steal the lead-comp slew window.
const rendezvousBurnLeadPad = 5 * time.Second

// RecommendedRendezvousBurn returns the cached rendezvous advisory
// for the current active+target craft pair, recomputing on cache
// miss. Returns (_, false) when the advisory cannot be computed
// (no craft target, different primaries, or no active craft) so the
// TARGET HUD just hides the block.
//
// The returned advisory is the same struct callers see from the
// underlying planner.RecommendRendezvousNudge. ok=true-with-advisory
// where advisory.Ok=false is the "computed, but no improvement
// available" path (advisory.Reason populated — "no useful nudge" or
// "docked") the HUD surfaces as a faint single-line tag; ok=false means
// the advisory couldn't be computed at all (no craft target, different
// primaries, degenerate state) and the HUD hides the block.
func (w *World) RecommendedRendezvousBurn() (planner.RendezvousAdvisory, bool) {
	active := w.ActiveCraft()
	// v0.28 S4: a ghost (remote player's craft) is a valid rendezvous
	// target too — HasRelativeTarget covers both local craft and ghosts.
	if active == nil || !w.HasRelativeTarget() {
		w.rendezvousCache = rendezvousAdvisoryCache{}
		return planner.RendezvousAdvisory{}, false
	}
	primary, ok := w.rendezvousTargetPrimary()
	if !ok || primary.EnglishName != active.Primary.EnglishName {
		// Different-primary case is a gate, not an advisory — the
		// HUD just hides the block (cross-SOI rendezvous is out of
		// the v0.10.2 scope, matches v0.9.3 NextClosestApproach).
		return planner.RendezvousAdvisory{}, false
	}

	// Cache key: (active, target, ghost-owner, sim-time within interval).
	if w.rendezvousCache.populated &&
		w.rendezvousCache.activeIdx == w.ActiveCraftIdx &&
		w.rendezvousCache.targetID == w.Target.CraftID &&
		w.rendezvousCache.targetGhostOwner == w.Target.GhostOwner &&
		w.Clock.SimTime.Sub(w.rendezvousCache.lastSimTime) < rendezvousRecomputeInterval {
		return w.rendezvousCache.advisory, w.rendezvousCache.ok
	}

	advisory, ok := w.computeRendezvousAdvisory(active, primary)
	w.rendezvousCache = rendezvousAdvisoryCache{
		lastSimTime:      w.Clock.SimTime,
		activeIdx:        w.ActiveCraftIdx,
		targetID:         w.Target.CraftID,
		targetGhostOwner: w.Target.GhostOwner,
		advisory:         advisory,
		ok:               ok,
		populated:        true,
	}
	return advisory, ok
}

// rendezvousCommitHorizonSec bounds the current-course closest-approach
// search RendezvousCommit falls back to — the same 4 h window the TARGET
// chip's TCA row uses, so the committed encounter is one the player can
// already see on the HUD. Tunable.
const rendezvousCommitHorizonSec = 4 * 3600.0

// RendezvousCommit returns the encounter the initiator commits a
// Rendezvous Warp to (v0.29 S2, ADR 0034 v0.29 addendum): the absolute
// τ and its predicted approach against the current relative target.
// ok=false when no encounter can be found at all: no relative target,
// cross-primary, or no approach inside the horizon — the App toasts
// instead of arming.
//
// Two sources, tried in order:
//
//  1. A PLANTED rendezvous nudge (K). If the active craft is currently
//     carrying an unfired ManeuverNode tagged AdvisoryKeyRendezvousNudge,
//     the encounter is found on the course that node actually promises:
//     forward-integrate to the node's own TriggerTime, apply its Δv
//     (PostBurnState), Kepler-propagate the target to the same instant,
//     then search from there. This is the real course the player is
//     already committed to flying — the burn is queued and will fire on
//     its own; Engage is just naming where it leads.
//  2. Falls back to the CURRENT-course search (no burn assumed) when no
//     such node is planted, or the node doesn't yield an encounter
//     inside the horizon.
//
// ok=false from both means there is genuinely no encounter to commit to
// yet: the App's refusal names the doctrine's actionable remedy, plant
// a nudge with K first (see app.go's SessionCmdRendezvous handling).
//
// #276: this used to prefer the K-nudge ADVISORY's post-burn encounter
// — the HUD preview, computed fresh every tick whether or not the
// player ever presses K — on the theory that the initiator would go on
// to plant and fly it. But Engage never plants the nudge itself, so
// with two craft on matched orbits (zero relative drift, no approach on
// the current course) the advisory could still preview a hypothetical
// nudge and Engage would silently commit to ITS post-burn closest
// approach: an encounter the player was never actually flying toward.
// The fix restricted RendezvousCommit to the current-course search only
// — closing that hole, but also making the refusal's own prescribed
// remedy ("plant a nudge [K] first") a dead end: planting a node alone
// doesn't touch active.State (only FIRING does), so the very next
// Engage press hit the identical current-course refusal again with no
// way out (PR #392 review, finding 1).
//
// The fix here is narrower than the pre-#276 behavior it replaces: it
// doesn't resurrect the transient advisory preview, it honors an
// ACTUALLY-PLANTED node — one the player pressed K to queue, which will
// fire on its own whether or not Engage ever runs. Committing to that
// course is not committing to a "hypothetical" burn; the burn is
// already scheduled.
// rendezvousGapNoteBar is the "far above the lock gate" bar for the
// engage-time gap note (ADR 0039 S3, #281): a generous multiple of the
// couple gate, not "just outside" it — a committed CA a little past
// coWarpCoupleRangeM is still a normal near-miss the coast can plausibly
// narrow on its own across waypoints. This only fires once riding
// waypoints alone plainly will not close it — #281's live case grew
// 4,400 km → 11,049 km, orders of magnitude past this bar, with nothing
// on screen ever saying a burn was needed.
const rendezvousGapNoteBar = 3 * coWarpCoupleRangeM // 105 km

// RendezvousNeedsBurnToClose reports whether a committed CA is far
// enough above the couple/lock gate that a deliberate burn — not just
// riding waypoints — will be needed to actually close it (ADR 0039 S3).
// Called at Engage time on both sides (the initiator's own commit and
// the responder's join, which adopts the same τ/CA off the wire) so the
// choice to coast toward a wide encounter is visible up front rather
// than discovered after riding it for days.
func (w *World) RendezvousNeedsBurnToClose(ca float64) bool {
	return ca >= rendezvousGapNoteBar
}

// RendezvousPlan is RendezvousCommitWithPlan's result (ADR 0045 S7,
// #400): everything Engage needs to form the agreement — the same τ/CA
// RendezvousCommit returns, plus the Meeting Place + lap count when the
// commit's source was a planted Meeting Burn node. MeetingPlaceLabel is
// "" whenever the source was the trim-rung nudge or the current-course
// search — neither has a Place to name. A zero Tau (Tau.IsZero()) means
// "agreed, no plan yet": Engage no longer refuses on this (see
// RendezvousCommitWithPlan's doc comment) — it commits the agreement
// with nothing to chase.
type RendezvousPlan struct {
	Tau               time.Time
	CommittedCA       float64
	MeetingPlaceLabel string
	MeetingLaps       int
}

// RendezvousCommitWithPlan is RendezvousCommit's meeting-aware sibling
// (ADR 0045 S7, #400): the SAME structural gates and the SAME two
// (now three) search sources, but it never refuses on "no encounter
// found" — only on a genuinely structural failure (no active craft, no
// resolvable relative target, cross-primary). ok=false means Engage
// itself has nothing to act on at all; ok=true with a zero-Tau Plan means
// the agreement forms with no committed encounter — neither a planted
// node nor the 4h current-course search found one — which used to be
// Engage's outright refusal ("no closable encounter... plant a nudge [K]
// first") and is now the "agreed, no plan yet" state instead: the whole
// point of this slice is that Engage stops meaning "we found an
// encounter" and starts meaning "we are going to meet".
//
// Kept alongside RendezvousCommit (which now just delegates here and
// folds a zero-Tau Plan back into its own ok=false) rather than replacing
// it — changing RendezvousCommit's 3-value signature would touch every
// existing call site for no behavioural gain outside Engage itself.
//
// Three sources, tried in this order:
//
//  1. A PLANTED rendezvous nudge (K's trim rung) — unchanged from
//     RendezvousCommit's old Source 1, see rendezvousCommitFromPlantedNode.
//  2. A PLANTED Meeting Burn node (K's meeting rung / the picker's Enter,
//     PlanMeetingBurn) — new in this slice. Tried second, after the trim
//     rung: the two AdvisoryKeys never collide (K plants exactly one of
//     them per press, PlanRendezvousOrOpenMeeting's whole point), so in
//     practice at most one of Source 1/2 ever has anything to find: this
//     ordering is precedence for the rare case both somehow exist, not a
//     load-bearing choice. See rendezvousCommitFromPlantedMeetingNode for
//     why this source does NOT re-search within the 4h horizon the way
//     Source 1 does.
//  3. The current-course fallback (no burn assumed) — unchanged from
//     RendezvousCommit's old Source 2, see rendezvousCommitCurrentCourse.
//     Still bounded by rendezvousCommitHorizonSec: this is a SEARCH, not
//     a plan.
//
// ok=true with a zero Tau when none of the three sources found anything —
// the agreed-no-plan state.
func (w *World) RendezvousCommitWithPlan() (RendezvousPlan, bool) {
	active := w.ActiveCraft()
	if active == nil || !w.HasRelativeTarget() {
		return RendezvousPlan{}, false
	}
	// Shared-primary gate (#261): the fallback below Kepler-propagates the
	// target's state around the ACTIVE craft's primary. For a target
	// orbiting another body, TargetStateRelativeToActivePrimary happily
	// converts via inertial — but the converted state is not on a conic
	// around this primary, so a closest approach found on it is
	// dynamically meaningless. Gate on primary IDENTITY (the same signal
	// rendezvousNextWaypoint's peer-set fallback filters on, c.Primary ==
	// active.Primary.ID), never on state math: refusing here lets the
	// standing intent (#252) fall through to the same-primary peer-set
	// search and, when that too is empty, to idle-and-retry with the
	// degrade warning up (armedPartnerLacksLocalCraft).
	if primary, pok := w.rendezvousTargetPrimary(); !pok || primary.ID != active.Primary.ID {
		return RendezvousPlan{}, false
	}
	rT, vT, rok := w.TargetStateRelativeToActivePrimary()
	if !rok {
		return RendezvousPlan{}, false
	}
	mu := active.Primary.GravitationalParameter()

	// Source 1: an actually-planted rendezvous nudge (finding 1), but
	// ONLY when it's still bound to the peer being engaged right now
	// (PR #392 review, follow-up finding): a nudge planted against peer
	// A must not be honored when w.Target has since moved to peer B —
	// plantedAdvisoryNode's own TargetCraftID/TargetGhostOwner check
	// enforces that, falling through to Source 2 on a mismatch exactly
	// as if nothing were planted. Tried first when it does match — a
	// queued, self-firing burn is a more concrete commitment than the
	// no-burn current course, so when both would yield an encounter the
	// planted one wins.
	if node, nok := plantedAdvisoryNode(active, AdvisoryKeyRendezvousNudge, w.Target.CraftID, w.Target.GhostOwner); nok {
		if t, c, cok := w.rendezvousCommitFromPlantedNode(active, node, rT, vT, mu); cok {
			return RendezvousPlan{Tau: t, CommittedCA: c}, true
		}
	}

	// Source 2: a planted Meeting Burn node (ADR 0045 S7, #400).
	if node, nok := plantedAdvisoryNode(active, AdvisoryKeyMeetingBurn, w.Target.CraftID, w.Target.GhostOwner); nok {
		if t, c, cok := w.rendezvousCommitFromPlantedMeetingNode(active, node, rT, vT, mu); cok {
			return RendezvousPlan{
				Tau: t, CommittedCA: c,
				MeetingPlaceLabel: node.MeetingPlaceLabel,
				MeetingLaps:       node.MeetingLaps,
			}, true
		}
	}

	// Source 3: the current-course fallback (no burn assumed). Its own
	// ok=false is no longer Engage's refusal — it's the agreed-no-plan
	// state (zero-value Plan), still reported as RendezvousCommitWithPlan
	// ok=true since every structural gate above already passed.
	if t, c, cok := w.rendezvousCommitCurrentCourse(active, rT, vT, mu); cok {
		return RendezvousPlan{Tau: t, CommittedCA: c}, true
	}
	return RendezvousPlan{}, true
}

// RendezvousCommit returns the encounter the initiator commits a
// Rendezvous Warp to (v0.29 S2, ADR 0034 v0.29 addendum): the absolute
// τ and its predicted approach against the current relative target.
// ok=false when no encounter can be found at all: no relative target,
// cross-primary, or no approach inside the horizon.
//
// ADR 0045 S7 (#400): this signature and its "nothing found ⇒ ok=false"
// behaviour are UNCHANGED — every pre-existing caller keeps working
// exactly as before. Delegates to RendezvousCommitWithPlan above, which
// folds a zero-Tau Plan (the new "agreed, no plan yet" outcome) back into
// ok=false here. Engage itself (app.go's SessionCmdRendezvous handling)
// calls RendezvousCommitWithPlan directly, since it now DOES act on the
// zero-Tau case instead of refusing — see that function's doc comment
// for the full source list and the behavioural change.
func (w *World) RendezvousCommit() (tau time.Time, ca float64, ok bool) {
	plan, pok := w.RendezvousCommitWithPlan()
	if !pok || plan.Tau.IsZero() {
		return time.Time{}, 0, false
	}
	return plan.Tau, plan.CommittedCA, true
}

// rendezvousCommitCurrentCourse searches for a closest approach on the
// CURRENT, no-burn courses only (#276) — active.State as-is against the
// target's rT/vT snapshot, nothing forward-integrated or Δv-applied.
//
// This is its own function (PR #392 review, finding 2) because it has
// two distinct callers with two distinct contracts: RendezvousCommit's
// own Source 2 fallback (Engage may prefer a planted-but-unfired nudge
// first, then fall back to this), and rendezvousNextWaypoint's mid-coast
// re-derivation, which must NEVER prefer an unfired node — a standing
// waypoint/τ that re-derives itself off a node that might be deleted,
// refused, or superseded before it ever fires would coast the pair
// toward an encounter that can evaporate out from under them. Before
// this split, rendezvousNextWaypoint called RendezvousCommit itself,
// which was safe only as long as RendezvousCommit had no planted-node
// preference of its own — finding 1 added that preference, silently
// breaking rendezvousNextWaypoint's own pinned "honors a nudge only once
// FIRED" comment. Extracting the current-course search keeps that
// invariant true by construction: rendezvousNextWaypoint's mid-coast
// path calls THIS, never RendezvousCommit.
func (w *World) rendezvousCommitCurrentCourse(active *spacecraft.Spacecraft, rT, vT orbital.Vec3, mu float64) (time.Time, float64, bool) {
	tCA, distCA, _, err := planner.NextClosestApproach(
		orbital.Vec3State{R: active.State.R, V: active.State.V},
		orbital.Vec3State{R: rT, V: vT},
		active.Primary, mu, rendezvousCommitHorizonSec)
	if err != nil || tCA <= 0 {
		return time.Time{}, 0, false
	}
	return w.Clock.SimTime.Add(time.Duration(tCA * float64(time.Second))), distCA, true
}

// rendezvousCommitFromPlantedNode computes the encounter a planted
// rendezvous-nudge node actually leads to: Kepler-propagate the target's
// CURRENT relative state (rT, vT) forward to the node's TriggerTime
// first, then forward-integrate the active craft to the same instant and
// apply its Δv in the node's direction mode — resolving target-relative
// axes (BurnTargetPrograde/Retrograde, PR #392 review finding 1) against
// that SAME propagated target state, not the target's state now, so the
// direction and the closest-approach search below share one consistent
// snapshot. ok=false when the node is already past-due, the target's
// coast doesn't admit a Kepler propagation (hyperbolic/degenerate —
// KeplerStep returns ok=false), the post-burn state lands in a different
// primary's frame (an SOI crossing before the burn fires — the target's
// frame no longer matches, so no meaningful comparison exists), the
// node's direction can't be resolved (a target-relative axis with a
// degenerate relative state — postBurnStateWithTarget's own ok=false;
// this is "skip the planted-node path", never "commit an unburned
// coast"), or no approach turns up inside the horizon.
func (w *World) rendezvousCommitFromPlantedNode(active *spacecraft.Spacecraft, node spacecraft.ManeuverNode, rT, vT orbital.Vec3, mu float64) (time.Time, float64, bool) {
	dt := node.TriggerTime.Sub(w.Clock.SimTime).Seconds()
	if dt <= 0 {
		return time.Time{}, 0, false
	}
	targetState, tok := physics.KeplerStep(physics.StateVector{R: rT, V: vT}, mu, dt)
	if !tok {
		return time.Time{}, 0, false
	}
	postState, primaryID, pok := w.postBurnStateWithTarget(node, targetState.R, targetState.V)
	if !pok || primaryID != active.Primary.ID {
		return time.Time{}, 0, false
	}
	tCA, distCA, _, err := planner.NextClosestApproach(
		orbital.Vec3State{R: postState.R, V: postState.V},
		orbital.Vec3State{R: targetState.R, V: targetState.V},
		active.Primary, mu, rendezvousCommitHorizonSec)
	if err != nil || tCA <= 0 {
		return time.Time{}, 0, false
	}
	return node.TriggerTime.Add(time.Duration(tCA * float64(time.Second))), distCA, true
}

// rendezvousCommitFromPlantedMeetingNode computes the encounter a planted
// Meeting Burn node (AdvisoryKeyMeetingBurn) actually leads to — the
// meeting-aware sibling of rendezvousCommitFromPlantedNode just above,
// called only from RendezvousCommitWithPlan's Source 2 (ADR 0045 S7,
// #400). Shares its sibling's first two steps (Kepler-propagate the
// target's current relative state to the node's TriggerTime, then apply
// the node's burn against that same propagated snapshot via
// postBurnStateWithTarget) but then DIVERGES: instead of running
// NextClosestApproach over rendezvousCommitHorizonSec, it propagates BOTH
// the post-burn mover and the (unburned) holder straight to
// node.MeetingArrivalSec past TriggerTime — no SEARCH. The Meeting
// Planner's tangential solve (planner.meetingLadderCore) already aimed
// this exact burn at this exact instant; re-searching within the 4h
// window the way the trim-rung sibling does would miss any wait longer
// than that window entirely, which is the ordinary case for more than a
// couple of laps (ADR 0045 §5 — "the 4h window bounds a search, not a
// plan").
//
// ok=false for the same reasons as the sibling (past-due node, a
// degenerate Kepler step, an SOI crossing before the burn fires, an
// unresolvable direction), plus a node with no MeetingArrivalSec — a
// non-Meeting-Burn node reaching this by construction error, or a node
// planted before ADR 0045 S7 added the field — treated as "no plan
// info", not zero wait.
func (w *World) rendezvousCommitFromPlantedMeetingNode(active *spacecraft.Spacecraft, node spacecraft.ManeuverNode, rT, vT orbital.Vec3, mu float64) (time.Time, float64, bool) {
	if node.MeetingArrivalSec <= 0 {
		return time.Time{}, 0, false
	}
	dt := node.TriggerTime.Sub(w.Clock.SimTime).Seconds()
	if dt <= 0 {
		return time.Time{}, 0, false
	}
	targetState, tok := physics.KeplerStep(physics.StateVector{R: rT, V: vT}, mu, dt)
	if !tok {
		return time.Time{}, 0, false
	}
	postState, primaryID, pok := w.postBurnStateWithTarget(node, targetState.R, targetState.V)
	if !pok || primaryID != active.Primary.ID {
		return time.Time{}, 0, false
	}
	moverArr, mok := physics.KeplerStep(postState, mu, node.MeetingArrivalSec)
	holderArr, hok := physics.KeplerStep(targetState, mu, node.MeetingArrivalSec)
	if !mok || !hok {
		return time.Time{}, 0, false
	}
	arrival := node.TriggerTime.Add(time.Duration(node.MeetingArrivalSec * float64(time.Second)))
	dist := moverArr.R.Sub(holderArr.R).Norm()
	return arrival, dist, true
}

// rendezvousWaypointMinLead is the minimum forward distance a newly
// derived waypoint must have from SimTime (#252). Guards the advance loop
// against a closest-approach search whose grid minimum sits essentially
// at "now" (a pair at their CA and separating): re-committing to a τ a
// tick away would advance — and chip — every tick. Below the lead the
// derivation reports not-found and the coast idles at the 1× floor
// instead.
const rendezvousWaypointMinLead = 5 * time.Second

// rendezvousNextWaypoint derives the standing intent's next waypoint
// after the previous one passed (#252). Two paths:
//   - the target slot still holds the armed partner's ghost → call
//     rendezvousCommitCurrentCourse directly (PR #392 review, finding 2
//     — NOT RendezvousCommit, which since finding 1 prefers a planted-
//     but-unfired rendezvous nudge). This path must stay CURRENT-
//     course-only (#276): a nudge planted and FIRED mid-coast is
//     honoured automatically once its burn has actually changed the
//     craft's course; an unfired advisory nudge is not — delete the
//     node, or let it get refused at ignition, and a waypoint derived
//     from its hypothetical post-burn course would coast the pair
//     toward an encounter that will never occur;
//   - otherwise (player retargeted mid-coast, or the ghost slate is
//     momentarily stale) → target-slot-independent fallback: the
//     current-course closest approach against the partner's same-primary
//     craft from this tick's peer set (min over crafts, like
//     rendezvousCAAtTau — a deployed probe must not masquerade as the
//     encounter).
//
// A passive-flyby waypoint (no useful nudge, encounter is whatever the
// current course gives) is a legitimate result, not a failure. ok=false
// only when no future approach can be found at all this tick.
func (w *World) rendezvousNextWaypoint(partner *CoWarpPeer) (tau time.Time, ca float64, ok bool) {
	floor := w.Clock.SimTime.Add(rendezvousWaypointMinLead)
	active := w.ActiveCraft()
	if w.Target.Kind == TargetGhost && w.Target.GhostOwner == partner.Owner && active != nil {
		if primary, pok := w.rendezvousTargetPrimary(); pok && primary.ID == active.Primary.ID {
			if rT, vT, rok := w.TargetStateRelativeToActivePrimary(); rok {
				mu := active.Primary.GravitationalParameter()
				if mu > 0 {
					if t, c, cok := w.rendezvousCommitCurrentCourse(active, rT, vT, mu); cok && t.After(floor) {
						return t, c, true
					}
				}
			}
		}
	}
	if active == nil || active.Landed || active.Crashed {
		return time.Time{}, 0, false
	}
	mu := active.Primary.GravitationalParameter()
	if mu <= 0 {
		return time.Time{}, 0, false
	}
	best := math.Inf(1)
	for _, c := range partner.Crafts {
		if c.Primary != active.Primary.ID {
			continue
		}
		tCA, distCA, _, err := planner.NextClosestApproach(
			orbital.Vec3State{R: active.State.R, V: active.State.V},
			orbital.Vec3State{R: c.R, V: c.V},
			active.Primary, mu, rendezvousCommitHorizonSec)
		if err != nil || tCA <= 0 {
			continue
		}
		at := w.Clock.SimTime.Add(time.Duration(tCA * float64(time.Second)))
		if !at.After(floor) {
			continue
		}
		if distCA < best {
			best, tau, ca, ok = distCA, at, distCA, true
		}
	}
	return tau, ca, ok
}

// rendezvousTargetPrimary returns the SOI primary the current target
// orbits, for both a local craft target and a remote ghost (v0.28 S4).
// ok=false when no relative target is set or the ref is stale.
func (w *World) rendezvousTargetPrimary() (bodies.CelestialBody, bool) {
	switch w.Target.Kind {
	case TargetCraft:
		t, _, ok := w.craftByID(w.Target.CraftID)
		if !ok {
			return bodies.CelestialBody{}, false
		}
		return t.Primary, true
	case TargetGhost:
		_, primary, ok := w.ResolveTargetGhost()
		return primary, ok
	}
	return bodies.CelestialBody{}, false
}

// computeRendezvousAdvisory does the uncached work: gather primary-
// relative states, compute currentCA via NextClosestApproach, check
// the docked gate, then hand off to the planner. targetPrimary is the
// SOI primary the target orbits — the same body as active.Primary here
// (same-primary gated upstream); it works identically whether the
// target is a local craft or a remote ghost (v0.28 S4), since the
// relative state comes from TargetStateRelativeToActivePrimary which
// already resolves both.
func (w *World) computeRendezvousAdvisory(active *spacecraft.Spacecraft, targetPrimary bodies.CelestialBody) (planner.RendezvousAdvisory, bool) {
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return planner.RendezvousAdvisory{}, false
	}
	stateA := orbital.Vec3State{R: active.State.R, V: active.State.V}
	stateB := orbital.Vec3State{R: rT, V: vT}

	mu := active.Primary.GravitationalParameter()
	if mu <= 0 {
		return planner.RendezvousAdvisory{}, false
	}

	// Docked gate: < 50 m + |v_rel| < 0.1 m/s ⇒ no recommendation.
	separation := stateB.R.Sub(stateA.R).Norm()
	vRel := stateB.V.Sub(stateA.V).Norm()
	if separation < 50 && vRel < 0.1 {
		return planner.RendezvousAdvisory{
			Ok:     false,
			Reason: "docked",
		}, true
	}

	// Horizon: the same 4 h window every other rendezvous surface
	// (TARGET chip TCA, Rendezvous Warp commit) searches, so K scores
	// against the encounter the pilot can actually see (ADR 0045 S1,
	// #394). Was previously its own period-scaled window that
	// disagreed with the other two surfaces.
	_, currentCA, _, err := planner.NextClosestApproach(stateA, stateB, targetPrimary, mu, rendezvousCommitHorizonSec)
	if err != nil {
		return planner.RendezvousAdvisory{}, false
	}

	advisory := planner.RecommendRendezvousNudge(stateA, stateB, targetPrimary, mu, rendezvousCommitHorizonSec, currentCA)
	if !advisory.Ok {
		// no-improvement / Lambert-divergence / degenerate-axes:
		// caller surfaces advisory.Reason in the HUD; bool=true here
		// so the HUD can distinguish "we computed and the answer is
		// 'no useful nudge'" from "we couldn't compute" (false from
		// the outer gate).
		return advisory, true
	}
	return advisory, true
}

// PlanRendezvousNudge plants the recommended single-burn nudge as a
// new ManeuverNode on the active craft. Returns the advisory used so
// the caller can build a status flash; returns (nil, err) when the
// gate fails (no target, different primaries, no improvement, etc.).
//
// TriggerTime = SimTime + leadBuffer, where leadBuffer is dynamic:
// max(rendezvousBurnLeadMin, nodeLeadSlack·angle/SlewRateRad + pad).
// This ensures v0.10.0 lead-compensated slew has room to converge
// even when the recommended axis is far from the current attitude.
// Event=TriggerAbsolute (immediate-style — fires at the computed
// time, no future event-relative resolution). TargetCraftIdx is
// captured one-based per the spacecraft.ManeuverNode convention so
// a later target switch does not retarget the planted burn.
//
// v0.10.2+.
func (w *World) PlanRendezvousNudge() (*planner.RendezvousAdvisory, error) {
	c := w.ActiveCraft()
	if c == nil {
		return nil, ErrRendezvousNoCraft
	}
	// v0.28 S4: ghost targets (remote players' craft) plant just like
	// local craft targets — HasRelativeTarget covers both.
	if !w.HasRelativeTarget() {
		return nil, ErrRendezvousNoTarget
	}
	primary, ok := w.rendezvousTargetPrimary()
	if !ok {
		return nil, ErrRendezvousNoTarget
	}
	if primary.EnglishName != c.Primary.EnglishName {
		return nil, ErrRendezvousDifferentPrimaries
	}

	advisory, ok := w.RecommendedRendezvousBurn()
	if !ok {
		return nil, ErrRendezvousNoImprovement
	}
	if !advisory.Ok {
		// Computed, but the answer is "no useful nudge" — #278: the
		// specific reason travels to the player instead of collapsing
		// into one generic refusal.
		return nil, rendezvousReasonToErr(advisory.Reason)
	}

	leadBuffer := w.rendezvousLeadBuffer(c, advisory.AxisUnit)

	// #293: a second K press replaces its own previous unfired nudge
	// node instead of stacking a stale duplicate behind it — every
	// advisory is computed from the craft's CURRENT orbit, so a node
	// queued behind an earlier one would fire against an orbit it was
	// never computed for. Must run before the append below.
	w.replaceAdvisoryNode(c, AdvisoryKeyRendezvousNudge)

	mode := axisLabelToBurnMode(advisory.Axis)
	node := ManeuverNode{
		Mode:          mode,
		DV:            advisory.DV,
		Duration:      c.BurnTimeForDV(advisory.DV),
		Event:         spacecraft.TriggerAbsolute,
		TriggerTime:   w.Clock.SimTime.Add(leadBuffer),
		PrimaryID:     c.Primary.ID,
		Throttle:      1.0,
		TargetCraftID: w.Target.CraftID, // bind by stable ID (ADR 0012)
		// v0.28 S4: for a ghost target, carry the remote owner so the
		// node ref resolves against the ghost slate (empty for a local
		// craft target — w.Target.GhostOwner is "" unless Kind==Ghost).
		TargetGhostOwner: w.Target.GhostOwner,
		AdvisoryKey:      AdvisoryKeyRendezvousNudge,
	}
	w.PlanNode(node)
	out := advisory
	return &out, nil
}

// rendezvousLeadBuffer computes the lead time PlanRendezvousNudge
// applies to TriggerTime. Mirrors the v0.10.0 nodeLeadActive formula
// (nodeLeadSlack·angle/SlewRate) and adds a 5 s pad + a 30 s floor.
func (w *World) rendezvousLeadBuffer(c *spacecraft.Spacecraft, axisUnit orbital.Vec3) time.Duration {
	floor := rendezvousBurnLeadMin
	slew := c.SlewRateRad()
	if slew <= 0 {
		return floor
	}
	cur := c.CurrentAttitudeDir.Unit()
	axisU := axisUnit.Unit()
	if cur.Norm() == 0 || axisU.Norm() == 0 {
		return floor
	}
	cosA := cur.Dot(axisU)
	if cosA > 1 {
		cosA = 1
	} else if cosA < -1 {
		cosA = -1
	}
	ang := math.Acos(cosA)
	slewSecs := nodeLeadSlack * ang / slew
	dynamic := time.Duration(slewSecs*float64(time.Second)) + rendezvousBurnLeadPad
	if dynamic < floor {
		return floor
	}
	return dynamic
}

// axisLabelToBurnMode maps the planner's local axis enum to the
// spacecraft package's BurnMode. The planner cannot import
// spacecraft (sibling packages), so the mapping lives here on the
// sim side which already imports both.
func axisLabelToBurnMode(a planner.AxisLabel) spacecraft.BurnMode {
	switch a {
	case planner.AxisPrograde:
		return spacecraft.BurnPrograde
	case planner.AxisRetrograde:
		return spacecraft.BurnRetrograde
	case planner.AxisNormalPlus:
		return spacecraft.BurnNormalPlus
	case planner.AxisNormalMinus:
		return spacecraft.BurnNormalMinus
	case planner.AxisRadialOut:
		return spacecraft.BurnRadialOut
	case planner.AxisRadialIn:
		return spacecraft.BurnRadialIn
	case planner.AxisTargetPrograde:
		return spacecraft.BurnTargetPrograde
	case planner.AxisTargetRetrograde:
		return spacecraft.BurnTargetRetrograde
	}
	return spacecraft.BurnPrograde
}
