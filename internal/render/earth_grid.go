package render

import "math"

// Earth surface mask: a 144×72 grid (2.5° per cell) classifying
// each lat/lon cell as ocean / land / desert / ice. Replaces the
// ellipse-table approximation v0.7.6 / v0.8.5.7 used; the grid
// reads as a recognisable continental outline at typical disk
// pixel-radii (16–64 px) because the projection from screen pixel
// to (lat, lon) and the grid resolution are roughly matched.
//
// The grid is generated at package-init time by rasterising a
// list of lat/lon polygons (earthMaskPolys). Polygons are stored
// rather than the rendered grid because the vertex list (~50
// polys × 10–20 verts ≈ 700 floats) is far easier to refine than
// a 10k-character ASCII grid would be.
//
// Future polish: a v0.9-class raster (real 1° NOAA land/sea mask
// embedded with go:embed) would slot into the same earthGrid
// storage with a different generator. See docs/state-of-game.md
// "future-rendering" backlog.

const (
	earthGridRows = 72   // 2.5° lat per row, row 0 = lat band [87.5°, 90°]
	earthGridCols = 144  // 2.5° lon per col, col 0 = lon band [-180°, -177.5°]
	earthGridStep = 2.5  // cell size in degrees
)

type earthCell byte

const (
	cellOcean earthCell = iota
	cellLand
	cellDesert
	cellIce
)

// earthGrid stores the rasterised mask. Filled by rasterizeEarthGrid
// at package-init time.
var earthGrid [earthGridRows][earthGridCols]earthCell

// llpoly is a closed polygon in lat/lon space — outer-only,
// vertex order doesn't matter for the standard ray-casting
// point-in-polygon test. Cell labels paint over earlier polys in
// list order (so deserts / ice can sit on top of land base).
type llpoly struct {
	name  string
	cell  earthCell
	verts [][2]float64 // (lat, lon) pairs, degrees
}

