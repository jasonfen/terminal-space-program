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
	m := Mission{
		Type: TypeCircularize,
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
	if got := m.Evaluate(ctx); got != InProgress {
		t.Fatalf("at 500 km, expected InProgress, got %v", got)
	}
}

func TestCircularizePassesAtTarget(t *testing.T) {
	m := Mission{
		Type: TypeCircularize,
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
	if got := m.Evaluate(ctx); got != Passed {
		t.Fatalf("at 1000 km circular, expected Passed, got %v", got)
	}
}

func TestCircularizeTolerance(t *testing.T) {
	m := Mission{
		Type: TypeCircularize,
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
	if got := m.Evaluate(ctx); got != Passed {
		t.Errorf("inside tolerance, expected Passed, got %v", got)
	}
	// Outside tolerance: 10% under.
	outsideAlt := 1_000_000 - 0.10*target
	ctx.State = circularState(earthRadius, earthMu, outsideAlt)
	if got := m.Evaluate(ctx); got != InProgress {
		t.Errorf("outside tolerance, expected InProgress, got %v", got)
	}
}

func TestCircularizeEccentricityCap(t *testing.T) {
	m := Mission{
		Type: TypeCircularize,
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
	if got := m.Evaluate(ctx); got != InProgress {
		t.Fatalf("e=0.05 over cap=0.005, expected InProgress, got %v", got)
	}
}

func TestCircularizeWrongPrimary(t *testing.T) {
	m := Mission{
		Type: TypeCircularize,
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
	if got := m.Evaluate(ctx); got != InProgress {
		t.Fatalf("primary mismatch, expected InProgress, got %v", got)
	}
}

func TestOrbitInsertionPassesOnBoundOrbit(t *testing.T) {
	m := Mission{
		Type:   TypeOrbitInsertion,
		Params: Params{PrimaryID: "moon"},
	}
	ctx := EvalContext{
		PrimaryID:      "moon",
		PrimaryRadiusM: moonRadius,
		PrimaryMu:      moonMu,
		State:          circularState(moonRadius, moonMu, 100e3),
	}
	if got := m.Evaluate(ctx); got != Passed {
		t.Fatalf("bound moon orbit, expected Passed, got %v", got)
	}
}

func TestOrbitInsertionInProgressOnHyperbolic(t *testing.T) {
	m := Mission{
		Type:   TypeOrbitInsertion,
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
	if got := m.Evaluate(ctx); got != InProgress {
		t.Fatalf("hyperbolic, expected InProgress, got %v", got)
	}
}

func TestSOIFlybyPassesOnPrimaryMatch(t *testing.T) {
	m := Mission{
		Type:   TypeSOIFlyby,
		Params: Params{PrimaryID: "mars"},
	}
	ctx := EvalContext{PrimaryID: "mars"}
	if got := m.Evaluate(ctx); got != Passed {
		t.Fatalf("inside Mars SOI, expected Passed, got %v", got)
	}
}

func TestSOIFlybyInProgressOtherwise(t *testing.T) {
	m := Mission{
		Type:   TypeSOIFlyby,
		Params: Params{PrimaryID: "mars"},
	}
	ctx := EvalContext{PrimaryID: "earth"}
	if got := m.Evaluate(ctx); got != InProgress {
		t.Fatalf("not in Mars SOI, expected InProgress, got %v", got)
	}
}

func TestEvaluateIdempotentOnTerminal(t *testing.T) {
	m := Mission{
		Type:   TypeSOIFlyby,
		Params: Params{PrimaryID: "mars"},
		Status: Passed,
	}
	// Even though we're back at Earth, a Passed mission stays Passed.
	ctx := EvalContext{PrimaryID: "earth"}
	if got := m.Evaluate(ctx); got != Passed {
		t.Fatalf("Passed mission should stay Passed, got %v", got)
	}
}

func TestDefaultCatalogLoads(t *testing.T) {
	cat, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog: %v", err)
	}
	if len(cat.Missions) != 3 {
		t.Fatalf("expected 3 starter missions, got %d", len(cat.Missions))
	}
	wantIDs := map[string]bool{
		"leo-circularize-1000": false,
		"luna-orbit-insertion": false,
		"mars-soi-flyby":       false,
	}
	for _, m := range cat.Missions {
		if _, ok := wantIDs[m.ID]; !ok {
			t.Errorf("unexpected mission id %q", m.ID)
			continue
		}
		wantIDs[m.ID] = true
	}
	for id, found := range wantIDs {
		if !found {
			t.Errorf("missing mission id %q", id)
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
		t.Fatal("Clone shares backing memory with source")
	}
}

// Sanity check that EvalContext's SimTime field is wired but not
// currently consumed — guards against accidentally tying behaviour
// to wall-clock during the v0.6.5 scaffold.
func TestEvalIgnoresSimTime(t *testing.T) {
	m := Mission{
		Type:   TypeSOIFlyby,
		Params: Params{PrimaryID: "mars"},
	}
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx1 := EvalContext{PrimaryID: "earth", SimTime: t1}
	ctx2 := EvalContext{PrimaryID: "earth", SimTime: t2}
	if m.Evaluate(ctx1) != m.Evaluate(ctx2) {
		t.Fatal("SimTime should not affect predicate outcome in v0.6.5")
	}
}
