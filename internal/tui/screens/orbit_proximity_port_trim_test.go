package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func trimRowValue(chip []string) string {
	row := chipLine(chip, "trim:")
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ansi.Strip(row)), "trim:"))
}

// ADR 0055 decision 4: in PORT the trim: row reads the port pair as
// `1° up  2° left (port)`; outside PORT it is the surface pair (no tag).
// Sabotage: the old row (surface pair in PORT) prints `0°  hdg: 090°`.
func TestProximityTrimRowReadsThePortPairInPort(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 5}, 0)
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	c := w.ActiveCraft()
	w.NavMode = sim.NavPort

	cases := []struct {
		up, right float64 // degrees
		want      string
	}{
		{1, -2, "1° up  2° left (port)"},
		{-3, 2, "3° down  2° right (port)"},
		{3, 0, "3° up (port)"},
		{0, -0.25, "0.25° left (port)"},
		{0, 0, "0° (port)"},
	}
	for _, k := range cases {
		c.PortPitchTrim = k.up * math.Pi / 180
		c.PortYawTrim = k.right * math.Pi / 180
		if got := trimRowValue(v.buildProximityChip(w)); got != k.want {
			t.Errorf("port pair (%v up, %v right): trim row = %q, want %q", k.up, k.right, got, k.want)
		}
	}

	// Outside PORT the same pair is not shown: the surface pair is.
	w.NavMode = sim.NavOrbit
	if got := trimRowValue(v.buildProximityChip(w)); strings.Contains(got, "(port)") {
		t.Errorf("trim row outside PORT carries the port tag: %q", got)
	}
}

// The row's longest case fits the 26-cell value field.
func TestProximityTrimRowPortFitsTheValueField(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 5}, 0)
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	c := w.ActiveCraft()
	w.NavMode = sim.NavPort
	c.PortPitchTrim = -12 * math.Pi / 180
	c.PortYawTrim = 12.75 * math.Pi / 180
	got := trimRowValue(v.buildProximityChip(w))
	if n := len([]rune(got)); n > proximityChipInnerWidth-chipValueCol {
		t.Errorf("%q is %d cells, value field is 26", got, n)
	}
}

// Compact Form keeps a live trim visible; a live PORT trim counts.
func TestProximityCompactKeepsPortTrimRow(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 5}, 0)
	v := NewOrbitView(chipTestTheme())
	c := w.ActiveCraft()
	w.NavMode = sim.NavPort
	c.PortPitchTrim = 2 * math.Pi / 180
	chip := v.buildProximityChipCompact(w)
	if got := trimRowValue(chip); got != "2° up (port)" {
		t.Errorf("compact chip trim row = %q (chip %q)", got, chip)
	}
}

// ADR 0055 slice 3: the DOCKING page carries the arrows (PORT) row with the
// 1° / 0.25° steps the code uses, and stays at 15 rows.
func TestHelpDockingArrowsRowQuotesTheSteps(t *testing.T) {
	var rows [][2]string
	for _, s := range helpSections {
		if strings.HasPrefix(s.header, "DOCKING") {
			rows = s.rows
		}
	}
	var text string
	for _, r := range rows {
		if r[0] == "arrows (PORT)" {
			text = r[1]
		}
	}
	if text == "" {
		t.Fatal("DOCKING page has no arrows (PORT) row")
	}
	for _, want := range []string{"1°", "0.25°", "| clears"} {
		if !strings.Contains(text, want) {
			t.Errorf("arrows row %q does not quote %q", text, want)
		}
	}
	if len(rows) != 15 {
		t.Errorf("DOCKING page has %d rows, want 15", len(rows))
	}
}
