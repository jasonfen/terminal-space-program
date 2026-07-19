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
var (
	ErrRendezvousNoTarget           = transferError("rendezvous: no craft target")
	ErrRendezvousDifferentPrimaries = transferError("rendezvous: target around a different primary")
	ErrRendezvousAlreadyDocked      = transferError("rendezvous: already in DOCK READY range")
	ErrRendezvousNoImprovement      = transferError("rendezvous: no useful nudge in range")
	ErrRendezvousNoCraft            = transferError("rendezvous: no active craft")
)

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
// Prefers the K-nudge advisory's post-burn encounter — the initiator is
// expected to plant that burn and live through it en route — and falls
// back to the current-course closest approach when the advisory has no
// useful nudge (the encounter is already set up). ok=false when no
// encounter can be found at all: no relative target, cross-primary, or
// no approach inside the horizon — the App toasts instead of arming.
func (w *World) RendezvousCommit() (tau time.Time, ca float64, ok bool) {
	if adv, aok := w.RecommendedRendezvousBurn(); aok && adv.Ok {
		return w.Clock.SimTime.Add(time.Duration(adv.TArrival * float64(time.Second))), adv.AchievableCA, true
	}
	active := w.ActiveCraft()
	if active == nil || !w.HasRelativeTarget() {
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
		// Computed, but the answer is "no useful nudge".
		if advisory.Reason == "docked" {
			return nil, ErrRendezvousAlreadyDocked
		}
		return nil, ErrRendezvousNoImprovement
	}

	leadBuffer := w.rendezvousLeadBuffer(c, advisory.AxisUnit)

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
