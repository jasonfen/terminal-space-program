package sim

// soi_entry_fix_test.go — CI regression pins for the v0.17.2 SOI-entry
// prediction fix (ADR 0017): both predictor sites now default to
// defaultPredictTuning() (BodyInterp + RefineCrossing + CoastSubStepCap=
// 120). These tests assert the entry-time *stability* the flip buys —
// red on the legacy zero value, green on the default — complementing the
// diagnostic eval harness (soi_entry_prediction_eval_test.go), which is
// skipped in normal runs. They share that file's predictEntry helper and
// soiEvalCheckpoint type (same package).

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// flyMoonApproach plants a coplanar LEO→Luna transfer, live-flies its
// coast leg with the production integrator (integrateOneCraft), and
// returns the flown SOI-entry clock plus checkpoints sampled along the
// way. Trimmed sibling of the eval harness setup: one Earth→Moon coast,
// no per-variant fan-out, so it's fast enough for CI. The checkpoints are
// the player's actual mid-coast states — predicting forward from each is
// exactly what the dashed Projected Orbit and the capture preview do as
// the craft drifts in.
func flyMoonApproach(t *testing.T) (w *World, moon bodies.CelestialBody, flownEntry time.Time, cps []soiEvalCheckpoint) {
	t.Helper()
	w = mustWorld(t)
	leg := coplanarLEOTowardMoon(t, w)

	for _, b := range w.System().Bodies {
		if b.EnglishName == "Moon" {
			moon = b
		}
	}
	if moon.ID == "" {
		t.Skip("Moon not in loaded Sol system")
	}

	craft := w.ActiveCraft()
	w.Clock.SimTime = leg.StartClock
	craft.State = leg.State
	craft.Primary = leg.Primary
	craft.Nodes = nil

	const chunkSecs = 120.0
	horizon := leg.HorizonSecs*1.6 + 86400
	cpEvery := leg.HorizonSecs / 24
	nextCp, elapsed := 0.0, 0.0
	for elapsed < horizon && craft.Primary.ID != moon.ID {
		if elapsed >= nextCp {
			cps = append(cps, soiEvalCheckpoint{state: craft.State, primary: craft.Primary, clock: w.Clock.SimTime})
			nextCp += cpEvery
		}
		d := time.Duration(chunkSecs * float64(time.Second))
		w.Clock.SimTime = w.Clock.SimTime.Add(d)
		w.integrateOneCraft(craft, d)
		elapsed += chunkSecs
	}
	if craft.Primary.ID != moon.ID {
		t.Skipf("live coast never entered Moon SOI within %.1f d — planner missed; scenario unusable", horizon/86400)
	}
	flownEntry = w.Clock.SimTime

	// Drop checkpoints inside the final chunk before entry (no coast left
	// to predict) and stamp each one's time-to-entry.
	kept := cps[:0]
	for _, cp := range cps {
		tte := flownEntry.Sub(cp.clock).Seconds()
		if tte <= chunkSecs {
			continue
		}
		cp.tToEntry = tte
		kept = append(kept, cp)
	}
	return w, moon, flownEntry, kept
}

// TestDefaultPredictTuningIsFixVariant pins the production default
// (defaultPredictTuning) equal to the SOI-entry eval harness's "fix"
// variant — the converged candidate the attribution selected. If either
// drifts (a knob added to one but not the other), this fails, so the
// harness's "fix" column always reflects what production actually runs.
func TestDefaultPredictTuningIsFixVariant(t *testing.T) {
	got := defaultPredictTuning()
	want := soiEvalVariantByName("fix").tu
	if got != want {
		t.Fatalf("defaultPredictTuning() = %+v, want fix-variant %+v — keep them in sync", got, want)
	}
}

// TestPredictDefaultsRouteThroughFixKnobs locks the two production
// wrappers to defaultPredictTuning(): PredictedSegmentsFrom and
// propagateStateWithPrimary must produce results identical to their
// *Tuned siblings called with the default profile. Guards against a
// future edit reverting either wrapper to the legacy zero value.
func TestPredictDefaultsRouteThroughFixKnobs(t *testing.T) {
	w := mustWorld(t)
	leg := coplanarLEOTowardMoon(t, w)

	// Site A: segment IDs + every point must match the default-tuned call.
	wantSegs, _ := w.predictedSegmentsFromTuned(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, leg.Samples, defaultPredictTuning())
	gotSegs := w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, leg.Samples)
	if len(gotSegs) != len(wantSegs) {
		t.Fatalf("PredictedSegmentsFrom routed through wrong tuning: %d segments, want %d", len(gotSegs), len(wantSegs))
	}
	for i := range gotSegs {
		if gotSegs[i].PrimaryID != wantSegs[i].PrimaryID {
			t.Errorf("segment %d primary = %s, want %s", i, gotSegs[i].PrimaryID, wantSegs[i].PrimaryID)
		}
		if len(gotSegs[i].Points) != len(wantSegs[i].Points) {
			t.Errorf("segment %d point count = %d, want %d", i, len(gotSegs[i].Points), len(wantSegs[i].Points))
			continue
		}
		for j := range gotSegs[i].Points {
			if gotSegs[i].Points[j] != wantSegs[i].Points[j] {
				t.Errorf("segment %d point %d = %v, want %v (wrapper not on default tuning)", i, j, gotSegs[i].Points[j], wantSegs[i].Points[j])
				break
			}
		}
	}

	// Site B: end state + primary must match the default-tuned call.
	wantState, wantPrimary, _ := w.propagateStateWithPrimaryTuned(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, defaultPredictTuning())
	gotState, gotPrimary := w.propagateStateWithPrimary(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs)
	if gotPrimary.ID != wantPrimary.ID {
		t.Errorf("propagateStateWithPrimary primary = %s, want %s", gotPrimary.ID, wantPrimary.ID)
	}
	if gotState.R != wantState.R || gotState.V != wantState.V {
		t.Errorf("propagateStateWithPrimary state mismatch: got R=%v V=%v, want R=%v V=%v", gotState.R, gotState.V, wantState.R, wantState.V)
	}
}

