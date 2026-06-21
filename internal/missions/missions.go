// Package missions defines pass/fail Objectives and the Missions that
// bundle them, evaluated against the live sim state each Tick.
//
// Vocabulary (ADR 0025, v0.21 — a deliberate inversion of the pre-v0.21
// naming, where a single predicate was called a "Mission"):
//
//   - Objective — the atomic pass/fail predicate over the spacecraft's
//     current (primary, state, sim-time) tuple. Four instantaneous kinds
//     ship today (circularize, orbit_insertion, soi_flyby,
//     circularize_from_pad); the v0.21 vocabulary grows in later slices.
//   - Mission — an ordered list of Objectives plus campaign metadata
//     (a `program` tag and `requires`/`unlocks` edges). The player sees a
//     Mission as a checklist of sub-steps; the Mission carries the
//     sequencing memory so each Objective stays memoryless. "Luna landing
//     & return" is one Mission whose ordered Objectives are
//     [land_at_luna] → [return_to_earth].
//
// Status is a three-state machine (InProgress → Passed | Failed) at both
// levels. Terminal states are sticky: Evaluate is idempotent on
// Passed/Failed, so the per-tick caller can blindly walk everything
// without filtering.
package missions

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// Status is the objective/mission state machine.
type Status int

const (
	InProgress Status = iota
	Passed
	Failed
)

// String returns a human-readable label for the status.
func (s Status) String() string {
	switch s {
	case InProgress:
		return "in progress"
	case Passed:
		return "passed"
	case Failed:
		return "failed"
	}
	return "?"
}

// Kind discriminates the objective predicate kind. Stored in JSON as a
// string so the catalog stays human-editable without leaking enum
// integers. Kind names are part of the modder-facing schema (ADR 0025
// §7) — renaming one breaks community catalogs.
type Kind string

const (
	KindCircularize        Kind = "circularize"
	KindOrbitInsertion     Kind = "orbit_insertion"
	KindSOIFlyby           Kind = "soi_flyby"
	KindCircularizeFromPad Kind = "circularize_from_pad" // v0.9.2+

	// v0.21 (ADR 0025 §3) — the new state-objective vocabulary. All are
	// instantaneous current-state checks; dwell is deferred to v0.22.
	KindReachAltitude Kind = "reach_altitude"
	KindLandAtBody    Kind = "land_at_body"
	KindRendezvous    Kind = "rendezvous"
	KindDock          Kind = "dock"
	KindReturnToBody  Kind = "return_to_body"

	// KindEvent (ADR 0025 §6, v0.21) is the second objective family: it
	// matches a semantic gameplay Action (Params.Action) that fired while the
	// objective was active, rather than a world-state predicate. Used by
	// control-teaching tutorial steps that leave no world trace.
	KindEvent Kind = "event"
)

// Action is a semantic gameplay verb the player triggers, recorded downward
// from the input layer (ADR 0025 §7, v0.21). The input resolves a keybinding
// to an Action and records the Action — not the raw keystroke — so event
// objectives survive rebinding (GH #130) and layout presets (ADR 0022). Like
// Kind, the string values are modder-facing schema. Pure camera / navigation
// / meta bindings (zoom, tilt, focus, help, quit) are deliberately excluded —
// no tutorial verifies them.
type Action string

const (
	ActionThrottleFull    Action = "throttle_full"
	ActionThrottleCut     Action = "throttle_cut"
	ActionThrottleUp      Action = "throttle_up"
	ActionThrottleDown    Action = "throttle_down"
	ActionOpenManeuver    Action = "open_maneuver"
	ActionPlanTransfer    Action = "plan_transfer"
	ActionPlanCircularize Action = "plan_circularize"
	ActionPlanIncl        Action = "plan_incl"
	ActionPlanRendezvous  Action = "plan_rendezvous"
	ActionRefinePlan      Action = "refine_plan"
	ActionClearNodes      Action = "clear_nodes"
	ActionToggleBurn      Action = "toggle_burn"
	ActionStage           Action = "stage"
	ActionCycleTarget     Action = "cycle_target"
	ActionClearTarget     Action = "clear_target"
	ActionCycleView       Action = "cycle_view"
	ActionCycleNavMode    Action = "cycle_navmode"
	ActionAutoWarp        Action = "auto_warp"
	ActionSpawnCraft      Action = "spawn_craft"
	ActionUndock          Action = "undock"
	ActionTranspose       Action = "transpose"
)

