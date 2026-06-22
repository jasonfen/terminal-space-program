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
)

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
}

// Evaluate steps the objective one tick and returns the new status.
// Idempotent on terminal states (Passed/Failed return immediately).
// Does not mutate the receiver — the caller owns the status.
func (o Objective) Evaluate(ctx EvalContext) Status {
	if o.Status != InProgress {
		return o.Status
	}
	switch o.Kind {
	case KindCircularize:
		return evalCircularize(o.Params, ctx)
	case KindOrbitInsertion:
		return evalOrbitInsertion(o.Params, ctx)
	case KindSOIFlyby:
		return evalSOIFlyby(o.Params, ctx)
	case KindCircularizeFromPad:
		return evalCircularizeFromPad(o.Params, ctx)
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
// every Objective has Passed; it Fails the moment any Objective Fails (no
// Objective produces Failed until the slice-3 failure semantics land). A
// Mission with no Objectives never passes.
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
// Requires / Unlocks slices are copied too — a shallow copy would let a
// cloned mission's objective-status mutation bleed back into the
// embedded catalog.
func Clone(ms []Mission) []Mission {
	if len(ms) == 0 {
		return nil
	}
	out := make([]Mission, len(ms))
	for i := range ms {
		out[i] = ms[i]
		out[i].Objectives = append([]Objective(nil), ms[i].Objectives...)
		out[i].Requires = append([]string(nil), ms[i].Requires...)
		out[i].Unlocks = append([]string(nil), ms[i].Unlocks...)
	}
	return out
}
