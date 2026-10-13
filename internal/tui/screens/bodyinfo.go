package screens

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
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
		"  Mean radius:     " + readout.Distance(cb.MeanRadius*1000),
		"  Mass:            " + readout.MassSci(cb.MassKg()),
		"  Gravity:         " + bodyGravity(cb),
		"  Escape velocity: " + bodyEscape(cb),
		"  Density:         " + bodyDensity(cb),
	}, lw)...)

	var right []string
	if cb.SemimajorAxis > 0 {
		peri := cb.SemimajorAxisMeters() * (1 - cb.Eccentricity)
		apo := cb.SemimajorAxisMeters() * (1 + cb.Eccentricity)
		// A moon's closest/farthest points are around its planet, so the
		// Sun's words (perihelion/aphelion) become periapsis/apoapsis.
		nearWord, farWord := "Perihelion:", "Aphelion:"
		if par := sys.ParentOf(cb); par != nil && par.BodyType != "Star" {
			nearWord, farWord = "Periapsis:", "Apoapsis:"
		}
		right = append(right, formBox(b.theme, "ORBIT", []string{
			"  Semimajor axis:  " + readout.Distance(cb.SemimajorAxisMeters()),
			"  " + padCells(nearWord, 17) + readout.Distance(peri),
			"  " + padCells(farWord, 17) + readout.Distance(apo),
			fmt.Sprintf("  Eccentricity:    %.5f", cb.Eccentricity),
			fmt.Sprintf("  Inclination:     %.3f°", cb.Inclination),
			fmt.Sprintf("  Ω (LAN):         %.3f°", cb.LongitudeOfAscendingNode),
			fmt.Sprintf("  ω (arg peri):    %.3f°", cb.ArgumentOfPeriapsis),
			fmt.Sprintf("  Sidereal period: %.3f days", cb.SideralOrbit),
			fmt.Sprintf("  Sidereal rot.:   %.3f h", cb.SideralRotation),
		}, rw)...)
	}

	if cb.StellarClass != "" {
		ls := []string{
			fmt.Sprintf("  Class:       %s", cb.StellarClass),
			fmt.Sprintf("  Temperature: %.0f K", cb.Temperature),
		}
		if cb.Age > 0 {
			ls = append(ls, "  Age:         "+readout.Years(cb.Age))
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

// bodyGravity is the surface gravity row: the catalog value when it has one,
// else GM/r² from mass and radius, else "--" (#617). The catalog leaves it
// out for every Sol moon and most exoplanet bodies; zero means "not given".
func bodyGravity(cb bodies.CelestialBody) string {
	if cb.Gravity > 0 {
		return readout.Accel(cb.Gravity)
	}
	r := cb.RadiusMeters()
	if gm := cb.GravitationalParameter(); gm > 0 && r > 0 {
		return readout.Accel(gm / (r * r))
	}
	return "--"
}

// bodyEscape is the escape velocity row in km/s: catalog value, else
// sqrt(2GM/r), else "--".
func bodyEscape(cb bodies.CelestialBody) string {
	v := cb.Escape
	if v <= 0 {
		r := cb.RadiusMeters()
		gm := cb.GravitationalParameter()
		if gm <= 0 || r <= 0 {
			return "--"
		}
		v = math.Sqrt(2*gm/r) / 1000
	}
	if v < 0.1 {
		return fmt.Sprintf("%.3f km/s", v)
	}
	return fmt.Sprintf("%.2f km/s", v)
}

// bodyDensity is the mean density row in g/cm³: catalog value, else
// M / (4/3 π r³), else "--".
func bodyDensity(cb bodies.CelestialBody) string {
	if cb.Density > 0 {
		return fmt.Sprintf("%.3f g/cm³", cb.Density)
	}
	r := cb.RadiusMeters()
	if m := cb.MassKg(); m > 0 && r > 0 {
		return fmt.Sprintf("%.3f g/cm³", m/(4.0/3.0*math.Pi*r*r*r)/1000)
	}
	return "--"
}