// FailCondition is an opt-in trigger that transitions an InProgress
// Objective to Failed (ADR 0025 §5, v0.21 — the first code path that
// produces Failed). An Objective declares zero or more on its FailOn
// list; declaring none means it never fails (retry forever). Like Kind,
// the string values are part of the modder-facing schema — renaming one
// breaks community catalogs. Both conditions read the Active Vessel
// (per-craft binding is deferred to a v0.22 ADR amendment).
type FailCondition string

const (
	// FailCrashed fails when the active craft is Crashed (the ADR 0004
	// destructive surface-contact signal).
	FailCrashed FailCondition = "crashed"
	// FailOutOfFuel fails when the active craft has no main propellant
	// left in ANY stage (TotalFuelKg <= 0). It reads the summed all-stage
	// fuel, not the active stage, so a multi-stage craft whose bottom
	// stage is spent but whose upper stages are full is not "out of fuel"
	// — staging would continue the flight. RCS monoprop is excluded.
	FailOutOfFuel FailCondition = "out_of_fuel"
)

// Label returns the player-facing phrasing of a fail condition, used by the
// in-flight failure flash (ADR 0025 §5 / Slice 5). Falls back to the raw
// schema string for any condition without an explicit label.
func (fc FailCondition) Label() string {
	switch fc {
	case FailCrashed:
		return "crashed"
	case FailOutOfFuel:
		return "out of fuel"
	}
	return string(fc)
}

// Params is the union of parameters across all objective kinds. Each
// kind reads only the fields it cares about; the rest stay zero.
type Params struct {
	// PrimaryID is the body whose frame the predicate evaluates in
	// (Circularize / OrbitInsertion) or whose SOI counts as a flyby
	// (SOIFlyby).
	PrimaryID string `json:"primary_id,omitempty"`

	// AltitudeM is the target altitude above the primary's mean
	// radius, in metres. Circularize only.
	AltitudeM float64 `json:"altitude_m,omitempty"`

	// AltitudeTolPct is the fractional tolerance on |a − target|/target.
	// 0.05 = ±5%. Circularize only.
	AltitudeTolPct float64 `json:"altitude_tol_pct,omitempty"`

	// EccentricityCap is the upper bound on eccentricity. Circularize
	// only. 0.005 ≈ ≤0.5% radial swing.
	EccentricityCap float64 `json:"eccentricity_cap,omitempty"`

	// MinPeriapsisAltM is the minimum periapsis altitude (m above
	// primary's mean radius) for the CircularizeFromPad predicate
	// to pass. Looser than Circularize: the orbit just has to be
	// bound (e < 1) and clear the floor — eccentricity / specific
	// altitude shape is unconstrained, so a 100 × 300 km elliptical
	// LEO counts the same as a 200 × 200 km circular one.
	// v0.9.2+.
	MinPeriapsisAltM float64 `json:"min_periapsis_alt_m,omitempty"`

	// MinAltitudeM is the minimum altitude above the primary's mean
	// radius (m) for ReachAltitude to pass. v0.21+.
	MinAltitudeM float64 `json:"min_altitude_m,omitempty"`

	// SiteLatDeg / SiteLonDeg / SiteRadiusM constrain LandAtBody to a
	// target landing zone: the touchdown must lie within SiteRadiusM
	// (great-circle metres) of (SiteLatDeg, SiteLonDeg). Coordinates are
	// north-positive, east-positive. When SiteRadiusM is zero the kind
	// accepts a landing anywhere on the body. v0.21+.
	SiteLatDeg  float64 `json:"site_lat_deg,omitempty"`
	SiteLonDeg  float64 `json:"site_lon_deg,omitempty"`
	SiteRadiusM float64 `json:"site_radius_m,omitempty"`

	// RangeM / RelSpeedMs are the Rendezvous gates: the active craft must
	// be within RangeM metres of the targeted craft AND closing slower
	// than RelSpeedMs metres/second. v0.21+.
	RangeM     float64 `json:"range_m,omitempty"`
	RelSpeedMs float64 `json:"rel_speed_ms,omitempty"`

	// Action is the semantic gameplay verb an Event objective matches
	// (ADR 0025 §6/§7). KindEvent only. v0.21+.
	Action Action `json:"action,omitempty"`
}

