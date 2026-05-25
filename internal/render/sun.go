package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Solar palette. Limb darkening + a couple of sunspots distinguish
// the Sun from a flat colored disk; a faint two-ring corona halo
// extends the disk's visual presence at any zoom (the v0.7.x
// crosshair-style outline + center dot it replaced read as a
// target reticle, not a star).
const (
	ColorSunCore    = lipgloss.Color("#FFF6C8") // bright yellow-white center
	ColorSunSurface = lipgloss.Color("#FFD050") // saturated solar yellow
	ColorSunLimb    = lipgloss.Color("#E89020") // darker orange-yellow limb
	ColorSunCorona  = lipgloss.Color("#FFE070") // faint corona halo
	ColorSunSpot    = lipgloss.Color("#A06030") // dark sunspot
)

// sunSpots are scattered dark patches representing sunspots. The
// real Sun's spots wander with the solar cycle and rotate with the
// 25-day sidereal period; threading sim time through the texture
// pipeline (v0.8.5) means these positions rotate visibly in the
// renderer too.
var sunSpots = []continentEllipse{
	{12, -25, 2, 3, ColorSunSpot},
	{-15, 18, 2, 2, ColorSunSpot},
	{22, 60, 1, 2, ColorSunSpot},
	{-20, -55, 2, 3, ColorSunSpot},
}

// SunPixelColor returns the surface color for a pixel inside the
// Sun's disk. Concentric brightness bands approximate solar limb
// darkening (the Sun looks brightest at the centre of the disk
// and noticeably darker at the limb in real photos); a small set
// of sunspots layered on the mid-surface band adds character.
// v0.8.5.7+.
func SunPixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg, screenUpX, screenUpY float64) lipgloss.Color {
	if pxRadius < 1 {
		return ColorSunSurface
	}
	nx := float64(dx) / float64(pxRadius)
	ny := float64(dy) / float64(pxRadius)
	r2 := nx*nx + ny*ny
	switch {
	case r2 > 0.85:
		// Outer limb — Eddington-ish darkening.
		return ColorSunLimb
	case r2 > 0.45:
		// Mid-disk — solar surface, with sunspots layered on top.
		// Project to lat/lon for the spot lookup so spots track
		// the rotation phase set by SubObserverPointDeg.
		lat, absLon, ok := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg, screenUpX, screenUpY)
		if ok {
			for _, s := range sunSpots {
				if inEllipse(lat, absLon, s) {
					return ColorSunSpot
				}
			}
		}
		return ColorSunSurface
	default:
		// Inner bright core — center of disk reads near-white.
		return ColorSunCore
	}
}
