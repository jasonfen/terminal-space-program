package render

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Navball palette. v0.9.6-polish retuned this toward KSP's navball:
// a saturated sky-blue upper hemisphere over a tan/brown lower
// hemisphere (KSP uses brown ground, not the classic ADI orange),
// with a bright pale-tan horizon band so the equator reads as an
// explicit drawn line rather than just the blue/brown boundary.
const (
	ColorNavballSky    = lipgloss.Color("#2E74C0") // upper-hemisphere sky (KSP blue)
	ColorNavballGround = lipgloss.Color("#9C6B3F") // lower-hemisphere ground (KSP tan-brown)
	ColorNavballGrid   = lipgloss.Color("#C8C8C8") // structural labels (compass ticks)

	// Limb shading — darker hemisphere tints used on cells at the
	// disk edge (few in-disk dots) so the ball reads as a sphere
	// with depth, not a flat color disk. Roughly 60% brightness of
	// the parent hemisphere color.
	ColorNavballSkyEdge    = lipgloss.Color("#1C4A82") // darker sky for limb cells
	ColorNavballGroundEdge = lipgloss.Color("#5E3F26") // darker ground for limb cells

	// Horizon band — bright pale tan used on cells where sky/ground
	// dot counts are nearly balanced (the cell straddles the
	// equator). KSP draws a crisp horizon line; the brightness here
	// makes it pop against both the blue sky and brown ground.
	ColorNavballHorizon = lipgloss.Color("#E6D2A0") // bright horizon line

	// Grid tints — slightly brighter versions of each hemisphere,
	// used on cells whose dots fall on or near a 30° parallel /
	// meridian. Keeps the grid in-hemisphere (not white) so the
	// disk doesn't wash out — the 357937f bug was a single bright
	// grid color winning ties and turning the whole disk white +
	// flickery. These stay tonally adjacent to their hemisphere.
	ColorNavballSkyGrid    = lipgloss.Color("#5AA0E0") // brighter sky for grid-line cells
	ColorNavballGroundGrid = lipgloss.Color("#C2925A") // brighter ground for grid-line cells

	// Marker colors. Prograde / retrograde mirror KSP's yellow; normal
	// vectors are pink (KSP magenta-ish); radial markers are cyan;
	// target markers are pink-purple to read distinctly against the
	// orbit-frame markers when both render in target mode.
	ColorNavballMarkerPrograde  = lipgloss.Color("#E0D040") // prograde / retrograde yellow
	ColorNavballMarkerNormal    = lipgloss.Color("#D08CC8") // normal+ / normal- pink
	ColorNavballMarkerRadial    = lipgloss.Color("#5CC8D0") // radial+ / radial- cyan
	ColorNavballMarkerTarget    = lipgloss.Color("#C880E8") // target / anti-target purple
	ColorNavballMarkerNoseFront = lipgloss.Color("#FFFFFF") // craft nose, front hemisphere
)

// NavballMarker is a glyph drawn at a fixed (lat, lon) on the navball
// sphere in the active basis. The painter projects (LatDeg, LonDeg)
// through the sub-observer point to a cell offset; if the marker lies
// in the front hemisphere (or in the narrow limb dead zone) it draws
// Glyph in Color, otherwise the marker is skipped — the ball reads
// like a solid sphere with no antipodal bleed-through.
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
// z > -limbFrontEpsZ — the limb band counts as front so rim markers
// read solid and don't strobe on float noise); false when it's
// clearly behind the ball.
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
	// Limb dead-zone. A marker sitting exactly on the limb has z≈0,
	// so a bare `z > 0` test picks its front/back class from float
	// noise and strobes it bold↔faint every frame. This is not a rare
	// edge: in NavOrbit the orbit-normal markers ARE the basis pole
	// (EZ = r×v), and while SAS holds prograde the sub-observer is at
	// the equator, so the normal markers land precisely on the limb
	// with z genuinely ≈0 — the persistent "normal marker flickers
	// most" bug. Treat the whole |z| ≤ limbFrontEpsZ band as front:
	// a rim marker reads solid (KSP-style) and, crucially, its class
	// no longer flips on sub-noise. The band (~0.6° off the limb) is
	// far wider than the noise yet far below any clearly-front/back
	// marker's |z|, so non-limb markers are unaffected.
	return dx, dy, z > -limbFrontEpsZ
}