// Objective is the atomic pass/fail predicate (the pre-v0.21 Mission).
// JSON-serialisable for both the catalog and the save file.
type Objective struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Kind        Kind   `json:"kind"`
	Params      Params `json:"params"`
	Status      Status `json:"status,omitempty"`

	// FailOn is the opt-in set of conditions that fail this objective
	// while it is InProgress (ADR 0025 §5, v0.21). Empty (the default)
	// means the objective never fails. Additive to the v9 save shape
	// (omitempty), so no schema bump. v0.21+.
	FailOn []FailCondition `json:"fail_on,omitempty"`
}

// EvalContext is the minimum slice of World state an objective predicate
// needs. Lifted out of sim so the missions package can depend only on
// orbital/physics and avoid an import cycle. (Slice 2 expands this.)
type EvalContext struct {
	PrimaryID      string              // craft's current primary ID
	PrimaryRadiusM float64             // craft primary's mean radius
	PrimaryMu      float64             // craft primary's GM
	State          physics.StateVector // craft state in primary frame
	SimTime        time.Time           // current sim time

	// v0.21 (ADR 0025) surface/landing state. Landed is the ADR 0004
	// surface-contact flag; SurfaceLatDeg/LonDeg are the craft's
	// body-fixed touchdown coordinates (north-positive, east-positive),
	// valid when Landed. Read by land_at_body and return_to_body.
	Landed        bool
	SurfaceLatDeg float64
	SurfaceLonDeg float64

	// v0.21 (ADR 0025) target-craft relative state, against the player's
	// current target slot. HasTargetCraft is false when no craft is
	// targeted; TargetRangeM / TargetRelSpeedMs are the instantaneous
	// separation and closing speed. Read by rendezvous.
	HasTargetCraft   bool
	TargetRangeM     float64
	TargetRelSpeedMs float64

	// Docked is true when the active craft is a docked composite (it has
	// fused with at least one other vessel). Read by dock. v0.21+.
	Docked bool

	// v0.21 (ADR 0025) resource + outcome snapshot. FuelKg is the active
	// (bottom) stage's propellant; DvBudget is the active stage's remaining
	// Δv (m/s); no v0.21 state-objective kind consumes either yet (slice-4
	// outcome steps will). TotalFuelKg is the summed main propellant across
	// ALL stages — the signal the out_of_fuel fail condition reads, since a
	// spent bottom stage with full upper stages is not stranded (staging
	// continues the flight). Crashed is the ADR 0004 destructive-contact
	// flag read by the crashed fail condition.
	FuelKg      float64
	MonopropKg  float64
	DvBudget    float64
	TotalFuelKg float64
	Crashed     bool
	HasNode     bool // active craft has at least one planted maneuver node
	HasTarget   bool // a body or craft is selected in the world target slot
	Staged      bool // the player has decoupled a stage this session

	// RecentActions are the semantic gameplay actions recorded since the last
	// mission-eval tick (ADR 0025 §6, v0.21). The sim drains this each tick,
	// so the event objective sees only actions fired during the current tick
	// window — which, combined with Mission ordering (an objective is only
	// evaluated while active), gives the "fired while InProgress" semantics
	// without any per-objective watermark. Read by KindEvent.
	RecentActions []Action
}

