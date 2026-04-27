// Package widgets provides drawille-backed canvas + lipgloss HUD helpers
// shared by all screens.
package widgets

import (
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
	NodeIdx  int  // 0 = no node; otherwise 1 + Nodes-slice index
	IsVessel bool
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

	// pixelTags maps a pixel coord (px, py) → CellTag. Set by the
	// *Colored* / *Tagged* draw helpers; plain Plot / FillDisk /
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
	pixelTags map[[2]int]CellTag

	// cellOverlays maps a cell coord → a Unicode glyph that replaces
	// the drawille-derived char at String() time. v0.5.12+ — used by
	// the orbit renderer to overlay body-identity glyphs (☉ ◉ ● ○)
	// on top of the underlying braille so different body types read
	// distinctly even at small pixel-radius. Overlay color comes from
	// the cell's pixelTags as usual.
	cellOverlays map[[2]int]rune
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
	return &Canvas{
		cols:  cols,
		rows:  rows,
		pxW:   cols * 2,
		pxH:   rows * 4,
		scale: 1,
		basis: DefaultBasis(),
		dc:    drawille.NewCanvas(),
	}
}

// SetBasis swaps the projection basis. Called per-frame by render
// code that wants a non-equatorial view; pass DefaultBasis() to
// restore. v0.6.4+.
func (c *Canvas) SetBasis(b Basis) { c.basis = b }

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
}

// Clear wipes the drawille buffer, per-pixel color tags, and cell
// overlays. Call at the start of every frame.
func (c *Canvas) Clear() {
	c.dc.Clear()
	c.pixelTags = nil
	c.cellOverlays = nil
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
	r2 := pxRadius * pxRadius
	if c.pixelTags == nil {
		c.pixelTags = make(map[[2]int]CellTag)
	}
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
			c.pixelTags[[2]int{px, py}] = tag
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
	if c.pixelTags == nil {
		c.pixelTags = make(map[[2]int]CellTag)
	}
	for i := 0; i < samples; i++ {
		theta := 2 * math.Pi * float64(i) / float64(samples)
		px := cx + int(math.Round(float64(pxRadius)*math.Cos(theta)))
		py := cy + int(math.Round(float64(pxRadius)*math.Sin(theta)))
		if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
			continue
		}
		c.dc.Set(px, py)
		c.pixelTags[[2]int{px, py}] = tag
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
		if c.pixelTags == nil {
			c.pixelTags = make(map[[2]int]CellTag)
		}
		c.pixelTags[[2]int{px, py}] = tag
	}
}

// Cols / Rows expose the configured terminal cell dimensions.
func (c *Canvas) Cols() int { return c.cols }
func (c *Canvas) Rows() int { return c.rows }

// Center sets the world coordinate that maps to the pixel grid center.
func (c *Canvas) Center(w orbital.Vec3) { c.centerW = w }

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

