package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestAdaptiveSampleCount: the predicted-leg sample budget tracks how
// many orbital periods the horizon spans (~96 points per revolution),
// clamped to [predictSamplesMin, predictSamplesMax]. Non-periodic or
// degenerate inputs fall back to the minimum.
func TestAdaptiveSampleCount(t *testing.T) {
	const period = 5400.0 // ~90 min LEO
	cases := []struct {
		name    string
		horizon float64
		period  float64
		want    int
	}{
		{"one period", period, period, 96},
		{"half period floors to min", period / 2, period, 96},
		{"five periods", 5 * period, period, 480},
		{"hundred periods caps", 100 * period, period, 720},
		{"hyperbolic period falls back to min", 10 * period, math.Inf(1), 96},
		{"nan period falls back to min", 10 * period, math.NaN(), 96},
		{"zero period falls back to min", 10 * period, 0, 96},
		{"zero horizon falls back to min", 0, period, 96},
	}
	for _, c := range cases {
		if got := adaptiveSampleCount(c.horizon, c.period); got != c.want {
			t.Errorf("%s: adaptiveSampleCount(%.0f, %.0f) = %d, want %d",
				c.name, c.horizon, c.period, got, c.want)
		}
	}
}

// TestPredictMaxSubStep: the integrator sub-step cap is period/100 for
// a short-period orbit but clamps to predictMaxSubStepCap for a long
// one (so a multi-day transfer ellipse still integrates finely), and
// falls back to 1 s for a degenerate period.
func TestPredictMaxSubStep(t *testing.T) {
	cases := []struct {
		name   string
		period float64
		want   float64
	}{
		{"short LEO period → period/100", 5400, 54},
		{"long transfer period → capped", 800000, predictMaxSubStepCap},
		{"at the cap boundary → capped", predictMaxSubStepCap * 100, predictMaxSubStepCap},
		{"degenerate zero → 1 s", 0, 1.0},
		{"degenerate NaN → 1 s", math.NaN(), 1.0},
		{"degenerate +Inf → 1 s", math.Inf(1), 1.0},
	}
	for _, c := range cases {
		if got := predictMaxSubStep(c.period); got != c.want {
			t.Errorf("%s: predictMaxSubStep(%.0f) = %.2f, want %.2f", c.name, c.period, got, c.want)
		}
	}
}

// TestPredictedSegmentsTransferEncounterStable: a long Earth→Moon
// transfer leg must be integrated finely enough that the predicted
// trajectory is the same regardless of the output sample count — the
// SOI encounter the predictor draws can't depend on how densely the
// leg is sampled. Before v0.10.3 the integrator sub-step was period/100
// (~8000 s for a transfer ellipse), so a sparsely-sampled leg stepped
// over the lunar SOI and the dashed line flew off to a bogus
// heliocentric escape, while a densely-sampled one drew the encounter.
func TestPredictedSegmentsTransferEncounterStable(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon not in loaded Sol system")
	}
	moon := sys.Bodies[moonIdx]

	// Place the craft in a circular LEO coplanar with the Moon, so the
	// intra-primary Hohmann's transfer leg actually reaches the Moon.
	mel := orbital.ElementsFromBody(moon)
	sI, cI := math.Sin(mel.I), math.Cos(mel.I)
	sO, cO := math.Sin(mel.Omega), math.Cos(mel.Omega)
	moonN := orbital.Vec3{X: sO * sI, Y: -cO * sI, Z: cI}.Unit()
	ref := orbital.Vec3{X: 1}
	if math.Abs(moonN.Dot(ref)) > 0.9 {
		ref = orbital.Vec3{Y: 1}
	}
	e1 := ref.Sub(moonN.Scale(moonN.Dot(ref))).Unit()
	e2 := moonN.Cross(e1)

	mu := w.ActiveCraft().Primary.GravitationalParameter()
	r := w.ActiveCraft().State.R.Norm()
	v := math.Sqrt(mu / r)
	w.ActiveCraft().State.R = e1.Scale(r)
	w.ActiveCraft().State.V = e2.Scale(v)

	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs after PlanTransfer")
	}
	leg := legs[0]

	segIDs := func(samples int) []string {
		segs := w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, samples)
		ids := make([]string, len(segs))
		for i, s := range segs {
			ids[i] = s.PrimaryID
		}
		return ids
	}
	dense := segIDs(4000)
	adaptive := segIDs(leg.Samples)

	// The dense prediction is the reference: it must find the Moon.
	foundMoon := false
	for _, id := range dense {
		if id == moon.ID {
			foundMoon = true
		}
		if id == "sun" {
			t.Fatalf("dense prediction itself escaped to the Sun (%v) — test setup did not produce a Moon transfer", dense)
		}
	}
	if !foundMoon {
		t.Fatalf("dense prediction never reached the Moon (%v) — test setup did not produce an encounter", dense)
	}
	// The adaptive-sample prediction must agree — no sample-count
	// sensitivity, and in particular no bogus escape to the Sun.
	if len(adaptive) != len(dense) {
		t.Errorf("segment count depends on sample density: adaptive=%v dense=%v", adaptive, dense)
	}
	for i := range adaptive {
		if i < len(dense) && adaptive[i] != dense[i] {
			t.Errorf("segment %d differs by sample density: adaptive=%v dense=%v", i, adaptive, dense)
		}
	}
}

