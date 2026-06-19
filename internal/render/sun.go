package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Solar palette. Retained as named colors; the Sun's surface (limb
// darkening + sunspots) is now data-driven from sol.json via the
// generic `star` texture kind (ADR 0024 PR4), replacing the
// SunPixelColor shader. ColorSunCorona is still used by the corona
// halo renderer.
const (
	ColorSunCore    = lipgloss.Color("#FFF6C8") // bright yellow-white center
	ColorSunSurface = lipgloss.Color("#FFD050") // saturated solar yellow
	ColorSunLimb    = lipgloss.Color("#E89020") // darker orange-yellow limb
	ColorSunCorona  = lipgloss.Color("#FFE070") // faint corona halo
	ColorSunSpot    = lipgloss.Color("#A06030") // dark sunspot
)
