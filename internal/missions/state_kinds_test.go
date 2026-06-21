package missions

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// v0.21 Slice 2 (ADR 0025 §3) — the five new instantaneous state-objective
// kinds. Each test exercises pass / not-yet / frame-mismatch per the
// cycle-plan done-criteria. Reuses circularState + the earth/moon
// constants from missions_test.go (same package).

// --- reach_altitude {primary, min_altitude_m} ---

func TestReachAltitudePassesAtOrAboveTarget(t *testing.T) {
	o := Objective{
		Kind:   KindReachAltitude,
		Params: Params{PrimaryID: "earth", MinAltitudeM: 500e3},
	}
	// 1000 km altitude — comfortably above the 500 km floor.
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 1000e3),
	}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("1000 km vs 500 km floor: got %v, want Passed", got)
	}
}

func TestReachAltitudeInProgressBelowTarget(t *testing.T) {
	o := Objective{
		Kind:   KindReachAltitude,
		Params: Params{PrimaryID: "earth", MinAltitudeM: 500e3},
	}
	// 300 km — short of the 500 km floor.
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 300e3),
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("300 km vs 500 km floor: got %v, want InProgress", got)
	}
}

func TestReachAltitudeFrameMismatch(t *testing.T) {
	o := Objective{
		Kind:   KindReachAltitude,
		Params: Params{PrimaryID: "earth", MinAltitudeM: 500e3},
	}
	// Plenty high, but around the Moon — wrong primary, predicate dormant.
	ctx := EvalContext{
		PrimaryID:      "moon",
		PrimaryRadiusM: moonRadius,
		PrimaryMu:      moonMu,
		State:          circularState(moonRadius, moonMu, 1000e3),
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("right altitude wrong primary: got %v, want InProgress", got)
	}
}

// --- return_to_body {primary} : captured-or-landed ---

func TestReturnToBodyPassesOnBoundOrbit(t *testing.T) {
	o := Objective{Kind: KindReturnToBody, Params: Params{PrimaryID: "earth"}}
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 300e3),
	}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("bound earth orbit: got %v, want Passed", got)
	}
}

func TestReturnToBodyPassesWhenLanded(t *testing.T) {
	o := Objective{Kind: KindReturnToBody, Params: Params{PrimaryID: "earth"}}
	// Landed back on Earth — passes even though the (pinned, surface) state
	// vector isn't a bound orbit.
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		Landed:         true,
		State:          physics.StateVector{R: orbital.Vec3{X: earthRadius}, M: 1000},
	}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("landed on earth: got %v, want Passed", got)
	}
}

func TestReturnToBodyInProgressOnHyperbolicPassthrough(t *testing.T) {
	o := Objective{Kind: KindReturnToBody, Params: Params{PrimaryID: "earth"}}
	// In Earth's SOI but on a hyperbolic flyby (not captured, not landed) —
	// a whizz-through is not a return.
	radius := earthRadius + 500e3
	vEsc := math.Sqrt(2 * earthMu / radius)
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State: physics.StateVector{
			R: orbital.Vec3{X: radius},
			V: orbital.Vec3{Y: vEsc * 1.5},
			M: 1000,
		},
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("hyperbolic passthrough: got %v, want InProgress", got)
	}
}

func TestReturnToBodyFrameMismatch(t *testing.T) {
	o := Objective{Kind: KindReturnToBody, Params: Params{PrimaryID: "earth"}}
	// Bound around the Moon — right shape, wrong body.
	ctx := EvalContext{
		PrimaryID:      "moon",
		PrimaryRadiusM: moonRadius,
		PrimaryMu:      moonMu,
		State:          circularState(moonRadius, moonMu, 100e3),
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("bound around moon, want earth: got %v, want InProgress", got)
	}
}

// --- land_at_body {primary, optional site lat/lon + radius_m} ---

// landedCtx builds a landed-on-earth EvalContext at the given surface
// coordinates.
func landedCtx(latDeg, lonDeg float64) EvalContext {
	return EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		Landed:         true,
		SurfaceLatDeg:  latDeg,
		SurfaceLonDeg:  lonDeg,
		State:          physics.StateVector{R: orbital.Vec3{X: earthRadius}, M: 1000},
	}
}

func TestLandAtBodyPassesLandedAnywhereWithoutSite(t *testing.T) {
	// No site constraint (SiteRadiusM == 0): landing anywhere on Earth passes.
	o := Objective{Kind: KindLandAtBody, Params: Params{PrimaryID: "earth"}}
	if got := o.Evaluate(landedCtx(12, 34)); got != Passed {
		t.Fatalf("landed anywhere, no site: got %v, want Passed", got)
	}
}

