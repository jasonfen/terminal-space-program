package screens

// orbit_predict_cache_test.go — pins the predict-on-change cache that
// keeps the SOI-fidelity predictors (ADR 0017, v0.17.2) off the per-frame
// render path. drawNodes plots node markers + dashed legs from
// cachedPredictedRender, which recomputes only when (craft, nodes, clock-
// bucket) changes. predictCacheComputes counts misses.

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestPredictRenderCacheReuse: re-rendering with nothing changed must NOT
// re-run the predictors (the dashed orbit is identical), while a node edit
// or a clock jump past the bucket must. This is the whole point of ADR
// 0017 decision C — paused / non-tick frames stop paying the ~10 ms/call
// site-A cost.
func TestPredictRenderCacheReuse(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft from NewWorld")
	}
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{DV: 100, TriggerTime: w.Clock.SimTime.Add(15 * time.Minute)},
		spacecraft.ManeuverNode{DV: 50, TriggerTime: w.Clock.SimTime.Add(45 * time.Minute)},
	)

	// First render computes once.
	v.Render(w, 0, 180, 48)
	if got := v.predictCacheComputes; got != 1 {
		t.Fatalf("first render: predictCacheComputes = %d, want 1", got)
	}

	// Re-render with nothing changed: pure cache hit, no recompute.
	v.Render(w, 0, 180, 48)
	v.Render(w, 0, 180, 48)
	if got := v.predictCacheComputes; got != 1 {
		t.Errorf("re-render with no change recomputed: predictCacheComputes = %d, want 1 (cache should hit)", got)
	}

	// Editing a node's Δv changes the predicted trajectory: must recompute.
	c.Nodes[0].DV = 250
	v.Render(w, 0, 180, 48)
	if got := v.predictCacheComputes; got != 2 {
		t.Errorf("node edit did not bust the cache: predictCacheComputes = %d, want 2", got)
	}

	// Advancing the clock well past the bucket (bodies have moved; the
	// prediction differs) must recompute.
	w.Clock.SimTime = w.Clock.SimTime.Add(24 * time.Hour)
	v.Render(w, 0, 180, 48)
	if got := v.predictCacheComputes; got != 3 {
		t.Errorf("clock jump did not bust the cache: predictCacheComputes = %d, want 3", got)
	}
}

// TestPredictRenderCacheBustsOnThrust: a change to the live orbit that is
// NOT clock-derived — a burn / RCS pulse altering R/V within one bucket —
// must bust the cache, or the dashed overlay would freeze stale while the
// player thrusts (warp is ~1× during a burn, so clockBucket alone won't
// catch it). The element quanta in the key are conserved under coast but
// move the instant the orbit changes, so this holds without a per-thrust-
// source special case.
func TestPredictRenderCacheBustsOnThrust(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{DV: 100, TriggerTime: w.Clock.SimTime.Add(20 * time.Minute)},
	)

	v.Render(w, 0, 180, 48)
	if got := v.predictCacheComputes; got != 1 {
		t.Fatalf("first render: predictCacheComputes = %d, want 1", got)
	}

	// Simulate a prograde burn: bump velocity ~50 m/s, raising the orbit,
	// WITHOUT advancing the clock or touching the nodes. The orbital
	// elements change, so the cache must miss.
	c.State.V = c.State.V.Add(c.State.V.Unit().Scale(50))
	v.Render(w, 0, 180, 48)
	if got := v.predictCacheComputes; got != 2 {
		t.Errorf("orbit change (thrust) did not bust the cache: predictCacheComputes = %d, want 2 (overlay would freeze stale mid-burn)", got)
	}
}

// TestPredictClockBucketTolerance: the clock bucket must be wide enough
// that a low-warp coast reuses the prediction (a frame-or-three of sim
// time stays in one bucket), yet a large advance lands in a new bucket.
// This is the staleness-tolerance knob that lets the cache help during
// active flight, not just while paused.
func TestPredictClockBucketTolerance(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.ActiveCraft() == nil {
		t.Fatal("expected an active craft from NewWorld")
	}

	bucketNanos := v.predictClockBucketNanos(w)
	if bucketNanos <= int64(time.Second) {
		t.Fatalf("bucket %v is not wider than 1 s — no low-warp reuse window", time.Duration(bucketNanos))
	}

	// Align to a bucket start so a half-bucket step provably stays inside.
	base := w.Clock.SimTime
	aligned := time.Unix(0, (base.UnixNano()/bucketNanos)*bucketNanos)
	within := aligned.Add(time.Duration(bucketNanos / 2))
	beyond := aligned.Add(time.Duration(2 * bucketNanos))

	b0 := v.predictRenderKeyAt(w, aligned).clockBucket
	bWithin := v.predictRenderKeyAt(w, within).clockBucket
	bBeyond := v.predictRenderKeyAt(w, beyond).clockBucket

	if bWithin != b0 {
		t.Errorf("a half-bucket clock advance changed the bucket (%d → %d) — reuse window too tight", b0, bWithin)
	}
	if bBeyond == b0 {
		t.Errorf("a two-bucket clock advance stayed in the same bucket (%d) — cache would go stale", b0)
	}
}
