package sim

// soi_entry_prediction_eval_test.go — DIAGNOSTIC harness (not a CI
// assertion): quantifies how reliably the trajectory predictors report
// a Hohmann moon transfer's SOI entry, and attributes the error to the
// individual numeric artifacts via predictTuning knobs (vault:
// warp-orbit-accuracy.md §"2026-06-09 — SOI-entry prediction fidelity").
//
// What it measures, per scenario (planet→moon transfer, both predictor
// sites):
//
//   accuracy   — predicted moon-periapsis radius + entry-time error at
//                checkpoints along the live approach, scored against the
//                LIVE-FLOWN outcome (what integrateOneCraft actually
//                does — the player's truth) and cross-checked against a
//                converged reference variant ("ref").
//   stability  — the swing of the predicted periapsis across
//                checkpoints: the playtest symptom is that this number
//                won't hold still enough to fine-tune against.
//   tunability — predicted-periapsis response to a 1 m/s prograde
//                correction vs the local checkpoint-to-checkpoint
//                jitter: signal-to-noise for mid-course tuning.
//   drawing    — post-entry: how far the sampled dashed path's closest
//                approach is from the analytic periapsis implied by the
//                (exact) entry state — the hyperbolic-Verlet artifact.
//
// Heavy (minutes, many full-horizon propagations); excluded from normal
// runs. Run with:
//
//	TSP_SOI_EVAL=1 go test ./internal/sim -run TestSOIEntryPredictionEval -v
//
// It always passes; read the t.Log output.

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

type soiEvalScenario struct {
	name       string
	systemName string
	planet     string // EnglishName
	moon       string // EnglishName
}

type soiEvalVariant struct {
	name string
	tu   predictTuning
}

// soiEvalVariants: legacy plus one knob at a time, the combined
// candidate fix, and the converged reference. "ref" doubles as the
// convergence cross-check against "ref/2" (halved caps) — if those two
// disagree the reference itself isn't converged and the scenario's
// numbers are suspect.
func soiEvalVariants() []soiEvalVariant {
	return []soiEvalVariant{
		{"legacy", predictTuning{}},
		{"+bodies", predictTuning{BodyPerSubStep: true}},
		{"+interp", predictTuning{BodyInterp: true}},
		{"+refine", predictTuning{RefineCrossing: true}},
		{"+cap120", predictTuning{CoastSubStepCap: 120}},
		{"+hyp5", predictTuning{HyperbolicDtCap: 5, MaxSubSteps: 1 << 16}},
		{"all", predictTuning{BodyPerSubStep: true, RefineCrossing: true, CoastSubStepCap: 120, HyperbolicDtCap: 5, MaxSubSteps: 1 << 16}},
		// fix = the shipped production default (v0.17.2, ADR 0017 —
		// defaultPredictTuning(), pinned equal by
		// TestDefaultPredictTuningIsFixVariant): interpolated bodies
		// (cheap), refined crossings, capped node-chain sub-steps; no
		// hyperbolic dt cap (the drawing metric shows it's worth only a
		// few km once positions are fresh) and no perf-clamp lift.
		{"fix", predictTuning{BodyInterp: true, RefineCrossing: true, CoastSubStepCap: 120}},
		{"ref", predictTuning{BodyPerSubStep: true, RefineCrossing: true, CoastSubStepCap: 30, HyperbolicDtCap: 1, MaxSubSteps: 1 << 20}},
		{"ref/2", predictTuning{BodyPerSubStep: true, RefineCrossing: true, CoastSubStepCap: 15, HyperbolicDtCap: 0.5, MaxSubSteps: 1 << 20}},
	}
}

// soiEvalVariantByName panics on a missing name — eval-internal lookup.
func soiEvalVariantByName(name string) soiEvalVariant {
	for _, v := range soiEvalVariants() {
		if v.name == name {
			return v
		}
	}
	panic("unknown eval variant " + name)
}

