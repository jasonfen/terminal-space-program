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
//
// The in-SOI residence variant (#157) reuses the same shape after SOI
// entry: while the craft sits inside a non-root Body's SOI on a trajectory
// that leaves it, Body is the craft's *current* primary, HasEntry is false
// (the crossing is in the past), and OnwardSegments carries the post-exit
// continuation — so the ring/arc/marker pipeline keeps drawing through the
// transit instead of switching off at the boundary.
type SOIPass struct {
	Body           bodies.CelestialBody // body whose SOI the live path crosses
	PeriluneRadius float64              // distance to Body centre at closest approach (m)
	TimeToPerilune float64              // seconds from now to perilune
	Impact         bool                 // perilune radius is below the Body surface
	PeriluneRel    orbital.Vec3         // body-relative offset of perilune from Body centre (Local-to-Body, ADR 0021 B)
	HasPerilunePt  bool                 // false when the arc couldn't place the marker point
	EntryRel       orbital.Vec3         // body-relative offset of the arc's SOI-entry ring crossing (ADR 0021 C)
	ExitRel        orbital.Vec3         // body-relative offset of the SOI-exit ring crossing
	HasEntry       bool                 // false when the arc's first sample isn't on the SOI Ring
	HasExit        bool                 // false when the arc never exits — impact, horizon-truncated, node-capped
	TimeToEntry    float64              // seconds from now to SOI entry (the SOI PASS chip's T-entry readout)
	HasEntryTime   bool                 // false when the predictor reported no entry transition for the Body
	ArcSegments    []SOISegment         // foreign-SOI arc (PrimaryID == Body.ID); draw via SegmentDrawPoints
	OnwardSegments []SOISegment         // in-SOI residence pass only (#157): the post-exit continuation (parent / heliocentric legs); nil for sibling passes
}

// soiRingCrossingTol is the ring-proximity gate for the SOI Entry / Exit
// markers, as a fraction of the SOI radius: the predictor opens a foreign
// segment with a sample rebased at the (bisection-refined) crossing and
// closes it with one at the crossing back out, so a genuine ring crossing
// sits on the boundary to well within this; an arc that ends in the
// interior — surface impact, horizon truncation, the node-capped
// counterfactual — misses it and draws no Exit marker.
const soiRingCrossingTol = 0.05

// PerilunePosition returns the Perilune marker's canvas position under the
// Local-to-Body Arc rule (ADR 0021 B): the pass Body's CURRENT position plus
// the body-relative perilune offset — the same anchoring SegmentDrawPoints
// gives the arc, so the marker rides the drawn hyperbola. As arrival nears
// the Body's current position closes on its encounter position, so the
// marker converges to the true perilune.
func (w *World) PerilunePosition(p SOIPass) orbital.Vec3 {
	return w.BodyPosition(p.Body).Add(p.PeriluneRel)
}

// EntryPosition returns the SOI Entry marker's canvas position under the
// Local-to-Body Arc rule (ADR 0021 C): the pass Body's CURRENT position plus
// the body-relative ring-crossing offset — the same anchoring
// SegmentDrawPoints gives the arc and PerilunePosition gives the Perilune,
// so the glyph rides the drawn ring crossing. Callers gate on HasEntry.
func (w *World) EntryPosition(p SOIPass) orbital.Vec3 {
	return w.BodyPosition(p.Body).Add(p.EntryRel)
}