// TestPredictedSegmentsCoastMatchesKeplerArc: a ballistic coast leg must
// be propagated with analytic Kepler propagation, not fixed-step Verlet,
// so the dashed Projected Orbit follows the true two-body arc with no
// truncation drift. On a highly-eccentric ellipse (e≈0.91) the old 120 s
// Verlet sub-step drifted ~46 000 km by apogee (GH #66, clean dt²
// convergence); analytic Kepler is exact. This is the load-bearing #66
// regression guard — it goes red on the Verlet path and green on Kepler.
func TestPredictedSegmentsCoastMatchesKeplerArc(t *testing.T) {
	w := mustWorld(t)
	craft := w.ActiveCraft()
	primary := craft.Primary
	mu := primary.GravitationalParameter()
	R := primary.RadiusMeters()

	// High-eccentricity bound orbit about the home primary: perigee
	// ~1000 km altitude (well above the atmosphere, so the Kepler path is
	// eligible), apogee ~150 000 km — comfortably inside Earth's SOI and
	// never within ~168 000 km of the Moon, so the leg stays a single
	// segment. Start at perigee in the X–Y plane.
	rp := R + 1000e3
	ra := R + 150000e3
	a := (rp + ra) / 2
	vp := math.Sqrt(mu * (2/rp - 1/a)) // vis-viva at perigee
	post := physics.StateVector{
		R: orbital.Vec3{X: rp},
		V: orbital.Vec3{Y: vp},
		M: craft.State.M,
	}

	// Propagate perigee→apogee (half the period) — the stretch where
	// Verlet truncation peaks.
	period := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	horizon := period / 2
	const samples = 128
	startClock := w.Clock.SimTime
	// Equal-time path: this guards integrator fidelity by comparing point i
	// to an analytic Kepler step of i·dt, so it must use uniform-time samples.
	// The periapsis-dense public wrapper's accuracy is covered separately by
	// TestPeriapsisDenseLegMatchesKeplerArc.
	segs, _ := w.predictedSegmentsFromTuned(post, primary, startClock, horizon, samples, defaultPredictTuning())

	if len(segs) != 1 {
		ids := make([]string, len(segs))
		for i, s := range segs {
			ids[i] = s.PrimaryID
		}
		t.Fatalf("coast leg split into %d segments %v; want 1 (no SOI crossing)", len(segs), ids)
	}
	pts := segs[0].Points
	if len(pts) != samples {
		t.Fatalf("got %d points, want %d", len(pts), samples)
	}

	// Each predicted point i sits at the body position at startClock+i·dt
	// plus the craft's primary-relative state. The exact reference is a
	// single analytic Kepler step of i·dt from the start state. They must
	// agree to within solver noise; a Verlet path would diverge by tens
	// of thousands of km near apogee.
	stepSecs := horizon / float64(samples-1)
	stepDur := time.Duration(stepSecs * float64(time.Second))
	var maxErr float64
	for i := 0; i < samples; i++ {
		exact, ok := physics.KeplerStep(post, mu, float64(i)*stepSecs)
		if !ok {
			t.Fatalf("KeplerStep reference failed at sample %d", i)
		}
		bodyPos := w.BodyPositionAt(primary, startClock.Add(time.Duration(i)*stepDur))
		rel := pts[i].Sub(bodyPos)
		if e := rel.Sub(exact.R).Norm(); e > maxErr {
			maxErr = e
		}
	}
	if maxErr > 1000 {
		t.Errorf("predicted coast leg drifts %.0f m from the exact Kepler arc (want <1 km) — fixed-step Verlet truncation, not analytic propagation", maxErr)
	}
}

