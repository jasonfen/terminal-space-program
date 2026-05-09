package render

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Navball palette. Sky / ground hemispheres mirror the KSP convention
// (blue upper, brown lower) so the disk reads as an attitude indicator
// even before markers land. Tints chosen to fit the existing UI tier
// colors — saturated enough to read at low cell counts.
const (
	ColorNavballSky    = lipgloss.Color("#3A6FA8") // upper-hemisphere sky
	ColorNavballGround = lipgloss.Color("#7A5C3A") // lower-hemisphere ground
	ColorNavballGrid   = lipgloss.Color("#C8C8C8") // grid + equator
)

// NavballString paints a navball into a cols×rows cell grid as a
// multi-line lipgloss-styled string. The sphere is rendered with a
// horizon split (sky upper, ground lower) plus a lat/lon grid at 30°
// spacing. (subLatDeg, subLonDeg) is the sub-observer point — the
// (lat, lon) on the sphere that sits at the visible disk centre.
// For the navball this corresponds to the craft's nose direction
// expressed in the ball's reference frame.
//
// v0.9.5 spike: cell-resolution rendering, no braille subcells, no
// markers, no frame derivation. Validates that projectPixelToLatLon
// reuses cleanly for the navball's frame-relative sphere. The
// per-pixel projection is identical to the body-rendering path; only
// the painter loop and texture differ.
//
// Cells are assumed to be ≈2× taller than wide (typical terminal),
// so dy is halved before feeding into the projection so the disk
// reads as roughly circular on screen.
func NavballString(cols, rows int, subLatDeg, subLonDeg float64) string {
	if cols < 4 || rows < 4 {
		return ""
	}
	cx := cols / 2
	cy := rows / 2
	// Disk pixel-radius: smaller of half-width / half-height-in-cell-
	// units after the 0.5 cell-aspect compensation.
	pxR := cx
	if doubleY := cy * 2; doubleY < pxR {
		pxR = doubleY
	}
	if pxR < 2 {
		pxR = 2
	}

	// Foreground-colored full-block glyph keeps the disk visible
	// even when lipgloss strips colors (non-TTY tests, dumb terminals).
	const blockGlyph = "█"
	skyStyle := lipgloss.NewStyle().Foreground(ColorNavballSky)
	groundStyle := lipgloss.NewStyle().Foreground(ColorNavballGround)
	gridStyle := lipgloss.NewStyle().Foreground(ColorNavballGrid)

	var lines []string
	for row := 0; row < rows; row++ {
		var line strings.Builder
		for col := 0; col < cols; col++ {
			dx := col - cx
			// Cell-aspect compensation: a cell is ~2× taller than
			// wide, so one row of dy corresponds to two pixels of
			// vertical extent. Multiply dy by 2 inside the projection
			// so the rendered disk reads as roughly circular.
			dy := (row - cy) * 2
			if dx*dx+dy*dy > pxR*pxR {
				line.WriteString(" ")
				continue
			}
			lat, lon, ok := projectPixelToLatLon(dx, dy, pxR, subLatDeg, subLonDeg)
			if !ok {
				line.WriteString(" ")
				continue
			}
			switch navballCell(lat, lon) {
			case navballGrid:
				line.WriteString(gridStyle.Render(blockGlyph))
			case navballSky:
				line.WriteString(skyStyle.Render(blockGlyph))
			default:
				line.WriteString(groundStyle.Render(blockGlyph))
			}
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

// navballCellKind picks which palette slot a (lat, lon) on the
// navball maps to: sky upper, ground lower, or grid (equator + lat
// lines at every 30° + lon lines at every 30°).
type navballCellKind int

const (
	navballSky navballCellKind = iota
	navballGround
	navballGrid
)

// gridSpacingDeg is the lat/lon grid period in degrees. 30° gives 6
// equatorial-band lon lines + 5 lat lines (excluding poles), enough
// for the player to read attitude from the grid alone before markers
// land.
const gridSpacingDeg = 30.0

// gridHalfWidthDeg is the lat-or-lon distance within which a pixel
// snaps to the grid color instead of the hemisphere fill. ~2.5° at
// the equator works out to roughly one cell of grid line at the
// 12-cell navball size. Tightened from 3° during the spike because
// adjacent grid lines were occasionally collapsing into a single
// thick band near the limb where the projection compresses
// longitude.
const gridHalfWidthDeg = 2.5

func navballCell(lat, lon float64) navballCellKind {
	for tick := -90.0; tick <= 90.0+1e-9; tick += gridSpacingDeg {
		if math.Abs(lat-tick) < gridHalfWidthDeg {
			return navballGrid
		}
	}
	for tick := -180.0; tick < 180.0; tick += gridSpacingDeg {
		// Wrap-aware comparison so the lon = ±180 meridian draws
		// as a single line.
		d := lon - tick
		for d > 180 {
			d -= 360
		}
		for d < -180 {
			d += 360
		}
		if math.Abs(d) < gridHalfWidthDeg {
			return navballGrid
		}
	}
	if lat >= 0 {
		return navballSky
	}
	return navballGround
}