// TestPredictedEntryTimeStableSiteA: the dashed Projected Orbit must
// predict the SAME lunar SOI-entry clock from successive points along the
// approach. Pre-v0.17.2 the segment predictor tested the SOI against
// per-sample-stale body positions, so the predicted entry time swung by
// thousands of seconds as the craft coasted in (the playtest symptom: the
// encounter wouldn't hold still to fine-tune against). The flip to
// defaultPredictTuning (interpolated bodies + bisected crossing) holds it
// flat. Red on legacy, green on the default. (Site A of the v0.17.1 eval
// harness; warp-orbit-accuracy.md §2026-06-09.)
func TestPredictedEntryTimeStableSiteA(t *testing.T) {
	w, moon, flown, cps := flyMoonApproach(t)
	if len(cps) < 3 {
		t.Skipf("only %d usable checkpoints; need ≥3 to measure entry-time swing", len(cps))
	}

	entrySwingSecs := func(tu predictTuning) (swing float64, n int) {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, cp := range cps {
			e, ok := predictEntry(w, cp, moon, cp.tToEntry+6*3600, tu, true)
			if !ok {
				continue
			}
			off := e.Clock.Sub(flown).Seconds()
			lo, hi, n = math.Min(lo, off), math.Max(hi, off), n+1
		}
		return hi - lo, n
	}

	legacySwing, nLegacy := entrySwingSecs(predictTuning{})
	defaultSwing, nDefault := entrySwingSecs(defaultPredictTuning())
	if nLegacy < 3 || nDefault < 3 {
		t.Skipf("too few predictions reached the Moon (legacy %d, default %d)", nLegacy, nDefault)
	}
	t.Logf("site-A entry-time swing over %d checkpoints: legacy %.0f s → default %.0f s", nDefault, legacySwing, defaultSwing)

	// Sanity: the legacy path must actually wobble, or this guard could
	// not catch a revert. Baseline (warp-orbit-accuracy.md §2026-06-09)
	// is several thousand seconds of swing across the coast.
	if legacySwing < 300 {
		t.Fatalf("legacy entry-time swing %.0f s is unexpectedly small — the regression guard would not catch a revert (default %.0f s)", legacySwing, defaultSwing)
	}
	// The fix holds the entry clock within live-chunk quantization.
	if defaultSwing > 180 {
		t.Errorf("default-tuned site-A entry-time swing %.0f s across %d checkpoints; want ≤180 s (legacy was %.0f s)", defaultSwing, nDefault, legacySwing)
	}
}

// TestPropagateEntryTimeBiasGoneSiteB: the node-chain propagator (feeds
// ArrivalCapturePreview) must predict the lunar SOI-entry clock without a
// growing bias as the craft nears arrival. Pre-v0.17.2 this site coasted
// with an uncapped period/100 sub-step and no post-rebase re-resolution,
// so SOI detection quantized to hours and the predicted entry time ran
// +6720 s late at ~2 h out. CoastSubStepCap=120 + bisected crossing
// (defaultPredictTuning) removes it. Red on legacy, green on the default.
func TestPropagateEntryTimeBiasGoneSiteB(t *testing.T) {
	w, moon, flown, cps := flyMoonApproach(t)
	if len(cps) == 0 {
		t.Skip("no checkpoints from the lunar approach")
	}

	// Pick the checkpoint nearest ~2 h before entry — the worst legacy
	// case in the baseline table.
	cp, best, ok := cps[0], math.Inf(1), false
	for _, c := range cps {
		if d := math.Abs(c.tToEntry - 2*3600); d < best {
			best, cp, ok = d, c, true
		}
	}
	if !ok {
		t.Skip("no checkpoints from the lunar approach")
	}

	legacyE, okL := predictEntry(w, cp, moon, cp.tToEntry+6*3600, predictTuning{}, false)
	defaultE, okD := predictEntry(w, cp, moon, cp.tToEntry+6*3600, defaultPredictTuning(), false)
	if !okL || !okD {
		t.Skipf("site-B prediction missed the Moon (legacy=%v default=%v)", okL, okD)
	}
	legacyBias := legacyE.Clock.Sub(flown).Seconds()
	defaultBias := defaultE.Clock.Sub(flown).Seconds()
	t.Logf("site-B entry-time bias at %.1f h out: legacy %+.0f s → default %+.0f s", cp.tToEntry/3600, legacyBias, defaultBias)

	// Sanity: legacy must carry a large bias here, or the guard is moot.
	if math.Abs(legacyBias) < 1500 {
		t.Fatalf("legacy site-B entry-time bias %.0f s at %.1f h out is unexpectedly small — guard would miss a revert (default %.0f s)", legacyBias, cp.tToEntry/3600, defaultBias)
	}
	if math.Abs(defaultBias) > 600 {
		t.Errorf("default-tuned site-B entry-time bias %+.0f s at %.1f h out; want ≤600 s (legacy was %+.0f s)", defaultBias, cp.tToEntry/3600, legacyBias)
	}
}
