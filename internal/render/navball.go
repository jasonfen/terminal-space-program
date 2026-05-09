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

	// Marker colors. Prograde / retrograde mirror KSP's yellow; normal
	// vectors are pink (KSP magenta-ish); radial markers are cyan;
	// target markers are pink-purple to read distinctly against the
	// orbit-frame markers when both render in target mode.
	ColorNavballMarkerPrograde   = lipgloss.Color("#E0D040") // prograde / retrograde yellow
	ColorNavballMarkerNormal     = lipgloss.Color("#D08CC8") // normal+ / normal- pink
	ColorNavballMarkerRadial     = lipgloss.Color("#5CC8D0") // radial+ / radial- cyan
	ColorNavballMarkerTarget     = lipgloss.Color("#C880E8") // target / anti-target purple
	ColorNavballMarkerNoseFront  = lipgloss.Color("#FFFFFF") // craft nose, front hemisphere
)

// NavballMarker is a glyph drawn at a fixed (lat, lon) on the navball
// sphere in the active basis. The painter projects (LatDeg, LonDeg)
// through the sub-observer point to a cell offset; if the marker lies
// in the front hemisphere it draws Glyph in Color, otherwise the
// marker is skipped (back-hemisphere dimming + limb bracketing are
// future polish).
//
// v0.9.5+.
type NavballMarker struct {
	LatDeg, LonDeg float64
	Glyph          rune
	Color          lipgloss.Color
}

// projectLatLonToPixel is the forward orthographic projection
// counterpart of projectPixelToLatLon. Given a sub-observer point at
// (subLatDeg, subLonDeg) and a target (latDeg, lonDeg), it returns
// the pixel offset (dx, dy) on the disk — same coordinate convention
// as the inverse (dx east+, dy screen-down, i.e. ny = -dy/pxRadius).
//
// front=true when the point is on the visible hemisphere (camera-side
// z > 0); false when it's behind the ball. Callers gate front-only
// rendering on this flag during the v0.9.5 spike + initial commit.
//
// Math: same rotation as projectPixelToLatLon, transposed (the body→
// view rotation is orthogonal, so its inverse is its transpose).
func projectLatLonToPixel(latDeg, lonDeg float64, pxRadius int, subLatDeg, subLonDeg float64) (dx, dy int, front bool) {
	if pxRadius < 1 {
		return 0, 0, false
	}
	phi := latDeg * math.Pi / 180.0
	lam := lonDeg * math.Pi / 180.0
	bodyX := math.Cos(phi) * math.Cos(lam)
	bodyY := math.Cos(phi) * math.Sin(lam)
	bodyZ := math.Sin(phi)

	phi0 := subLatDeg * math.Pi / 180.0
	lam0 := subLonDeg * math.Pi / 180.0
	sP, cP := math.Sin(phi0), math.Cos(phi0)
	sL, cL := math.Sin(lam0), math.Cos(lam0)

	// Inverse rotation: (bodyX, bodyY, bodyZ) → (nx, ny, z) where the
	// sub-observer point lands at (0, 0, +1) and z > 0 is the visible
	// hemisphere.
	nx := -sL*bodyX + cL*bodyY
	ny := -sP*cL*bodyX - sP*sL*bodyY + cP*bodyZ
	z := cP*cL*bodyX + cP*sL*bodyY + sP*bodyZ

	dx = int(math.Round(nx * float64(pxRadius)))
	dy = int(math.Round(-ny * float64(pxRadius)))
	return dx, dy, z > 0
}

// NavballString paints a navball into a cols×rows cell grid as a
// multi-line lipgloss-styled string. The sphere is rendered with a
// horizon split (sky upper, ground lower) plus a lat/lon grid at 30°
// spacing, and any front-hemisphere markers are overlaid as glyphs.
// (subLatDeg, subLonDeg) is the sub-observer point — the (lat, lon)
// on the sphere that sits at the visible disk centre. For the
// navball this corresponds to the craft's nose direction expressed
// in the ball's reference frame.
//
// markers may be nil. Each marker is projected via
// projectLatLonToPixel; only markers in the front hemisphere render.
// Markers that fall outside the disk (e.g. due to rounding at the
// limb) are skipped.
//
// v0.9.5: cell-resolution rendering — one full-block char per cell,
// no braille subcells. The per-pixel projection is identical to the
// body-rendering path; only the painter loop and texture differ.
// Cells are assumed to be ≈2× taller than wide (typical terminal),
// so dy is halved before feeding into the projection so the disk
// reads as roughly circular on screen.
func NavballString(cols, rows int, subLatDeg, subLonDeg float64, markers []NavballMarker) string {
	if cols < 4 || rows < 4 {
		return ""
	}
	cx := cols / 2
	cy := rows / 2
	pxR := cx
	if doubleY := cy * 2; doubleY < pxR {
		pxR = doubleY
	}
	if pxR < 2 {
		pxR = 2
	}

	const blockGlyph = "█"
	skyStyle := lipgloss.NewStyle().Foreground(ColorNavballSky)
	groundStyle := lipgloss.NewStyle().Foreground(ColorNavballGround)
	gridStyle := lipgloss.NewStyle().Foreground(ColorNavballGrid)

	// Render the sphere into a 2-D cell grid first so marker overlays
	// can rewrite individual cells before the lines are joined.
	cells := make([][]string, rows)
	for row := 0; row < rows; row++ {
		cells[row] = make([]string, cols)
		for col := 0; col < cols; col++ {
			dx := col - cx
			dy := (row - cy) * 2
			if dx*dx+dy*dy > pxR*pxR {
				cells[row][col] = " "
				continue
			}
			lat, lon, ok := projectPixelToLatLon(dx, dy, pxR, subLatDeg, subLonDeg)
			if !ok {
				cells[row][col] = " "
				continue
			}
			switch navballCell(lat, lon) {
			case navballGrid:
				cells[row][col] = gridStyle.Render(blockGlyph)
			case navballSky:
				cells[row][col] = skyStyle.Render(blockGlyph)
			default:
				cells[row][col] = groundStyle.Render(blockGlyph)
			}
		}
	}

	for _, m := range markers {
		mdx, mdy, front := projectLatLonToPixel(m.LatDeg, m.LonDeg, pxR, subLatDeg, subLonDeg)
		if !front {
			continue
		}
		// Map projection-pixel (mdx, mdy) back to cell coords. dy is
		// in projection units (already doubled for cell aspect), so
		// halve to recover the cell row.
		col := cx + mdx
		row := cy + mdy/2
		if col < 0 || col >= cols || row < 0 || row >= rows {
			continue
		}
		// Skip markers that landed on a transparent cell (outside the
		// disk after rounding) — keeps marker glyphs anchored visually
		// to the disk surface.
		if cells[row][col] == " " {
			continue
		}
		glyph := string(m.Glyph)
		if m.Glyph == 0 {
			glyph = "•"
		}
		cells[row][col] = lipgloss.NewStyle().Foreground(m.Color).Render(glyph)
	}

	lines := make([]string, rows)
	for row := 0; row < rows; row++ {
		lines[row] = strings.Join(cells[row], "")
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
