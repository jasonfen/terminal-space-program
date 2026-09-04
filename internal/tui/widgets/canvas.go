// Package widgets provides drawille-backed canvas + lipgloss HUD helpers
// shared by all screens.
package widgets

import (
	"cmp"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/exrook/drawille-go"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// Canvas wraps a drawille.Canvas with a world-to-pixel projection. One
// terminal cell = 2×4 braille dots, so the pixel grid is (cols*2, rows*4).
//
// World coordinates are inertial meters. The projection is ortho: drop Z,
// map (x_world, y_world) → (px, py) via a single scale and the cached
// center (panning is out-of-scope for v0.1 per plan C8 commit body).
// Basis selects the world-space unit vectors that map to canvas X+
// and Y+ on render. A point's screen position is:
//
//	screen.x = (world − center) · basis.X
//	screen.y = (world − center) · basis.Y
//
// DefaultBasis (X = (1,0,0), Y = (0,1,0)) is the v0.1 equatorial
// projection — drop Z, plot (X, Y). v0.6.4+ uses orbit-perpendicular
// bases (perifocal x̂/ŷ from a craft's elements) so inclined orbits
// project without foreshortening.
type Basis struct {
	X, Y orbital.Vec3
}

// CellTag is the per-pixel record set by the *Colored* / *Tagged*
// draw helpers. Color drives String()'s per-cell colorization;
// BodyID / NodeIdx / IsVessel let HitAt resolve mouse clicks back
// to a sim object. v0.6.4+. Zero value = untagged (no color, no
// hit). NodeIdx 0 means "not a node" — planted nodes are 1-indexed
// in the tag and decoded by callers.
type CellTag struct {
	Color    lipgloss.Color
	BodyID   string
	NodeIdx  int // 0 = no node; otherwise 1 + Nodes-slice index
	IsVessel bool
	// Owner is the Inspect identity key of whatever drew this pixel
	// (ADR 0041 §3 / #346) — an opaque string minted by the caller, so
	// the canvas never has to know what a vessel, ghost, body, node or
	// closest-approach pair IS. It is the field that extends hit-testing
	// from the three glyph/disk cases above to ORBIT LINES: the class
	// drawers (lineclass.go) thread a tag through, so every lit pixel of
	// a trajectory answers "whose line is this?" on a click, which is
	// exactly the question ADR 0041 diagnosed as unanswerable.
	//
	// Deliberately independent of BodyID / NodeIdx / IsVessel rather than
	// replacing them: those three drive pre-existing click ACTIONS
	// (focus, open the node form, move the body Cursor) that Inspect does
	// not change.
	Owner string
	// OwnerTied is a HitAt OUTPUT only — never set by a drawer, and
	// ignored on any tag passed into a draw helper. It reports that two
	// or more owners inked the same number of the cell's pixels, so the
	// Owner above is the tie-break's answer rather than a majority's.
	//
	// It exists because the click paths are not symmetric: resolving an
	// ambiguous cell to ANOTHER vessel costs a name chip, while resolving
	// it to your OWN vessel's orbit line falls through to "stage a burn
	// here" and changes screen. A coin-toss that sometimes opens the
	// planner reads as a broken click, so the App gates that fallthrough
	// on the cell being unambiguous.
	OwnerTied bool
}

// DefaultBasis is the top-down projection: world X axis maps to
// canvas X+, world Y axis to canvas Y+. Z drops. Pre-v0.6.4 the
// only projection.
func DefaultBasis() Basis {
	return Basis{
		X: orbital.Vec3{X: 1},
		Y: orbital.Vec3{Y: 1},
	}
}

// TiltedWorldBasis is the v0.10.6 ViewTilted projection when no
// valid orbit is available (Landed / hyperbolic / degenerate / no
// craft) — start from DefaultBasis (world X, world Y), yaw φ around
// world Z, then tilt θ around the yawed X axis. The (θ, φ) inputs
// are in radians. θ = 0 ∧ φ = 0 returns DefaultBasis verbatim.
// Keeps the depth cue alive on the pad — a flat DefaultBasis
// fallback would defeat the headline feature on the very view
// that needs depth most (planet looming under the rocket).
func TiltedWorldBasis(thetaRad, phiRad float64) Basis {
	xHat := orbital.Vec3{X: 1}
	yHat := orbital.Vec3{Y: 1}
	zHat := orbital.Vec3{Z: 1}
	xYaw := orbital.Rotate(xHat, zHat, phiRad)
	yYaw := orbital.Rotate(yHat, zHat, phiRad)
	yTilt := orbital.Rotate(yYaw, xYaw, thetaRad)
	return Basis{X: xYaw, Y: yTilt}
}

// TiltedPerifocalBasis is the v0.10.6 ViewTilted projection when
// the active craft has a valid Keplerian orbit — start from
// PerifocalBasis(el), yaw φ around the orbit normal (x̂ × ŷ), then
// tilt θ around the yawed x̂ axis. Inputs in radians. θ = 0 ∧
// φ = 0 collapses to PerifocalBasis verbatim (same as ViewOrbitFlat).
// θ rotates the camera away from straight-down-on-the-orbit-plane
// toward looking at it from an angle; the perifocal frame still
// anchors the basis so an inclined orbit still reads as a clean
// ellipse, just tilted.
func TiltedPerifocalBasis(el orbital.Elements, thetaRad, phiRad float64) Basis {
	xHat, yHat := orbital.PerifocalBasis(el)
	n := xHat.Cross(yHat)
	xYaw := orbital.Rotate(xHat, n, phiRad)
	yYaw := orbital.Rotate(yHat, n, phiRad)
	yTilt := orbital.Rotate(yYaw, xYaw, thetaRad)
	return Basis{X: xYaw, Y: yTilt}
}

// DepthAxis returns the unit vector pointing toward the camera, i.e.
// "out of screen." Points with positive (world − center)·DepthAxis()
// are in front of the basis plane through center; negative is
// behind. Computed as basis.X × basis.Y so the cardinal-view axes
// derive consistently from the X / Y choice. v0.6.4+: orbit-screen
// uses this for back-of-body occlusion in side views.
func (b Basis) DepthAxis() orbital.Vec3 {
	return orbital.Vec3{
		X: b.X.Y*b.Y.Z - b.X.Z*b.Y.Y,
		Y: b.X.Z*b.Y.X - b.X.X*b.Y.Z,
		Z: b.X.X*b.Y.Y - b.X.Y*b.Y.X,
	}
}

type Canvas struct {
	cols, rows int          // terminal cells
	pxW, pxH   int          // pixel grid (cols*2, rows*4)
	centerW    orbital.Vec3 // world coord at pixel center
	scale      float64      // pixels per meter
	basis      Basis        // world axes mapped to canvas X+/Y+ (v0.6.4+)
	dc         drawille.Canvas

	// pixelTags maps a pixel coord (px, py) → CellTag, backed by a dense
	// grid rather than a map (#369 — see canvas_pixel_tags.go). Set by
	// the *Colored* / *Tagged* draw helpers; plain Plot / FillDisk /
	// RingOutline / DrawEllipse / PlotArrow leave pixels untagged.
	//
	// At String() time, each terminal cell picks its color from the
	// most-common Color among its 2×4 pixels (cells with no tagged
	// pixels render in the default terminal color). v0.6.4+: HitAt
	// aggregates the same cell-level pixel set to answer "what's
	// under cursor (col, row)?" for mouse hit-testing — bodies,
	// vessel, planted nodes — using BodyID / NodeIdx / IsVessel
	// fields on CellTag.
	//
	// v0.5.10+: replaces the v0.5.3 cell-rectangle approach, which
	// colored a body's entire bounding box of cells — bleeding into
	// orbit lines, craft glyphs, and apo/peri markers near a body.
	// Per-pixel tagging keeps body color confined to the body's own
	// pixels.
	pixelTags pixelTagGrid

	// cellOverlays maps a cell coord → a Unicode glyph that replaces
	// the drawille-derived char at String() time. v0.5.12+ — used by
	// the orbit renderer to overlay body-identity glyphs (☉ ◉ ● ○)
	// on top of the underlying braille so different body types read
	// distinctly even at small pixel-radius. Overlay color comes from
	// the cell's pixelTags as usual.
	cellOverlays map[[2]int]rune

	// cellOverlayColors optionally pins an overlay cell to a specific
	// foreground color, independent of any pixelTags under it. Used by
	// SetCellLabelColored so HUD-style corner labels (e.g. the "view:"
	// readout) can render in the theme palette even though they sit on
	// untagged background cells. Cells absent from this map fall back to
	// the pixelTag-derived color (terminal default when untagged).
	cellOverlayColors map[[2]int]lipgloss.TerminalColor

	// curveCache memoizes a drawn orbit LINE's flattened+projected pixel
	// list (#367), the same predict-on-change discipline diskRasterCache
	// applies to a body's textured-disk pixels — see
	// canvas_curve_cache.go for the key derivation and why it must cover
	// the canvas's own view state, not just the curve's orbital elements.
	// curveCacheOrder is its LRU recency list (oldest first).
	// curveCacheHits / …Computes are test hooks.
	curveCache         map[string]*curveCacheEntry
	curveCacheOrder    []string
	curveCacheHits     int
	curveCacheComputes int

	// ringCache is curveCache's #369 sibling for RingTiltedOutlineTagged
	// (tilted ring-band outlines — Saturn's C/B/A/F bands): same
	// recorded-pixel replay discipline, same reasons, different draw
	// primitive. See canvas_ring_cache.go.
	ringCache         map[string]*ringCacheEntry
	ringCacheOrder    []string
	ringCacheHits     int
	ringCacheComputes int
}

// DiskIntersectsCanvas reports whether a disk of the given pixel
// radius centered at center could paint any on-canvas pixel — a cheap
// screen-space bounding-box test callers use to skip disk-fill work
// (and any per-body raster-cache allocation) entirely for a body
// that's off-canvas this frame, rather than paying for a fill loop
// that would clip away to nothing anyway (#363 review fix).
func (c *Canvas) DiskIntersectsCanvas(center orbital.Vec3, pxRadius int) bool {
	if pxRadius < 1 {
		pxRadius = 1
	}
	cx, cy, _ := c.Project(center)
	return cx+pxRadius >= 0 && cx-pxRadius < c.pxW && cy+pxRadius >= 0 && cy-pxRadius < c.pxH
}

// NewCanvas builds a canvas sized to fit cols × rows terminal cells.
// Scale of 1 pixel per meter is the default; callers call FitTo() to
// rescale around a bounding radius.
func NewCanvas(cols, rows int) *Canvas {
	if cols < 4 {
		cols = 4
	}
	if rows < 4 {
		rows = 4
	}
	c := &Canvas{
		cols:  cols,
		rows:  rows,
		pxW:   cols * 2,
		pxH:   rows * 4,
		scale: 1,
		basis: DefaultBasis(),
		dc:    drawille.NewCanvas(),
	}
	c.pixelTags.ensureSize(c.pxW, c.pxH)
	return c
}

// SetBasis swaps the projection basis. Called per-frame by render
// code that wants a non-equatorial view; pass DefaultBasis() to
// restore. v0.6.4+.
func (c *Canvas) SetBasis(b Basis) { c.basis = b }

// Basis returns the canvas's current projection basis. v0.8.5.7+ —
// callers (the orbit screen's view-aware texture pipeline) need
// the basis to compute camera direction for ViewOrbitFlat, where
// the depth axis is the active craft's orbit-plane normal rather
// than a cardinal world axis.
func (c *Canvas) Basis() Basis { return c.basis }

// Resize updates the terminal-cell dimensions. Does not clear the canvas.
func (c *Canvas) Resize(cols, rows int) {
	if cols < 4 {
		cols = 4
	}
	if rows < 4 {
		rows = 4
	}
	c.cols, c.rows = cols, rows
	c.pxW, c.pxH = cols*2, rows*4
	// #369: the dense pixelTags grid is sized to pxW×pxH, unlike the map
	// it replaced. ensureSize drops stale contents on an actual size
	// change, which is safe here — see its doc comment — because every
	// screen's Render calls Clear() before drawing again after a resize.
	c.pixelTags.ensureSize(c.pxW, c.pxH)
}

// Clear wipes the drawille buffer, per-pixel color tags, and cell
// overlays. Call at the start of every frame.
func (c *Canvas) Clear() {
	c.dc.Clear()
	c.pixelTags.clear()
	c.cellOverlays = nil
	c.cellOverlayColors = nil
}

// SetCellOverlay places a Unicode glyph at the cell containing the
// given world coord, replacing whatever drawille would have rendered
// there at String() time. Color comes from the cell's pixel tags
// (FillColoredDisk etc) — combine with a tagged draw to get a
// colored overlay. v0.5.12+ — used for body-identity glyphs (☉ ◉ ●
// ○) so different body types read distinctly.
func (c *Canvas) SetCellOverlay(w orbital.Vec3, glyph rune) {
	px, py, ok := c.Project(w)
	if !ok {
		return
	}
	cellX, cellY := px/2, py/4
	if cellX < 0 || cellX >= c.cols || cellY < 0 || cellY >= c.rows {
		return
	}
	if c.cellOverlays == nil {
		c.cellOverlays = make(map[[2]int]rune)
	}
	c.cellOverlays[[2]int{cellX, cellY}] = glyph
}

// SetCellOverlayColored is SetCellOverlay that also pins the glyph's
// foreground to color, so the overlay reads in that exact color rather
// than the cell's pixelTag majority. Without the pin, a glyph whose
// cell overlaps a body's projected disk picks up the body's color (the
// body's pixels outvote the glyph's own tag) — so e.g. a vessel marker
// flips to Earth-blue while passing in front of Earth. Pinning keeps
// vessel glyphs in their own color regardless of what's behind them.
func (c *Canvas) SetCellOverlayColored(w orbital.Vec3, glyph rune, color lipgloss.TerminalColor) {
	px, py, ok := c.Project(w)
	if !ok {
		return
	}
	cellX, cellY := px/2, py/4
	if cellX < 0 || cellX >= c.cols || cellY < 0 || cellY >= c.rows {
		return
	}
	if c.cellOverlays == nil {
		c.cellOverlays = make(map[[2]int]rune)
	}
	c.cellOverlays[[2]int{cellX, cellY}] = glyph
	if c.cellOverlayColors == nil {
		c.cellOverlayColors = make(map[[2]int]lipgloss.TerminalColor)
	}
	c.cellOverlayColors[[2]int{cellX, cellY}] = color
}

// ClearCellOverlay removes any overlay glyph at the cell containing
// the given world coord so the cell's underlying braille pattern
// renders through. Used by the v0.11.3 composed-rocket render path:
// braille rocket pixels need to override the LUT's body-fixed
// SetCellOverlay glyphs at the pad without losing the braille dots.
// No-op when the cell has no overlay set or the coord projects
// off-canvas.
func (c *Canvas) ClearCellOverlay(w orbital.Vec3) {
	px, py, ok := c.Project(w)
	if !ok {
		return
	}
	cellX, cellY := px/2, py/4
	if c.cellOverlays == nil {
		return
	}
	delete(c.cellOverlays, [2]int{cellX, cellY})
	// Also drop any pinned overlay color (SetCellOverlayColored) so it
	// can't recolor the braille pattern that now renders through.
	delete(c.cellOverlayColors, [2]int{cellX, cellY})
}

// SetCellLabel writes text into consecutive cells starting at
// (col, row), one rune per cell, going right. Out-of-bounds cells
// are skipped silently. Used by the orbit / maneuver screens to
// stamp HUD-style overlays directly into the canvas (e.g. a
// "view: top" label in the bottom-right corner) so the indicator
// stays attached to the projection it describes. v0.7.4+.
//
// Color comes from each cell's existing pixelTags — uncolored
// (background-empty) cells render the label in the terminal
// default, which is fine for the corner-overlay use case.
func (c *Canvas) SetCellLabel(col, row int, text string) {
	if c.cellOverlays == nil {
		c.cellOverlays = make(map[[2]int]rune)
	}
	i := 0
	for _, ch := range text {
		x := col + i
		i++
		if x < 0 || x >= c.cols || row < 0 || row >= c.rows {
			continue
		}
		c.cellOverlays[[2]int{x, row}] = ch
	}
}

// SetCellLabelColored is SetCellLabel that also pins the label's
// foreground to color, so corner-overlay HUD text reads in the theme
// palette instead of the terminal default. Same right-going,
// one-rune-per-cell, skip-out-of-bounds behavior as SetCellLabel.
func (c *Canvas) SetCellLabelColored(col, row int, text string, color lipgloss.TerminalColor) {
	c.SetCellLabel(col, row, text)
	if c.cellOverlayColors == nil {
		c.cellOverlayColors = make(map[[2]int]lipgloss.TerminalColor)
	}
	i := 0
	for range text {
		x := col + i
		i++
		if x < 0 || x >= c.cols || row < 0 || row >= c.rows {
			continue
		}
		c.cellOverlayColors[[2]int{x, row}] = color
	}
}

// FillColoredDisk fills a disk of the given pixel radius around a
// world coord AND tags every set pixel with the given color. Used
// for body rendering: the cells containing body pixels render in
// the body's palette color, while cells that happen to overlap with
// craft / orbit / marker pixels (untagged) stay default-colored.
//
// Replaces the v0.5.3 AddColoredDisk approach which painted the
// body's entire cell-bounding-box, bleeding into nearby content.
func (c *Canvas) FillColoredDisk(center orbital.Vec3, pxRadius int, color lipgloss.Color) {
	c.FillColoredDiskTagged(center, pxRadius, CellTag{Color: color})
}

// FillColoredDiskTagged is FillColoredDisk that records the supplied
// CellTag (color + body / node / vessel hit fields) on every pixel
// it sets. v0.6.4+: callers pass a tag with BodyID set so HitAt can
// resolve mouse clicks back to bodies. Tag values default to "no
// hit" so older draw paths that just need color stay untouched.
func (c *Canvas) FillColoredDiskTagged(center orbital.Vec3, pxRadius int, tag CellTag) {
	if pxRadius < 1 {
		pxRadius = 1
	}
	cx, cy, _ := c.Project(center)
	// #363 introduced a per-radius offset-mask cache here (diskOffsetCache);
	// #367 removed it after re-benchmarking with the curve-geometry cache
	// (canvas_curve_cache.go) also in place: with the color-grid cache
	// (diskRasterCache) and the off-canvas DiskIntersectsCanvas skip
	// already doing the heavy lifting, the offset mask's own nested-loop
	// re-derivation benchmarked within noise on BenchmarkOrbitViewRenderIdle
	// (see the #367 PR body) — not worth the cache's own bookkeeping and
	// retained memory (the LRU cap existed specifically to bound a real
	// leak: an unbounded version measured ~1.22 GB retained after a
	// radius-1..384 zoom sweep).
	r2 := pxRadius * pxRadius
	for dy := -pxRadius; dy <= pxRadius; dy++ {
		for dx := -pxRadius; dx <= pxRadius; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			px, py := cx+dx, cy+dy
			if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
				continue
			}
			c.dc.Set(px, py)
			c.pixelTags.set(px, py, tag)
		}
	}
}

