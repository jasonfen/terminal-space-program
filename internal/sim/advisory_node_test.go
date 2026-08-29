package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestAdvisoryNodeDifferentKeysCoexist — #293's "replace" ruling is scoped
// to a key replacing its OWN previous unfired node, not to "only one
// advisory node may ever be queued". Planting via C (circularize) then K
// (rendezvous nudge) must leave BOTH nodes queued — they carry different
// AdvisoryKey tags, so neither plant path removes the other's node.
func TestAdvisoryNodeDifferentKeysCoexist(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()

	_, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("geometry yielded no useful nudge (%v); not a regression", err)
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if got := len(c.Nodes); got != 1 {
		t.Fatalf("after K: expected 1 node, got %d", got)
	}
	nudgeID := c.Nodes[0].ID
	if c.Nodes[0].AdvisoryKey != AdvisoryKeyRendezvousNudge {
		t.Errorf("AdvisoryKey = %q, want %q", c.Nodes[0].AdvisoryKey, AdvisoryKeyRendezvousNudge)
	}

	// Overwrite the craft's live state to a circularize-friendly
	// elliptical orbit (apoapsis clear of the atmosphere). This only
	// affects what PlanCircularizeAtApoapsis reads at call time — the
	// already-planted K node is unaffected by a later state change.
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	const (
		periAlt = 200e3
		apoAlt  = 1000e3
	)
	rPeri := primaryR + periAlt
	rApo := primaryR + apoAlt
	a := (rPeri + rApo) / 2
	vPeri := math.Sqrt(mu * (2/rPeri - 1/a))
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	c.State.R = frame.ToWorld(orbital.Vec3{X: rPeri})
	c.State.V = frame.ToWorld(orbital.Vec3{Y: vPeri})

	if _, err := w.PlanCircularizeAtApoapsis(); err != nil {
		t.Fatalf("PlanCircularizeAtApoapsis: %v", err)
	}
	if got := len(c.Nodes); got != 2 {
		t.Fatalf("after K then C: expected 2 nodes (both advisories coexist), got %d", got)
	}
	// The rendezvous-nudge node from the first plant must still be
	// present, untouched by the C plant.
	found := false
	for _, n := range c.Nodes {
		if n.ID == nudgeID {
			found = true
		}
	}
	if !found {
		t.Error("C plant removed the earlier K (rendezvous nudge) node — replace must be scoped to the SAME advisory key only")
	}
}

// TestReplaceAdvisoryNodeIsNoOpForEmptyKey guards the replace helper's
// empty-key guard: an empty AdvisoryKey must never match and strip
// ordinary (non-advisory) planted nodes, e.g. a manual PlanNode or a
// PlanTransfer leg — those never carry an AdvisoryKey tag.
func TestReplaceAdvisoryNodeIsNoOpForEmptyKey(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	base := w.Clock.SimTime
	w.PlanNode(ManeuverNode{TriggerTime: base.Add(60 * time.Second), DV: 10, Mode: spacecraft.BurnPrograde})
	if got := len(c.Nodes); got != 1 {
		t.Fatalf("setup: expected 1 node, got %d", got)
	}
	w.replaceAdvisoryNode(c, "")
	if got := len(c.Nodes); got != 1 {
		t.Errorf("replaceAdvisoryNode(\"\") must be a no-op; node count = %d, want 1", got)
	}
}

// TestKReplacesEditedNudgeInsteadOfStacking — #294 second-round review
// finding 6, the "K after edit" half. Before the fix, editing a planted
// K-nudge through the maneuver form re-planted it with NO AdvisoryKey
// (tui/screens/maneuver.go's commitCmd never carried it into
// BurnExecutedMsg, and the message had no field for it at all) — so
// the edited node could never match replaceAdvisoryNode's key lookup,
// and a later K press stacked a second node behind it instead of
// replacing it (#293's ruling). This stands in for "the node an edit
// produced": same shape commitCmd now emits post-fix (proven at the
// tui layer by TestEditedAdvisoryNudgeKeepsIdentityAndAutoWarpHonorsIt)
// — AdvisoryKey present, an arbitrary edited Δv — and confirms a fresh
// K plant replaces it in place rather than stacking.
func TestKReplacesEditedNudgeInsteadOfStacking(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()

	// Stand in for "a nudge that was planted, then edited": AdvisoryKey
	// carried through (the fix), Δv deliberately NOT what a fresh nudge
	// would compute, so the test can tell whether it survived.
	c.Nodes = append(c.Nodes, ManeuverNode{
		Mode:        spacecraft.BurnPrograde,
		DV:          12345,
		TriggerTime: w.Clock.SimTime.Add(2 * time.Hour),
		PrimaryID:   c.Primary.ID,
		AdvisoryKey: AdvisoryKeyRendezvousNudge,
	})
	if got := len(c.Nodes); got != 1 {
		t.Fatalf("setup: expected 1 node, got %d", got)
	}

	_, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("geometry yielded no useful nudge (%v); not a regression", err)
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}

	if got := len(c.Nodes); got != 1 {
		t.Fatalf("K after an edit STACKED instead of replacing: len(Nodes)=%d, nodes=%+v", got, c.Nodes)
	}
	if c.Nodes[0].DV == 12345 {
		t.Error("the old edited node is still queued — the fresh K plant did not replace it")
	}
}
