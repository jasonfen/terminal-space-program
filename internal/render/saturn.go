package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Saturn-surface palette. Retained as named colors; the banded cloud
// texture (incl. the polar-hexagon band) is now data-driven from
// sol.json (ADR 0024 PR4), replacing the SaturnPixelColor shader.
const (
	ColorSaturnZone = lipgloss.Color("#E8D9A8") // bright zone (warm pale gold)
	ColorSaturnBelt = lipgloss.Color("#B89968") // dark belt (muted ochre)
	ColorSaturnPole = lipgloss.Color("#9A8458") // muted polar haze, slightly darker
	ColorSaturnSpot = lipgloss.Color("#D9B070") // hexagonal storm + occasional bright ovals
)
