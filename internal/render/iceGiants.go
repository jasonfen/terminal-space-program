package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Ice-giant palette. Retained as named colors; the banded disks (and
// Neptune's Great Dark Spot) are now data-driven from sol.json
// (ADR 0024 PR4), replacing the Uranus/NeptunePixelColor shaders.
const (
	ColorUranusBase = lipgloss.Color("#A8D8E0") // pale cyan (matches palette)
	ColorUranusBand = lipgloss.Color("#8FC4D0") // very subtle banding
	ColorUranusPole = lipgloss.Color("#C0E0E8") // brighter polar haze (Uranus's pole-on view)

	ColorNeptuneBase  = lipgloss.Color("#3A6FB8") // deep methane blue
	ColorNeptuneBand  = lipgloss.Color("#2A5494") // darker band
	ColorNeptuneCloud = lipgloss.Color("#7AA4D8") // bright cirrus / scooter
	ColorNeptuneSpot  = lipgloss.Color("#1F3A6E") // Great Dark Spot
)
