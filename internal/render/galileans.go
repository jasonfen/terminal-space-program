package render

import (
	"math"

	"github.com/charmbracelet/lipgloss"
)

// Galilean-moon palette. Retained as named colors; the per-moon
// surfaces (Io's paterae, Europa's lineae, Ganymede's regiones,
// Callisto's craters) are now data-driven from sol.json (ADR 0024 PR4),
// replacing the Io/Europa/Ganymede/CallistoPixelColor shaders. The
// shared orthographic projection (projectPixelToLatLon) below outlived
// them — every texture kind uses it.
const (
	// Io — sulfur-yellow base, dark patera deposits, fresh-flow orange.
	ColorIoBase   = lipgloss.Color("#E8D940") // sulfurous yellow (matches palette entry)
	ColorIoPatera = lipgloss.Color("#7A4A20") // dark volcanic deposits
	ColorIoFresh  = lipgloss.Color("#E07530") // fresh flow orange

	// Europa — bright water ice base with dark linear cracks (lineae).
	ColorEuropaIce  = lipgloss.Color("#E5DBC6") // pale ice
	ColorEuropaLine = lipgloss.Color("#9A6F4A") // brown linea (cryomagma stains)

	// Ganymede — bright young grooved terrain vs. dark ancient terrain.
	ColorGanymedeBright = lipgloss.Color("#C8B498") // grooved terrain
	ColorGanymedeDark   = lipgloss.Color("#6E5A3E") // ancient cratered terrain
	ColorGanymedeRay    = lipgloss.Color("#E0D2B0") // fresh impact ejecta

	// Callisto — uniformly dark, heavily cratered, with bright rays.
	ColorCallistoBase   = lipgloss.Color("#5C4A36") // dark base
	ColorCallistoCrater = lipgloss.Color("#9A8260") // bright crater rim / ray
)

// projectPixelToLatLon does the orthographic dx,dy → (body lat,
// body lon) transform with arbitrary sub-observer point and screen-
// up orientation. v0.8.5.7+ generalised sub-observer latitude;
// v0.11.2+ adds screenUpX/Y so the painter rotates the texture into
// the body's physical north/east frame for any view (ADR 0003).
// Returns (lat, absLon, ok); ok is false when the pixel is outside
// the visible disk or the longitude is degenerate (sub-observer at
// the body's pole).
//
// (screenUpX, screenUpY) is the unit vector — in canvas frame, where
// canvas-X is right and canvas-Y is up — pointing in the direction
// body-local-north at the sub-observer projects on the screen. For
// ViewTop on an untilted body this is (0, 1) — body-north is screen-
// up — and the math reduces to the v0.8.5.7 form. For ViewTop on
// Earth it is approximately (1, 0): the 23.44° tilt rotates body-
// north to canvas-right. Pole-on views (the sub-observer point at
// the body's pole) collapse north's direction; callers pass (0, 1)
// as a stable fallback since the longitude is undefined there anyway.
//
// Math: rotate the canvas (nx_can, ny_can) into the body's local
// (east, north) basis at the sub-observer point:
//   east_can  = ( screenUpY, −screenUpX)   (90° CW from north_can)
//   north_can = ( screenUpX,  screenUpY)
//   nx_body   = (nx_can, ny_can) · east_can
//   ny_body   = (nx_can, ny_can) · north_can
// Then apply the standard inverse orthographic projection (Snyder
// 1987 §20, "Orthographic Projection - inverse formulas"):
//
//	z      = sqrt(1 - nx² - ny²)              (out-of-screen, toward camera)
//	body_z = sin(φ₀)·z + cos(φ₀)·ny           (body-frame along spin axis)
//	body_x = cos(φ₀)·cos(λ₀)·z − sin(λ₀)·nx − sin(φ₀)·cos(λ₀)·ny
//	body_y = cos(φ₀)·sin(λ₀)·z + cos(λ₀)·nx − sin(φ₀)·sin(λ₀)·ny
//	lat    = asin(body_z)
//	lon    = atan2(body_y, body_x)
func projectPixelToLatLon(dx, dy, pxRadius int, subLatDeg, subLonDeg, screenUpX, screenUpY float64) (lat, absLon float64, ok bool) {
	if pxRadius < 1 {
		return 0, 0, false
	}
	nxCan := float64(dx) / float64(pxRadius)
	// v0.8.5.7 fix: canvas uses screen-Y-down (dy > 0 = below body
	// center), but the orthographic projection wants nyCan > 0 to be
	// screen-up (north toward sub-observer).
	nyCan := -float64(dy) / float64(pxRadius)
	// Rotate canvas frame into body's (east, north) frame at the
	// sub-observer point. east = 90° CW from north when looking at
	// the screen with depth toward camera (right-handed local frame
	// at the surface: east × north = up = +camDir).
	nx := nxCan*screenUpY - nyCan*screenUpX
	ny := nxCan*screenUpX + nyCan*screenUpY
	r2 := nx*nx + ny*ny
	if r2 > 1 {
		// Outside the disk — caller should clip first, but keep the
		// math safe under rounding.
		s := 1.0 / math.Sqrt(r2)
		nx *= s
		ny *= s
		r2 = 1
	}
	z := math.Sqrt(1 - r2)

	phi0 := subLatDeg * math.Pi / 180.0
	lam0 := subLonDeg * math.Pi / 180.0
	sP, cP := math.Sin(phi0), math.Cos(phi0)
	sL, cL := math.Sin(lam0), math.Cos(lam0)

	bodyZ := sP*z + cP*ny
	if bodyZ > 1 {
		bodyZ = 1
	} else if bodyZ < -1 {
		bodyZ = -1
	}
	lat = math.Asin(bodyZ) * 180.0 / math.Pi

	bodyX := cP*cL*z - sL*nx - sP*cL*ny
	bodyY := cP*sL*z + cL*nx - sP*sL*ny
	if math.Abs(bodyX) < 1e-9 && math.Abs(bodyY) < 1e-9 {
		// Degenerate — pixel is at the body's pole, longitude undefined.
		return lat, 0, false
	}
	absLon = math.Atan2(bodyY, bodyX) * 180.0 / math.Pi
	for absLon > 180 {
		absLon -= 360
	}
	for absLon <= -180 {
		absLon += 360
	}
	return lat, absLon, true
}
