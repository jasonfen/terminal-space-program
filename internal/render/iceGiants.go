package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Ice-giant palette. Uranus reads as a featureless pale cyan to
// most amateur scopes and to Voyager 2's flyby; Neptune is a
// deeper blue with subtle bands and the iconic Great Dark Spot
// (which itself comes and goes — Voyager 2 saw one in 1989, HST
// saw it dissipate, then a new one appear in 2018). v0.8.5+
// renders these as banded disks with a single dark-spot accent
// on Neptune; the player's eye can tell them apart at small
// pxRadius.
const (
	ColorUranusBase    = lipgloss.Color("#A8D8E0") // pale cyan (matches palette)
	ColorUranusBand    = lipgloss.Color("#8FC4D0") // very subtle banding
	ColorUranusPole    = lipgloss.Color("#C0E0E8") // brighter polar haze (Uranus's pole-on view)

	ColorNeptuneBase  = lipgloss.Color("#3A6FB8") // deep methane blue
	ColorNeptuneBand  = lipgloss.Color("#2A5494") // darker band
	ColorNeptuneCloud = lipgloss.Color("#7AA4D8") // bright cirrus / scooter
	ColorNeptuneSpot  = lipgloss.Color("#1F3A6E") // Great Dark Spot
)

var uranusBands = []struct {
	latMin, latMax float64
	color          lipgloss.Color
}{
	// Uranus's axial tilt (98°) means the visible banding is gentle
	// when seen in our equator-on convention, but the polar haze
	// still matters at high lats.
	{-90, -60, ColorUranusPole},
	{-60, -25, ColorUranusBand},
	{-25, 25, ColorUranusBase},
	{25, 60, ColorUranusBand},
	{60, 90, ColorUranusPole},
}

var neptuneBands = []struct {
	latMin, latMax float64
	color          lipgloss.Color
}{
	{-90, -60, ColorNeptuneBand},
	{-60, -30, ColorNeptuneBase},
	{-30, -10, ColorNeptuneBand},  // Southern dark band
	{-10, 10, ColorNeptuneBase},   // Equatorial
	{10, 35, ColorNeptuneCloud},   // Bright "scooter" latitude
	{35, 60, ColorNeptuneBand},
	{60, 90, ColorNeptuneBase},
}

// neptuneDarkSpot is the GDS analog — large oval at lat ≈ -22°
// (Voyager 2's GDS sat near there). Wanders zonally on the real
// planet; static here for v0.8.5.
var neptuneDarkSpot = continentEllipse{
	lat: -22, lon: 30, aLat: 8, aLon: 18, color: ColorNeptuneSpot,
}

// UranusPixelColor — pale cyan base with very subtle latitudinal
// banding. v0.8.5.7+ takes the full sub-observer point. With
// Uranus's 97° axial tilt populated in sol.json, the banded
// pattern reads pole-on from "top of orbit" views — the iconic
// "rolling along its orbit" geometry.
func UranusPixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg, screenUpX, screenUpY float64) lipgloss.Color {
	lat, _, _ := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg, screenUpX, screenUpY)
	color := ColorUranusBase
	for _, b := range uranusBands {
		if lat >= b.latMin && lat < b.latMax {
			color = b.color
			break
		}
	}
	return color
}

// NeptunePixelColor — deeper methane blue with stronger banding +
// Great Dark Spot accent. v0.8.5.7+.
func NeptunePixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg, screenUpX, screenUpY float64) lipgloss.Color {
	lat, lon, ok := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg, screenUpX, screenUpY)
	color := ColorNeptuneBase
	for _, b := range neptuneBands {
		if lat >= b.latMin && lat < b.latMax {
			color = b.color
			break
		}
	}
	if ok && inEllipse(lat, lon, neptuneDarkSpot) {
		color = ColorNeptuneSpot
	}
	return color
}
