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

// TestDiskOffsetCacheBoundedAcrossZoomSweep is the review-mandated
// memory-bound regression test: pxRadius changes on every zoom
// step/refit, and the offset cache used to be keyed on radius with no
// eviction — a session that zoomed across a wide radius range once
// retained a mask for every distinct radius it ever passed through
// (measured ~1.22 GB retained sweeping 1..384). After the LRU cap, the
// cache must never hold more than diskOffsetCacheCap distinct radii,
// no matter how many distinct radii a session sweeps through.
func TestDiskOffsetCacheBoundedAcrossZoomSweep(t *testing.T) {
	c := NewCanvas(200, 60)
	// Sweep every radius from 1 to 384 — far more distinct radii than
	// diskOffsetCacheCap, standing in for a full zoom-in-then-out pass.
	for r := 1; r <= 384; r++ {
		c.diskOffsets(r)
		if len(c.diskOffsetCache) > diskOffsetCacheCap {
			t.Fatalf("after radius %d: diskOffsetCache holds %d entries, want <= %d (cap)", r, len(c.diskOffsetCache), diskOffsetCacheCap)
		}
		if len(c.diskOffsetOrder) > diskOffsetCacheCap {
			t.Fatalf("after radius %d: diskOffsetOrder holds %d entries, want <= %d (cap)", r, len(c.diskOffsetOrder), diskOffsetCacheCap)
		}
	}
	if got := len(c.diskOffsetCache); got != diskOffsetCacheCap {
		t.Errorf("after a 384-radius sweep, diskOffsetCache = %d entries, want exactly the cap (%d) — the sweep should have filled and evicted, not stayed short", got, diskOffsetCacheCap)
	}
	// The most recently swept radii (nearest 384) must be the ones
	// still resident — LRU eviction should have dropped the early,
	// long-unused ones (nearest 1), not an arbitrary subset.
	if _, ok := c.diskOffsetCache[384]; !ok {
		t.Error("most-recently-used radius 384 was evicted — eviction isn't LRU")
	}
	if _, ok := c.diskOffsetCache[1]; ok {
		t.Error("least-recently-used radius 1 is still resident after the sweep — eviction isn't LRU")
	}
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
