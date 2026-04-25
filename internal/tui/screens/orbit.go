// Package screens implements the individual tea.Model screens composed by
// tui.App: OrbitView (C8), BodyInfo (C9), Maneuver (C20), Help (C9).
package screens

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
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

	// lastSystemIdx + lastFocus track the (system, focus) pair the canvas
	// was last fit to, so we re-FitTo only on a real change (not every
	// frame). Focus fit keeps the camera on moving targets smoothly without
	// reshooting the zoom level each tick.
	lastSystemIdx int
	lastFocus     sim.Focus
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

	// Refit only on system switch or focus-kind/idx change. When focused on
	// a moving target (body or craft), we still re-center every frame
	// below — this path only fires when the camera "target" changes, not
	// when the target moves.
	if v.lastSystemIdx != w.SystemIdx || v.lastFocus != w.Focus || !v.fitted {
		v.lastSystemIdx = w.SystemIdx
		v.lastFocus = w.Focus
		v.fitted = true
		v.canvas.FitTo(w.FocusZoomRadius())
	}

	v.canvas.Clear()
	v.canvas.Center(w.FocusPosition())

	// Dotted orbit ellipses for each body with a nonzero semimajor axis.
	for i := range sys.Bodies {
		b := sys.Bodies[i]
		if b.SemimajorAxis == 0 {
			continue
		}
		el := orbital.ElementsFromBody(b)
		v.canvas.DrawEllipseDotted(el, 360, 6)
	}

	// Plot each body at its perceived-size disk. System primary (index 0)
	// gets a hollow ring + filled center to distinguish it from planets.
	// Each body's cells are tagged with its palette color (v0.5.3) so
	// the canvas emits per-cell ANSI foreground at String() time.
	// See BodyPixelRadius for the size-tier logic.
	scale := v.canvas.Scale()
	for i := range sys.Bodies {
		b := sys.Bodies[i]
		pos := w.BodyPosition(b)
		r := BodyPixelRadius(b, i == 0, scale)
		color := render.ColorFor(b)
		if i == 0 {
			v.canvas.RingOutline(pos, r)
			v.canvas.FillDisk(pos, 1)
		} else {
			v.canvas.FillDisk(pos, r)
		}
		v.canvas.AddColoredDisk(pos, r, color)
		if i == selectedIdx {
			v.plotCluster(pos, r+4)
		}
	}

	// Vessel position trail (v0.5.2): a fading dotted history of
	// where the craft has actually been, distinct from the current
	// Keplerian orbit ellipse. Stride increases (sparser dots) for
	// older samples to give a visual gradient — newest = densest.
	if w.CraftVisibleHere() {
		trail := w.CraftTrail()
		// Draw oldest first (sparse stride 4) → newest (every point),
		// so newer samples overdraw older ones at any cell collision.
		for i, p := range trail {
			// stride: 4 at oldest end, 1 at newest end. Linear ramp.
			progress := float64(i) / float64(len(trail))
			stride := 4 - int(3*progress)
			if stride < 1 {
				stride = 1
			}
			if i%stride != 0 {
				continue
			}
			v.canvas.Plot(p)
		}
	}

	// Spacecraft current-orbit ellipse + glyph. Orbit is the craft's
	// live Keplerian ellipse in its home primary's frame, translated
	// into the system frame so it renders alongside planet orbits.
	// Only bound orbits (a > 0) render; hyperbolic escape trajectories
	// are already shown by the maneuver-preview SOI-segmented trace.
	if w.CraftVisibleHere() {
		c := w.Craft
		muCraft := c.Primary.GravitationalParameter()
		el := orbital.ElementsFromState(c.State.R, c.State.V, muCraft)
		primaryPos := w.BodyPosition(c.Primary)
		if el.A > 0 && !math.IsNaN(el.A) && !math.IsInf(el.A, 0) {
			v.canvas.DrawEllipseOffsetDotted(el, primaryPos, 360, 3)
			// Apoapsis / periapsis markers — render even for low-e
			// orbits so the player sees WHERE the two extremes are
			// when the ellipse shape alone is near-circular. Apoapsis
			// gets a larger disk, periapsis smaller; distinct sizes
			// read at a glance.
			peri := primaryPos.Add(orbital.PositionAtTrueAnomaly(el, 0))
			apo := primaryPos.Add(orbital.PositionAtTrueAnomaly(el, math.Pi))
			v.canvas.FillDisk(peri, 2)
			v.canvas.FillDisk(apo, 3)
		}
		// Directional vessel glyph — chevron rotated into the craft's
		// velocity frame so the player reads "which way am I going"
		// without staring at raw r, v numbers.
		v.canvas.PlotArrow(w.CraftInertial(), c.State.V, 5)
	}

	// Planned maneuver nodes — cluster glyph at each node's inertial
	// position, plus a dashed predicted trajectory from the first node's
	// post-burn state. Only meaningful when the craft is visible here.
	if w.CraftVisibleHere() {
		v.drawNodes(w)
	}

	canvasStr := v.canvas.String()
	canvasPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.theme.Primary.GetForeground()).
		Render(canvasStr)

	hud := v.renderHUD(w, selectedIdx, totalCols-v.canvas.Cols()-4)

	title := v.theme.Title.Render(fmt.Sprintf("terminal-space-program — %s", sys.Name))
	footer := v.theme.Footer.Render(
		"[q]quit [s]system [←/→]body [+/-]zoom [f/F]focus [g]sys [n]node [N]clr [H]hohmann [P]porkchop [R]refine [m]burn [i]info [?]help [.,]warp [0]pause",
	)

	body := lipgloss.JoinHorizontal(lipgloss.Top, canvasPanel, hud)
	return title + "\n" + body + "\n" + footer
}

