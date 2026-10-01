package planner

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// rendezvousCalibrationRadius / rendezvousCalibrationMu anchor the ADR 0045
// calibration scenario: "two matched circular orbits 1 km apart at
// 500 km" — real-Earth LEO, same fixture family as
// rendezvous_recommend_test.go's r=6.771e6 (400 km) cases, just at the
// ADR's own 500 km altitude. Real Earth mu/radius (muEarth, from
// transfer_test.go) — the ADR's own numbers were argued from
// real-body scale, not this game's stripped-back bodies.
const rendezvousCalibrationRadius = 6.371e6 + 500e3 // 500 km LEO

// TestRecommendRendezvousLadder_TheirOrbit_PhaseOffsetConverges — ADR 0045
// §2's own calibration geometry: near-matched circular 500 km orbits
// with a phase offset (quarter-lap-ish here — the ADR's own "closing a
// quarter-lap offset takes ~1,100 laps" natural-drift anchor), Rendezvous
// Place = "their orbit". Acceptance criterion (#398): a planted row
// must actually produce a small closest approach at the predicted
// time — propagate and check, don't trust the closed form. The row's
// own AchievableCA IS that propagate-and-check (see
// rendezvousLadderCore's doc comment), so this asserts it directly rather
// than re-deriving a second predictor call.
func TestRecommendRendezvousLadder_TheirOrbit_PhaseOffsetConverges(t *testing.T) {
	r := rendezvousCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	// Quarter-lap phase offset — the ADR's own "~1,100 laps to close
	// naturally" anchor scenario.
	chaser := circularStateAtRadius(r, -math.Pi/2, mu)

	ladder, err := RecommendRendezvousLadder(chaser, target, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("RecommendRendezvousLadder err: %v", err)
	}
	if !ladder.MoverIsA {
		t.Fatalf("RendezvousTheirOrbit must burn the active craft (stateA): MoverIsA=false")
	}
	if len(ladder.Rows) == 0 {
		t.Fatalf("expected at least one row")
	}

	sawOk := false
	for _, row := range ladder.Rows {
		if !row.Ok {
			continue
		}
		sawOk = true
		// "small" — a generous few-km bound at 500 km altitude. In
		// practice AchievableCA lands near machine-precision (both the
		// row's Δv solve and its AchievableCA verification use the same
		// closed-form KeplerStep propagation, so there is no
		// Verlet/analytic model mismatch to produce residual here — see
		// rendezvousLadderCore's doc comment); this bound exists to catch
		// a wrong-direction or wrong-frame bug (km-scale), not to size
		// an expected numerical tolerance.
		if row.AchievableCA > 5_000 {
			t.Errorf("laps=%d: AchievableCA=%.0f m, want small (<5 km)", row.Laps, row.AchievableCA)
		}
		if row.TArrival <= 0 {
			t.Errorf("laps=%d: TArrival=%.1f, want > 0", row.Laps, row.TArrival)
		}
		if row.DV <= 0 {
			t.Errorf("laps=%d: DV=%.3f, want > 0", row.Laps, row.DV)
		}
	}
	if !sawOk {
		t.Fatalf("expected at least one Ok row, got none: %+v", ladder.Rows)
	}
}

