package planner

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// meetingCalibrationRadius / meetingCalibrationMu anchor the ADR 0045
// calibration scenario: "two matched circular orbits 1 km apart at
// 500 km" — real-Earth LEO, same fixture family as
// rendezvous_recommend_test.go's r=6.771e6 (400 km) cases, just at the
// ADR's own 500 km altitude. Real Earth mu/radius (muEarth, from
// transfer_test.go) — the ADR's own numbers were argued from
// real-body scale, not this game's stripped-back bodies.
const meetingCalibrationRadius = 6.371e6 + 500e3 // 500 km LEO

// TestRecommendMeetingLadder_TheirOrbit_PhaseOffsetConverges — ADR 0045
// §2's own calibration geometry: near-matched circular 500 km orbits
// with a phase offset (quarter-lap-ish here — the ADR's own "closing a
// quarter-lap offset takes ~1,100 laps" natural-drift anchor), Meeting
// Place = "their orbit". Acceptance criterion (#398): a planted row
// must actually produce a small closest approach at the predicted
// time — propagate and check, don't trust the closed form. The row's
// own AchievableCA IS that propagate-and-check (see
// meetingLadderCore's doc comment), so this asserts it directly rather
// than re-deriving a second predictor call.
func TestRecommendMeetingLadder_TheirOrbit_PhaseOffsetConverges(t *testing.T) {
	r := meetingCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	// Quarter-lap phase offset — the ADR's own "~1,100 laps to close
	// naturally" anchor scenario.
	chaser := circularStateAtRadius(r, -math.Pi/2, mu)

	ladder, err := RecommendMeetingLadder(chaser, target, bodies.CelestialBody{}, mu, MeetingTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("RecommendMeetingLadder err: %v", err)
	}
	if !ladder.MoverIsA {
		t.Fatalf("MeetingTheirOrbit must burn the active craft (stateA): MoverIsA=false")
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
		// meetingLadderCore's doc comment); this bound exists to catch
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

// TestRecommendMeetingLadder_TensOfKm_NotOk — review finding (LOW):
// meetingAchievableCATolFrac at its old 1% let Ok=true rows through
// whose own propagated AchievableCA ran into the tens of kilometres —
// not a rounding error, but not "an encounter" either, and Ok flows
// straight through PlanMeetingBurn into the node an Engage commits to.
//
// Reproduces the reviewer's measured scenario: a mildly eccentric
// holder (e in roughly [0.002, 0.01], built the same way as
// TestRecommendMeetingLadder_EccentricHolder_UnachievableRefused's own
// fixture family — a co-orbital pair offset by a small fraction of a
// period) a small fraction of a period ahead of the mover, at the 500 km
// LEO calibration radius (r0mag≈6.871e6 m). At the old 1% bound
// (≈68,710 m) every row here reported Ok=true with AchievableCA in
// 12,717-49,422 m; this asserts those specific measured geometries are
// no longer Ok=true at meetingAchievableCATolFrac's current (tighter)
// value.
func TestRecommendMeetingLadder_TensOfKm_NotOk(t *testing.T) {
	r := meetingCalibrationRadius
	mu := muEarth

	cases := []struct {
		k     float64
		fracP float64
	}{
		{1.005, 1.0 / 64},
		{1.008, 1.0 / 64},
		{1.01, 1.0 / 64},
		{1.01, 1.0 / 128},
	}
	for _, c := range cases {
		target := eccentricStateAtRadius(r, 0, c.k, mu)
		period := orbitalPeriod(physics.StateVector{R: target.R, V: target.V}, mu)
		svB, ok := physics.KeplerStep(physics.StateVector{R: target.R, V: target.V}, mu, period*c.fracP)
		if !ok {
			t.Fatalf("setup: KeplerStep failed for k=%.3f fracP=%.5f", c.k, c.fracP)
		}
		chaser := orbital.Vec3State{R: svB.R, V: svB.V}

		ladder, err := RecommendMeetingLadder(chaser, target, bodies.CelestialBody{}, mu, MeetingTheirOrbit, 4*3600, -1)
		if err != nil {
			t.Fatalf("k=%.3f fracP=%.5f: err: %v", c.k, c.fracP, err)
		}
		for _, row := range ladder.Rows {
			if row.AchievableCA <= 0 {
				continue // convergence-failure row, not a pricing row
			}
			if row.AchievableCA < 10_000 {
				continue // below the reviewer's flagged tens-of-km range — not what this test targets
			}
			if row.Ok {
				t.Errorf("k=%.3f fracP=%.5f laps=%d: Ok=true with AchievableCA=%.0f m (tens of km) — must not read as a meeting",
					c.k, c.fracP, row.Laps, row.AchievableCA)
			}
		}
	}
}

// TestRecommendMeetingLadder_EccentricHolder_UnachievableRefused —
// review Finding 1: "Ok never consults AchievableCA." meetingLadderCore's
// t0 derivation sweeps the holder at a uniform angular rate, exact only
// for a circular holder; on an eccentric one the model is wrong and the
// row it prices doesn't actually deliver a meeting, yet every other gate
// (r0 in [holderPeri, holderApo], periapsis safety, affordability) still
// passes because both craft sit on the SAME eccentric orbit.
//
// Reproduces the reviewer's exact measured scenario: a periapsis 6771 km
// / apoapsis 12000 km orbit (e≈0.28), two co-orbital craft 1/6 period
// apart (built by Kepler-stepping one craft's own state forward P/6 —
// the literal "co-orbital, offset in time" geometry, not just a linear
// phase offset). Before the fix every row here reported Ok=true with
// AchievableCA≈6,563,744 m against r0mag≈6.77e6 m (a ~97%-of-orbit
// miss); this asserts no row is allowed to claim Ok=true when its own
// propagated check misses this badly.
func TestRecommendMeetingLadder_EccentricHolder_UnachievableRefused(t *testing.T) {
	mu := muEarth
	rp := 6.771e6
	ra := 12.000e6
	e := (ra - rp) / (ra + rp)
	k := math.Sqrt(1 + e) // eccentricStateAtRadius's periapsis-speed multiplier for this e

	stateA := eccentricStateAtRadius(rp, 0, k, mu)
	period := orbitalPeriod(physics.StateVector{R: stateA.R, V: stateA.V}, mu)
	svB, ok := physics.KeplerStep(physics.StateVector{R: stateA.R, V: stateA.V}, mu, period/6)
	if !ok {
		t.Fatalf("setup: KeplerStep failed advancing the co-orbital partner by P/6")
	}
	stateB := orbital.Vec3State{R: svB.R, V: svB.V}

	ladder, err := RecommendMeetingLadder(stateA, stateB, bodies.CelestialBody{}, mu, MeetingTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("RecommendMeetingLadder err: %v", err)
	}
	if len(ladder.Rows) == 0 {
		t.Fatalf("expected rows")
	}
	for _, row := range ladder.Rows {
		if row.Ok {
			t.Errorf("laps=%d: Ok=true with AchievableCA=%.0f m (r0≈%.0f m) — a %.0f%% miss must not be offered as a meeting",
				row.Laps, row.AchievableCA, rp, 100*row.AchievableCA/rp)
		}
		// The row is still RETURNED with its real numbers (ADR 0045 §2:
		// "unaffordable rows are shown but not plantable") — only Ok
		// flips, AchievableCA itself must stay the honest propagated
		// value, not get hidden or zeroed.
		if row.AchievableCA <= 0 {
			t.Errorf("laps=%d: AchievableCA=%.0f, want the real (large) propagated miss, not zeroed", row.Laps, row.AchievableCA)
		}
	}
}

// TestRecommendMeetingLadder_MoreLapsCostsLess — the doctrine's own
// wait-vs-Δv lever (ADR 0045 §2's ladder example: 2 laps/630 m/s vs
// 5 laps/250 m/s vs 20 laps/60 m/s — monotonically cheaper with more
// laps). Exact figures aren't reproduced (they depend on assumptions
// the ADR doesn't fully pin — see PR description) but the monotonic
// trend is a geometry-independent property of the solver and is what
// makes the ladder a real trade rather than a fixed price.
func TestRecommendMeetingLadder_MoreLapsCostsLess(t *testing.T) {
	r := meetingCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -math.Pi/2, mu)

	ladder, err := RecommendMeetingLadder(chaser, target, bodies.CelestialBody{}, mu, MeetingTheirOrbit, 4*3600, -1)
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

// TestRecommendMeetingLadder_YourOrbit_PlansForPartner — #398
// acceptance: "meet on your orbit" must produce a plan for the
// PARTNER (stateB), never for the active craft (stateA) — MoverIsA
// must be false, and the row's burn must be sized against stateB's
// own current orbit/period, not stateA's.
func TestRecommendMeetingLadder_YourOrbit_PlansForPartner(t *testing.T) {
	r := meetingCalibrationRadius
	mu := muEarth
	active := circularStateAtRadius(r, 0, mu)
	partner := circularStateAtRadius(r, -math.Pi/2, mu)

	ladder, err := RecommendMeetingLadder(active, partner, bodies.CelestialBody{}, mu, MeetingYourOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ladder.MoverIsA {
		t.Fatalf("MeetingYourOrbit must plan for the partner (stateB): MoverIsA=true")
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

	// Cross-check: MeetingTheirOrbit on the SAME two states (active as
	// mover) should generally differ from MeetingYourOrbit's rows
	// (partner as mover) — confirms the roles actually swapped rather
	// than the solver silently always burning stateA.
	theirs, err := RecommendMeetingLadder(active, partner, bodies.CelestialBody{}, mu, MeetingTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("err (their orbit cross-check): %v", err)
	}
	if !theirs.MoverIsA {
		t.Fatalf("MeetingTheirOrbit must burn the active craft: MoverIsA=false")
	}
}

// TestRecommendMeetingLadder_Unaffordable_ReturnedNotDropped — #398
// acceptance: an unaffordable row is returned AND marked, not hidden
// — "the trade stays visible" (ADR 0045 §2).
func TestRecommendMeetingLadder_Unaffordable_ReturnedNotDropped(t *testing.T) {
	r := meetingCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -math.Pi/2, mu)

	// A near-zero Δv budget: every row's burn will exceed it.
	ladder, err := RecommendMeetingLadder(chaser, target, bodies.CelestialBody{}, mu, MeetingTheirOrbit, 4*3600, 0.001)
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

// TestRecommendMeetingLadder_UnsafePeriapsis_Rejected — #398 acceptance:
// a row that would deorbit the mover is rejected by the (reused)
// periapsis-safety gate. The tangential model (meetingLadderCore)
// tries both "catch up by dropping" and "fall back by raising" per
// lap count and prefers whichever is safe — a raise leaves periapsis
// AT the burn point (r0) for a near-circular start, so it's always
// periapsis-safe UNLESS r0 itself already sits below the primary's
// surface+50km floor. Starting the mover (and holder) already inside
// that floor (30 km altitude, well under it) forces every row —
// raise or drop — to fail: there is no altitude "fall back" to when
// the starting point itself is already unsafe.
func TestRecommendMeetingLadder_UnsafePeriapsis_Rejected(t *testing.T) {
	primary := bodies.CelestialBody{MeanRadius: 6378} // km — Earth-radius primary, RadiusMeters() > 0
	mu := muEarth
	r := 6.378e6 + 30e3 // 30 km altitude — under the surface+50km floor
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -0.1*math.Pi, mu)

	ladder, err := RecommendMeetingLadder(chaser, target, primary, mu, MeetingTheirOrbit, 4*3600, -1)
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
	r := meetingCalibrationRadius
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

// TestRecommendMeetingLadder_NonCoplanarRefused — #398 out-of-scope
// note: "this slice assumes coplanar and refuses otherwise, naming
// [I]." A target inclined well past meetingPlaneTolDeg must refuse
// with ErrMeetingPlaneMismatch, not silently attempt a 3D Lambert fit.
func TestRecommendMeetingLadder_NonCoplanarRefused(t *testing.T) {
	r := meetingCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := inclinedCircularState(r, -math.Pi/2, 30*math.Pi/180, mu) // 30° plane tilt

	_, err := RecommendMeetingLadder(chaser, target, bodies.CelestialBody{}, mu, MeetingTheirOrbit, 4*3600, -1)
	if !errors.Is(err, ErrMeetingPlaneMismatch) {
		t.Fatalf("err = %v, want ErrMeetingPlaneMismatch", err)
	}
}

// TestRecommendMeetingLadder_Crossing_AlwaysRefuses — review round 2
// regression revert: PR #412's attempt to anchor meetingLadderCore's
// solve at the natural crossing instant (tCA) produced rows whose
// DV/BurnDir were only correct for a burn executed AT tCA, while
// internal/sim.PlanMeetingBurn always plants the resulting node to
// fire at TriggerTime = now + a slew lead — not at tCA. The two times
// coincide only by chance, so the planted burn routinely missed by
// megametres (see ErrMeetingCrossingNotImplemented's doc comment for
// the measured numbers, and internal/sim/meeting_test.go for the
// sim-layer regression test against the actual plant path). Reverted
// rather than fixed forward: MeetingCrossing now refuses
// unconditionally — ErrMeetingNoCrossing when no natural crossing
// exists within the search horizon, ErrMeetingCrossingNotImplemented
// when one does but there is still no solver for it. Neither path ever
// returns a ladder with rows.
//
// This fixture (a small phase offset on matched circular orbits) is
// exactly the case that used to legitimately produce a plantable
// MeetingCrossing row (the natural crossing is ~"now" there) — the
// case a partial fix could most easily miss.
func TestRecommendMeetingLadder_Crossing_AlwaysRefuses(t *testing.T) {
	r := meetingCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -0.5*math.Pi/180, mu) // small offset — NextClosestApproach converges, a natural crossing exists

	ladder, err := RecommendMeetingLadder(chaser, target, bodies.CelestialBody{}, mu, MeetingCrossing, 4*3600, -1)
	if !errors.Is(err, ErrMeetingCrossingNotImplemented) {
		t.Fatalf("err = %v, want ErrMeetingCrossingNotImplemented (a natural crossing exists here, so this must be the not-implemented refusal, not ErrMeetingNoCrossing)", err)
	}
	if len(ladder.Rows) != 0 {
		t.Fatalf("expected zero rows on a structural refusal, got %d: %+v", len(ladder.Rows), ladder.Rows)
	}
}

// TestRecommendMeetingLadder_Crossing_InvalidHorizonRefused — the input
// guard ahead of the existence check: a non-positive
// crossingSearchHorizon is a caller bug (this must always be
// rendezvousCommitHorizonSec, per this function's own doc comment), not
// something MeetingCrossing should try to interpret as "no crossing".
func TestRecommendMeetingLadder_Crossing_InvalidHorizonRefused(t *testing.T) {
	r := meetingCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -0.5*math.Pi/180, mu)

	_, err := RecommendMeetingLadder(chaser, target, bodies.CelestialBody{}, mu, MeetingCrossing, 0, -1)
	if !errors.Is(err, errMeetingInvalidInput) {
		t.Fatalf("err = %v, want errMeetingInvalidInput", err)
	}
}

// TestRecommendMeetingLadder_ShapeMismatchNowYieldsPlan — #398
// acceptance: the #290 mismatch geometry (TestRecommendRendezvousNudge_
// ShapeMismatch's own fixture — a sharply eccentric chaser, e≈0.69,
// against a circular target at the same periapsis radius) is exactly
// the case K's Shape-Match Gate used to refuse outright. The Meeting
// Planner never had that gate (it doesn't use K's single-axis
// projection, so it isn't exposed to the failure mode the gate
// existed to prevent) — it must produce a real plan for this geometry.
func TestRecommendMeetingLadder_ShapeMismatchNowYieldsPlan(t *testing.T) {
	r := 6.771e6
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := eccentricStateAtRadius(r, -0.5*math.Pi/180, 1.3, mu) // e≈0.69, #290's geometry

	ladder, err := RecommendMeetingLadder(chaser, target, bodies.CelestialBody{}, mu, MeetingTheirOrbit, 4*3600, -1)
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
		t.Fatalf("expected the Meeting Planner to produce a real plan for the #290 mismatch geometry, got: %+v", ladder.Rows)
	}
}

// meetingApplyBurn advances (moverState, holderState) forward by
// row.TArrival, applying row's burn to mover at t=0 first — via
// physics.KeplerStep, the SAME analytic (closed-form) two-body
// propagation meetingLadderCore itself uses to derive
// row.AchievableCA. This is NOT an independent check against a
// numerical integrator (Verlet/RK4) or the live game's actual coast —
// see TestMeetingLadder_IterateSelfConsistent's doc comment for what
// that limits this helper to proving. Used to chain repeated Meeting
// Planner calls in that test.
func meetingApplyBurn(moverState orbital.Vec3State, row MeetingBurnOption, holderState orbital.Vec3State, mu float64) (orbital.Vec3State, orbital.Vec3State) {
	burned := orbital.Vec3State{R: moverState.R, V: moverState.V.Add(row.BurnDir.Scale(row.DV))}
	mSV, mok := physics.KeplerStep(physics.StateVector{R: burned.R, V: burned.V}, mu, row.TArrival)
	hSV, hok := physics.KeplerStep(physics.StateVector{R: holderState.R, V: holderState.V}, mu, row.TArrival)
	if !mok || !hok {
		return burned, holderState
	}
	return orbital.Vec3State{R: mSV.R, V: mSV.V}, orbital.Vec3State{R: hSV.R, V: hSV.V}
}

// TestMeetingLadder_IterateSelfConsistent repeatedly opens the Meeting
// Planner "the way a pilot mashing the key would" on the #290 mismatch
// geometry (a sharply eccentric chaser against a circular target),
// takes the cheapest Ok row each time, applies it via meetingApplyBurn,
// and re-opens the ladder from the resulting state. It asserts the
// solver's own predicted AchievableCA stays small across every
// iteration.
//
// SCOPE, READ CAREFULLY: this is a SELF-CONSISTENCY check on the
// analytic model, not a live-integrator anti-divergence proof.
// meetingApplyBurn propagates with physics.KeplerStep — the exact same
// closed-form two-body model meetingLadderCore itself uses internally
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
func TestMeetingLadder_IterateSelfConsistent(t *testing.T) {
	r := 6.771e6
	mu := muEarth
	primary := bodies.CelestialBody{}
	target := circularStateAtRadius(r, 0, mu)
	chaser := eccentricStateAtRadius(r, -0.5*math.Pi/180, 1.3, mu) // e≈0.69, #290's geometry

	const iterations = 5
	cas := make([]float64, 0, iterations)

	for i := 0; i < iterations; i++ {
		ladder, err := RecommendMeetingLadder(chaser, target, primary, mu, MeetingTheirOrbit, 4*3600, -1)
		if err != nil {
			t.Fatalf("iteration %d: RecommendMeetingLadder err: %v", i, err)
		}
		var best MeetingBurnOption
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
		chaser, target = meetingApplyBurn(chaser, best, target, mu)
	}

	t.Logf("CA sequence across %d iterations (self-consistency, analytic model only): %v", iterations, cas)

	// Self-consistency bound, not an anti-divergence proof (see the
	// test's own doc comment): every iteration's solver-predicted
	// AchievableCA should stay near the closed-form's own residual
	// noise floor, since meetingApplyBurn flies each burn with the
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
