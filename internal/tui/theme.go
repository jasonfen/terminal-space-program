package tui

import "github.com/charmbracelet/lipgloss"

// Theme collects the lipgloss styles used across screens. Palette matches
// docs/plan.md §Visual polish: primary cyan, warning amber, alert red,
// dim gray for orbits.
type Theme struct {
	Primary lipgloss.Style
	Warning lipgloss.Style
	Alert   lipgloss.Style
	Dim     lipgloss.Style
	HUDBox  lipgloss.Style
	Footer  lipgloss.Style
	Title   lipgloss.Style
}

func DefaultTheme() Theme {
	cyan := lipgloss.Color("#5FD7FF")
	amber := lipgloss.Color("#FFAF00")
	red := lipgloss.Color("#FF5F5F")
	gray := lipgloss.Color("#5F5F5F")
	return Theme{
		Primary: lipgloss.NewStyle().Foreground(cyan),
		Warning: lipgloss.NewStyle().Foreground(amber),
		Alert:   lipgloss.NewStyle().Foreground(red),
		Dim:     lipgloss.NewStyle().Foreground(gray),
		HUDBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cyan).
			Padding(0, 1),
		Footer: lipgloss.NewStyle().Foreground(gray).Italic(true),
		Title:  lipgloss.NewStyle().Foreground(cyan).Bold(true),
	}
}
