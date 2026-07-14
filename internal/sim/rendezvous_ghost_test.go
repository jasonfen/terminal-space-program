package sim

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// ghostRendezvousWorld builds a world whose active craft is the mustWorld
// default, plus a single ghost placed at the given primary-relative state
// around the active craft's own primary, targeted. This mirrors
// rendezvousTwoCraftWorld's geometry but with the target as a remote
// player's coasting craft (a Ghost) instead of a local sister.
func ghostRendezvousWorld(t *testing.T, relR, relV orbital.Vec3) *World {
	t.Helper()
	w := mustWorld(t)
	c := w.ActiveCraft()
	w.Ghosts = []Ghost{{
		Owner:     "SHA256:gern",
		CraftID:   7,
		Handle:    "gern",
		Name:      "Aloft",
		PrimaryID: c.Primary.ID,
		Pos:       w.BodyPosition(c.Primary).Add(relR),
		RelPos:    relR,
		Vel:       relV,
	}}
	w.SetTargetGhost("SHA256:gern", 7)
	return w
}

// TestRendezvousNudge_GhostMatchesCraft — the acceptance heart of S4:
// the K-nudge advisory computed against a coasting ghost must match the
// advisory computed against a LOCAL sister craft occupying the identical
// primary-relative state. A coasting ghost is Kepler-exact between
// reports, so plan quality is craft-to-craft identical.
func TestRendezvousNudge_GhostMatchesCraft(t *testing.T) {
	craftW := rendezvousTwoCraftWorld(t)
	sister := craftW.Crafts[1]
	admCraft, okCraft := craftW.RecommendedRendezvousBurn()

	ghostW := ghostRendezvousWorld(t, sister.State.R, sister.State.V)
	admGhost, okGhost := ghostW.RecommendedRendezvousBurn()

	if okCraft != okGhost {
		t.Fatalf("ok mismatch: craft=%v ghost=%v", okCraft, okGhost)
	}
	if !okGhost {
		t.Skipf("baseline advisory not computable (Reason=%q) — geometry, not a regression", admGhost.Reason)
	}
	if admCraft.Ok != admGhost.Ok {
		t.Fatalf("advisory.Ok mismatch: craft=%v ghost=%v", admCraft.Ok, admGhost.Ok)
	}
	if admCraft.Axis != admGhost.Axis {
		t.Errorf("Axis mismatch: craft=%v ghost=%v", admCraft.Axis, admGhost.Axis)
	}
	// Position noise from world-frame add/sub (BodyPosition ~1e11 m) is
	// sub-mm; the Δv it induces is far below 1 m/s on a ~km/s intercept.
	if d := math.Abs(admCraft.DV - admGhost.DV); d > 1.0 {
		t.Errorf("DV mismatch: craft=%.4f ghost=%.4f (Δ=%.4f m/s)", admCraft.DV, admGhost.DV, d)
	}
}

// TestPlanRendezvousNudge_GhostRefusalPathsGone — the craft-gate is gone:
// with a ghost target sharing the active primary, the planter no longer
// refuses. It plants a node carrying the node-level ghost ref (owner +
// remote craft id).
func TestPlanRendezvousNudge_GhostRefusalPathsGone(t *testing.T) {
	craftW := rendezvousTwoCraftWorld(t)
	sister := craftW.Crafts[1]
	w := ghostRendezvousWorld(t, sister.State.R, sister.State.V)
	c := w.ActiveCraft()

	adv, err := w.PlanRendezvousNudge()
	if err != nil {
		if errors.Is(err, ErrRendezvousNoImprovement) {
			t.Skipf("ghost geometry yielded no useful nudge; not a regression")
		}
		t.Fatalf("PlanRendezvousNudge against ghost: %v (refusal path not gone)", err)
	}
	if adv == nil || !adv.Ok {
		t.Fatalf("expected a usable advisory, got %+v", adv)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 planted node, got %d", len(c.Nodes))
	}
	n := c.Nodes[0]
	if n.TargetCraftID != 7 {
		t.Errorf("node TargetCraftID = %d, want 7 (ghost craft id)", n.TargetCraftID)
	}
	if n.TargetGhostOwner != "SHA256:gern" {
		t.Errorf("node TargetGhostOwner = %q, want %q", n.TargetGhostOwner, "SHA256:gern")
	}
}

// TestPlanRendezvousNudge_LocalCraftHasNoGhostOwner — a local craft plant
// leaves TargetGhostOwner empty, so the omitempty save path and the
// "owner==\"\" ⇒ local ref" dispatch stay intact.
func TestPlanRendezvousNudge_LocalCraftHasNoGhostOwner(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	c := w.ActiveCraft()
	if _, err := w.PlanRendezvousNudge(); err != nil {
		if errors.Is(err, ErrRendezvousNoImprovement) {
			t.Skip("no useful nudge in this geometry")
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(c.Nodes))
	}
	if c.Nodes[0].TargetGhostOwner != "" {
		t.Errorf("local-craft node carried a ghost owner: %q", c.Nodes[0].TargetGhostOwner)
	}
}