func TestLandAtBodyPassesInsideSite(t *testing.T) {
	// KSC site, 100 km radius; craft set down essentially on the pad.
	o := Objective{Kind: KindLandAtBody, Params: Params{
		PrimaryID: "earth", SiteLatDeg: 28.6, SiteLonDeg: -80.6, SiteRadiusM: 100e3,
	}}
	if got := o.Evaluate(landedCtx(28.61, -80.59)); got != Passed {
		t.Fatalf("landed inside site radius: got %v, want Passed", got)
	}
}

func TestLandAtBodyInProgressNotLanded(t *testing.T) {
	o := Objective{Kind: KindLandAtBody, Params: Params{PrimaryID: "earth"}}
	ctx := landedCtx(12, 34)
	ctx.Landed = false
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("not landed: got %v, want InProgress", got)
	}
}

func TestLandAtBodyInProgressOutsideSite(t *testing.T) {
	// KSC site, 100 km radius; craft set down at the equator/prime-meridian
	// — thousands of km away.
	o := Objective{Kind: KindLandAtBody, Params: Params{
		PrimaryID: "earth", SiteLatDeg: 28.6, SiteLonDeg: -80.6, SiteRadiusM: 100e3,
	}}
	if got := o.Evaluate(landedCtx(0, 0)); got != InProgress {
		t.Fatalf("landed outside site radius: got %v, want InProgress", got)
	}
}

func TestLandAtBodyFrameMismatch(t *testing.T) {
	// Landed, but the objective is for the Moon while the craft is on Earth.
	o := Objective{Kind: KindLandAtBody, Params: Params{PrimaryID: "moon"}}
	if got := o.Evaluate(landedCtx(0, 0)); got != InProgress {
		t.Fatalf("landed on earth, objective moon: got %v, want InProgress", got)
	}
}

// TestGreatCircleDistance — sanity-check the haversine helper against the
// classic ~111 km per degree of latitude on Earth.
func TestGreatCircleDistance(t *testing.T) {
	d := greatCircleDistanceM(0, 0, 1, 0, earthRadius)
	want := earthRadius * math.Pi / 180 // 1° in radians × R
	if math.Abs(d-want) > 1 {
		t.Fatalf("1° of latitude: got %.1f m, want ~%.1f m", d, want)
	}
}

// --- rendezvous {range_m, rel_speed_ms} : vs the targeted craft ---

func TestRendezvousPassesWithinRangeAndSpeed(t *testing.T) {
	o := Objective{Kind: KindRendezvous, Params: Params{RangeM: 100, RelSpeedMs: 0.5}}
	ctx := EvalContext{HasTargetCraft: true, TargetRangeM: 80, TargetRelSpeedMs: 0.3}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("80 m / 0.3 m/s vs 100 m / 0.5 m/s: got %v, want Passed", got)
	}
}

func TestRendezvousInProgressTooFar(t *testing.T) {
	o := Objective{Kind: KindRendezvous, Params: Params{RangeM: 100, RelSpeedMs: 0.5}}
	ctx := EvalContext{HasTargetCraft: true, TargetRangeM: 200, TargetRelSpeedMs: 0.1}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("200 m (too far): got %v, want InProgress", got)
	}
}

func TestRendezvousInProgressTooFast(t *testing.T) {
	o := Objective{Kind: KindRendezvous, Params: Params{RangeM: 100, RelSpeedMs: 0.5}}
	ctx := EvalContext{HasTargetCraft: true, TargetRangeM: 50, TargetRelSpeedMs: 2.0}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("2.0 m/s (too fast): got %v, want InProgress", got)
	}
}

func TestRendezvousInProgressNoTarget(t *testing.T) {
	o := Objective{Kind: KindRendezvous, Params: Params{RangeM: 100, RelSpeedMs: 0.5}}
	// No craft targeted — nothing to rendezvous with (frame-mismatch analogue).
	ctx := EvalContext{HasTargetCraft: false, TargetRangeM: 0, TargetRelSpeedMs: 0}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("no target craft: got %v, want InProgress", got)
	}
}

// --- dock {} : active craft is a docked composite ---

func TestDockPassesWhenDocked(t *testing.T) {
	o := Objective{Kind: KindDock}
	if got := o.Evaluate(EvalContext{Docked: true}); got != Passed {
		t.Fatalf("docked composite: got %v, want Passed", got)
	}
}

func TestDockInProgressWhenUndocked(t *testing.T) {
	o := Objective{Kind: KindDock}
	if got := o.Evaluate(EvalContext{Docked: false}); got != InProgress {
		t.Fatalf("not docked: got %v, want InProgress", got)
	}
}
