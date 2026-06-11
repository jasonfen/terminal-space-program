package screens

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestApsisMarkersRenderAsUnifiedGlyphs locks the ADR 0020 conversion:
// the live orbit's apoapsis/periapsis render as the ▲/▼ glyphs (via
// SetCellOverlayColored), not the retired FillDisk blobs.
func TestApsisMarkersRenderAsUnifiedGlyphs(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = false
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	// A roomy eccentric orbit so both apsides project clear of the body
	// disk and above the minOrbitPixels zoom-skip, placed at periapsis.
	rPeri := primaryR + 400e3
	rApo := primaryR + 4000e3
	// Place the craft at true anomaly 90° (on the minor axis) with the
	// line of apsides along ±X, so the vessel glyph doesn't sit on top of
	// — and overdraw — either apsis marker cell.
	e := (rApo - rPeri) / (rApo + rPeri)
	p := rPeri * (1 + e) // semi-latus rectum
	vScale := math.Sqrt(mu / p)
	c.State.R.X, c.State.R.Y, c.State.R.Z = 0, p, 0
	c.State.V.X, c.State.V.Y, c.State.V.Z = -vScale, vScale*e, 0

	out := v.Render(w, 0, 200, 60)
	if !strings.ContainsRune(out, render.MarkerGlyph(render.MarkerApoapsis)) {
		t.Errorf("apoapsis glyph %q not found in orbit render", render.MarkerGlyph(render.MarkerApoapsis))
	}
	if !strings.ContainsRune(out, render.MarkerGlyph(render.MarkerPeriapsis)) {
		t.Errorf("periapsis glyph %q not found in orbit render", render.MarkerGlyph(render.MarkerPeriapsis))
	}
}

// TestManeuverMarkerRendersGlyphAndKeepsClickTag verifies that the
// maneuver marker is a single Δ glyph (ADR 0020) while still carrying the
// NodeIdx hit tag so a click on its cell resolves to the planted node.
func TestManeuverMarkerRendersGlyphAndKeepsClickTag(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft from NewWorld")
	}
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{DV: 100, TriggerTime: w.Clock.SimTime.Add(15 * time.Minute)},
		spacecraft.ManeuverNode{DV: 100, TriggerTime: w.Clock.SimTime.Add(45 * time.Minute)},
	)

	out := v.Render(w, 0, 200, 60)
	if !strings.ContainsRune(out, render.MarkerGlyph(render.MarkerManeuver)) {
		t.Errorf("maneuver glyph %q not found in orbit render", render.MarkerGlyph(render.MarkerManeuver))
	}
	// Click tag preserved (same assertion path HitHudNode routes through).
	if countNodeTaggedCells(v) == 0 {
		t.Error("maneuver markers carry no NodeIdx hit tag — node clicks would not resolve")
	}
}