// TestPlanRendezvousNudge_GhostDifferentPrimary — a ghost around a body
// other than the active craft's primary is refused with the same
// different-primaries gate as a craft target.
func TestPlanRendezvousNudge_GhostDifferentPrimary(t *testing.T) {
	w := mustWorld(t)
	moon := w.Systems[0].FindBody("Moon")
	if moon == nil {
		t.Skip("Moon not in catalog — skipping different-primaries gate")
	}
	c := w.ActiveCraft()
	w.Ghosts = []Ghost{{
		Owner:     "SHA256:gern",
		CraftID:   7,
		Handle:    "gern",
		Name:      "Aloft",
		PrimaryID: moon.ID,
		Pos:       w.BodyPosition(*moon).Add(orbital.Vec3{X: 2e6}),
		RelPos:    orbital.Vec3{X: 2e6},
		Vel:       orbital.Vec3{Y: 1500},
	}}
	w.SetTargetGhost("SHA256:gern", 7)

	_, err := w.PlanRendezvousNudge()
	if !errors.Is(err, ErrRendezvousDifferentPrimaries) {
		t.Errorf("err = %v, want ErrRendezvousDifferentPrimaries", err)
	}
	if got := len(c.Nodes); got != 0 {
		t.Errorf("rejected plant should append no node; got %d", got)
	}
}

// TestRendezvousGhostNode_StaleNoAutoUpdate — a planted node against a
// ghost that then burns goes STALE: the next report corrects the ghost,
// NOT the plan. The node struct must be untouched by any report/recompute
// after plant. Replanting is the player's move.
func TestRendezvousGhostNode_StaleNoAutoUpdate(t *testing.T) {
	craftW := rendezvousTwoCraftWorld(t)
	sister := craftW.Crafts[1]
	w := ghostRendezvousWorld(t, sister.State.R, sister.State.V)
	c := w.ActiveCraft()

	if _, err := w.PlanRendezvousNudge(); err != nil {
		if errors.Is(err, ErrRendezvousNoImprovement) {
			t.Skip("no useful nudge in this geometry")
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(c.Nodes))
	}
	snapshot := c.Nodes[0]

	// Simulate a fresh report: the ghost burned and is now somewhere
	// else entirely. The world re-derives the advisory against the
	// corrected ghost...
	w.Ghosts[0].Pos = w.Ghosts[0].Pos.Add(orbital.Vec3{X: 5e5, Y: -3e5})
	w.Ghosts[0].RelPos = w.Ghosts[0].RelPos.Add(orbital.Vec3{X: 5e5, Y: -3e5})
	w.Ghosts[0].Vel = w.Ghosts[0].Vel.Add(orbital.Vec3{Z: 40})
	w.Clock.SimTime = w.Clock.SimTime.Add(rendezvousRecomputeInterval) // force recompute
	_, _ = w.RecommendedRendezvousBurn()

	// ...but the planted node is unchanged — no auto-update.
	if c.Nodes[0] != snapshot {
		t.Errorf("planted node auto-updated after a ghost report:\n  before = %+v\n  after  = %+v", snapshot, c.Nodes[0])
	}
}

// TestNodeTargetRelState_GhostAndCraftAndStale — the fire-time resolver
// used by executeDueNodesFor / nodeLeadActive / navball resolves a
// node's bound target for both a local craft ref (owner=="") and a
// remote ghost ref (owner!=""), and reports ok=false for a stale ref.
func TestNodeTargetRelState_GhostAndCraftAndStale(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	c := w.ActiveCraft()
	sister := w.Crafts[1]

	// Local craft ref.
	rT, vT, ok := w.nodeTargetRelState("", sister.ID, c.Primary)
	if !ok {
		t.Fatal("local craft ref did not resolve")
	}
	if rT != sister.State.R || vT != sister.State.V {
		t.Errorf("local ref state = (%v,%v), want (%v,%v)", rT, vT, sister.State.R, sister.State.V)
	}

	// Ghost ref at a known primary-relative state.
	relR := orbital.Vec3{X: 7.1e6}
	relV := orbital.Vec3{Y: 7400}
	w.Ghosts = []Ghost{{
		Owner: "SHA256:gern", CraftID: 9, PrimaryID: c.Primary.ID,
		Pos: w.BodyPosition(c.Primary).Add(relR), RelPos: relR, Vel: relV,
	}}
	grT, gvT, ok := w.nodeTargetRelState("SHA256:gern", 9, c.Primary)
	if !ok {
		t.Fatal("ghost ref did not resolve")
	}
	if d := grT.Sub(relR).Norm(); d > 1e-3 {
		t.Errorf("ghost rel R off by %g m", d)
	}
	if gvT != relV {
		t.Errorf("ghost rel V = %v, want %v", gvT, relV)
	}

	// Stale ghost (slate cleared) → no resolution.
	w.Ghosts = nil
	if _, _, ok := w.nodeTargetRelState("SHA256:gern", 9, c.Primary); ok {
		t.Error("stale ghost ref still resolved")
	}
	// Unbound (id 0) → no resolution.
	if _, _, ok := w.nodeTargetRelState("", 0, c.Primary); ok {
		t.Error("zero craft id resolved")
	}
}
