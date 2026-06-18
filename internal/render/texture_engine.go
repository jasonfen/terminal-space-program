package render

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// Data-driven body-texture engine (ADR 0024). A single generic shader
// consumes a bodies.Texture spec, replacing the per-body Go functions
// dispatched by `switch b.ID` in bodyTextureBase. PR1 renders the
// ellipse kinds (continents / craters / spots) and bands over a base
// color; mask / limb-tint / star kinds are carried in the schema but
// not yet drawn (ADR 0024 rollout PR3).
//
// The spec is compiled once per body per frame (hex → lipgloss.Color,
// ellipses → the render-package continentEllipse type) so the per-pixel
// closure does no parsing.

// craterDefaultBright is the floor/rim color a crater falls back to
// when its spec carries neither color nor rim — keeps a rayed crater
// visible against any base rather than rendering invisibly.
const craterDefaultBright = lipgloss.Color("#EFEAE0")

// craterRimInner is the normalized elliptical radius (0=center, 1=edge)
// above which a crater paints its rim color instead of its floor. Only
// applies when the crater carries a rim.
const craterRimInner = 0.55

type compiledBand struct {
	latMin, latMax float64
	color          lipgloss.Color
}

type compiledCrater struct {
	c      continentEllipse // center + semi-axes; color is the floor
	rim    lipgloss.Color
	hasRim bool
}

type compiledTexture struct {
	base       lipgloss.Color
	bands      []compiledBand
	continents []continentEllipse
	spots      []continentEllipse
	craters    []compiledCrater
}

// compileTexture turns a JSON texture spec into a per-pixel-ready form.
// fallbackBase is used when the spec's base color is empty (the body's
// display Color); if both are empty the engine still renders, defaulting
// to a neutral disk. Zero-radius ellipses are dropped (defensive — the
// loader validates user overlays, but embedded data or future kinds
// shouldn't be able to divide by zero).
func compileTexture(t *bodies.Texture, fallbackBase string) *compiledTexture {
	ct := &compiledTexture{base: parseColor(t.Base, parseColor(fallbackBase, ColorMoonHighland))}
	for _, b := range t.Bands {
		ct.bands = append(ct.bands, compiledBand{
			latMin: b.LatMin, latMax: b.LatMax,
			color: parseColor(b.Color, ct.base),
		})
	}
	for _, e := range t.Continents {
		if e.LatR <= 0 || e.LonR <= 0 {
			continue
		}
		ct.continents = append(ct.continents, toEllipse(e, ct.base))
	}
	for _, e := range t.Spots {
		if e.LatR <= 0 || e.LonR <= 0 {
			continue
		}
		ct.spots = append(ct.spots, toEllipse(e, ct.base))
	}
	for _, e := range t.Craters {
		if e.LatR <= 0 || e.LonR <= 0 {
			continue
		}
		floor := e.Color
		if floor == "" && e.Rim == "" {
			cr := compiledCrater{c: toEllipseColor(e, craterDefaultBright)}
			ct.craters = append(ct.craters, cr)
			continue
		}
		cr := compiledCrater{c: toEllipseColor(e, parseColor(floor, craterDefaultBright))}
		if e.Rim != "" {
			cr.rim = parseColor(e.Rim, cr.c.color)
			cr.hasRim = true
		}
		ct.craters = append(ct.craters, cr)
	}
	return ct
}

func toEllipse(e bodies.TextureEllipse, fallback lipgloss.Color) continentEllipse {
	return toEllipseColor(e, parseColor(e.Color, fallback))
}

func toEllipseColor(e bodies.TextureEllipse, color lipgloss.Color) continentEllipse {
	return continentEllipse{lat: e.Lat, lon: e.Lon, aLat: e.LatR, aLon: e.LonR, color: color}
}

// parseColor returns the hex string as a lipgloss.Color, or fallback
// when empty. The loader validates user-overlay hex; embedded data is
// covered by tests, so no per-pixel validation is needed here.
func parseColor(hex string, fallback lipgloss.Color) lipgloss.Color {
	if hex == "" {
		return fallback
	}
	return lipgloss.Color(hex)
}

// colorAt is the per-pixel shader. Mirrors the resolution order of the
// hand-written shaders: base → band (by latitude) → continents (last
// wins) → spots (last wins) → craters (top). Bands are applied off the
// latitude alone (valid even at a pole-on view, matching JupiterPixel-
// Color); the ellipse kinds are gated on a valid longitude.
func (ct *compiledTexture) colorAt(dx, dy, pxRadius int, subLatDeg, subLonDeg, screenUpX, screenUpY float64) lipgloss.Color {
	lat, lon, ok := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg, screenUpX, screenUpY)
	color := ct.base
	for _, b := range ct.bands {
		if lat >= b.latMin && lat < b.latMax {
			color = b.color
			break
		}
	}
	if !ok {
		return color
	}
	for _, c := range ct.continents {
		if inEllipse(lat, lon, c) {
			color = c.color
		}
	}
	for _, s := range ct.spots {
		if inEllipse(lat, lon, s) {
			color = s.color
		}
	}
	for _, cr := range ct.craters {
		if rr := ellipseNorm(lat, lon, cr.c); rr < 1.0 {
			if cr.hasRim && rr > craterRimInner {
				color = cr.rim
			} else {
				color = cr.c.color
			}
		}
	}
	return color
}

// ellipseNorm returns the squared normalized elliptical radius of
// (lat, lon) relative to ellipse c: < 1 inside, == 1 on the edge. Same
// metric inEllipse thresholds at 1.0, exposed so craters can shade an
// inner floor vs. outer rim.
func ellipseNorm(lat, lon float64, c continentEllipse) float64 {
	dlat := lat - c.lat
	dlon := lon - c.lon
	for dlon > 180 {
		dlon -= 360
	}
	for dlon < -180 {
		dlon += 360
	}
	return (dlat/c.aLat)*(dlat/c.aLat) + (dlon/c.aLon)*(dlon/c.aLon)
}
