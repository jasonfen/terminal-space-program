package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// SOIPass is the predicted transit of the live, *unburned* trajectory
// through a sibling Body's sphere of influence (ADR 0019). It is computed
// always-on from the active craft's live state and is independent of the
// Target slot — KSP shows the encounter whether or not the body is
// targeted, and so do we.
type SOIPass struct {
	Body           bodies.CelestialBody // body whose SOI the live path crosses
	PeriluneRadius float64              // distance to Body centre at closest approach (m)
	TimeToPerilune float64              // seconds from now to perilune
	Impact         bool                 // perilune radius is below the Body surface
	PeriluneRel    orbital.Vec3         // body-relative offset of perilune from Body centre (Local-to-Body, ADR 0021 B)
	HasPerilunePt  bool                 // false when the arc couldn't place the marker point
	ArcSegments    []SOISegment         // foreign-SOI arc (PrimaryID == Body.ID); draw via SegmentDrawPoints
}

// PerilunePosition returns the Perilune marker's canvas position under the
// Local-to-Body Arc rule (ADR 0021 B): the pass Body's CURRENT position plus
// the body-relative perilune offset — the same anchoring SegmentDrawPoints
// gives the arc, so the marker rides the drawn hyperbola. As arrival nears
// the Body's current position closes on its encounter position, so the
// marker converges to the true perilune.
func (w *World) PerilunePosition(p SOIPass) orbital.Vec3 {
	return w.BodyPosition(p.Body).Add(p.PeriluneRel)
}

// PeriluneAltitude is the perilune radius above the Body's surface; negative
// means the trajectory impacts.
func (p SOIPass) PeriluneAltitude() float64 {
	return p.PeriluneRadius - p.Body.RadiusMeters()
}

// soiPassHyperbolicHorizon caps the forward-prediction window for an escape
// / hyperbolic live orbit at one sim-day — an unbound path never loops, so
// there is no period to bound it (ADR 0019 B).
const soiPassHyperbolicHorizon = 24 * 3600.0

// LiveSOIPass computes the active craft's upcoming SOI pass from its live
// state, with no maneuver node required (ADR 0019 decisions A/B/C/E). This
// is the always-on, Target-independent pass the canvas draws bright and the
// SOI PASS chip reads when nothing is planted.
func (w *World) LiveSOIPass() (SOIPass, bool) {
	c := w.ActiveCraft()
	if c == nil || c.Landed {
		return SOIPass{}, false
	}
	return w.soiPassFromState(c.State, c.Primary, w.Clock.SimTime, math.Inf(1))
}

// CounterfactualSOIPass is the dual-arc "no-burn" pass (ADR 0019 D): the
// live trajectory's upcoming pass, but capped at the first planted node so
// the counterfactual is never predicted — or drawn — past the burn the
// craft will actually make. With no node planted it is identical to
// LiveSOIPass. ok=false when the first node is already due (cap ≤ 0).
func (w *World) CounterfactualSOIPass() (SOIPass, bool) {
	c := w.ActiveCraft()
	if c == nil || c.Landed {
		return SOIPass{}, false
	}
	maxHorizon := math.Inf(1)
	if t, ok := firstNodeTime(c.Nodes); ok {
		maxHorizon = t.Sub(w.Clock.SimTime).Seconds()
		if maxHorizon <= 0 {
			return SOIPass{}, false
		}
	}
	return w.soiPassFromState(c.State, c.Primary, w.Clock.SimTime, maxHorizon)
}

// PlannedSOIPass is the dual-arc "planned" pass (ADR 0019 D, bright path):
// the SOI pass of the node-modified trajectory, scanned from the post-burn
// legs so the player sees the safe periapsis their burns produce against
// the no-burn Impact. Returns false with no node planted, or when the
// planned path reaches no SOI. TimeToPerilune is rebased to now — the legs
// begin when their node fires, in the future.
func (w *World) PlannedSOIPass() (SOIPass, bool) {
	c := w.ActiveCraft()
	if c == nil || len(c.Nodes) == 0 {
		return SOIPass{}, false
	}
	now := w.Clock.SimTime
	var best SOIPass
	bestTCA := math.Inf(1)
	found := false
	for _, leg := range w.PredictedLegs() {
		pass, ok := w.soiPassFromState(leg.State, leg.Primary, leg.StartClock, math.Inf(1))
		if !ok {
			continue
		}
		pass.TimeToPerilune += leg.StartClock.Sub(now).Seconds()
		if pass.TimeToPerilune < bestTCA {
			bestTCA = pass.TimeToPerilune
			best = pass
			found = true
		}
	}
	return best, found
}

// bestSOIPass returns the most relevant upcoming SOI pass for framing: the
// planned (node-modified) pass when nodes are planted — the bright path the
// burns actually produce — else the live pass. The encounter-aware
// Framing-Event fit (FocusZoomRadius, ADR 0021 F) reads it so focusing the
// pass Body fits to SOI scale, even while flying a planted transfer whose
// *pre-burn* orbit can't yet reach the body (in which case LiveSOIPass alone
// is false). Runs a forward prediction — call it at Framing Events, never per
// frame (the Camera Contract retired the per-frame framers that used to).
func (w *World) bestSOIPass() (SOIPass, bool) {
	if p, ok := w.PlannedSOIPass(); ok {
		return p, true
	}
	return w.LiveSOIPass()
}

