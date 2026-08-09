package widgets

import "testing"

// TestPixelTagGridSetGetRoundTrips confirms the basic map-replacement
// contract: a set pixel reads back with ok=true and the exact tag; an
// unset pixel reads back CellTag{} with ok=false — the same "absent key
// returns the zero value, comma-ok reports absence" contract the old
// map[[2]int]CellTag gave every caller for free.
func TestPixelTagGridSetGetRoundTrips(t *testing.T) {
	var g pixelTagGrid
	g.ensureSize(10, 10)

	if tag, ok := g.get(3, 4); ok || tag != (CellTag{}) {
		t.Fatalf("unset pixel: got (%v, %v), want (CellTag{}, false)", tag, ok)
	}

	want := CellTag{Color: "#FF00FF", Owner: "v:1"}
	g.set(3, 4, want)
	if tag, ok := g.get(3, 4); !ok || tag != want {
		t.Errorf("set pixel: got (%v, %v), want (%v, true)", tag, ok, want)
	}
}

// TestPixelTagGridSetOutOfBoundsIsNoop mirrors the old map's implicit
// safety: every existing call site already bounds-checks before writing,
// but set() bounds-checks defensively too rather than panicking on an
// index a future caller might not have guarded.
func TestPixelTagGridSetOutOfBoundsIsNoop(t *testing.T) {
	var g pixelTagGrid
	g.ensureSize(10, 10)

	g.set(-1, 0, CellTag{Color: "#FFFFFF"})
	g.set(0, -1, CellTag{Color: "#FFFFFF"})
	g.set(10, 0, CellTag{Color: "#FFFFFF"})
	g.set(0, 10, CellTag{Color: "#FFFFFF"})

	if got := g.count(); got != 0 {
		t.Errorf("out-of-bounds set()s changed count to %d, want 0", got)
	}
	if tag, ok := g.get(-1, 0); ok || tag != (CellTag{}) {
		t.Errorf("out-of-bounds get(-1, 0) = (%v, %v), want (CellTag{}, false)", tag, ok)
	}
}

// TestPixelTagGridOverwriteDoesNotDoubleCountTouched confirms the
// dedup-on-overwrite property the map iteration relied on implicitly: two
// writes to the SAME pixel in one frame (two overlapping draws — genuinely
// happens, e.g. two orbit lines crossing) must appear exactly once in
// count()/each(), with the SECOND tag winning — exactly like a map's
// `m[k] = v` overwrite semantics.
//
// Non-vacuousness: an implementation that appended to touched on EVERY
// set (not just the first, untagged->tagged transition) would make count()
// report 2 here instead of 1 — this test was run against exactly that
// broken variant (touched = append(touched, idx) unconditionally) and
// failed as expected before the dedup guard was added, confirming the
// assertion actually exercises the guard.
func TestPixelTagGridOverwriteDoesNotDoubleCountTouched(t *testing.T) {
	var g pixelTagGrid
	g.ensureSize(10, 10)

	g.set(2, 2, CellTag{Color: "#111111"})
	g.set(2, 2, CellTag{Color: "#222222"}) // overwrite, same pixel

	if got := g.count(); got != 1 {
		t.Fatalf("count() = %d after two writes to the same pixel, want 1", got)
	}
	if tag, ok := g.get(2, 2); !ok || tag.Color != "#222222" {
		t.Errorf("get(2,2) = (%v, %v), want the SECOND write's color #222222", tag, ok)
	}
	n := 0
	g.each(func(px, py int, tag CellTag) { n++ })
	if n != 1 {
		t.Errorf("each() visited the overwritten pixel %d times, want 1", n)
	}
}

// TestPixelTagGridZeroThenRealDoesNotDoubleCountTouched is the #369
// review's F1 regression test: a ZERO-value write (CellTag{}, e.g.
// FillProjectedSphere fed a body whose SurfaceColorHex() is "" —
// alpha-centauri.json, trappist-1.json, and kepler-452.json all have
// bodies like this) followed by a REAL write to the SAME pixel must
// still land the pixel in touched/each() exactly once — the existing
// overwrite test above only covers real-then-real. A zero write is a
// true no-op (see set()'s doc comment for why), so this also covers the
// reverse order (real-then-zero) and a zero-only write, which must never
// appear as touched at all.
//
// Non-vacuousness (per the review's request): run with the zero-tag
// no-op guard at the top of set() removed (so a zero tag flows through
// internTag/tagIdx like any other value). "zero-then-real" still passed
// under that sabotage — this implementation's touched-dedup flag is
// tagIdx[idx]==0 (a presence bit, independent of the tag's CONTENT), not
// "does the stored value read as zero" the way the review's original
// finding described, so that specific double-touch shape doesn't
// reproduce here. But "real-then-zero" and "zero-only" both failed
// exactly as expected: without the guard, a later zero write clobbers a
// real tag instead of no-opping (get(5,5) returned the zero tag, not the
// real one), and a zero-only write registers as touched (count()==1
// instead of 0) — both confirmed, then reverted. All three sub-tests
// together pin the "zero is a true no-op regardless of order" contract
// set()'s doc comment describes.
func TestPixelTagGridZeroThenRealDoesNotDoubleCountTouched(t *testing.T) {
	t.Run("zero-then-real", func(t *testing.T) {
		var g pixelTagGrid
		g.ensureSize(10, 10)

		g.set(4, 4, CellTag{})                 // e.g. an empty SurfaceColorHex()
		g.set(4, 4, CellTag{Color: "#ABCDEF"}) // a real overpaint (orbit path, marker, ...)

		if got := g.count(); got != 1 {
			t.Fatalf("count() = %d after a zero write then a real write to the same pixel, want 1", got)
		}
		if tag, ok := g.get(4, 4); !ok || tag.Color != "#ABCDEF" {
			t.Errorf("get(4,4) = (%v, %v), want the real write's color #ABCDEF", tag, ok)
		}
		n := 0
		g.each(func(px, py int, tag CellTag) { n++ })
		if n != 1 {
			t.Errorf("each() visited the pixel %d times, want 1 (zero-then-real should not double-count)", n)
		}
	})

	t.Run("real-then-zero", func(t *testing.T) {
		var g pixelTagGrid
		g.ensureSize(10, 10)

		g.set(5, 5, CellTag{Color: "#ABCDEF"})
		g.set(5, 5, CellTag{}) // a later zero write must not erase or double-touch

		if got := g.count(); got != 1 {
			t.Fatalf("count() = %d after a real write then a zero write to the same pixel, want 1", got)
		}
		if tag, ok := g.get(5, 5); !ok || tag.Color != "#ABCDEF" {
			t.Errorf("get(5,5) = (%v, %v), want the real tag to survive a later zero write", tag, ok)
		}
	})

	t.Run("zero-only", func(t *testing.T) {
		var g pixelTagGrid
		g.ensureSize(10, 10)

		g.set(6, 6, CellTag{})

		if got := g.count(); got != 0 {
			t.Errorf("count() = %d after a zero-only write, want 0 (never touched)", got)
		}
		if tag, ok := g.get(6, 6); ok || tag != (CellTag{}) {
			t.Errorf("get(6,6) after a zero-only write = (%v, %v), want (CellTag{}, false)", tag, ok)
		}
	})
}

