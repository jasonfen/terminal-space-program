package screens

import (
	"strings"
	"testing"
)

// ADR 0060 decision 6 guard 11: `SLS + Orion` opened from the o list shows
// five stage headers with the ESM and Orion rows as the ADR prints them, and
// the palette's subassembly section lists Orion.
func TestVABSLSOrionHeaders(t *testing.T) {
	v := subVAB(t)
	v.HandleKey("o")
	moveLoadTo(t, v, "SLS-Orion")
	v.HandleKey("enter")
	if len(v.stages) != 5 {
		t.Fatalf("stages = %d, want 5", len(v.stages))
	}
	out := stripANSI(strings.Join(v.renderVehicleColumn(100, 60), "\n"))
	headers := map[string]string{} // "S4" -> header text (the column draws the top stage first)
	for _, ln := range strings.Split(out, "\n") {
		if h := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(ln), "▸ ")); len(h) > 3 && h[0] == 'S' && h[1] >= '1' && h[1] <= '9' && h[2] == ' ' {
			headers[h[:2]] = h
		}
	}
	if len(headers) != 5 {
		t.Fatalf("stage headers = %d (%q), want 5:\n%s", len(headers), headers, out)
	}
	for k, want := range map[string]string{
		"S4": "S4  ESM · 27kN@316s · Δv 1295",
		"S5": "S5  Orion · no engine · Δv 0 · port",
	} {
		if headers[k] != want {
			t.Errorf("header %s = %q, want %q", k, headers[k], want)
		}
	}
	for k, h := range headers {
		if strings.Contains(h, "· port") != (k == "S5") {
			t.Errorf("header %s = %q: `· port` only belongs on S5", k, h)
		}
	}
}

func TestVABPaletteHasOrionSubassembly(t *testing.T) {
	v := realVAB(t)
	found := false
	for _, it := range v.palette {
		if it.subassemblyID != "orion" {
			continue
		}
		found = true
		kind, label := v.paletteItemLabel(it)
		if kind != "subassembly" || label != "Orion [stage · 2 stages]" {
			t.Errorf("orion: section %q label %q, want subassembly / %q", kind, label, "Orion [stage · 2 stages]")
		}
	}
	if !found {
		t.Error("subassembly orion is not in the palette")
	}
}
