package spacecraft

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// adr0053PortedParts is ADR 0053 decision 3's list, spelled out here rather
// than read back from parts.json so the test can disagree with the data.
var adr0053PortedParts = []string{
	"command-module", "csm", "csm-lm", "capsule", "kern-pod",
	"apollo-ascent", "lander-ascent", "socket-ascent",
	"rcs-tug", "nudge-tug", "ntr-tug", "ion-keeper",
	"deep-space-relay", "thread-relay", "heartbeat-keeper", "uplink-relay",
	"endpoint-station", "probe-sat-stage",
	// ADR 0057 decision 1: the two Dragon capsules carry the nose port.
	"dragon-crew", "dragon-cargo",
}

// The `ports` field in parts.json reaches the stage lookup: every part on
// the ADR's list reads ported through BuildStage (catalog modules carry an
// empty LoadoutID, so this is the name-fallback path), every other part
// reads port-less, and nothing else is flagged.
func TestPartsJSONPortsReachStageLookup(t *testing.T) {
	want := map[string]bool{}
	for _, id := range adr0053PortedParts {
		want[id] = true
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no developer overlay in the way
	parts, _, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for id, p := range parts {
		st, ok := BuildStage(id)
		if !ok {
			t.Fatalf("BuildStage(%q) not found", id)
		}
		if got := len(p.Ports) > 0; got != want[id] {
			t.Errorf("parts.json %s: ports present = %v, want %v", id, got, want[id])
		}
		if got := st.HasPort(); got != want[id] {
			t.Errorf("BuildStage(%s).HasPort() = %v, want %v", id, got, want[id])
		}
		if want[id] {
			ports := st.Ports()
			if len(ports) != 1 || ports[0].End != PortEndTop {
				t.Errorf("%s ports = %+v, want one top port", id, ports)
			}
		}
	}
	for id := range want {
		if _, ok := parts[id]; !ok {
			t.Errorf("ADR lists part %q, which the catalog does not have", id)
		}
	}
}

// ADR 0053 decision 3: of the 36 shipped loadouts (the 35 plus the Apollo-TLI
// default, decision 4), 17 spawn with a ported top stage, 19 do not; ADR 0057
// adds four Dragon vessels, all ported (21 of 40). Names
// the 21 so adding a loadout later fails here loudly instead of shifting a
// count.
func TestLoadoutTopStagePortsMatchADR(t *testing.T) {
	ported := []string{
		"Lander", "Apollo-Stack", "Apollo-TLI", "Capsule", "Kern-Stack",
		"RCS-tug", "Relay-Tug", "Station-Keeper", "Deep-Space-Relay",
		"Thread-Relay", "Heartbeat-Keeper", "Uplink-Relay", "Nudge-Tug",
		"Endpoint-Station", "Socket-Lander", "Token-Pod", "Probe-Sat",
		// ADR 0057: the four Dragon vessels top out on a Dragon capsule.
		"Dragon", "Cargo-Dragon", "Falcon-9-Dragon", "Falcon-9-Cargo-Dragon",
	}
	want := map[string]bool{}
	for _, id := range ported {
		want[id] = true
	}
	var got []string
	for id, l := range Loadouts {
		top := l.Stages[len(l.Stages)-1]
		if top.HasPort() {
			got = append(got, id)
		}
		if top.HasPort() != want[id] {
			t.Errorf("loadout %s top stage %q HasPort = %v, want %v", id, top.Name, top.HasPort(), want[id])
		}
	}
	sort.Strings(got)
	if len(got) != len(ported) {
		t.Errorf("%d loadouts spawn with a ported top stage, want %d: %v", len(got), len(ported), got)
	}
}

// A carrier's tug is ported only once its payloads are gone: the loaded
// stack's top is a payload, the bare tug's is the tug.
func TestCarrierPortFollowsTopStage(t *testing.T) {
	l := Loadouts["Comsat-Carrier-3"]
	if l.Stages[len(l.Stages)-1].HasPort() {
		t.Error("loaded carrier top (Relay Comsat) must be port-less")
	}
	if !l.Stages[0].HasPort() {
		t.Error("the carrier's tug stage itself carries a port (it is the top once payloads deploy)")
	}
}

// Nothing about the port is stored on the Stage: a stage with an unknown
// name and no loadout (a custom VAB stage) has none.
func TestUnknownStageHasNoPort(t *testing.T) {
	if (Stage{Name: "Stage 1"}).HasPort() {
		t.Error("custom-named stage reads ported")
	}
	if (Stage{}).HasPort() {
		t.Error("zero stage reads ported")
	}
}

// A user overlay part that replaces an embedded one and omits `ports` reads
// port-less, both through its loadout and through the name fallback.
func TestOverlayPartWithoutPortsReadsPortless(t *testing.T) {
	savedLoadouts, savedOrder, savedCatalog, saved := Loadouts, LoadoutOrder, StageCatalog, portIdx
	t.Cleanup(func() { Loadouts, LoadoutOrder, StageCatalog, portIdx = savedLoadouts, savedOrder, savedCatalog, saved })

	dir := withUserCatalog(t)
	user := `{"parts":[{"id":"capsule","name":"Capsule","dry_mass_kg":5000,"launch_sprite_rows_px":6,"launch_sprite_width_px":3}]}`
	if err := os.WriteFile(filepath.Join(dir, "o.json"), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Loadouts["Capsule"].Stages[0].HasPort() {
		t.Fatal("setup: embedded Capsule should be ported before the overlay")
	}
	if w := LoadCatalogOverlay(); len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	if Loadouts["Capsule"].Stages[0].HasPort() {
		t.Error("overlay Capsule without ports reads ported via its loadout")
	}
	if st, _ := BuildStage("capsule"); st.HasPort() {
		t.Error("overlay Capsule without ports reads ported via the name fallback")
	}
	// Untouched neighbours keep their ports.
	if !Loadouts["RCS-tug"].Stages[0].HasPort() {
		t.Error("overlay of one part cleared an unrelated part's port")
	}
}

// A name shared by a ported and a port-less part reads port-less on the
// fallback path (the conservative reading of the ADR's accepted residual
// risk), while the owning loadout still answers authoritatively.
func TestNameCollisionFallbackIsConservative(t *testing.T) {
	parts := map[string]Part{
		"a": {ID: "a", Name: "Twin", Ports: []PartPort{{End: PortEndTop}}},
		"b": {ID: "b", Name: "Twin"},
		"c": {ID: "c", Name: "Solo", Ports: []PartPort{{End: PortEndTop}}},
	}
	defs := []LoadoutDef{{ID: "L", Parts: []PartRef{{PartID: "a"}}}}
	idx := buildPortIndex(parts, defs)
	if got := idx.lookup("", "Twin"); len(got) != 0 {
		t.Errorf("colliding name fallback = %v, want none", got)
	}
	if got := idx.lookup("L", "Twin"); len(got) != 1 {
		t.Errorf("owning loadout lookup = %v, want its part's port", got)
	}
	if got := idx.lookup("", "Solo"); len(got) != 1 {
		t.Errorf("unique ported name fallback = %v, want one", got)
	}
}
