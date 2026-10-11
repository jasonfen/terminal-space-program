package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// sgrHas reports whether the escape sequence esc sets a 24-bit foreground
// within one unit per channel of c (termenv rounds through its own colour
// conversion, so #FFD93D is emitted as 255;217;60).
func sgrHas(esc string, c lipgloss.Color) bool {
	var r, g, b int
	fmt.Sscanf(strings.TrimPrefix(string(c), "#"), "%02x%02x%02x", &r, &g, &b)
	for dr := -1; dr <= 1; dr++ {
		for dg := -1; dg <= 1; dg++ {
			for db := -1; db <= 1; db++ {
				if strings.Contains(esc, fmt.Sprintf("38;2;%d;%d;%d", r+dr, g+dg, b+db)) {
					return true
				}
			}
		}
	}
	return false
}

// sgrBeforeGlyph returns the SGR escape that precedes the first occurrence
// of glyph standing alone as a styled cell (the next byte is an escape, so
// readout text such as "Δv" is skipped). "" when there is none.
func sgrBeforeGlyph(out, glyph string) string {
	from := 0
	for {
		i := strings.Index(out[from:], glyph)
		if i < 0 {
			return ""
		}
		i += from
		from = i + len(glyph)
		if !strings.HasPrefix(out[from:], "\x1b") {
			continue
		}
		if j := strings.LastIndex(out[:i], "\x1b["); j >= 0 {
			return out[j:i]
		}
	}
}

// TestPlannedNodeColourIsNotChrome (#592): the planned-node marker was the
// Primary chrome cyan (#5FD7FF), so a burn read as UI instead of as
// something on the orbit. ColorPlannedNode must differ from the theme's
// Primary AND a planted node must actually paint in it on the map.
func TestPlannedNodeColourIsNotChrome(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	th := DefaultTheme()
	primary, ok := th.Primary.GetForeground().(lipgloss.Color)
	if !ok {
		t.Fatalf("theme Primary foreground is %T, want lipgloss.Color", th.Primary.GetForeground())
	}
	if render.ColorPlannedNode == primary {
		t.Errorf("ColorPlannedNode %q is the theme Primary %q: a node reads as chrome", render.ColorPlannedNode, primary)
	}

	v := screens.NewOrbitView(screens.Theme{
		Primary: th.Primary, Warning: th.Warning, Alert: th.Alert, Dim: th.Dim,
		HUDBox: th.HUDBox, Footer: th.Footer, Title: th.Title,
	})
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{DV: 100, TriggerTime: w.Clock.SimTime.Add(15 * time.Minute)})
	out := v.Render(w, 0, 200, 60)

	glyph := string(render.MarkerGlyph(render.MarkerManeuver))
	if !strings.Contains(out, glyph) {
		t.Fatalf("no %q marker in the map render", glyph)
	}
	// Positive control: the finder sees a colour at all, and it can see the
	// vessel's own colour on its own glyph, so a "not cyan" result below is
	// a measurement and not a parse miss.
	if got := sgrBeforeGlyph(out, spacecraft.VesselGlyph); !sgrHas(got, render.ColorCraftMarker) {
		t.Fatalf("instrument cannot see the vessel's colour %s before its glyph: %q", render.ColorCraftMarker, got)
	}
	got := sgrBeforeGlyph(out, glyph)
	if !sgrHas(got, render.ColorPlannedNode) {
		t.Errorf("planted node paints %q, want ColorPlannedNode %s", got, render.ColorPlannedNode)
	}
	if sgrHas(got, primary) {
		t.Errorf("planted node paints in the chrome cyan: %q", got)
	}
}

// TestPlannedNodeColourIsDistinctFromEveryMarkerAndLine: the node colour has
// a CIE76 distance of at least 30 from every other marker, line and UI
// colour the map uses, measured in Lab (#592).
func TestPlannedNodeColourIsDistinctFromEveryMarkerAndLine(t *testing.T) {
	others := map[string]lipgloss.Color{
		"Alert": render.ColorAlert, "Warning": render.ColorWarning, "Trajectory": render.ColorTrajectory,
		"CraftMarker": render.ColorCraftMarker, "ForeignSOI": render.ColorForeignSOI, "Dim": render.ColorDim,
		"BodyOrbit": render.ColorBodyOrbit, "Flame": render.ColorFlame, "Target": render.ColorTarget,
		"CommLink": render.ColorCommLink, "Apoapsis": render.ColorMarkerApoapsis,
		"Periapsis": render.ColorMarkerPeriapsis, "AN": render.ColorMarkerAscendingNode,
		"DN": render.ColorMarkerDescendingNode, "ClosestApproach": render.ColorMarkerClosestApproach,
		"Impact": render.ColorMarkerImpact, "Primary chrome": lipgloss.Color("#5FD7FF"),
		"leg 1": render.ManeuverSegmentColor(1), "leg 2": render.ManeuverSegmentColor(2), "leg 3": render.ManeuverSegmentColor(3),
	}
	if d := labDistance(render.ColorPlannedNode, lipgloss.Color("#5FD7FF")); d < 30 {
		t.Errorf("ColorPlannedNode is only %.1f from the chrome cyan", d)
	}
	for name, c := range others {
		if d := labDistance(render.ColorPlannedNode, c); d < 30 {
			t.Errorf("ColorPlannedNode %s is only %.1f (CIE76) from %s %s", render.ColorPlannedNode, d, name, c)
		}
	}
	// Instrument check: identical colours measure 0.
	if d := labDistance(render.ColorAlert, render.ColorAlert); d != 0 {
		t.Errorf("labDistance(x, x) = %v", d)
	}
}
