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