// periRadiusKm is the analytic periapsis radius (km) implied by a
// primary-relative state — a(1−e), valid for elliptic and hyperbolic
// arcs alike. All accuracy scoring runs through this single lens: a
// prediction is exactly as good as the SOI-entry state it produces.
func periRadiusKm(s physics.StateVector, mu float64) float64 {
	el := orbital.ElementsFromState(s.R, s.V, mu)
	return el.A * (1 - el.E) / 1000
}

// moonPlaneCircularState returns a circular parking orbit of radius r
// in the moon's orbital plane (same construction as
// coplanarLEOTowardMoon, generalised to any planet/moon).
func moonPlaneCircularState(planet, moon bodies.CelestialBody, r float64) (orbital.Vec3, orbital.Vec3) {
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
	v := math.Sqrt(planet.GravitationalParameter() / r)
	return e1.Scale(r), e2.Scale(v)
}

type soiEvalCheckpoint struct {
	state    physics.StateVector
	primary  bodies.CelestialBody
	clock    time.Time
	tToEntry float64 // seconds until the live-flown SOI entry
}

// predictEntry runs one tuned prediction from a checkpoint and returns
// the first predicted entry into the target moon's SOI (ok=false when
// the prediction misses the moon entirely — itself a reliability
// signal). siteA = the segment predictor (the dashed Projected Orbit);
// otherwise the node-chain propagator (feeds ArrivalCapturePreview).
func predictEntry(w *World, cp soiEvalCheckpoint, moon bodies.CelestialBody, horizon float64, tu predictTuning, siteA bool) (soiEntry, bool) {
	var entries []soiEntry
	if siteA {
		samples := adaptiveSampleCount(horizon, orbitalPeriod(cp.state, cp.primary.GravitationalParameter()))
		_, entries = w.predictedSegmentsFromTuned(cp.state, cp.primary, cp.clock, horizon, samples, tu)
	} else {
		_, _, entries = w.propagateStateWithPrimaryTuned(cp.state, cp.primary, cp.clock, horizon, tu)
	}
	for _, e := range entries {
		if e.BodyID == moon.ID {
			return e, true
		}
	}
	return soiEntry{}, false
}

func TestSOIEntryPredictionEval(t *testing.T) {
	if os.Getenv("TSP_SOI_EVAL") == "" {
		t.Skip("diagnostic eval harness; run with TSP_SOI_EVAL=1 go test ./internal/sim -run TestSOIEntryPredictionEval -v")
	}

	scenarios := []soiEvalScenario{
		{"Sol Earth→Moon (slow moon, big SOI)", "Sol", "Earth", "Moon"},
		{"Lumen Kern→Cursor (fast tiny moon)", "Lumen", "Kern", "Cursor"},
		{"Lumen Daemon→Byte (inclined e=0.235 i=15°)", "Lumen", "Daemon", "Byte"},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) { runSOIEvalScenario(t, sc) })
	}
}