// ExitPosition is EntryPosition for the SOI Exit crossing; gate on HasExit.
func (w *World) ExitPosition(p SOIPass) orbital.Vec3 {
	return w.BodyPosition(p.Body).Add(p.ExitRel)
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

// soiArcSamples is the point count for the analytic encounter arc (ADR 0023
// D). The integrated pass carries only the few uniform-time samples that land
// inside the SOI — a shallow flyby catches a handful, which the render-side
// gap-fill connects into a visible polygon. Redrawing the foreign-SOI arc
// from the body-relative conic at this density makes the hyperbola a smooth
// curve at any zoom. Pure conic trig per point (no ephemeris), computed only
// when the pass changes (the predict-on-change cache), so the density is cheap.
const soiArcSamples = 96

// LiveSOIPass computes the active craft's upcoming SOI pass from its live
// state, with no maneuver node required (ADR 0019 decisions A/B/C/E). This
// is the always-on, Target-independent pass the canvas draws bright and the
// SOI PASS chip reads when nothing is planted.
//
// When the sibling scan finds nothing AND the craft sits inside a non-root
// body's SOI on a trajectory that leaves it, the in-SOI residence pass
// takes over (#157) — the encounter picture used to switch off at the
// exact moment of SOI entry, because the body had just become the primary
// and so stopped being a sibling.
func (w *World) LiveSOIPass() (SOIPass, bool) {
	c := w.ActiveCraft()
	if c == nil || c.Landed {
		return SOIPass{}, false
	}
	if p, ok := w.soiPassFromState(c.State, c.Primary, w.Clock.SimTime, math.Inf(1)); ok {
		return p, true
	}
	return w.inSOIEscapePass(c.State, c.Primary, w.Clock.SimTime, math.Inf(1))
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
	if p, ok := w.soiPassFromState(c.State, c.Primary, w.Clock.SimTime, maxHorizon); ok {
		return p, true
	}
	// In-SOI residence fallback (#157), node-capped like the sibling scan:
	// with a capture burn planted the dim no-burn arc shows the escape the
	// craft flies if it doesn't fire, truncated at the node.
	return w.inSOIEscapePass(c.State, c.Primary, w.Clock.SimTime, maxHorizon)
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
		// Rebase the leg-relative clocks to now — the legs begin when their
		// node fires, in the future.
		offset := leg.StartClock.Sub(now).Seconds()
		pass.TimeToPerilune += offset
		if pass.HasEntryTime {
			pass.TimeToEntry += offset
		}
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
	var bestRelEl orbital.Elements
	bestHasRelEl := false
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
				PeriluneRel:    enc.PeriluneRel,
				HasPerilunePt:  enc.HasPeriluneRel,
			}
			bestRelEl, bestHasRelEl = enc.RelEl, enc.HasRelEl
			found = true
		}
	}
	if !found {
		return SOIPass{}, false
	}

	// Sample the trajectory over the bounded horizon and keep only the
	// body's own segments — these span the transit (entry → perilune →
	// exit), because the predictor rebases back out of the SOI on
	// exit, so any post-exit (escape / re-captured) samples land in a
	// segment we drop here. A node-capped horizon naturally truncates the
	// arc at the burn (ADR 0019 D: counterfactual never drawn past the node).
	// The tuned call (same production tuning PredictedSegmentsFrom wraps)
	// also reports each SOI transition, so the pass carries the predicted
	// entry clock for the chip without re-deriving it from sampled points.
	samples := adaptiveSampleCount(horizon, period)
	segs, entries := w.predictedSegmentsFromTuned(state, primary, fromClock, horizon, samples, defaultPredictTuning())
	for _, s := range segs {
		if s.PrimaryID == best.Body.ID {
			best.ArcSegments = append(best.ArcSegments, s)
		}
	}
	for _, e := range entries {
		if e.BodyID == best.Body.ID {
			best.TimeToEntry = e.Clock.Sub(fromClock).Seconds()
			best.HasEntryTime = true
			break
		}
	}

	// SOI Entry / Exit ring crossings (ADR 0021 C): the arc's first and
	// last body-relative samples are the predictor's rebase points at the
	// SOI boundary, so when they sit on the parent-relative ring (within
	// soiRingCrossingTol) they place the Entry / Exit marker glyphs. soi
	// is parent-relative by construction — the sibling scan above only
	// admits bodies whose ParentID is `primary`.
	if soi := physics.SOIRadius(best.Body, primary); soi > 0 && len(best.ArcSegments) > 0 {
		onRing := func(rel orbital.Vec3) bool {
			return math.Abs(rel.Norm()-soi) <= soi*soiRingCrossingTol
		}
		if first := best.ArcSegments[0].RelPoints; len(first) > 0 && onRing(first[0]) {
			best.EntryRel = first[0]
			best.HasEntry = true
		}
		if last := best.ArcSegments[len(best.ArcSegments)-1].RelPoints; len(last) > 0 && onRing(last[len(last)-1]) {
			best.ExitRel = last[len(last)-1]
			best.HasExit = true
		}
	}

	// Perilune marker offset: the analytic closest approach (rp along the
	// periapsis direction, set on best above from targetPerilune) is the
	// primary placement — it matches the chip's PeriluneRadius and rides the
	// drawn hyperbola's true bottom instead of snapping to a sample that
	// re-phases each prediction (ADR 0023 B). Fall back to the nearest
	// in-arc sample only when the relative hyperbola was degenerate and no
	// analytic direction was available.
	if !best.HasPerilunePt {
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
	}

	// Analytic encounter arc (ADR 0023 D): the integrated arc above carries
	// only the few uniform-time samples that fall inside the SOI — a shallow
	// flyby catches a handful, which the render-side gap-fill then connects
	// with straight chords into a visible polygon (the faceted-perilune
	// report). Redraw the foreign-SOI arc from the same body-relative conic
	// that places the analytic perilune (decision B), sampled densely by
	// eccentric anomaly between the integrated arc's entry and exit. The
	// integrated endpoints set the extent, so impact / node-cap / horizon
	// truncation carry over unchanged; only the shape between them becomes the
	// smooth conic, with the marker sitting exactly on its drawn bottom.
	if bestHasRelEl && len(best.ArcSegments) > 0 {
		first := best.ArcSegments[0].RelPoints
		lastSeg := best.ArcSegments[len(best.ArcSegments)-1].RelPoints
		if len(first) > 0 && len(lastSeg) > 0 {
			nuIn := orbital.TrueAnomalyAt(bestRelEl, first[0])
			nuOut := orbital.TrueAnomalyAt(bestRelEl, lastSeg[len(lastSeg)-1])
			if rel := orbital.SampleConicArc(bestRelEl, nuIn, nuOut, soiArcSamples); len(rel) >= 2 {
				// Points must stay the segment's contracted "inertial position
				// at each sample's clock": the body keeps moving through the
				// transit, so each conic sample's true inertial position uses
				// the body's position at that sample's flyby time (periapsis
				// clock + the analytic time-from-periapsis at its ν). The drawn
				// arc still rebases RelPoints onto the body's CURRENT position
				// (SegmentDrawPoints, ADR 0021 B); honest Points just keep the
				// field true for any inertial-frame reader.
				muT := best.Body.GravitationalParameter()
				periClock := fromClock.Add(time.Duration(best.TimeToPerilune * float64(time.Second)))
				pts := make([]orbital.Vec3, len(rel))
				for i, r := range rel {
					tau := orbital.SecsFromPeriapsisAt(bestRelEl, orbital.TrueAnomalyAt(bestRelEl, r), muT)
					sampleClock := periClock.Add(time.Duration(tau * float64(time.Second)))
					pts[i] = w.BodyPositionAt(best.Body, sampleClock).Add(r)
				}
				best.ArcSegments = []SOISegment{{
					PrimaryID: best.Body.ID,
					Points:    pts,
					RelPoints: rel,
				}}
			}
		}
	}

	return best, true
}