// pickDominantColor returns the highest-count color in counts. Ties
// break deterministically on the lexicographically smaller color
// string so the chosen color is stable frame-to-frame regardless of
// Go's randomized map iteration order. Without this tie-break,
// textured-body cells with mixed-color pixel sets (v0.7.2.1+)
// flicker as a different "first" color wins each render.
func pickDominantColor(counts map[lipgloss.Color]int) lipgloss.Color {
	var best lipgloss.Color
	bestN := 0
	for color, n := range counts {
		if n > bestN || (n == bestN && string(color) < string(best)) {
			bestN = n
			best = color
		}
	}
	return best
}

// FillTexturedDiskTagged fills a disk like FillColoredDiskTagged but
// asks `texture` for the per-pixel color instead of using a single
// uniform value from the supplied tag. The tag's BodyID / NodeIdx /
// IsVessel fields still apply to every set pixel so HitAt resolves
// clicks correctly; only the Color field is overwritten per pixel.
//
// Used for body-texture rendering (v0.7.2.1+) — Earth's continents +
// cloud streaks, with future bodies plugging in via the same path.
func (c *Canvas) FillTexturedDiskTagged(center orbital.Vec3, pxRadius int, texture func(dx, dy int) lipgloss.Color, tag CellTag) {
	if pxRadius < 1 {
		pxRadius = 1
	}
	cx, cy, _ := c.Project(center)
	// See FillColoredDiskTagged's comment: the #363 offset-mask cache
	// this loop used to go through was removed in #367 after
	// re-benchmarking within noise.
	r2 := pxRadius * pxRadius
	for dy := -pxRadius; dy <= pxRadius; dy++ {
		for dx := -pxRadius; dx <= pxRadius; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			px, py := cx+dx, cy+dy
			if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
				continue
			}
			c.dc.Set(px, py)
			t := tag
			t.Color = texture(dx, dy)
			c.pixelTags.set(px, py, t)
		}
	}
}

