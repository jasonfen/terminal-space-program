package save_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// v14Fixture is a real v14 envelope written by the main binary at 872bc16
// (Apollo-TLI x2, RCS-tug, Endpoint-Station, Probe-Sat, S-IVB-1, a loaded
// Comsat-Carrier-3, a vessel still on its pad and one that launched from it).
// Load refuses a save whose body_catalog_hash differs, so if the body catalog
// changes this fixture must be regenerated from a binary that writes v14.
func v14Fixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/v14_main_872bc16.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadV14(t *testing.T) *sim.World {
	t.Helper()
	w, err := save.Load(v14Fixture(t))
	if err != nil {
		if strings.Contains(err.Error(), "catalog") {
			t.Fatalf("Load: %v\nthe body catalog changed: regenerate testdata/v14_main_872bc16.json from a binary that writes v14", err)
		}
		t.Fatalf("Load: %v", err)
	}
	return w
}

func craftNamed(t *testing.T, w *sim.World, name string) *spacecraft.Spacecraft {
	t.Helper()
	for _, c := range w.Crafts {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no craft %q in the loaded fixture", name)
	return nil
}

// Every stage of every craft and docked component loads with exactly the
// ports the pre-15 name lookup gave it, and the vessels ADR 0053 ported keep
// their one nose port.
func TestMigrateV14ToV15BackfillsEveryStagePort(t *testing.T) {
	w := loadV14(t)
	nPorted := 0
	check := func(where string, stages []spacecraft.Stage) {
		for i, s := range stages {
			want := spacecraft.LookupCatalogPorts(s.LoadoutID, s.Name)
			got := s.Ports()
			if len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
				t.Errorf("%s stage %d (%s/%s): ports %+v, old lookup %+v", where, i, s.LoadoutID, s.Name, got, want)
			}
			if len(got) > 0 {
				nPorted++
			}
		}
	}
	for _, c := range w.Crafts {
		check(c.Name, c.Stages)
		for _, dc := range c.DockedComponents {
			check(c.Name+"/"+dc.Name, dc.Stages)
		}
	}
	if nPorted == 0 {
		t.Fatal("instrument check: no stage in the fixture reads ported, so the comparison above proved nothing")
	}
	for name, want := range map[string]bool{
		"Apollo TLI-1": true, "RCS Tug-1": true, "Endpoint Station-1": true,
		"Probe Sat-1": true, "S-IVB-1": false, "Comsat Carrier x3-1": false,
	} {
		c := craftNamed(t, w, name)
		top := c.Stages[len(c.Stages)-1]
		if top.HasPort() != want {
			t.Errorf("%s top stage %q HasPort = %v, want %v", name, top.Name, top.HasPort(), want)
		}
		if want && (len(top.Ports()) != 1 || top.Ports()[0].End != spacecraft.PortEndTop) {
			t.Errorf("%s ports = %+v, want one nose port", name, top.Ports())
		}
	}
}

// The stages of the fixture that carried a port under the old lookup: each
// Apollo stack's CM and Ascent, the tug, station and Probe Sat, the launched
// and on-pad vessels' own, and the carrier's NTR Tug.
var portedStages = map[string]bool{
	"Apollo TLI-1/CM": true, "Apollo TLI-1/Ascent": true,
	"Apollo TLI-2/CM": true, "Apollo TLI-2/Ascent": true,
	"OnPad/CM": true, "OnPad/Ascent": true,
	"RCS Tug-1/RCS Tug": true, "Launched/RCS Tug": true,
	"Endpoint Station-1/Endpoint Station": true, "Probe Sat-1/Probe Sat": true,
	"Comsat Carrier x3-1/NTR Tug": true,
}

