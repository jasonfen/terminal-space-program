package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestEngineBoxThrottleRowShowsDryOrder (#466): a standing throttle order
// on a dry lit stage reads "✕ DRY" in the warning colour, not "idle".
func TestEngineBoxThrottleRowShowsDryOrder(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8800"))
	v := NewOrbitView(Theme{
		Primary: lipgloss.NewStyle(),
		Warning: warn,
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle(),
	})
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "Descent", DryMass: 500, FuelMass: 0, FuelCapacity: 20, Thrust: 200000, Isp: 250},
		{Name: "Ascent", DryMass: 800, FuelMass: 4000, FuelCapacity: 4000, Thrust: 100000, Isp: 300},
	}
	c.SyncFields()
	c.Throttle = 1
	row := func() string {
		for _, l := range v.buildEngineBox(w) {
			if strings.Contains(l, "throttle:") {
				return l
			}
		}
		return ""
	}
	if r := row(); strings.Contains(r, "DRY") {
		t.Fatalf("no order yet, row must not say DRY: %q", r)
	}
	c.DryOrder = true
	r := row()
	if !strings.Contains(r, "100% "+warn.Render("✕ DRY")) {
		t.Errorf("want warning-coloured '100%% ✕ DRY', got %q", r)
	}
	if strings.Contains(r, "idle") {
		t.Errorf("dry-armed row still says idle: %q", r)
	}
}