// TestRecommendRendezvousLadder_TensOfKm_NotOk — review finding (LOW):
// rendezvousAchievableCATolFrac at its old 1% let Ok=true rows through
// whose own propagated AchievableCA ran into the tens of kilometres —
// not a rounding error, but not "an encounter" either, and Ok flows
// straight through PlanRendezvousBurn into the node an Engage commits to.
//
// Reproduces the reviewer's measured scenario: a mildly eccentric
// holder (e in roughly [0.002, 0.01], built the same way as
// TestRendezvousLadderEccentricHolderSolves (#413): two vessels phasing
// on the SAME eccentric orbit (perigee 6771 km / apogee 12000 km,
// e≈0.28) get real rows instead of a refusal. The holder's timing comes
// from Kepler's equation, not a uniform angular sweep (exact only for a
// circle); the 0.1% tolerance gate stays untouched as the backstop.
//
// Asserting a row exists is not enough (ADR 0045's history is full of
// harnesses that checked the wrong thing): each Ok row's advertised
// burn is re-applied to the mover here, both vessels are propagated
// independently to TArrival, and the separation must land inside the
// tolerance. Phase offsets and start points cover perigee, apogee and
// mid-orbit, and both Places (mover A / mover B).
func TestRendezvousLadderEccentricHolderSolves(t *testing.T) {
	mu := muEarth
	rp := 6.771e6
	ra := 12.000e6
	e := (ra - rp) / (ra + rp)
	k := math.Sqrt(1 + e) // eccentricStateAtRadius's periapsis-speed multiplier for this e

	base := eccentricStateAtRadius(rp, 0, k, mu)
	period := orbitalPeriod(physics.StateVector{R: base.R, V: base.V}, mu)
	step := func(s orbital.Vec3State, dt float64) orbital.Vec3State {
		sv, ok := physics.KeplerStep(physics.StateVector{R: s.R, V: s.V}, mu, dt)
		if !ok {
			t.Fatalf("setup: KeplerStep failed dt=%.1f", dt)
		}
		return orbital.Vec3State{R: sv.R, V: sv.V}
	}

	for _, startFrac := range []float64{0, 0.25, 0.5, 0.8} {
		for _, gapFrac := range []float64{1.0 / 6, 1.0 / 3, 0.5, 5.0 / 6} {
			stateA := step(base, startFrac*period)
			stateB := step(stateA, gapFrac*period)
			for _, place := range []RendezvousOrbit{RendezvousTheirOrbit, RendezvousYourOrbit} {
				name := fmt.Sprintf("start=%.2f gap=%.3f %s", startFrac, gapFrac, place)
				ladder, err := RecommendRendezvousLadder(stateA, stateB, bodies.CelestialBody{}, mu, place, 4*3600, -1)
				if err != nil {
					t.Fatalf("%s: err: %v", name, err)
				}
				mover, holder := stateA, stateB
				if !ladder.MoverIsA {
					mover, holder = stateB, stateA
				}
				okRows := 0
				for _, row := range ladder.Rows {
					if !row.Ok {
						continue
					}
					okRows++
					// Independent propagation of the ADVERTISED burn.
					v0 := mover.V
					burned := orbital.Vec3State{R: mover.R, V: v0.Add(row.BurnDir.Scale(row.DV))}
					m := step(burned, row.TArrival)
					h := step(holder, row.TArrival)
					ca := m.R.Sub(h.R).Norm()
					if ca > rendezvousAchievableCATolFrac*mover.R.Norm() {
						t.Errorf("%s laps=%d: advertised row Ok but re-propagated separation = %.0f m (> %.0f m)",
							name, row.Laps, ca, rendezvousAchievableCATolFrac*mover.R.Norm())
					}
				}
				if okRows == 0 {
					t.Errorf("%s: no Ok rows on a same-orbit eccentric pair; rows=%+v", name, ladder.Rows)
				}
			}
		}
	}
}