func runSOIEvalScenario(t *testing.T, sc soiEvalScenario) {
	w := mustWorld(t)

	sysIdx := -1
	for i, s := range w.Systems {
		if s.Name == sc.systemName {
			sysIdx = i
		}
	}
	if sysIdx < 0 {
		t.Skipf("system %q not loaded", sc.systemName)
	}
	w.SystemIdx = sysIdx
	craft := w.ActiveCraft()
	craft.SystemIdx = sysIdx
	sys := w.System()

	planetIdx, moonIdx := -1, -1
	for i, b := range sys.Bodies {
		if b.EnglishName == sc.planet {
			planetIdx = i
		}
		if b.EnglishName == sc.moon {
			moonIdx = i
		}
	}
	if planetIdx < 0 || moonIdx < 0 {
		t.Skipf("%s/%s not in system %s", sc.planet, sc.moon, sc.systemName)
	}
	planet, moon := sys.Bodies[planetIdx], sys.Bodies[moonIdx]
	moonMu := moon.GravitationalParameter()

	// Parking orbit: coplanar with the moon, above atmosphere/surface.
	rPark := planet.RadiusMeters() * 1.08
	if atm := planet.Atmosphere; atm != nil {
		if min := planet.RadiusMeters() + atm.CutoffAltitude + 50e3; min > rPark {
			rPark = min
		}
	}
	craft.Primary = planet
	craft.State.R, craft.State.V = moonPlaneCircularState(planet, moon, rPark)

	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Skipf("PlanTransfer(%s): %v", sc.moon, err)
	}
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Skip("no predicted legs after PlanTransfer")
	}
	leg := legs[0]

	// Jump to the post-departure-burn state and live-fly the coast with
	// the production integrator in warp-sized chunks, snapshotting
	// checkpoints. Burn-execution fidelity is out of scope — the studied
	// phenomenon is coast→SOI-entry prediction.
	const chunkSecs = 120.0
	w.Clock.SimTime = leg.StartClock
	craft.State = leg.State
	craft.Primary = leg.Primary
	craft.Nodes = nil

	horizon := leg.HorizonSecs*1.6 + 86400
	cpEvery := leg.HorizonSecs / 24
	var cps []soiEvalCheckpoint
	nextCp, elapsed := 0.0, 0.0
	for elapsed < horizon && craft.Primary.ID != moon.ID {
		if elapsed >= nextCp {
			cps = append(cps, soiEvalCheckpoint{craft.State, craft.Primary, w.Clock.SimTime, 0})
			nextCp += cpEvery
		}
		d := time.Duration(chunkSecs * float64(time.Second))
		w.Clock.SimTime = w.Clock.SimTime.Add(d)
		w.integrateOneCraft(craft, d)
		elapsed += chunkSecs
	}
	if craft.Primary.ID != moon.ID {
		t.Skipf("live coast never entered %s SOI within %.1f d — planner missed; scenario unusable (itself a data point)", sc.moon, horizon/86400)
	}
	flownEntryClock := w.Clock.SimTime
	flownPeriKm := periRadiusKm(craft.State, moonMu)
	flownEntryState := craft.State

	totalCoast := flownEntryClock.Sub(leg.StartClock).Seconds()
	kept := cps[:0]
	for _, cp := range cps {
		tte := flownEntryClock.Sub(cp.clock).Seconds()
		if tte <= chunkSecs {
			continue
		}
		cp.tToEntry = tte
		kept = append(kept, cp)
	}
	cps = kept

	t.Logf("=== %s ===", sc.name)
	t.Logf("coast %.2f d · moon SOI R=%.0f km · FLOWN entry peri %.0f km (radius; moon R=%.0f km) at %s (±%.0f s live-chunk quantization)",
		totalCoast/86400, physics.SOIRadius(moon, planet)/1000, flownPeriKm, moon.RadiusMeters()/1000,
		flownEntryClock.Format("15:04:05"), chunkSecs)

	variants := soiEvalVariants()
	for _, siteA := range []bool{true, false} {
		site := "site A (segment predictor — dashed Projected Orbit)"
		if !siteA {
			site = "site B (node-chain propagator — feeds ArrivalCapturePreview)"
		}
		t.Logf("--- %s ---", site)
		header := fmt.Sprintf("%12s", "t-to-entry")
		for _, v := range variants {
			header += fmt.Sprintf(" │ %16s", v.name)
		}
		t.Logf("%s", header)
		t.Logf("%s  (cells: peri-radius km / entry-time err s; '—' = prediction misses the moon)", strings.Repeat("·", len(header)/2))

		// peris[v] collects each variant's predicted periapsis series for
		// the stability stats; NaN marks a miss.
		peris := make([][]float64, len(variants))
		for vi := range variants {
			peris[vi] = make([]float64, len(cps))
		}
		for ci, cp := range cps {
			predHorizon := cp.tToEntry + 6*3600
			row := fmt.Sprintf("%11.2fh", cp.tToEntry/3600)
			for vi, v := range variants {
				e, ok := predictEntry(w, cp, moon, predHorizon, v.tu, siteA)
				if !ok {
					peris[vi][ci] = math.NaN()
					row += fmt.Sprintf(" │ %16s", "—")
					continue
				}
				p := periRadiusKm(e.State, moonMu)
				peris[vi][ci] = p
				dtErr := e.Clock.Sub(flownEntryClock).Seconds()
				row += fmt.Sprintf(" │ %9.0f/%+5.0f", p, dtErr)
			}
			t.Logf("%s", row)
		}

		// Stability: swing (max−min) of the predicted periapsis over all
		// checkpoints and over the final approach (last third), plus the
		// miss rate. The flown outcome is one number — a reliable
		// predictor's series should be flat at it.
		t.Logf("stability (swing = max−min of predicted peri, km; miss = %% of checkpoints with no moon entry predicted):")
		for vi, v := range variants {
			all, late := swingKm(peris[vi], cps, math.Inf(1)), swingKm(peris[vi], cps, totalCoast/3)
			miss := 0
			for _, p := range peris[vi] {
				if math.IsNaN(p) {
					miss++
				}
			}
			t.Logf("  %8s: swing(all) %9.0f km · swing(final-third) %9.0f km · miss %d/%d · vs-flown bias(last) %+.0f km",
				v.name, all, late, miss, len(peris[vi]), lastValid(peris[vi])-flownPeriKm)
		}

		// Tunability at the mid-course checkpoint: does a 1 m/s prograde
		// correction move the predicted periapsis by more than the local
		// checkpoint-to-checkpoint jitter? Ratio < 1 means the knob the
		// player turns is below the noise floor.
		if len(cps) >= 3 {
			mid := len(cps) / 2
			for _, vi := range []int{variantIdx(variants, "legacy"), variantIdx(variants, "fix")} {
				v := variants[vi]
				cp := cps[mid]
				nudged := cp
				nudged.state.V = cp.state.V.Add(cp.state.V.Unit().Scale(1.0))
				base, okB := predictEntry(w, cp, moon, cp.tToEntry+6*3600, v.tu, siteA)
				bumped, okN := predictEntry(w, nudged, moon, cp.tToEntry+6*3600, v.tu, siteA)
				jitter := math.Abs(peris[vi][mid] - peris[vi][mid-1])
				if okB && okN {
					resp := math.Abs(periRadiusKm(bumped.State, moonMu) - periRadiusKm(base.State, moonMu))
					t.Logf("tunability @%.1fh out, %s: 1 m/s prograde moves peri %.0f km · local jitter %.0f km · signal/noise %.2f",
						cp.tToEntry/3600, v.name, resp, jitter, resp/math.Max(jitter, 1e-9))
				} else {
					t.Logf("tunability @%.1fh out, %s: unmeasurable (prediction misses the moon: base=%v nudged=%v)",
						cp.tToEntry/3600, v.name, okB, okN)
				}
			}
		}
	}

	// Post-entry drawing error: from the (exact, by construction) flown
	// entry state, how far is the sampled dashed path's closest approach
	// from the analytic periapsis? Isolates the hyperbolic-Verlet
	// artifact — entry-state error is zero here.
	// Perf: cost per prediction call at the longest horizon (the render
	// loop re-predicts every frame — the v0.8.4 per-sample snapshot
	// exists because sub-step refresh was estimated at "60% of a render
	// frame"; the fix decision needs that measured, not estimated).
	if len(cps) > 0 {
		cp := cps[0]
		predHorizon := cp.tToEntry + 6*3600
		t.Logf("--- cost per site-A prediction call at %.1fh horizon (10-call mean) ---", predHorizon/3600)
		for _, v := range soiEvalVariants() {
			start := time.Now()
			for i := 0; i < 10; i++ {
				predictEntry(w, cp, moon, predHorizon, v.tu, true)
			}
			t.Logf("  %8s: %7.2f ms/call", v.name, float64(time.Since(start).Microseconds())/10000)
		}
	}

	// Post-entry drawing: from the (exact, by construction) flown entry
	// state, integrate the in-SOI arc at the PRODUCTION sample budget and
	// compare each variant point-by-point against the converged "ref"
	// variant evaluated at the same sample times. Isolates the
	// hyperbolic-Verlet dt artifact at fixed sampling; the endpoint Δ is
	// the drawn impact/exit-point error. (An earlier dense-sampled
	// closest-approach version of this metric was useless: these
	// transfers impact, so the line correctly clamps at the surface, and
	// dense sampling shrinks the integration dt it was meant to measure.)
	tPeri := 2 * flownEntryState.R.Norm() / flownEntryState.V.Norm()
	drawSamples := adaptiveSampleCount(2*tPeri, orbitalPeriod(flownEntryState, moonMu))
	variantsAll := soiEvalVariants()
	refTu := soiEvalVariantByName("ref").tu
	refSegs, _ := w.predictedSegmentsFromTuned(flownEntryState, moon, flownEntryClock, 2*tPeri, drawSamples, refTu)
	refPts := flattenSegPoints(refSegs)
	t.Logf("--- post-entry drawing (from flown entry state, %d production samples, vs ref; analytic peri %.0f km, moon R %.0f km) ---",
		drawSamples, flownPeriKm, moon.RadiusMeters()/1000)
	for _, v := range variantsAll {
		if v.name == "ref" || v.name == "ref/2" {
			continue
		}
		segs, _ := w.predictedSegmentsFromTuned(flownEntryState, moon, flownEntryClock, 2*tPeri, drawSamples, v.tu)
		pts := flattenSegPoints(segs)
		n := len(pts)
		if len(refPts) < n {
			n = len(refPts)
		}
		maxD := 0.0
		for i := 0; i < n; i++ {
			if d := pts[i].Sub(refPts[i]).Norm() / 1000; d > maxD {
				maxD = d
			}
		}
		endD := math.NaN()
		if len(pts) > 0 && len(refPts) > 0 {
			endD = pts[len(pts)-1].Sub(refPts[len(refPts)-1]).Norm() / 1000
		}
		t.Logf("  %8s: max path divergence %8.1f km · endpoint Δ %8.1f km · %d pts (ref %d)",
			v.name, maxD, endD, len(pts), len(refPts))
	}
}