// RingColoredOutline mirrors RingOutline but tags every set pixel
// with the given color. Used for the system primary's hollow ring
// and ring-system bodies (Saturn).
//
// v0.5.15: samples is hard-capped at 4× the canvas pixel diagonal —
// at extreme zoom (e.g. focused on Phobos with Saturn's rings
// projecting to hundreds of millions of pixels) the prior unbounded
// `pxRadius * 8` would loop billions of times and lock the game.
// The cap still produces dense enough sampling for the ring to look
// smooth at any radius that contributes visible pixels to the canvas.
func (c *Canvas) RingColoredOutline(center orbital.Vec3, pxRadius int, color lipgloss.Color) {
	c.RingColoredOutlineTagged(center, pxRadius, CellTag{Color: color})
}

// RingColoredOutlineTagged is RingColoredOutline that records the
// supplied CellTag on every pixel it sets. v0.6.4+ — same role as
// FillColoredDiskTagged for ring-system bodies.
func (c *Canvas) RingColoredOutlineTagged(center orbital.Vec3, pxRadius int, tag CellTag) {
	if pxRadius < 1 {
		pxRadius = 1
	}
	cx, cy, _ := c.Project(center)
	samples := pxRadius * 8
	if samples < 16 {
		samples = 16
	}
	maxSamples := 4 * (c.pxW + c.pxH)
	if samples > maxSamples {
		samples = maxSamples
	}
	for i := 0; i < samples; i++ {
		theta := 2 * math.Pi * float64(i) / float64(samples)
		px := cx + int(math.Round(float64(pxRadius)*math.Cos(theta)))
		py := cy + int(math.Round(float64(pxRadius)*math.Sin(theta)))
		if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
			continue
		}
		c.dc.Set(px, py)
		c.pixelTags.set(px, py, tag)
	}
}

// ringDotSpacingPx is the circumference distance (pixels) between
// successive dots of RingDottedColored — sparse enough that the ring
// reads as a quiet dotted boundary cue rather than solid ink. ADR 0041
// §2: the SOI Ring is Scenery (dotted, faint) alongside body orbits, so
// this shares body orbits' classSceneryNearSpacingPx cadence exactly
// (folded from its own previously-separate value of 4) rather than a
// second, incidentally-close number.
const ringDotSpacingPx = classSceneryNearSpacingPx

// RingDottedColored draws a dotted screen-space circle of pxRadius
// pixels around center — the SOI Ring (ADR 0021 C). A sphere's
// orthographic projection is the same circle under every view basis, so
// like RingColoredOutline the ring draws in screen space rather than as
// a tilted world-plane ellipse. Dots are spaced ~ringDotSpacingPx of
// circumference apart.
//
// ADR 0042 §3: the sweep is restricted to the angular window the canvas
// subtends from the ring's centre (visibleAngleWindow), so the cost is
// O(the visible arc) rather than O(the circumference). Previously the
// full circle was sampled and the count capped at 4×(pxW+pxH), which
// meant the dots thinned out as the player zoomed in — the ring
// dissolved exactly when it mattered most. Now the commanded spacing
// holds at any radius.
func (c *Canvas) RingDottedColored(center orbital.Vec3, pxRadius int, color lipgloss.Color) {
	if pxRadius < 1 {
		return
	}
	cxF, cyF := c.projectPx(center)
	start, span, ok := visibleAngleWindow(cxF, cyF, float64(pxRadius), c.pxW, c.pxH)
	if !ok {
		return
	}
	samples := int(math.Round(span * float64(pxRadius) / ringDotSpacingPx))
	if samples < 8 {
		samples = 8
	}
	maxSamples := 4 * (c.pxW + c.pxH)
	if samples > maxSamples {
		samples = maxSamples
	}
	tag := CellTag{Color: color}
	for i := 0; i < samples; i++ {
		theta := start + span*float64(i)/float64(samples)
		px := int(math.Round(cxF + float64(pxRadius)*math.Cos(theta)))
		py := int(math.Round(cyF + float64(pxRadius)*math.Sin(theta)))
		if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
			continue
		}
		c.dc.Set(px, py)
		c.pixelTags.set(px, py, tag)
	}
}

// RingTiltedOutlineTagged draws a ring of radius rMeters around center in
// the plane spanned by basis vectors e1, e2 (both in world inertial
// frame, both unit-length, mutually orthogonal), tagging every pixel it
// sets with the full CellTag. v0.8.5.7+ — for ringed bodies whose ring
// plane is perpendicular to a tilted spin axis (Saturn 26.7°, Uranus
// 97.8°), this draws the ring as the foreshortened ellipse the current
// canvas view sees rather than as a screen-space circle.
//
// The samples loop is identical in spirit to RingColoredOutlineTagged;
// the only difference is that the per-sample world position is in
// 3D and goes through Canvas.Project (which already accounts for
// the canvas's view basis). Same 4×(pxW+pxH) cap on samples to keep
// the inner loop bounded at extreme zoom.
//
// #369 review F3: the plain-color wrapper (RingTiltedOutline) was deleted
// — every production call site went through the cache
// (RingTiltedOutlineCachedTagged, canvas_ring_cache.go) once #369 landed,
// leaving it with zero callers. This function stays: it's the uncached
// ground truth the ring cache's tests compare against, and the recording
// variant miss path (ringTiltedOutlineTaggedRecording) shares its body.
func (c *Canvas) RingTiltedOutlineTagged(center orbital.Vec3, e1, e2 orbital.Vec3, rMeters float64, tag CellTag) {
	c.ringTiltedOutlineTaggedRecording(center, e1, e2, rMeters, tag, nil)
}

// ringTiltedOutlineTaggedRecording is RingTiltedOutlineTagged with an
// optional pixel recorder (#369, mirroring drawEllipseAdaptiveTaggedRecording
// from #367): every pixel actually plotted is also handed to record (nil ⇒
// identical to RingTiltedOutlineTagged). The ring-geometry cache
// (canvas_ring_cache.go) calls this directly on a cache miss to capture
// the projected output for replay, so caching costs nothing beyond the
// draw that already had to happen.
func (c *Canvas) ringTiltedOutlineTaggedRecording(center orbital.Vec3, e1, e2 orbital.Vec3, rMeters float64, tag CellTag, record func(px, py int)) {
	if rMeters <= 0 {
		return
	}
	// Sample density: aim for ~8 samples per visible-pixel of arc
	// length. The arc length is ≤ 2π·rMeters·scale screen pixels;
	// for the ring's bounding box we cap by the canvas perimeter
	// the same way RingColoredOutline does.
	pxRadius := int(rMeters * c.scale)
	samples := pxRadius * 8
	if samples < 16 {
		samples = 16
	}
	maxSamples := 4 * (c.pxW + c.pxH)
	if samples > maxSamples {
		samples = maxSamples
	}
	// #369 review F1: gate the pixelTags write on tagged the same way
	// walkPixelSegment/blitCurvePoints do — pixelTagGrid.set() is itself
	// now a no-op for a zero tag (see its doc comment), so this isn't
	// load-bearing for correctness, but it skips the call entirely rather
	// than making it and immediately returning, and keeps this function
	// consistent with the rest of the file's *Tagged draw helpers.
	tagged := tag != (CellTag{})
	for i := 0; i < samples; i++ {
		theta := 2 * math.Pi * float64(i) / float64(samples)
		cT, sT := math.Cos(theta), math.Sin(theta)
		// world point = center + R·(e1·cos(θ) + e2·sin(θ))
		wp := center.
			Add(e1.Scale(rMeters * cT)).
			Add(e2.Scale(rMeters * sT))
		px, py, ok := c.Project(wp)
		if !ok {
			continue
		}
		c.dc.Set(px, py)
		if tagged {
			c.pixelTags.set(px, py, tag)
		}
		// record fires regardless of tagged: it captures the DRAWN POINT
		// for cache replay (canvas_ring_cache.go), not the tag — a cache
		// hit re-applies whatever tag the replay call passes (possibly a
		// different one, e.g. a color-only change), the same "geometry
		// cached, color applied fresh" contract DrawEllipseClassCachedTagged
		// documents.
		if record != nil {
			record(px, py)
		}
	}
}

