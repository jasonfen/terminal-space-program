package screens

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
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

// parentBodyBoxRows returns the content cells of the PARENT BODY box in the
// rendered spawn form (right column), one string per row between the title
// and the bottom edge.
func parentBodyBoxRows(t *testing.T, out string) []string {
	t.Helper()
	var rows []string
	in := false
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "│PARENT BODY") {
			in = true
			continue
		}
		if !in {
			continue
		}
		parts := strings.Split(ln, "│")
		if len(parts) < 3 {
			break
		}
		if strings.Contains(ln, "╰") {
			break
		}
		// "...│ │content│" + frame edge: content is third from the end.
		rows = append(rows, parts[len(parts)-3])
	}
	return rows
}

// TestSpawnParentBodyHasNoExponent: the PARENT BODY box reads gravity and
// radius through the readout helpers (ADR 0049), never `μ 3.99e+14`, and
// never a trailing-zero `24.790`.
func TestSpawnParentBodyHasNoExponent(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{earthLike()}, "earth", nil, bodies.ScaleReal, "", nil)
	out := ansiRE.ReplaceAllString(s.Render(140, 40), "")
	rows := strings.Join(parentBodyBoxRows(t, out), "\n")
	if !strings.Contains(rows, "Earth") || !strings.Contains(rows, "R 6371 km") || !strings.Contains(rows, "g "+bodyGravity(earthLike())) {
		t.Fatalf("parent body row not found; instrument blind\n%s", out)
	}
	if strings.Contains(rows, "e+") || strings.Contains(rows, "μ") {
		t.Errorf("exponent notation in parent body box: %q", rows)
	}
}

// TestSpawnParentBodyFitsBoxEveryBody: for every body of every shipped
// system, as parent, at the 140x40 Design Size, the PARENT BODY box shows the
// full name, gravity and radius with no ellipsis and no overflow. Widths are
// display cells, not bytes (the m/s² superscript is two bytes, one cell).
func TestSpawnParentBodyFitsBoxEveryBody(t *testing.T) {
	systems, _, err := bodies.LoadAllWithWarnings()
	if err != nil {
		t.Fatal(err)
	}
	_, rw := spawnWidths(140)
	checked := 0
	for _, sys := range systems {
		for i, b := range sys.Bodies {
			s := NewSpawnCraft(Theme{})
			s.Reset(sys.Bodies, sys.Bodies[i].ID, nil, bodies.ScaleReal, sys.Name, nil)
			out := ansiRE.ReplaceAllString(s.Render(140, 40), "")
			rows := parentBodyBoxRows(t, out)
			if len(rows) == 0 {
				t.Fatalf("%s/%s: PARENT BODY box not found; instrument blind\n%s", sys.Name, b.EnglishName, out)
			}
			joined := strings.Join(rows, "\n")
			for _, row := range rows {
				if w := lipgloss.Width(row); w != rw-2 {
					t.Errorf("%s/%s: box row is %d cells, want %d: %q", sys.Name, b.EnglishName, w, rw-2, row)
				}
			}
			if strings.Contains(joined, "…") {
				t.Errorf("%s/%s: PARENT BODY box truncated: %q", sys.Name, b.EnglishName, joined)
			}
			for _, want := range []string{b.EnglishName, "g " + bodyGravity(b), "R " + readout.Distance(b.RadiusMeters())} {
				if !strings.Contains(joined, want) {
					t.Errorf("%s/%s: box lacks %q: %q", sys.Name, b.EnglishName, want, joined)
				}
			}
			// ADR 0049 decision 4: four significant figures. `24.790` and
			// `274.000` are five and six.
			if m := regexp.MustCompile(`g ([0-9.]+) m/s²`).FindStringSubmatch(joined); m == nil {
				t.Errorf("%s/%s: no gravity reading in %q", sys.Name, b.EnglishName, joined)
			} else if sig := len(strings.TrimLeft(strings.ReplaceAll(m[1], ".", ""), "0")); sig > 4 {
				t.Errorf("%s/%s: gravity %q has %d significant figures, want at most 4", sys.Name, b.EnglishName, m[1], sig)
			}
			checked++
		}
	}
	if checked < 50 {
		t.Fatalf("only %d bodies checked; expected the full catalog (52)", checked)
	}
}
