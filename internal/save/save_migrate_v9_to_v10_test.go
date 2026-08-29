package save

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestMigrateV9PayloadToV10IsIdentity — #294 review finding 6. The v9→v10
// bump exists solely to make Load's version gate refuse an older binary
// reading a v10 envelope (Target.Kind's vocabulary widened to include
// TargetGhost, an already-additive/omitempty wire shape); the payload
// itself needs no field transform. Pin that: migrating a representative
// payload — including a ghost Target and a ghost-targeted Node/ActiveBurn,
// the exact shapes finding 6 is about — must leave every field untouched.
func TestMigrateV9PayloadToV10IsIdentity(t *testing.T) {
	p := &Payload{
		SystemIdx:   2,
		SimTimeNano: 123456789,
		WarpIdx:     3,
		NavMode:     2,
		Target: &Target{
			Kind:       4, // TargetGhost
			GhostOwner: "SHA256:gern",
			CraftID:    99,
		},
		Crafts: []Craft{{
			ID:   1,
			Name: "Test Craft",
			Nodes: []Node{{
				Mode:             1,
				TargetCraftID:    987654,
				TargetGhostOwner: "SHA256:gern",
			}},
			ActiveBurn: &ActiveBurn{
				Mode:             1,
				TargetCraftID:    987654,
				TargetGhostOwner: "SHA256:gern",
			},
		}},
	}
	before := *p // shallow copy for comparison; nested pointers/slices below are checked field-by-field

	migrateV9PayloadToV10(p)

	if p.SystemIdx != before.SystemIdx || p.SimTimeNano != before.SimTimeNano ||
		p.WarpIdx != before.WarpIdx || p.NavMode != before.NavMode {
		t.Errorf("scalar payload fields changed: got %+v, want unchanged from %+v", p, before)
	}
	if p.Target == nil || *p.Target != *before.Target {
		t.Errorf("Target changed: got %+v, want unchanged %+v", p.Target, before.Target)
	}
	if len(p.Crafts) != 1 {
		t.Fatalf("Crafts count changed: got %d, want 1", len(p.Crafts))
	}
	c := p.Crafts[0]
	if c.ID != 1 || c.Name != "Test Craft" {
		t.Errorf("Craft identity fields changed: %+v", c)
	}
	if len(c.Nodes) != 1 || c.Nodes[0].TargetGhostOwner != "SHA256:gern" || c.Nodes[0].TargetCraftID != 987654 {
		t.Errorf("Node ghost ref changed: %+v", c.Nodes)
	}
	if c.ActiveBurn == nil || c.ActiveBurn.TargetGhostOwner != "SHA256:gern" || c.ActiveBurn.TargetCraftID != 987654 {
		t.Errorf("ActiveBurn ghost ref changed: %+v", c.ActiveBurn)
	}
}

// TestSchemaVersionBumpedToV10 pins the version number itself (#294
// review finding 6) — a regression here means someone widened
// Target.Kind's vocabulary again (or otherwise changed persisted shape)
// without bumping, defeating the whole point of the migration file
// alongside it.
func TestSchemaVersionBumpedToV10(t *testing.T) {
	if SchemaVersion != 10 {
		t.Errorf("SchemaVersion = %d, want 10", SchemaVersion)
	}
}

// TestLoadAcceptsV10Envelope — Load's version gate ([1, SchemaVersion])
// must accept a freshly-written v10 envelope (Version == SchemaVersion,
// pinned to 10 above) without routing it through the (identity) v9→v10
// migration in a way that loses data — exercised here with the exact
// finding-6 shape, a ghost Target. A real "does an OLD binary refuse a
// v10 file" check can't run inside this binary (its own SchemaVersion
// IS 10) — that half of finding 6 is structural: Load's existing
// `f.Version > SchemaVersion` gate refuses anything above whatever
// SchemaVersion the reading binary was built with, and
// TestSchemaVersionBumpedToV10 above pins that a pre-fix binary's
// SchemaVersion of 9 is now less than a fixed save's 10.
func TestLoadAcceptsV10Envelope(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:gern", CraftID: 7, PrimaryID: c.Primary.ID}}
	w.SetTargetGhost("SHA256:gern", 7)

	path := t.TempDir() + "/v10.json"
	if err := Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load a same-version v10 envelope: %v", err)
	}
	if got.Target.Kind != sim.TargetGhost || got.Target.GhostOwner != "SHA256:gern" {
		t.Errorf("loaded Target = %+v, want the ghost lock intact through the v10 envelope", got.Target)
	}
}
