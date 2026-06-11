package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// setupKernCursor places the active craft in a coplanar circular parking
// orbit around Kern (Lumen system) with Cursor as the transfer target.
// Returns the Cursor body index. Skips if Lumen/Kern/Cursor are absent.
func setupKernCursor(t *testing.T, w *World) (cursorIdx int, kern, cursor bodies.CelestialBody) {
	t.Helper()
	sysIdx := -1
	for i, s := range w.Systems {
		if s.Name == "Lumen" {
			sysIdx = i
		}
	}
	if sysIdx < 0 {
		t.Skip("Lumen not loaded")
	}
	w.SystemIdx = sysIdx
	craft := w.ActiveCraft()
	craft.SystemIdx = sysIdx
	sys := w.System()

	kernIdx := -1
	cursorIdx = -1
	for i, b := range sys.Bodies {
		switch b.EnglishName {
		case "Kern":
			kernIdx = i
		case "Cursor":
			cursorIdx = i
		}
	}
	if kernIdx < 0 || cursorIdx < 0 {
		t.Skip("Kern/Cursor not in Lumen")
	}
	kern, cursor = sys.Bodies[kernIdx], sys.Bodies[cursorIdx]

	rPark := kern.RadiusMeters() * 1.08
	if atm := kern.Atmosphere; atm != nil {
		if min := kern.RadiusMeters() + atm.CutoffAltitude + 50e3; min > rPark {
			rPark = min
		}
	}
	craft.Primary = kern
	craft.State.R, craft.State.V = moonPlaneCircularState(kern, cursor, rPark)
	return cursorIdx, kern, cursor
}

// TestCoplanarCaptureFiresAtPerilune is the regression for the Kern→Cursor
// flight bug: the combined (fused-Lambert) transfer's retrograde capture
// burn used to fire at the nominal Lambert arrival epoch — the moon-*centre*
// crossing in Kern's frame — which sits ~26 min after the true perilune,
// because the craft accelerates into Cursor's SOI and swings through
// periapsis early. refineCombinedCapture rebases the SOI-entry state into
// Cursor's frame and fires the capture at the analytic hyperbolic perilune.
//
// The oracle is a live patched-conic coast: fly the post-departure leg with
// the production integrator and record the time of true closest approach to
// Cursor. The planted capture must land within a few minutes of it (pre-fix
// gap ≈ 26 min; the residual is the analytic-vs-integrated approximation
// over a multi-hour transfer).
func TestCoplanarCaptureFiresAtPerilune(t *testing.T) {
	w := mustWorld(t)
	cursorIdx, _, cursor := setupKernCursor(t, w)

	if _, err := w.PlanTransfer(cursorIdx); err != nil {
		t.Fatalf("PlanTransfer(Cursor): %v", err)
	}
	if w.LastTransfer.Strategy != "combined" {
		t.Fatalf("strategy = %q, want combined (Kern→Cursor is coplanar)", w.LastTransfer.Strategy)
	}
	c := w.ActiveCraft()
	now := w.Clock.SimTime
	var capture *spacecraft.ManeuverNode
	for i := range c.Nodes {
		if c.Nodes[i].Mode == spacecraft.BurnRetrograde {
			capture = &c.Nodes[i]
		}
	}
	if capture == nil {
		t.Fatalf("no retrograde capture node among %d planted nodes", len(c.Nodes))
	}
	captureOffset := capture.TriggerTime.Sub(now)

	// Oracle: live-fly the post-departure coast with the production
	// integrator and record the time of minimum craft↔Cursor distance.
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs")
	}
	leg := legs[0]
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	c.Nodes = nil

	const chunkSecs = 30.0
	horizon := leg.HorizonSecs*1.6 + 86400
	minD := math.Inf(1)
	var tCAFromStart float64
	elapsed := 0.0
	for elapsed < horizon {
		cursorRelPrimary := w.BodyPositionAt(cursor, w.Clock.SimTime).Sub(w.BodyPositionAt(c.Primary, w.Clock.SimTime))
		d := c.State.R.Sub(cursorRelPrimary).Norm()
		if c.Primary.ID == cursor.ID {
			d = c.State.R.Norm() // already rebased into Cursor's frame
		}
		if d < minD {
			minD = d
			tCAFromStart = elapsed
		}
		dt := time.Duration(chunkSecs * float64(time.Second))
		w.Clock.SimTime = w.Clock.SimTime.Add(dt)
		w.integrateOneCraft(c, dt)
		elapsed += chunkSecs
	}
	trueCAOffset := leg.StartClock.Add(time.Duration(tCAFromStart * float64(time.Second))).Sub(now)

	gap := captureOffset - trueCAOffset
	if gap < 0 {
		gap = -gap
	}
	const tol = 5 * time.Minute
	if gap > tol {
		t.Errorf("capture fires %v from true perilune (capture off=%v, true CA off=%v); want within %v",
			gap, captureOffset, trueCAOffset, tol)
	}
}

