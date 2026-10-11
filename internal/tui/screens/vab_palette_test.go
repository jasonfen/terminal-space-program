package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// paletteRowFor renders the real VAB at the Design Size with the palette
// cursor on the item, and returns the plain-text line carrying the cursor
// marker, so the assertion reads what the player reads.
func paletteRowFor(t *testing.T, v *VAB, idx int) string {
	t.Helper()
	v.focus = focusPalette
	v.paletteIdx = idx
	out := v.Render(140, 40)
	for _, ln := range strings.Split(out, "\n") {
		// "││▸ ▼ Name ... │ │VEHICLE": the palette box is the text between
		// the second and third border runes.
		parts := strings.Split(ln, "│")
		if len(parts) > 3 && strings.HasPrefix(parts[2], "▸ ") {
			return parts[2]
		}
	}
	t.Fatalf("no palette cursor row rendered for item %d", idx)
	return ""
}

func realVAB(t *testing.T) *VAB {
	t.Helper()
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)
	return v
}

// TestVABPaletteRowNamesChemistry (#597): an engine or tank palette row names
// its fuel chemistry in text, not by hue alone.
func TestVABPaletteRowNamesChemistry(t *testing.T) {
	v := realVAB(t)
	n := 0
	for i, it := range v.palette {
		if !it.isComponent {
			continue
		}
		c := v.comps[it.id]
		if (c.Kind != spacecraft.ComponentEngine && c.Kind != spacecraft.ComponentTank) || c.FuelType == "" {
			continue
		}
		n++
		row := paletteRowFor(t, v, i)
		if !strings.Contains(row, c.FuelType) {
			t.Errorf("%s row %q does not name its chemistry %q", c.ID, strings.TrimSpace(row), c.FuelType)
		}
		if w := len([]rune(row)); w > 50 {
			t.Errorf("%s row is %d cells, wider than the palette box", c.ID, w)
		}
	}
	if n < 10 {
		t.Fatalf("only %d fuelled rows checked; the catalog did not load", n)
	}
}

// TestVABPaletteYellowRowsDifferInText (#597): the LT-50 probe engine (its own
// #FFD93D yellow) and the hypergolic engines (yellow-amber) are near-identical
// yellows; their rows must differ in text, not only in hue.
func TestVABPaletteYellowRowsDifferInText(t *testing.T) {
	v := realVAB(t)
	find := func(id string) string {
		for i, it := range v.palette {
			if it.isComponent && it.id == id {
				return paletteRowFor(t, v, i)
			}
		}
		t.Fatalf("%s not in palette", id)
		return ""
	}
	probe, sps := find("probe-engine"), find("sps-transfer")
	word := func(row, chem string) bool { return strings.Contains(row, chem) }
	if !word(probe, "kerolox") || !word(sps, "hypergolic") || word(probe, "hypergolic") || word(sps, "kerolox") {
		t.Errorf("yellow rows not told apart in text:\n  %q\n  %q", strings.TrimSpace(probe), strings.TrimSpace(sps))
	}
}