// flattenSegPoints concatenates all segment points in order — sample
// times line up index-wise across variants of the same call shape.
func flattenSegPoints(segs []SOISegment) []orbital.Vec3 {
	var pts []orbital.Vec3
	for _, s := range segs {
		pts = append(pts, s.Points...)
	}
	return pts
}

// swingKm is max−min over the checkpoints whose time-to-entry is below
// the window (NaN misses excluded); NaN when fewer than two qualify.
func swingKm(peris []float64, cps []soiEvalCheckpoint, window float64) float64 {
	lo, hi, n := math.Inf(1), math.Inf(-1), 0
	for i, p := range peris {
		if math.IsNaN(p) || cps[i].tToEntry > window {
			continue
		}
		n++
		lo, hi = math.Min(lo, p), math.Max(hi, p)
	}
	if n < 2 {
		return math.NaN()
	}
	return hi - lo
}

func variantIdx(vs []soiEvalVariant, name string) int {
	for i, v := range vs {
		if v.name == name {
			return i
		}
	}
	panic("unknown eval variant " + name)
}

func lastValid(peris []float64) float64 {
	for i := len(peris) - 1; i >= 0; i-- {
		if !math.IsNaN(peris[i]) {
			return peris[i]
		}
	}
	return math.NaN()
}