// TestRecommendRendezvousLadder_MoreLapsCostsLess — the doctrine's own
// wait-vs-Δv lever (ADR 0045 §2's ladder example: 2 laps/630 m/s vs
// 5 laps/250 m/s vs 20 laps/60 m/s — monotonically cheaper with more
// laps). Exact figures aren't reproduced (they depend on assumptions
// the ADR doesn't fully pin — see PR description) but the monotonic
// trend is a geometry-independent property of the solver and is what
// makes the ladder a real trade rather than a fixed price.
func TestRecommendRendezvousLadder_MoreLapsCostsLess(t *testing.T) {
	r := rendezvousCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -math.Pi/2, mu)

	ladder, err := RecommendRendezvousLadder(chaser, target, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	var lastDV float64 = math.Inf(1)
	var lastLaps int
	nCompared := 0
	for _, row := range ladder.Rows {
		if !row.Ok {
			continue
		}
		if lastLaps > 0 && row.Laps > lastLaps {
			if row.DV > lastDV {
				t.Errorf("laps %d→%d: DV grew %.1f→%.1f m/s, want non-increasing", lastLaps, row.Laps, lastDV, row.DV)
			}
			nCompared++
		}
		lastDV, lastLaps = row.DV, row.Laps
	}
	if nCompared == 0 {
		t.Fatalf("not enough Ok rows to compare a trend: %+v", ladder.Rows)
	}
}

// TestRecommendRendezvousLadder_YourOrbit_PlansForPartner — #398
// acceptance: "meet on your orbit" must produce a plan for the
// PARTNER (stateB), never for the active craft (stateA) — MoverIsA
// must be false, and the row's burn must be sized against stateB's
// own current orbit/period, not stateA's.
func TestRecommendRendezvousLadder_YourOrbit_PlansForPartner(t *testing.T) {
	r := rendezvousCalibrationRadius
	mu := muEarth
	active := circularStateAtRadius(r, 0, mu)
	partner := circularStateAtRadius(r, -math.Pi/2, mu)

	ladder, err := RecommendRendezvousLadder(active, partner, bodies.CelestialBody{}, mu, RendezvousYourOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ladder.MoverIsA {
		t.Fatalf("RendezvousYourOrbit must plan for the partner (stateB): MoverIsA=true")
	}
	sawOk := false
	for _, row := range ladder.Rows {
		if row.Ok {
			sawOk = true
		}
	}
	if !sawOk {
		t.Fatalf("expected at least one Ok row for the partner's plan: %+v", ladder.Rows)
	}

	// Cross-check: RendezvousTheirOrbit on the SAME two states (active as
	// mover) should generally differ from RendezvousYourOrbit's rows
	// (partner as mover) — confirms the roles actually swapped rather
	// than the solver silently always burning stateA.
	theirs, err := RecommendRendezvousLadder(active, partner, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("err (their orbit cross-check): %v", err)
	}
	if !theirs.MoverIsA {
		t.Fatalf("RendezvousTheirOrbit must burn the active craft: MoverIsA=false")
	}
}

// TestRecommendRendezvousLadder_Unaffordable_ReturnedNotDropped — #398
// acceptance: an unaffordable row is returned AND marked, not hidden
// — "the trade stays visible" (ADR 0045 §2).
func TestRecommendRendezvousLadder_Unaffordable_ReturnedNotDropped(t *testing.T) {
	r := rendezvousCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -math.Pi/2, mu)

	// A near-zero Δv budget: every row's burn will exceed it.
	ladder, err := RecommendRendezvousLadder(chaser, target, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, 0.001)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ladder.Rows) == 0 {
		t.Fatalf("expected rows even though none are affordable")
	}
	sawUnaffordable := false
	for _, row := range ladder.Rows {
		if row.DV <= 0 {
			continue // a convergence-failure row, not a pricing row
		}
		if row.Ok {
			t.Errorf("laps=%d: DV=%.1f Ok=true with a 0.001 m/s budget", row.Laps, row.DV)
			continue
		}
		if row.Reason == "unaffordable" {
			sawUnaffordable = true
		}
	}
	if !sawUnaffordable {
		t.Fatalf("expected at least one row marked unaffordable, got: %+v", ladder.Rows)
	}
}

