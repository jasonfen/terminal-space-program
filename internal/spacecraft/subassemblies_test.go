package spacecraft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR 0056 S4: the Subassemblies Store. Three ship embedded; a user file wins
// on ID; delete removes only the user file.

func subIDs(subs []Subassembly) string {
	var ids []string
	for _, s := range subs {
		ids = append(ids, s.ID())
	}
	return strings.Join(ids, ",")
}

func TestShippedSubassembliesLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	subs, warns := ListSubassemblies()
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if got := subIDs(subs); got != "comsat,lm,csm-lm,dragon,cargo-dragon,orion" {
		t.Fatalf("shipped subassemblies = %q, want comsat,lm,csm-lm,dragon,cargo-dragon,orion", got)
	}
	want := map[string]struct {
		role  string
		parts string
		plan  string
	}{
		"comsat":       {"payload", "relay-comsat", ""},
		"lm":           {"payload", "apollo-descent,apollo-ascent", ""},
		"csm-lm":       {"stage", "apollo-sm,command-module,apollo-descent,apollo-ascent", "[2]"},
		"dragon":       {"stage", "dragon-trunk,dragon-crew", ""},
		"cargo-dragon": {"stage", "dragon-trunk,dragon-cargo", ""},
		"orion":        {"stage", "orion-esm,orion-cm", ""},
	}
	for _, s := range subs {
		w := want[s.ID()]
		if s.Role() != w.role {
			t.Errorf("%s role = %q, want %q", s.ID(), s.Role(), w.role)
		}
		var ids []string
		for _, r := range s.Loadout.Parts {
			ids = append(ids, r.PartID)
			if _, ok := StageCatalog[r.PartID]; !ok {
				t.Errorf("%s references unknown part %q", s.ID(), r.PartID)
			}
		}
		if strings.Join(ids, ",") != w.parts {
			t.Errorf("%s parts = %v, want %s", s.ID(), ids, w.parts)
		}
		plan := ""
		if len(s.Loadout.NosePayloadPlan) > 0 {
			plan = "[2]"
		}
		if plan != w.plan {
			t.Errorf("%s nose plan = %v, want %q", s.ID(), s.Loadout.NosePayloadPlan, w.plan)
		}
		if !s.Shipped() {
			t.Errorf("%s should read as shipped", s.ID())
		}
	}
}

func TestSubassemblyRoundTripAndOverlay(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	own := Subassembly{Design: Design{Loadout: LoadoutDef{
		ID: "comsat-x3", Name: "Comsat x3", Role: "custom", SubassemblyRole: "payload",
		Parts: []PartRef{{PartID: "relay-comsat"}, {PartID: "relay-comsat"}, {PartID: "relay-comsat"}},
	}}}
	if err := SaveSubassembly(own); err != nil {
		t.Fatal(err)
	}
	subs, _ := ListSubassemblies()
	if got := subIDs(subs); got != "comsat,lm,csm-lm,dragon,cargo-dragon,orion,comsat-x3" {
		t.Fatalf("after save = %q", got)
	}
	back := subs[6]
	if back.Role() != "payload" || len(back.Loadout.Parts) != 3 || back.Shipped() {
		t.Errorf("round trip lost role / parts / provenance: %+v", back.Loadout)
	}

	// A user file with a shipped ID replaces it in place (user wins on ID).
	rep := Subassembly{Design: Design{Loadout: LoadoutDef{
		ID: "comsat", Name: "Comsat", Role: "custom", SubassemblyRole: "stage",
		Parts: []PartRef{{PartID: "relay-comsat"}, {PartID: "relay-comsat"}},
	}}}
	if err := SaveSubassembly(rep); err != nil {
		t.Fatal(err)
	}
	subs, _ = ListSubassemblies()
	if got := subIDs(subs); got != "comsat,lm,csm-lm,dragon,cargo-dragon,orion,comsat-x3" {
		t.Fatalf("replace changed the order: %q", got)
	}
	if subs[0].Role() != "stage" || len(subs[0].Loadout.Parts) != 2 || subs[0].Shipped() {
		t.Errorf("user file did not win on ID: %+v", subs[0].Loadout)
	}

	// Deleting the replacement restores the shipped block; the shipped block
	// itself cannot be deleted.
	if err := DeleteSubassembly("comsat"); err != nil {
		t.Fatal(err)
	}
	subs, _ = ListSubassemblies()
	if subs[0].Role() != "payload" || !subs[0].Shipped() {
		t.Errorf("delete did not restore the shipped Comsat: %+v", subs[0].Loadout)
	}
	if err := DeleteSubassembly("comsat"); err == nil {
		t.Error("deleting a shipped subassembly should be refused")
	}
	if err := DeleteSubassembly("comsat-x3"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(SubassembliesDir(), "comsat-x3.json")); !os.IsNotExist(err) {
		t.Errorf("file still on disk: %v", err)
	}
}

// A subassembly file is a catalog fragment with one extra loadout field, so
// copying it into loadouts/ still parses (ADR 0029's portability rule).
func TestSubassemblyFileParsesAsLoadoutOverlay(t *testing.T) {
	dir := t.TempDir()
	data, err := catalogFS.ReadFile("data/subassemblies/comsat.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comsat.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, defs, warns := mergeUserCatalog(map[string]Component{}, map[string]Part{}, nil, dir)
	if len(warns) != 0 || len(defs) != 1 || defs[0].SubassemblyRole != "payload" {
		t.Errorf("defs=%v warns=%v", defs, warns)
	}
}
