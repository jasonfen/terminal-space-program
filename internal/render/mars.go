package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Mars-surface palette. Retained as named colors; the Mars surface
// texture (dark/bright albedo features + polar caps) is now data-driven
// from sol.json (ADR 0024 PR4), replacing the MarsPixelColor shader.
const (
	ColorMarsRust   = lipgloss.Color("#B7553A") // base regolith
	ColorMarsDark   = lipgloss.Color("#7A3422") // dark albedo features
	ColorMarsBright = lipgloss.Color("#D9A07A") // bright high-albedo (Arabia)
	ColorMarsIce    = lipgloss.Color("#F0E8E0") // polar caps (CO₂ frost)
)