// TestPredictedTargetApproachMatchesFlight: the live TARGET readout
// (PredictedTargetApproach) must report the same encounter the craft
// actually flies — it crosses Cursor's SOI, the perilune radius is a sane
// in-SOI value, and the predicted time-to-closest-approach matches the
// live-flown closest approach within a few minutes. This is the readout the
// player reads while hand-flying a correction, so it has to track reality.
func TestPredictedTargetApproachMatchesFlight(t *testing.T) {
	w := mustWorld(t)
	cursorIdx, kern, cursor := setupKernCursor(t, w)

	if _, err := w.PlanTransfer(cursorIdx); err != nil {
		t.Fatalf("PlanTransfer(Cursor): %v", err)
	}
	// Target Cursor so the readout resolves it.
	w.SetTargetBody(cursorIdx)

	ap, ok := w.PredictedTargetApproach()
	if !ok {
		t.Fatal("PredictedTargetApproach returned ok=false")
	}
	if !ap.EntersSOI {
		t.Errorf("readout says the transfer misses Cursor's SOI; want an encounter")
	}
	soi := physics.SOIRadius(cursor, kern)
	if ap.Dist <= 0 || ap.Dist > soi {
		t.Errorf("perilune radius %.1f km outside (0, SOI=%.1f km]", ap.Dist/1e3, soi/1e3)
	}

	// Oracle: live-fly the post-departure coast and time the true closest
	// approach, then compare the readout's TCA to it.
	c := w.ActiveCraft()
	now := w.Clock.SimTime
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs")
	}
	leg := legs[0]
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	c.Nodes = nil

	const chunkSecs = 30.0
	horizon := leg.HorizonSecs*1.6 + 86400
	minD := math.Inf(1)
	var tCAFromStart float64
	elapsed := 0.0
	for elapsed < horizon {
		cursorRel := w.BodyPositionAt(cursor, w.Clock.SimTime).Sub(w.BodyPositionAt(c.Primary, w.Clock.SimTime))
		d := c.State.R.Sub(cursorRel).Norm()
		if c.Primary.ID == cursor.ID {
			d = c.State.R.Norm()
		}
		if d < minD {
			minD = d
			tCAFromStart = elapsed
		}
		dt := time.Duration(chunkSecs * float64(time.Second))
		w.Clock.SimTime = w.Clock.SimTime.Add(dt)
		w.integrateOneCraft(c, dt)
		elapsed += chunkSecs
	}
	trueCAOffset := leg.StartClock.Add(time.Duration(tCAFromStart * float64(time.Second))).Sub(now).Seconds()

	gap := math.Abs(ap.TCA - trueCAOffset)
	if gap > 300 {
		t.Errorf("readout TCA %.0fs vs live closest approach %.0fs (gap %.0fs); want within 300s",
			ap.TCA, trueCAOffset, gap)
	}
}

// TestCombinedTransferArrivesSafePeriapsis (ADR 0018): the planted combined
// Kern→Cursor transfer must arrive at the Capture Orbit radius (Cursor
// radius + 200 km), not the body centre — a safe periapsis above the
// surface — and prograde (moon-frame angular momentum aligned with Cursor's
// orbit normal). Live-flown with the production integrator: pre-ADR-0018 the
// min distance to Cursor's centre clamped at the surface (impact).
func TestCombinedTransferArrivesSafePeriapsis(t *testing.T) {
	w := mustWorld(t)
	cursorIdx, kern, cursor := setupKernCursor(t, w)
	if _, err := w.PlanTransfer(cursorIdx); err != nil {
		t.Fatalf("PlanTransfer(Cursor): %v", err)
	}
	if w.LastTransfer.Strategy != "combined" {
		t.Fatalf("strategy = %q, want combined", w.LastTransfer.Strategy)
	}
	c := w.ActiveCraft()
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs")
	}
	leg := legs[0]
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	c.Nodes = nil

	const chunkSecs = 30.0
	horizon := leg.HorizonSecs*1.6 + 86400
	minD := math.Inf(1)
	var relRAtMin, relVAtMin orbital.Vec3
	elapsed := 0.0
	for elapsed < horizon {
		cursorRel := w.BodyPositionAt(cursor, w.Clock.SimTime).Sub(w.BodyPositionAt(c.Primary, w.Clock.SimTime))
		rel := c.State.R.Sub(cursorRel)
		if c.Primary.ID == cursor.ID {
			rel = c.State.R // already Cursor-frame
		}
		if d := rel.Norm(); d < minD {
			minD = d
			relRAtMin = rel
			cursorVel := w.bodyInertialVelocityAt(cursor, w.Clock.SimTime).Sub(w.bodyInertialVelocityAt(c.Primary, w.Clock.SimTime))
			relVAtMin = c.State.V.Sub(cursorVel)
		}
		dt := time.Duration(chunkSecs * float64(time.Second))
		w.Clock.SimTime = w.Clock.SimTime.Add(dt)
		w.integrateOneCraft(c, dt)
		elapsed += chunkSecs
	}

	rCapture := cursor.RadiusMeters() + 200e3
	t.Logf("live perilune = %.1f km (Capture Orbit radius %.1f km, Cursor radius %.1f km)",
		minD/1e3, rCapture/1e3, cursor.RadiusMeters()/1e3)
	if minD <= cursor.RadiusMeters() {
		t.Errorf("transfer still impacts Cursor: perilune %.1f km ≤ radius %.1f km", minD/1e3, cursor.RadiusMeters()/1e3)
	}
	// Within 25% of the Capture Orbit radius (analytic aim vs integrated flight).
	if math.Abs(minD-rCapture) > 0.25*rCapture {
		t.Errorf("perilune %.1f km not near Capture Orbit radius %.1f km", minD/1e3, rCapture/1e3)
	}
	// Prograde capture: moon-frame angular momentum aligned with Cursor's
	// orbit normal about Kern.
	hRel := relRAtMin.Cross(relVAtMin)
	cursorOrbitNormal := w.BodyPosition(cursor).Sub(w.BodyPosition(kern)).Cross(
		w.bodyInertialVelocityAt(cursor, w.Clock.SimTime).Sub(w.bodyInertialVelocityAt(kern, w.Clock.SimTime)))
	if hRel.Dot(cursorOrbitNormal) <= 0 {
		t.Errorf("capture is retrograde (h_rel·n_target = %.3g ≤ 0); want prograde", hRel.Dot(cursorOrbitNormal))
	}
}