// earthMaskPolys is the polygon source for the rasterised grid.
// Continents use coarse outlines (10–25 vertices each) — at 2.5°
// resolution the rendered cell count is much smaller than the
// vertex count anyway, so adding finer geographic detail past a
// point gives diminishing returns. Order matters: earlier entries
// paint first (land base), later entries override (deserts /
// ice). Islands are separate polygons so they appear without
// merging into mainlands.
//
// Coordinate convention: (lat, lon) — lat ∈ [-90, 90] with positive
// north, lon ∈ [-180, 180] with positive east. Antimeridian-
// crossing landmasses (Aleutians, Russia / Bering tip) are split
// into two polygons rather than special-cased.
var earthMaskPolys = []llpoly{
	// --- Land bases (paint first) ---

	// North America: Alaska / Arctic Canada / mainland US / Mexico /
	// Central America. Coarse outline approximating the gulf-
	// crossing coastline.
	{"north_america", cellLand, [][2]float64{
		{72, -156}, {73, -130}, {72, -100}, {74, -80}, {70, -75},
		{63, -77}, {62, -85}, {58, -95}, {62, -75}, {60, -65},
		{55, -60}, {49, -52}, {45, -58}, {45, -68}, {40, -72},
		{35, -76}, {30, -80}, {25, -80}, {28, -86}, {30, -90},
		{30, -95}, {26, -97}, {21, -97}, {18, -94}, {18, -88},
		{15, -88}, {13, -85}, {10, -83}, {8, -80}, {15, -95},
		{18, -103}, {22, -106}, {28, -114}, {32, -117}, {40, -124},
		{48, -125}, {55, -132}, {60, -148}, {66, -163}, {72, -156},
	}},
	// South America: roughly triangular continent.
	{"south_america", cellLand, [][2]float64{
		{12, -72}, {12, -62}, {7, -55}, {2, -50}, {-2, -45},
		{-8, -35}, {-15, -39}, {-23, -41}, {-30, -50}, {-38, -57},
		{-50, -68}, {-55, -68}, {-52, -72}, {-45, -74}, {-35, -73},
		{-28, -71}, {-20, -70}, {-10, -78}, {-3, -80}, {2, -78},
		{8, -77}, {12, -72},
	}},
	// Greenland (mostly ice — cellIce paints over below).
	{"greenland", cellLand, [][2]float64{
		{83, -30}, {81, -15}, {77, -18}, {72, -22}, {68, -25},
		{60, -45}, {65, -52}, {72, -55}, {77, -65}, {80, -55},
		{83, -30},
	}},
	// Europe: Atlantic Iberia / France / British Isles / Scandinavia,
	// Mediterranean, into Russia.
	{"europe", cellLand, [][2]float64{
		{72, 30}, {68, 40}, {60, 55}, {50, 50}, {45, 38},
		{40, 28}, {38, 23}, {37, 14}, {41, 9}, {44, 8},
		{43, 5}, {44, -2}, {44, -9}, {39, -10}, {37, -8},
		{36, -6}, {38, -2}, {44, 0}, {49, 0}, {51, 4},
		{53, 7}, {54, 9}, {57, 12}, {59, 17}, {66, 22},
		{70, 25}, {72, 30},
	}},
	// British Isles: combined UK + Ireland blob (separate from
	// mainland Europe so the channel reads).
	{"british_isles", cellLand, [][2]float64{
		{59, -3}, {58, 0}, {56, 1}, {52, 2}, {51, -1},
		{50, -5}, {52, -5}, {54, -8}, {55, -10}, {57, -7},
		{59, -3},
	}},
	// Iceland.
	{"iceland", cellLand, [][2]float64{
		{66, -23}, {66, -14}, {64, -14}, {63, -19}, {64, -23},
		{66, -23},
	}},
	// Italy: boot peninsula. Important for "is this Earth?"
	// recognition.
	{"italy", cellLand, [][2]float64{
		{46, 8}, {46, 13}, {44, 13}, {42, 14}, {40, 18},
		{38, 17}, {37, 15}, {40, 14}, {41, 12}, {44, 10},
		{45, 7}, {46, 8},
	}},
	// Sicily.
	{"sicily", cellLand, [][2]float64{
		{38, 13}, {38, 15}, {37, 15}, {37, 13}, {38, 13},
	}},
	// Africa: bulky north + tapering south.
	{"africa", cellLand, [][2]float64{
		{37, -8}, {37, 10}, {35, 23}, {32, 32}, {32, 35},
		{27, 35}, {17, 38}, {12, 42}, {12, 50}, {5, 47},
		{-3, 41}, {-12, 40}, {-25, 35}, {-34, 29}, {-35, 23},
		{-34, 19}, {-30, 15}, {-22, 12}, {-12, 13}, {-5, 9},
		{2, 6}, {5, -2}, {10, -10}, {15, -16}, {18, -16},
		{22, -16}, {28, -12}, {35, -6}, {37, -8},
	}},
	// Madagascar.
	{"madagascar", cellLand, [][2]float64{
		{-12, 49}, {-15, 50}, {-22, 47}, {-25, 45}, {-23, 44},
		{-18, 44}, {-13, 47}, {-12, 49},
	}},
	// Asia: massive landmass east of Europe to Pacific. Includes
	// Siberia, Kazakhstan, China, Mongolia. Indian subcontinent and
	// SE Asia are separate polys for shape clarity.
	{"asia", cellLand, [][2]float64{
		{75, 50}, {77, 100}, {72, 145}, {68, 180}, {68, 178},
		{62, 175}, {60, 162}, {58, 150}, {60, 140}, {53, 142},
		{45, 138}, {42, 130}, {39, 122}, {35, 120}, {33, 122},
		{30, 121}, {25, 118}, {22, 110}, {21, 108}, {28, 95},
		{30, 90}, {30, 80}, {35, 75}, {37, 70}, {40, 60},
		{45, 55}, {50, 50}, {55, 50}, {60, 60}, {68, 65},
		{72, 55}, {75, 50},
	}},
	// Indian subcontinent.
	{"india", cellLand, [][2]float64{
		{32, 75}, {32, 80}, {28, 88}, {22, 90}, {22, 89},
		{20, 87}, {15, 80}, {10, 78}, {8, 77}, {12, 75},
		{15, 73}, {20, 73}, {23, 70}, {25, 68}, {28, 70},
		{32, 75},
	}},
	// Sri Lanka.
	{"sri_lanka", cellLand, [][2]float64{
		{10, 80}, {9, 82}, {6, 82}, {6, 80}, {10, 80},
	}},
	// Korean peninsula.
	{"korea", cellLand, [][2]float64{
		{43, 130}, {39, 130}, {36, 129}, {34, 128}, {34, 126},
		{38, 125}, {41, 126}, {43, 130},
	}},
	// Japan: Honshu + Hokkaido + Kyushu/Shikoku, simplified to one
	// outline.
	{"japan", cellLand, [][2]float64{
		{45, 142}, {44, 145}, {42, 144}, {39, 142}, {36, 140},
		{33, 137}, {31, 131}, {33, 130}, {36, 134}, {38, 138},
		{41, 140}, {45, 142},
	}},
	// Indochina / SE Asia mainland (Vietnam / Thailand / Malaysia).
	{"indochina", cellLand, [][2]float64{
		{22, 100}, {22, 108}, {18, 108}, {12, 110}, {10, 105},
		{6, 102}, {2, 102}, {2, 100}, {8, 98}, {12, 99},
		{15, 97}, {18, 95}, {22, 100},
	}},
	// Indonesia: Sumatra.
	{"sumatra", cellLand, [][2]float64{
		{6, 95}, {3, 99}, {-2, 102}, {-5, 105}, {-6, 105},
		{-3, 102}, {0, 99}, {3, 96}, {6, 95},
	}},
	// Java.
	{"java", cellLand, [][2]float64{
		{-6, 105}, {-6, 114}, {-8, 114}, {-9, 110}, {-8, 105},
		{-6, 105},
	}},
	// Borneo.
	{"borneo", cellLand, [][2]float64{
		{7, 117}, {5, 119}, {0, 119}, {-3, 116}, {-4, 113},
		{-1, 110}, {2, 110}, {7, 114}, {7, 117},
	}},
	// Sulawesi (Celebes).
	{"sulawesi", cellLand, [][2]float64{
		{2, 121}, {-1, 122}, {-5, 120}, {-3, 119}, {0, 120},
		{2, 121},
	}},
	// New Guinea.
	{"new_guinea", cellLand, [][2]float64{
		{-1, 131}, {-3, 142}, {-6, 147}, {-10, 150}, {-9, 146},
		{-8, 140}, {-5, 134}, {-2, 132}, {-1, 131},
	}},
	// Philippines (Luzon-ish blob).
	{"philippines", cellLand, [][2]float64{
		{19, 121}, {17, 122}, {14, 121}, {12, 124}, {9, 124},
		{8, 122}, {12, 122}, {15, 120}, {18, 120}, {19, 121},
	}},
	// Australia.
	{"australia", cellLand, [][2]float64{
		{-12, 130}, {-11, 142}, {-15, 145}, {-22, 150}, {-30, 153},
		{-37, 150}, {-39, 146}, {-37, 140}, {-35, 137}, {-32, 133},
		{-32, 126}, {-35, 118}, {-32, 115}, {-22, 113}, {-18, 122},
		{-14, 127}, {-12, 130},
	}},
	// Tasmania.
	{"tasmania", cellLand, [][2]float64{
		{-40, 145}, {-40, 148}, {-43, 148}, {-43, 145}, {-40, 145},
	}},
	// New Zealand: combined N + S islands.
	{"new_zealand", cellLand, [][2]float64{
		{-34, 173}, {-37, 178}, {-42, 174}, {-46, 170}, {-46, 167},
		{-42, 171}, {-39, 173}, {-37, 175}, {-34, 173},
	}},
	// Cuba.
	{"cuba", cellLand, [][2]float64{
		{23, -82}, {22, -76}, {20, -74}, {20, -77}, {22, -84},
		{23, -82},
	}},
	// Hispaniola (Haiti + DR).
	{"hispaniola", cellLand, [][2]float64{
		{20, -73}, {19, -68}, {17, -68}, {18, -72}, {20, -73},
	}},
	// Antarctica: rough outer outline. Polygon extends from the
	// continent's coastal edge (~-65°) down to lat -89° on both
	// antimeridian sides so the south polar region fills correctly
	// — flat 2D polygons don't naturally wrap at the pole, so the
	// closure at lat -89 / lon ±180 supplies the bottom edge.
	{"antarctica", cellLand, [][2]float64{
		{-65, -180}, {-65, -60}, {-72, -45}, {-78, 0},
		{-72, 60}, {-65, 100}, {-65, 145}, {-72, 165}, {-65, 180},
		{-89, 180}, {-89, -180},
	}},

	// --- Deserts (paint over land) ---

	// Sahara: northern Africa belt.
	{"sahara", cellDesert, [][2]float64{
		{32, -8}, {32, 32}, {25, 35}, {18, 35}, {15, 22},
		{17, 8}, {18, -5}, {22, -10}, {28, -8}, {32, -8},
	}},
	// Arabian peninsula.
	{"arabian", cellDesert, [][2]float64{
		{30, 35}, {30, 50}, {25, 56}, {18, 55}, {12, 50},
		{12, 43}, {18, 38}, {25, 35}, {30, 35},
	}},
	// Gobi (Mongolia / N. China).
	{"gobi", cellDesert, [][2]float64{
		{48, 95}, {48, 115}, {42, 115}, {38, 105}, {38, 95},
		{42, 90}, {48, 95},
	}},
	// Australian outback (interior).
	{"outback", cellDesert, [][2]float64{
		{-20, 122}, {-20, 138}, {-30, 138}, {-32, 130}, {-30, 122},
		{-20, 122},
	}},
	// Atacama / Patagonian arid stripe (small Andes lee).
	{"atacama", cellDesert, [][2]float64{
		{-22, -70}, {-22, -67}, {-30, -68}, {-30, -71}, {-22, -70},
	}},

	// --- Ice (paints over land + ocean) ---

	// Greenland interior ice sheet.
	{"greenland_ice", cellIce, [][2]float64{
		{82, -32}, {80, -18}, {76, -22}, {72, -26}, {65, -45},
		{72, -52}, {78, -55}, {81, -45}, {82, -32},
	}},
	// Antarctic ice sheet (mirrors the Antarctica land poly so the
	// continent reads white in renderings even though the land
	// underneath is also painted).
	{"antarctic_ice", cellIce, [][2]float64{
		{-66, -180}, {-66, -60}, {-72, -45}, {-78, 0},
		{-72, 60}, {-66, 100}, {-66, 145}, {-72, 165}, {-66, 180},
		{-89, 180}, {-89, -180},
	}},
	// Arctic Ocean ice cap. Same closure trick: flat polygon
	// extends from ~lat 76° down to lat 89° across the antimeridian
	// so cells around the north pole fall inside.
	{"arctic_ice", cellIce, [][2]float64{
		{76, -180}, {78, -90}, {76, 0}, {78, 90}, {76, 180},
		{89, 180}, {89, -180},
	}},
}