// TestRecommendRendezvousLadder_UnsafePeriapsis_Rejected — #398 acceptance:
// a row that would deorbit the mover is rejected by the (reused)
// periapsis-safety gate. The tangential model (rendezvousLadderCore)
// tries both "catch up by dropping" and "fall back by raising" per
// lap count and prefers whichever is safe — a raise leaves periapsis
// AT the burn point (r0) for a near-circular start, so it's always
// periapsis-safe UNLESS r0 itself already sits below the primary's
// surface+50km floor. Starting the mover (and holder) already inside
// that floor (30 km altitude, well under it) forces every row —
// raise or drop — to fail: there is no altitude "fall back" to when
// the starting point itself is already unsafe.
func TestRecommendRendezvousLadder_UnsafePeriapsis_Rejected(t *testing.T) {
	primary := bodies.CelestialBody{MeanRadius: 6378} // km — Earth-radius primary, RadiusMeters() > 0
	mu := muEarth
	r := 6.378e6 + 30e3 // 30 km altitude — under the surface+50km floor
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -0.1*math.Pi, mu)

	ladder, err := RecommendRendezvousLadder(chaser, target, primary, mu, RendezvousTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ladder.Rows) == 0 {
		t.Fatalf("expected rows")
	}
	for _, row := range ladder.Rows {
		if row.Reason != "burn drops periapsis unsafely" || row.Ok {
			t.Errorf("laps=%d: Ok=%v Reason=%q, want Ok=false Reason=\"burn drops periapsis unsafely\"", row.Laps, row.Ok, row.Reason)
		}
	}
}

// TestOrbitSafetyGate_DirectRejection unit-tests orbitSafetyGate
// directly (rather than relying on a Lambert geometry happening to
// trip it) — a burn that puts periapsis well below the primary's
// surface must be rejected regardless of which caller reuses the gate.
func TestOrbitSafetyGate_DirectRejection(t *testing.T) {
	primary := bodies.CelestialBody{MeanRadius: 6378} // km
	mu := muEarth
	r := rendezvousCalibrationRadius
	pre := circularStateAtRadius(r, 0, mu)
	// A hard retrograde burn at the current point drops periapsis to
	// near zero (a near-radial-fall orbit).
	unsafeV := pre.V.Scale(0.2)
	if orbitSafetyGate(pre.R, pre.V, pre.R, unsafeV, primary, mu) {
		t.Fatalf("expected orbitSafetyGate to reject a burn dropping periapsis near zero")
	}
	// The unperturbed state must pass (post == pre).
	if !orbitSafetyGate(pre.R, pre.V, pre.R, pre.V, primary, mu) {
		t.Fatalf("expected orbitSafetyGate to accept a no-op burn")
	}
}

// TestRecommendRendezvousLadder_NonCoplanarRefused — #398 out-of-scope
// note: "this slice assumes coplanar and refuses otherwise, naming
// [I]." A target inclined well past rendezvousPlaneTolDeg must refuse
// with ErrRendezvousPlaneMismatch, not silently attempt a 3D Lambert fit.
func TestRecommendRendezvousLadder_NonCoplanarRefused(t *testing.T) {
	r := rendezvousCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := inclinedCircularState(r, -math.Pi/2, 30*math.Pi/180, mu) // 30° plane tilt

	_, err := RecommendRendezvousLadder(chaser, target, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1)
	if !errors.Is(err, ErrRendezvousPlaneMismatch) {
		t.Fatalf("err = %v, want ErrRendezvousPlaneMismatch", err)
	}
}

// TestRecommendRendezvousLadder_Crossing_CoincidentOrbitsRefuse: a matched
// circular pair shares EVERY point, so there is no single crossing to coast
// to; it refuses as no-crossing (their orbit / your orbit are the tools), and
// never returns rows or the retired not-implemented decoy (PR #412 / #415).
func TestRecommendRendezvousLadder_Crossing_CoincidentOrbitsRefuse(t *testing.T) {
	r := rendezvousCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -0.5*math.Pi/180, mu)

	ladder, err := RecommendRendezvousLadder(chaser, target, bodies.CelestialBody{}, mu, RendezvousCrossing, 4*3600, -1)
	if !errors.Is(err, ErrRendezvousNoCrossing) {
		t.Fatalf("err = %v, want ErrRendezvousNoCrossing", err)
	}
	if len(ladder.Rows) != 0 {
		t.Fatalf("expected zero rows on a structural refusal, got %d: %+v", len(ladder.Rows), ladder.Rows)
	}
}

