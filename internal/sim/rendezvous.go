package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
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
// τ and its predicted approach against the current relative target,
// found on the players' CURRENT courses — no burn assumed. ok=false
// when no encounter can be found at all: no relative target,
// cross-primary, or no approach inside the horizon — the App toasts
// instead of arming.
//
// #276: this used to prefer the K-nudge advisory's post-burn encounter
// on the theory that the initiator would go on to plant (K) and fly
// that burn — but Engage never plants the nudge itself, so with two
// craft on matched orbits (zero relative drift, no approach on the
// current course) the advisory could still find a hypothetical nudge
// and Engage would silently commit to ITS post-burn closest approach:
// an encounter the player was never actually flying toward. Committing
// only to the current-course search means a phantom advisory can no
// longer bypass the "no real encounter" refusal below; a player who
// wants the advisory's encounter must plant it with K first, then
// Engage sees it via their new current course.
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

func (w *World) RendezvousCommit() (tau time.Time, ca float64, ok bool) {
	active := w.ActiveCraft()
	if active == nil || !w.HasRelativeTarget() {
		return time.Time{}, 0, false
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
		return time.Time{}, 0, false
	}
	rT, vT, rok := w.TargetStateRelativeToActivePrimary()
	if !rok {
		return time.Time{}, 0, false
	}
	mu := active.Primary.GravitationalParameter()
	tCA, distCA, _, err := planner.NextClosestApproach(
		orbital.Vec3State{R: active.State.R, V: active.State.V},
		orbital.Vec3State{R: rT, V: vT},
		active.Primary, mu, rendezvousCommitHorizonSec)
	if err != nil || tCA <= 0 {
		return time.Time{}, 0, false
	}
	return w.Clock.SimTime.Add(time.Duration(tCA * float64(time.Second))), distCA, true
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
// after the previous one passed (#252). Two paths, mirroring the Engage
// commit:
//   - the target slot still holds the armed partner's ghost → reuse
//     RendezvousCommit verbatim, which is CURRENT-course-only (#276) — a
//     nudge planted and FIRED mid-coast is honoured automatically once
//     its burn has actually changed the craft's course; an unfired
//     advisory nudge is not;
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
	if w.Target.Kind == TargetGhost && w.Target.GhostOwner == partner.Owner {
		if t, c, cok := w.RendezvousCommit(); cok && t.After(floor) {
			return t, c, true
		}
	}
	active := w.ActiveCraft()
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

	// Horizon mirrors v0.9.3 NextClosestApproach defaults: ~2× the
	// longer orbital period, capped so the predictor's grid stays
	// dense.
	horizon := rendezvousHorizonSeconds(stateA, stateB, mu)
	_, currentCA, _, err := planner.NextClosestApproach(stateA, stateB, targetPrimary, mu, horizon)
	if err != nil {
		return planner.RendezvousAdvisory{}, false
	}

	advisory := planner.RecommendRendezvousNudge(stateA, stateB, targetPrimary, mu, horizon, currentCA)
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

// rendezvousHorizonSeconds picks a horizon for the closest-approach
// search. ~2× the larger of the two craft's orbital periods is
// enough to find the first encounter for any practical co-orbital
// scenario; capped at 2 hours so the predictor's grid stays sparse
// at deep-space distances.
func rendezvousHorizonSeconds(stateA, stateB orbital.Vec3State, mu float64) float64 {
	period := func(s orbital.Vec3State) float64 {
		r := s.R.Norm()
		v := s.V.Norm()
		// Vis-viva: a = 1 / (2/r - v²/μ).
		denom := 2/r - v*v/mu
		if denom <= 0 {
			return math.Inf(1)
		}
		a := 1 / denom
		return 2 * math.Pi * math.Sqrt(a*a*a/mu)
	}
	pA := period(stateA)
	pB := period(stateB)
	p := math.Max(pA, pB)
	if math.IsInf(p, 0) || p <= 0 {
		return 7200 // 2-hour fallback
	}
	horizon := 2 * p
	if horizon > 7200 {
		horizon = 7200
	}
	if horizon < 600 {
		horizon = 600
	}
	return horizon
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
