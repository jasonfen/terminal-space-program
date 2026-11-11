package sessiondir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const v5Session = `{"version": 5, "body_catalog_hash": "x", "roster": [], "invites": [],
  "docks": [{"id": 7, "owner": "SHA256:a", "composite_id": 5, "phase": 1}]}`

// A v5 session.json loads at the current meta (7) with no sites and its dock
// intact; a site write, with its LZ count and names (ADR 0068), persists at 7 and a SetDocks afterwards does not drop it.
func TestMigrateMetaV5ToV6(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(v5Session), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Meta()
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 7 || MetaVersion != 7 || len(m.Sites) != 0 || len(m.Docks) != 1 {
		t.Fatalf("version %d/%d sites %d docks %d, want 7/7 0 1", m.Version, MetaVersion, len(m.Sites), len(m.Docks))
	}
	want := SiteLink{Owner: "SHA256:a", ID: 2, BodyID: "moon", System: "Sol", LatDeg: 12.5, LonDeg: -33, Name: "Eagle site", UpdatedAtNano: 9,
		LZCount: 3, LZNames: []string{"", "", "Alpha"}}
	if err := s.SetSites([]SiteLink{want}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDocks(m.Docks); err != nil {
		t.Fatal(err)
	}
	m2, _ := s.Meta()
	if len(m2.Sites) != 1 || !reflect.DeepEqual(m2.Sites[0], want) || len(m2.Docks) != 1 {
		t.Errorf("after SetSites+SetDocks: %+v docks %d", m2.Sites, len(m2.Docks))
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "session.json"))
	var probe struct{ Version int }
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Version != 7 {
		t.Errorf("on-disk version %d (%v), want 7", probe.Version, err)
	}
}