// TestPeriapsisDenseLegMatchesKeplerArc: the periapsis-dense public wrapper
// (PredictedSegmentsFrom) still places every sample on the exact Kepler arc —
// at its own non-uniform sample time — and packs more samples through the
// fast perigee than the slow apogee (ADR 0023 C). Same eccentric,
// single-segment setup as the equal-time fidelity guard above.
func TestPeriapsisDenseLegMatchesKeplerArc(t *testing.T) {
	w := mustWorld(t)
	craft := w.ActiveCraft()
	primary := craft.Primary
	mu := primary.GravitationalParameter()
	R := primary.RadiusMeters()

	rp := R + 1000e3
	ra := R + 150000e3
	a := (rp + ra) / 2
	vp := math.Sqrt(mu * (2/rp - 1/a))
	post := physics.StateVector{R: orbital.Vec3{X: rp}, V: orbital.Vec3{Y: vp}, M: craft.State.M}
	period := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	horizon := period / 2 // perigee → apogee
	const samples = 128
	startClock := w.Clock.SimTime

	segs := w.PredictedSegmentsFrom(post, primary, startClock, horizon, samples)
	if len(segs) != 1 {
		t.Fatalf("coast leg split into %d segments; want 1", len(segs))
	}
	pts := segs[0].Points
	if len(pts) != samples {
		t.Fatalf("got %d points, want %d", len(pts), samples)
	}

	steps, ok := eccentricAnomalyStepSecs(post, mu, horizon, samples)
	if !ok {
		t.Fatal("expected a periapsis-dense schedule for an eccentric leg")
	}
	sum := 0.0
	for _, s := range steps {
		if s <= 0 {
			t.Fatalf("non-positive schedule step %.3fs", s)
		}
		sum += s
	}
	if math.Abs(sum-horizon) > 1 {
		t.Errorf("schedule sums to %.0fs, want horizon %.0fs", sum, horizon)
	}

	// Every sample lies on the exact Kepler arc at its own (non-uniform) time.
	tCum, maxErr := 0.0, 0.0
	for i := 0; i < samples; i++ {
		exact, kok := physics.KeplerStep(post, mu, tCum)
		if !kok {
			t.Fatalf("KeplerStep reference failed at sample %d", i)
		}
		bodyPos := w.BodyPositionAt(primary, startClock.Add(time.Duration(tCum*float64(time.Second))))
		if e := pts[i].Sub(bodyPos).Sub(exact.R).Norm(); e > maxErr {
			maxErr = e
		}
		if i < samples-1 {
			tCum += steps[i]
		}
	}
	if maxErr > 1000 {
		t.Errorf("periapsis-dense leg drifts %.0f m from the exact Kepler arc (want <1 km)", maxErr)
	}

	// Dense at BOTH apsides in SPACE: the body-relative chord between
	// consecutive points is shortest at perigee (first) and apogee (last) and
	// widest at quadrature (middle) — even arc-length, densest where the orbit
	// turns. (Uniform-E's largest TIME step is at apogee, but the craft is
	// slow there, so the spatial spacing is small — that's the property that
	// matters for dot density.)
	rel := segs[0].RelPoints
	chord := func(i int) float64 { return rel[i+1].Sub(rel[i]).Norm() }
	mid := chord(samples / 2)
	if chord(0) >= mid {
		t.Errorf("perigee chord %.0f km not shorter than quadrature %.0f km — not apsis-dense", chord(0)/1e3, mid/1e3)
	}
	if chord(samples-2) >= mid {
		t.Errorf("apogee chord %.0f km not shorter than quadrature %.0f km — not apsis-dense", chord(samples-2)/1e3, mid/1e3)
	}
}

