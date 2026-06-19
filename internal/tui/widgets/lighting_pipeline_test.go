package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
)

// loadSolBody fetches a body from the embedded Sol catalog by id.
func loadSolBody(t *testing.T, id string) bodies.CelestialBody {
	t.Helper()
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for _, s := range systems {
		if s.Name != "Sol" {
			continue
		}
		for _, b := range s.Bodies {
			if b.ID == id {
				return b
			}
		}
	}
	t.Fatalf("Sol body %q not found", id)
	return bodies.CelestialBody{}
}

// TestTerminatorReachesDistinctCells is the v0.9.6 end-to-end check:
// a shaded body disk, painted through the real FillTexturedDiskTagged
// → String() pixel-aggregation path, must emit ANSI and contain more
// than one distinct foreground color — i.e. the day/night terminator
// survives the per-cell dominant-color aggregation and is actually
// visible, not averaged away.
func TestTerminatorReachesDistinctCells(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()

	// Earth's surface texture is data-driven from sol.json (ADR 0024),
	// so load the real catalog body — a bare CelestialBody carries no
	// Texture and would render flat.
	earth := loadSolBody(t, "earth")
	const r = 30
	// Sub-solar at the +x limb → strong day/night gradient across
	// the disk so adjacent cells differ.
	light := &render.SolarLight{SubSolarLatDeg: 0, SubSolarLonDeg: 90, EclipseFactor: 1}
	tex := render.TextureFor(earth, r, 0, 0, 0, 1, light)
	if tex == nil {
		t.Fatal("earth texture nil")
	}
	c.FillTexturedDiskTagged(orbital.Vec3{}, r, func(dx, dy int) lipgloss.Color {
		return tex(dx, dy, r)
	}, CellTag{Color: render.ColorFor(earth), BodyID: "earth"})

	out := c.String()
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("shaded disk emitted no ANSI escape")
	}
	// Collect distinct SGR parameter strings (the text between
	// "\x1b[" and the terminating 'm').
	seen := map[string]bool{}
	for _, seg := range strings.Split(out, "\x1b[")[1:] {
		if i := strings.IndexByte(seg, 'm'); i >= 0 {
			seen[seg[:i]] = true
		}
	}
	if len(seen) < 2 {
		t.Errorf("want ≥2 distinct foreground codes (day vs night), got %d: %v", len(seen), seen)
	}
}
