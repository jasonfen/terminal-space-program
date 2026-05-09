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
	ColorNavballSky    = lipgloss.Color("#3A6FA8") // upper-hemisphere sky (classic ADI blue)
	ColorNavballGround = lipgloss.Color("#D87A3C") // lower-hemisphere ground (classic ADI orange)
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

// brailleBitForDot maps an in-cell sub-pixel position (sx ∈ {0,1},
// sy ∈ {0..3}) to the corresponding braille pattern bit. Standard
// drawille / Unicode braille encoding (U+28xx).
//
//	(0,0) 0x01   (1,0) 0x08
//	(0,1) 0x02   (1,1) 0x10
//	(0,2) 0x04   (1,2) 0x20
//	(0,3) 0x40   (1,3) 0x80
var brailleBitForDot = [2][4]rune{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
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
// Markers that fall outside the disk are skipped.
//
// v0.9.5: braille sub-pixel rendering — each terminal cell contains
// a 2×4 grid of braille dots (square in physical screen space, since
// terminal cells are ≈1×2). For an N-cell-wide × M-cell-tall region
// the dot grid is 2N×4M, with disk pxRadius = min(N, 2M) so the disk
// is genuinely circular on screen. Per cell: sample all 8 sub-pixels,
// build the braille pattern from in-disk dots, and color the cell
// with the dominant texture (grid wins ties so lines stay visible).
//
// Per-pixel (dx, dy) → (lat, lon) projection reuses
// projectPixelToLatLon; only the painter loop differs from the
// body-rendering path.
func NavballString(cols, rows int, subLatDeg, subLonDeg float64, markers []NavballMarker) string {
	if cols < 4 || rows < 2 {
		return ""
	}
	dotsW := cols * 2
	dotsH := rows * 4
	dotCx := dotsW / 2
	dotCy := dotsH / 2
	pxR := dotCx
	if dotCy < pxR {
		pxR = dotCy
	}
	if pxR < 4 {
		pxR = 4
	}

	skyStyle := lipgloss.NewStyle().Foreground(ColorNavballSky)
	groundStyle := lipgloss.NewStyle().Foreground(ColorNavballGround)
	gridStyle := lipgloss.NewStyle().Foreground(ColorNavballGrid)

	cells := make([][]string, rows)
	for row := 0; row < rows; row++ {
		cells[row] = make([]string, cols)
		for col := 0; col < cols; col++ {
			var pattern rune
			var skyCount, groundCount, gridCount int
			for sx := 0; sx < 2; sx++ {
				for sy := 0; sy < 4; sy++ {
					dx := col*2 + sx - dotCx
					dy := row*4 + sy - dotCy
					if dx*dx+dy*dy > pxR*pxR {
						continue
					}
					lat, lon, ok := projectPixelToLatLon(dx, dy, pxR, subLatDeg, subLonDeg)
					if !ok {
						continue
					}
					pattern |= brailleBitForDot[sx][sy]
					switch navballCell(lat, lon) {
					case navballGrid:
						gridCount++
					case navballSky:
						skyCount++
					default:
						groundCount++
					}
				}
			}
			if pattern == 0 {
				cells[row][col] = " "
				continue
			}
			ch := string(rune(0x2800) + pattern)
			var style lipgloss.Style
			switch {
			case gridCount > 0:
				style = gridStyle
			case skyCount >= groundCount:
				style = skyStyle
			default:
				style = groundStyle
			}
			cells[row][col] = style.Render(ch)
		}
	}

	// Render markers in two passes so front markers overwrite back
	// markers at coincident cells (e.g. prograde at the disk centre
	// hides retrograde when sub-observer points at prograde — KSP
	// behavior).
	paint := func(m NavballMarker, dimmed bool) {
		mdx, mdy, _ := projectLatLonToPixel(m.LatDeg, m.LonDeg, pxR, subLatDeg, subLonDeg)
		// Marker positions are in dot units; map to the containing
		// cell. (dotCx + mdx) ∈ [0, dotsW); divide by 2 dots/cell.
		col := (dotCx + mdx) / 2
		row := (dotCy + mdy) / 4
		if col < 0 || col >= cols || row < 0 || row >= rows {
			return
		}
		if cells[row][col] == " " {
			return
		}
		glyph := string(m.Glyph)
		if m.Glyph == 0 {
			glyph = "•"
		}
		style := lipgloss.NewStyle().Foreground(m.Color)
		if dimmed {
			style = style.Faint(true)
		}
		cells[row][col] = style.Render(glyph)
	}
	for _, m := range markers {
		_, _, front := projectLatLonToPixel(m.LatDeg, m.LonDeg, pxR, subLatDeg, subLonDeg)
		if front {
			continue
		}
		paint(m, true)
	}
	for _, m := range markers {
		_, _, front := projectLatLonToPixel(m.LatDeg, m.LonDeg, pxR, subLatDeg, subLonDeg)
		if !front {
			continue
		}
		paint(m, false)
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