// The first re-save writes "ports" explicitly, on the ported stages and no
// others, and a stage with no key afterwards stays port-less even when its
// name is a ported part's name.
func TestMigrateV14ToV15ResaveWritesPortsExplicitly(t *testing.T) {
	w := loadV14(t)
	path := filepath.Join(t.TempDir(), "resave.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var env struct {
		Version int `json:"version"`
		Payload struct {
			Crafts []struct {
				Name   string `json:"name"`
				Stages []struct {
					Name  string `json:"name"`
					Ports []struct {
						End string `json:"end"`
					} `json:"ports"`
				} `json:"stages"`
			} `json:"crafts"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if env.Version != save.SchemaVersion || save.SchemaVersion < 15 {
		t.Fatalf("version %d, SchemaVersion %d, want the current version, >= 15", env.Version, save.SchemaVersion)
	}
	seen := 0
	for _, c := range env.Payload.Crafts {
		for _, s := range c.Stages {
			wantPort := portedStages[c.Name+"/"+s.Name]
			if got := len(s.Ports) == 1 && s.Ports[0].End == "top"; got != wantPort {
				t.Errorf("%s/%s: re-saved ports %+v, want ported=%v", c.Name, s.Name, s.Ports, wantPort)
			}
			if wantPort {
				seen++
			}
		}
	}
	if seen != len(portedStages) {
		t.Errorf("found %d ported stages in the re-save, want %d", seen, len(portedStages))
	}
	// A reload of the re-save keeps them (the field is read, not looked up).
	back, err := save.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !craftNamed(t, back, "RCS Tug-1").Stages[0].HasPort() {
		t.Error("re-saved tug lost its port on reload")
	}
}

// #611: the migration decides HasLaunchSite = OnPad || launch lat/lon != 0.
// A vessel on its pad and one that launched keep a site; orbit spawns do not.
func TestMigrateV14ToV15LaunchSiteFlag(t *testing.T) {
	w := loadV14(t)
	for name, want := range map[string]bool{
		"OnPad": true, "Launched": true,
		"Apollo TLI-1": false, "RCS Tug-1": false, "Probe Sat-1": false,
		"S-IVB-1": false, "Comsat Carrier x3-1": false,
	} {
		if got := craftNamed(t, w, name).HasLaunchSite; got != want {
			t.Errorf("%s HasLaunchSite = %v, want %v", name, got, want)
		}
	}
	launched := craftNamed(t, w, "Launched")
	if launched.OnPad || launched.Landed || launched.LaunchLatDeg == 0 {
		t.Fatalf("fixture setup: Launched = %+v", launched)
	}
}

// (a) every loadout's stages, and the four carriers' docked components, keep
// their ports across the wire.
func TestStagePortsSurviveSaveRoundTrip(t *testing.T) {
	for _, id := range spacecraft.LoadoutOrder {
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatal(err)
		}
		c := spacecraft.NewFromLoadout(id)
		seed := w.ActiveCraft()
		c.Primary, c.State, c.SystemIdx = seed.Primary, seed.State, seed.SystemIdx
		w.Crafts = []*spacecraft.Spacecraft{c}
		w.ActiveCraftIdx = 0
		path := filepath.Join(t.TempDir(), "s.json")
		if err := save.Save(w, path); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		back, err := save.Load(path)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		got := back.Crafts[0]
		cmp := func(where string, a, b []spacecraft.Stage) {
			if len(a) != len(b) {
				t.Errorf("%s %s: %d stages back, want %d", id, where, len(b), len(a))
				return
			}
			for i := range a {
				if len(a[i].Ports()) != len(b[i].Ports()) || (len(a[i].Ports()) > 0 && !reflect.DeepEqual(a[i].Ports(), b[i].Ports())) {
					t.Errorf("%s %s stage %d (%s): ports %+v after the wire, want %+v", id, where, i, a[i].Name, b[i].Ports(), a[i].Ports())
				}
			}
		}
		cmp("stages", c.Stages, got.Stages)
		if len(got.DockedComponents) != len(c.DockedComponents) {
			t.Fatalf("%s: %d docked components back, want %d", id, len(got.DockedComponents), len(c.DockedComponents))
		}
		for i := range c.DockedComponents {
			cmp("docked "+c.DockedComponents[i].Name, c.DockedComponents[i].Stages, got.DockedComponents[i].Stages)
		}
	}
}

// (c) a VAB-composed stage with the docking-port component docks after a
// save and load, with no design file on disk and a name no catalog knows.
func TestComposedStageWithDockingPortKeepsItAcrossSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st, warn := spacecraft.ComposeStage([]string{"probe-core", "docking-port"}, spacecraft.Components)
	if warn != "" {
		t.Fatal(warn)
	}
	st.Name = "Station Core" // a name in no catalog index
	plain, _ := spacecraft.ComposeStage([]string{"probe-core"}, spacecraft.Components)
	plain.Name = "Plain Core"

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []spacecraft.Stage{st, plain} {
		if _, err := w.SpawnCraft(sim.SpawnSpec{CustomStages: []spacecraft.Stage{s}, AltitudeM: 400e3}); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "s.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	back, err := save.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Crafts[len(back.Crafts)-2].Stages[0]; !got.HasPort() {
		t.Errorf("composed Station Core lost its port across save/load: %+v", got.Ports())
	}
	if got := back.Crafts[len(back.Crafts)-1].Stages[0]; got.HasPort() {
		t.Errorf("composed Plain Core gained a port across save/load: %+v", got.Ports())
	}
}

// #611: the flag round-trips, and a vessel the player spawned onto a pad
// keeps its site after liftoff.
func TestHasLaunchSiteRoundTrips(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	pad, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: "RCS-tug", Launchpad: true, Latitude: 28.6, LongitudeOffset: -80.6})
	if err != nil {
		t.Fatal(err)
	}
	pad.Landed, pad.OnPad = false, false
	path := filepath.Join(t.TempDir(), "s.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	back, err := save.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Crafts[len(back.Crafts)-1].HasLaunchSite {
		t.Error("launched vessel lost HasLaunchSite")
	}
	if back.Crafts[0].HasLaunchSite {
		t.Error("orbit-spawned seed vessel gained HasLaunchSite")
	}
}