// FillProjectedSphere fills every canvas pixel that lies inside the
// orthographic projection of a sphere of `radius` metres centred at
// `center` world coords. Unlike FillColoredDisk (which iterates the
// disk's own bounding box and so hangs on planet-scale radii at low
// zoom), this iterates canvas pixels — work bounded by canvas size,
// not sphere size. v0.11.0+: used by the ViewLaunch chase-cam to
// flood-fill the body below the horizon with SurfaceColor.
func (c *Canvas) FillProjectedSphere(center orbital.Vec3, radius float64, color lipgloss.Color) {
	if radius <= 0 {
		return
	}
	cx, cy, _ := c.Project(center)
	pxR := radius * c.scale
	pxR2 := pxR * pxR
	tag := CellTag{Color: color}
	for py := 0; py < c.pxH; py++ {
		dy := float64(py - cy)
		dy2 := dy * dy
		if dy2 > pxR2 {
			continue
		}
		for px := 0; px < c.pxW; px++ {
			dx := float64(px - cx)
			if dx*dx+dy2 > pxR2 {
				continue
			}
			c.dc.Set(px, py)
			c.pixelTags.set(px, py, tag)
		}
	}
}

// HorizonBand is one solid-colour strip in a Canvas.FillHorizonBands
// gradient: Rows sub-pixel rows painted in Color, working outward
// from the horizon edge. v0.40 / issue #424.
type HorizonBand struct {
	Color lipgloss.Color
	Rows  int
}

// FillHorizonBands paints a LIMITED horizon strip in place of
// FillProjectedSphere's unbroken disk flood (v0.40 / issue #424, ADR
// 0048 §4). The old flood painted the WHOLE silhouette below the
// horizon one flat shade all the way to the far canvas edge — no
// horizon line, no scale reference (the UX review's dump at 104×24:
// "ten unbroken rows of identical... blocks"). This walks the SAME
// near-flat sphere edge FillProjectedSphere uses (locally flat
// because planet radius ≫ chase-cam distance) but only paints a
// bounded run of rows out from that edge on each side: `ground` bands
// work DOWN from the edge (into the body — a lit apron right at the
// horizon, a dimmer band beneath it), `sky` bands work UP from the
// edge (away from the body — a horizon glow fading toward open
// space). Anything past the given bands is left untouched (terminal
// background) — that's what turns a flood into a readable STRIP: a
// player judges scale against the visible ground/sky edge instead of
// an undifferentiated colour field.
//
// Ground bands stay clipped to the sphere's own silhouette (dy² ≤
// pxR²) so the strip still narrows correctly as the visible arc
// curves at altitude, matching FillProjectedSphere's existing
// curvature behaviour. Sky bands have no silhouette to clip against —
// they paint into open space — so only the canvas bounds gate them;
// callers shrink the row counts themselves as altitude grows (the ADR's
// "thins with altitude").
func (c *Canvas) FillHorizonBands(center orbital.Vec3, radius float64, ground, sky []HorizonBand) {
	if radius <= 0 {
		return
	}
	cx, cy, _ := c.Project(center)
	pxR := radius * c.scale
	pxR2 := pxR * pxR
	for px := 0; px < c.pxW; px++ {
		dx := float64(px - cx)
		dx2 := dx * dx
		if dx2 > pxR2 {
			continue // this column never reaches the sphere's edge
		}
		edge := float64(cy) - math.Sqrt(pxR2-dx2) // topmost "ground" row here

		row := edge
		for _, band := range ground {
			tag := CellTag{Color: band.Color}
			for i := 0; i < band.Rows; i++ {
				py := int(math.Round(row))
				row++
				if py < 0 || py >= c.pxH {
					continue
				}
				dy := float64(py) - float64(cy)
				if dy*dy > pxR2 {
					continue
				}
				c.dc.Set(px, py)
				c.pixelTags.set(px, py, tag)
			}
		}

		row = edge - 1
		for _, band := range sky {
			tag := CellTag{Color: band.Color}
			for i := 0; i < band.Rows; i++ {
				py := int(math.Round(row))
				row--
				if py < 0 || py >= c.pxH {
					continue
				}
				c.dc.Set(px, py)
				c.pixelTags.set(px, py, tag)
			}
		}
	}
}

// PlotColored sets a single pixel and tags it with the given color.
// Used by callers that want a tagged dot (e.g. v0.6.1 maneuver-leg
// preview). v0.6.4+: routed through PlotColoredTagged so tagged
// variants of node-cluster / vessel-glyph plots can land hit-test
// metadata while reusing the same path.
func (c *Canvas) PlotColored(w orbital.Vec3, color lipgloss.Color) {
	c.PlotColoredTagged(w, CellTag{Color: color})
}

// PlotColoredTagged is PlotColored with the full CellTag (color +
// hit-test metadata) preserved on the pixel. v0.6.4+.
func (c *Canvas) PlotColoredTagged(w orbital.Vec3, tag CellTag) {
	if px, py, ok := c.Project(w); ok {
		c.dc.Set(px, py)
		c.pixelTags.set(px, py, tag)
	}
}

// PlotDenseLineColored plots a dotted line between world points a and b by
// walking the projected pixel segment and setting one braille pixel every
// `step` pixels (step=1 → solid, step≥2 → dashed texture). It keeps on-screen
// dot spacing constant regardless of zoom: consecutive orbit/arc samples that
// spread apart when zoomed in get the gap between them filled (ADR 0023 C).
// The chord is exact under the affine projection, so no curve fidelity is
// lost beyond the sampling already present.
//
// ADR 0042 §3: a long chord is now CLIPPED AND BRIDGED, not refused. The old
// rule dropped any chord longer than the canvas's shorter dimension back to
// endpoint dots, on the theory that such a chord straddles a fast periapsis
// arc and a straight fill would shoot a line across the view. That theory
// held only because the curve upstream was sampled at a fixed count: once the
// caller flattens adaptively (see arc.go), a long chord is long because the
// view is zoomed in, and it IS the curve — refusing it is precisely what made
// zooming in dissolve a trajectory into dust. Clipping keeps the cost O(the
// visible run) however far off-canvas the far endpoint sits.
func (c *Canvas) PlotDenseLineColored(a, b orbital.Vec3, color lipgloss.Color, step int) {
	ax, ay := c.projectPx(a)
	bx, by := c.projectPx(b)
	c.walkPixelSegment(ax, ay, bx, by, CellTag{Color: color}, step, 0, 0, 0, 0, nil)
}

// PlotDenseLineForcedColored draws a dotted line between world points a and b.
// Kept as a distinct name for the CommNet relay sightline (C2-7), where the
// chord IS the real straight path and must draw all the way toward its (often
// off-screen) far endpoint — a Moon→Earth relay hop spans far more than the
// canvas. Since ADR 0042 §3 replaced PlotDenseLineColored's long-chord refusal
// with the same clipped bridging, the two behave identically; the separate
// entry point survives so the call site keeps stating its intent.
func (c *Canvas) PlotDenseLineForcedColored(a, b orbital.Vec3, color lipgloss.Color, step int) {
	c.PlotDenseLineColored(a, b, color, step)
}

// PlotDensePolylineColored inks a run of world points as one connected dotted
// curve: every consecutive pair is bridged like PlotDenseLineColored, but the
// dash phase carries ACROSS the joins, so the dot cadence is measured along
// the whole polyline's on-screen arc length rather than restarting at each
// sample. Without that, a densely sampled leg (or a finely flattened ellipse)
// inks solid regardless of `step`, because every segment re-plots its own
// start pixel. A single point plots as a dot.
func (c *Canvas) PlotDensePolylineColored(pts []orbital.Vec3, color lipgloss.Color, step int) {
	c.plotDensePolylineTagged(pts, CellTag{Color: color}, step)
}

// plotDensePolylineTagged is PlotDensePolylineColored carrying the full
// CellTag (colour + Inspect Owner) onto every pixel it lights, so a
// trajectory's pixels answer HitAt the way a body disk or vessel glyph
// already does (ADR 0041 §3).
func (c *Canvas) plotDensePolylineTagged(pts []orbital.Vec3, tag CellTag, step int) {
	if len(pts) == 0 {
		return
	}
	if len(pts) == 1 {
		c.PlotColoredTagged(pts[0], tag)
		return
	}
	phase := 0.0
	ax, ay := c.projectPx(pts[0])
	for i := 1; i < len(pts); i++ {
		bx, by := c.projectPx(pts[i])
		phase = c.walkPixelSegment(ax, ay, bx, by, tag, step, phase, 0, 0, 0, nil)
		ax, ay = bx, by
	}
}

