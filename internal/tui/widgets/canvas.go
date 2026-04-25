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

	// colorRegions records cell-coordinate rectangles to colorize at
	// String() time. v0.5.3+ — used by the orbit renderer to color
	// body disks by palette without per-pixel color tagging.
	// Last-written wins on overlap.
	colorRegions []colorRegion
}

type colorRegion struct {
	cellX, cellY, w, h int
	color              lipgloss.Color
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

// Clear wipes the drawille buffer and the per-cell color regions.
// Call at the start of every frame.
func (c *Canvas) Clear() {
	c.dc.Clear()
	c.colorRegions = c.colorRegions[:0]
}

// AddColoredDisk records a cell-region tag for the disk that
// FillDisk(center, pxRadius) would draw. Use it after a body's
// FillDisk to colorize the body's footprint at String() time.
// Translates pixel-radius to cell-radius via the 2×4 braille grid
// (rounded up so single-pixel disks still hit a full cell).
func (c *Canvas) AddColoredDisk(center orbital.Vec3, pxRadius int, color lipgloss.Color) {
	cx, cy, _ := c.Project(center)
	// Body cell footprint: convert pixel radius → cell radius. Add 1
	// to ensure the disk's edge cells are included (drawille cells
	// are 2 px wide × 4 px tall — round up generously so the rim of
	// a body's pixels falls inside the colorized region).
	cellRX := pxRadius/2 + 1
	cellRY := pxRadius/4 + 1
	cellX := cx/2 - cellRX
	cellY := cy/4 - cellRY
	w := 2*cellRX + 1
	h := 2*cellRY + 1
	c.colorRegions = append(c.colorRegions, colorRegion{
		cellX: cellX,
		cellY: cellY,
		w:     w,
		h:     h,
		color: color,
	})
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
// When color regions are set (via AddColoredDisk), each cell inside a
// region is wrapped in a lipgloss foreground color. Last-written
// region wins on overlap; cells not in any region render uncolored.
// Skips coloring blank cells so per-cell escape sequences don't
// inflate output for empty space.
func (c *Canvas) String() string {
	rows := c.dc.Rows(0, 0, c.pxW, c.pxH)
	if len(c.colorRegions) == 0 {
		return c.joinRows(rows)
	}
	// Build per-cell color map (last-write-wins on overlap). Sparse —
	// only set for cells inside any region.
	cellColor := make(map[[2]int]lipgloss.Color)
	for _, r := range c.colorRegions {
		for dy := 0; dy < r.h; dy++ {
			y := r.cellY + dy
			if y < 0 || y >= c.rows {
				continue
			}
			for dx := 0; dx < r.w; dx++ {
				x := r.cellX + dx
				if x < 0 || x >= c.cols {
					continue
				}
				cellColor[[2]int{x, y}] = r.color
			}
		}
	}
	var b strings.Builder
	for i := 0; i < c.rows; i++ {
		var line string
		if i < len(rows) {
			line = rows[i]
		}
		// Colorize per-cell, padding short lines with spaces.
		runes := []rune(line)
		for x := 0; x < c.cols; x++ {
			var ch rune = ' '
			if x < len(runes) {
				ch = runes[x]
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
