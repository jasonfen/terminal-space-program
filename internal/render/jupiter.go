package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Jupiter-surface palette. Retained as named colors; the banded cloud
// texture + Great Red Spot are now data-driven from sol.json (ADR 0024
// PR4), replacing the JupiterPixelColor shader.
const (
	ColorJupiterZone = lipgloss.Color("#D7B98C") // bright zone (warm)
	ColorJupiterBelt = lipgloss.Color("#8B6240") // dark belt (warm)
	ColorJupiterPole = lipgloss.Color("#7A6450") // muted polar haze
	ColorJupiterGRS  = lipgloss.Color("#A03A28") // Great Red Spot
)
