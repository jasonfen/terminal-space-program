package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// findMoon locates the Moon in the loaded Sol system (skips if absent).
func findMoon(t *testing.T, w *World) (int, bodies.CelestialBody) {
	t.Helper()
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			return i, b
		}
	}
	t.Skip("Moon missing from Sol")
	return -1, bodies.CelestialBody{}
}

// TestPlanTransferSplitArrivesAtMoon: the default LEO is ~19° off Luna's
// plane, so [H] auto-picks the split. Per ADR 0006 decision A the split
// must place its apoapsis on the line of nodes where Luna will be, so the
// transfer actually rendezvous. Asserted geometrically: the transfer
// apoapsis (reached at the capture node's time) lands within Luna's SOI.
// Pre-fix the plane change sat at an arbitrary apoapsis and missed by
// ~100 000 km (GH #67).
func TestPlanTransferSplitArrivesAtMoon(t *testing.T) {
	w := mustWorld(t)
	moonIdx, moon := findMoon(t, w)

	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	if w.LastTransfer.Strategy != "split" {
		t.Fatalf("strategy = %q, want split (default LEO is inclined to Luna)", w.LastTransfer.Strategy)
	}

	c := w.ActiveCraft()
	muShared := c.Primary.GravitationalParameter()
	nodes := c.Nodes
	if len(nodes) < 3 {
		t.Fatalf("split should plant 3 nodes; got %d", len(nodes))
	}
	depTime := nodes[0].TriggerTime
	arrTime := nodes[len(nodes)-1].TriggerTime
	waitSecs := depTime.Sub(w.Clock.SimTime).Seconds()

	// Transfer apoapsis lies 180° from the departure point in the craft's
	// plane, at the target's orbital radius. A plane change there doesn't
	// move it, so this is where the craft arrives regardless of strategy.
	depState, ok := physics.KeplerStep(c.State, muShared, waitSecs)
	if !ok {
		t.Fatalf("KeplerStep to departure failed")
	}
	rArrival := moon.SemimajorAxisMeters()
	apo := depState.R.Unit().Scale(-rArrival)

	lunaPos := w.BodyPositionAt(moon, arrTime).Sub(w.BodyPositionAt(c.Primary, arrTime))
	miss := apo.Sub(lunaPos).Norm()
	soi := physics.SOIRadius(moon, c.Primary)
	if miss > soi {
		t.Errorf("transfer apoapsis misses Luna by %.0f km (Luna SOI %.0f km) — split does not rendezvous",
			miss/1e3, soi/1e3)
	}
}

// TestPlanTransferSplitArrivesCoplanar: after the line-of-nodes plane
// change, the arrival orbit must be coplanar with Luna (≈0° relative
// inclination), so the capture inserts into a sane orbit rather than a
// badly tilted one (ADR 0006 decision A acceptance bar). The post-plane-
// change leg's orbital plane normal must align with Luna's.
func TestPlanTransferSplitArrivesCoplanar(t *testing.T) {
	w := mustWorld(t)
	moonIdx, moon := findMoon(t, w)

	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	if w.LastTransfer.Strategy != "split" {
		t.Fatalf("strategy = %q, want split", w.LastTransfer.Strategy)
	}

	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	_, nTargetHat, ok := w.craftTargetPlaneNormals(c, moon)
	if !ok {
		t.Fatal("craftTargetPlaneNormals degenerate")
	}

	// Coplanarity is a property of the Earth-relative transfer orbit at
	// arrival: propagate the raise leg to apoapsis (Earth frame) and apply
	// the plane-change impulse there, then compare the orbit's plane normal
	// to Luna's. (We can't read the predicted leg's stored state directly —
	// the predictor rebases into the Moon's frame on SOI entry, which
	// happens ~21 000 km out, before apoapsis; that state is Moon-relative
	// and not comparable to the Earth-frame Luna plane.)
	legs := w.PredictedLegs()
	raise := legs[0]
	apo, okk := physics.KeplerStep(raise.State, mu, raise.HorizonSecs)
	if !okk {
		t.Fatal("KeplerStep to apoapsis failed")
	}
	var pc spacecraft.ManeuverNode
	for _, n := range c.Nodes {
		if n.Mode == spacecraft.BurnPlaneChange {
			pc = n
		}
	}
	dir := spacecraft.NodeBurnDirection(pc, apo.R, apo.V)
	vNew := apo.V.Add(dir.Scale(pc.DV))
	normal := apo.R.Cross(vNew).Unit()
	relIncl := relInclination(normal, nTargetHat) * 180 / math.Pi
	if relIncl > 2.0 {
		t.Errorf("arrival orbit is %.2f° off Luna's plane (want ≈0°) — plane change not on the line of nodes", relIncl)
	}
}

// TestPlanTransferSplitPredictedPathEntersMoonSOI: end-to-end — with the
// node-aligned split, the predicted coast into apoapsis must cross Luna's
// SOI (a foreign "moon" segment present). This exercises the #66 Kepler
// predictor on the #67-corrected geometry: the dashed Projected Orbit
// actually draws the encounter for the default inclined Luna.
func TestPlanTransferSplitPredictedPathEntersMoonSOI(t *testing.T) {
	w := mustWorld(t)
	moonIdx, _ := findMoon(t, w)

	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs")
	}
	// The raise leg (node 0) coasts from departure to apoapsis, where Luna
	// is — so the encounter shows up there.
	leg := legs[0]
	segs := w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, leg.Samples)
	foundMoon := false
	ids := make([]string, len(segs))
	for i, s := range segs {
		ids[i] = s.PrimaryID
		if s.PrimaryID == "moon" {
			foundMoon = true
		}
	}
	if !foundMoon {
		t.Errorf("predicted raise leg never enters Luna's SOI (segments %v) — split misses the encounter", ids)
	}
}
