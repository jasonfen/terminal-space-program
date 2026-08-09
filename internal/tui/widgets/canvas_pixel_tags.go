package widgets

// #369: pixelTags used to be a map[[2]int]CellTag. The PR #368 pre-merge
// review measured a cache HIT on the #367 curve-geometry cache still
// costing ~19.4µs for a 475-point curve (~41 ns/point) with zero geometry
// left to compute — entirely blitCurvePoints's per-pixel
// `pixelTags[[2]int{px, py}] = tag` writes, i.e. hashing a [2]int key and
// probing/inserting a bucket, on every one of a curve's points, on every
// frame, even for a curve that hasn't moved.
//
// pixelTagGrid replaces the map with a dense per-pixel INDEX array plus a
// small per-frame table of the distinct CellTag values actually in use —
// not a dense array of full CellTag values. The #369 review's first pass
// stored CellTag directly (72 bytes each): at a 200×50 terminal canvas
// (400×200 = 80,000 pixels) that's 5.76 MB PER CANVAS, ×3 canvases/seat,
// ×N ssh seats — 69.2 MB measured live for 4 seats, and unbounded because
// Canvas.Resize takes the client's raw WindowSizeMsg (a 500×150 client
// window is ~130 MB/seat on its own). tagIdx is an int32 per pixel (4
// bytes) indexing into `tags`, a table of only the FEW distinct tags a
// frame actually uses (body colors, orbit-class colors, a handful of
// vessel/ghost colors — not one entry per pixel): ~320 KB for the same
// 200×50 canvas, and pointer-free (no GC scan cost the CellTag-per-pixel
// version paid — CellTag holds three strings).
//
// tagIdx is 1-BASED so a freshly make()'d (zero-filled) array already
// means "every pixel untagged" — 0 → untagged, n → tags[n-1] — without an
// explicit fill pass on every ensureSize.
//
// Write cost: the common case (every pixel of one draw call shares the
// SAME CellTag — a curve, a disk fill, a ring outline all call set() in a
// tight loop closing over one fixed tag) is a single struct-equality
// check against lastTag, no table lookup at all.
//
// A first cut of this file interned a new tag with a LINEAR scan of
// g.tags on the theory that the table stays "small, dozens of entries at
// most." That theory was wrong for a real frame: CellTag carries an
// Inspect Owner (ADR 0041 §3) that's near-unique PER BODY/VESSEL/GHOST,
// so a scene with a few dozen drawn entities produces a few dozen
// DISTINCT tags over the course of one frame even though each individual
// draw call only ever uses one of them — and every one of those
// tag-changed transitions paid an O(current table size) scan, so total
// scan cost grew roughly with (distinct tags)² per frame, not O(distinct
// tags) as intended. Measured on BenchmarkOrbitViewRenderIdle (default
// LEO scene, no ring cost in play at all): the linear-scan version was
// ~6% SLOWER whole-frame than pre-#369 main, i.e. this file's per-pixel
// write win was more than eaten by the per-draw-call intern cost — see
// the PR body's memory/perf sections for the measured numbers. tagLookup
// fixes it: an O(1)-average map lookup keyed on the tag itself, checked
// only on the same tag-changed transitions the linear scan was already
// restricted to (never per pixel, thanks to lastTag below) — so the
// per-pixel cost this file exists to cut stays cut, and the per-frame
// intern cost stops growing with scene complexity.
//
// CellTag{} (its zero value) means "untagged" — see set()'s doc comment
// for why it's an explicit no-op rather than relying on "no writer stores
// a zero tag" as an invariant (the #369 review's F1 finding: that
// invariant was false — FillProjectedSphere plus a body with an empty
// SurfaceColorHex breaks it in practice).
type pixelTagGrid struct {
	w, h      int
	tagIdx    []int32           // 1-based index into tags; 0 = untagged
	tags      []CellTag         // this frame's distinct tag values, deduped
	tagLookup map[CellTag]int32 // tag -> its 1-based index in tags
	touched   []int32           // pixel indices (py*w+px) currently tagged

	// lastTag / lastIdx memoize the most recent set() call's (tag, index)
	// pair so a run of same-tag writes — the overwhelmingly common case,
	// see the type doc comment — never touches tagLookup at all.
	hasLast bool
	lastTag CellTag
	lastIdx int32
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
	if g.w == w && g.h == h && g.tagIdx != nil {
		return
	}
	g.w, g.h = w, h
	g.tagIdx = make([]int32, w*h) // zero-valued == every pixel untagged
	g.touched = g.touched[:0]
	g.tags = g.tags[:0]
	clear(g.tagLookup)
	g.hasLast = false
}