// Evaluate steps the objective one tick and returns the new status.
// Idempotent on terminal states (Passed/Failed return immediately).
// Does not mutate the receiver — the caller owns the status.
//
// Pass takes precedence over fail on the same tick: a terminal status from
// the kind predicate (today only Passed) wins even when a declared fail
// condition is also met — achieving the goal beats, say, running the tanks
// dry on touchdown. Only while the kind reports InProgress do the opt-in
// fail_on conditions apply (ADR 0025 §5, v0.21).
func (o Objective) Evaluate(ctx EvalContext) Status {
	if o.Status != InProgress {
		return o.Status
	}
	if s := o.evalKind(ctx); s != InProgress {
		return s
	}
	if o.failOnTriggered(ctx) {
		return Failed
	}
	return InProgress
}

// evalKind dispatches to the per-kind state predicate. An unknown kind
// stays InProgress so an unrecognised catalog entry sits inert rather than
// spuriously passing.
func (o Objective) evalKind(ctx EvalContext) Status {
	switch o.Kind {
	case KindCircularize:
		return evalCircularize(o.Params, ctx)
	case KindOrbitInsertion:
		return evalOrbitInsertion(o.Params, ctx)
	case KindSOIFlyby:
		return evalSOIFlyby(o.Params, ctx)
	case KindCircularizeFromPad:
		return evalCircularizeFromPad(o.Params, ctx)
	case KindReachAltitude:
		return evalReachAltitude(o.Params, ctx)
	case KindReturnToBody:
		return evalReturnToBody(o.Params, ctx)
	case KindLandAtBody:
		return evalLandAtBody(o.Params, ctx)
	case KindRendezvous:
		return evalRendezvous(o.Params, ctx)
	case KindDock:
		return evalDock(o.Params, ctx)
	case KindEvent:
		return evalEvent(o.Params, ctx)
	}
	return InProgress
}

// evalEvent: pass when the objective's Action appears in the since-last-tick
// action sink (ctx.RecentActions) — i.e. the player triggered that semantic
// gameplay verb during this tick window. Because the sim drains the sink each
// tick and Mission ordering only evaluates an objective while it is active, a
// match here means the action fired while the objective was InProgress. A
// missing Action param is inert (never matches). v0.21+ (ADR 0025 §6).
func evalEvent(p Params, ctx EvalContext) Status {
	if p.Action == "" {
		return InProgress
	}
	for _, a := range ctx.RecentActions {
		if a == p.Action {
			return Passed
		}
	}
	return InProgress
}

// failOnTriggered reports whether any of the objective's declared fail
// conditions is currently met. An objective with no FailOn never triggers
// (ADR 0025 §5: declare nothing → never fails, retry forever).
func (o Objective) failOnTriggered(ctx EvalContext) bool {
	_, ok := o.FailReason(ctx)
	return ok
}

// FailReason returns the first declared fail condition currently met and
// true, or ("", false) when none is — the same predicate failOnTriggered
// gates Evaluate on, but surfacing *which* condition fired so the player
// surface can name it ("failed: crashed"). v0.21 Slice 5 (ADR 0025 §5).
func (o Objective) FailReason(ctx EvalContext) (FailCondition, bool) {
	for _, fc := range o.FailOn {
		switch fc {
		case FailCrashed:
			if ctx.Crashed {
				return FailCrashed, true
			}
		case FailOutOfFuel:
			if ctx.TotalFuelKg <= 0 {
				return FailOutOfFuel, true
			}
		}
	}
	return "", false
}

// evalDock: pass when the active craft is a docked composite. Grounded in
// the durable composite state (DockedComponents) rather than the ephemeral
// dock event, keeping the check instantaneous. The optional target_craft
// param (ADR 0025 §3) is deferred — any dock satisfies the kind. v0.21+.
func evalDock(_ Params, ctx EvalContext) Status {
	if ctx.Docked {
		return Passed
	}
	return InProgress
}