// TestRecommendRendezvousLadder_Crossing_InvalidHorizonRefused — the input
// guard ahead of the existence check: a non-positive
// crossingSearchHorizon is a caller bug (this must always be
// rendezvousCommitHorizonSec, per this function's own doc comment), not
// something RendezvousCrossing should try to interpret as "no crossing".
func TestRecommendRendezvousLadder_Crossing_InvalidHorizonRefused(t *testing.T) {
	r := rendezvousCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -0.5*math.Pi/180, mu)

	_, err := RecommendRendezvousLadder(chaser, target, bodies.CelestialBody{}, mu, RendezvousCrossing, 0, -1)
	if !errors.Is(err, errRendezvousInvalidInput) {
		t.Fatalf("err = %v, want errRendezvousInvalidInput", err)
	}
}

// TestRecommendRendezvousLadder_ShapeMismatchNowYieldsPlan — #398
// acceptance: the #290 mismatch geometry (TestRecommendRendezvousNudge_
// ShapeMismatch's own fixture — a sharply eccentric chaser, e≈0.69,
// against a circular target at the same periapsis radius) is exactly
// the case K's Shape-Match Gate used to refuse outright. The Rendezvous
// Planner never had that gate (it doesn't use K's single-axis
// projection, so it isn't exposed to the failure mode the gate
// existed to prevent) — it must produce a real plan for this geometry.
func TestRecommendRendezvousLadder_ShapeMismatchNowYieldsPlan(t *testing.T) {
	r := 6.771e6
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := eccentricStateAtRadius(r, -0.5*math.Pi/180, 1.3, mu) // e≈0.69, #290's geometry

	ladder, err := RecommendRendezvousLadder(chaser, target, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	sawOk := false
	for _, row := range ladder.Rows {
		if row.Ok {
			sawOk = true
		}
	}
	if !sawOk {
		t.Fatalf("expected the Rendezvous Planner to produce a real plan for the #290 mismatch geometry, got: %+v", ladder.Rows)
	}
}

// rendezvousApplyBurn advances (moverState, holderState) forward by
// row.TArrival, applying row's burn to mover at t=0 first — via
// physics.KeplerStep, the SAME analytic (closed-form) two-body
// propagation rendezvousLadderCore itself uses to derive
// row.AchievableCA. This is NOT an independent check against a
// numerical integrator (Verlet/RK4) or the live game's actual coast —
// see TestRendezvousLadder_IterateSelfConsistent's doc comment for what
// that limits this helper to proving. Used to chain repeated Rendezvous
// Planner calls in that test.
func rendezvousApplyBurn(moverState orbital.Vec3State, row RendezvousBurnOption, holderState orbital.Vec3State, mu float64) (orbital.Vec3State, orbital.Vec3State) {
	burned := orbital.Vec3State{R: moverState.R, V: moverState.V.Add(row.BurnDir.Scale(row.DV))}
	mSV, mok := physics.KeplerStep(physics.StateVector{R: burned.R, V: burned.V}, mu, row.TArrival)
	hSV, hok := physics.KeplerStep(physics.StateVector{R: holderState.R, V: holderState.V}, mu, row.TArrival)
	if !mok || !hok {
		return burned, holderState
	}
	return orbital.Vec3State{R: mSV.R, V: mSV.V}, orbital.Vec3State{R: hSV.R, V: hSV.V}
}

// TestRendezvousLadder_IterateSelfConsistent repeatedly opens the Rendezvous
// Planner "the way a pilot mashing the key would" on the #290 mismatch
// geometry (a sharply eccentric chaser against a circular target),
// takes the cheapest Ok row each time, applies it via rendezvousApplyBurn,
// and re-opens the ladder from the resulting state. It asserts the
// solver's own predicted AchievableCA stays small across every
// iteration.
//
// SCOPE, READ CAREFULLY: this is a SELF-CONSISTENCY check on the
// analytic model, not a live-integrator anti-divergence proof.
// rendezvousApplyBurn propagates with physics.KeplerStep — the exact same
// closed-form two-body model rendezvousLadderCore itself uses internally
// to compute AchievableCA. Prediction and "flight" are the same
// equations evaluated twice, so a mismatch between them is structurally
// impossible; the near-zero CA sequence this test records shows the
// solver doesn't contradict itself between calls, NOT that flying the
// plan under the real integrator (Verlet/RK4, SOI handling — what
// #290's own 577 km → 1,110 km divergence was actually measured
// against) would converge. #398's Shape-Match Gate removal was
// reverted (PR #405 review) specifically because this test cannot
// stand in for that missing live-integrator proof; that proof is a
// separate follow-up slice.
func TestRendezvousLadder_IterateSelfConsistent(t *testing.T) {
	r := 6.771e6
	mu := muEarth
	primary := bodies.CelestialBody{}
	target := circularStateAtRadius(r, 0, mu)
	chaser := eccentricStateAtRadius(r, -0.5*math.Pi/180, 1.3, mu) // e≈0.69, #290's geometry

	const iterations = 5
	cas := make([]float64, 0, iterations)

	for i := 0; i < iterations; i++ {
		ladder, err := RecommendRendezvousLadder(chaser, target, primary, mu, RendezvousTheirOrbit, 4*3600, -1)
		if err != nil {
			t.Fatalf("iteration %d: RecommendRendezvousLadder err: %v", i, err)
		}
		var best RendezvousBurnOption
		found := false
		for _, row := range ladder.Rows {
			if !row.Ok {
				continue
			}
			if !found || row.DV < best.DV {
				best, found = row, true
			}
		}
		if !found {
			t.Fatalf("iteration %d: no usable row: %+v", i, ladder.Rows)
		}
		cas = append(cas, best.AchievableCA)
		chaser, target = rendezvousApplyBurn(chaser, best, target, mu)
	}

	t.Logf("CA sequence across %d iterations (self-consistency, analytic model only): %v", iterations, cas)

	// Self-consistency bound, not an anti-divergence proof (see the
	// test's own doc comment): every iteration's solver-predicted
	// AchievableCA should stay near the closed-form's own residual
	// noise floor, since rendezvousApplyBurn flies each burn with the
	// exact model the solver used to predict it. A bound this loose
	// (50 km, vs. the ~µm residuals actually observed) exists only to
	// catch a gross logic error in the iterate/re-solve loop itself
	// (e.g. feeding the wrong state into the next call) — it says
	// nothing about live-integrator behavior.
	const selfConsistencyBoundM = 50_000.0 // 50 km
	for i, ca := range cas {
		if ca > selfConsistencyBoundM {
			t.Errorf("iteration %d: AchievableCA=%.0f m exceeds the self-consistency bound (%.0f m) — sequence: %v", i, ca, selfConsistencyBoundM, cas)
		}
	}
}

// A holder already at r0 must wait ~0, however the atan2 angle lands:
// exactly 0, a hair above, or a hair below (which normalises to ~2π and
// used to read as one full period of waiting).
func TestHolderTimeToPhaseWrapAroundIsNotAPeriodLate(t *testing.T) {
	mu := muEarth
	rp, ra := 6.771e6, 12.0e6
	e := (ra - rp) / (ra + rp)
	k := math.Sqrt(1 + e)
	for _, nuFrac := range []float64{0, 0.3, 0.5, 0.9} {
		base := eccentricStateAtRadius(rp, 0, k, mu)
		period := orbitalPeriod(physics.StateVector{R: base.R, V: base.V}, mu)
		sv, ok := physics.KeplerStep(physics.StateVector{R: base.R, V: base.V}, mu, nuFrac*period)
		if !ok {
			t.Fatal("KeplerStep")
		}
		h := orbital.Vec3State{R: sv.R, V: sv.V}
		hEl := orbital.ElementsFromState(h.R, h.V, mu)
		for _, dPhi := range []float64{0, 1e-12, 2*math.Pi - 1e-12} {
			if got := holderTimeToPhase(h, hEl, mu, dPhi, period); got > 1 {
				t.Errorf("nu=%.1f of period, dPhi0=%g: t0 = %.1f s, want ~0 (period %.0f s)", nuFrac, dPhi, got, period)
			}
		}
		// A real quarter-turn must still take real time.
		if got := holderTimeToPhase(h, hEl, mu, math.Pi/2, period); got <= 1 || got >= period {
			t.Errorf("quarter-turn t0 = %.1f s, want within (1, %.0f)", got, period)
		}
	}
}

// rendezvousFlownCA applies the row's burn to the mover at the row's own
// burn epoch (both states Kepler-coasted TBurn, no burn yet) and flies both
// to TArrival with the independent Kepler stepper. Returns the separation.
// This does NOT use the row's AchievableCA, so a solver that agrees only
// with itself fails here.
func rendezvousFlownCA(t *testing.T, mover, holder orbital.Vec3State, row RendezvousBurnOption, mu float64) float64 {
	t.Helper()
	m, ok1 := physics.KeplerStep(physics.StateVector{R: mover.R, V: mover.V}, mu, row.TBurn)
	h, ok2 := physics.KeplerStep(physics.StateVector{R: holder.R, V: holder.V}, mu, row.TBurn)
	if !ok1 || !ok2 {
		t.Fatal("coast to burn failed")
	}
	m.V = m.V.Add(row.BurnDir.Scale(row.DV))
	m2, ok3 := physics.KeplerStep(m, mu, row.FlightSec())
	h2, ok4 := physics.KeplerStep(h, mu, row.FlightSec())
	if !ok3 || !ok4 {
		t.Fatal("flight to rendezvous failed")
	}
	return m2.R.Sub(h2.R).Norm()
}

// G4 Q2: every row carries its own burn time. With a 5 min lead each row
// burns 300 s out, wait counts from now, and the burn flown at THAT epoch
// (not now) closes the gap.
func TestRecommendRendezvousLadderAfter_RowsCarryTBurn(t *testing.T) {
	r := rendezvousCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -math.Pi/2, mu)
	const lead = 300.0

	ladder, err := RecommendRendezvousLadderAfter(chaser, target, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1, lead)
	if err != nil {
		t.Fatal(err)
	}
	ok := 0
	for _, row := range ladder.Rows {
		if !row.Ok {
			continue
		}
		ok++
		if row.TBurn != lead {
			t.Errorf("laps=%d TBurn=%.1f want %.1f", row.Laps, row.TBurn, lead)
		}
		if row.TArrival <= row.TBurn {
			t.Errorf("laps=%d TArrival=%.1f must count from now and exceed TBurn=%.1f", row.Laps, row.TArrival, row.TBurn)
		}
		if ca := rendezvousFlownCA(t, chaser, target, row, mu); ca > 5_000 {
			t.Errorf("laps=%d flown CA=%.0f m, want <5 km (row burn at its own TBurn)", row.Laps, ca)
		}
	}
	if ok == 0 {
		t.Fatalf("no Ok rows: %+v", ladder.Rows)
	}
}

