package sessiondir

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
)

// TestMigrateV2SessionForward — a live v0.28–v0.32 session.json (schema v2:
// dock cross-refs with no payloads) must survive into the durable-ledger
// build. The docks carry forward untouched, arriving with no parked payload
// and no pending flag, which is exactly the state v2 could express; a re-write
// stamps v3. ADR 0040.
func TestMigrateV2SessionForward(t *testing.T) {
	dir := t.TempDir()
	v2 := `{
  "version": 2,
  "body_catalog_hash": "deadbeef",
  "roster": [
    {"fingerprint": "local", "handle": "jason", "role": "host", "calibrated": true},
    {"fingerprint": "SHA256:gern", "handle": "gern", "role": "guest", "calibrated": true}
  ],
  "invites": [],
  "docks": [
    {"id": 3, "owner": "local", "owner_handle": "jason", "docker_craft_id": 1,
     "composite_id": 2, "guest_owner": "SHA256:gern", "guest_handle": "gern",
     "guest_craft_id": 200, "phase": 1}
  ]
}`
	if err := os.MkdirAll(dir+"/players", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/session.json", []byte(v2), 0o644); err != nil {
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
	if m.Version != MetaVersion {
		t.Errorf("migrated Version = %d, want %d", m.Version, MetaVersion)
	}
	if len(m.Docks) != 1 || m.Docks[0].ID != 3 || m.Docks[0].GuestCraftID != 200 || m.Docks[0].Phase != 1 {
		t.Fatalf("v2 dock cross-ref not carried forward: %+v", m.Docks)
	}
	d := m.Docks[0]
	if d.GuestPayload != nil || d.ReturnPayload != nil || d.TransferPayload != nil {
		t.Errorf("migrated v2 dock invented a payload: %+v", d)
	}
	if d.UndockAsk || d.UndockRefused || d.Aborted || d.Parcel || d.TransferTo != "" {
		t.Errorf("migrated v2 dock invented a pending request: %+v", d)
	}
}

// TestDockPayloadRoundTripsThroughSessionJSON: the whole point of v3 — a craft
// parked on a dock record survives the file. Pre-fix the payload had no home
// on disk at all, so the only copy of a migrating composite died with the
// process (#311/#313).
func TestDockPayloadRoundTripsThroughSessionJSON(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	parked := save.Craft{
		ID: 42, Name: "Kern Stack", PrimaryID: "earth", DryMass: 1234,
		R: save.Vec3{X: 7.0e6}, V: save.Vec3{Y: 7500},
	}
	links := []DockLink{{
		ID: 1, Owner: "local", OwnerHandle: "jason", DockerCraftID: 1,
		CompositeID: 2, GuestOwner: "SHA256:gern", GuestHandle: "gern",
		GuestCraftID: 200, Phase: 1,
		TransferPayload: &parked, TransferTo: "SHA256:gern", UndockAsk: true,
	}}
	if err := s.SetDocks(links); err != nil {
		t.Fatalf("SetDocks: %v", err)
	}

	// Re-open the store from scratch: a server restart reads the file cold.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	m, err := s2.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if len(m.Docks) != 1 {
		t.Fatalf("docks = %+v, want 1", m.Docks)
	}
	got := m.Docks[0]
	if got.TransferPayload == nil {
		t.Fatalf("the parked craft did not survive session.json — it exists nowhere now")
	}
	if got.TransferPayload.ID != 42 || got.TransferPayload.Name != "Kern Stack" {
		t.Errorf("parked craft identity lost: %+v", got.TransferPayload)
	}
	if got.TransferPayload.R != (save.Vec3{X: 7.0e6}) || got.TransferPayload.V != (save.Vec3{Y: 7500}) {
		t.Errorf("parked craft state lost: R=%+v V=%+v", got.TransferPayload.R, got.TransferPayload.V)
	}
	if !got.UndockAsk || got.TransferTo != "SHA256:gern" {
		t.Errorf("request flags lost across the file: %+v", got)
	}

	// And the file itself is stamped at the current schema.
	data, _ := os.ReadFile(dir + "/session.json")
	var probe struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(data, &probe)
	if probe.Version != MetaVersion {
		t.Errorf("on-disk version = %d, want %d", probe.Version, MetaVersion)
	}
}
