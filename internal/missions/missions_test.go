package missions

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

const (
	earthMu     = 3.986004418e14
	earthRadius = 6.371e6
	moonMu      = 4.9028e12
	moonRadius  = 1.7374e6
)

// circularState builds a state vector for a circular prograde orbit
// at altitude alt over a body of radius r and gravitational parameter
// mu. Returned in (X, Y) with V along +Y so the orbit lies in the
// equatorial plane.
func circularState(r, mu, alt float64) physics.StateVector {
	radius := r + alt
	v := math.Sqrt(mu / radius)
	return physics.StateVector{
		R: orbital.Vec3{X: radius},
		V: orbital.Vec3{Y: v},
		M: 1000,
	}
}

func TestCircularizeStartsInProgress(t *testing.T) {
	o := Objective{
		Kind: KindCircularize,
		Params: Params{
			PrimaryID:       "earth",
			AltitudeM:       1_000_000,
			AltitudeTolPct:  0.05,
			EccentricityCap: 0.005,
		},
	}
	// Craft in 500 km LEO — way short of 1000 km target.
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 500e3),
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("at 500 km, expected InProgress, got %v", got)
	}
}

func TestCircularizePassesAtTarget(t *testing.T) {
	o := Objective{
		Kind: KindCircularize,
		Params: Params{
			PrimaryID:       "earth",
			AltitudeM:       1_000_000,
			AltitudeTolPct:  0.05,
			EccentricityCap: 0.005,
		},
	}
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 1_000_000),
	}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("at 1000 km circular, expected Passed, got %v", got)
	}
}

func TestCircularizeTolerance(t *testing.T) {
	o := Objective{
		Kind: KindCircularize,
		Params: Params{
			PrimaryID:       "earth",
			AltitudeM:       1_000_000,
			AltitudeTolPct:  0.05, // ±5% on |a − target|
			EccentricityCap: 0.005,
		},
	}
	target := earthRadius + 1_000_000
	// Inside tolerance: a within 5% of target.
	insideAlt := 1_000_000 - 0.04*target // 4% under target
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, insideAlt),
	}
	if got := o.Evaluate(ctx); got != Passed {
		t.Errorf("inside tolerance, expected Passed, got %v", got)
	}
	// Outside tolerance: 10% under.
	outsideAlt := 1_000_000 - 0.10*target
	ctx.State = circularState(earthRadius, earthMu, outsideAlt)
	if got := o.Evaluate(ctx); got != InProgress {
		t.Errorf("outside tolerance, expected InProgress, got %v", got)
	}
}

func TestCircularizeEccentricityCap(t *testing.T) {
	o := Objective{
		Kind: KindCircularize,
		Params: Params{
			PrimaryID:       "earth",
			AltitudeM:       1_000_000,
			AltitudeTolPct:  0.05,
			EccentricityCap: 0.005,
		},
	}
	// Build an elliptical orbit with a == target but e = 0.05 (well
	// over the cap). Using vis-viva at a periapsis chosen to give the
	// right semimajor axis.
	a := earthRadius + 1_000_000
	e := 0.05
	rp := a * (1 - e)
	vp := math.Sqrt(earthMu * (2/rp - 1/a))
	state := physics.StateVector{
		R: orbital.Vec3{X: rp},
		V: orbital.Vec3{Y: vp},
		M: 1000,
	}
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          state,
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("e=0.05 over cap=0.005, expected InProgress, got %v", got)
	}
}

func TestCircularizeWrongPrimary(t *testing.T) {
	o := Objective{
		Kind: KindCircularize,
		Params: Params{
			PrimaryID:       "earth",
			AltitudeM:       1_000_000,
			AltitudeTolPct:  0.05,
			EccentricityCap: 0.005,
		},
	}
	// Craft is at the right altitude but around the moon — predicate
	// doesn't apply.
	ctx := EvalContext{
		PrimaryID:      "moon",
		PrimaryRadiusM: moonRadius,
		PrimaryMu:      moonMu,
		State:          circularState(moonRadius, moonMu, 1_000_000),
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("primary mismatch, expected InProgress, got %v", got)
	}
}

func TestOrbitInsertionPassesOnBoundOrbit(t *testing.T) {
	o := Objective{
		Kind:   KindOrbitInsertion,
		Params: Params{PrimaryID: "moon"},
	}
	ctx := EvalContext{
		PrimaryID:      "moon",
		PrimaryRadiusM: moonRadius,
		PrimaryMu:      moonMu,
		State:          circularState(moonRadius, moonMu, 100e3),
	}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("bound moon orbit, expected Passed, got %v", got)
	}
}

func TestOrbitInsertionInProgressOnHyperbolic(t *testing.T) {
	o := Objective{
		Kind:   KindOrbitInsertion,
		Params: Params{PrimaryID: "moon"},
	}
	// Velocity well above escape — hyperbolic.
	radius := moonRadius + 100e3
	vEsc := math.Sqrt(2 * moonMu / radius)
	state := physics.StateVector{
		R: orbital.Vec3{X: radius},
		V: orbital.Vec3{Y: vEsc * 1.5},
		M: 1000,
	}
	ctx := EvalContext{
		PrimaryID:      "moon",
		PrimaryRadiusM: moonRadius,
		PrimaryMu:      moonMu,
		State:          state,
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("hyperbolic, expected InProgress, got %v", got)
	}
}

func TestSOIFlybyPassesOnPrimaryMatch(t *testing.T) {
	o := Objective{
		Kind:   KindSOIFlyby,
		Params: Params{PrimaryID: "mars"},
	}
	ctx := EvalContext{PrimaryID: "mars"}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("inside Mars SOI, expected Passed, got %v", got)
	}
}