// coplanarLEOTowardMoon places the active craft in a circular LEO
// coplanar with the Moon so an intra-primary [H] transfer's coast leg
// actually reaches the Moon, then plants the transfer. Returns the
// first predicted leg. (Pulled out so the integrator-fidelity and
// SOI-entry guards share one setup; the default inclined Luna split
// doesn't rendezvous yet — that's GH #67.)
func coplanarLEOTowardMoon(t *testing.T, w *World) PredictedLeg {
	t.Helper()
	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon not in loaded Sol system")
	}
	moon := sys.Bodies[moonIdx]

	mel := orbital.ElementsFromBody(moon)
	sI, cI := math.Sin(mel.I), math.Cos(mel.I)
	sO, cO := math.Sin(mel.Omega), math.Cos(mel.Omega)
	moonN := orbital.Vec3{X: sO * sI, Y: -cO * sI, Z: cI}.Unit()
	ref := orbital.Vec3{X: 1}
	if math.Abs(moonN.Dot(ref)) > 0.9 {
		ref = orbital.Vec3{Y: 1}
	}
	e1 := ref.Sub(moonN.Scale(moonN.Dot(ref))).Unit()
	e2 := moonN.Cross(e1)

	mu := w.ActiveCraft().Primary.GravitationalParameter()
	r := w.ActiveCraft().State.R.Norm()
	v := math.Sqrt(mu / r)
	w.ActiveCraft().State.R = e1.Scale(r)
	w.ActiveCraft().State.V = e2.Scale(v)

	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs after PlanTransfer")
	}
	return legs[0]
}

// TestPredictedSegmentsEntersMoonSOI: the now-analytic coast leg of an
// LEO→Luna transfer must actually cross into the Moon's sphere of
// influence — a foreign "moon" segment present — proving the Kepler
// propagation path still drives the per-sub-step FindPrimary/Rebase
// (GH #66 acceptance test 2: the predicted path draws the encounter).
func TestPredictedSegmentsEntersMoonSOI(t *testing.T) {
	w := mustWorld(t)
	leg := coplanarLEOTowardMoon(t, w)

	segs := w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, leg.Samples)
	ids := make([]string, len(segs))
	foundMoon := false
	for i, s := range segs {
		ids[i] = s.PrimaryID
		if s.PrimaryID == "moon" {
			foundMoon = true
		}
		if s.PrimaryID == "sun" {
			t.Fatalf("predicted leg escaped to the Sun (%v) — coast leg flung off its conic", ids)
		}
	}
	if !foundMoon {
		t.Errorf("predicted coast leg never entered the Moon's SOI (segments %v); the dashed line misses the encounter", ids)
	}
}

