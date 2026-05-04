// Package missions defines pass/fail objectives evaluated against the
// live sim state each Tick. v0.6.5+.
//
// A Mission is a typed predicate over the spacecraft's current
// (primary, state, sim-time) tuple. Three predicate kinds shipped in
// the v0.6.5 starter catalog (circularize / orbit_insertion /
// soi_flyby) and a fourth — Expression — landed in v0.8.7 as the
// modder-friendly path: drop a `.json` mission with a boolean
// expression in `params.expression` and the engine evaluates it each
// tick against a fixed schema (craft.* + world.*) without recompile.
//
// Predicate kinds:
//
//   - Circularize: craft is in the named primary's frame, orbit is
//     bound, eccentricity ≤ cap, and semimajor axis is within ±tol of
//     target altitude (radius + altitude_m).
//   - OrbitInsertion: craft is in the named primary's frame on a
//     bound orbit (e < 1).
//   - SOIFlyby: any tick where the craft's current primary ID matches
//     the named body.
//   - Expression (v0.8.7+): a CEL-style boolean expression over an
//     EvalContext-derived environment. Optional FailExpression for
//     explicit failure conditions (e.g. "burn-through detected ⇒
//     fail"). Compiled once per mission, cached, evaluated per tick.
//
// Status is a three-state machine (InProgress → Passed | Failed).
// Terminal states are sticky: Evaluate is idempotent on Passed/Failed
// missions, so the per-tick caller can blindly walk all missions
// without filtering.
package missions

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// Status is the mission state machine.
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

// Type discriminates the predicate kind. Stored in JSON as a string
// so the catalog stays human-editable without leaking enum integers.
type Type string

const (
	TypeCircularize    Type = "circularize"
	TypeOrbitInsertion Type = "orbit_insertion"
	TypeSOIFlyby       Type = "soi_flyby"
	TypeExpression     Type = "expression" // v0.8.7+
)

// Params is the union of parameters across all predicate kinds. Each
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

	// Expression (v0.8.7+) is a boolean expression evaluated each tick
	// against an EvalContext-derived environment (see ExpressionEnv).
	// Mission passes when the expression returns true. Compile errors
	// fail-closed (mission stays InProgress and the catalog loader
	// reports the issue). Expression-type missions only.
	Expression string `json:"expression,omitempty"`

	// FailExpression (v0.8.7+) is the optional companion that, when it
	// evaluates true, transitions the mission to Failed. Evaluated
	// before Expression so a single tick can't both pass and fail.
	// Expression-type missions only.
	FailExpression string `json:"fail_expression,omitempty"`
}

// Mission groups the catalog metadata, the predicate parameters, and
// the runtime status. JSON-serialisable for both the catalog and the
// save file.
type Mission struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        Type   `json:"type"`
	Params      Params `json:"params"`
	Status      Status `json:"status,omitempty"`
}

// EvalContext is the minimum slice of World state a mission predicate
// needs. Lifted out of sim so the missions package can depend only on
// orbital/physics and avoid an import cycle.
type EvalContext struct {
	PrimaryID      string              // craft's current primary ID
	PrimaryRadiusM float64             // craft primary's mean radius
	PrimaryMu      float64             // craft primary's GM
	State          physics.StateVector // craft state in primary frame
	SimTime        time.Time           // current sim time
}