func init() {
	rasterizeEarthGrid()
}

// rasterizeEarthGrid samples each cell's centre against the
// polygon list and stores the most-specific label found. Runs once
// at package init; cost is ~10k cells × ~50 polys × ~10 verts ≈
// 5 million float ops, ~10 ms on cold start.
func rasterizeEarthGrid() {
	for r := 0; r < earthGridRows; r++ {
		// Lat at centre of row r: start at 90°, step down by 2.5°
		// per row, offset by half a step to land at the cell centre.
		lat := 90 - earthGridStep*(float64(r)+0.5)
		for c := 0; c < earthGridCols; c++ {
			lon := -180 + earthGridStep*(float64(c)+0.5)
			cell := cellOcean
			for _, p := range earthMaskPolys {
				if pointInLatLonPolygon(lat, lon, p.verts) {
					cell = p.cell
				}
			}
			earthGrid[r][c] = cell
		}
	}
}

// earthCellAt returns the rasterised mask cell for the given
// lat/lon, in degrees. Wraps longitude into (-180, 180]; clamps
// latitude into [-90, 90].
func earthCellAt(latDeg, lonDeg float64) earthCell {
	if latDeg > 90 {
		latDeg = 90
	} else if latDeg < -90 {
		latDeg = -90
	}
	for lonDeg > 180 {
		lonDeg -= 360
	}
	for lonDeg < -180 {
		lonDeg += 360
	}
	r := int(math.Floor((90 - latDeg) / earthGridStep))
	if r < 0 {
		r = 0
	} else if r >= earthGridRows {
		r = earthGridRows - 1
	}
	c := int(math.Floor((lonDeg + 180) / earthGridStep))
	if c < 0 {
		c = 0
	} else if c >= earthGridCols {
		c = earthGridCols - 1
	}
	return earthGrid[r][c]
}

// pointInLatLonPolygon implements the ray-casting point-in-polygon
// test in lat/lon space. Treats lat/lon as a flat 2D coordinate;
// fine for compact non-pole-crossing polygons at the resolution
// we render. Antarctic / Arctic polygons that touch ±180° are
// handled by closing them right at the antimeridian rather than
// wrapping.
func pointInLatLonPolygon(lat, lon float64, verts [][2]float64) bool {
	if len(verts) < 3 {
		return false
	}
	inside := false
	n := len(verts)
	j := n - 1
	for i := 0; i < n; i++ {
		yi, xi := verts[i][0], verts[i][1]
		yj, xj := verts[j][0], verts[j][1]
		if ((yi > lat) != (yj > lat)) &&
			(lon < (xj-xi)*(lat-yi)/(yj-yi)+xi) {
			inside = !inside
		}
		j = i
	}
	return inside
}
