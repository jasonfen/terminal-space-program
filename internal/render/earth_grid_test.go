package render

import (
	"testing"
)

// TestEarthGridLandmarks: known lat/lon points should land in the
// expected mask cell — sanity check that the polygon outlines
// rasterise into something recognisable. v0.8.5.7+.
func TestEarthGridLandmarks(t *testing.T) {
	cases := []struct {
		name       string
		lat, lon   float64
		want       earthCell
	}{
		// Continents (land cells).
		{"new_york", 40.7, -74.0, cellLand},
		{"london", 51.5, -0.1, cellLand},
		{"tokyo", 35.7, 139.7, cellLand},
		{"sao_paulo", -23.5, -46.6, cellLand},
		{"sydney", -33.9, 151.2, cellLand}, // east coast of Australia
		{"madagascar", -18.8, 46.9, cellLand},
		{"iceland", 64.9, -19.0, cellLand},
		{"new_zealand", -41.0, 174.7, cellLand},
		// Oceans.
		{"mid_pacific", 0, -150, cellOcean},
		{"south_atlantic", -30, -20, cellOcean},
		{"north_atlantic", 35, -40, cellOcean},
		{"indian_ocean", -30, 80, cellOcean},
		// Deserts.
		{"sahara_central", 22, 10, cellDesert},
		{"arabian_central", 22, 47, cellDesert},
		// Ice (poles + Greenland).
		{"greenland_interior", 75, -40, cellIce},
		{"antarctica", -82, 0, cellIce},
		{"north_pole", 89, 0, cellIce},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := earthCellAt(c.lat, c.lon)
			if got != c.want {
				t.Errorf("(%v, %v): got %v, want %v", c.lat, c.lon, got, c.want)
			}
		})
	}
}

// TestEarthGridLonWrap: longitudes wrap correctly around ±180°.
func TestEarthGridLonWrap(t *testing.T) {
	a := earthCellAt(0, 179.5)
	b := earthCellAt(0, -180.5) // same cell after wrap
	if a != b {
		t.Errorf("longitude wrap broken: lon=179.5 → %v, lon=-180.5 → %v", a, b)
	}
}

// TestEarthGridContainsAllCellTypes: a full grid sweep should
// produce at least one of every cell type. Catches regressions
// where a polygon list change accidentally drops a category.
func TestEarthGridContainsAllCellTypes(t *testing.T) {
	seen := map[earthCell]bool{}
	for r := 0; r < earthGridRows; r++ {
		for c := 0; c < earthGridCols; c++ {
			seen[earthGrid[r][c]] = true
		}
	}
	for _, want := range []earthCell{cellOcean, cellLand, cellDesert, cellIce} {
		if !seen[want] {
			t.Errorf("grid missing cell type %v", want)
		}
	}
}
