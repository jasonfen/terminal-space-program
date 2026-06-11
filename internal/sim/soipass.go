package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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
	PerilunePoint  orbital.Vec3         // inertial, system-frame — marker position
	HasPerilunePt  bool                 // false when the arc couldn't place the marker point
	ArcSegments    []SOISegment         // foreign-SOI arc (PrimaryID == Body.ID), system-frame
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
// state, with no maneuver node required (ADR 0019 decisions A/B/C/E).
//
// A cheap apoapsis-reach guard runs first: a bound orbit reaches at most
// its apoapsis, so if that can't even reach within a sibling body's SOI of
// the body's closest approach to the primary, no encounter is possible —
// the call returns ok=false without forward-integrating, and a stable LEO
// pays nothing. When the guard passes, the live trajectory is scanned per
// reachable sibling (reusing scanTargetEncounter / the moon-frame
// targetPerilune so the readout agrees with the TARGET chip), the earliest
// SOI-entering pass wins, and its foreign-SOI arc is sampled via
// PredictedSegmentsFrom for drawing.
func (w *World) LiveSOIPass() (SOIPass, bool) {
	c := w.ActiveCraft()
	if c == nil || c.Landed {
		return SOIPass{}, false
	}
	mu := c.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
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
	// sim-day wall for an escape/hyperbolic leg.
	period := orbitalPeriod(c.State, mu)
	horizon := soiPassHyperbolicHorizon
	if bound && period > 0 && !math.IsNaN(period) && !math.IsInf(period, 0) {
		horizon = period
	}

	sys := w.System()
	now := w.Clock.SimTime

	// Scan every sibling body the orbit can geometrically reach; keep the
	// earliest SOI-entering pass.
	var best SOIPass
	bestTCA := math.Inf(1)
	found := false
	for _, b := range sys.Bodies {
		if b.ParentID != c.Primary.ID {
			continue // only siblings of the craft's primary have a sibling SOI
		}
		// Apoapsis-reach prune: the craft's farthest radius must reach
		// within the body's SOI of the body's closest approach to the
		// primary. Cheap geometry, no integration.
		bEl := orbital.ElementsFromBody(b)
		bodyPeri := bEl.A * (1 - bEl.E) // body's closest distance to the primary
		soi := physics.SOIRadius(b, c.Primary)
		if craftReach < bodyPeri-soi {
			continue
		}
		enc, ok := w.scanTargetEncounter(c.State, c.Primary, b, now, horizon)
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

	// Sample the live trajectory over the full bounded horizon and keep only
	// the body's own segments — these span the *whole* transit (entry →
	// perilune → exit), because PredictedSegmentsFrom rebases back out of
	// the SOI on exit, so any post-exit (escape / re-captured) samples land
	// in a different segment we drop here. Drawing the complete flyby (not
	// just the approach to perilune) reads as the single bright leg ADR
	// 0019 E calls for, while the period/sim-day horizon keeps it bounded
	// (ADR 0019 B: time-capped, ≤3 patches).
	samples := adaptiveSampleCount(horizon, period)
	segs := w.PredictedSegmentsFrom(c.State, c.Primary, now, horizon, samples)
	for _, s := range segs {
		if s.PrimaryID == best.Body.ID {
			best.ArcSegments = append(best.ArcSegments, s)
		}
	}

	// Perilune marker point: the foreign-arc sample closest to the body
	// centre at the predicted time of closest approach. The glyph marks
	// "which marker, what state" — the value lives in the chip (ADR 0020 C)
	// — so the nearest-sample approximation is sufficient for placement.
	moonAtTCA := w.BodyPositionAt(best.Body, now.Add(time.Duration(best.TimeToPerilune*float64(time.Second))))
	minD := math.Inf(1)
	for _, s := range best.ArcSegments {
		for _, p := range s.Points {
			if d := p.Sub(moonAtTCA).Norm(); d < minD {
				minD = d
				best.PerilunePoint = p
				best.HasPerilunePt = true
			}
		}
	}

	return best, true
}
