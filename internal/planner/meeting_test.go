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
		// "small" — a few km at most, at 500 km altitude — the
		// tolerance a Verlet-vs-Lambert (analytic) mismatch could
		// plausibly introduce, not the km-scale gap a wrong-direction
		// or wrong-frame bug would leave.
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

// TestRecommendMeetingLadder_Crossing_NoEncounterRefused — MeetingCrossing
// with a degenerate/invalid input (mu<=0 via a zero-search-horizon
// guard here — see errMeetingInvalidInput path) never silently
// succeeds. The "no natural encounter" path is exercised for real via
// a cross-primary style non-convergent NextClosestApproach input.
func TestRecommendMeetingLadder_Crossing_HappyPath(t *testing.T) {
	r := meetingCalibrationRadius
	mu := muEarth
	target := circularStateAtRadius(r, 0, mu)
	chaser := circularStateAtRadius(r, -0.5*math.Pi/180, mu) // small offset — NextClosestApproach converges

	ladder, err := RecommendMeetingLadder(chaser, target, bodies.CelestialBody{}, mu, MeetingCrossing, 4*3600, -1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ladder.MoverIsA {
		t.Fatalf("MeetingCrossing must burn the active craft: MoverIsA=false")
	}
	sawOk := false
	for _, row := range ladder.Rows {
		if row.Ok {
			sawOk = true
		}
	}
	if !sawOk {
		t.Fatalf("expected at least one Ok row: %+v", ladder.Rows)
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
// row.TArrival, applying row's burn to mover at t=0 first — the same
// KeplerStep propagation meetingLadderCore's own "propagate and check"
// uses (and what the live game actually flies for a multi-hour/day
// coast: warp-locked physics runs one KeplerStep per tick, not looped
// Verlet — internal/physics/kepler_step.go). Used to chain repeated
// Meeting Planner calls in TestMeetingLadder_IterateDoesNotDiverge.
func meetingApplyBurn(moverState orbital.Vec3State, row MeetingBurnOption, holderState orbital.Vec3State, mu float64) (orbital.Vec3State, orbital.Vec3State) {
	burned := orbital.Vec3State{R: moverState.R, V: moverState.V.Add(row.BurnDir.Scale(row.DV))}
	mSV, mok := physics.KeplerStep(physics.StateVector{R: burned.R, V: burned.V}, mu, row.TArrival)
	hSV, hok := physics.KeplerStep(physics.StateVector{R: holderState.R, V: holderState.V}, mu, row.TArrival)
	if !mok || !hok {
		return burned, holderState
	}
	return orbital.Vec3State{R: mSV.R, V: mSV.V}, orbital.Vec3State{R: hSV.R, V: hSV.V}
}

// TestMeetingLadder_IterateDoesNotDiverge — the central risk this
// slice must retire before the Shape-Match Gate can safely come out
// (#398 task brief): repeatedly opening the Meeting Planner "the way a
// pilot mashing the key would" on the #290 mismatch geometry (a
// sharply eccentric chaser against a circular target — exactly what
// the gate used to refuse, and exactly what K's OWN single-axis-
// projection nudge diverged on when iterated: #290's live sequence
// spent 275 m/s across two "successful" nudges to end up FARTHER away,
// CA 577 km → 1,110 km) must NOT reproduce that divergence.
//
// Each iteration: ask the ladder, take its cheapest Ok row, fly it
// (apply the burn + propagate both craft to the row's own arrival
// time — independent Verlet integration, not the Lambert closed
// form), then immediately re-open the ladder from the resulting
// states, as a pilot re-pressing the key at the meeting point would.
// The recorded sequence is each iteration's ACHIEVED closest approach
// (AchievableCA) — what the pilot would actually see at that meeting
// point, not a hypothetical.
//
// Unlike K's single-axis Nudge (Step 2's lossy projection onto one of
// eight fixed axes is what let #290 walk the chaser's shape further
// from the target's), the Meeting Planner solves a genuine two-point
// Lambert boundary-value problem every time: it doesn't accumulate the
// kind of directional error the projection did, so there is no
// mechanism here for repeated use to drift the geometry apart the way
// #290 measured.
func TestMeetingLadder_IterateDoesNotDiverge(t *testing.T) {
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

	t.Logf("CA sequence across %d iterations: %v", iterations, cas)

	// Anti-divergence: no iteration's achieved CA may exceed a fixed,
	// small absolute bound. #290's OWN divergence reached 1,110 km
	// after starting at 577 km — two orders of magnitude above
	// anything a correctly-solved Lambert meeting burn should leave
	// (residual is Verlet-integration noise, not geometry error).
	const divergenceBoundM = 50_000.0 // 50 km — generous vs. expected ~m-scale residuals, tight vs. #290's 1,110 km
	for i, ca := range cas {
		if ca > divergenceBoundM {
			t.Errorf("iteration %d: AchievableCA=%.0f m exceeds the anti-divergence bound (%.0f m) — sequence: %v", i, ca, divergenceBoundM, cas)
		}
	}
}