// BodyPixelRadius returns the body's render radius in pixels. When the
// canvas is zoomed in enough that the body's true radius projects to
// at least trueSizeThreshold pixels, render at true size — so the
// rendered disk represents real surface, and a periapsis marker
// inside it visually reads as a collision. When zoomed out, fall back
// to a tier bucket so the body stays visible (true would be sub-pixel
// at system-wide zoom).
//
// Pass scale=0 to force the tier-bucket path (used when projection
// metadata isn't available).
//
// The `isPrimary` flag promotes a small body to star tier in the
// fallback path so the system primary always renders bigger than its
// planets even when its physical radius wouldn't otherwise put it
// there.
func BodyPixelRadius(b bodies.CelestialBody, isPrimary bool, scale float64) int {
	const trueSizeThreshold = 4
	const trueSizeCap = 64 // keep the Sun from filling the canvas at extreme zoom-in
	r := b.RadiusMeters()
	if scale > 0 && r > 0 {
		truePx := int(math.Round(r * scale))
		if truePx >= trueSizeThreshold {
			if truePx > trueSizeCap {
				truePx = trueSizeCap
			}
			return truePx
		}
	}
	switch {
	case isPrimary, r >= 1e8: // star-class
		return 6
	case r >= 2e7: // gas giant
		return 4
	case r >= 3e6: // terrestrial
		return 2
	case r > 0: // small body / moon / dwarf
		return 1
	default:
		return 1
	}
}

// plotCluster dots a cross of size n around a world point — useful for
// highlighting a body or spacecraft on the sparse braille grid.
func (v *OrbitView) plotCluster(center orbital.Vec3, n int) {
	step := 1.0 / v.canvas.Scale()
	for i := -n / 2; i <= n/2; i++ {
		v.canvas.Plot(center.Add(orbital.Vec3{X: float64(i) * step}))
		v.canvas.Plot(center.Add(orbital.Vec3{Y: float64(i) * step}))
	}
}