// Evaluate steps the predicate one tick and returns the new status.
// Idempotent on terminal states (Passed/Failed return immediately).
func (m *Mission) Evaluate(ctx EvalContext) Status {
	if m.Status != InProgress {
		return m.Status
	}
	switch m.Type {
	case TypeCircularize:
		return evalCircularize(m.Params, ctx)
	case TypeOrbitInsertion:
		return evalOrbitInsertion(m.Params, ctx)
	case TypeSOIFlyby:
		return evalSOIFlyby(m.Params, ctx)
	case TypeExpression:
		return m.evalExpression(ctx)
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

// ExpressionEnv is the schema visible to expression-type missions.
// Field names here become identifiers in the expression text — adding
// a field is a backward-compatible extension; renaming or removing
// breaks any catalog that referenced it. v0.8.7+.
type ExpressionEnv struct {
	// Craft state — current primary, orbital geometry, and propellant
	// snapshot. All distances are metres, velocities m/s, angles
	// degrees. Inclination is body-equatorial (ECI for Earth, MCI for
	// Mars, etc.) per the v0.8.6 frame convention.
	Primary       string  `expr:"primary"`         // craft's current primary ID (e.g. "earth", "mars")
	AltitudeM     float64 `expr:"altitude_m"`      // altitude above primary surface
	ApoapsisAltM  float64 `expr:"apoapsis_alt_m"`  // apo altitude (m above surface)
	PeriapsisAltM float64 `expr:"periapsis_alt_m"` // peri altitude (m above surface)
	Eccentricity  float64 `expr:"eccentricity"`
	InclinationD  float64 `expr:"inclination_deg"` // body-equatorial
	VelocityMs    float64 `expr:"velocity_m_s"`    // |v| in primary frame
	FuelKg        float64 `expr:"fuel_kg"`
	MonopropKg    float64 `expr:"monoprop_kg"`
	DvBudgetMs    float64 `expr:"dv_budget_m_s"`   // remaining Δv via rocket equation

	// World state.
	SimTimeUnix int64 `expr:"sim_time_unix"` // seconds since 1970-01-01 UTC
}

// expressionEnv builds the environment record from EvalContext for
// the expression-type evaluator. Mirrors the existing struct
// extraction the typed evaluators do, then exposes the fields under
// the names declared in ExpressionEnv's expr-tags.
func expressionEnv(ctx EvalContext) ExpressionEnv {
	env := ExpressionEnv{
		Primary:     ctx.PrimaryID,
		SimTimeUnix: ctx.SimTime.Unix(),
	}
	if ctx.PrimaryMu > 0 {
		el := orbital.ElementsFromState(ctx.State.R, ctx.State.V, ctx.PrimaryMu)
		if el.A > 0 && !math.IsNaN(el.A) && !math.IsInf(el.A, 0) {
			env.Eccentricity = el.E
			env.InclinationD = el.I * 180 / math.Pi
			if el.E < 1 {
				env.ApoapsisAltM = el.Apoapsis() - ctx.PrimaryRadiusM
				env.PeriapsisAltM = el.Periapsis() - ctx.PrimaryRadiusM
			}
		}
	}
	if r := ctx.State.R.Norm(); r > 0 {
		env.AltitudeM = r - ctx.PrimaryRadiusM
	}
	env.VelocityMs = ctx.State.V.Norm()
	// Mass / propellant fields are not threaded through the existing
	// EvalContext today — left zero, populated when sim.World.Tick is
	// updated to fill them in (v0.8.7 follow-up). Expressions that
	// read these on v0.8.7 see 0; fine for the initial catalog.
	return env
}

// programCache stores compiled expressions keyed by source text.
// Mission catalogs typically have a handful of expressions and the
// same expression may appear in multiple missions — caching by text
// lets duplicates share a compiled program. Single-threaded access
// from World.Tick today, but the mutex is cheap insurance against
// any future per-tick concurrency.
var (
	programCacheMu sync.Mutex
	programCache   = map[string]*vm.Program{}
)

// compileExpression returns the compiled program for src, caching by
// source text. Returns the cached program on hit; compiles + caches
// on miss. Compilation errors are surfaced verbatim; callers decide
// how to react (catalog loader fails the load, evaluator stays
// InProgress).
func compileExpression(src string) (*vm.Program, error) {
	programCacheMu.Lock()
	defer programCacheMu.Unlock()
	if prog, ok := programCache[src]; ok {
		return prog, nil
	}
	prog, err := expr.Compile(src,
		expr.Env(ExpressionEnv{}),
		expr.AsBool(), // mission expressions must evaluate to bool
	)
	if err != nil {
		return nil, err
	}
	programCache[src] = prog
	return prog, nil
}

// evalExpression runs the mission's pass / fail expressions against
// the environment built from ctx. Order: FailExpression first
// (failure dominates) → Expression. Returns InProgress on any error
// or when neither expression matches yet.
func (m *Mission) evalExpression(ctx EvalContext) Status {
	env := expressionEnv(ctx)
	if m.Params.FailExpression != "" {
		prog, err := compileExpression(m.Params.FailExpression)
		if err == nil {
			out, err := vm.Run(prog, env)
			if err == nil {
				if b, ok := out.(bool); ok && b {
					return Failed
				}
			}
		}
	}
	if m.Params.Expression != "" {
		prog, err := compileExpression(m.Params.Expression)
		if err == nil {
			out, err := vm.Run(prog, env)
			if err == nil {
				if b, ok := out.(bool); ok && b {
					return Passed
				}
			}
		}
	}
	return InProgress
}

// ValidateExpressions compiles every expression-type mission's
// Expression and FailExpression. Returns the first compile error
// found, with the offending mission ID prefixed for diagnostics.
// Callers (typically the catalog loader path) use this to fail
// loud rather than letting bad expressions silently never pass.
// v0.8.7+.
func ValidateExpressions(missions []Mission) error {
	for _, m := range missions {
		if m.Type != TypeExpression {
			continue
		}
		if m.Params.Expression != "" {
			if _, err := compileExpression(m.Params.Expression); err != nil {
				return fmt.Errorf("missions: %s: expression: %w", m.ID, err)
			}
		}
		if m.Params.FailExpression != "" {
			if _, err := compileExpression(m.Params.FailExpression); err != nil {
				return fmt.Errorf("missions: %s: fail_expression: %w", m.ID, err)
			}
		}
	}
	return nil
}

// Catalog is a list of missions, persisted to JSON. The starter
// catalog ships embedded in the binary; future config-file work could
// supply user-authored catalogs through the same shape.
type Catalog struct {
	Missions []Mission `json:"missions"`
}

// LoadCatalog parses JSON bytes into a Catalog and validates any
// expression-type missions by attempting compilation. Returns the
// first compile error found (mission ID prefixed) so a malformed
// expression fails the load loudly rather than sitting forever as
// InProgress in the player's slate.
func LoadCatalog(data []byte) (Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return Catalog{}, fmt.Errorf("missions: parse catalog: %w", err)
	}
	if err := ValidateExpressions(c.Missions); err != nil {
		return Catalog{}, err
	}
	return c, nil
}

// Clone returns a deep copy of the mission slice. Used when seeding
// World.Missions from the embedded catalog so per-session status
// progression doesn't mutate the shared template.
func Clone(missions []Mission) []Mission {
	if len(missions) == 0 {
		return nil
	}
	out := make([]Mission, len(missions))
	copy(out, missions)
	return out
}