// firstNodeTime returns the earliest planted-node trigger time.
func firstNodeTime(nodes []spacecraft.ManeuverNode) (time.Time, bool) {
	if len(nodes) == 0 {
		return time.Time{}, false
	}
	first := nodes[0].TriggerTime
	for _, n := range nodes[1:] {
		if n.TriggerTime.Before(first) {
			first = n.TriggerTime
		}
	}
	return first, true
}

// soiPassFromState is the shared core behind LiveSOIPass /
// CounterfactualSOIPass / PlannedSOIPass: the upcoming SOI pass of a
// trajectory starting at `state` in `primary`'s frame at `fromClock`,
// scanned and sampled out to at most maxHorizon seconds.
//
// A cheap apoapsis-reach guard runs first: a bound orbit reaches at most its
// apoapsis, so if that can't even reach within a sibling body's SOI of the
// body's closest approach to the primary, no encounter is possible — it
// returns ok=false without forward-integrating, and a stable LEO pays
// nothing. The period/sim-day window is the natural horizon; maxHorizon
// tightens it for the node-capped counterfactual. When the guard passes,
// the trajectory is scanned per reachable sibling (reusing
// scanTargetEncounter / the moon-frame targetPerilune so the readout agrees
// with the TARGET chip), the earliest SOI-entering pass wins, and its
// foreign-SOI arc is sampled via PredictedSegmentsFrom for drawing.
func (w *World) soiPassFromState(state physics.StateVector, primary bodies.CelestialBody, fromClock time.Time, maxHorizon float64) (SOIPass, bool) {
	mu := primary.GravitationalParameter()
	el := orbital.ElementsFromState(state.R, state.V, mu)
	if math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return SOIPass{}, false
	}

	// A bound orbit (a>0) reaches at most its apoapsis; an unbound orbit
	// reaches arbitrarily far, so it skips the geometric prune entirely.
	bound := el.A > 0
	craftReach := math.Inf(1)
	if bound {
		craftReach = el.Apoapsis()
	}

	// Forward-prediction horizon: ~one orbital period for a bound orbit
	// (the encounter sits within the next revolution, ADR 0019 B); a
	// sim-day wall for an escape/hyperbolic leg; tightened by maxHorizon
	// (the counterfactual's first-node cap).
	period := orbitalPeriod(state, mu)
	horizon := soiPassHyperbolicHorizon
	if bound && period > 0 && !math.IsNaN(period) && !math.IsInf(period, 0) {
		horizon = period
	}
	if maxHorizon < horizon {
		horizon = maxHorizon
	}
	if horizon <= 0 {
		return SOIPass{}, false
	}

	sys := w.System()

	// Scan every sibling body the orbit can geometrically reach; keep the
	// earliest SOI-entering pass.
	var best SOIPass
	bestTCA := math.Inf(1)
	found := false
	for _, b := range sys.Bodies {
		if b.ParentID != primary.ID {
			continue // only siblings of `primary` have a sibling SOI
		}
		// Apoapsis-reach prune: the craft's farthest radius must reach
		// within the body's SOI of the body's closest approach to the
		// primary. Cheap geometry, no integration.
		bEl := orbital.ElementsFromBody(b)
		bodyPeri := bEl.A * (1 - bEl.E) // body's closest distance to the primary
		soi := physics.SOIRadius(b, primary)
		if craftReach < bodyPeri-soi {
			continue
		}
		enc, ok := w.scanTargetEncounter(state, primary, b, fromClock, horizon)
		if !ok || !enc.EntersSOI {
			continue
		}
		if enc.TCA < bestTCA {
			bestTCA = enc.TCA
			best = SOIPass{
				Body:           b,
				PeriluneRadius: enc.Dist,
				TimeToPerilune: enc.TCA,
				Impact:         enc.Dist < b.RadiusMeters(),
			}
			found = true
		}
	}
	if !found {
		return SOIPass{}, false
	}

	// Sample the trajectory over the bounded horizon and keep only the
	// body's own segments — these span the transit (entry → perilune →
	// exit), because PredictedSegmentsFrom rebases back out of the SOI on
	// exit, so any post-exit (escape / re-captured) samples land in a
	// segment we drop here. A node-capped horizon naturally truncates the
	// arc at the burn (ADR 0019 D: counterfactual never drawn past the node).
	samples := adaptiveSampleCount(horizon, period)
	segs := w.PredictedSegmentsFrom(state, primary, fromClock, horizon, samples)
	for _, s := range segs {
		if s.PrimaryID == best.Body.ID {
			best.ArcSegments = append(best.ArcSegments, s)
		}
	}

	// Perilune marker offset: the foreign-arc sample closest to the body
	// centre. RelPoints are body-relative at each sample's clock, so the
	// minimum-norm sample IS the drawn closest approach (Local-to-Body,
	// ADR 0021 B) — the draw site anchors it at the Body's current
	// position via PerilunePosition. The glyph marks "which marker, what
	// state" — the value lives in the chip (ADR 0020 C) — so the
	// nearest-sample approximation is sufficient for placement.
	minD := math.Inf(1)
	for _, s := range best.ArcSegments {
		for _, r := range s.RelPoints {
			if d := r.Norm(); d < minD {
				minD = d
				best.PeriluneRel = r
				best.HasPerilunePt = true
			}
		}
	}

	return best, true
}