// drawNodes plots every planned maneuver node at its projected inertial
// position and draws the post-burn predicted trajectory starting from
// the first node. The trajectory is segmented by SOI: samples inside
// the craft's home SOI use stride-2 (dashed); samples that cross into
// another body's SOI use stride-1 (solid) so the crossing is visually
// distinct at a glance.
func (v *OrbitView) drawNodes(w *sim.World) {
	if len(w.Nodes) == 0 || w.Craft == nil {
		return
	}
	homeID := w.Craft.Primary.ID
	for _, n := range w.Nodes {
		// Frame-distinct cluster size: home-frame nodes get a tight cross,
		// foreign-frame (heliocentric or destination-SOI) get a larger
		// one so the player can see at a glance which leg is which on
		// auto-planted transfers.
		size := 6
		if n.PrimaryID != "" && n.PrimaryID != homeID {
			size = 10
		}
		v.plotCluster(w.NodeInertialPosition(n), size)
	}

	first := w.Nodes[0]
	post, postPrimaryID := w.PostBurnState(first)
	// Use the mu of whichever primary the post-burn state is expressed in
	// — usually craft's current primary (departure node), but may differ
	// for nodes planted in a foreign frame (auto-plant arrival).
	mu := w.Craft.Primary.GravitationalParameter()
	if postPrimaryID != w.Craft.Primary.ID {
		// Post-burn frame differs from the craft's home; PredictedSegments
		// is not yet parameterised on start-frame, so skip the trajectory
		// preview rather than render a wrong one. Glyphs (drawn above)
		// still mark where the burn fires.
		return
	}
	horizon := postBurnHorizon(post, mu)
	if horizon <= 0 {
		return
	}

	segments := w.PredictedSegments(post, horizon, 96)
	for _, seg := range segments {
		stride := 2
		if seg.PrimaryID != homeID {
			stride = 1 // foreign SOI — solid, eye-catching
		}
		for i, p := range seg.Points {
			if stride > 1 && i%stride == 0 {
				continue
			}
			v.canvas.Plot(p)
		}
	}
}

// postBurnHorizon picks a sensible prediction window based on the
// orbit's semimajor axis. Returns the orbital period for bound orbits,
// or a time-of-flight covering ~10 primary-radii of travel for
// hyperbolic (a ≤ 0) orbits so the preview is visible but finite.
func postBurnHorizon(state physics.StateVector, mu float64) float64 {
	a := physics.SemimajorAxis(state, mu)
	if a > 0 && !math.IsNaN(a) {
		return 2 * math.Pi * math.Sqrt(a*a*a/mu)
	}
	v := state.V.Norm()
	if v <= 0 {
		return 0
	}
	return 10 * state.R.Norm() / v
}