// TestPixelTagGridClearResetsTouchedOnly confirms clear() drops every
// touched pixel back to CellTag{} and empties the touched list, without
// needing (or performing) a full w×h sweep — get() on a cleared pixel
// returns the zero value exactly like a fresh grid.
func TestPixelTagGridClearResetsTouchedOnly(t *testing.T) {
	var g pixelTagGrid
	g.ensureSize(10, 10)

	g.set(1, 1, CellTag{Color: "#111111"})
	g.set(5, 5, CellTag{Color: "#222222"})
	g.clear()

	if got := g.count(); got != 0 {
		t.Errorf("count() = %d after clear(), want 0", got)
	}
	if tag, ok := g.get(1, 1); ok || tag != (CellTag{}) {
		t.Errorf("get(1,1) after clear() = (%v, %v), want (CellTag{}, false)", tag, ok)
	}
	if tag, ok := g.get(5, 5); ok || tag != (CellTag{}) {
		t.Errorf("get(5,5) after clear() = (%v, %v), want (CellTag{}, false)", tag, ok)
	}

	// A pixel can be re-set after clear() and shows up in count()/each()
	// again — clear() didn't leave the grid in a state where touched
	// tracking is permanently broken.
	g.set(1, 1, CellTag{Color: "#333333"})
	if got := g.count(); got != 1 {
		t.Errorf("count() = %d after re-setting a cleared pixel, want 1", got)
	}
}

// TestPixelTagGridEnsureSizeDropsStaleContentsOnResize confirms a size
// change reallocates the grid (and so drops whatever was tagged under the
// old dimensions) — Canvas.Resize's doc comment explains why this is safe
// (every screen's Render calls Clear() before drawing again after a
// resize).
func TestPixelTagGridEnsureSizeDropsStaleContentsOnResize(t *testing.T) {
	var g pixelTagGrid
	g.ensureSize(10, 10)
	g.set(1, 1, CellTag{Color: "#111111"})
	if got := g.count(); got != 1 {
		t.Fatalf("precondition: count() = %d, want 1", got)
	}

	g.ensureSize(20, 20)
	if got := g.count(); got != 0 {
		t.Errorf("count() = %d immediately after a resize, want 0 (stale contents should be dropped)", got)
	}

	// Same-size ensureSize is a no-op — must NOT drop live contents (a
	// per-frame Resize call with unchanged dimensions is common: the
	// terminal simply didn't change size between ticks).
	g.set(3, 3, CellTag{Color: "#222222"})
	g.ensureSize(20, 20)
	if got := g.count(); got != 1 {
		t.Errorf("count() = %d after a same-size ensureSize(), want 1 (should be a no-op)", got)
	}
}

// TestPixelTagGridEachVisitsExactlySetPixels confirms each() visits
// precisely the tagged set — no more, no less — regardless of grid size,
// mirroring `for coord, tag := range oldMap`.
func TestPixelTagGridEachVisitsExactlySetPixels(t *testing.T) {
	var g pixelTagGrid
	g.ensureSize(50, 50)

	want := map[[2]int]CellTag{
		{0, 0}:   {Color: "#111111"},
		{49, 49}: {Color: "#222222"},
		{10, 20}: {Color: "#333333"},
	}
	for coord, tag := range want {
		g.set(coord[0], coord[1], tag)
	}

	got := make(map[[2]int]CellTag)
	g.each(func(px, py int, tag CellTag) {
		got[[2]int{px, py}] = tag
	})

	if len(got) != len(want) {
		t.Fatalf("each() visited %d pixels, want %d", len(got), len(want))
	}
	for coord, tag := range want {
		if got[coord] != tag {
			t.Errorf("each() reported %v at %v, want %v", got[coord], coord, tag)
		}
	}
}
