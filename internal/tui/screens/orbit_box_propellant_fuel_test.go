package screens

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// PROPELLANT's fuel: cell is the lit stage's tank as a percentage only
// (Jason 2026-10-10: "fuel readout doesn't need % AND (X t) in its
// display. just keep %."). The mass rides on mass: beside it.
func TestPropellantFuelReadsPercentOnly(t *testing.T) {
	w, _ := spawnLandedOnEarthAt28p6(t)
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	box := ansi.Strip(strings.Join(v.buildPropellantBox(w), "\n"))
	if !regexp.MustCompile(`fuel:\s+\d+%\s{2,}mass:`).MatchString(box) {
		t.Errorf("fuel: should read just the percentage before mass:\n%s", box)
	}
}
