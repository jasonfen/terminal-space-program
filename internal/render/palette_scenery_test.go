package render

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// lightness is CIE L* (0-100) of a "#RRGGBB" colour, the 100-point scale the
// GS grill measured on (#6E6E6E = 46, Dim = 40, #8A8A8A = 57).
func lightness(c lipgloss.Color) float64 {
	var r, g, b int
	fmt.Sscanf(strings.TrimPrefix(string(c), "#"), "%02x%02x%02x", &r, &g, &b)
	lin := func(v int) float64 {
		f := float64(v) / 255
		if f <= 0.04045 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	y := 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
	if y > 0.008856 {
		return 116*math.Cbrt(y) - 16
	}
	return 903.3 * y
}

// TestSceneryGreyIsDistinctFromDim (#593): planet and moon orbits must be
// clearly lighter than Dim, the grey of other players' orbits and dim text.
func TestSceneryGreyIsDistinctFromDim(t *testing.T) {
	// Instrument check: the helper reproduces GS's measured Dim and old values.
	if d := lightness(ColorDim); math.Abs(d-40) > 1 {
		t.Fatalf("lightness helper reads Dim as %.1f, GS measured 40", d)
	}
	if o := lightness(lipgloss.Color("#6E6E6E")); math.Abs(o-46) > 1 {
		t.Fatalf("lightness helper reads #6E6E6E as %.1f, GS measured 46", o)
	}
	gap := lightness(ColorBodyOrbit) - lightness(ColorDim)
	if gap < 57-40-1 {
		t.Errorf("scenery orbit grey %s is only %.1f lighter than Dim %s, want at least 16 (GS: 57 vs 40)",
			ColorBodyOrbit, gap, ColorDim)
	}
	if ColorDim != lipgloss.Color("#5F5F5F") {
		t.Errorf("Dim moved to %s; #593 leaves Dim alone", ColorDim)
	}
}
