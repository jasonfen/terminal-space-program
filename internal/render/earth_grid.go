package render

// pointInLatLonPolygon implements the ray-casting point-in-polygon
// test in lat/lon space. Treats lat/lon as a flat 2D coordinate; fine
// for compact non-pole-crossing polygons at the resolution we render.
// Polygons that touch ±180° are handled by closing them right at the
// antimeridian rather than wrapping.
//
// The generic data-driven texture engine (ADR 0024) uses this for the
// `mask` kind. It was originally the rasteriser for Earth's hardcoded
// land/ocean grid; that grid (and Earth's per-body Go shader) were
// retired in PR4 when Earth migrated to a JSON mask, leaving this
// point-in-polygon primitive as the shared survivor.
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