// ellipseState builds a coplanar (XY) state at true anomaly nu on an orbit
// with periapsis rp, eccentricity e, periapsis direction angle omega.
func ellipseState(rp, e, omega, nu, mu float64) orbital.Vec3State {
	p := rp * (1 + e)
	r := p / (1 + e*math.Cos(nu))
	vr := math.Sqrt(mu/p) * e * math.Sin(nu)
	vt := math.Sqrt(mu/p) * (1 + e*math.Cos(nu))
	th := omega + nu
	c, s := math.Cos(th), math.Sin(th)
	return orbital.Vec3State{
		R: orbital.Vec3{X: r * c, Y: r * s},
		V: orbital.Vec3{X: vr*c - vt*s, Y: vr*s + vt*c},
	}
}

// distanceToOrbit is the minimum distance from point pos to the orbit of
// state s, sampled finely over one period.
func distanceToOrbit(s orbital.Vec3State, pos orbital.Vec3, mu float64) float64 {
	P := orbitalPeriod(physics.StateVector{R: s.R, V: s.V}, mu)
	best := math.Inf(1)
	const n = 40000
	for i := 0; i < n; i++ {
		st, ok := physics.KeplerStep(physics.StateVector{R: s.R, V: s.V}, mu, P*float64(i)/n)
		if !ok {
			continue
		}
		if d := st.R.Sub(pos).Norm(); d < best {
			best = d
		}
	}
	return best
}

