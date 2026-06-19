package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Lunar-surface palette. Retained as named colors for the HUD/label
// chain; the Moon's surface texture (maria + bright rayed craters) is
// now data-driven from sol.json (ADR 0024 PR4) rather than the
// MoonPixelColor Go shader this file used to hold.
const (
	ColorMoonHighland = lipgloss.Color("#BFB8AA") // warm grey-tan regolith
	ColorMoonMare     = lipgloss.Color("#4A4A55") // darker basalt for contrast
	ColorMoonRay      = lipgloss.Color("#EFEAE0") // bright ejecta, slightly warm
)