// TestSplitArrivalPeriapsisCharacterization (ADR 0018 D): measures the
// split (inclined) transfer's actual arrival periapsis. ADR 0018 scoped the
// capture-aim fix to the combined path; this characterizes whether the split
// also arrives at the target's centre (a collision). It asserts only that an
// encounter resolves and logs the perilune — a sub-surface value is the
// signal that the split needs the same offset-aim treatment (logged as a
// follow-up, not fixed here: the split is entangled with #67/#68).
func TestSplitArrivalPeriapsisCharacterization(t *testing.T) {
	w := mustWorld(t)
	moonIdx, moon := findMoon(t, w)
	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer(Moon): %v", err)
	}
	if w.LastTransfer.Strategy != "split" {
		t.Skipf("strategy = %q, want split — default LEO must be inclined to Luna", w.LastTransfer.Strategy)
	}
	c := w.ActiveCraft()
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs")
	}
	leg := legs[0]
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	c.Nodes = nil

	const chunkSecs = 60.0
	horizon := leg.HorizonSecs*1.6 + 86400
	minD := math.Inf(1)
	elapsed := 0.0
	for elapsed < horizon {
		moonRel := w.BodyPositionAt(moon, w.Clock.SimTime).Sub(w.BodyPositionAt(c.Primary, w.Clock.SimTime))
		d := c.State.R.Sub(moonRel).Norm()
		if c.Primary.ID == moon.ID {
			d = c.State.R.Norm()
		}
		if d < minD {
			minD = d
		}
		dt := time.Duration(chunkSecs * float64(time.Second))
		w.Clock.SimTime = w.Clock.SimTime.Add(dt)
		w.integrateOneCraft(c, dt)
		elapsed += chunkSecs
	}
	alt := minD - moon.RadiusMeters()
	t.Logf("split arrival perilune = %.1f km (Luna radius %.1f km, altitude %.1f km)",
		minD/1e3, moon.RadiusMeters()/1e3, alt/1e3)
	if math.IsInf(minD, 1) {
		t.Fatal("split transfer never reached Luna — characterization unusable")
	}
	if alt <= 0 {
		t.Logf("FOLLOW-UP (ADR 0018 D): the split also arrives sub-surface (impact) — it needs the same capture-aim offset; not fixed here (entangled with #67/#68)")
	}
}

// TestPredictedTargetApproachDuringCoast: the readout must still report the
// Cursor encounter once the departure burn has fired and the craft is
// coasting toward the moon — the case the original legs-only scan went blind
// on (the approach coast is no longer a primary-frame PredictedLegs leg, so
// nothing was scanned and the chip showed nothing). Regression for the
// "I don't see peri: IMPACT" playtest report.
func TestPredictedTargetApproachDuringCoast(t *testing.T) {
	w := mustWorld(t)
	cursorIdx, _, _ := setupKernCursor(t, w)
	if _, err := w.PlanTransfer(cursorIdx); err != nil {
		t.Fatalf("PlanTransfer(Cursor): %v", err)
	}
	w.SetTargetBody(cursorIdx)
	c := w.ActiveCraft()

	// Advance onto the post-departure transfer leg and drop the (now-fired)
	// departure node, leaving only the retrograde capture — the coasting
	// state the player hand-flies in.
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs")
	}
	leg := legs[0]
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	var capture []spacecraft.ManeuverNode
	for _, n := range c.Nodes {
		if n.Mode == spacecraft.BurnRetrograde {
			capture = append(capture, n)
		}
	}
	c.Nodes = capture

	ap, ok := w.PredictedTargetApproach()
	if !ok {
		t.Fatal("ok=false during the coast — the readout went blind (the reported bug)")
	}
	if !ap.EntersSOI {
		t.Errorf("readout missed the Cursor encounter during the coast (Dist=%.0f km)", ap.Dist/1e3)
	}
}