// HitAt aggregates the CellTag fields recorded on the 2×4 pixels
// of the terminal cell at (col, row). Returns the most-common
// non-empty BodyID among those pixels (with first-seen as the tie-
// breaker). NodeIdx and IsVessel resolve the same way (most common
// non-zero / true wins). Color falls out of the existing String()
// aggregation and is not returned here. v0.6.4+: paired with the
// app's MouseMsg dispatch so a click resolves to "what sim object
// is this cell rendering?"
//
// (col, row) outside the canvas → zero-value CellTag (no hit).
// Cells whose pixel set is entirely untagged also return zero —
// the caller treats this as "click on empty canvas" and may then
// Unproject for an in-orbit-plane world coord.
func (c *Canvas) HitAt(col, row int) CellTag {
	if col < 0 || col >= c.cols || row < 0 || row >= c.rows {
		return CellTag{}
	}
	if len(c.pixelTags) == 0 {
		return CellTag{}
	}
	bodyCounts := map[string]int{}
	nodeCounts := map[int]int{}
	vesselCount := 0
	pxStart, pyStart := col*2, row*4
	for dx := 0; dx < 2; dx++ {
		for dy := 0; dy < 4; dy++ {
			tag, ok := c.pixelTags[[2]int{pxStart + dx, pyStart + dy}]
			if !ok {
				continue
			}
			if tag.BodyID != "" {
				bodyCounts[tag.BodyID]++
			}
			if tag.NodeIdx != 0 {
				nodeCounts[tag.NodeIdx]++
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
	if best, n := mostCommonString(bodyCounts); n > 0 {
		hit.BodyID = best
	}
	if best, n := mostCommonInt(nodeCounts); n > 0 {
		hit.NodeIdx = best
	}
	return hit
}

func mostCommonString(counts map[string]int) (string, int) {
	var best string
	bestN := 0
	for k, n := range counts {
		if n > bestN {
			bestN = n
			best = k
		}
	}
	return best, bestN
}

func mostCommonInt(counts map[int]int) (int, int) {
	var best int
	bestN := 0
	for k, n := range counts {
		if n > bestN {
			bestN = n
			best = k
		}
	}
	return best, bestN
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

// DrawEllipseOffsetOccluded plots a dotted ellipse but skips any
// sample IsBehindBody-occluded by the supplied (bodyPos, bodyPxR).
// Same shape as DrawEllipseOffsetDotted; v0.6.4+ side-view variant.
// Pass an untagged stride to keep the existing colour helpers; this
// helper writes through PlotColored when `color` is non-empty,
// otherwise plain Plot.
func (c *Canvas) DrawEllipseOffsetOccluded(
	el orbital.Elements,
	offset orbital.Vec3,
	samples int,
	stride int,
	bodyPos orbital.Vec3,
	bodyPxR int,
	color lipgloss.Color,
) {
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
		p := offset.Add(orbital.PositionAtTrueAnomaly(el, nu))
		if c.IsBehindBody(p, bodyPos, bodyPxR) {
			continue
		}
		if color == "" {
			c.Plot(p)
		} else {
			c.PlotColored(p, color)
		}
	}
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
	if tagSet && c.pixelTags == nil {
		c.pixelTags = make(map[[2]int]CellTag)
	}
	setPixel := func(px, py int) {
		if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
			return
		}
		c.dc.Set(px, py)
		if tagSet {
			c.pixelTags[[2]int{px, py}] = tag
		}
	}
	for t := 0; t <= wingLen; t++ {
		setPixel(tipX+int(math.Round(lx*float64(t))), tipY+int(math.Round(ly*float64(t))))
		setPixel(tipX+int(math.Round(rx*float64(t))), tipY+int(math.Round(ry*float64(t))))
	}
}

// DrawEllipseDotted traces an ellipse defined by orbital elements. Dotted:
// every `stride`th sample is plotted. stride=1 gives a solid curve.
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
	if len(c.pixelTags) == 0 && len(c.cellOverlays) == 0 {
		return c.joinRows(rows)
	}
	// Aggregate tags per cell: for each tagged pixel, accumulate a
	// per-color count in the cell that contains it. Highest count
	// wins. Ties go to whichever color appeared first (map iteration
	// order is intentionally undefined; the tie-breaker is good enough
	// since collisions are rare).
	cellColor := make(map[[2]int]lipgloss.Color)
	cellCounts := make(map[[2]int]map[lipgloss.Color]int)
	for coord, tag := range c.pixelTags {
		if tag.Color == "" {
			continue
		}
		cellX, cellY := coord[0]/2, coord[1]/4
		key := [2]int{cellX, cellY}
		if cellCounts[key] == nil {
			cellCounts[key] = make(map[lipgloss.Color]int)
		}
		cellCounts[key][tag.Color]++
	}
	for key, counts := range cellCounts {
		var bestColor lipgloss.Color
		bestN := 0
		for color, n := range counts {
			if n > bestN {
				bestN = n
				bestColor = color
			}
		}
		cellColor[key] = bestColor
	}
	var b strings.Builder
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
			if hasColor && ch != ' ' {
				b.WriteString(lipgloss.NewStyle().Foreground(color).Render(string(ch)))
			} else {
				b.WriteRune(ch)
			}
		}
		if i < c.rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
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