// plotDensePolylineDashedColored is PlotDensePolylineColored's dashed
// sibling (ADR 0041 §2 / issue #346's LineClass vocabulary): instead of a
// single lit pixel every `step` px, it inks a contiguous run of `onPx`
// pixels followed by a gap of `offPx` pixels, repeating — a genuine dash
// pattern rather than sparse dots. Phase carries across the whole
// polyline exactly like PlotDensePolylineColored, so the dash/gap cadence
// holds across joins instead of restarting (and therefore looking solid)
// at every sample.
func (c *Canvas) plotDensePolylineDashedColored(pts []orbital.Vec3, color lipgloss.Color, onPx, offPx int) {
	c.plotDensePolylineDashedTagged(pts, CellTag{Color: color}, onPx, offPx)
}

// plotDensePolylineDashedTagged is plotDensePolylineDashedColored carrying
// the full CellTag (colour + Inspect Owner) — the Planned-class sibling of
// plotDensePolylineTagged.
func (c *Canvas) plotDensePolylineDashedTagged(pts []orbital.Vec3, tag CellTag, onPx, offPx int) {
	if len(pts) == 0 {
		return
	}
	if len(pts) == 1 {
		c.PlotColoredTagged(pts[0], tag)
		return
	}
	phase := 0.0
	ax, ay := c.projectPx(pts[0])
	for i := 1; i < len(pts); i++ {
		bx, by := c.projectPx(pts[i])
		phase = c.walkPixelDashSegment(ax, ay, bx, by, tag, onPx, offPx, phase)
		ax, ay = bx, by
	}
}

// walkPixelDashSegment is walkPixelSegment's dashed sibling: it inks every
// integer pixel of ON-SCREEN ARC LENGTH that falls in the "on" part of a
// repeating onPx-on / offPx-off cycle, instead of a single dot per `step`.
// Walking pixel-by-pixel (rather than jumping straight to each mark, as
// walkPixelSegment does) is what makes a dash a CONTIGUOUS run instead of
// a lone point; the cost is still bounded by the canvas-clipped run
// length, so it stays cheap. `phase` is the position within the cycle the
// curve already occupies, carried across chords/segments the same way
// walkPixelSegment carries its dot phase, so a dash never restarts (and
// therefore never reads as solid) at a chord or polyline join.
func (c *Canvas) walkPixelDashSegment(ax, ay, bx, by float64, tag CellTag, onPx, offPx int, phase float64) float64 {
	if onPx < 1 {
		onPx = 1
	}
	if offPx < 0 {
		offPx = 0
	}
	cycle := float64(onPx + offPx)
	if !(phase >= 0) { // NaN-safe
		phase = 0
	}
	// A non-finite endpoint is not a point on this curve, so it neither inks
	// nor advances the dash cadence — see finitePx and clipSegmentToCanvas.
	if !finitePx(ax, ay) || !finitePx(bx, by) {
		return phase
	}
	full := segLenPx(ax, ay, bx, by)
	newPhase := math.Mod(phase+full, cycle)
	cax, cay, cbx, cby, ok := clipSegmentToCanvas(ax, ay, bx, by, c.pxW, c.pxH)
	if !ok {
		return newPhase
	}
	tagged := tag != (CellTag{})
	plotPx := func(px, py int) {
		if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
			return
		}
		c.dc.Set(px, py)
		if tagged {
			c.pixelTags.set(px, py, tag)
		}
	}
	// Length clipped off the a-end still counts toward the cycle position,
	// matching walkPixelSegment's `lead` so the dash pattern doesn't slide
	// when the visible run changes.
	lead := phase + segLenPx(ax, ay, cax, cay)
	clipLen := segLenPx(cax, cay, cbx, cby)
	if clipLen == 0 {
		if math.Mod(lead, cycle) < float64(onPx) {
			plotPx(int(math.Round(cax)), int(math.Round(cay)))
		}
		return newPhase
	}
	dx, dy := cbx-cax, cby-cay
	steps := int(math.Ceil(clipLen))
	for s := 0; s <= steps; s++ {
		d := float64(s)
		if d > clipLen {
			d = clipLen
		}
		if math.Mod(lead+d, cycle) < float64(onPx) {
			f := d / clipLen
			plotPx(int(math.Round(cax+dx*f)), int(math.Round(cay+dy*f)))
		}
	}
	return newPhase
}

// walkPixelSegment inks the pixel segment (ax,ay)→(bx,by), clipped to the
// canvas, placing a dot every `step` pixels of ON-SCREEN ARC LENGTH and
// returning the phase to hand to the next segment of the same curve.
// `phase` is how far past the last dot the curve already is, so a caller
// that walks a flattened arc chord by chord gets one continuous cadence.
// Arc length is therefore the parameter — which is both why the dot spacing
// is zoom-invariant and what a future dashed / dotted line vocabulary needs.
//
// The phase is a float and advances by the FULL segment length, including
// the part clipped away: sub-pixel chords (a finely flattened curve) still
// accumulate toward the next dot instead of each plotting their own start
// pixel and inking solid, and the pattern stays put as the view pans.
//
// exR > 0 excludes pixels within exR of (exCx, exCy) — the body-disk cut that
// makes an occluded far-side arc read as passing behind the body. A zero-value
// tag sets pixels without recording a colour (the untagged Plot path).
//
// record, if non-nil, is called with every pixel actually plotted (after the
// bounds and exR exclusion tests) — #367's curve-geometry cache uses this to
// capture the exact set of pixels a miss produced, so a later cache hit can
// replay them without re-flattening or re-walking anything. nil for every
// caller that isn't building a cache entry.
func (c *Canvas) walkPixelSegment(ax, ay, bx, by float64, tag CellTag, step int, phase float64, exCx, exCy, exR float64, record func(px, py int)) float64 {
	if step < 1 {
		step = 1
	}
	stepF := float64(step)
	if !(phase >= 0) { // NaN-safe
		phase = 0
	}
	// A non-finite endpoint is not a point on this curve, so it neither inks
	// nor advances the dot cadence — see finitePx and clipSegmentToCanvas.
	if !finitePx(ax, ay) || !finitePx(bx, by) {
		return phase
	}
	full := segLenPx(ax, ay, bx, by)
	advanced := math.Mod(phase+full, stepF)
	cax, cay, cbx, cby, ok := clipSegmentToCanvas(ax, ay, bx, by, c.pxW, c.pxH)
	if !ok {
		return advanced
	}
	tagged := tag != (CellTag{})
	exR2 := exR * exR
	plotPx := func(px, py int) {
		if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
			return
		}
		if exR > 0 {
			dx, dy := float64(px)-exCx, float64(py)-exCy
			if dx*dx+dy*dy <= exR2 {
				return
			}
		}
		c.dc.Set(px, py)
		if tagged {
			c.pixelTags.set(px, py, tag)
		}
		if record != nil {
			record(px, py)
		}
	}
	// Length clipped off the a-end still counts toward the cadence, so the
	// dot positions don't slide when the visible run changes.
	lead := phase + segLenPx(ax, ay, cax, cay)
	clipLen := segLenPx(cax, cay, cbx, cby)
	if clipLen == 0 {
		if math.Mod(lead, stepF) == 0 {
			plotPx(int(math.Round(cax)), int(math.Round(cay)))
		}
		return advanced
	}
	dx, dy := cbx-cax, cby-cay
	for k := math.Ceil(lead / stepF); ; k++ {
		s := k*stepF - lead
		if s > clipLen {
			break
		}
		f := s / clipLen
		plotPx(int(math.Round(cax+dx*f)), int(math.Round(cay+dy*f)))
	}
	return advanced
}

// segLenPx is the on-screen length of a pixel segment — the arc-length unit
// the dense-line cadence counts in, so a dot every `step` means every `step`
// pixels of curve however the curve is oriented. Clamped so an
// astronomically distant (or NaN) projected endpoint can't run away with the
// loop.
func segLenPx(ax, ay, bx, by float64) float64 {
	d := math.Hypot(bx-ax, by-ay)
	if !(d < 1e9) { // NaN-safe
		return 1e9
	}
	return d
}

// finitePx reports whether a projected pixel coordinate pair is a real
// point. The clamp in segLenPx keeps a merely ENORMOUS coordinate honest —
// the segment still clips correctly and the visible run is still right —
// but a NaN or ±Inf coordinate is not a point at all, and the only correct
// thing to do with it is draw nothing. Callers use it to bail before the
// arc-length walk, so a bad sample costs one comparison instead of the
// clamp's worth of iterations.
func finitePx(x, y float64) bool {
	return !math.IsNaN(x) && !math.IsNaN(y) && !math.IsInf(x, 0) && !math.IsInf(y, 0)
}

