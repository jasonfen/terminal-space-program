package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// BodyInfo is the full-screen detail view for a single celestial body.
// Entered via `i` from OrbitView; `esc` (handled at App level) returns.
type BodyInfo struct {
	theme Theme
}

func NewBodyInfo(th Theme) *BodyInfo { return &BodyInfo{theme: th} }

// Render displays the selected body's physical and orbital data.
func (b *BodyInfo) Render(w *sim.World, selectedIdx, cols, rows int) string {
	sys := w.System()
	if selectedIdx < 0 || selectedIdx >= len(sys.Bodies) {
		return b.theme.Dim.Render("no body selected") + "\n"
	}
	cb := sys.Bodies[selectedIdx]

	titleStyle := lipgloss.NewStyle().Foreground(render.ColorFor(cb)).Bold(true)
	sections := []string{
		titleStyle.Render(cb.EnglishName),
		b.theme.Dim.Render(fmt.Sprintf("%s in %s", cb.BodyType, sys.Name)),
		"",
		b.theme.Primary.Render("PHYSICAL"),
		fmt.Sprintf("  Mean radius:     %.1f km", cb.MeanRadius),
		fmt.Sprintf("  Mass:            %.3e kg", cb.MassKg()),
		fmt.Sprintf("  Gravity:         %.3f m/s²", cb.Gravity),
		fmt.Sprintf("  Escape velocity: %.2f km/s", cb.Escape),
		fmt.Sprintf("  Density:         %.3f g/cm³", cb.Density),
	}

	if cb.SemimajorAxis > 0 {
		auVal := cb.SemimajorAxisMeters() / bodies.AU
		peri := cb.SemimajorAxisMeters() * (1 - cb.Eccentricity) / bodies.AU
		apo := cb.SemimajorAxisMeters() * (1 + cb.Eccentricity) / bodies.AU
		sections = append(sections,
			"",
			b.theme.Primary.Render("ORBIT"),
			fmt.Sprintf("  Semimajor axis:  %.4f AU  (%.3e m)", auVal, cb.SemimajorAxisMeters()),
			fmt.Sprintf("  Perihelion:      %.4f AU", peri),
			fmt.Sprintf("  Aphelion:        %.4f AU", apo),
			fmt.Sprintf("  Eccentricity:    %.5f", cb.Eccentricity),
			fmt.Sprintf("  Inclination:     %.3f°", cb.Inclination),
			fmt.Sprintf("  Ω (LAN):         %.3f°", cb.LongitudeOfAscendingNode),
			fmt.Sprintf("  ω (arg peri):    %.3f°", cb.ArgumentOfPeriapsis),
			fmt.Sprintf("  Sideral period:  %.3f days", cb.SideralOrbit),
			fmt.Sprintf("  Sideral rot.:    %.3f h", cb.SideralRotation),
		)
	}

	if cb.StellarClass != "" {
		sections = append(sections,
			"",
			b.theme.Primary.Render("STELLAR"),
			fmt.Sprintf("  Class:       %s", cb.StellarClass),
			fmt.Sprintf("  Temperature: %.0f K", cb.Temperature),
		)
		if cb.Age > 0 {
			sections = append(sections, fmt.Sprintf("  Age:         %.2e years", cb.Age))
		}
	}

	if len(cb.Moons) > 0 {
		sections = append(sections, "", b.theme.Primary.Render("MOONS"))
		for _, m := range cb.Moons {
			sections = append(sections, "  - "+m.EnglishName)
		}
	}

	if cb.DiscoveredBy != "" {
		sections = append(sections,
			"",
			b.theme.Dim.Render(fmt.Sprintf("Discovered by %s (%s)", cb.DiscoveredBy, cb.DiscoveryDate)),
		)
	}

	footer := b.theme.Footer.Render("[esc] back  [←/→] prev/next body  [q] quit")
	return strings.Join(sections, "\n") + "\n\n" + footer
}
