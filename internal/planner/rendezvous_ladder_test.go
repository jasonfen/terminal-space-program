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

// TestRecommendRendezvousLadder_Crossing_AlwaysRefuses — review round 2
// regression revert: PR #412's attempt to anchor rendezvousLadderCore's
// solve at the natural crossing instant (tCA) produced rows whose
// DV/BurnDir were only correct for a burn executed AT tCA, while
// internal/sim.PlanRendezvousBurn always plants the resulting node to
// fire at TriggerTime = now + a slew lead — not at tCA. The two times
// coincide only by chance, so the planted burn routinely missed by
// megametres (see ErrRendezvousCrossingNotImplemented's doc comment for
// the measured numbers, and internal/sim/rendezvous_burn_test.go for the
// sim-layer regression test against the actual plant path). Reverted
// rather than fixed forward: RendezvousCrossing now refuses
// unconditionally — ErrRendezvousNoCrossing when no natural crossing
// exists within the search horizon, ErrRendezvousCrossingNotImplemented
// when one does but there is still no solver for it. Neither path ever
// returns a ladder with rows.
//
// This fixture (a small phase offset on matched circular orbits) is
// exactly the case that used to legitimately produce a plantable
// RendezvousCrossing row (the natural crossing is ~"now" there) — the
// case a partial fix could most easily miss.
func TestRecommendRendezvousLadder_Crossing_AlwaysRefuses(t *testing.T) {
	r := rendezvousCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -0.5*math.Pi/180, mu) // small offset — NextClosestApproach converges, a natural crossing exists

	ladder, err := RecommendRendezvousLadder(chaser, target, bodies.CelestialBody{}, mu, RendezvousCrossing, 4*3600, -1)
	if !errors.Is(err, ErrRendezvousCrossingNotImplemented) {
		t.Fatalf("err = %v, want ErrRendezvousCrossingNotImplemented (a natural crossing exists here, so this must be the not-implemented refusal, not ErrRendezvousNoCrossing)", err)
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
