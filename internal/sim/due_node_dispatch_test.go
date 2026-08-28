package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestExecuteDueNodesFiniteBurnNotOverwritten — GH #88 (#1, HIGH).
// When two finite nodes both come due in the same tick, the dispatcher
// must start the *earlier* one's ActiveBurn and leave the later one
// queued — not overwrite ActiveBurn with the second node and silently
// drop the first node's Δv. Pre-fix the loop set c.ActiveBurn for node
// A, kept iterating, overwrote it with node B, and popped both — so
// node A's burn never executed.
func TestExecuteDueNodesFiniteBurnNotOverwritten(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	now := w.Clock.SimTime

	// Two finite nodes whose centered burn windows both straddle `now`,
	// so both BurnStart() <= now on this tick. A triggers slightly
	// before B; distinct Δv so we can tell which one started.
	nodeA := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnPrograde,
		DV:          100,
		TriggerTime: now.Add(10 * time.Second),
		Duration:    60 * time.Second, // BurnStart = now-20s
	}
	nodeB := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnPrograde,
		DV:          200,
		TriggerTime: now.Add(15 * time.Second),
		Duration:    60 * time.Second, // BurnStart = now-15s
	}
	c.Nodes = []spacecraft.ManeuverNode{nodeA, nodeB}
	sortNodes(c.Nodes)

	w.executeDueNodes()

	if c.ActiveBurn == nil {
		t.Fatal("no ActiveBurn started; expected node A's finite burn")
	}
	if c.ActiveBurn.DVRemaining != 100 {
		t.Errorf("ActiveBurn.DVRemaining = %.0f, want 100 (node A) — node B overwrote node A's burn",
			c.ActiveBurn.DVRemaining)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d after dispatch, want 1 (node B must stay queued, not be silently dropped)", len(c.Nodes))
	}
	if c.Nodes[0].DV != 200 {
		t.Errorf("remaining node DV = %.0f, want 200 (node B) — wrong node was retained", c.Nodes[0].DV)
	}
}

// TestExecuteDueNodesDoesNotOverwriteInFlightBurn — GH #88 (#1, HIGH),
// cross-tick variant. A finite burn started on a previous tick must not
// be overwritten when a second finite node comes due while the first is
// still in flight; the due node stays queued until the active burn ends.
func TestExecuteDueNodesDoesNotOverwriteInFlightBurn(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	now := w.Clock.SimTime

	// Simulate an in-flight burn from a prior tick.
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 500,
		EndTime:     now.Add(30 * time.Second),
	}
	// A second finite node that is already due this tick.
	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:        spacecraft.BurnPrograde,
		DV:          200,
		TriggerTime: now.Add(5 * time.Second),
		Duration:    40 * time.Second, // BurnStart = now-15s
	}}

	w.executeDueNodes()

	if c.ActiveBurn == nil || c.ActiveBurn.DVRemaining != 500 {
		t.Fatalf("in-flight ActiveBurn was overwritten: %+v", c.ActiveBurn)
	}
	if len(c.Nodes) != 1 {
		t.Errorf("len(Nodes) = %d, want 1 — the due node must stay queued behind the in-flight burn", len(c.Nodes))
	}
}

// TestSortNodesOrdersByBurnStart — GH #88 (#2, MEDIUM). executeDueNodesFor
// fires/breaks on BurnStart() (= TriggerTime - Duration/2), so the node
// slice must be sorted by BurnStart, not TriggerTime. A later-trigger but
// longer-duration node has an earlier BurnStart and must dispatch first.
func TestSortNodesOrdersByBurnStart(t *testing.T) {
	base := time.Unix(0, 0).UTC()
	// Node A: impulsive at T+100 (BurnStart = T+100).
	a := spacecraft.ManeuverNode{Mode: spacecraft.BurnPrograde, DV: 10, TriggerTime: base.Add(100 * time.Second)}
	// Node B: finite at T+110, Duration 40s (BurnStart = T+90 < A's).
	b := spacecraft.ManeuverNode{Mode: spacecraft.BurnPrograde, DV: 20, TriggerTime: base.Add(110 * time.Second), Duration: 40 * time.Second}

	nodes := []spacecraft.ManeuverNode{a, b}
	sortNodes(nodes)

	if !nodes[0].BurnStart().Before(nodes[1].BurnStart()) {
		t.Errorf("nodes not ordered by BurnStart: [0]=%v [1]=%v",
			nodes[0].BurnStart(), nodes[1].BurnStart())
	}
	if nodes[0].DV != 20 {
		t.Errorf("node[0].DV = %.0f, want 20 (the earlier-BurnStart finite node B)", nodes[0].DV)
	}
}