// clipSegmentToCanvas clips the pixel segment [(ax,ay),(bx,by)] to the canvas
// rectangle [0,w-1]×[0,h-1] via Liang-Barsky, returning the clipped endpoints
// and ok=false when the segment lies wholly outside. Bounds the dense-line
// iteration so a chord toward a far off-screen endpoint costs O(canvas), not
// O(projected distance). Works in floats: an adaptively flattened arc hands
// over raw projected coordinates, which are only rounded once inside the
// canvas rectangle.
//
// A non-finite endpoint is refused outright. Liang-Barsky cannot reject one
// on its own — every comparison against NaN is false, so all four
// return-false branches are skipped and the routine reports ok=true with NaN
// endpoints. The float rewrite (ADR 0042 §3) made that fatal rather than
// merely wrong: the walkers then take segLenPx's 1e9 NaN clamp as a real
// clipped length and iterate a billion times, which is a multi-second freeze
// of the whole render loop rather than one dropped line. The pre-ADR-0042
// callers rounded to int before clipping and so were bounded by accident;
// this is where that bound now lives, on purpose.
func clipSegmentToCanvas(ax, ay, bx, by float64, w, h int) (float64, float64, float64, float64, bool) {
	if !finitePx(ax, ay) || !finitePx(bx, by) {
		return 0, 0, 0, 0, false
	}
	x0, y0 := ax, ay
	dx, dy := bx-ax, by-ay
	maxX, maxY := float64(w-1), float64(h-1)
	p := [4]float64{-dx, dx, -dy, dy}
	q := [4]float64{x0, maxX - x0, y0, maxY - y0}
	u0, u1 := 0.0, 1.0
	for i := 0; i < 4; i++ {
		if p[i] == 0 {
			if q[i] < 0 {
				return 0, 0, 0, 0, false // parallel to an edge and outside it
			}
			continue
		}
		t := q[i] / p[i]
		if p[i] < 0 {
			if t > u1 {
				return 0, 0, 0, 0, false
			}
			if t > u0 {
				u0 = t
			}
		} else {
			if t < u0 {
				return 0, 0, 0, 0, false
			}
			if t < u1 {
				u1 = t
			}
		}
	}
	return x0 + u0*dx, y0 + u0*dy, x0 + u1*dx, y0 + u1*dy, true
}

// Cols / Rows expose the configured terminal cell dimensions.
func (c *Canvas) Cols() int { return c.cols }
func (c *Canvas) Rows() int { return c.rows }

// Center sets the world coordinate that maps to the pixel grid center.
func (c *Canvas) Center(w orbital.Vec3) { c.centerW = w }

// CenterWorld returns the world coordinate currently mapped to the pixel grid
// center (the inverse of Center). Test/diagnostic accessor.
func (c *Canvas) CenterWorld() orbital.Vec3 { return c.centerW }

// Scale returns the current pixels-per-meter.
func (c *Canvas) Scale() float64 { return c.scale }

// SetScale sets pixels-per-meter directly. Used by manual +/- zoom.
func (c *Canvas) SetScale(pxPerMeter float64) {
	if pxPerMeter > 0 {
		c.scale = pxPerMeter
	}
}

// FitTo sets scale so a circle of the given world radius (meters) around
// the current center fills ~90% of the smaller pixel dimension.
func (c *Canvas) FitTo(radiusMeters float64) {
	if radiusMeters <= 0 {
		return
	}
	shorter := float64(c.pxW)
	if c.pxH < c.pxW {
		shorter = float64(c.pxH)
	}
	c.scale = 0.45 * shorter / radiusMeters
}

// ZoomBy multiplies scale (e.g. 1.25 for zoom-in).
func (c *Canvas) ZoomBy(factor float64) {
	if factor > 0 {
		c.scale *= factor
	}
}

// Project converts a world-frame inertial Vec3 to integer pixel coords.
// Y is flipped so increasing world-Y visually points up. Returns the
// pixel location and ok=false if the point is off-canvas.
func (c *Canvas) Project(w orbital.Vec3) (int, int, bool) {
	rel := w.Sub(c.centerW)
	relX := rel.X*c.basis.X.X + rel.Y*c.basis.X.Y + rel.Z*c.basis.X.Z
	relY := rel.X*c.basis.Y.X + rel.Y*c.basis.Y.Y + rel.Z*c.basis.Y.Z
	px := int(math.Round(relX*c.scale)) + c.pxW/2
	py := c.pxH/2 - int(math.Round(relY*c.scale))
	if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
		return px, py, false
	}
	return px, py, true
}

// ProjectClamped is Project with the result pinned inside the canvas
// rectangle instead of reported as off-canvas: an off-screen world point
// comes back as the edge pixel nearest to it. For overlays that must stay
// visible while still pointing at where their subject actually is — the
// Inspect name chip on an entity whose orbit line crosses the view but
// whose own marker does not. A non-finite projection (see finitePx) has
// no nearest edge, so it clamps to the canvas centre.
func (c *Canvas) ProjectClamped(w orbital.Vec3) (int, int) {
	fx, fy := c.projectPx(w)
	if !finitePx(fx, fy) {
		return c.pxW / 2, c.pxH / 2
	}
	px := int(math.Round(math.Min(math.Max(fx, 0), float64(c.pxW-1))))
	py := int(math.Round(math.Min(math.Max(fy, 0), float64(c.pxH-1))))
	return px, py
}

// Unproject returns the world coord whose Project would land at the
// given pixel — assuming the world point lies in the basis plane
// through centerW. v0.6.4+: paired with Project for view-aware mouse
// hit-testing in v0.6.4's mouse work. The Z axis (out of screen) is
// implicitly the depth direction; Unproject doesn't disambiguate, so
// callers that need a 3D world point on a specific surface must do
// their own ray-cast.
func (c *Canvas) Unproject(px, py int) orbital.Vec3 {
	relX := float64(px-c.pxW/2) / c.scale
	relY := float64(c.pxH/2-py) / c.scale
	return c.centerW.
		Add(c.basis.X.Scale(relX)).
		Add(c.basis.Y.Scale(relY))
}

// cellCentreDist2 is the squared pixel distance from a cell-local braille
// pixel (dx ∈ [0,2), dy ∈ [0,4)) to the geometric centre of its terminal
// cell, in HALF-pixel units so the arithmetic stays integral: the centre
// sits at (0.5, 1.5), which doubles to (1, 3).
//
// A braille pixel is cellW/2 wide and cellH/4 tall, and a terminal cell is
// roughly twice as tall as it is wide, so the two axes are close enough to
// square that an unweighted metric is honest here.
func cellCentreDist2(dx, dy int) int {
	x, y := 2*dx-1, 2*dy-3
	return x*x + y*y
}

// hitCandidates accumulates, per tag value, how many of a cell's pixels
// carry it and how close its closest pixel got to the cell centre. Both
// are needed to resolve a hit deterministically: count is the primary
// rule, distance is the tie-break.
type hitCandidates[K comparable] struct {
	count map[K]int
	dist  map[K]int
}

func newHitCandidates[K comparable]() hitCandidates[K] {
	return hitCandidates[K]{count: map[K]int{}, dist: map[K]int{}}
}

func (h hitCandidates[K]) add(k K, dist2 int) {
	if nearest, seen := h.dist[k]; !seen || dist2 < nearest {
		h.dist[k] = dist2
	}
	h.count[k]++
}

// HitAt aggregates the CellTag fields recorded on the 2×4 pixels
// of the terminal cell at (col, row). Returns the most-common
// non-empty BodyID among those pixels; NodeIdx and IsVessel resolve
// the same way (most common non-zero / true wins). Color falls out
// of the existing String() aggregation and is not returned here.
// v0.6.4+: paired with the app's MouseMsg dispatch so a click
// resolves to "what sim object is this cell rendering?"
//
// Resolution is DETERMINISTIC, on every platform and every call: see
// pickHit for the tie-break ladder. It has to be — the caller turns a
// hit into a mode change, and a click that answers differently on two
// consecutive presses reads as a broken input, not as an ambiguity.
//
// (col, row) outside the canvas → zero-value CellTag (no hit).
// Cells whose pixel set is entirely untagged also return zero —
// the caller treats this as "click on empty canvas" and may then
// Unproject for an in-orbit-plane world coord.
func (c *Canvas) HitAt(col, row int) CellTag {
	if col < 0 || col >= c.cols || row < 0 || row >= c.rows {
		return CellTag{}
	}
	if c.pixelTags.count() == 0 {
		return CellTag{}
	}
	bodies := newHitCandidates[string]()
	nodes := newHitCandidates[int]()
	owners := newHitCandidates[string]()
	vesselCount := 0
	pxStart, pyStart := col*2, row*4
	for dx := 0; dx < 2; dx++ {
		for dy := 0; dy < 4; dy++ {
			tag, ok := c.pixelTags.get(pxStart+dx, pyStart+dy)
			if !ok {
				continue
			}
			d2 := cellCentreDist2(dx, dy)
			if tag.BodyID != "" {
				bodies.add(tag.BodyID, d2)
			}
			if tag.NodeIdx != 0 {
				nodes.add(tag.NodeIdx, d2)
			}
			if tag.Owner != "" {
				owners.add(tag.Owner, d2)
			}
			if tag.IsVessel {
				vesselCount++
			}
		}
	}
	hit := CellTag{}
	if vesselCount > 0 {
		hit.IsVessel = true
	}
	if best, n, _ := pickHit(bodies); n > 0 {
		hit.BodyID = best
	}
	if best, n, _ := pickHit(nodes); n > 0 {
		hit.NodeIdx = best
	}
	// Owner resolves the same most-common-wins way (ADR 0041 §3): a cell
	// straddling two orbit lines answers with whichever owns more of its
	// eight pixels, which is also the one the player can see more of.
	// When neither owns more, OwnerTied says so, and the caller is
	// expected to pick its LEAST destructive reading of the click.
	if best, n, tied := pickHit(owners); n > 0 {
		hit.Owner = best
		hit.OwnerTied = tied
	}
	return hit
}