// internTag returns tag's 1-based index into g.tags, appending it (and
// recording it in tagLookup) if this is the first time this frame the
// exact value has been seen. set() only calls this on a tag-CHANGED
// transition (see lastTag), never per pixel — see the type doc comment
// for why an O(1)-average map lookup here, not a per-pixel one, is what
// actually fixes the #368 review's map-write finding without
// reintroducing an O(distinct tags)² per-frame cost.
func (g *pixelTagGrid) internTag(tag CellTag) int32 {
	if idx, ok := g.tagLookup[tag]; ok {
		return idx
	}
	g.tags = append(g.tags, tag)
	idx := int32(len(g.tags))
	if g.tagLookup == nil {
		g.tagLookup = make(map[CellTag]int32)
	}
	g.tagLookup[tag] = idx
	return idx
}

// set stores tag at (px, py). A zero-value tag (CellTag{}) is an explicit
// no-op — NOT a write of "untagged" — matching the old map's observable
// behavior exactly: a map write of CellTag{} still inserted an entry, but
// pickDominantColor's aggregation skips any tag.Color == "" entry, so it
// never contributed to a cell's color count either way. Treating it as a
// true no-op here is behaviorally equivalent and closes a real bug (#369
// review F1): FillProjectedSphere calls set() unconditionally with
// CellTag{Color: color}, and every body in alpha-centauri.json,
// trappist-1.json, and kepler-452.json has SurfaceColorHex() == "" —
// CellTag{Color: ""} equals the zero value. Under the PRIOR
// touched-dedup logic (`if g.tags[idx] == (CellTag{}) { touched =
// append(...) }`), a zero write marked the pixel touched WITHOUT
// recording a real value; a later real write on the same pixel then
// found tags[idx] still reading as zero and appended to touched a SECOND
// time, so each()/String() visited that one pixel twice with the same
// (real) tag — double-counting it in pickDominantColor's per-cell vote
// and potentially flipping which color wins a cell where an orbit path,
// descent arc, or marker overpaints a horizon fill. No-opping the zero
// write here means only real writes ever touch state, so a pixel is
// marked touched at most once regardless of write order or how many
// zero writes land on it first.
func (g *pixelTagGrid) set(px, py int, tag CellTag) {
	if tag == (CellTag{}) {
		return
	}
	if px < 0 || px >= g.w || py < 0 || py >= g.h {
		return
	}
	idx := py*g.w + px
	oneBased := g.lastIdx
	if !g.hasLast || tag != g.lastTag {
		oneBased = g.internTag(tag)
		g.hasLast, g.lastTag, g.lastIdx = true, tag, oneBased
	}
	if g.tagIdx[idx] == 0 {
		g.touched = append(g.touched, int32(idx))
	}
	g.tagIdx[idx] = oneBased
}

// get returns the tag at (px, py) and whether it's actually tagged (the
// map's comma-ok pattern) — ok is false for an out-of-bounds pixel or one
// that was never set (with a non-zero tag) this frame.
func (g *pixelTagGrid) get(px, py int) (CellTag, bool) {
	if px < 0 || px >= g.w || py < 0 || py >= g.h {
		return CellTag{}, false
	}
	oneBased := g.tagIdx[py*g.w+px]
	if oneBased == 0 {
		return CellTag{}, false
	}
	return g.tags[oneBased-1], true
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
		oneBased := g.tagIdx[i]
		if oneBased == 0 { // defensive; touched and tagIdx should stay in sync
			continue
		}
		fn(i%g.w, i/g.w, g.tags[oneBased-1])
	}
}

// clear resets every touched pixel back to untagged, empties the touched
// list, and empties the per-frame tag table (and its lookup map) — all
// reusing their backing storage. O(touched + distinct tags), not O(w×h):
// a canvas typically inks a small fraction of its pixel grid, so this is
// cheaper than a full-grid clear would be, and — unlike the old
// `pixelTags = nil` — never forces a fresh allocation (and the GC churn
// that comes with one) on the next frame's first write. clear(map) (Go
// 1.21+ builtin) empties tagLookup's entries without releasing its
// bucket allocation, the map equivalent of touched/tags reslicing to :0.
func (g *pixelTagGrid) clear() {
	for _, idx := range g.touched {
		g.tagIdx[idx] = 0
	}
	g.touched = g.touched[:0]
	g.tags = g.tags[:0]
	clear(g.tagLookup)
	g.hasLast = false
}