// TestPredictedSegmentsContinuousAtSOIBoundary: plant a hyperbolic
// trajectory escaping Earth SOI; PredictedSegments should split into
// (≥) two segments at the boundary, AND the last point of the inner
// segment should match (in inertial coordinates) the first point of the
// outer segment within a small tolerance. Pre-v0.3.0 the predictor
// integrated with Earth's μ throughout, so the post-escape segment was
// geometrically wrong but the JOIN was at least continuous because
// segments shared the same coordinates. v0.3.0 rebases on crossing —
// the join must still land continuously after the rebase.
func TestPredictedSegmentsContinuousAtSOIBoundary(t *testing.T) {
	w := mustWorld(t)

	// Boost velocity well past Earth escape (|v_circ| ≈ 7.78 km/s,
	// |v_esc| ≈ 11.0 km/s). 16 km/s gives v∞ ≈ 10 km/s — past Earth
	// SOI (~924 000 km) within ~1 day with margin to spare.
	post := w.ActiveCraft().State
	post.V = orbital.Vec3{Y: 16000}

	const totalSecs = 3 * 86400.0 // 3 days
	const samples = 600
	segs := w.PredictedSegmentsFrom(post, w.ActiveCraft().Primary, w.Clock.SimTime, totalSecs, samples)

	if len(segs) < 2 {
		t.Fatalf("expected ≥2 SOI segments after escape, got %d (no SOI crossing detected)", len(segs))
	}

	// Find the first inter-segment join and assert continuity in inertial.
	for i := 0; i+1 < len(segs); i++ {
		if len(segs[i].Points) == 0 || len(segs[i+1].Points) == 0 {
			t.Fatalf("segment %d or %d has zero points", i, i+1)
		}
		end := segs[i].Points[len(segs[i].Points)-1]
		start := segs[i+1].Points[0]
		gap := end.Sub(start).Norm()
		// Earth SOI ≈ 924,000 km; a discontinuity of more than 1000 km
		// would indicate the rebase math dropped the relative-position
		// bookkeeping. 100 km buffer accounts for one Verlet sub-step
		// of motion at the boundary (typically << 1 km, but we want
		// generous slack).
		if gap > 100e3 {
			t.Errorf("segment %d→%d join discontinuity: %.1f km (primary %s → %s)",
				i, i+1, gap/1000, segs[i].PrimaryID, segs[i+1].PrimaryID)
		}
	}
}

// TestPredictedSegmentsBoundOrbitStaysInOneSegment: an unmodified LEO
// orbit propagated for one full period must stay in a single segment
// labeled with Earth's ID. Catches a regression where the SOI check
// false-positively rebases inside the home SOI.
func TestPredictedSegmentsBoundOrbitStaysInOneSegment(t *testing.T) {
	w := mustWorld(t)
	post := w.ActiveCraft().State

	mu := w.ActiveCraft().Primary.GravitationalParameter()
	period := 2 * math.Pi * math.Sqrt(math.Pow(post.R.Norm(), 3)/mu)
	segs := w.PredictedSegmentsFrom(post, w.ActiveCraft().Primary, w.Clock.SimTime, period, 128)

	if len(segs) != 1 {
		ids := make([]string, len(segs))
		for i, s := range segs {
			ids[i] = s.PrimaryID
		}
		t.Errorf("LEO orbit produced %d segments (%v); want 1", len(segs), ids)
	}
	if len(segs) > 0 && segs[0].PrimaryID != w.ActiveCraft().Primary.ID {
		t.Errorf("LEO segment primary = %s, want %s", segs[0].PrimaryID, w.ActiveCraft().Primary.ID)
	}
}

// TestIntegrateSpacecraftSwitchesPrimaryMidTick: regression for v0.4.2.
// At high warp a single tick can cover an SOI crossing; the live
// integrator must rebase to the new primary inside its sub-step loop,
// not wait for maybeSwitchPrimary's per-20-tick throttle. Otherwise
// post-crossing sub-steps integrate with the wrong μ and the live
// orbit drifts off the predicted one.
func TestIntegrateSpacecraftSwitchesPrimaryMidTick(t *testing.T) {
	w := mustWorld(t)
	homeID := w.ActiveCraft().Primary.ID

	// 16 km/s y-velocity → hyperbolic Earth escape; ~3 days clears
	// Earth SOI (~924 000 km) with margin.
	w.ActiveCraft().State.V = orbital.Vec3{Y: 16000}

	// Single tick covering 3 days of sim time. integrateSpacecraft
	// caps sub-steps at 1024 and dt at period/100, but per-sub-step
	// SOI check should still fire when the boundary is crossed
	// regardless of how the dt is sized.
	w.integrateOneCraft(w.ActiveCraft(), time.Duration(3 * 86400 * float64(time.Second)))

	if w.ActiveCraft().Primary.ID == homeID {
		t.Errorf("live integrator stayed in home primary %q after 3-day escape; SOI check did not fire mid-tick",
			homeID)
	}
	// State should now be on a heliocentric scale, not 8e8 m geocentric.
	if w.ActiveCraft().State.R.Norm() < 1e9 {
		t.Errorf("post-tick |r|=%.3e m — looks like state wasn't rebased", w.ActiveCraft().State.R.Norm())
	}
}

