package save

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestMigrateV6PayloadToV7 — GH #87 / ADR 0012. A pre-v7 payload binds
// targets by slate index (Target.CraftIdx 0-based, TargetCraftIdx
// one-based). The migration must assign stable IDs by slate position and
// rewrite every index binding to the matching ID so the loaded world
// resolves targets by identity.
func TestMigrateV6PayloadToV7(t *testing.T) {
	craft := int(spacecraft.TargetCraft)
	p := Payload{
		Crafts: []Craft{
			{ // idx 0 → ID 1; a node aimed at craft idx 2 (one-based 3)
				Name:  "A",
				Nodes: []Node{{TargetCraftIdx: 3}},
			},
			{ // idx 1 → ID 2; per-craft target aimed at craft idx 2 (0-based)
				Name:   "B",
				Target: &Target{Kind: craft, CraftIdx: 2},
				ActiveBurn: &ActiveBurn{
					TargetCraftIdx: 1, // one-based → craft idx 0 → ID 1
				},
			},
			{Name: "C"}, // idx 2 → ID 3
		},
		// Pre-polish payload-level target aimed at craft idx 1 (0-based).
		Target: &Target{Kind: craft, CraftIdx: 1},
	}

	migrateV6PayloadToV7(&p)

	// Sequential IDs by slate position + primed counter.
	for i, c := range p.Crafts {
		if c.ID != uint64(i+1) {
			t.Errorf("craft %d ID = %d, want %d", i, c.ID, i+1)
		}
	}
	if p.NextCraftID != 4 {
		t.Errorf("NextCraftID = %d, want 4", p.NextCraftID)
	}

	// Node bound to one-based idx 3 → craft idx 2 → ID 3.
	if got := p.Crafts[0].Nodes[0].TargetCraftID; got != 3 {
		t.Errorf("node TargetCraftID = %d, want 3 (craft C)", got)
	}
	// Per-craft target: 0-based idx 2 → ID 3.
	if got := p.Crafts[1].Target.CraftID; got != 3 {
		t.Errorf("craft B Target.CraftID = %d, want 3 (craft C)", got)
	}
	// ActiveBurn one-based idx 1 → craft idx 0 → ID 1.
	if got := p.Crafts[1].ActiveBurn.TargetCraftID; got != 1 {
		t.Errorf("ActiveBurn TargetCraftID = %d, want 1 (craft A)", got)
	}
	// Payload-level target: 0-based idx 1 → ID 2.
	if got := p.Target.CraftID; got != 2 {
		t.Errorf("payload Target.CraftID = %d, want 2 (craft B)", got)
	}
}

// TestMigrateV6PayloadToV7OutOfRange — an index pointing past the slate
// (stale binding) maps to "no target" (ID 0), matching the silent-drop
// the pre-v7 bounds checks produced.
func TestMigrateV6PayloadToV7OutOfRange(t *testing.T) {
	craft := int(spacecraft.TargetCraft)
	p := Payload{
		Crafts: []Craft{
			{Name: "A", Target: &Target{Kind: craft, CraftIdx: 9}}, // out of range
			{Name: "B", Nodes: []Node{{TargetCraftIdx: 9}}},        // out of range
		},
	}
	migrateV6PayloadToV7(&p)
	if got := p.Crafts[0].Target.CraftID; got != 0 {
		t.Errorf("out-of-range Target.CraftID = %d, want 0 (no target)", got)
	}
	if got := p.Crafts[1].Nodes[0].TargetCraftID; got != 0 {
		t.Errorf("out-of-range node TargetCraftID = %d, want 0 (no target)", got)
	}
}
