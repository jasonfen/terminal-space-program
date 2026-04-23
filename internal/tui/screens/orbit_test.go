package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Basic "render path doesn't panic and produces non-empty output" smoke test.
// Covers the critical integration that real tests (TTY-only) can't exercise:
// that Canvas.String()/Project()/HUD lipgloss panels compose into a real frame.
func TestOrbitViewRendersAllSystems(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
	v := NewOrbitView(th)
	v.Resize(120, 40)

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for i := 0; i < len(w.Systems); i++ {
		out := v.Render(w, 0, 120, 40)
		if len(out) == 0 {
			t.Errorf("system %d (%s): empty render", i, w.System().Name)
		}
		if !strings.Contains(out, w.System().Name) {
			t.Errorf("system %d: expected system name %q in render", i, w.System().Name)
		}
		w.CycleSystem()
	}
}

func TestOrbitViewZoom(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(80, 24)
	w, _ := sim.NewWorld()
	v.Render(w, 0, 80, 24) // triggers autoFit
	before := v.canvas.Scale()
	v.ZoomIn()
	if v.canvas.Scale() <= before {
		t.Errorf("ZoomIn did not increase scale (before=%.3e after=%.3e)",
			before, v.canvas.Scale())
	}
}
