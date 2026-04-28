// Package missions defines pass/fail objectives evaluated against the
// live sim state each Tick. v0.6.5+.
//
// A Mission is a typed predicate over the spacecraft's current
// (primary, state, sim-time) tuple. Three predicate kinds ship in the
// v0.6.5 starter catalog:
//
//   - Circularize: craft is in the named primary's frame, orbit is
//     bound, eccentricity ≤ cap, and semimajor axis is within ±tol of
//     target altitude (radius + altitude_m).
//   - OrbitInsertion: craft is in the named primary's frame on a
//     bound orbit (e < 1).
//   - SOIFlyby: any tick where the craft's current primary ID matches
//     the named body.
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
	"time"

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
func (m Mission) Evaluate(ctx EvalContext) Status {
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

// Catalog is a list of missions, persisted to JSON. The starter
// catalog ships embedded in the binary; future config-file work could
// supply user-authored catalogs through the same shape.
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
