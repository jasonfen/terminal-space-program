package sessiondir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// openV3Fixture copies testdata/session_v3_main_872bc16.json, a session.json
// written by the main binary at 872bc16 (schema 14, before per-stage ports,
// MetaVersion 3) holding five parked payloads: Apollo-TLI (return), RCS-tug
// (transfer), Probe-Sat, a Falcon-9 still on its pad, and a Saturn-V that
// launched from a non-zero pad, into a fresh session dir and opens it through
// the public Open path.
func openV3Fixture(t *testing.T) (*Store, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/session_v3_main_872bc16.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, dir
}

func payloadsOf(m Meta) map[string]*save.Craft {
	out := map[string]*save.Craft{}
	for _, d := range m.Docks {
		for _, p := range []*save.Craft{d.GuestPayload, d.ReturnPayload, d.TransferPayload} {
			if p != nil {
				out[p.LoadoutID] = p
			}
		}
	}
	return out
}

func simCraft(t *testing.T, wc *save.Craft) *spacecraft.Spacecraft {
	t.Helper()
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	c, err := save.CraftFromWire(*wc, systems)
	if err != nil {
		t.Fatalf("CraftFromWire: %v", err)
	}
	return c
}

// A v3 session.json parked payload comes back with the ports the pre-v15 name
// lookup gave each stage, and a stage the catalog gave nothing stays bare.
func TestMetaV3ParkedPayloadsComeBackWithTheirPorts(t *testing.T) {
	s, _ := openV3Fixture(t)
	m, err := s.Meta()
	if err != nil {
		t.Fatal(err)
	}
	ps := payloadsOf(m)
	if len(ps) != 5 {
		t.Fatalf("fixture should park 5 payloads, got %d", len(ps))
	}
	ported := 0
	for id, wc := range ps {
		c := simCraft(t, wc)
		for _, st := range c.Stages {
			want := spacecraft.LookupCatalogPorts(st.LoadoutID, st.Name)
			if len(st.Ports()) != len(want) {
				t.Errorf("%s stage %q: %d ports after load, catalog lookup gives %d", id, st.Name, len(st.Ports()), len(want))
			}
			if st.HasPort() {
				ported++
			}
		}
	}
	// Instrument check: the fixture is only worth anything if the lookup
	// returns a positive for the vessels the review named.
	if ported == 0 {
		t.Fatal("no stage in the fixture reads ported: the check cannot fail")
	}
	for _, tc := range []struct{ id, stage string }{{"Apollo-TLI", "CM"}, {"RCS-tug", "RCS Tug"}} {
		found := false
		for _, st := range simCraft(t, ps[tc.id]).Stages {
			if st.Name == tc.stage {
				found = true
				if !st.HasPort() {
					t.Errorf("parked %s %q came back port-less", tc.id, tc.stage)
				}
			}
		}
		if !found {
			t.Errorf("fixture %s has no stage %q", tc.id, tc.stage)
		}
	}
}

// The same pad rule the save migration uses: OnPad || lat != 0 || lon != 0.
func TestMetaV3ParkedPayloadsGetHasLaunchSite(t *testing.T) {
	s, _ := openV3Fixture(t)
	m, _ := s.Meta()
	ps := payloadsOf(m)
	for id, want := range map[string]bool{"Falcon-9": true, "Saturn-V": true, "Probe-Sat": false, "Apollo-TLI": false, "RCS-tug": false} {
		if ps[id].HasLaunchSite != want {
			t.Errorf("%s HasLaunchSite = %v, want %v", id, ps[id].HasLaunchSite, want)
		}
	}
}

// The migration runs once: it persists at MetaVersion on the next write, and
// a v4 file is read back as it is, never backfilled by name again.
func TestMetaV4LoadsUnchangedNoSecondBackfill(t *testing.T) {
	s, dir := openV3Fixture(t)
	m, _ := s.Meta()
	// A v15 host parks a port-less VAB stage that happens to be called CM.
	bare := payloadsOf(m)["Apollo-TLI"]
	for i := range bare.Stages {
		if bare.Stages[i].Name == "CM" {
			bare.Stages[i].Ports = nil
		}
	}
	if err := s.SetDocks(m.Docks); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "session.json"))
	var probe struct{ Version int }
	_ = json.Unmarshal(raw, &probe)
	if probe.Version != MetaVersion {
		t.Fatalf("on-disk version %d, MetaVersion %d, want them equal", probe.Version, MetaVersion)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m2, _ := s2.Meta()
	for _, st := range simCraft(t, payloadsOf(m2)["Apollo-TLI"]).Stages {
		if st.Name == "CM" && st.HasPort() {
			t.Error("a v4 file's port-less CM was backfilled by name on load")
		}
	}
	b1, _ := json.Marshal(m.Docks)
	b2, _ := json.Marshal(m2.Docks)
	if string(b1) != string(b2) {
		t.Error("v4 reload changed the docks")
	}
}