// TestExecuteDueNodesRefusesUnresolvedTargetRelativeImpulsive — #294
// review finding 5. A due impulsive target-relative node whose bound
// ghost ref doesn't resolve (survived a save/load without a live
// serve session to re-latch it, or hasn't re-latched yet post-
// reconnect) must refuse to fire rather than burn against
// nodeTargetRelState's zero-value fallback (DirectionUnitTarget's
// BurnTarget/AntiTarget cases degrade that to a direction aimed at, or
// away from, the primary's centre — real Δv spent in a bogus
// direction). The node must stay queued (not popped) and
// LastNodeTargetRefusal must fire so the player is told.
func TestExecuteDueNodesRefusesUnresolvedTargetRelativeImpulsive(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime
	startFuel := c.ActiveStageFuel()

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now, // due now
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil // the ghost ref never resolves

	w.executeDueNodes()

	if len(c.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d after refused fire, want 1 (node stays queued)", len(c.Nodes))
	}
	if c.ActiveStageFuel() != startFuel {
		t.Errorf("fuel changed on a refused fire: before=%.6f after=%.6f", startFuel, c.ActiveStageFuel())
	}
	if w.LastNodeTargetRefusal == nil {
		t.Fatal("LastNodeTargetRefusal not set on a refused fire")
	}
	if w.LastNodeTargetRefusal.CraftName != c.Name {
		t.Errorf("LastNodeTargetRefusal.CraftName = %q, want %q", w.LastNodeTargetRefusal.CraftName, c.Name)
	}
}

// TestExecuteDueNodesRefusesUnresolvedTargetRelativeFinite — same as
// above for a finite (Duration>0) target-relative node: it must not
// start c.ActiveBurn against an unresolved target.
func TestExecuteDueNodesRefusesUnresolvedTargetRelativeFinite(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTargetPrograde,
		DV:               100,
		TriggerTime:      now.Add(10 * time.Second),
		Duration:         60 * time.Second, // BurnStart = now-20s, already due
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil

	w.executeDueNodes()

	if c.ActiveBurn != nil {
		t.Fatalf("ActiveBurn started against an unresolved target: %+v", c.ActiveBurn)
	}
	if len(c.Nodes) != 1 {
		t.Errorf("len(Nodes) = %d after refused fire, want 1 (node stays queued)", len(c.Nodes))
	}
	if w.LastNodeTargetRefusal == nil {
		t.Error("LastNodeTargetRefusal not set on a refused finite-burn fire")
	}
}

// TestExecuteDueNodesFiresOnceGhostResolves — the flip side: once the
// bound ghost resolves, the previously-refused node fires normally on
// the next tick, proving the refusal is a hold, not a silent drop.
func TestExecuteDueNodesFiresOnceGhostResolves(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil
	w.executeDueNodes()
	if len(c.Nodes) != 1 {
		t.Fatalf("node fired/dropped despite an unresolved target: len=%d", len(c.Nodes))
	}

	// The ghost's report resumes.
	w.Ghosts = []Ghost{{
		Owner: "SHA256:gern", CraftID: 987654, PrimaryID: c.Primary.ID,
		Pos: w.BodyPosition(c.Primary).Add(c.State.R).Add(orbital.Vec3{X: 1e6}),
	}}
	w.executeDueNodes()
	if len(c.Nodes) != 0 {
		t.Errorf("node did not fire once its target resolved: len(Nodes)=%d", len(c.Nodes))
	}
}
