package screens

import (
	"math"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// scaleBarMinCells is the shortest a scale bar may be, measured between
// its two tick marks (#602, grill GS Q7): below this the bar stops reading
// as a length and starts reading as a glyph.
const scaleBarMinCells = 6

// scaleBar is a round distance (1, 2 or 5 x 10^n) drawn as a bar of whole
// canvas cells: `├──────┤ 200 Mm`. The two ticks sit `cells` cells apart
// centre to centre, so the bar measures exactly `cells * metres-per-cell`
// to within the rounding of metres to whole cells.
type scaleBar struct {
	metres float64 // the round distance the bar stands for
	cells  int     // tick-to-tick span in canvas cells (>= scaleBarMinCells)
	label  string  // metres on the ADR 0049 distance ladder, round .0 dropped
}

// scaleBarFor builds the bar for a canvas drawing pxPerMeter pixels per
// metre along its horizontal screen axis. A canvas cell is two pixels wide
// (a braille cell), so one cell spans 2/pxPerMeter metres. Every map
// projection is orthographic with a unit horizontal basis vector, so this
// one scale holds along the whole horizontal axis at any depth (see the
// tilted-projection guard in scalebar_test.go). ok is false when there is
// no usable scale.
func scaleBarFor(pxPerMeter float64) (scaleBar, bool) {
	if !(pxPerMeter > 0) || math.IsInf(pxPerMeter, 0) {
		return scaleBar{}, false
	}
	mPerCell := 2 / pxPerMeter
	want := mPerCell * scaleBarMinCells
	m := roundScaleMetres(want)
	cells := int(math.Round(m / mPerCell))
	if cells < scaleBarMinCells {
		cells = scaleBarMinCells
	}
	return scaleBar{metres: m, cells: cells, label: scaleBarLabel(m)}, true
}

// roundScaleMetres is the smallest 1/2/5 x 10^n distance >= want. Past
// 0.1 AU the ladder prints AU, so the round numbers are taken in AU there
// (`2 AU`, never `1.336 AU`).
func roundScaleMetres(want float64) float64 {
	m := roundOneTwoFive(want)
	if m >= bodies.AU/10 {
		m = roundOneTwoFive(want/bodies.AU) * bodies.AU
	}
	return m
}

func roundOneTwoFive(want float64) float64 {
	dec := math.Pow(10, math.Floor(math.Log10(want)))
	for _, f := range []float64{1, 2, 5, 10} {
		if f*dec >= want*(1-1e-12) {
			return f * dec
		}
	}
	return 10 * dec
}

// scaleBarLabel is readout.Distance with the padding zeros of a round value
// dropped: `200.0 Mm` -> `200 Mm`, `1.000 km` -> `1 km`, `0.2000 AU` -> `0.2 AU`.
func scaleBarLabel(m float64) string {
	s := readout.Distance(m)
	num, unit, ok := strings.Cut(s, " ")
	if !ok || !strings.Contains(num, ".") {
		return s
	}
	num = strings.TrimRight(strings.TrimRight(num, "0"), ".")
	return num + " " + unit
}

// text is the drawn bar: ticks `cells` columns apart, then the label.
func (b scaleBar) text() string {
	return "├" + strings.Repeat("─", b.cells-1) + "┤ " + b.label
}

// width is the bar's width in canvas cells, label included.
func (b scaleBar) width() int { return b.cells + 2 + len([]rune(b.label)) }

// scaleBarOff is a test seam: the overlap guards render a frame with and
// without the bar and require the two to differ only in the bar's cells.
var scaleBarOff bool

// paintScaleBar stamps the bar onto the canvas's last row, flush to the
// right edge (one blank cell from the border) where the navball's frame
// ends a row above and nothing else composites. stripEnd is the first
// column past the label and Hint Strip already painted on that row: the bar
// never lands on them. When the row is too full it uses the fallback cell
// instead (fbRow < 0 disables it; fbCol < 0 means right-aligned), and when
// that is gone too it draws no bar rather than cover a key hint.
func (v *OrbitView) paintScaleBar(stripEnd, fbRow, fbCol int) {
	if scaleBarOff {
		return
	}
	bar, ok := scaleBarFor(v.canvas.Scale())
	if !ok {
		return
	}
	cols, last, w := v.canvas.Cols(), v.canvas.Rows()-1, bar.width()
	col := cols - w - 1
	row := last
	if col < stripEnd+hintStripGap {
		if fbRow < 0 {
			return
		}
		row, col = fbRow, fbCol
		if col < 0 {
			col = cols - w - 1
		}
	}
	if col < 0 || col+w > cols {
		return
	}
	v.canvas.SetCellLabelColored(col, row, bar.text(), v.theme.Primary.GetForeground())
}

// withScaleBar appends the scale bar to the launch view's last-row HUD
// strip, right-aligned the way the map's is (#602). The chase cam's scale
// is altitude-derived (launchAutoScale) or the player's pinned zoom; either
// way it reaches the canvas as pixels per metre, so canvas.Scale() is what
// is drawn. When the strip runs too long for the bar the strip wins.
func (v *LaunchView) withScaleBar(hud string) string {
	if scaleBarOff || hud == "" {
		return hud
	}
	bar, ok := scaleBarFor(v.canvas.Scale())
	if !ok {
		return hud
	}
	cols := v.canvas.Cols()
	pad := cols - bar.width() - 1 - displayWidth(hud)
	if pad < hintStripGap {
		return hud
	}
	return hud + strings.Repeat(" ", pad) + v.theme.Primary.Render(bar.text())
}