// evalRendezvous: pass when a craft is targeted and the active craft is
// within RangeM metres of it AND closing slower than RelSpeedMs. Evaluated
// against the player's current target slot rather than a catalog-named
// craft, since runtime craft IDs aren't authorable in a static catalog.
// v0.21+.
func evalRendezvous(p Params, ctx EvalContext) Status {
	if !ctx.HasTargetCraft {
		return InProgress
	}
	if ctx.TargetRangeM <= p.RangeM && ctx.TargetRelSpeedMs <= p.RelSpeedMs {
		return Passed
	}
	return InProgress
}

// evalLandAtBody: pass when the craft is Landed on the named primary. An
// optional site (SiteRadiusM > 0) further requires the touchdown to lie
// within SiteRadiusM great-circle metres of (SiteLatDeg, SiteLonDeg);
// without a site, landing anywhere on the body passes. v0.21+.
func evalLandAtBody(p Params, ctx EvalContext) Status {
	if ctx.PrimaryID != p.PrimaryID {
		return InProgress
	}
	if !ctx.Landed {
		return InProgress
	}
	if p.SiteRadiusM <= 0 {
		return Passed
	}
	d := greatCircleDistanceM(ctx.SurfaceLatDeg, ctx.SurfaceLonDeg, p.SiteLatDeg, p.SiteLonDeg, ctx.PrimaryRadiusM)
	if d <= p.SiteRadiusM {
		return Passed
	}
	return InProgress
}

// greatCircleDistanceM returns the surface distance (metres) between two
// lat/lon points (degrees) on a sphere of the given radius, via the
// haversine formula.
func greatCircleDistanceM(lat1, lon1, lat2, lon2, radiusM float64) float64 {
	const deg2rad = math.Pi / 180
	phi1 := lat1 * deg2rad
	phi2 := lat2 * deg2rad
	dPhi := (lat2 - lat1) * deg2rad
	dLambda := (lon2 - lon1) * deg2rad
	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLambda/2)*math.Sin(dLambda/2)
	return radiusM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// evalReturnToBody: pass when the craft has "come back" to the named body
// — its current primary matches AND it is either captured (a bound orbit,
// e < 1) or Landed. A hyperbolic whizz-through of the SOI is not a return.
// The there-and-back sequencing comes from the Mission's ordering; this
// predicate just confirms the craft actually stayed. v0.21+.
func evalReturnToBody(p Params, ctx EvalContext) Status {
	if ctx.PrimaryID != p.PrimaryID {
		return InProgress
	}
	if ctx.Landed {
		return Passed
	}
	if ctx.PrimaryMu == 0 {
		return InProgress
	}
	el := orbital.ElementsFromState(ctx.State.R, ctx.State.V, ctx.PrimaryMu)
	if el.A <= 0 || math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return InProgress
	}
	if el.E >= 1 {
		return InProgress
	}
	return Passed
}

// evalReachAltitude: pass when the craft is in the named primary's frame
// and its current altitude above the primary's mean radius reaches the
// floor. A pure radial check — orbit shape is irrelevant. v0.21+.
func evalReachAltitude(p Params, ctx EvalContext) Status {
	if ctx.PrimaryID != p.PrimaryID {
		return InProgress
	}
	if ctx.State.R.Norm()-ctx.PrimaryRadiusM >= p.MinAltitudeM {
		return Passed
	}
	return InProgress
}

func evalCircularize(p Params, ctx EvalContext) Status {
	if ctx.PrimaryID != p.PrimaryID {
		return InProgress
	}
	if ctx.PrimaryMu == 0 {
		return InProgress
	}
	el := orbital.ElementsFromState(ctx.State.R, ctx.State.V, ctx.PrimaryMu)
	if el.A <= 0 || math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return InProgress
	}
	if el.E >= 1 {
		return InProgress
	}
	if el.E > p.EccentricityCap {
		return InProgress
	}
	target := ctx.PrimaryRadiusM + p.AltitudeM
	if target <= 0 {
		return InProgress
	}
	if math.Abs(el.A-target)/target > p.AltitudeTolPct {
		return InProgress
	}
	return Passed
}

