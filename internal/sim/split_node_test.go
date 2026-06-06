package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSplitFallbackPlaneChangeOffsetSeparatesStacked (GH #88 finding #3):
// in the degenerate split fallback (no SOI encounter resolved, so
// transferEncounterTimes can't place the plane change at SOI entry) the
// plane-change and capture offsets both default to the arrival offset.
// splitFallbackPlaneChangeOffset must pull the plane change strictly
// earlier — and clear of the capture's burn window — so the two finite
// burns are ordered and non-overlapping rather than stacked at one
// instant. It must leave an already-distinct pair (the working found==
// SOI-entry path) untouched.
func TestSplitFallbackPlaneChangeOffsetSeparatesStacked(t *testing.T) {
	const (
		capture  = 100 * time.Minute
		pcDur    = 30 * time.Second
		capDur   = 50 * time.Second
		earliest = 10 * time.Minute // end of the departure burn
	)

	// Stacked: equal offsets must separate into an ordered, non-overlapping pair.
	got := splitFallbackPlaneChangeOffset(capture, capture, pcDur, capDur, earliest)
	if got >= capture {
		t.Fatalf("stacked offset not pulled earlier: got %v, capture %v", got, capture)
	}
	if gap := capture - got; gap < pcDur/2+capDur/2 {
		t.Errorf("burn windows overlap: gap %v < half-burn sum %v", gap, pcDur/2+capDur/2)
	}
	if got <= earliest {
		t.Errorf("plane change %v not after the departure burn end %v", got, earliest)
	}

	// Distinct (found==true path): returned unchanged.
	const pcOff = 90 * time.Minute
	if got := splitFallbackPlaneChangeOffset(pcOff, capture, pcDur, capDur, earliest); got != pcOff {
		t.Errorf("distinct offset was rewritten: got %v, want %v", got, pcOff)
	}

	// Clamp: a transfer too short to fit the full separation after the
	// departure burn clamps to earliest, still strictly before capture.
	shortCap := earliest + 5*time.Second
	if got := splitFallbackPlaneChangeOffset(shortCap, shortCap, pcDur, capDur, earliest); got != earliest {
		t.Errorf("clamp to departure end: got %v, want %v", got, earliest)
	}
	if earliest >= shortCap {
		t.Fatalf("test invariant broken: earliest %v must precede capture %v", earliest, shortCap)
	}
}

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

	// Coplanarity is a property of the Earth-relative transfer orbit after
	// the plane change. The plane change fires just before SOI entry, in
	// the Earth frame (the raise leg's horizon ends at that node); apply it
	// to the propagated raise-leg state there and compare the orbit's plane
	// normal to Luna's. (We can't read the predicted leg's stored state
	// directly — the predictor rebases into the Moon's frame on SOI entry,
	// so that state is Moon-relative and not comparable to the Earth-frame
	// Luna plane.) Firing off-node only *reduces* the tilt — but near
	// apoapsis the residual is small.
	legs := w.PredictedLegs()
	raise := legs[0]
	atPC, okk := physics.KeplerStep(raise.State, mu, raise.HorizonSecs)
	if !okk {
		t.Fatal("KeplerStep to plane-change point failed")
	}
	var pc spacecraft.ManeuverNode
	for _, n := range c.Nodes {
		if n.Mode == spacecraft.BurnPlaneChange {
			pc = n
		}
	}
	dir := spacecraft.NodeBurnDirection(pc, atPC.R, atPC.V)
	vNew := atPC.V.Add(dir.Scale(pc.DV))
	normal := atPC.R.Cross(vNew).Unit()
	relIncl := relInclination(normal, nTargetHat) * 180 / math.Pi
	if relIncl > 2.0 {
		t.Errorf("arrival orbit is %.2f° off Luna's plane after the plane change (want <2°)", relIncl)
	}
}

// TestPlanTransferSplitBurnsAreDistinct: the plane change and capture
// must be planted at distinct times — the plane change in the Earth frame
// before SOI entry, the capture at perilune. Pre-fix both were stacked at
// the apoapsis instant, which the executor can't fly as two burns (the
// later ActiveBurn clobbers the earlier). GH #67 follow-up.
func TestPlanTransferSplitBurnsAreDistinct(t *testing.T) {
	w := mustWorld(t)
	moonIdx, _ := findMoon(t, w)
	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	if w.LastTransfer.Strategy != "split" {
		t.Fatalf("strategy = %q, want split", w.LastTransfer.Strategy)
	}
	nodes := w.ActiveCraft().Nodes
	if len(nodes) < 3 {
		t.Fatalf("split should plant 3 nodes; got %d", len(nodes))
	}
	var planeChange, capture spacecraft.ManeuverNode
	for _, n := range nodes {
		switch n.Mode {
		case spacecraft.BurnPlaneChange:
			planeChange = n
		case spacecraft.BurnRetrograde:
			capture = n
		}
	}
	gap := capture.TriggerTime.Sub(planeChange.TriggerTime)
	if gap <= 0 {
		t.Errorf("plane change (T=%v) and capture (T=%v) are not ordered distinctly — burns collide",
			planeChange.TriggerTime, capture.TriggerTime)
	}
	// The plane change must fire before the capture by more than their
	// burn windows so the integrator doesn't run them concurrently.
	if gap < planeChange.Duration/2+capture.Duration/2 {
		t.Errorf("plane change and capture burn windows overlap (gap %v < %v)",
			gap, planeChange.Duration/2+capture.Duration/2)
	}
}

// TestPlanTransferSplitPredictedPathEntersMoonSOI: end-to-end — with the
// node-aligned split, the predicted transfer must cross Luna's SOI (a
// foreign "moon" segment present on some leg). This exercises the #66
// Kepler predictor on the #67-corrected geometry: the dashed Projected
// Orbit actually draws the encounter for the default inclined Luna.
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
	// The encounter shows up on whichever leg coasts through apoapsis (the
	// plane change splits the coast at SOI entry), so scan all of them —
	// either a leg already rebased into the Moon's frame or a "moon"
	// segment in a leg's predicted trajectory counts.
	foundMoon := false
	for _, leg := range legs {
		if leg.Primary.ID == "moon" {
			foundMoon = true
			break
		}
		for _, s := range w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, leg.Samples) {
			if s.PrimaryID == "moon" {
				foundMoon = true
			}
		}
	}
	if !foundMoon {
		t.Error("predicted transfer never enters Luna's SOI — split misses the encounter")
	}
}
