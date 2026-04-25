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
type Canvas struct {
	cols, rows int          // terminal cells
	pxW, pxH   int          // pixel grid (cols*2, rows*4)
	centerW    orbital.Vec3 // world coord at pixel center
	scale      float64      // pixels per meter
	dc         drawille.Canvas

	// pixelTags maps a pixel coord (px, py) → its render color. Set by
	// the *Colored* draw helpers (FillColoredDisk, RingColoredOutline);
	// the plain Plot / FillDisk / RingOutline / DrawEllipse / PlotArrow
	// helpers leave pixels untagged. At String() time, each terminal
	// cell picks its color from the most-common tag among its 2×4
	// pixels; cells with no tagged pixels render in the default
	// terminal color.
	//
	// v0.5.10+: replaces the v0.5.3 cell-rectangle approach, which
	// colored a body's entire bounding box of cells — bleeding into
	// orbit lines, craft glyphs, and apo/peri markers near a body.
	// Per-pixel tagging keeps body color confined to the body's own
	// pixels.
	pixelTags map[[2]int]lipgloss.Color

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
		dc:    drawille.NewCanvas(),
	}
}

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
	if pxRadius < 1 {
		pxRadius = 1
	}
	cx, cy, _ := c.Project(center)
	r2 := pxRadius * pxRadius
	if c.pixelTags == nil {
		c.pixelTags = make(map[[2]int]lipgloss.Color)
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
			c.pixelTags[[2]int{px, py}] = color
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
		c.pixelTags = make(map[[2]int]lipgloss.Color)
	}
	for i := 0; i < samples; i++ {
		theta := 2 * math.Pi * float64(i) / float64(samples)
		px := cx + int(math.Round(float64(pxRadius)*math.Cos(theta)))
		py := cy + int(math.Round(float64(pxRadius)*math.Sin(theta)))
		if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
			continue
		}
		c.dc.Set(px, py)
		c.pixelTags[[2]int{px, py}] = color
	}
}

// PlotColored sets a single pixel and tags it with the given color.
// Used by callers that want a tagged dot (currently unused — Plot
// stays untagged, which is what most callers want for orbit lines
// and trail samples).
func (c *Canvas) PlotColored(w orbital.Vec3, color lipgloss.Color) {
	if px, py, ok := c.Project(w); ok {
		c.dc.Set(px, py)
		if c.pixelTags == nil {
			c.pixelTags = make(map[[2]int]lipgloss.Color)
		}
		c.pixelTags[[2]int{px, py}] = color
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
	px := int(math.Round(rel.X*c.scale)) + c.pxW/2
	py := c.pxH/2 - int(math.Round(rel.Y*c.scale))
	if px < 0 || px >= c.pxW || py < 0 || py >= c.pxH {
		return px, py, false
	}
	return px, py, true
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
	for t := 0; t <= wingLen; t++ {
		plx := tipX + int(math.Round(lx*float64(t)))
		ply := tipY + int(math.Round(ly*float64(t)))
		if plx >= 0 && plx < c.pxW && ply >= 0 && ply < c.pxH {
			c.dc.Set(plx, ply)
		}
		prx := tipX + int(math.Round(rx*float64(t)))
		pry := tipY + int(math.Round(ry*float64(t)))
		if prx >= 0 && prx < c.pxW && pry >= 0 && pry < c.pxH {
			c.dc.Set(prx, pry)
		}
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
	for coord, color := range c.pixelTags {
		cellX, cellY := coord[0]/2, coord[1]/4
		key := [2]int{cellX, cellY}
		if cellCounts[key] == nil {
			cellCounts[key] = make(map[lipgloss.Color]int)
		}
		cellCounts[key][color]++
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