func evalOrbitInsertion(p Params, ctx EvalContext) Status {
	if ctx.PrimaryID != p.PrimaryID {
		return InProgress
	}
	if ctx.PrimaryMu == 0 {
		return InProgress
	}
	el := orbital.ElementsFromState(ctx.State.R, ctx.State.V, ctx.PrimaryMu)
	if el.A <= 0 || math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return InProgress
	}
	if el.E >= 1 {
		return InProgress
	}
	return Passed
}

func evalSOIFlyby(p Params, ctx EvalContext) Status {
	if ctx.PrimaryID == p.PrimaryID {
		return Passed
	}
	return InProgress
}

// evalCircularizeFromPad: pass when the craft is in the right
// primary's frame on a bound orbit (e < 1) with periapsis above
// PrimaryRadiusM + MinPeriapsisAltM. Looser than Circularize —
// no eccentricity / altitude-tolerance constraint, just a
// periapsis floor. The "from pad" framing is informational;
// the predicate doesn't gate on initial conditions, only on the
// achieved orbit. v0.9.2+.
func evalCircularizeFromPad(p Params, ctx EvalContext) Status {
	if ctx.PrimaryID != p.PrimaryID {
		return InProgress
	}
	if ctx.PrimaryMu == 0 {
		return InProgress
	}
	el := orbital.ElementsFromState(ctx.State.R, ctx.State.V, ctx.PrimaryMu)
	if el.A <= 0 || math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return InProgress
	}
	if el.E >= 1 {
		return InProgress
	}
	periapsis := el.Periapsis()
	if periapsis < ctx.PrimaryRadiusM+p.MinPeriapsisAltM {
		return InProgress
	}
	return Passed
}

// Mission bundles an ordered list of Objectives plus campaign metadata.
// A Program (ADR 0025 §2) is not a separate type — it is a lightweight
// grouping expressed by the Program tag plus Requires/Unlocks edges
// between Missions, so a campaign and the tutorial are both gated
// Mission chains. JSON-serialisable for both the catalog and the save
// file.
type Mission struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Program     string      `json:"program,omitempty"`  // campaign grouping tag
	Requires    []string    `json:"requires,omitempty"` // mission IDs that must Pass before this unlocks
	Unlocks     []string    `json:"unlocks,omitempty"`  // mission IDs this unlocks (informational)
	Objectives  []Objective `json:"objectives"`
	Status      Status      `json:"status,omitempty"`
}

// Evaluate steps the Mission's Objectives in catalog order and returns
// the rolled-up Mission status, mutating per-Objective and Mission
// Status in place. Sticky on terminal states.
//
// Ordering is the defining behaviour of a Mission-as-sequence: an
// Objective is evaluated only once every earlier Objective has Passed, so
// a later step can't latch before its predecessor (e.g. you can't
// "return" before you "land"). An Objective that just Passed this tick
// lets the next one be evaluated the same tick. The Mission Passes when
// every Objective has Passed; it Fails the moment any Objective Fails —
// which an objective does when one of its opt-in fail_on conditions fires
// (ADR 0025 §5, v0.21). A Mission with no Objectives never passes.
func (m *Mission) Evaluate(ctx EvalContext) Status {
	if m.Status != InProgress {
		return m.Status
	}
	for i := range m.Objectives {
		if m.Objectives[i].Status == Passed {
			continue
		}
		s := m.Objectives[i].Evaluate(ctx)
		m.Objectives[i].Status = s
		if s == Failed {
			m.Status = Failed
			return m.Status
		}
		if s != Passed {
			// Ordered: this objective is still in progress, so every
			// later objective stays locked until it passes.
			return m.Status
		}
	}
	if len(m.Objectives) > 0 {
		m.Status = Passed
	}
	return m.Status
}