func (v *OrbitView) renderHUD(w *sim.World, selectedIdx int, width int) string {
	if width < 20 {
		width = 20
	}
	sys := w.System()

	warpLine := fmt.Sprintf("  warp: %.0fx", w.Clock.Warp())
	if eff := w.EffectiveWarp(); eff < w.Clock.Warp() {
		warpLine += v.theme.Warning.Render(fmt.Sprintf(" (clamped to %.0fx)", eff))
	}
	lines := []string{
		v.theme.Primary.Render("CLOCK"),
		"  T+" + w.Clock.SimTime.Format("2006-01-02"),
		warpLine,
	}
	if w.Clock.Paused {
		lines = append(lines, "  "+v.theme.Warning.Render("[PAUSED]"))
	}
	lines = append(lines,
		"",
		v.theme.Primary.Render("FOCUS"),
		"  "+w.FocusName(),
	)

	// Spacecraft block — only in Sol per plan §MVP.
	if w.CraftVisibleHere() {
		c := w.Craft
		mu := c.Primary.GravitationalParameter()
		el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
		primaryR := c.Primary.RadiusMeters()
		apoAlt := el.Apoapsis() - primaryR
		periAlt := el.Periapsis() - primaryR
		incDeg := el.I * 180.0 / 3.141592653589793

		lines = append(lines,
			"",
			v.theme.Primary.Render("VESSEL"),
			"  "+c.Name,
			"  primary:   "+c.Primary.EnglishName,
			fmt.Sprintf("  altitude:  %.1f km", c.Altitude()/1000),
			fmt.Sprintf("  velocity:  %.2f km/s", c.OrbitalSpeed()/1000),
			fmt.Sprintf("  apoapsis:  %.1f km", apoAlt/1000),
			fmt.Sprintf("  periapsis: %.1f km", periAlt/1000),
			fmt.Sprintf("  inclin.:   %.2f°", incDeg),
		)
		if periAlt < 0 && el.A > 0 && !math.IsNaN(el.A) && !math.IsInf(el.A, 0) {
			lines = append(lines, "  "+v.theme.Alert.Render("PERIAPSIS BELOW SURFACE"))
		}
		lines = append(lines,
			"",
			v.theme.Primary.Render("PROPELLANT"),
			fmt.Sprintf("  fuel:      %.0f kg", c.Fuel),
			fmt.Sprintf("  mass:      %.0f kg", c.TotalMass()),
			fmt.Sprintf("  Δv budget: %.0f m/s", c.RemainingDeltaV()),
		)
	} else if w.Craft != nil {
		lines = append(lines, "",
			v.theme.Dim.Render("VESSEL (in Sol — [s] to switch)"),
		)
	}

	if w.ActiveBurn != nil {
		remaining := w.ActiveBurn.EndTime.Sub(w.Clock.SimTime).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		lines = append(lines, "",
			v.theme.Warning.Render("BURN ACTIVE"),
			fmt.Sprintf("  mode:    %s", w.ActiveBurn.Mode.String()),
			fmt.Sprintf("  Δv-to-go: %.1f m/s", w.ActiveBurn.DVRemaining),
			fmt.Sprintf("  T-%.1fs remaining", remaining),
		)
	}

	if len(w.Nodes) > 0 {
		lines = append(lines, "", v.theme.Primary.Render("NODES"))
		for i, n := range w.Nodes {
			dt := n.TriggerTime.Sub(w.Clock.SimTime).Seconds()
			kind := "imp"
			if n.Duration > 0 {
				kind = fmt.Sprintf("fin %.0fs", n.Duration.Seconds())
			}
			lines = append(lines, fmt.Sprintf(
				"  #%d T%+.0fs  %s  %.0f m/s  %s",
				i+1, dt, n.Mode.String(), n.DV, kind,
			))
		}
	}

	lines = append(lines, "", v.theme.Primary.Render("SYSTEM"),
		"  "+sys.Name,
		fmt.Sprintf("  %d bodies", len(sys.Bodies)),
		"",
		v.theme.Primary.Render("SELECTED"),
	)

	if selectedIdx >= 0 && selectedIdx < len(sys.Bodies) {
		b := sys.Bodies[selectedIdx]
		nameStyle := lipgloss.NewStyle().Foreground(render.ColorFor(b)).Bold(true)
		lines = append(lines,
			"  "+nameStyle.Render(b.EnglishName),
			"  "+b.BodyType,
		)
		if b.SemimajorAxis > 0 {
			auVal := b.SemimajorAxisMeters() / bodies.AU
			lines = append(lines, fmt.Sprintf("  a: %.3f AU", auVal))
			lines = append(lines, fmt.Sprintf("  e: %.4f", b.Eccentricity))
			lines = append(lines, fmt.Sprintf("  T: %.1f d", b.SideralOrbit))

			if preview := w.HohmannPreviewFor(selectedIdx); preview.Valid || preview.Note != "" {
				lines = append(lines, "", v.theme.Primary.Render("HOHMANN PREVIEW"))
				lines = append(lines, preview.Format()...)
			}
		} else {
			lines = append(lines, v.theme.Dim.Render("  (primary)"))
		}
	}

	content := strings.Join(lines, "\n")
	return v.theme.HUDBox.Width(width).Render(content)
}
