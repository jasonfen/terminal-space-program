package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
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

// TestBodyPixelRadiusMonotonic: perceived-size bucketing is monotonic
// in physical radius. Tier 1 (small) < tier 2 (terrestrial) < tier 4
// (gas giant) < tier 6 (star). System-primary flag promotes to star
// even for small primaries (e.g. a dwarf star that would otherwise
// bucket lower).
func TestBodyPixelRadiusMonotonic(t *testing.T) {
	cases := []struct {
		name   string
		radius float64
		want   int
	}{
		{"tiny moon 500 km", 5e5, 1},
		{"terrestrial Earth 6378 km", 6.378e6, 2},
		{"gas giant Jupiter 69911 km", 6.9911e7, 4},
		{"star Sun 696000 km", 6.96e8, 6},
	}
	prev := 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := bodies.CelestialBody{MeanRadius: c.radius / 1000} // Radius field is in km
			got := BodyPixelRadius(b, false)
			if got != c.want {
				t.Errorf("got pxRadius=%d, want %d (radius %.0f km)",
					got, c.want, c.radius/1000)
			}
			if got < prev {
				t.Errorf("non-monotonic: %s got %d after previous %d",
					c.name, got, prev)
			}
			prev = got
		})
	}
}

// TestBodyPixelRadiusPrimaryFlag: even a sub-star-sized body rendered
// as system primary gets the star tier so the rendering distinguishes
// it from planets.
func TestBodyPixelRadiusPrimaryFlag(t *testing.T) {
	small := bodies.CelestialBody{MeanRadius: 1000} // 1000 km = terrestrial
	nonPrim := BodyPixelRadius(small, false)
	prim := BodyPixelRadius(small, true)
	if prim <= nonPrim {
		t.Errorf("primary flag should promote size: non-primary=%d primary=%d",
			nonPrim, prim)
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
