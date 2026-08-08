package widgets

import (
	"testing"
)

// HitAt has to answer the same way every time it is asked about the same
// pixels. It resolves a mouse click into an ACTION — focus a vessel, open
// the node form, move the body Cursor, inspect a line, or stage a burn —
// so a cell that answers "your orbit" on one press and "their orbit" on
// the next is not an ambiguity the player can reason about, it is a
// broken button.
//
// The aggregation used to be a bare `for k, n := range counts` keeping the
// first key that beat the running max, which meant Go's per-range
// randomized map iteration decided every exact tie. It stayed invisible
// while the map only ever tagged one owner per cell; ADR 0041 §3 put a
// second vessel's orbit line on the canvas and made ties reachable, at
// which point the Inspect click test started failing a tenth of the time
// on both arm64 and amd64.
//
// setCellPixels writes tags straight into the per-pixel map so a test can
// construct an exact pixel distribution without solving for world coords
// that project onto the cell it wants.

// setCellPixels tags `n` pixels of the terminal cell at (col, row),
// starting at cell-local pixel index `from` in (dy-major) order, with the
// supplied tag. Cell-local index i maps to (dx, dy) = (i/4, i%4).
func setCellPixels(c *Canvas, col, row, from, n int, tag CellTag) {
	for i := from; i < from+n && i < 8; i++ {
		dx, dy := i/4, i%4
		px, py := col*2+dx, row*4+dy
		c.dc.Set(px, py)
		c.pixelTags.set(px, py, tag)
	}
}

// TestHitAtOwnerMajorityWins pins the ADR 0041 §3 rule itself: with an
// unequal split the owner holding more of the cell's ink wins, which is
// also the one the player can see more of.
func TestHitAtOwnerMajorityWins(t *testing.T) {
	c := NewCanvas(40, 20)
	setCellPixels(c, 10, 5, 0, 3, CellTag{Owner: "v:2"})
	setCellPixels(c, 10, 5, 3, 1, CellTag{Owner: "v:1"})

	hit := c.HitAt(10, 5)
	if hit.Owner != "v:2" {
		t.Errorf("3-vs-1 split resolved to %q, want %q", hit.Owner, "v:2")
	}
	if hit.OwnerTied {
		t.Error("a 3-vs-1 split reported OwnerTied — it has a clear majority")
	}
}

// TestHitAtOwnerTieIsDeterministic is the regression proper: an exact
// pixel-count tie must resolve to the SAME owner on every call, in every
// process, on every platform.
func TestHitAtOwnerTieIsDeterministic(t *testing.T) {
	c := NewCanvas(40, 20)
	// Cell-local pixels 0..3 are column dx=0, 4..7 are dx=1 — an even
	// four-and-four straddle, the shape two orbit lines crossing a cell
	// actually make.
	setCellPixels(c, 10, 5, 0, 4, CellTag{Owner: "v:1"})
	setCellPixels(c, 10, 5, 4, 4, CellTag{Owner: "v:2"})

	first := c.HitAt(10, 5)
	if first.Owner == "" {
		t.Fatal("a fully-inked cell resolved to no owner")
	}
	if !first.OwnerTied {
		t.Error("a 4-vs-4 split did not report OwnerTied")
	}
	// 500 calls is far past the point where a randomized map-iteration
	// tie-break would have shown both answers.
	for i := 0; i < 500; i++ {
		if got := c.HitAt(10, 5); got.Owner != first.Owner {
			t.Fatalf("call %d resolved the same cell to %q, first call said %q — HitAt is not deterministic", i, got.Owner, first.Owner)
		}
	}
}

// TestHitAtTieBreaksOnTheNearestPixel: with the counts equal, the owner
// whose ink lies closest to the cell centre wins. That is the only
// tie-break rung grounded in what the player pointed at — the cell is all
// the sub-cell resolution a terminal mouse report carries, so its centre
// stands in for the click point.
func TestHitAtTieBreaksOnTheNearestPixel(t *testing.T) {
	// Cell-local (dx, dy) distances² from the centre (see cellCentreDist2):
	// dy 0 and 3 are the far rows (9+1=10), dy 1 and 2 the near ones (1+1=2).
	c := NewCanvas(40, 20)
	setCellPixels(c, 10, 5, 0, 1, CellTag{Owner: "v:1"}) // (0,0) — far row
	setCellPixels(c, 10, 5, 1, 1, CellTag{Owner: "v:2"}) // (0,1) — near row

	hit := c.HitAt(10, 5)
	if hit.Owner != "v:2" {
		t.Errorf("1-vs-1 tie resolved to %q, want %q (its pixel is nearer the cell centre)", hit.Owner, "v:2")
	}
	if !hit.OwnerTied {
		t.Error("a 1-vs-1 split did not report OwnerTied")
	}

	// Mirror it: the same two owners, near/far swapped, must swap the
	// winner. If it does not, something other than the geometry is
	// deciding (e.g. the key order backstop).
	c2 := NewCanvas(40, 20)
	setCellPixels(c2, 10, 5, 1, 1, CellTag{Owner: "v:1"}) // (0,1) — near row
	setCellPixels(c2, 10, 5, 0, 1, CellTag{Owner: "v:2"}) // (0,0) — far row
	if hit2 := c2.HitAt(10, 5); hit2.Owner != "v:1" {
		t.Errorf("mirrored tie resolved to %q, want %q — the tie-break is not reading the geometry", hit2.Owner, "v:1")
	}
}

// TestHitAtBodyAndNodeTiesAreDeterministic: BodyID and NodeIdx go through
// the same ladder as Owner and had the same latent bug. Two body disks
// overlapping a cell, or two node markers, must not answer differently
// per click either.
func TestHitAtBodyAndNodeTiesAreDeterministic(t *testing.T) {
	c := NewCanvas(40, 20)
	setCellPixels(c, 10, 5, 0, 4, CellTag{BodyID: "earth", NodeIdx: 1})
	setCellPixels(c, 10, 5, 4, 4, CellTag{BodyID: "luna", NodeIdx: 2})

	first := c.HitAt(10, 5)
	if first.BodyID == "" || first.NodeIdx == 0 {
		t.Fatalf("fully-inked cell resolved to %+v", first)
	}
	for i := 0; i < 500; i++ {
		got := c.HitAt(10, 5)
		if got.BodyID != first.BodyID {
			t.Fatalf("call %d resolved BodyID to %q, first call said %q", i, got.BodyID, first.BodyID)
		}
		if got.NodeIdx != first.NodeIdx {
			t.Fatalf("call %d resolved NodeIdx to %d, first call said %d", i, got.NodeIdx, first.NodeIdx)
		}
	}
}

// TestHitAtSingleOwnerIsNeverTied: the overwhelmingly common case — one
// entity's ink in the cell — must not trip the ambiguity flag, or the App
// would refuse the own-orbit-line burn placement everywhere.
func TestHitAtSingleOwnerIsNeverTied(t *testing.T) {
	for n := 1; n <= 8; n++ {
		c := NewCanvas(40, 20)
		setCellPixels(c, 10, 5, 0, n, CellTag{Owner: "v:1"})
		hit := c.HitAt(10, 5)
		if hit.Owner != "v:1" {
			t.Errorf("%d pixels of one owner resolved to %q", n, hit.Owner)
		}
		if hit.OwnerTied {
			t.Errorf("%d pixels of a SINGLE owner reported OwnerTied", n)
		}
	}
}
