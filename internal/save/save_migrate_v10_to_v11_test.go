package save

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestMigrateV10PayloadToV11IsIdentity — ADR 0049 decision 8 (#453).
// HeadingTrim stores a signed offset from due east (PitchTrim's own
// "zero means no trim" shape, not an absolute compass bearing — see
// spacecraft.Spacecraft.HeadingTrim's doc comment), so the due-east
// default the ADR specifies already IS Go's float64 zero value. A
// pre-v11 payload's Craft entries carry no heading_trim key at all,
// and json.Unmarshal leaves an absent field at that same zero value —
// the wire shape itself needs no transform. Pin that: migrating a
// representative payload, including a craft that already has other
// nonzero trim fields set, must leave every field (HeadingTrim
// included) exactly as decode left it. Same discipline
// TestMigrateV9PayloadToV10IsIdentity pins for the prior bump.
func TestMigrateV10PayloadToV11IsIdentity(t *testing.T) {
	p := &Payload{
		SystemIdx: 2,
		WarpIdx:   3,
		Crafts: []Craft{
			{ID: 1, Name: "Unset"}, // pre-v11: zero value, as a real decode would leave it.
			{
				ID:        2,
				Name:      "Also Has Pitch Trim",
				PitchTrim: 0.35,
				Nodes:     []Node{{Mode: 1, TargetCraftID: 42}},
			},
		},
	}
	before := *p // shallow copy; nested slices checked field-by-field below.

	migrateV10PayloadToV11(p)

	if p.SystemIdx != before.SystemIdx || p.WarpIdx != before.WarpIdx {
		t.Errorf("scalar payload fields changed: got %+v, want unchanged from %+v", p, before)
	}
	if len(p.Crafts) != 2 {
		t.Fatalf("Crafts count changed: got %d, want 2", len(p.Crafts))
	}
	for i, c := range p.Crafts {
		want := before.Crafts[i]
		if c.ID != want.ID || c.Name != want.Name || c.PitchTrim != want.PitchTrim || c.HeadingTrim != want.HeadingTrim {
			t.Errorf("craft %d changed: got %+v, want unchanged %+v", i, c, want)
		}
		if c.HeadingTrim != 0 {
			t.Errorf("craft %d: HeadingTrim = %v, want 0 (the due-east default, unset pre-v11)", i, c.HeadingTrim)
		}
	}
	if len(p.Crafts[1].Nodes) != 1 || p.Crafts[1].Nodes[0].TargetCraftID != 42 {
		t.Errorf("Node fields changed: %+v", p.Crafts[1].Nodes)
	}
}

// TestSchemaVersionBumpedToV11 pins the version number itself — a
// regression here means someone added another persisted-shape change
// without bumping, defeating the migration file alongside it (same
// discipline as TestSchemaVersionBumpedToV10 before it).
func TestSchemaVersionBumpedToV11(t *testing.T) {
	if SchemaVersion != 11 {
		t.Errorf("SchemaVersion = %d, want 11", SchemaVersion)
	}
}

// TestLoadAcceptsV11Envelope — a freshly-written v11 envelope carrying a
// non-default (nonzero-offset) HeadingTrim must round-trip through
// Save/Load intact: Load's version gate must accept it, and the
// (identity) v10->v11 migration must not disturb a real value.
func TestLoadAcceptsV11Envelope(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.HeadingTrim = math.Pi // 180° offset from due east: commanded bearing 270° (west).

	path := t.TempDir() + "/v11.json"
	if err := Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load a same-version v11 envelope: %v", err)
	}
	loaded := got.ActiveCraft()
	if loaded == nil {
		t.Fatal("loaded world has no active craft")
	}
	if loaded.HeadingTrim != math.Pi {
		t.Errorf("loaded HeadingTrim = %v, want %v (270° west) intact through the v11 envelope", loaded.HeadingTrim, math.Pi)
	}
}

// TestLoadDefaultsHeadingTrimForPreV11Save — an end-to-end companion to
// TestMigrateV10PayloadToV11IsIdentity: a v10 envelope with no
// heading_trim key on the wire (simulating a save written before this
// feature existed) must load with HeadingTrim == 0 (due east, no
// trim), the same ascent behaviour that save always had.
func TestLoadDefaultsHeadingTrimForPreV11Save(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := t.TempDir() + "/v10.json"
	if err := Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := downgradeSavedVersionForTest(path, 10); err != nil {
		t.Fatalf("downgrade fixture to v10: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load a downgraded v10 envelope: %v", err)
	}
	loaded := got.ActiveCraft()
	if loaded == nil {
		t.Fatal("loaded world has no active craft")
	}
	if loaded.HeadingTrim != 0 {
		t.Errorf("loaded HeadingTrim = %v, want 0 (due east) for a pre-v11 save", loaded.HeadingTrim)
	}
}

// downgradeSavedVersionForTest rewrites a just-Saved envelope's version
// number in place, to fabricate a fixture that looks like an older
// binary wrote it (a fresh v11 Save already carries no heading_trim key
// for a default-heading craft, courtesy of omitempty, so rewriting the
// version number is the only byte that needs to change to simulate a
// genuine pre-v11 file for this test).
func downgradeSavedVersionForTest(path string, version int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	f.Version = version
	out, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
