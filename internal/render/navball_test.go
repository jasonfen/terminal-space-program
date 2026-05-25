package render

import (
	"math"
	"strings"
	"testing"
)

// TestNavballStringDimensions confirms the painter returns the
// requested cell-grid shape: rows × cols ANSI-styled cells (or a
// single space for transparent cells outside the disk).
func TestNavballStringDimensions(t *testing.T) {
	got := NavballString(12, 12, 0, 0, nil)
	lines := strings.Split(got, "\n")
	if len(lines) != 12 {
		t.Fatalf("want 12 lines, got %d", len(lines))
	}
	// Each line should contain at least one styled cell (the disk
	// fills > 0 cells per row inside the disk band). The center
	// rows definitely have content.
	if strings.TrimSpace(lines[6]) == "" {
		t.Errorf("expected center row to have rendered cells, got empty: %q", lines[6])
	}
}

// TestNavballSubObserverProjection confirms that the projection
// reuses cleanly: the cell at the disk centre projects to (subLat,
// subLon). This is the spike's gating assertion — if this passes,
// projectPixelToLatLon is reusable as-is for the navball, which is
// the v0.9.5 plan's flagged sizing risk.
func TestNavballSubObserverProjection(t *testing.T) {
	cases := []struct {
		name             string
		subLat, subLon   float64
		wantLat, wantLon float64
	}{
		{"equator origin", 0, 0, 0, 0},
		{"north tilt", 30, 0, 30, 0},
		{"east shift", 0, 45, 0, 45},
		{"both", -20, -120, -20, -120},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Centre pixel: dx = 0, dy = 0; pxR arbitrary.
			lat, lon, ok := projectPixelToLatLon(0, 0, 12, tc.subLat, tc.subLon, 0, 1)
			if !ok {
				t.Fatalf("centre pixel should be inside disk")
			}
			if math.Abs(lat-tc.wantLat) > 1e-6 {
				t.Errorf("lat: want %g, got %g", tc.wantLat, lat)
			}
			if math.Abs(lon-tc.wantLon) > 1e-6 {
				t.Errorf("lon: want %g, got %g", tc.wantLon, lon)
			}
		})
	}
}

// TestProjectLatLonToPixelRoundTrip confirms forward + inverse
// projections compose to identity for a representative grid of points
// on the visible hemisphere. The forward projection has to land
// pixels that the inverse can recover to the original (lat, lon)
// modulo rounding.
func TestProjectLatLonToPixelRoundTrip(t *testing.T) {
	const pxR = 24 // bigger pxR keeps int rounding error sub-degree
	cases := []struct {
		subLat, subLon float64
		lat, lon       float64
	}{
		{0, 0, 0, 0},
		{0, 0, 0, 30},
		{0, 0, 30, 0},
		{0, 0, 30, 60},
		{30, 0, 30, 0},
		{0, 45, 0, 60},
		{20, -90, 10, -120},
	}
	for _, tc := range cases {
		dx, dy, front := projectLatLonToPixel(tc.lat, tc.lon, pxR, tc.subLat, tc.subLon)
		if !front {
			t.Errorf("(%g,%g) under sub-obs (%g,%g): expected front", tc.lat, tc.lon, tc.subLat, tc.subLon)
			continue
		}
		gotLat, gotLon, ok := projectPixelToLatLon(dx, dy, pxR, tc.subLat, tc.subLon, 0, 1)
		if !ok {
			t.Errorf("(%g,%g) → pixel (%d,%d) failed inverse", tc.lat, tc.lon, dx, dy)
			continue
		}
		if math.Abs(gotLat-tc.lat) > 3 || math.Abs(gotLon-tc.lon) > 3 {
			t.Errorf("(%g,%g) round-tripped to (%g,%g)", tc.lat, tc.lon, gotLat, gotLon)
		}
	}
}

