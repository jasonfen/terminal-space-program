package widgets

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// ADR 0041 §3 extends per-pixel hit-testing from glyphs and disks to ORBIT
// LINES, threaded through the class drawers (the one chokepoint every
// trajectory call site already goes through). These tests pin the property
// that makes "click a line, learn whose it is" possible at all: a lit
// trajectory pixel remembers its owner, and HitAt hands that owner back for
// the cell the player clicked.

func ownerTestCanvas() *Canvas {
	c := NewCanvas(80, 24)
	c.SetScale(1e-5) // ~10 px per 1000 km — an ellipse that fits the canvas
	c.Center(orbital.Vec3{})
	return c
}

func circularElements(radius float64) orbital.Elements {
	return orbital.Elements{A: radius, E: 0}
}

// findOwnedCell returns a terminal cell whose HitAt resolves to owner.
func findOwnedCell(c *Canvas, owner string) (col, row int, found bool) {
	for r := 0; r < c.Rows(); r++ {
		for col := 0; col < c.Cols(); col++ {
			if c.HitAt(col, r).Owner == owner {
				return col, r, true
			}
		}
	}
	return 0, 0, false
}

// TestEllipseClassPixelsCarryOwnerToHitAt is the core of the mouse half of
// Inspect: an orbit drawn through DrawEllipseClassTagged must be clickable
// back to the entity that owns it, anywhere along the line — not just at
// the vessel glyph, which is the one place identity was already available.
func TestEllipseClassPixelsCarryOwnerToHitAt(t *testing.T) {
	c := ownerTestCanvas()
	c.DrawEllipseClassTagged(circularElements(4_000_000), orbital.Vec3{}, 180, ClassReal,
		orbital.Vec3{}, 0, CellTag{Color: lipgloss.Color("#A8B8C8"), Owner: "v:7"})

	col, row, ok := findOwnedCell(c, "v:7")
	if !ok {
		t.Fatal("no canvas cell resolved to the drawn ellipse's owner — orbit-line pixels aren't tagged")
	}
	hit := c.HitAt(col, row)
	if hit.Owner != "v:7" {
		t.Errorf("HitAt(%d,%d).Owner = %q, want %q", col, row, hit.Owner, "v:7")
	}
	// The new field must not have quietly become a second body/vessel tag:
	// those three drive pre-existing click ACTIONS that Inspect doesn't change.
	if hit.BodyID != "" || hit.IsVessel || hit.NodeIdx != 0 {
		t.Errorf("owner-tagged line pixel also claims an action tag: %+v", hit)
	}
}

// TestTwoOverlappingOrbitsResolveToTheirOwnOwners is the "whose line is
// whose" question stated as a test: two lines drawn in the SAME frame each
// keep their own identity, so the answer doesn't depend on draw order or on
// the colours (which under ADR 0041 are semantic, never identity).
func TestTwoOverlappingOrbitsResolveToTheirOwnOwners(t *testing.T) {
	c := ownerTestCanvas()
	dim := lipgloss.Color("#5F5F5F")
	c.DrawEllipseClassTagged(circularElements(3_000_000), orbital.Vec3{}, 180, ClassReal,
		orbital.Vec3{}, 0, CellTag{Color: dim, Owner: "g:alice/1"})
	c.DrawEllipseClassTagged(circularElements(6_000_000), orbital.Vec3{}, 180, ClassReal,
		orbital.Vec3{}, 0, CellTag{Color: dim, Owner: "g:bob/2"})

	for _, owner := range []string{"g:alice/1", "g:bob/2"} {
		if _, _, ok := findOwnedCell(c, owner); !ok {
			t.Errorf("no cell resolves to %q — one of two same-coloured lines lost its identity", owner)
		}
	}
}

// TestPolylineClassPixelsCarryOwner covers the non-ellipse trajectories
// (node legs, encounter arcs) through the polyline sibling of the drawer
// above — the dashed Planned class, whose pixel walk is a separate code
// path from the ellipse's chord walk.
func TestPolylineClassPixelsCarryOwner(t *testing.T) {
	c := ownerTestCanvas()
	pts := []orbital.Vec3{
		{X: -3_000_000, Y: -1_000_000},
		{X: 0, Y: 500_000},
		{X: 3_000_000, Y: 1_000_000},
	}
	c.PlotPolylineClassTagged(pts, CellTag{Color: lipgloss.Color("#5FD7FF"), Owner: "n:1"}, ClassPlanned)

	if _, _, ok := findOwnedCell(c, "n:1"); !ok {
		t.Fatal("no canvas cell resolved to the drawn polyline's owner")
	}
}

// TestUntaggedClassDrawsStayUntagged pins that the pre-Inspect entry points
// are unchanged: DrawEllipseClass / PlotPolylineClass still record colour
// only, so nothing that didn't opt into an owner suddenly answers HitAt.
func TestUntaggedClassDrawsStayUntagged(t *testing.T) {
	c := ownerTestCanvas()
	c.DrawEllipseClass(circularElements(4_000_000), orbital.Vec3{}, 180, ClassScenery,
		orbital.Vec3{}, 0, lipgloss.Color("#6E6E6E"))

	c.pixelTags.each(func(x, y int, tag CellTag) {
		if tag.Owner != "" {
			t.Fatalf("untagged DrawEllipseClass wrote an owner %q at (%d, %d)", tag.Owner, x, y)
		}
	})
}
