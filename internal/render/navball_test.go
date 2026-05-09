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
	got := NavballString(12, 12, 0, 0)
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
			lat, lon, ok := projectPixelToLatLon(0, 0, 12, tc.subLat, tc.subLon)
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

// TestNavballGridFires checks that navballCell flags equator + lat
// lines + lon lines as grid cells, and that off-grid points fall
// through to the hemisphere fill.
func TestNavballGridFires(t *testing.T) {
	cases := []struct {
		name     string
		lat, lon float64
		want     navballCellKind
	}{
		{"equator-meridian intersection", 0, 0, navballGrid},
		{"30N parallel", 30, 17, navballGrid},
		{"60S parallel", -60, -42, navballGrid},
		{"+150 lon meridian", 12, 150, navballGrid},
		{"-180 lon meridian (wrap-aware)", 5, -180, navballGrid},
		{"off-grid sky", 45, 75, navballSky},
		{"off-grid ground", -25, -75, navballGround},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := navballCell(tc.lat, tc.lon)
			if got != tc.want {
				t.Errorf("navballCell(%g, %g) = %d, want %d", tc.lat, tc.lon, got, tc.want)
			}
		})
	}
}

// TestNavballHorizonSplit confirms that with sub-observer at the
// equator, the upper half of the disk reports as sky and the lower
// half as ground. This validates the horizon-split texture path.
func TestNavballHorizonSplit(t *testing.T) {
	// pxR = 12, dx = 0. dy < 0 should project to lat > 0 (sky); dy > 0
	// should project to lat < 0 (ground).
	for _, dy := range []int{-8, -4, 4, 8} {
		lat, _, ok := projectPixelToLatLon(0, dy, 12, 0, 0)
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