// TestProjectLatLonToPixelHemisphere confirms that points on the far
// hemisphere are flagged front=false, and the antipode of the sub-
// observer (lat, lon shifted by 180° + flipped lat) is the most-
// behind point.
func TestProjectLatLonToPixelHemisphere(t *testing.T) {
	// Sub-observer at the equator, lon=0. The antipode is at (0, 180).
	_, _, front := projectLatLonToPixel(0, 180, 12, 0, 0)
	if front {
		t.Errorf("antipode of sub-obs should be back-hemisphere")
	}
	// Same-point case is on the visible disk.
	_, _, front = projectLatLonToPixel(0, 0, 12, 0, 0)
	if !front {
		t.Errorf("sub-observer point itself should be front")
	}
	// Tilted sub-observer: a polar marker is front when sub-obs lat > 0.
	_, _, front = projectLatLonToPixel(90, 0, 12, 30, 0)
	if !front {
		t.Errorf("north pole should be front when sub-obs lat=30")
	}
	_, _, front = projectLatLonToPixel(-90, 0, 12, 30, 0)
	if front {
		t.Errorf("south pole should be back when sub-obs lat=30")
	}
}

// TestNavballMarkerOverlay confirms that a marker passed to
// NavballString gets painted onto the disk and replaces the
// underlying texture cell at its projected position.
func TestNavballMarkerOverlay(t *testing.T) {
	// Sub-observer at (0, 0) → centre cell projects to (lat≈0, lon≈0).
	// A marker at (0, 0) lands at the disk centre.
	withoutMarker := NavballString(13, 13, 0, 0, nil)
	markers := []NavballMarker{
		{LatDeg: 0, LonDeg: 0, Glyph: '⊕', Color: ColorNavballMarkerPrograde},
	}
	withMarker := NavballString(13, 13, 0, 0, markers)
	if withMarker == withoutMarker {
		t.Fatalf("marker overlay produced identical output")
	}
	// The marker glyph should appear in the rendered string. Lipgloss
	// strips colors in non-TTY tests, so the rune itself survives.
	if !strings.ContainsRune(withMarker, '⊕') {
		t.Errorf("marker glyph not found in output: %q", withMarker)
	}
}

// TestNavballMarkerBackHemisphereHidden confirms a marker on the far
// side of the ball is NOT painted. The ball should read like a solid
// sphere — the antipode of the sub-observer is hidden, not dimmed.
// Faint dimming was tried in v0.9.5 but read too strongly on many
// terminals, so the prograde/retrograde pair looked like both were
// on the visible side at once.
func TestNavballMarkerBackHemisphereHidden(t *testing.T) {
	withoutMarker := NavballString(13, 13, 0, 0, nil)
	markers := []NavballMarker{
		{LatDeg: 0, LonDeg: 180, Glyph: '⊕', Color: ColorNavballMarkerPrograde},
	}
	withMarker := NavballString(13, 13, 0, 0, markers)
	if withMarker != withoutMarker {
		t.Errorf("back-hemisphere marker should be skipped, but output changed")
	}
	if strings.ContainsRune(withMarker, '⊕') {
		t.Errorf("back-hemisphere marker glyph leaked into output")
	}
}

// TestNavballMarkerFrontOverridesBack confirms that when front and
// back markers project to the same cell (antipodes under
// orthographic projection), the front marker wins. Sub-observer at
// (0, 0); prograde at (0, 0) is dead front, retrograde at (0, 180)
// is dead back, both project to the disk centre.
func TestNavballMarkerFrontOverridesBack(t *testing.T) {
	markers := []NavballMarker{
		// Order intentionally back-first; painter must still let
		// front win regardless of input order.
		{LatDeg: 0, LonDeg: 180, Glyph: '⊖', Color: ColorNavballMarkerPrograde},
		{LatDeg: 0, LonDeg: 0, Glyph: '⊕', Color: ColorNavballMarkerPrograde},
	}
	out := NavballString(13, 13, 0, 0, markers)
	// The centre line should contain ⊕, NOT a ⊖ over it.
	lines := strings.Split(out, "\n")
	mid := lines[len(lines)/2]
	if !strings.ContainsRune(mid, '⊕') {
		t.Errorf("front prograde glyph missing from centre line: %q", mid)
	}
	// ⊖ may still appear elsewhere in output if its (col, row)
	// rounded differently — what matters is that the centre cell
	// is the front glyph.
}