// Progress reports how many of the mission's objectives have Passed and
// the total count — the "N/M" the checklist chip and ladder screen show.
// Only Passed counts toward N; a Failed objective is not progress. v0.21
// Slice 5 (ADR 0025).
func (m Mission) Progress() (passed, total int) {
	for i := range m.Objectives {
		if m.Objectives[i].Status == Passed {
			passed++
		}
	}
	return passed, len(m.Objectives)
}

// CurrentObjective returns the first objective that has not yet Passed (the
// one the player is working on) and true, or the zero Objective and false
// when every objective has Passed or the mission has none. For an active
// (InProgress) mission this is the first still-InProgress step; for a Failed
// mission it is the step that failed. v0.21 Slice 5 (ADR 0025).
func (m Mission) CurrentObjective() (Objective, bool) {
	for i := range m.Objectives {
		if m.Objectives[i].Status != Passed {
			return m.Objectives[i], true
		}
	}
	return Objective{}, false
}

// FailedObjective returns the first objective in the Failed state and true,
// or the zero Objective and false when none has failed. The player surface
// uses it (together with Objective.FailReason) to name why a mission died.
// v0.21 Slice 5 (ADR 0025).
func (m Mission) FailedObjective() (Objective, bool) {
	for i := range m.Objectives {
		if m.Objectives[i].Status == Failed {
			return m.Objectives[i], true
		}
	}
	return Objective{}, false
}

// RequirementsMet reports whether every mission ID in this mission's
// Requires list is present and true in passed (the set of already-Passed
// mission IDs). A mission with no Requires is always met (an ungated rung).
// Gates both the ladder screen's locked-vs-available classification (ADR
// 0025 §2 / §"Locked rungs") and — since v0.21 Slice 6 — the evaluator
// itself: a locked mission is not evaluated, so its objectives can't latch
// out of order (ADR 0025 §8: "requires gates the next mission"). v0.21.
func (m Mission) RequirementsMet(passed map[string]bool) bool {
	for _, id := range m.Requires {
		if !passed[id] {
			return false
		}
	}
	return true
}

// PassedSet returns the set of mission IDs currently in the Passed state —
// the prerequisite set RequirementsMet is checked against. v0.21 Slice 6.
func PassedSet(ms []Mission) map[string]bool {
	out := make(map[string]bool, len(ms))
	for i := range ms {
		if ms[i].Status == Passed {
			out[ms[i].ID] = true
		}
	}
	return out
}

// Catalog is a list of Missions, persisted to JSON. The starter catalog
// ships embedded in the binary; user overlays load through LoadAll.
type Catalog struct {
	Missions []Mission `json:"missions"`
}

// LoadCatalog parses JSON bytes into a Catalog.
func LoadCatalog(data []byte) (Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return Catalog{}, fmt.Errorf("missions: parse catalog: %w", err)
	}
	return c, nil
}

// Clone returns a deep copy of the mission slice. Used when seeding
// World.Missions from a catalog so per-session status progression
// doesn't mutate the shared template. The per-Mission Objectives /
// Requires / Unlocks slices are copied too — and each Objective's FailOn
// slice — because a shallow copy would let a cloned mission's mutation
// bleed back into the embedded catalog.
func Clone(ms []Mission) []Mission {
	if len(ms) == 0 {
		return nil
	}
	out := make([]Mission, len(ms))
	for i := range ms {
		out[i] = ms[i]
		out[i].Objectives = append([]Objective(nil), ms[i].Objectives...)
		for j := range out[i].Objectives {
			out[i].Objectives[j].FailOn = append([]FailCondition(nil), ms[i].Objectives[j].FailOn...)
		}
		out[i].Requires = append([]string(nil), ms[i].Requires...)
		out[i].Unlocks = append([]string(nil), ms[i].Unlocks...)
	}
	return out
}