// pickHit resolves one cell's candidates to a single winner, plus whether
// the win was a genuine tie.
//
// The ladder, in order:
//
//  1. Most pixels in the cell wins — the ADR 0041 §3 rule, and the one
//     the player can see: whichever entity inked more of the cell.
//  2. Exact count tie → the candidate whose closest pixel is nearest the
//     cell centre, i.e. nearest to where the click landed. A cell is all
//     the sub-cell resolution a terminal mouse report gives us, so its
//     centre is the best available stand-in for the click point.
//  3. Still tied → the ordered key, purely so the answer is a function of
//     the pixels and nothing else.
//
// Rung 3 exists only as a backstop and is deliberately LAST: on its own
// it would systematically hand ties to the lowest-sorting key, which for
// Inspect owner keys ("v:1", "v:2", …) means the lowest craft ID —
// usually the active vessel, whose orbit line is the one click path with
// a destructive fallthrough (stage a burn). Rungs 1 and 2 are grounded in
// what is on screen; rung 3 is grounded in nothing, so it decides least.
//
// This used to be a bare `for k, n := range counts` keeping the first key
// that beat the running max, with no tie-break at all — which meant Go's
// per-range randomized map iteration picked the winner of every tie. It is
// the same latent class of bug pickDominantColor was fixed for in
// v0.7.2.1, missed here when hit-testing arrived in v0.6.4 and unreachable
// in practice until ADR 0041 §3 put a SECOND owner's ink on the map beside
// your own.
func pickHit[K cmp.Ordered](h hitCandidates[K]) (best K, bestN int, tied bool) {
	bestDist, atBest := 0, 0
	for k, n := range h.count {
		d := h.dist[k]
		switch {
		case n > bestN:
			best, bestN, bestDist, atBest = k, n, d, 1
		case n < bestN:
		default:
			atBest++
			if d < bestDist || (d == bestDist && k < best) {
				best, bestDist = k, d
			}
		}
	}
	return best, bestN, atBest > 1
}

// IsBehindBody reports whether a world point `samplePos` is occluded
// by a body at `bodyPos` with screen-projected radius `bodyPxR`,
// under the canvas's active basis. Two conditions must hold:
//
//  1. Negative depth: (samplePos − bodyPos) · DepthAxis() < 0 — the
//     sample is on the camera-far side of the body's plane.
//  2. Inside disk: the sample's projected pixel coord lies within
//     `bodyPxR` pixels of the body's projected pixel coord.
//
// Used by the orbit + maneuver renders (v0.6.4+) to skip plots
// behind a body in side views, so the body disk reads as opaque
// and the orbit visibly passes around — not through — it.
func (c *Canvas) IsBehindBody(samplePos, bodyPos orbital.Vec3, bodyPxR int) bool {
	depthAxis := c.basis.DepthAxis()
	rel := samplePos.Sub(bodyPos)
	depth := rel.X*depthAxis.X + rel.Y*depthAxis.Y + rel.Z*depthAxis.Z
	if depth >= 0 {
		return false
	}
	spx, spy, ok := c.Project(samplePos)
	if !ok {
		return false
	}
	bpx, bpy, _ := c.Project(bodyPos)
	dx := spx - bpx
	dy := spy - bpy
	return dx*dx+dy*dy <= bodyPxR*bodyPxR
}

// DrawEllipseOffsetFarSideDashed plots an ellipse with near-side
// pixels rendered at nearSpacingPx and far-side pixels rendered at
// nearSpacingPx*2 (visually dashed) in the same colour. Pixels truly
// occluded by the body disk — far side AND inside the body's projected
// pixel radius — are cut entirely. v0.10.6+.
//
// "Far side" is decided by the sign of (p − bodyPos) · DepthAxis()
// against the canvas's active basis: negative depth is behind the
// basis plane through bodyPos, positive is in front.
//
// Same-hue stippling is the lossless equivalent of KSP's
// dim-the-back-arc rendering — a terminal canvas can't do alpha,
// so spacing flips do the depth read instead. The convention is
// documented in internal/render/palette.go; see ColorBodyOrbit's doc
// comment for the standard.
//
// Pass bodyPxR = 0 to skip the disk-occlusion check (suitable for
// heliocentric body orbits, where the system primary's disk doesn't
// meaningfully occlude at default zoom).
//
// ADR 0042 §3 changed what the two count arguments mean. The ellipse is
// no longer a fixed loop of `samples` true-anomalies plotted every
// `nearStride`th index — that scattered into dust the moment the player
// zoomed in, because index stride is not a screen-space quantity. It is
// now flattened adaptively (arc.go) and inked at a constant on-screen dot
// spacing: `minSamples` is only a floor on the coarse pass, and
// `nearSpacingPx` is the near-side dot spacing in canvas pixels.
func (c *Canvas) DrawEllipseOffsetFarSideDashed(
	el orbital.Elements,
	offset orbital.Vec3,
	minSamples int,
	nearSpacingPx int,
	bodyPos orbital.Vec3,
	bodyPxR int,
	color lipgloss.Color,
) {
	if nearSpacingPx < 1 {
		nearSpacingPx = 1
	}
	c.drawEllipseAdaptive(el, offset, minSamples, nearSpacingPx, nearSpacingPx*2, bodyPos, bodyPxR, color)
}

// Plot sets the pixel at the given world coord. No-op if off-canvas.
func (c *Canvas) Plot(w orbital.Vec3) {
	if px, py, ok := c.Project(w); ok {
		c.dc.Set(px, py)
	}
}

// FillDisk fills a disk of the given pixel radius around a world coord.
// Used for perceived body size on the orbit canvas — the physical
// radius of a planet in world meters maps to far less than one pixel
// at system-view zoom, so the renderer passes a size-tier pxRadius
// (1 for moons, 2–4 for planets, 5+ for stars) rather than a true
// world-space radius. Off-canvas portions of the disk are clipped.
func (c *Canvas) FillDisk(center orbital.Vec3, pxRadius int) {
	if pxRadius < 1 {
		pxRadius = 1
	}
	cx, cy, _ := c.Project(center)
	r2 := pxRadius * pxRadius
	for dy := -pxRadius; dy <= pxRadius; dy++ {
		for dx := -pxRadius; dx <= pxRadius; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			px, py := cx+dx, cy+dy
			if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
				continue
			}
			c.dc.Set(px, py)
		}
	}
}

// RingOutline draws a ring (outline only) at the given pixel radius
// around a world coord. Distinguishes the system primary (hollow ring
// plus a filled center dot) from planets (fully filled disks). Uses
// Bresenham-style sampling on the pixel grid; off-canvas arcs are
// clipped.
func (c *Canvas) RingOutline(center orbital.Vec3, pxRadius int) {
	if pxRadius < 1 {
		pxRadius = 1
	}
	cx, cy, _ := c.Project(center)
	// Sample enough angles to leave no gaps at small radii.
	samples := pxRadius * 8
	if samples < 16 {
		samples = 16
	}
	// v0.5.15: cap samples at 4× canvas pixel diagonal so an extreme-
	// zoom call (radius in millions of pixels from a misaligned focus
	// + ring-system body) doesn't loop billions of times and lock.
	maxSamples := 4 * (c.pxW + c.pxH)
	if samples > maxSamples {
		samples = maxSamples
	}
	for i := 0; i < samples; i++ {
		theta := 2 * math.Pi * float64(i) / float64(samples)
		px := cx + int(math.Round(float64(pxRadius)*math.Cos(theta)))
		py := cy + int(math.Round(float64(pxRadius)*math.Sin(theta)))
		if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
			continue
		}
		c.dc.Set(px, py)
	}
}

// PlotArrow draws a chevron-style arrow (">") at a world point, rotated
// so the tip points in the direction of the supplied velocity vector.
// The arrow's body is two diagonal "wings" meeting at the forward tip;
// no filled stem, so the shape reads as a directional glyph without
// overwhelming the rest of the canvas. `size` is the half-length in
// pixels (total arrow width is roughly 2×size). Velocity magnitude is
// irrelevant — only the direction is used.
func (c *Canvas) PlotArrow(center, velocity orbital.Vec3, size int) {
	c.PlotArrowTagged(center, velocity, size, CellTag{})
}

// PlotArrowTagged is PlotArrow that records `tag` on every pixel
// it sets. v0.6.4+: callers pass IsVessel = true so HitAt resolves
// chevron pixels back to "vessel was clicked." Tag's Color drives
// per-cell colorization the same way as the other tagged helpers.
func (c *Canvas) PlotArrowTagged(center, velocity orbital.Vec3, size int, tag CellTag) {
	vMag := velocity.Norm()
	if vMag == 0 || size < 1 {
		return
	}
	const cos45 = 0.7071067811865476
	dx := velocity.X / vMag
	dy := -velocity.Y / vMag // screen Y flipped
	bx, by := -dx, -dy       // back-pointing unit
	// Left wing direction: rotate back-unit by +45°.
	lx := cos45 * (bx - by)
	ly := cos45 * (bx + by)
	// Right wing direction: rotate back-unit by -45°.
	rx := cos45 * (bx + by)
	ry := cos45 * (-bx + by)

	cx, cy, _ := c.Project(center)
	tipX := cx + int(math.Round(dx*float64(size)))
	tipY := cy + int(math.Round(dy*float64(size)))
	wingLen := int(float64(size) * 1.2)
	if wingLen < 1 {
		wingLen = 1
	}
	tagSet := tag != (CellTag{})
	setPixel := func(px, py int) {
		if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
			return
		}
		c.dc.Set(px, py)
		if tagSet {
			c.pixelTags.set(px, py, tag)
		}
	}
	for t := 0; t <= wingLen; t++ {
		setPixel(tipX+int(math.Round(lx*float64(t))), tipY+int(math.Round(ly*float64(t))))
		setPixel(tipX+int(math.Round(rx*float64(t))), tipY+int(math.Round(ry*float64(t))))
	}
}

