// Package screens implements the individual tea.Model screens composed by
// tui.App: OrbitView (C8), BodyInfo (C9), Maneuver (C20), Help (C9).
package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// Theme is the subset of styles OrbitView needs. Passed in from tui.App.
type Theme struct {
	Primary, Warning, Alert, Dim, HUDBox, Footer, Title lipgloss.Style
}

// OrbitView renders the system's bodies, orbit lines, and a right-side HUD.
// Phase 0 — no spacecraft yet; C16 will add a glyph + augmented HUD.
type OrbitView struct {
	canvas *widgets.Canvas
	theme  Theme

	// lastSystemIdx tracks the system the canvas was last fit to, so we
	// re-FitTo only on a real switch (not every frame).
	lastSystemIdx int
	fitted        bool
}

// NewOrbitView constructs the screen with an initially-small canvas; a
// Resize call from the root model sizes it to the terminal.
func NewOrbitView(th Theme) *OrbitView {
	return &OrbitView{
		canvas:        widgets.NewCanvas(80, 24),
		theme:         th,
		lastSystemIdx: -1,
	}
}

// Resize forwards terminal dimensions to the canvas. Left ~70% for canvas,
// right ~30% for HUD per plan §Screen details.
func (v *OrbitView) Resize(totalCols, totalRows int) {
	canvasCols := totalCols * 7 / 10
	if canvasCols < 20 {
		canvasCols = 20
	}
	v.canvas.Resize(canvasCols, totalRows-4) // reserve 4 rows for header/footer
	v.fitted = false                          // force refit after resize
}

// ZoomIn / ZoomOut are thin wrappers for App to call on +/-.
func (v *OrbitView) ZoomIn()  { v.canvas.ZoomBy(1.25) }
func (v *OrbitView) ZoomOut() { v.canvas.ZoomBy(1.0 / 1.25) }

// Render composes the frame: canvas on the left, HUD on the right.
// selectedIdx is the index of the cursor-selected body in system.Bodies.
func (v *OrbitView) Render(w *sim.World, selectedIdx int, totalCols, totalRows int) string {
	sys := w.System()

	if v.lastSystemIdx != w.SystemIdx || !v.fitted {
		v.lastSystemIdx = w.SystemIdx
		v.fitted = true
		v.autoFit(sys)
	}

	v.canvas.Clear()
	v.canvas.Center(orbital.Vec3{}) // primary at origin

	// Dotted orbit ellipses for each body with a nonzero semimajor axis.
	for i := range sys.Bodies {
		b := sys.Bodies[i]
		if b.SemimajorAxis == 0 {
			continue
		}
		el := orbital.ElementsFromBody(b)
		v.canvas.DrawEllipseDotted(el, 360, 6)
	}

	// Plot each body (heavier mark for selected).
	for i := range sys.Bodies {
		b := sys.Bodies[i]
		pos := w.BodyPosition(b)
		v.canvas.Plot(pos)
		if i == selectedIdx {
			// Nudge four neighbors so selection stands out on a sparse braille grid.
			nudges := []orbital.Vec3{
				{X: 1 / v.canvas.Scale()},
				{X: -1 / v.canvas.Scale()},
				{Y: 1 / v.canvas.Scale()},
				{Y: -1 / v.canvas.Scale()},
			}
			for _, n := range nudges {
				v.canvas.Plot(pos.Add(n))
			}
		}
	}

	canvasStr := v.canvas.String()
	canvasPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.theme.Primary.GetForeground()).
		Render(canvasStr)

	hud := v.renderHUD(w, selectedIdx, totalCols-v.canvas.Cols()-4)

	title := v.theme.Title.Render(fmt.Sprintf("terminal-space-program — %s", sys.Name))
	footer := v.theme.Footer.Render(
		"[q] quit  [s] next system  [←/→] body  [+/-] zoom  [i] info  [?] help  [.,] warp  [0] pause",
	)

	body := lipgloss.JoinHorizontal(lipgloss.Top, canvasPanel, hud)
	return title + "\n" + body + "\n" + footer
}

// autoFit sets the canvas scale so the outermost body's apoapsis is visible.
func (v *OrbitView) autoFit(sys bodies.System) {
	var maxR float64
	for _, b := range sys.Bodies {
		r := b.SemimajorAxisMeters() * (1 + b.Eccentricity)
		if r > maxR {
			maxR = r
		}
	}
	if maxR == 0 {
		maxR = 1e11 // 1/7 AU fallback
	}
	v.canvas.FitTo(maxR)
}

func (v *OrbitView) renderHUD(w *sim.World, selectedIdx int, width int) string {
	if width < 20 {
		width = 20
	}
	sys := w.System()

	lines := []string{
		v.theme.Primary.Render("SYSTEM"),
		"  " + sys.Name,
		"  " + fmt.Sprintf("%d bodies", len(sys.Bodies)),
		"",
		v.theme.Primary.Render("CLOCK"),
		"  T+" + w.Clock.SimTime.Format("2006-01-02"),
		"  " + fmt.Sprintf("warp: %.0fx", w.Clock.Warp()),
	}
	if w.Clock.Paused {
		lines = append(lines, "  "+v.theme.Warning.Render("[PAUSED]"))
	}
	lines = append(lines, "")
	lines = append(lines, v.theme.Primary.Render("SELECTED"))

	if selectedIdx >= 0 && selectedIdx < len(sys.Bodies) {
		b := sys.Bodies[selectedIdx]
		lines = append(lines,
			"  "+b.EnglishName,
			"  "+b.BodyType,
		)
		if b.SemimajorAxis > 0 {
			auVal := b.SemimajorAxisMeters() / bodies.AU
			lines = append(lines, fmt.Sprintf("  a: %.3f AU", auVal))
			lines = append(lines, fmt.Sprintf("  e: %.4f", b.Eccentricity))
			lines = append(lines, fmt.Sprintf("  T: %.1f d", b.SideralOrbit))
		} else {
			lines = append(lines, v.theme.Dim.Render("  (primary)"))
		}
	}

	content := strings.Join(lines, "\n")
	return v.theme.HUDBox.Width(width).Render(content)
}
