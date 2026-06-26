package spacecraft

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// S2 / ADR 0029 §4 — the app-managed designs store. A Design is a saved
// custom vehicle: a Loadout plus the composed Parts it references,
// serialized as a self-contained catalog fragment in its own designs/ dir
// (distinct from the modder overlay so an app-written file can never
// override a built-in by ID collision). Designs are global across saves and
// portable (drop one into the modder overlay dir to publish it as a mod).

// withDesignsAndOverlay points XDG_CONFIG_HOME at a temp root and returns
// the app-managed designs dir plus the modder overlay (loadouts) dir under
// it. Both empty.
func withDesignsAndOverlay(t *testing.T) (designsDir, overlayDir string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	designsDir = filepath.Join(root, "terminal-space-program", "designs")
	overlayDir = filepath.Join(root, "terminal-space-program", "loadouts")
	for _, d := range []string{designsDir, overlayDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return designsDir, overlayDir
}

// writeOverlayComponents drops a components-only overlay file so a design's
// composed parts have components to aggregate against (the embedded
// components.json is an empty stub until S4).
func writeOverlayComponents(t *testing.T, overlayDir string) {
	t.Helper()
	comps := `{"components":[
		{"id":"c-eng","kind":"engine","dry_mass_kg":500,"thrust_n":1000000,"isp_s":320,"fuel_type":"kerolox"},
		{"id":"c-tank","kind":"tank","dry_mass_kg":100,"fuel_capacity_kg":4000,"fuel_type":"kerolox"},
		{"id":"c-core","kind":"command-core","dry_mass_kg":90,"command_source":"probe"}
	]}`
	if err := os.WriteFile(filepath.Join(overlayDir, "comps.json"), []byte(comps), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sampleDesign is a Mun-hopper-style design: one composed stage (engine +
// tank + probe core) under an existing atomic catalog part (s-ivb), with a
// decouple plan. Exercises mixing composed + catalog parts in one design.
func sampleDesign() Design {
	return Design{
		Loadout: LoadoutDef{
			ID:    "my-hopper",
			Name:  "My Hopper",
			Role:  "custom",
			Glyph: VesselGlyph,
			Color: "#AABBCC",
			Parts: []PartRef{
				{PartID: "_my-hopper_s0"}, // composed stage
				{PartID: "s-ivb"},         // atomic catalog part
			},
			DecouplePlan: []int{1},
		},
		Parts: []Part{
			{ID: "_my-hopper_s0", Name: "Hopper Core", Glyph: VesselGlyph, Color: "#AABBCC",
				Components: []string{"c-eng", "c-tank", "c-core"}},
		},
	}
}

// TestDesignRoundTrip — serialize a design to the store, reload it, and
// confirm it resolves to an identical flyable Loadout (the composed stage
// aggregates the same, the atomic catalog part is referenced the same).
func TestDesignRoundTrip(t *testing.T) {
	_, overlayDir := withDesignsAndOverlay(t)
	writeOverlayComponents(t, overlayDir)

	d := sampleDesign()
	want, warnings := d.Resolve()
	if len(warnings) != 0 {
		t.Fatalf("Resolve warnings = %v, want none", warnings)
	}
	if len(want.Stages) != 2 {
		t.Fatalf("resolved stages = %d, want 2", len(want.Stages))
	}

	if err := SaveDesign(d); err != nil {
		t.Fatalf("SaveDesign: %v", err)
	}
	got, ok, warnings := LoadDesign("my-hopper")
	if !ok {
		t.Fatal("LoadDesign(my-hopper) not found after save")
	}
	if len(warnings) != 0 {
		t.Fatalf("LoadDesign warnings = %v, want none", warnings)
	}
	reLoadout, warnings := got.Resolve()
	if len(warnings) != 0 {
		t.Fatalf("reloaded Resolve warnings = %v, want none", warnings)
	}
	if !reflect.DeepEqual(want, reLoadout) {
		t.Errorf("round-trip drift:\n want %+v\n  got %+v", want, reLoadout)
	}
	// Concretely: the composed stage aggregated and the atomic ref came through.
	if reLoadout.Stages[0].Thrust != 1_000_000 || reLoadout.Stages[0].Isp != 320 {
		t.Errorf("composed stage = %g N / %g s, want 1e6/320", reLoadout.Stages[0].Thrust, reLoadout.Stages[0].Isp)
	}
	if reLoadout.Stages[1].Name != "S-IVB" {
		t.Errorf("atomic stage name = %q, want S-IVB", reLoadout.Stages[1].Name)
	}
}

// TestDesignPortabilityAsOverlay — the same design file dropped into the
// modder overlay dir loads as a normal catalog entry (the publish-as-mod
// path). A design is a plain catalog fragment, so the existing overlay
// loader resolves it with no special case.
func TestDesignPortabilityAsOverlay(t *testing.T) {
	_, overlayDir := withDesignsAndOverlay(t)
	writeOverlayComponents(t, overlayDir)

	d := sampleDesign()
	data, err := d.marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "my-hopper.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	parts, defs, warnings, err := LoadCatalogWithWarnings()
	if err != nil {
		t.Fatalf("LoadCatalogWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	// The composed part is in the merged catalog with aggregated stats.
	cp, ok := parts["_my-hopper_s0"]
	if !ok || cp.ThrustN != 1_000_000 {
		t.Errorf("composed part not aggregated in overlay path: %+v", cp)
	}
	// The loadout resolves as a normal catalog loadout.
	var def *LoadoutDef
	for i := range defs {
		if defs[i].ID == "my-hopper" {
			def = &defs[i]
		}
	}
	if def == nil {
		t.Fatal("design loadout not loaded via the overlay path")
	}
	l := resolveLoadout(*def, parts)
	if len(l.Stages) != 2 {
		t.Errorf("resolved overlay loadout stages = %d, want 2", len(l.Stages))
	}
}

// TestDesignMalformedSkipped — a malformed design file is skipped with a
// warning; the other designs in the store still list.
func TestDesignMalformedSkipped(t *testing.T) {
	designsDir, overlayDir := withDesignsAndOverlay(t)
	writeOverlayComponents(t, overlayDir)
	if err := SaveDesign(sampleDesign()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(designsDir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, warnings := ListDesigns()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1 (malformed skipped)", len(warnings))
	}
	if len(designs) != 1 || designs[0].ID() != "my-hopper" {
		t.Errorf("designs = %v, want just my-hopper", designs)
	}
}

// TestDesignIDCollisionIsolation — a design whose loadout ID equals a
// built-in does NOT override the built-in: designs live in their own
// namespace and never merge into the global Loadouts map.
func TestDesignIDCollisionIsolation(t *testing.T) {
	withDesignsAndOverlay(t)
	clash := sampleDesign()
	clash.Loadout.ID = LoadoutSaturnVID
	clash.Loadout.Name = "Not Really Saturn"
	clash.Loadout.Parts = []PartRef{{PartID: "s-ivb"}} // atomic only, no components needed
	clash.Parts = nil
	if err := SaveDesign(clash); err != nil {
		t.Fatal(err)
	}
	// The built-in Saturn-V is untouched in the global catalog.
	if l := Loadouts[LoadoutSaturnVID]; l.Name == "Not Really Saturn" {
		t.Error("a saved design overrode the built-in Saturn-V in Loadouts")
	}
	if len(Loadouts[LoadoutSaturnVID].Stages) != 3 {
		t.Errorf("built-in Saturn-V stages = %d, want 3 (design must not clobber it)", len(Loadouts[LoadoutSaturnVID].Stages))
	}
	// The design IS stored, separately.
	designs, _ := ListDesigns()
	if len(designs) != 1 || designs[0].Name() != "Not Really Saturn" {
		t.Errorf("design not stored in its own namespace: %v", designs)
	}
}

// TestDeleteDesign — delete removes the design from the store and its file.
func TestDeleteDesign(t *testing.T) {
	designsDir, overlayDir := withDesignsAndOverlay(t)
	writeOverlayComponents(t, overlayDir)
	if err := SaveDesign(sampleDesign()); err != nil {
		t.Fatal(err)
	}
	if err := DeleteDesign("my-hopper"); err != nil {
		t.Fatalf("DeleteDesign: %v", err)
	}
	designs, _ := ListDesigns()
	if len(designs) != 0 {
		t.Errorf("designs after delete = %d, want 0", len(designs))
	}
	if _, err := os.Stat(filepath.Join(designsDir, "my-hopper.json")); !os.IsNotExist(err) {
		t.Errorf("design file still present after delete (err=%v)", err)
	}
	// Deleting a missing design is a no-op error the caller can ignore.
	if err := DeleteDesign("nope"); err == nil {
		t.Error("DeleteDesign(missing) should report not-found")
	}
}

// TestDesignResolveRejectsBrokenComposedPart — a design whose composed part
// fails aggregation (mixed fuel chemistry) resolves to an EMPTY loadout +
// warnings, not a flyable-but-dead ghost stage. The spawn path keys off the
// empty loadout to surface the failure instead of spawning dead weight.
func TestDesignResolveRejectsBrokenComposedPart(t *testing.T) {
	_, overlayDir := withDesignsAndOverlay(t)
	comps := `{"components":[
		{"id":"x-eng","kind":"engine","dry_mass_kg":500,"thrust_n":1000000,"isp_s":300,"fuel_type":"kerolox"},
		{"id":"x-hydro","kind":"tank","dry_mass_kg":100,"fuel_capacity_kg":2000,"fuel_type":"hydrolox"}
	]}`
	if err := os.WriteFile(filepath.Join(overlayDir, "c.json"), []byte(comps), 0o644); err != nil {
		t.Fatal(err)
	}
	d := Design{
		Loadout: LoadoutDef{ID: "broken", Name: "Broken", Parts: []PartRef{{PartID: "_broken_s0"}}},
		Parts:   []Part{{ID: "_broken_s0", Components: []string{"x-eng", "x-hydro"}}},
	}
	l, warnings := d.Resolve()
	if len(l.Stages) != 0 {
		t.Errorf("mixed-fuel design resolved %d stages, want 0 (rejected)", len(l.Stages))
	}
	if len(warnings) == 0 {
		t.Error("expected an aggregation warning for the mixed-fuel composed stage")
	}
}

// TestDesignResolveSkipsBadRef — a design whose loadout references a part
// that exists nowhere resolves to a warning, not a panic (hand-edited
// design files must not crash the app).
func TestDesignResolveSkipsBadRef(t *testing.T) {
	withDesignsAndOverlay(t)
	d := Design{Loadout: LoadoutDef{ID: "broken", Name: "Broken", Parts: []PartRef{{PartID: "ghost-part"}}}}
	l, warnings := d.Resolve()
	if len(warnings) == 0 {
		t.Error("Resolve of a dangling ref should warn")
	}
	if len(l.Stages) != 0 {
		t.Errorf("broken design resolved %d stages, want 0", len(l.Stages))
	}
}
