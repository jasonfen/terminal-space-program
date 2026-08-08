package widgets

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestFillTexturedDiskTaggedMatchesColoredShape confirms FillTexturedDiskTagged
// and FillColoredDiskTagged paint exactly the same set of on-canvas pixels for
// the same center/radius — the two disk-fill paths share the same nested
// bounds/distance-test loop (the #363 offset-mask cache that used to sit in
// front of it was removed in #367; see FillColoredDiskTagged's comment).
func TestFillTexturedDiskTaggedMatchesColoredShape(t *testing.T) {
	texturedCanvas := NewCanvas(200, 60)
	texturedCanvas.SetScale(1)
	coloredCanvas := NewCanvas(200, 60)
	coloredCanvas.SetScale(1)

	center := orbital.Vec3{}
	const r = 20
	texturedCanvas.FillTexturedDiskTagged(center, r, func(dx, dy int) lipgloss.Color {
		return "#123456"
	}, CellTag{})
	coloredCanvas.FillColoredDiskTagged(center, r, CellTag{Color: "#123456"})

	if texturedCanvas.pixelTags.count() != coloredCanvas.pixelTags.count() {
		t.Fatalf("textured disk painted %d pixels, colored disk painted %d — offset mask should agree",
			texturedCanvas.pixelTags.count(), coloredCanvas.pixelTags.count())
	}
	coloredCanvas.pixelTags.each(func(px, py int, _ CellTag) {
		if _, ok := texturedCanvas.pixelTags.get(px, py); !ok {
			t.Errorf("pixel (%d, %d) painted by colored disk but not textured disk", px, py)
		}
	})
}

// TestDiskIntersectsCanvas checks the bounding-box viewport test used
// to skip disk-fill work (and any per-body raster-cache allocation,
// #363 review fix) for a body that's entirely off-canvas: a disk
// dead-center is on-canvas, a disk far enough away that even its
// radius can't reach the canvas is not, and a disk whose center is
// off-canvas but whose edge still overlaps the canvas is correctly
// reported as intersecting (the bounding-box test must not be a
// center-point-only test).
func TestDiskIntersectsCanvas(t *testing.T) {
	c := NewCanvas(40, 20) // pxW=80, pxH=80
	c.SetScale(1)
	c.Center(orbital.Vec3{})

	if !c.DiskIntersectsCanvas(orbital.Vec3{}, 10) {
		t.Error("a disk centered on-canvas should intersect")
	}
	if c.DiskIntersectsCanvas(orbital.Vec3{X: 10000}, 10) {
		t.Error("a disk far off-canvas with a small radius should not intersect")
	}
	// Center just past the right edge (canvas half-width 40px), but
	// with a radius large enough that the disk's LEFT edge still
	// overlaps the canvas.
	if !c.DiskIntersectsCanvas(orbital.Vec3{X: 45}, 20) {
		t.Error("a disk whose center is off-canvas but whose edge overlaps should intersect")
	}
}
