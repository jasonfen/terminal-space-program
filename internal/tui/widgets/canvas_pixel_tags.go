package widgets

// #369: pixelTags used to be a map[[2]int]CellTag. The PR #368 pre-merge
// review measured a cache HIT on the #367 curve-geometry cache still
// costing ~19.4µs for a 475-point curve (~41 ns/point) with zero geometry
// left to compute — entirely blitCurvePoints's per-pixel
// `pixelTags[[2]int{px, py}] = tag` writes, i.e. hashing a [2]int key and
// probing/inserting a bucket, on every one of a curve's points, on every
// frame, even for a curve that hasn't moved.
//
// pixelTagGrid replaces the map with a dense array indexed by
// `py*w + px`: a write is an unconditional slice store, no hashing, no
// bucket allocation. CellTag{} (its zero value) means "untagged", the
// same contract a map's "absent key returns the zero value" already gave
// every caller — safe because every *Tagged draw site already gates on
// `tagged := tag != (CellTag{})` before touching pixelTags at all, so no
// writer ever legitimately stores a zero-value tag.
//
// A dense array can't be ranged like a map without visiting all pxW*pxH
// cells regardless of how few are actually tagged (a system-view canvas
// at 200×50 terminal cells is a 400×200 == 80,000-pixel grid; a typical
// frame tags a small fraction of that). touched keeps the "range only
// what's set" property the map gave String()/CountColor for free, and
// lets clear() reset only what was actually written instead of either a
// full-grid sweep or (the map's old approach) dropping the whole
// allocation and paying for a fresh one next frame.
type pixelTagGrid struct {
	w, h    int
	tags    []CellTag
	touched []int32
}

// ensureSize (re)allocates the backing grid when the canvas's pixel
// dimensions change (Resize / a fresh NewCanvas). A stale-sized grid from
// before a resize would index the wrong cell, or panic out of bounds —
// Canvas.Resize is always followed by Clear before anything draws again
// (every screen's Render starts there), so dropping stale contents on a
// size change is unobservable, not a behavior change from the map's
// implicit "resize without clear leaves old entries orphaned outside the
// new bounds" prior behavior.
func (g *pixelTagGrid) ensureSize(w, h int) {
	if g.w == w && g.h == h && g.tags != nil {
		return
	}
	g.w, g.h = w, h
	g.tags = make([]CellTag, w*h)
	g.touched = g.touched[:0]
}

// set stores tag at (px, py). Appends to touched only the FIRST time this
// frame a given index goes from untagged to tagged — two overlapping
// draws touching the same pixel in one frame (two orbit lines crossing,
// say) overwrite tags[idx] in place without a second touched entry,
// mirroring how ranging a map counts a repeatedly-assigned key once.
// Bounds-checked defensively (every current call site already checks
// before calling this), matching Canvas's existing silent-drop-off-canvas
// convention rather than panicking.
func (g *pixelTagGrid) set(px, py int, tag CellTag) {
	if px < 0 || px >= g.w || py < 0 || py >= g.h {
		return
	}
	idx := py*g.w + px
	if g.tags[idx] == (CellTag{}) {
		g.touched = append(g.touched, int32(idx))
	}
	g.tags[idx] = tag
}

// get returns the tag at (px, py) and whether it's actually tagged (the
// map's comma-ok pattern) — ok is false for an out-of-bounds pixel or one
// that was never set this frame.
func (g *pixelTagGrid) get(px, py int) (CellTag, bool) {
	if px < 0 || px >= g.w || py < 0 || py >= g.h {
		return CellTag{}, false
	}
	tag := g.tags[py*g.w+px]
	return tag, tag != (CellTag{})
}

// count mirrors len(map) for the emptiness checks String()/HitAt open with.
func (g *pixelTagGrid) count() int { return len(g.touched) }

// each visits every currently-tagged pixel — the touched list, not a full
// w×h sweep. Mirrors `for coord, tag := range pixelTags` over the old map;
// map iteration order was never something callers could rely on anyway
// (String()'s per-cell color aggregation already tie-breaks
// deterministically for exactly that reason), so touched's write-order
// walk is a strictly more predictable replacement, not a looser one.
func (g *pixelTagGrid) each(fn func(px, py int, tag CellTag)) {
	for _, idx := range g.touched {
		i := int(idx)
		fn(i%g.w, i/g.w, g.tags[i])
	}
}

// clear resets every touched pixel back to CellTag{} and empties the
// touched list, reusing its backing array. O(touched), not O(w*h): a
// canvas typically inks a small fraction of its pixel grid, so this is
// cheaper than a full-array clear() would be, and — unlike the old
// `pixelTags = nil` — never forces a fresh allocation (and the GC churn
// that comes with one) on the next frame's first write.
func (g *pixelTagGrid) clear() {
	for _, idx := range g.touched {
		g.tags[idx] = CellTag{}
	}
	g.touched = g.touched[:0]
}
