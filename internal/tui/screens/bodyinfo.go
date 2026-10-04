package screens

import (
	"fmt"

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

// bodyInfoLegend is the key legend on the frame's bottom edge.
// h/l are the keys that actually step the body cursor here
// (Keymap.PrevBody / NextBody). shift+←/→ pan the map behind this panel and
// q is radial+, so neither belongs in this footer (#423). t/H/P act on
// the body shown: t targets it directly, H and P plan to the Target (#495).
// They sit in an ACTIONS box rather than on the bottom edge: a "target: Mars"
// flash overlays that edge and must not hide the keys that caused it.
const bodyInfoLegend = "[h/l] prev/next body · [esc] back"

// Render displays the selected body's physical and orbital data inside the
// shared form frame (B11 / G9 Q4): BODY, PHYSICAL (and STELLAR / MOONS when
// they apply) on the left, ORBIT on the right. cols x rows is the whole
// framed block (the App's Title Row sits above it).
func (b *BodyInfo) Render(w *sim.World, selectedIdx, cols, rows int) string {
	sys := w.System()
	if selectedIdx < 0 || selectedIdx >= len(sys.Bodies) {
		return formFrame(b.theme, formBox(b.theme, "BODY", []string{b.theme.Dim.Render("  no body selected")}, cols-2*frameInset),
			cols, rows, b.theme.Footer.Render("[esc] back"))
	}
	cb := sys.Bodies[selectedIdx]
	inner := cols - 2*frameInset
	lw := (inner - 1) / 2
	rw := inner - lw - 1

	titleStyle := lipgloss.NewStyle().Foreground(render.ColorFor(cb)).Bold(true)
	source := sys.Source
	if source == "" {
		source = "embedded"
	}
	left := formBox(b.theme, "BODY", []string{
		"  " + titleStyle.Render(cb.EnglishName),
		"  " + b.theme.Dim.Render(fmt.Sprintf("%s in %s (source: %s)", cb.BodyType, sys.Name, source)),
	}, lw)
	left = append(left, formBox(b.theme, "PHYSICAL", []string{
		fmt.Sprintf("  Mean radius:     %.1f km", cb.MeanRadius),
		fmt.Sprintf("  Mass:            %.3e kg", cb.MassKg()),
		fmt.Sprintf("  Gravity:         %.3f m/s²", cb.Gravity),
		fmt.Sprintf("  Escape velocity: %.2f km/s", cb.Escape),
		fmt.Sprintf("  Density:         %.3f g/cm³", cb.Density),
	}, lw)...)

	var right []string
	if cb.SemimajorAxis > 0 {
		auVal := cb.SemimajorAxisMeters() / bodies.AU
		peri := cb.SemimajorAxisMeters() * (1 - cb.Eccentricity) / bodies.AU
		apo := cb.SemimajorAxisMeters() * (1 + cb.Eccentricity) / bodies.AU
		right = append(right, formBox(b.theme, "ORBIT", []string{
			fmt.Sprintf("  Semimajor axis:  %.4f AU  (%.3e m)", auVal, cb.SemimajorAxisMeters()),
			fmt.Sprintf("  Perihelion:      %.4f AU", peri),
			fmt.Sprintf("  Aphelion:        %.4f AU", apo),
			fmt.Sprintf("  Eccentricity:    %.5f", cb.Eccentricity),
			fmt.Sprintf("  Inclination:     %.3f°", cb.Inclination),
			fmt.Sprintf("  Ω (LAN):         %.3f°", cb.LongitudeOfAscendingNode),
			fmt.Sprintf("  ω (arg peri):    %.3f°", cb.ArgumentOfPeriapsis),
			fmt.Sprintf("  Sideral period:  %.3f days", cb.SideralOrbit),
			fmt.Sprintf("  Sideral rot.:    %.3f h", cb.SideralRotation),
		}, rw)...)
	}

	if cb.StellarClass != "" {
		ls := []string{
			fmt.Sprintf("  Class:       %s", cb.StellarClass),
			fmt.Sprintf("  Temperature: %.0f K", cb.Temperature),
		}
		if cb.Age > 0 {
			ls = append(ls, fmt.Sprintf("  Age:         %.2e years", cb.Age))
		}
		left = append(left, formBox(b.theme, "STELLAR", ls, lw)...)
	}

	if len(cb.Moons) > 0 {
		var ls []string
		for _, m := range cb.Moons {
			ls = append(ls, "  - "+m.EnglishName)
		}
		right = append(right, formBox(b.theme, "MOONS", ls, rw)...)
	}

	if cb.DiscoveredBy != "" {
		right = append(right, formBox(b.theme, "DISCOVERY", []string{
			"  " + b.theme.Dim.Render(fmt.Sprintf("Discovered by %s (%s)", cb.DiscoveredBy, cb.DiscoveryDate)),
		}, rw)...)
	}

	right = append(right, formBox(b.theme, "ACTIONS", []string{
		"  [t] target  [H] transfer  [P] porkchop",
	}, rw)...)

	return formFrame(b.theme, joinBoxes(left, right, lw, 1), cols, rows, b.theme.Footer.Render(bodyInfoLegend))
}