// DrawEllipseDotted traces an ellipse defined by orbital elements. Dotted:
// every `stride`th sample is plotted. stride=1 gives a solid curve.
//
// This and DrawEllipseOffsetDotted[Colored] keep the pre-ADR-0042 fixed-count
// index-stride sampling and are no longer on any screen's draw path — the
// trajectory renderers all go through the adaptive helpers above. Reach for
// DrawEllipseOffsetFarSideDashed / DrawEllipseOffsetOccluded for anything the
// player zooms.
// Points are assumed to live in the system primary's inertial frame
// (PositionAtTrueAnomaly output), which is correct for heliocentric
// body orbits. For spacecraft orbiting a non-primary body, use
// DrawEllipseOffsetDotted to translate into the system frame.
func (c *Canvas) DrawEllipseDotted(el orbital.Elements, samples int, stride int) {
	c.DrawEllipseOffsetDotted(el, orbital.Vec3{}, samples, stride)
}

// DrawEllipseOffsetDotted traces an ellipse with every point translated
// by `offset` before plotting. Used for the vessel orbit around a non-
// primary body (Earth in Sol view): the offset is Earth's heliocentric
// position, so the ellipse is drawn in the same system frame as the
// rest of the canvas.
func (c *Canvas) DrawEllipseOffsetDotted(el orbital.Elements, offset orbital.Vec3, samples int, stride int) {
	if samples < 16 {
		samples = 16
	}
	if stride < 1 {
		stride = 1
	}
	for i := 0; i < samples; i++ {
		if i%stride != 0 {
			continue
		}
		nu := 2 * math.Pi * float64(i) / float64(samples)
		c.Plot(offset.Add(orbital.PositionAtTrueAnomaly(el, nu)))
	}
}

// DrawEllipseOffsetDottedColored traces a dotted ellipse like
// DrawEllipseOffsetDotted but tags each plotted pixel with the given
// color. v0.6.1: used to color the live vessel orbit and each
// post-maneuver leg distinctly so the player can read which orbit
// belongs to which planted burn.
func (c *Canvas) DrawEllipseOffsetDottedColored(el orbital.Elements, offset orbital.Vec3, samples int, stride int, color lipgloss.Color) {
	if samples < 16 {
		samples = 16
	}
	if stride < 1 {
		stride = 1
	}
	for i := 0; i < samples; i++ {
		if i%stride != 0 {
			continue
		}
		nu := 2 * math.Pi * float64(i) / float64(samples)
		c.PlotColored(offset.Add(orbital.PositionAtTrueAnomaly(el, nu)), color)
	}
}

// String renders the canvas as a multi-line braille string, trimmed to
// the configured cell dimensions. Pads short rows with spaces so the
// rectangular shape is preserved (lipgloss borders need uniform width).
//
// Per-pixel tags (set by FillColoredDisk / RingColoredOutline /
// PlotColored) drive cell-level coloring: each terminal cell's color
// is the most-frequent tag among its 8 (= 2×4) pixels. Cells whose
// pixels are all untagged render in the default terminal color.
// Pre-v0.5.10 used cell-rectangle tagging which over-painted nearby
// content (orbit lines, craft glyph) with the body's color.
func (c *Canvas) String() string {
	rows := c.dc.Rows(0, 0, c.pxW, c.pxH)
	if c.pixelTags.count() == 0 && len(c.cellOverlays) == 0 {
		return c.joinRows(rows)
	}
	// Aggregate tags per cell: for each tagged pixel, accumulate a
	// per-color count in the cell that contains it. Highest count
	// wins. Ties break deterministically on the color string so the
	// chosen color is stable frame-to-frame — Go's map iteration
	// order is randomized per range, which used to cause visible
	// flicker on textured bodies (v0.7.2.1+) where many cells legit-
	// imately have multi-color pixel sets. Pre-v0.7.2.1 there were
	// no multi-color cells in practice so the latent bug never
	// surfaced.
	cellColor := make(map[[2]int]lipgloss.Color)
	cellCounts := make(map[[2]int]map[lipgloss.Color]int)
	c.pixelTags.each(func(px, py int, tag CellTag) {
		if tag.Color == "" {
			return
		}
		key := [2]int{px / 2, py / 4}
		if cellCounts[key] == nil {
			cellCounts[key] = make(map[lipgloss.Color]int)
		}
		cellCounts[key][tag.Color]++
	})
	for key, counts := range cellCounts {
		cellColor[key] = pickDominantColor(counts)
	}
	var b strings.Builder
	// #364: run-length coalescing. Profiling (go test -cpuprofile on an
	// idle-frame render benchmark) showed the per-CELL
	// lipgloss.NewStyle().Foreground(fg).Render(string(ch)) call below
	// was the single most expensive line in the whole render path —
	// every one of a body disk's ~1000+ colored cells paid the full
	// Style.Render() machinery (tab conversion, word-wrap, border,
	// alignment, ansi/uniseg width scanning) independently, even though
	// a filled disk is mostly long horizontal runs of the SAME color.
	// Style.Render() is only ever called once per contiguous run of
	// identically-styled cells now — same bytes out, far fewer calls.
	var runBuf []rune
	var runFg lipgloss.TerminalColor
	var runStyled bool
	flushRun := func() {
		if len(runBuf) == 0 {
			return
		}
		if runStyled {
			b.WriteString(lipgloss.NewStyle().Foreground(runFg).Render(string(runBuf)))
		} else {
			for _, r := range runBuf {
				b.WriteRune(r)
			}
		}
		runBuf = runBuf[:0]
	}
	for i := 0; i < c.rows; i++ {
		var line string
		if i < len(rows) {
			line = rows[i]
		}
		// Colorize per-cell, padding short lines with spaces.
		// Cell overlays (v0.5.12) replace the drawille char with a
		// specific glyph at the same cell — used for body-identity
		// markers (☉ ◉ ● ○).
		runes := []rune(line)
		for x := 0; x < c.cols; x++ {
			var ch rune = ' '
			if x < len(runes) {
				ch = runes[x]
			}
			if overlay, ok := c.cellOverlays[[2]int{x, i}]; ok {
				ch = overlay
			}
			color, hasColor := cellColor[[2]int{x, i}]
			var fg lipgloss.TerminalColor = color
			// A pinned overlay color (SetCellLabelColored) wins over the
			// pixelTag-derived cell color so labels render in the theme
			// palette even on untagged background cells.
			if oc, ok := c.cellOverlayColors[[2]int{x, i}]; ok {
				fg, hasColor = oc, true
			}
			styled := hasColor && ch != ' '
			if styled != runStyled || (styled && fg != runFg) {
				flushRun()
				runStyled, runFg = styled, fg
			}
			runBuf = append(runBuf, ch)
		}
		flushRun()
		if i < c.rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// CountColor reports how many terminal cells resolve to `color` as their
// dominant per-cell colour — the same majority rule String() uses to
// paint each cell. Intended for render assertions (e.g. "the orbit
// ellipse lands on enough cells to read as a line, not just a marker").
func (c *Canvas) CountColor(color lipgloss.Color) int {
	cellCounts := make(map[[2]int]map[lipgloss.Color]int)
	c.pixelTags.each(func(px, py int, tag CellTag) {
		if tag.Color == "" {
			return
		}
		key := [2]int{px / 2, py / 4}
		if cellCounts[key] == nil {
			cellCounts[key] = make(map[lipgloss.Color]int)
		}
		cellCounts[key][tag.Color]++
	})
	n := 0
	for _, counts := range cellCounts {
		if pickDominantColor(counts) == color {
			n++
		}
	}
	return n
}

// CountOverlayColor reports how many cells carry the given pinned overlay
// color (SetCellOverlayColored / SetCellLabelColored) — the sibling of
// CountColor for the overlay-glyph channel, which paints independently of
// the pixelTag majority (see SetCellOverlayColored's doc comment). An
// untagged marker glyph (drawMarker called with an empty CellTag — the
// apsis / node / closest-approach markers) never touches pixelTags, so
// CountColor can't see it; this is the assertion callers need instead to
// distinguish e.g. a dimmed MarkerCounterfactual glyph from its bright
// MarkerNominal counterpart.
func (c *Canvas) CountOverlayColor(color lipgloss.Color) int {
	n := 0
	for _, oc := range c.cellOverlayColors {
		if oc == color {
			n++
		}
	}
	return n
}

// joinRows is the uncolored fast path used when no color regions are
// registered.
func (c *Canvas) joinRows(rows []string) string {
	var b strings.Builder
	for i := 0; i < c.rows; i++ {
		var line string
		if i < len(rows) {
			line = rows[i]
		}
		runeCount := 0
		for range line {
			runeCount++
		}
		if runeCount < c.cols {
			line += strings.Repeat(" ", c.cols-runeCount)
		}
		b.WriteString(line)
		if i < c.rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