// G4 Q3 / #416: the crossing coasts to the TRUE intersection of the two
// coplanar orbits and burns there. Two different-shape orbits (an ellipse
// and a circle that cross it) with arbitrary phasing.
func TestRecommendRendezvousLadderAfter_Crossing_BurnsAtTrueIntersection(t *testing.T) {
	mu := muEarth
	re := 6.371e6
	mover := ellipseState(re+400e3, 0.12, 0.3, 1.1, mu) // perigee 400 km, apogee ~1900 km
	holder := circularStateAtRadius(re+1100e3, 2.2, mu) // circle between peri and apo
	const lead = 300.0

	ladder, err := RecommendRendezvousLadderAfter(mover, holder, bodies.CelestialBody{}, mu, RendezvousCrossing, 4*3600, -1, lead)
	if err != nil {
		t.Fatalf("crossing refused: %v", err)
	}
	if !ladder.MoverIsA {
		t.Errorf("crossing: active craft burns")
	}
	oks := 0
	for _, row := range ladder.Rows {
		if !row.Ok {
			continue
		}
		oks++
		if row.TBurn < lead-1e-6 {
			t.Errorf("laps=%d TBurn=%.1f earlier than the lead %.1f", row.Laps, row.TBurn, lead)
		}
		// The burn point must sit ON the holder's orbit (an intersection).
		m, _ := physics.KeplerStep(physics.StateVector{R: mover.R, V: mover.V}, mu, row.TBurn)
		if d := distanceToOrbit(holder, m.R, mu); d > 5_000 {
			t.Errorf("laps=%d burn point is %.0f m from the holder's orbit, want an intersection (<5 km)", row.Laps, d)
		}
		if ca := rendezvousFlownCA(t, mover, holder, row, mu); ca > 7_000 {
			t.Errorf("laps=%d flown CA=%.0f m, want small", row.Laps, ca)
		}
	}
	if oks == 0 {
		t.Fatalf("no Ok crossing rows: %+v", ladder.Rows)
	}
}

// Orbits that never cross (concentric, different radius) refuse as
// no-crossing, not as the old not-implemented stub.
func TestRecommendRendezvousLadderAfter_Crossing_NoIntersectionRefuses(t *testing.T) {
	mu := muEarth
	a := circularStateAtRadius(rendezvousCalibrationRadius, 0, mu)
	b := circularStateAtRadius(rendezvousCalibrationRadius+300e3, 1, mu)
	_, err := RecommendRendezvousLadderAfter(a, b, bodies.CelestialBody{}, mu, RendezvousCrossing, 4*3600, -1, 300)
	if !errors.Is(err, ErrRendezvousNoCrossing) {
		t.Fatalf("err=%v want ErrRendezvousNoCrossing", err)
	}
}
