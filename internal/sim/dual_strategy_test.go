package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSplitPlaneChangeSizing: the split's plane-change burn at apoapsis
// is ~zero for coplanar planes and grows with relative inclination —
// 2·v_apo·sin(Δi/2), the slow-apoapsis plane change.
func TestSplitPlaneChangeSizing(t *testing.T) {
	mu := 3.986004418e14
	rPark, rArr := 6.871e6, 384.399e6
	dep := physics.StateVector{R: orbital.Vec3{X: rPark}, V: orbital.Vec3{Y: math.Sqrt(mu / rPark)}}

	// Coplanar: both normals +Z → ~no plane change.
	dvFlat, _ := splitPlaneChangeAtApoapsis(dep, orbital.Vec3{Z: 1}, orbital.Vec3{Z: 1}, mu, rPark, rArr)
	if dvFlat > 1e-6 {
		t.Errorf("coplanar plane change should be ~0, got %.6f", dvFlat)
	}

	// 25° tilt: dv = 2·v_apo·sin(12.5°), and small in absolute terms
	// (apoapsis is slow) — the whole point of the split.
	nTilt := orbital.Vec3{Y: math.Sin(25 * math.Pi / 180), Z: math.Cos(25 * math.Pi / 180)}
	dv25, theta := splitPlaneChangeAtApoapsis(dep, orbital.Vec3{Z: 1}, nTilt, mu, rPark, rArr)
	aT := (rPark + rArr) / 2
	vApo := math.Sqrt(mu * (2/rArr - 1/aT))
	want := 2 * vApo * math.Sin(25.0/2*math.Pi/180)
	if math.Abs(dv25-want) > 1.0 {
		t.Errorf("25° plane change dv = %.2f, want %.2f", dv25, want)
	}
	if dv25 > 200 { // slow apoapsis → cheap; should be well under 200 m/s
		t.Errorf("apoapsis plane change unexpectedly expensive: %.1f m/s", dv25)
	}
	if math.Abs(theta) < 1e-3 {
		t.Errorf("expected a non-zero signed rotation angle, got %.6f", theta)
	}
}

// TestPlanTransferLunaPicksSplit: an equatorial-LEO → inclined-Luna
// auto-plant picks the split strategy (cheap apoapsis plane change),
// keeps the transfer affordable, records both candidate Δv totals, and
// plants three nodes including a BurnPlaneChange. This is the
// playability guarantee behind the dual-strategy (ADR 0005 decision 4).
func TestPlanTransferLunaPicksSplit(t *testing.T) {
	w := mustWorld(t)
	moonIdx := -1
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon missing from Sol")
	}
	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}

	if w.LastTransfer.Strategy != "split" {
		t.Errorf("strategy = %q, want split (Luna is ~25° off the LEO plane)", w.LastTransfer.Strategy)
	}
	if !(w.LastTransfer.SplitDv < w.LastTransfer.CombinedDv) {
		t.Errorf("split (%.0f) should be cheaper than combined (%.0f)",
			w.LastTransfer.SplitDv, w.LastTransfer.CombinedDv)
	}
	if w.LastTransfer.CombinedDv <= 0 || math.IsInf(w.LastTransfer.CombinedDv, 0) {
		t.Errorf("combined Δv not computed: %.0f", w.LastTransfer.CombinedDv)
	}
	// Playable: split total should fit a typical transfer-stage budget.
	if w.LastTransfer.SplitDv > 5000 {
		t.Errorf("split total Δv = %.0f m/s — too expensive to be playable", w.LastTransfer.SplitDv)
	}

	// Three planted nodes: prograde raise, BurnPlaneChange at apoapsis,
	// retrograde capture.
	nodes := w.ActiveCraft().Nodes
	if len(nodes) < 3 {
		t.Fatalf("split should plant 3 nodes (raise, plane change, capture); got %d", len(nodes))
	}
	if nodes[0].Mode != spacecraft.BurnPrograde {
		t.Errorf("first node should be the prograde raise, got %v", nodes[0].Mode)
	}
	foundPlaneChange := false
	for _, n := range nodes {
		if n.Mode == spacecraft.BurnPlaneChange {
			foundPlaneChange = true
		}
	}
	if !foundPlaneChange {
		t.Error("split must plant a BurnPlaneChange node at the transfer apoapsis")
	}
}