// TestIsGridDot confirms the 30°-grid detection helper. Parallels
// run at every multiple of 30° latitude (excluding the poles);
// meridians at every multiple of 30° longitude, but only at
// |lat| ≤ 70° where the meridians haven't yet converged.
func TestIsGridDot(t *testing.T) {
	cases := []struct {
		name     string
		lat, lon float64
		want     bool
	}{
		{"equator", 0, 0, true},
		{"30N on prime meridian", 30, 0, true},
		{"parallel only (60S)", -60, 17, true},
		{"meridian only (0, 90)", 0, 90, true},
		{"meridian only (0, -150)", 0, -150, true},
		{"off-grid mid-cell", 17, 22, false},
		{"polar meridian skipped at 80N", 80, 30, false},
		{"polar parallel still drawn at 60", 60, 17, true},
		{"near-pole no parallel (89)", 89, 17, false},
		{"meridian visible at 70 cap", 70, 60, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isGridDot(tc.lat, tc.lon)
			if got != tc.want {
				t.Errorf("isGridDot(lat=%g, lon=%g) = %v, want %v", tc.lat, tc.lon, got, tc.want)
			}
		})
	}
}

// TestNavballCenterReticle confirms a faint `+` sits at the disk
// centre when no marker covers it. With sub-observer at the equator
// origin and no markers, the geometric centre cell paints the
// reticle glyph. This is the static "where the craft is pointing"
// reference, KSP's center marker analogue.
func TestNavballCenterReticle(t *testing.T) {
	out := NavballString(13, 13, 0, 0, nil)
	if !strings.ContainsRune(out, '+') {
		t.Errorf("center reticle `+` not found in empty-marker navball: %q", out)
	}
	// A marker at (0, 0) should overwrite the reticle: the markers'
	// SAS-axis directions win the cell. Confirm `+` is gone when
	// prograde lands at centre.
	withMarker := NavballString(13, 13, 0, 0, []NavballMarker{
		{LatDeg: 0, LonDeg: 0, Glyph: '⊕', Color: ColorNavballMarkerPrograde},
	})
	lines := strings.Split(withMarker, "\n")
	mid := lines[len(lines)/2]
	if strings.ContainsRune(mid, '+') {
		t.Errorf("marker at centre should overwrite reticle, but `+` still on centre row: %q", mid)
	}
}

// TestNavballHorizonSplit confirms that with sub-observer at the
// equator, the upper half of the disk reports as sky and the lower
// half as ground. This validates the horizon-split texture path.
func TestNavballHorizonSplit(t *testing.T) {
	// pxR = 12, dx = 0. dy < 0 should project to lat > 0 (sky); dy > 0
	// should project to lat < 0 (ground).
	for _, dy := range []int{-8, -4, 4, 8} {
		lat, _, ok := projectPixelToLatLon(0, dy, 12, 0, 0, 0, 1)
		if !ok {
			t.Fatalf("dy=%d should be inside disk", dy)
		}
		// Recall projectPixelToLatLon flips dy internally (ny = -dy/r),
		// so dy < 0 maps to lat > 0 (north / sky).
		if dy < 0 && lat <= 0 {
			t.Errorf("dy=%d should be northern hemisphere, got lat=%g", dy, lat)
		}
		if dy > 0 && lat >= 0 {
			t.Errorf("dy=%d should be southern hemisphere, got lat=%g", dy, lat)
		}
	}
}
