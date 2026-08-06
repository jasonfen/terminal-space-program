package widgets

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// bruteForceDiskOffsets recomputes the disk mask the slow way (the
// original per-call double loop + distance test FillTexturedDiskTagged
// and FillColoredDiskTagged used before #363), for comparison against
// the cached diskOffsets().
func bruteForceDiskOffsets(pxRadius int) map[[2]int]bool {
	r2 := pxRadius * pxRadius
	out := make(map[[2]int]bool)
	for dy := -pxRadius; dy <= pxRadius; dy++ {
		for dx := -pxRadius; dx <= pxRadius; dx++ {
			if dx*dx+dy*dy <= r2 {
				out[[2]int{dx, dy}] = true
			}
		}
	}
	return out
}

// TestDiskOffsetsMatchesBruteForce confirms the #363 offset-mask cache
// produces exactly the same set of (dx, dy) pairs as the original
// per-pixel distance test, for a range of radii including the
// BodyTextureMinRadius boundary and some larger zoomed-in sizes.
func TestDiskOffsetsMatchesBruteForce(t *testing.T) {
	c := NewCanvas(200, 60)
	for _, r := range []int{1, 2, 5, 11, 12, 13, 20, 24, 40, 100} {
		want := bruteForceDiskOffsets(r)
		got := c.diskOffsets(r)
		if len(got) != len(want) {
			t.Errorf("r=%d: len(offsets)=%d, want %d", r, len(got), len(want))
		}
		seen := make(map[[2]int]bool, len(got))
		for _, o := range got {
			key := [2]int{o.dx, o.dy}
			if !want[key] {
				t.Errorf("r=%d: offset (%d,%d) not in brute-force disk mask", r, o.dx, o.dy)
			}
			seen[key] = true
		}
		for key := range want {
			if !seen[key] {
				t.Errorf("r=%d: brute-force offset %v missing from cached mask", r, key)
			}
		}
	}
}

// TestDiskOffsetsCachedAcrossCalls confirms the second call for the
// same radius returns the identical backing slice (proof the mask is
// actually memoized, not recomputed).
func TestDiskOffsetsCachedAcrossCalls(t *testing.T) {
	c := NewCanvas(200, 60)
	first := c.diskOffsets(20)
	second := c.diskOffsets(20)
	if len(first) == 0 {
		t.Fatal("expected a non-empty disk mask")
	}
	if &first[0] != &second[0] {
		t.Error("diskOffsets(20) returned a different backing array on the second call — not cached")
	}
}

// TestFillTexturedDiskTaggedMatchesColoredShape confirms the #363
// offset-mask optimization didn't change which pixels get painted:
// a textured disk and a colored disk of the same radius at the same
// center must tag exactly the same set of on-canvas pixels.
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

	if len(texturedCanvas.pixelTags) != len(coloredCanvas.pixelTags) {
		t.Fatalf("textured disk painted %d pixels, colored disk painted %d — offset mask should agree",
			len(texturedCanvas.pixelTags), len(coloredCanvas.pixelTags))
	}
	for k := range coloredCanvas.pixelTags {
		if _, ok := texturedCanvas.pixelTags[k]; !ok {
			t.Errorf("pixel %v painted by colored disk but not textured disk", k)
		}
	}
}