// TestIntegrateSpacecraftMatchesPredictorAcrossSOI: at high warp the
// live integrator's end state should match the predictor's end state
// (same Verlet sub-stepping, same SOI boundary handling). Pre-v0.4.2
// the predictor's per-sub-step rebase didn't have a counterpart in
// integrateSpacecraft, so the two could diverge by tens of thousands
// of km after a mid-tick SOI crossing. The fix folds the same rebase
// logic into the live integrator; their post-crossing states should
// now match within a Verlet step's worth of motion.
//
// v0.8.4: both paths refresh body positions per chunk against
// wall-clock. The predictor integrates the window [SimTime,
// SimTime+dt]; the live integrator looks back over the elapsed tick
// [SimTime-simDelta, SimTime] (production Tick advances SimTime
// before calling integrateOneCraft). To compare apples to apples,
// snapshot the predictor's start state and target window first, then
// advance SimTime by dt so the live integrator looks back over the
// same window the predictor went forward through.
func TestIntegrateSpacecraftMatchesPredictorAcrossSOI(t *testing.T) {
	w := mustWorld(t)
	w.ActiveCraft().State.V = orbital.Vec3{Y: 16000}

	// Snapshot starting state, run the predictor on it.
	startState := w.ActiveCraft().State
	dt := 3 * 86400.0
	predicted := w.propagateCraft(dt)

	// Reset craft state, advance SimTime so the live integrator looks
	// back over [original-SimTime, original-SimTime+dt] — same window
	// the predictor integrated forward through.
	w.ActiveCraft().State = startState
	w.Clock.SimTime = w.Clock.SimTime.Add(time.Duration(dt * float64(time.Second)))
	w.integrateOneCraft(w.ActiveCraft(), time.Duration(dt * float64(time.Second)))

	gap := w.ActiveCraft().State.R.Sub(predicted.R).Norm()
	// The two paths share Verlet step + SOI-rebase math; allow 1e6 m
	// (1000 km) for accumulated single-precision noise across 1024
	// sub-steps. Pre-v0.4.2 the gap was 10⁷–10⁸ m (post-crossing wrong-
	// frame integration) so even a generous bound catches the bug.
	if gap > 1e6 {
		t.Errorf("live vs predicted divergence after SOI crossing: %.3e m (>1e6)", gap)
	}
}

// TestWarpLockPreservesCircularOrbit: regression for v0.4.3. At
// 10000× warp the default 200×200 km circular LEO drifted to roughly
// 209×191 km within a few real-time seconds because Verlet sub-steps
// at coarse dt accumulated eccentricity (random walk in apo/peri).
// With the warp lock (analytic Kepler propagation when warp > 1× and
// no active burn), the orbit must hold its semimajor axis and
// eccentricity essentially unchanged across many ticks.
func TestWarpLockPreservesCircularOrbit(t *testing.T) {
	w := mustWorld(t)

	// Bump to 10000× warp.
	for i := 0; i < 4; i++ {
		w.Clock.WarpUp()
	}
	if w.Clock.Warp() != 10000 {
		t.Fatalf("warp setup: got %.0f, want 10000", w.Clock.Warp())
	}

	mu := w.ActiveCraft().Primary.GravitationalParameter()
	startEl := orbital.ElementsFromState(w.ActiveCraft().State.R, w.ActiveCraft().State.V, mu)

	// Run 600 ticks ≈ 10 real-time seconds at the default 50 ms base
	// step → 600 × 0.5 s × 10000× = ~50 simulated days. That covers
	// ~1000 LEO orbits — enough sub-step error pre-fix to drift
	// eccentricity from ~0 to >1e-3.
	for i := 0; i < 600; i++ {
		w.Tick()
	}

	endEl := orbital.ElementsFromState(w.ActiveCraft().State.R, w.ActiveCraft().State.V, mu)

	// Semimajor axis must be conserved (analytic Kepler is exact).
	if relErr := math.Abs(endEl.A-startEl.A) / startEl.A; relErr > 1e-9 {
		t.Errorf("warp-lock semimajor drift: %.3e (rel)", relErr)
	}
	// Eccentricity must stay ~zero. Pre-fix: O(1e-3) random walk.
	if endEl.E > 1e-9 {
		t.Errorf("warp-lock eccentricity grew to %.3e (want < 1e-9)", endEl.E)
	}
}

