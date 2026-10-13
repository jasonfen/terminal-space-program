package screens

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestSpawnCatalogMassesRideTheLadder: every catalog row prints dry / fuel
// mass through ADR 0049's ladder (kg below 1000 kg, then t, unit spaced), so
// no row reads `181000kg` and the Saturn V reads tonnes.
func TestSpawnCatalogMassesRideTheLadder(t *testing.T) {
	old := regexp.MustCompile(`\d{4,} ?kg`)
	glued := regexp.MustCompile(`(dry|fuel) \d+kg`)
	for _, width := range []int{90, 130} {
		plain, _ := catalogPlainRows(t, Theme{}, width)
		tonnes := 0
		for _, row := range plain {
			if glued.MatchString(row) || old.MatchString(row) {
				t.Errorf("width %d: old-dialect mass in row %q", width, row)
			}
			if regexp.MustCompile(`(dry|fuel) \d+(\.\d+)? t\b`).MatchString(row) {
				tonnes++
			}
		}
		if tonnes < 2 {
			t.Errorf("width %d: only %d rows show a tonne mass; instrument cannot see the t rung", width, tonnes)
		}
	}
}

// TestSpawnParentBodyHasNoExponent: the PARENT BODY box reads gravity and
// radius on the distance ladder, never `μ 3.99e+14`.
func TestSpawnParentBodyHasNoExponent(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{earthLike()}, "earth", nil, bodies.ScaleReal, "", nil)
	out := ansiRE.ReplaceAllString(s.Render(140, 40), "")
	found := false
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "Earth") && strings.Contains(ln, "R ") && strings.Contains(ln, "km") {
			found = true
			if strings.Contains(ln, "e+") || strings.Contains(ln, "μ") {
				t.Errorf("exponent notation in parent body row: %q", ln)
			}
		}
	}
	if !found {
		t.Fatalf("parent body row not found; instrument blind\n%s", out)
	}
}
