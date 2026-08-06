package screens

import (
	"math"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/render"
)

// #363: idle CPU on the orbit screen was dominated by re-rasterizing the
// focused body's textured disk every 20 Hz tick — per-pixel lat/lon
// projection, trig, and feature-polygon tests via the render.BodyTexture
// closure — even though at a steady warp the visible picture is
// essentially unchanged frame to frame. This mirrors the ADR 0017
// predict-on-change discipline (see predictCache / soiPassCache above):
// memoize the rasterized disk, keyed on everything the picture actually
// depends on, and blit the cached grid on a key hit instead of calling
// the texture closure per pixel.
//
// diskRasterQuantDeg is the angular quantum (degrees) used to bucket
// every lat/lon-shaped input to the raster: the camera sub-observer
// point, the light source's sub-solar point, and the screen-up
// orientation (folded to an angle via atan2). It must be coarse enough
// that a steady warp-1× idle frame (sub-observer longitude drifts
// ~0.0002°/tick) keeps landing in the same bucket for many consecutive
// ticks, and fine enough that one bucket step never moves a rendered
// pixel — i.e. it must stay well under one pixel of arc at the limb of
// the largest disk the game actually draws.
//
// arc_px ≈ pxRadius × quantum(rad) = pxRadius × quantum(deg) × π/180.
// At the extreme end of the zoom range the focused body can fill a
// large fraction of a big terminal — call it pxRadius ≈ 300 px, well
// past anything a real playthrough sits at. 300 × 0.02 × π/180 ≈ 0.10
// px: under half a pixel even at that extreme, and BodyTextureMinRadius
// (12 px) floors the ratio from the other side. 0.02° is also 250×
// coarser than the ~0.0002°/tick idle drift, so consecutive idle ticks
// land in the same bucket for ~2.5 s (50 ticks) before a real miss.
const diskRasterQuantDeg = 0.02

// diskRasterQuantEclipse is the quantum for SolarLight.EclipseFactor
// (range [0, 1]). Shade() rounds the shaded color to 8-bit channels, so
// any factor step smaller than 1/255 can never change the rendered
// color; 1/512 stays comfortably under that with margin to spare.
const diskRasterQuantEclipse = 1.0 / 512.0

// diskRasterKey fingerprints everything a body's textured-disk raster
// depends on. Two frames with an equal key are guaranteed to paint the
// identical grid of pixel colors (up to sub-pixel drift smaller than the
// quantum, per the derivation above), so a key hit can blit the cached
// grid instead of re-running the texture closure.
type diskRasterKey struct {
	systemIdx int
	bodyID    string
	pxRadius  int
	subLatQ   int64
	subLonQ   int64
	upAngleQ  int64
	hasLight  bool
	lightLatQ int64
	lightLonQ int64
	eclipseQ  int64
}

// diskRasterEntry holds the last rasterized grid for one body and the
// key it was computed for. grid is (2r+1)×(2r+1), indexed
// (dy+r)*(2r+1)+(dx+r) — the same (dx, dy) offsets FillTexturedDiskTagged
// feeds the texture closure.
type diskRasterEntry struct {
	key  diskRasterKey
	r    int
	grid []lipgloss.Color
}

// texturedDiskRaster wraps tex (the freshly-built per-frame BodyTexture
// closure for body b) with the #363 raster cache. On a key hit it
// returns a closure that reads the cached grid — no trig, no
// feature-polygon tests, no Shade() hex parsing. On a miss it returns a
// closure that calls tex as before AND records each result into a fresh
// grid, so the cache is populated for free during the very pass that
// draws the frame (no duplicated raster work on a miss).
//
// Bodies are cached independently (keyed by ID) rather than in one
// single-slot cache, so a frame that textures more than one body (e.g.
// the Sun plus the focused planet) doesn't thrash a shared slot.
// systemIdx rides along in the key (not just the map's b.ID key) because
// user-overlay catalogs can reuse a body ID across systems (review
// finding) — without it, switching systems could serve one system's
// cached colors onto another system's body of the same ID. Including it
// in diskRasterKey is enough: an equal-ID, different-systemIdx entry
// simply compares unequal and is correctly treated as a miss.
func (v *OrbitView) texturedDiskRaster(systemIdx int, b bodies.CelestialBody, r int, subLat, subLon, upX, upY float64, light *render.SolarLight, tex render.BodyTexture) render.BodyTexture {
	if tex == nil {
		return nil
	}
	key := diskRasterKey{
		systemIdx: systemIdx,
		bodyID:    b.ID,
		pxRadius:  r,
		subLatQ:   quantize(subLat, diskRasterQuantDeg),
		subLonQ:   quantize(subLon, diskRasterQuantDeg),
		upAngleQ:  quantize(math.Atan2(upY, upX)*180.0/math.Pi, diskRasterQuantDeg),
	}
	if light != nil {
		key.hasLight = true
		key.lightLatQ = quantize(light.SubSolarLatDeg, diskRasterQuantDeg)
		key.lightLonQ = quantize(light.SubSolarLonDeg, diskRasterQuantDeg)
		key.eclipseQ = quantize(light.EclipseFactor, diskRasterQuantEclipse)
	}

	if v.diskRasterCache == nil {
		v.diskRasterCache = make(map[string]*diskRasterEntry)
	}
	side := 2*r + 1

	if entry, ok := v.diskRasterCache[b.ID]; ok && entry.key == key && entry.r == r {
		v.diskRasterCacheHits++
		grid := entry.grid
		// Self-healing hit path (fix for a viewport-clip bug caught in
		// review): FillTexturedDiskTagged never calls this closure for
		// an off-canvas offset (it clips BEFORE calling texture()), so
		// a grid built on one frame is only guaranteed complete for the
		// offsets that were actually on-canvas THAT frame. A later
		// identical-key frame — same body/radius/lighting, but panned
		// or resized so a previously-clipped offset is now on-canvas —
		// would otherwise read the zero value (lipgloss.Color(""))
		// for that offset and paint an uncolored cell. Since an equal
		// key guarantees identical colors regardless of when an offset
		// is first requested, it's always correct to lazily fill any
		// gap on the hit path too: the grid converges to the union of
		// every window this key has ever drawn.
		return func(dx, dy, rr int) lipgloss.Color {
			idx := (dy+r)*side + (dx + r)
			if idx < 0 || idx >= len(grid) {
				// Defensive: should be unreachable since the caller
				// always uses the same (r, mask) that built the grid.
				return tex(dx, dy, rr)
			}
			if c := grid[idx]; c != "" {
				return c
			}
			c := tex(dx, dy, rr)
			grid[idx] = c
			return c
		}
	}

	grid := make([]lipgloss.Color, side*side)
	v.diskRasterCache[b.ID] = &diskRasterEntry{key: key, r: r, grid: grid}
	v.diskRasterCacheComputes++
	return func(dx, dy, rr int) lipgloss.Color {
		c := tex(dx, dy, rr)
		idx := (dy+r)*side + (dx + r)
		if idx >= 0 && idx < len(grid) {
			grid[idx] = c
		}
		return c
	}
}