// TestWarpLockDetectsForeignSOIEntry: regression for v0.4.4. Place a
// heliocentric craft already inside Mars's SOI (with craft.Primary
// still set to Sun, simulating a craft that just crossed in). v0.4.3's
// analytic warp path returned without SOI re-evaluation, so the primary
// stayed Sun until the every-20-tick maybeSwitchPrimary throttle fired.
// v0.4.4's keplerStepWithSOICheck calls FindPrimary between Kepler
// chunks; one tick should be enough to switch primary.
func TestWarpLockDetectsForeignSOIEntry(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	sun := sys.Bodies[0]
	var mars bodies.CelestialBody
	for _, b := range sys.Bodies {
		if b.EnglishName == "Mars" {
			mars = b
			break
		}
	}
	if mars.EnglishName == "" {
		t.Skip("Mars not in loaded Sol system")
	}

	marsPos := w.BodyPosition(mars)
	marsVel := w.bodyInertialVelocity(mars)
	soi := physics.SOIRadius(mars, sun)

	// Place craft 50% inside Mars's SOI, on the heliocentric +X side
	// of Mars. Velocity = Mars's velocity + a small radial component
	// so the craft is bound to Mars in the post-rebase frame.
	w.ActiveCraft().Primary = sun
	w.ActiveCraft().State.R = orbital.Vec3{X: marsPos.X + soi*0.5, Y: marsPos.Y, Z: marsPos.Z}
	w.ActiveCraft().State.V = marsVel

	// Bump to 1000× warp (well above 1× so warp-lock activates) but not
	// so high we burn 20+ ticks of sim time waiting for the throttle.
	for i := 0; i < 3; i++ {
		w.Clock.WarpUp()
	}
	if w.Clock.Warp() != 1000 {
		t.Fatalf("warp setup: got %.0f, want 1000", w.Clock.Warp())
	}

	w.Tick()
	if w.ActiveCraft().Primary.ID != mars.ID {
		t.Errorf("after 1 tick under warp lock, primary = %s, want Mars (SOI re-eval skipped between Kepler chunks)",
			w.ActiveCraft().Primary.EnglishName)
	}
}

// TestPropagateCraftSOIAware: forward-integrate a hyperbolic escape via
// propagateCraft and confirm the resulting state isn't expressed in the
// original primary's frame anymore (i.e. |r| would have to be absurdly
// large if it were, but it should be reasonable in the new frame). This
// catches the case where propagateCraft forgot to rebase and returned
// a state vector still tied to Earth's center even after crossing Sol.
func TestPropagateCraftSOIAware(t *testing.T) {
	w := mustWorld(t)
	w.ActiveCraft().State.V = orbital.Vec3{Y: 16000}

	state := w.propagateCraft(3 * 86400.0)

	// Sanity: post-rebase r should be on a heliocentric scale (~1 AU, 1.5e11 m)
	// or planet-relative scale, never the geocentric escape distance which
	// would be ~v∞ × t ≈ 5km/s × 2d × 86400 ≈ 8.6e8 m if frame wasn't switched.
	// In the heliocentric frame after rebase, r should equal ≈ AU plus the
	// post-escape Earth-relative offset, so r > 1e11 m is the indicator.
	if state.R.Norm() < 1e10 {
		t.Errorf("propagateCraft after escape: |r|=%.3e m — looks like still in Earth frame", state.R.Norm())
	}
	// And shouldn't be NaN or stupidly large.
	if math.IsNaN(state.R.Norm()) || state.R.Norm() > 1e13 {
		t.Errorf("propagateCraft: unphysical |r|=%.3e m", state.R.Norm())
	}
	_ = physics.SemimajorAxis // reuse import
}