func TestSOIFlybyInProgressOtherwise(t *testing.T) {
	o := Objective{
		Kind:   KindSOIFlyby,
		Params: Params{PrimaryID: "mars"},
	}
	ctx := EvalContext{PrimaryID: "earth"}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("not in Mars SOI, expected InProgress, got %v", got)
	}
}

func TestEvaluateIdempotentOnTerminal(t *testing.T) {
	o := Objective{
		Kind:   KindSOIFlyby,
		Params: Params{PrimaryID: "mars"},
		Status: Passed,
	}
	// Even though we're back at Earth, a Passed objective stays Passed.
	ctx := EvalContext{PrimaryID: "earth"}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("Passed objective should stay Passed, got %v", got)
	}
}

func TestDefaultCatalogLoads(t *testing.T) {
	cat, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog: %v", err)
	}
	if len(cat.Missions) == 0 {
		t.Fatal("embedded catalog is empty")
	}
	// Key rungs of the v0.21 tutorial→challenge ladder are present.
	for _, id := range []string{"tut-orient", "tut-fly", "chal-high-orbit", "chal-luna-return", "chal-mars-flyby"} {
		if _, ok := missionByID(cat, id); !ok {
			t.Errorf("expected ladder mission %q in the embedded catalog", id)
		}
	}
	// Every mission is well-formed: a non-empty id and at least one objective,
	// each objective carrying a non-empty kind (an empty kind sits inert).
	for _, m := range cat.Missions {
		if m.ID == "" {
			t.Errorf("mission %q has an empty id", m.Name)
		}
		if len(m.Objectives) == 0 {
			t.Errorf("mission %q has no objectives", m.ID)
		}
		for j, o := range m.Objectives {
			if o.Kind == "" {
				t.Errorf("mission %q objective %d has an empty kind", m.ID, j)
			}
		}
	}
}

func TestStatusString(t *testing.T) {
	cases := []struct {
		s    Status
		want string
	}{
		{InProgress, "in progress"},
		{Passed, "passed"},
		{Failed, "failed"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Status(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestCloneIsIndependent(t *testing.T) {
	cat, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog: %v", err)
	}
	dup := Clone(cat.Missions)
	dup[0].Status = Passed
	if cat.Missions[0].Status == Passed {
		t.Fatal("Clone shares mission-level backing memory with source")
	}
	// Deep copy: mutating a cloned mission's nested objective status must
	// not bleed back into the source catalog. A shallow copy would alias
	// the Objectives slice and corrupt the embedded template.
	if len(dup[0].Objectives) == 0 {
		t.Fatal("test setup: cloned mission has no objectives")
	}
	dup[0].Objectives[0].Status = Passed
	if cat.Missions[0].Objectives[0].Status == Passed {
		t.Fatal("Clone shares objective backing memory with source")
	}
}

// Sanity check that EvalContext's SimTime field is wired but not
// currently consumed — guards against accidentally tying behaviour
// to wall-clock during the pre-dwell scaffold.
func TestEvalIgnoresSimTime(t *testing.T) {
	o := Objective{
		Kind:   KindSOIFlyby,
		Params: Params{PrimaryID: "mars"},
	}
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx1 := EvalContext{PrimaryID: "earth", SimTime: t1}
	ctx2 := EvalContext{PrimaryID: "earth", SimTime: t2}
	if o.Evaluate(ctx1) != o.Evaluate(ctx2) {
		t.Fatal("SimTime should not affect predicate outcome before dwell objectives")
	}
}

// TestCircularizeFromPadInProgressBelowFloor — a low orbit
// (periapsis below the configured floor) keeps the objective in
// progress. v0.9.2+.
func TestCircularizeFromPadInProgressBelowFloor(t *testing.T) {
	o := Objective{
		Kind: KindCircularizeFromPad,
		Params: Params{
			PrimaryID:        "earth",
			MinPeriapsisAltM: 200_000,
		},
	}
	// 100 km circular — below the 200 km floor.
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 100e3),
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Errorf("100 km LEO with 200 km floor: got %v, want InProgress", got)
	}
}

// TestCircularizeFromPadPassesAboveFloor — any bound orbit with
// periapsis above the floor passes, regardless of eccentricity.
// v0.9.2+.
func TestCircularizeFromPadPassesAboveFloor(t *testing.T) {
	o := Objective{
		Kind: KindCircularizeFromPad,
		Params: Params{
			PrimaryID:        "earth",
			MinPeriapsisAltM: 200_000,
		},
	}
	// 250 km circular — well above the 200 km floor.
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 250e3),
	}
	if got := o.Evaluate(ctx); got != Passed {
		t.Errorf("250 km LEO with 200 km floor: got %v, want Passed", got)
	}
}

// TestCircularizeFromPadRejectsHyperbolic — an unbound (e ≥ 1)
// trajectory keeps the objective in progress even if the
// instantaneous radius is above the floor. v0.9.2+.
func TestCircularizeFromPadRejectsHyperbolic(t *testing.T) {
	o := Objective{
		Kind: KindCircularizeFromPad,
		Params: Params{
			PrimaryID:        "earth",
			MinPeriapsisAltM: 200_000,
		},
	}
	// Build a hyperbolic state: 500 km altitude with v 50% above
	// escape velocity.
	radius := earthRadius + 500e3
	vEscape := math.Sqrt(2 * earthMu / radius)
	state := physics.StateVector{
		R: orbital.Vec3{X: radius},
		V: orbital.Vec3{Y: vEscape * 1.5},
		M: 1000,
	}
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          state,
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Errorf("hyperbolic with floor: got %v, want InProgress", got)
	}
}
