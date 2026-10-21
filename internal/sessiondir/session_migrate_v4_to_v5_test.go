package sessiondir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
)

// A v4 session.json (pre-10a) holding one live dock. The new acknowledgement
// fields do not exist in it, so it must read back with every mark zero, which
// means both seats acknowledged: all v4 could express.
const v4Session = `{
  "version": 4,
  "body_catalog_hash": "x",
  "roster": [],
  "invites": [],
  "docks": [
    {"id": 7, "owner": "SHA256:a", "docker_craft_id": 1, "composite_id": 5,
     "guest_owner": "SHA256:b", "guest_craft_id": 9, "phase": 1}
  ]
}`

func TestMigrateMetaV4ToV5(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(v4Session), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	m, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if m.Version != 5 || MetaVersion != 5 {
		t.Fatalf("migrated Version %d, MetaVersion %d, want 5", m.Version, MetaVersion)
	}
	if len(m.Docks) != 1 {
		t.Fatalf("docks = %d, want 1", len(m.Docks))
	}
	d := m.Docks[0]
	if d.ID != 7 || d.CompositeID != 5 || d.Phase != 1 {
		t.Errorf("identity fields changed by the migration: %+v", d)
	}
	if d.OwnerDirty || d.GuestDirty || d.OwnerCopy != nil || d.GuestCopy != nil || d.FromCompositeID != 0 || d.Done {
		t.Errorf("a v4 record must read as acknowledged, got %+v", d)
	}
	// A write persists at the current schema.
	if err := s.SetDocks(m.Docks); err != nil {
		t.Fatalf("SetDocks: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "session.json"))
	var probe struct{ Version int }
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Version != 5 {
		t.Errorf("on-disk version after a write = %d (%v), want 5", probe.Version, err)
	}
}

// The new fields survive SetDocks and a reopen.
func TestDockLinkAckFieldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := DockLink{
		ID: 3, Owner: "SHA256:a", GuestOwner: "SHA256:b", Phase: 1,
		OwnerDirty: true, GuestDirty: true, FromCompositeID: 12, Done: true,
		OwnerCopy: &save.Craft{ID: 4, Name: "owner copy"},
		GuestCopy: &save.Craft{ID: 5, Name: "guest copy"},
	}
	if err := s.SetDocks([]DockLink{want}); err != nil {
		t.Fatalf("SetDocks: %v", err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	m, err := s2.Meta()
	if err != nil || len(m.Docks) != 1 {
		t.Fatalf("Meta after reopen: %v, %d docks", err, len(m.Docks))
	}
	got := m.Docks[0]
	if !got.OwnerDirty || !got.GuestDirty || got.FromCompositeID != 12 || !got.Done ||
		got.OwnerCopy == nil || got.OwnerCopy.ID != 4 || got.GuestCopy == nil || got.GuestCopy.ID != 5 {
		t.Errorf("ack fields lost across a reopen: %+v", got)
	}
}
