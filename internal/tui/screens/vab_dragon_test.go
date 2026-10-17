package screens

import (
	"strings"
	"testing"
)

// ADR 0057 decisions 1 and 4: the three Dragon parts sit in the payload
// section of the palette, the two Dragon subassemblies in the subassembly
// section, and the inspector on Dragon (only) states the SuperDracos.
func TestVABPaletteHasDragonParts(t *testing.T) {
	v := realVAB(t)
	want := map[string]string{"dragon-crew": "Dragon", "dragon-cargo": "Cargo Dragon", "dragon-trunk": "Dragon Trunk"}
	for id, name := range want {
		found := false
		for _, it := range v.palette {
			if it.isComponent || it.id != id {
				continue
			}
			found = true
			kind, label := v.paletteItemLabel(it)
			if kind != "payload" || !strings.HasPrefix(label, name+" [payload]") {
				t.Errorf("%s: section %q label %q, want payload / %q", id, kind, label, name+" [payload]")
			}
		}
		if !found {
			t.Errorf("%s is not in the palette", id)
		}
	}
	for id, name := range map[string]string{"dragon": "Dragon", "cargo-dragon": "Cargo Dragon"} {
		found := false
		for _, it := range v.palette {
			if it.subassemblyID != id {
				continue
			}
			found = true
			kind, label := v.paletteItemLabel(it)
			if kind != "subassembly" || label != name+" [stage · 2 stages]" {
				t.Errorf("%s: section %q label %q, want subassembly / %q", id, kind, label, name+" [stage · 2 stages]")
			}
		}
		if !found {
			t.Errorf("subassembly %s is not in the palette", id)
		}
	}
}

func TestVABInspectorStatesSuperDracosOnDragonOnly(t *testing.T) {
	v := realVAB(t)
	inspect := func(id string) string {
		selectPalettePart(t, v, id)
		v.focus = focusPalette
		return stripANSI(strings.Join(v.renderInspector(52), "\n"))
	}
	const line = "8 SuperDraco abort engines, 71 kN each"
	got := inspect("dragon-crew")
	if !strings.Contains(got, line) {
		t.Errorf("Dragon inspector lacks the SuperDraco line:\n%s", got)
	}
	// Between the tier line and the add line.
	tier, desc, add := strings.Index(got, "catalog part · payload"), strings.Index(got, line), strings.Index(got, "adds as a new opaque stage")
	if !(tier >= 0 && tier < desc && desc < add) {
		t.Errorf("SuperDraco line is not between the tier line and the add line:\n%s", got)
	}
	for _, id := range []string{"dragon-cargo", "dragon-trunk", "capsule"} {
		if g := inspect(id); strings.Contains(g, "SuperDraco") || strings.Contains(g, "abort") {
			t.Errorf("%s inspector must not state SuperDracos:\n%s", id, g)
		}
	}
}
