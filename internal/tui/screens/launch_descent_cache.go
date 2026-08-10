package screens

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// launch_descent_cache.go — the predict-on-change cache (ADR 0017) for
// the descent corridor's expensive half (issue #377): the integrated
// stop-burn forecast (sim.PredictPoweredStop) and the "burn at"
// latest-safe-start search (sim.PredictBurnAt). Mirrors orbit.go's
// predictCache / soiPassCache pattern verbatim — a keyed cache on the
// View, recomputing only when the key changes, with a …Computes counter
// tests can assert against.
//
// A 1.6 km/s stop is ~800 s of integrated flight, and PredictBurnAt's
// bisection multiplies that by roughly another order of magnitude
// (issue #377 §5) — running both every render frame would reproduce the
// exact idle-CPU regression #363 fixed for the orbit map's own
// predictors, except worse (this is on screen exactly when the player is
// at close zoom during a landing, i.e. NOT idle). sim.DescentCorridorFor
// itself stays cheap and uncached (see its doc comment) — only the two
// powered forecasts live behind this cache.

// descentStopRenderCache holds the last computed stop-forecast + burn-at
// result and the key it was computed for.
type descentStopRenderCache struct {
	has bool
	key descentStopRenderKey
	dat descentStopRenderData
}

// descentStopRenderData is what cachedDescentStop returns: the merged
// powered-stop + burn-at + derived-alarm state ready to fold into a
// sim.DescentCorridor for rendering.
type descentStopRenderData struct {
	stop      sim.PoweredStopPrediction
	stopOK    bool
	burnAt    sim.BurnAtCue
	hasBurnAt bool
}

// descentStopRenderKey fingerprints what the stop forecast depends on:
// the active craft + its primary, position/velocity/mass/fuel
// (quantized), and a clock bucket that bounds staleness during an
// unpowered ballistic coast — where position and velocity change too
// slowly on their own to bust a tight quantum every frame. midBurn is
// its own key field (not inferred from the velocity quantum) so a
// burn start/stop transition busts the cache on the very next render
// regardless of whether the craft has moved enough yet to do it any
// other way — "burn at hides once the burn is under way" (issue #377)
// has to be immediate, not eventually-consistent.
//
// The position/velocity quantum is tight enough (descentStopVelQuantumMps)
// that an ACTIVE burn — which changes velocity every physics tick —
// busts the cache on essentially every tick too, which is exactly what
// "stop margin stays live through the burn" (issue #377 §3) requires:
// only the coast case benefits from the cache's staleness tolerance.
type descentStopRenderKey struct {
	craftID     uint64
	primaryID   string
	clockBucket int64
	midBurn     bool
	rQ          [3]int64
	vQ          [3]int64
	massQ       int64
	fuelQ       int64
}

const (
	// descentStopPosQuantumM / descentStopVelQuantumMps size the position
	// / velocity quanta the cache key rounds to. Tight enough that any
	// thrust (which changes velocity far faster than free-fall changes
	// position) busts the cache almost immediately; loose enough that an
	// unpowered ballistic coast — falling under gravity alone, no thrust
	// — reuses the forecast for a meaningful stretch of frames.
	descentStopPosQuantumM   = 25.0
	descentStopVelQuantumMps = 0.5
	// descentStopMassQuantumKg quantizes both total mass and active-stage
	// fuel — either changing (a burn, or a decouple) has to bust the
	// cache.
	descentStopMassQuantumKg = 1.0
	// descentStopClockBucket is the staleness ceiling during an unpowered
	// coast, matching the spirit of orbit.go's predictClockBucketNanos
	// but fixed rather than period-scaled: the descent corridor's horizon
	// is bounded (DescentPredictHorizon, 30 min) regardless of the live
	// orbit's own period, so there's no orbital timescale to derive a
	// bucket from the way the node-leg predictor has one.
	descentStopClockBucket = time.Second
)

// descentStopKeyFor builds the cache key for the active craft at clock t.
func descentStopKeyFor(c *spacecraft.Spacecraft, t time.Time) descentStopRenderKey {
	return descentStopRenderKey{
		craftID:     c.ID,
		primaryID:   c.Primary.ID,
		clockBucket: t.UnixNano() / int64(descentStopClockBucket),
		midBurn:     sim.StackMidBurn(c),
		rQ: [3]int64{
			quantize(c.State.R.X, descentStopPosQuantumM),
			quantize(c.State.R.Y, descentStopPosQuantumM),
			quantize(c.State.R.Z, descentStopPosQuantumM),
		},
		vQ: [3]int64{
			quantize(c.State.V.X, descentStopVelQuantumMps),
			quantize(c.State.V.Y, descentStopVelQuantumMps),
			quantize(c.State.V.Z, descentStopVelQuantumMps),
		},
		massQ: quantize(c.TotalMass(), descentStopMassQuantumKg),
		fuelQ: quantize(c.ActiveStageFuel(), descentStopMassQuantumKg),
	}
}

// cachedDescentStop returns the stop-forecast + burn-at cue for the
// active descending craft, recomputing (sim.PredictPoweredStop, and
// sim.PredictBurnAt when the burn isn't already under way) only when
// descentStopKeyFor's key changes (ADR 0017 predict-on-change). Callers
// must already have established the craft is descending
// (sim.DescentCorridorFor's own gate) — this does not re-check.
func (v *LaunchView) cachedDescentStop(w *sim.World, c *spacecraft.Spacecraft) descentStopRenderData {
	key := descentStopKeyFor(c, w.Clock.SimTime)
	if v.descentStopCache.has && v.descentStopCache.key == key {
		return v.descentStopCache.dat
	}
	var dat descentStopRenderData
	dat.stop, dat.stopOK = sim.PredictPoweredStop(c, sim.DescentPredictHorizon)
	// "burn at" hides once the burn is under way (issue #377 §2) — and
	// there's no point paying for the ~8-10x-more-expensive search for a
	// row that won't render.
	if !sim.StackMidBurn(c) {
		dat.burnAt, dat.hasBurnAt = sim.PredictBurnAt(c, sim.DescentPredictHorizon)
	}
	v.descentStopCache = descentStopRenderCache{has: true, key: key, dat: dat}
	v.descentStopCacheComputes++
	return dat
}