// inSOIEscapePass synthesizes the in-SOI residence variant of the SOI Pass
// (#157): while `state` sits inside a non-root `primary`'s SOI on a
// trajectory that LEAVES it — hyperbolic/parabolic (e ≥ 1, a ≤ 0) or bound
// with apoapsis at/past the parent-relative SOI radius — the encounter
// picture must not switch off at SOI entry. The pass it returns feeds the
// same ring/arc/marker pipeline as a sibling pass: Body is the current
// primary, ArcSegments is the remaining in-SOI leg (entry is in the past,
// so HasEntry stays false), and OnwardSegments carries the post-exit
// continuation into the parent / heliocentric frames.
//
// A captured orbit (bound, apoapsis inside the SOI) returns ok=false — the
// quiet case: a parked LEO or low lunar orbit draws its ellipse exactly as
// before, with no ring. maxHorizon caps the prediction at the first
// planted node for the counterfactual, as in soiPassFromState.
func (w *World) inSOIEscapePass(state physics.StateVector, primary bodies.CelestialBody, fromClock time.Time, maxHorizon float64) (SOIPass, bool) {
	soi := w.BodySOIRadius(primary)
	if soi <= 0 {
		return SOIPass{}, false // root primary — no enclosing SOI to leave
	}
	mu := primary.GravitationalParameter()
	el := orbital.ElementsFromState(state.R, state.V, mu)
	if math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return SOIPass{}, false
	}
	if el.A > 0 && el.E < 1 && el.Apoapsis() < soi {
		return SOIPass{}, false // captured — bound wholly inside the SOI, the quiet case
	}

	// Horizon: ~one period for a bound-but-escaping orbit (it crosses the
	// ring before apoapsis); the sim-day wall for an open trajectory
	// (ADR 0019 B). A slow flyby's remaining transit can far exceed that
	// wall — a lunar arrival spends ~2 days inside a 66 000 km SOI at
	// ~1 km/s relative — and a horizon-truncated arc would end in the
	// interior with no Exit and no onward path, so the horizon extends to
	// the analytic exit crossing (+25 %, so the onward continuation gets
	// ink too). maxHorizon (the counterfactual's first-node cap) clamps
	// last: the no-burn arc is never drawn past the burn (ADR 0019 D).
	period := orbitalPeriod(state, mu)
	horizon := soiPassHyperbolicHorizon
	if el.A > 0 && period > 0 && !math.IsNaN(period) && !math.IsInf(period, 0) {
		horizon = period
	}
	if tExit, ok := orbital.TimeToRadiusOutbound(orbital.Vec3State{R: state.R, V: state.V}, mu, soi); ok {
		if h := tExit * 1.25; h > horizon {
			horizon = h
		}
	}
	if maxHorizon < horizon {
		horizon = maxHorizon
	}
	if horizon <= 0 {
		return SOIPass{}, false
	}

	pass := SOIPass{Body: primary}

	// Closest approach: analytic while the periapsis is still ahead
	// (inbound, r·v < 0); once outbound the craft only recedes, so the
	// closest *future* approach is the current radius, now.
	if state.R.Dot(state.V) < 0 {
		pass.PeriluneRadius = el.A * (1 - el.E) // periapsis radius, valid both sides of e=1
		pass.Impact = pass.PeriluneRadius < primary.RadiusMeters()
		rel := orbital.Vec3State{R: state.R, V: state.V}
		if el.E >= 1 {
			if t, ok := orbital.TimeToPeriapsisHyperbolic(rel, mu); ok && t > 0 {
				pass.TimeToPerilune = t
			}
		} else if t := orbital.TimeToPeriapsis(rel, mu); t > 0 {
			pass.TimeToPerilune = t
		}
	} else {
		pass.PeriluneRadius = state.R.Norm()
	}

	// Sample the remaining transit plus the onward continuation. Unlike
	// the sibling scan, segments past the exit are KEPT (OnwardSegments):
	// the whole path the craft will fly draws, with or without a capture
	// burn planted (#157).
	samples := adaptiveSampleCount(horizon, period)
	segs, _ := w.predictedSegmentsFromTuned(state, primary, fromClock, horizon, samples, defaultPredictTuning())
	for _, s := range segs {
		if s.PrimaryID == primary.ID {
			pass.ArcSegments = append(pass.ArcSegments, s)
		} else {
			pass.OnwardSegments = append(pass.OnwardSegments, s)
		}
	}
	if len(pass.ArcSegments) == 0 {
		return SOIPass{}, false
	}

	// SOI Exit ring crossing (ADR 0021 C): the in-SOI leg's last sample is
	// the predictor's (bisection-refined) rebase point at the boundary, so
	// when it sits on the parent-relative ring it places the Exit marker;
	// an arc that ends in the interior — impact, node-capped,
	// horizon-truncated — draws no Exit. No Entry marker: the craft is
	// already inside, the entry crossing is in the past.
	if last := pass.ArcSegments[0].RelPoints; len(last) > 0 {
		if r := last[len(last)-1]; math.Abs(r.Norm()-soi) <= soi*soiRingCrossingTol {
			pass.ExitRel = r
			pass.HasExit = true
		}
	}

	// Perilune marker offset: the analytic closest approach, matching the
	// chip's PeriluneRadius above (ADR 0023 B). Inbound (r·v < 0) periapsis
	// is ahead, at rp along the periapsis direction; outbound it is behind,
	// so the closest *future* approach is the current position (consistent
	// with PeriluneRadius = |state.R| in that branch). Both are body-relative
	// — the frame RelPoints share. Fall back to the nearest in-arc sample
	// only if the periapsis direction is undefined (near-circular).
	if state.R.Dot(state.V) >= 0 {
		pass.PeriluneRel = state.R
		pass.HasPerilunePt = true
	} else if dir, ok := orbital.PeriapsisDirection(state.R, state.V, mu); ok {
		pass.PeriluneRel = dir.Scale(pass.PeriluneRadius)
		pass.HasPerilunePt = true
	}
	if !pass.HasPerilunePt {
		minD := math.Inf(1)
		for _, s := range pass.ArcSegments {
			for _, r := range s.RelPoints {
				if d := r.Norm(); d < minD {
					minD = d
					pass.PeriluneRel = r
					pass.HasPerilunePt = true
				}
			}
		}
	}
	return pass, true
}