// limbFrontEpsZ is the camera-z half-width of the limb dead-zone in
// projectLatLonToPixel. ~0.02 ≈ sin(1.1°): markers within ~1° of the
// limb are classed front (solid at the rim) instead of letting the
// sign of float noise decide. Comfortably above the ~5e-5 z-noise a
// pole-coincident marker accrues from the lat/lon round trip, and
// well below the |z| of any marker that's unambiguously on one face.
const limbFrontEpsZ = 0.02

// isGridDot reports whether (lat, lon) sits on or within tolerance
// of a 30° navball grid line — a parallel every 30° of latitude
// (excluding the poles, where the line collapses to a point) or a
// meridian every 30° of longitude at |lat| ≤ 70° (above which the
// 12 meridians converge too densely to render distinctly).
//
// Tolerance is 2° in either coordinate, chosen so each grid line is
// roughly 1 cell-dot wide at pxR=12. The skip at |lat| > 70°
// prevents the pole cap from going all-grid as meridians converge.
func isGridDot(lat, lon float64) bool {
	const tol = 2.0
	const parallelStep = 30.0
	const meridianStep = 30.0
	const meridianLatCap = 70.0

	absLat := math.Abs(lat)
	if absLat <= 90-tol {
		latMod := math.Mod(absLat, parallelStep)
		if latMod > parallelStep/2 {
			latMod = parallelStep - latMod
		}
		if latMod <= tol {
			return true
		}
	}
	if absLat <= meridianLatCap {
		absLon := math.Abs(lon)
		lonMod := math.Mod(absLon, meridianStep)
		if lonMod > meridianStep/2 {
			lonMod = meridianStep - lonMod
		}
		if lonMod <= tol {
			return true
		}
	}
	return false
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
// projectLatLonToPixel; back-hemisphere markers render dimmed and
// markers that project outside the disk are clamped to the rim
// (KSP-style edge pinning) rather than skipped, so a marker never
// blinks on/off at the limb.
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
	// NB: (subLatDeg, subLonDeg) is expected to arrive already
	// jitter-stabilised by the caller's sticky dead-band
	// (OrbitView.stickyNavballSubObserver). NavballString itself does
	// NOT quantize: an earlier math.Round here "fixed" equator flicker
	// but converted sub-degree SAS dither into full 1° snap-dither,
	// jumping the whole disk + every marker a cell per frame. With a
	// stable input, continuous projection is flicker-free and the
	// rounding is both unnecessary and harmful.
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
	skyEdgeStyle := lipgloss.NewStyle().Foreground(ColorNavballSkyEdge)
	groundEdgeStyle := lipgloss.NewStyle().Foreground(ColorNavballGroundEdge)
	horizonStyle := lipgloss.NewStyle().Foreground(ColorNavballHorizon)
	skyGridStyle := lipgloss.NewStyle().Foreground(ColorNavballSkyGrid)
	groundGridStyle := lipgloss.NewStyle().Foreground(ColorNavballGroundGrid)

	// Pre-pass: project every in-disk dot once. The horizon line needs to
	// compare a dot with its neighbours, so the (lat, lon) field is
	// computed up front rather than inside the per-cell loop.
	type dotInfo struct {
		lat, lon float64
		in       bool
	}
	field := make([]dotInfo, dotsW*dotsH)
	for y := 0; y < dotsH; y++ {
		for x := 0; x < dotsW; x++ {
			dx := x - dotCx
			dy := y - dotCy
			if dx*dx+dy*dy > pxR*pxR {
				continue
			}
			// Navball's own local-north (toward +90° lat on the
			// abstract orientation sphere) is canvas-up by
			// construction — the ball is drawn flush with the
			// terminal cell grid and is not a tilted celestial
			// body. v0.11.2+ (ADR 0003): pass (0, 1) directly so
			// the navball texture pipeline stays orientation-
			// preserving and decoupled from any body's spin axis.
			lat, lon, ok := projectPixelToLatLon(dx, dy, pxR, subLatDeg, subLonDeg, 0, 1)
			if !ok {
				continue
			}
			field[y*dotsW+x] = dotInfo{lat: lat, lon: lon, in: true}
		}
	}

	// Horizon dots (G8 Q4, #507). The equator is drawn as a continuous
	// line that does not depend on which dot row of a cell it falls in:
	// wherever sky (lat >= 0) meets ground (lat < 0) between two adjacent
	// in-disk dots, the dot of the pair nearer the equator is a horizon dot,
	// and its cell is painted horizon (checked before the limb rule so the
	// line reaches the rim). The old rule (sky/ground dot counts within 1)
	// never fired when a level nose put the equator on a cell boundary.
	horizonDot := make([]bool, dotsW*dotsH)
	markPair := func(i, j int) {
		a, b := field[i], field[j]
		if !a.in || !b.in || (a.lat >= 0) == (b.lat >= 0) {
			return
		}
		if math.Abs(a.lat) <= math.Abs(b.lat) {
			horizonDot[i] = true
		} else {
			horizonDot[j] = true
		}
	}
	for y := 0; y < dotsH; y++ {
		for x := 0; x < dotsW; x++ {
			if x+1 < dotsW {
				markPair(y*dotsW+x, y*dotsW+x+1)
			}
			if y+1 < dotsH {
				markPair(y*dotsW+x, (y+1)*dotsW+x)
			}
		}
	}

	cells := make([][]string, rows)
	horizonCell := make([][]bool, rows)
	for row := 0; row < rows; row++ {
		cells[row] = make([]string, cols)
		horizonCell[row] = make([]bool, cols)
		for col := 0; col < cols; col++ {
			var pattern rune
			var skyCount, groundCount, gridCount int
			horizon := false
			for sx := 0; sx < 2; sx++ {
				for sy := 0; sy < 4; sy++ {
					idx := (row*4+sy)*dotsW + col*2 + sx
					d := field[idx]
					if !d.in {
						continue
					}
					pattern |= brailleBitForDot[sx][sy]
					if d.lat >= 0 {
						skyCount++
					} else {
						groundCount++
					}
					if isGridDot(d.lat, d.lon) {
						gridCount++
					}
					if horizonDot[idx] {
						horizon = true
					}
				}
			}
			if pattern == 0 {
				cells[row][col] = " "
				continue
			}
			ch := string(rune(0x2800) + pattern)
			total := skyCount + groundCount
			var style lipgloss.Style
			switch {
			case horizon:
				horizonCell[row][col] = true
				// Horizon line: the pale band on the cells the equator
				// passes through, whatever the cell phase.
				style = horizonStyle
			case total <= 3:
				// Limb cell — most of the cell falls outside the disk,
				// so it sits on the ball's edge. Darker hemisphere tint
				// suggests a 3D ball with depth at the edge rather than
				// a flat-shaded disk.
				if skyCount >= groundCount {
					style = skyEdgeStyle
				} else {
					style = groundEdgeStyle
				}
			case gridCount >= 2:
				// Grid cell — cell contains enough dots near a 30°
				// parallel or meridian to read as a grid intersection
				// or arc. Use a slightly brighter tint of the dominant
				// hemisphere so the grid is felt as a sphere rotation
				// cue without competing with the markers.
				if skyCount >= groundCount {
					style = skyGridStyle
				} else {
					style = groundGridStyle
				}
			default:
				if skyCount >= groundCount {
					style = skyStyle
				} else {
					style = groundStyle
				}
			}
			cells[row][col] = style.Render(ch)
		}
	}

	// Pitch rungs (G8 Q2, #507): a KSP-style ladder, one numbered rung per
	// 10° of latitude (degrees above/below the horizon in SURF, degrees out
	// of the orbit plane in ORBIT/TGT). A rung is a short dash run centred on
	// the vertical through the disk centre (the meridian of the sub-observer)
	// carrying its two-digit number, drawn in the grid grey so it reads as
	// structure, not as a marker. The horizon (0°) is the pale line, so it
	// takes no rung; the poles take none either. Rungs are placed nearest
	// the centre first and a row already holding a rung is skipped, so the
	// foreshortened ladder near the limb thins out rather than smearing.
	// Markers and the reticle are painted after and win a contested cell.
	rungStyle := lipgloss.NewStyle().Foreground(ColorNavballGrid)
	type rungCand struct {
		lat      int
		col, row int
	}
	var rungs []rungCand
	for lat := -80; lat <= 80; lat += 10 {
		if lat == 0 || math.Abs(subLatDeg) >= RungPoleHideDeg {
			// No rung on the horizon; none at the pole view either, where
			// the parallels are concentric circles round the centre and
			// the ladder carries no reading (wave C review).
			continue
		}
		dx, dy, front := projectLatLonToPixel(float64(lat), subLonDeg, pxR, subLatDeg, subLonDeg)
		if !front {
			continue
		}
		rungs = append(rungs, rungCand{lat, (dotCx + dx) / 2, (dotCy + dy) / 4})
	}
	sort.SliceStable(rungs, func(i, j int) bool {
		di, dj := absInt(rungs[i].row-dotCy/4), absInt(rungs[j].row-dotCy/4)
		if di != dj {
			return di < dj
		}
		// Two rungs on one row: the one nearer the horizon wins, so the
		// ladder reads the same numbers above and below at level instead
		// of favouring whichever happened to be appended first.
		return absInt(rungs[i].lat) < absInt(rungs[j].lat)
	})
	type rungDigits struct {
		row, c0 int
		label   string
	}
	var drawn []rungDigits
	rungRowTaken := map[int]bool{}
	for _, rg := range rungs {
		if rg.row < 0 || rg.row >= rows || rungRowTaken[rg.row] {
			continue
		}
		label := fmt.Sprintf("%02d", absInt(rg.lat))
		// Label on the two cells straddling the centre vertical (the disk
		// centre sits between cols dotCx/2-1 and dotCx/2), dashes beyond.
		// The row holding the reticle shifts its label one cell right so
		// the reticle stays readable.
		l0 := rg.col - 1
		if rg.row == dotCy/4 {
			l0 = rg.col + 1
		}
		put := func(c int, g string) {
			// Rungs never paint over the horizon line or the blank margin.
			if c >= 0 && c < cols && cells[rg.row][c] != " " && !horizonCell[rg.row][c] {
				cells[rg.row][c] = rungStyle.Render(g)
			}
		}
		if l0 < 0 || l0+1 >= cols || cells[rg.row][l0] == " " || cells[rg.row][l0+1] == " " ||
			horizonCell[rg.row][l0] || horizonCell[rg.row][l0+1] {
			// A rung that lands on the horizon line (or off the disk)
			// draws nothing and does not claim the row.
			continue
		}
		put(l0, label[0:1])
		put(l0+1, label[1:2])
		put(l0-2, "─")
		put(l0-1, "─")
		put(l0+2, "─")
		put(l0+3, "─")
		rungRowTaken[rg.row] = true
		drawn = append(drawn, rungDigits{rg.row, l0, label})
	}

	// Center reticle — small faint `+` at the disk centre, the
	// conceptual analogue of KSP's static "T" indicating "this is
	// where the craft is pointing." Drawn before markers so any
	// marker that lands at the centre (prograde once SAS settles on
	// prograde, etc.) overwrites it; the reticle is the empty-state
	// reference, not a competing overlay.
	centerCol := dotCx / 2
	centerRow := dotCy / 4
	if centerCol >= 0 && centerCol < cols && centerRow >= 0 && centerRow < rows && cells[centerRow][centerCol] != " " {
		reticle := lipgloss.NewStyle().Foreground(ColorNavballGrid).Faint(true).Render("+")
		cells[centerRow][centerCol] = reticle
	}

	// Resolve each marker to a cell once, projecting through the same
	// (stabilised) sub-observer as the texture so markers stay
	// frame-consistent with the disk. Marker lat/lon are used
	// continuously — no rounding; with a jitter-free sub-observer a
	// sub-degree change can't cross a cell boundary, so there's
	// nothing to quantize away and rounding would only reintroduce
	// snap-dither at half-integer inputs.
	//
	// Off-disk markers are CLAMPED to the rim, not dropped. The
	// orbit-normal markers sit ~90° from the nose during typical
	// in-plane flight (SAS holding prograde), so they live right at
	// the disk limb — exactly where a hard radius cull made them blink
	// out whenever residual direction noise nudged them a hair past
	// pxR, and where back-hemisphere dimming made them read as "not
	// rendered". Riding the rim is continuous (no on/off transition)
	// and matches KSP, which pins off-disk markers to the navball edge
	// rather than hiding them. Nothing is dropped → every marker is
	// always present, so there is no per-frame appear/disappear.
	type placedMarker struct {
		m        NavballMarker
		col, row int
		front    bool
	}
	placed := make([]placedMarker, 0, len(markers))
	for _, m := range markers {
		mdx, mdy, front := projectLatLonToPixel(
			m.LatDeg, m.LonDeg, pxR, subLatDeg, subLonDeg)
		if r2 := mdx*mdx + mdy*mdy; r2 > pxR*pxR {
			scale := float64(pxR) / math.Sqrt(float64(r2))
			mdx = int(math.Round(float64(mdx) * scale))
			mdy = int(math.Round(float64(mdy) * scale))
		}
		// Marker positions are in dot units; map to the containing
		// cell. (dotCx + mdx) ∈ [0, dotsW); divide by 2 dots/cell.
		// Clamp the cell index (not the marker) so a rim marker at
		// exactly +pxR can't index one past the grid edge.
		col := (dotCx + mdx) / 2
		row := (dotCy + mdy) / 4
		if col < 0 {
			col = 0
		} else if col >= cols {
			col = cols - 1
		}
		if row < 0 {
			row = 0
		} else if row >= rows {
			row = rows - 1
		}
		placed = append(placed, placedMarker{m, col, row, front})
	}

	// Front-only: back-hemisphere markers are skipped entirely (not
	// dimmed) so the ball reads like a solid sphere — the player only
	// ever sees the glyphs on the side of the ball facing them, never
	// the antipodal pair bleeding through. Faint styling on terminals
	// varied too much to reliably read as "behind" (some terminals
	// render Faint nearly as strong as Bold), so the antipode of an
	// axis looked like it was sharing the disk with its partner —
	// especially confusing for prograde/retrograde, which sit at
	// opposite poles of the orbit-frame basis. Markers that genuinely
	// lie on the silhouette (|z| ≤ limbFrontEpsZ) are still classed
	// front by projectLatLonToPixel so they render at the rim instead
	// of strobing on float noise.
	//
	// Markers render Bold so the glyph reads cleanly against the
	// hemisphere texture even when marker and hemisphere colors are
	// close in hue (yellow prograde on orange ground, cyan radial on
	// blue sky).
	for _, p := range placed {
		if !p.front {
			continue
		}
		glyph := string(p.m.Glyph)
		if p.m.Glyph == 0 {
			glyph = "•"
		}
		style := lipgloss.NewStyle().Foreground(p.m.Color).Bold(true)
		cells[p.row][p.col] = style.Render(glyph)
	}

	// A glyph that won one digit cell of a rung leaves the other digit
	// reading as a different number (`6△` for 60). Replace the survivor
	// with a rung dash; the pitch/hdg readout carries the exact angle.
	for _, d := range drawn {
		a := rungStyle.Render(d.label[0:1])
		b := rungStyle.Render(d.label[1:2])
		ka, kb := cells[d.row][d.c0] == a, cells[d.row][d.c0+1] == b
		switch {
		case ka && !kb:
			cells[d.row][d.c0] = rungStyle.Render("─")
		case kb && !ka:
			cells[d.row][d.c0+1] = rungStyle.Render("─")
		}
	}

	lines := make([]string, rows)
	for row := 0; row < rows; row++ {
		lines[row] = strings.Join(cells[row], "")
	}
	return strings.Join(lines, "\n")
}

// RungPoleHideDeg is the sub-observer |latitude| at or above which the
// pitch rungs are not drawn (the pad pole view). sim.NavballPolePinDeg is
// the same threshold for the rose pin.
const RungPoleHideDeg = 89.0

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
