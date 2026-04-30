// Package screens implements the individual tea.Model screens composed by
// tui.App: OrbitView (C8), BodyInfo (C9), Maneuver (C20), Help (C9).
package screens

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
	"github.com/jasonfen/terminal-space-program/internal/version"
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

	// burnFrozenCenter pins the canvas center for the duration of an
	// active burn. Captured when ActiveBurn becomes non-nil, cleared
	// when it returns to nil. v0.6.3 fix: focus-on-craft tracks the
	// craft's live position every tick, and during a burn the craft
	// is sweeping through periapsis at km/s — every other element
	// (the selected-body cross, body disks, the live orbit ellipse,
	// apsidal markers) sweeps the opposite direction in screen space,
	// reading as the world rotating around the player. Holding the
	// camera lets the player watch the burn modify the orbit instead.
	burnFrozenCenter *orbital.Vec3

	// titleBar tracks the column ranges of the right-aligned [Menu]
	// and [Missions] click targets in the title bar (row 0). Set on
	// each Render so HitAt-style hit-tests stay accurate after
	// terminal resizes. v0.7.4+.
	menuColStart, menuColEnd         int
	missionsColStart, missionsColEnd int

	// hudNodeHits tracks NODES-block entries in render order. After
	// each HUD render, hudNodeRows is filled in by scanning the
	// rendered string for ▸ markers — that's robust to lipgloss
	// column-padding / wrapping quirks that would throw off a
	// "predicted from len(lines)" calculation. v0.8.2.x.
	//
	// hudScrollOffset is the number of rows the rendered output
	// extends past the terminal's visible area. Bubbletea reports
	// mouse coordinates relative to what's visible, so when the
	// HUD overflows, we subtract this offset before matching.
	hudNodeHits     []hudNodeHit
	hudNodeRows     []int // HUD-relative row of each hudNodeHits entry, in the rendered string
	hudScrollOffset int   // rows of overflow off the top when the rendered view exceeds terminal height
	hudColStart     int   // first screen-col of the HUD region
	totalRows       int   // last-known terminal height; updated by Render
}

// hudNodeHit identifies a clickable NODES-block entry. CraftIdx is
// the craft slot that owns the node; NodeIdx is the 0-based index
// into that craft's Nodes slice. Recorded in render order; the
// matching screen row is in hudNodeRows[i].
type hudNodeHit struct {
	craftIdx int
	nodeIdx  int
}

// hudNodeMarker is the visible click-affordance prefix inserted on
// each NODES row. Re-used by post-render row scanning to locate
// each entry's actual screen position.
const hudNodeMarker = "▸"

// minOrbitPixels is the projected apoapsis size below which an orbit
// (live or planted-leg) is suppressed at render time. v0.6.1: at
// heliocentric zoom a 200-km LEO ellipse projects to a sub-cell
// extent and every dotted sample piles onto a single cell, painting
// a misleading blob on top of the parent body. Six pixels is just
// large enough that ~6 evenly-spaced ellipse dots can resolve into
// a recognisable shape; below that, hide the orbit and let the
// vessel chevron / node markers carry the visual.
const minOrbitPixels = 6.0

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

// HitAt translates a screen-space mouse coordinate (col, row) into a
// CellTag from the orbit canvas. The orbit screen renders title (1
// row) + canvasPanel (rounded border = 1 row top, 1 col left)
// before the canvas content, so we offset (col-1, row-2). Returns a
// zero-value CellTag when the click lands outside the canvas
// content area, which the caller treats as "no hit." v0.6.4+.
func (v *OrbitView) HitAt(screenCol, screenRow int) widgets.CellTag {
	return v.canvas.HitAt(screenCol-1, screenRow-2)
}

// IsCanvasClick reports whether a screen-space (col, row) lands
// inside the canvas content area (i.e. between the rounded-border
// edges). v0.6.4 mouse dispatch uses this to distinguish "click on
// orbit canvas" from "click on HUD" when no body / vessel / node
// hit lands.
func (v *OrbitView) IsCanvasClick(col, row int) bool {
	return col >= 1 && col <= v.canvas.Cols() && row >= 2 && row <= v.canvas.Rows()+1
}

// IsHudClick reports whether a screen-space col lands on the HUD
// panel (right of the canvas + its border).
func (v *OrbitView) IsHudClick(col int) bool {
	return col > v.canvas.Cols()+1
}

// HitHudNode resolves a screen-space click against the HUD's
// NODES block. Returns (craftIdx, nodeIdx, true) when the row
// matches a recorded NODES entry and the column lands in the HUD
// region; (-1, -1, false) otherwise. v0.8.2.x:
//   - row matching uses the post-render scan in scanHudNodeRows.
//   - hudScrollOffset compensates for terminal scrolling when the
//     rendered HUD overflows the visible area.
func (v *OrbitView) HitHudNode(col, row int) (int, int, bool) {
	if !v.IsHudClick(col) {
		return -1, -1, false
	}
	// hudNodeRows stores absolute rows in the rendered output;
	// terminal-visible rows = rendered_row - hudScrollOffset.
	for i, renderedRow := range v.hudNodeRows {
		if i >= len(v.hudNodeHits) {
			break
		}
		visibleRow := renderedRow - v.hudScrollOffset
		if row == visibleRow {
			h := v.hudNodeHits[i]
			return h.craftIdx, h.nodeIdx, true
		}
	}
	return -1, -1, false
}

// ProjectToOrbit maps a screen-space click to the time-of-flight at
// which the active craft reaches the orbit point nearest the click.
// Used by the v0.6.4 empty-canvas mouse path to stage a new burn at
// "this point along my orbit."
//
// Algorithm: sample 360 true-anomalies on the live craft orbit,
// project each to canvas pixels, take the smallest screen distance
// to the click pixel; convert that ν to a time-of-flight via
// orbital.TimeToTrueAnomaly. ok=false for hyperbolic / no-craft
// states or when no sample lands on-canvas.
func (v *OrbitView) ProjectToOrbit(w *sim.World, screenCol, screenRow int) (time.Duration, bool) {
	if w.ActiveCraft() == nil {
		return 0, false
	}
	mu := w.ActiveCraft().Primary.GravitationalParameter()
	el := orbital.ElementsFromState(w.ActiveCraft().State.R, w.ActiveCraft().State.V, mu)
	if el.A <= 0 || el.E >= 1 || math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return 0, false
	}
	currentNu := orbital.TrueAnomalyFromState(w.ActiveCraft().State.R, w.ActiveCraft().State.V, mu, el)
	primaryPos := w.BodyPosition(w.ActiveCraft().Primary)

	clickCanvasCol := screenCol - 1 // strip border / title offsets
	clickCanvasRow := screenRow - 2
	bestNu := 0.0
	bestDist := math.MaxFloat64
	const samples = 360
	for i := 0; i < samples; i++ {
		nu := 2 * math.Pi * float64(i) / float64(samples)
		p := primaryPos.Add(orbital.PositionAtTrueAnomaly(el, nu))
		px, py, ok := v.canvas.Project(p)
		if !ok {
			continue
		}
		col, row := px/2, py/4
		dx := float64(col - clickCanvasCol)
		dy := float64(row - clickCanvasRow)
		d := dx*dx + dy*dy
		if d < bestDist {
			bestDist = d
			bestNu = nu
		}
	}
	if bestDist == math.MaxFloat64 {
		return 0, false
	}
	dtSecs := orbital.TimeToTrueAnomaly(currentNu, bestNu, el.A, el.E, mu)
	if dtSecs < 0 {
		return 0, false
	}
	return time.Duration(dtSecs * float64(time.Second)), true
}

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
	v.canvas.SetBasis(viewBasis(w))
	center := w.FocusPosition()
	if w.ActiveCraft().ActiveBurn != nil {
		if v.burnFrozenCenter == nil {
			snapshot := center
			v.burnFrozenCenter = &snapshot
		}
		center = *v.burnFrozenCenter
	} else if v.burnFrozenCenter != nil {
		v.burnFrozenCenter = nil
	}
	v.canvas.Center(center)

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
	// Body pixels are tagged with the body's palette color (v0.5.10) —
	// per-pixel tagging keeps the color confined to the body's actual
	// disk, so orbit lines and craft glyphs sharing nearby cells stay
	// default-colored.
	// See BodyPixelRadius for the size-tier logic.
	scale := v.canvas.Scale()
	for i := range sys.Bodies {
		b := sys.Bodies[i]
		pos := w.BodyPosition(b)
		r := BodyPixelRadius(b, i == 0, scale)
		color := render.ColorFor(b)
		// v0.6.4: tag body pixels with BodyID so HitAt resolves
		// mouse clicks back to the body for click-to-focus.
		bodyTag := widgets.CellTag{Color: color, BodyID: b.ID}
		if i == 0 {
			v.canvas.RingColoredOutlineTagged(pos, r, bodyTag)
			v.canvas.FillColoredDiskTagged(pos, 1, bodyTag)
		} else if tex := render.TextureFor(b, r); tex != nil {
			// Per-pixel textured fill (Earth continents + clouds in
			// v0.7.2.1; Moon maria + craters in v0.7.2.2). The tag's
			// BodyID / hit fields still propagate; only the per-pixel
			// color comes from the texture function.
			pxR := r
			v.canvas.FillTexturedDiskTagged(pos, r, func(dx, dy int) lipgloss.Color {
				return tex(dx, dy, pxR)
			}, bodyTag)
		} else {
			v.canvas.FillColoredDiskTagged(pos, r, bodyTag)
		}
		// Draw rings for ringed bodies (v0.5.11). World-scale ring
		// radii project to pixel radii via the canvas scale; only
		// draw when the outer ring would visibly clear the body's
		// rendered disk. v0.5.15: skip if outerPx is beyond a sane
		// canvas multiple — at extreme zoom the ring projects to
		// millions of pixels and is entirely off-canvas anyway. The
		// canvas has a samples cap as defense in depth.
		if _, outerR, ok := render.BodyRings(b.ID); ok {
			outerPx := int(outerR * scale)
			canvasReach := v.canvas.Cols()*2 + v.canvas.Rows()*4
			if outerPx > r && outerPx < canvasReach {
				v.canvas.RingColoredOutline(pos, outerPx, color)
			}
		}
		// Body-identity glyph overlay (v0.5.12). Skip the system
		// primary — it already has the ring+dot draw style and a
		// glyph would clash. Other bodies get ☉ / ◉ / ● / ○ based on
		// type so they read distinctly even at small pixel radius.
		// Also skip when the body has a per-pixel texture rendering
		// (v0.7.2.1+) — the glyph would blot the centre of the
		// continent/cloud detail it's meant to highlight.
		if i != 0 && !render.BodyHasTexture(b, r) {
			if g := render.GlyphFor(b); g != 0 {
				v.canvas.SetCellOverlay(pos, g)
			}
		}
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
	//
	// v0.6.1: orbits whose apoapsis projects to fewer than
	// minOrbitPixels (≈ a body's pixel-tier diameter) are skipped at
	// render time — at heliocentric zoom a 200-km LEO would otherwise
	// pile every dotted sample onto a single cell, painting a bright
	// blob over the parent body that doesn't read as an orbit. The
	// vessel chevron stays drawn so the craft's presence is still
	// communicated.
	if w.CraftVisibleHere() {
		c := w.ActiveCraft()
		muCraft := c.Primary.GravitationalParameter()
		el := orbital.ElementsFromState(c.State.R, c.State.V, muCraft)
		primaryPos := w.BodyPosition(c.Primary)
		scale := v.canvas.Scale()
		orbitVisible := el.A > 0 && !math.IsNaN(el.A) && !math.IsInf(el.A, 0) && el.Apoapsis()*scale >= minOrbitPixels
		// v0.6.4: in side views, the spacecraft orbit can pass behind
		// the primary body. The canvas's IsBehindBody / occluded-
		// ellipse helpers skip back-half samples that fall inside
		// the primary's projected disk — since the body disk has
		// already been drawn (line ~115), the gap reads as natural
		// occlusion. Apo / peri markers + the craft chevron use the
		// same check.
		primaryPxR := BodyPixelRadius(c.Primary, false, scale)
		if orbitVisible {
			v.canvas.DrawEllipseOffsetOccluded(el, primaryPos, 360, 3, primaryPos, primaryPxR, render.ColorCurrentOrbit)
			peri := primaryPos.Add(orbital.PositionAtTrueAnomaly(el, 0))
			apo := primaryPos.Add(orbital.PositionAtTrueAnomaly(el, math.Pi))
			if !v.canvas.IsBehindBody(peri, primaryPos, primaryPxR) {
				v.canvas.FillDisk(peri, 2)
			}
			if !v.canvas.IsBehindBody(apo, primaryPos, primaryPxR) {
				v.canvas.FillDisk(apo, 3)
			}
		}
		craftInertial := w.CraftInertial()
		// v0.8.3+: engine-firing flame trail behind the active craft
		// when an ActiveBurn or main-engine ManualBurn is firing.
		// flameStep = 5 / scale puts each step in a cell adjacent
		// to (or beyond) the craft glyph cell — the glyph's
		// SetCellOverlay covers a 2x4 sub-pixel cell, so a 5-px
		// offset crosses the boundary regardless of velocity
		// direction.
		if c.ActiveBurn != nil || (c.ManualBurn != nil && c.EngineMode == spacecraft.EngineMain && c.EffectiveThrottle() > 0) {
			vMag := c.State.V.Norm()
			if vMag > 0 && scale > 0 {
				vHat := c.State.V.Scale(1 / vMag)
				flameStep := 5.0 / scale
				for i := 1; i <= 3; i++ {
					p := craftInertial.Sub(vHat.Scale(float64(i) * flameStep))
					if v.canvas.IsBehindBody(p, primaryPos, primaryPxR) {
						continue
					}
					v.canvas.PlotColored(p, render.ColorFlame)
				}
			}
		}
		if !v.canvas.IsBehindBody(craftInertial, primaryPos, primaryPxR) {
			activeColor := render.ColorCraftMarker
			if c.Color != "" {
				activeColor = lipgloss.Color(c.Color)
			}
			vesselTag := widgets.CellTag{Color: activeColor, IsVessel: true}
			// v0.8.2+: active craft uses its loadout glyph just like
			// non-active ones — the v0.7.x chevron-arrow rendering
			// read as crusty next to the new ▲/◆/●/▼ markers, so the
			// glyph wins for visual consistency. The colored dot
			// underneath gives the cell a stable hit-test tag for
			// click-on-vessel.
			v.canvas.FillColoredDiskTagged(craftInertial, 1, vesselTag)
			if c.Glyph != "" {
				if g := []rune(c.Glyph); len(g) > 0 {
					v.canvas.SetCellOverlay(craftInertial, g[0])
				}
			}
		}

		// v0.8.3+: per-thruster RCS puff visual. Each pulse stamps a
		// small fading marker offset along the exhaust direction
		// (anti-thrust), with a short trail stretching that way to
		// signal "puff in this direction." Replaces the v0.8.0
		// placeholder centered dot.
		for _, p := range w.RCSPuffs() {
			if p.AgeFrac >= 0.75 {
				continue // late-stage puffs invisible — keep canvas tidy
			}
			if scale <= 0 {
				continue
			}
			// Canvas cells are 2 sub-pixels wide × 4 tall (braille
			// rendering). The craft glyph's SetCellOverlay covers
			// the whole cell, so puff sub-pixels in the same cell
			// as the glyph are invisible. Step needs to be ≥4 px
			// for vertical exhaust to land in an adjacent cell;
			// 5 px works for any direction (5·sin(45°) ≈ 3.5 < 4
			// for pure 45° but still visible because the
			// horizontal component of 5·cos(45°) ≈ 3.5 ≥ 2 px =
			// crosses horizontal cell boundary). v0.8.3+.
			puffStep := 5.0 / scale
			origin := p.Inertial.Add(p.Exhaust.Scale(puffStep))
			tip := p.Inertial.Add(p.Exhaust.Scale(2 * puffStep))
			if !v.canvas.IsBehindBody(origin, primaryPos, primaryPxR) {
				v.canvas.PlotColored(origin, render.ColorWarning)
			}
			if !v.canvas.IsBehindBody(tip, primaryPos, primaryPxR) {
				v.canvas.PlotColored(tip, render.ColorFlame)
			}
		}

		// v0.8.2+: render non-active craft with their per-loadout
		// glyph + color so each vessel reads distinctly even at
		// small pixel sizes. The current-orbit ellipse renders in
		// the craft's own color (dim when no Color is set, falling
		// back to ColorDim — preserves pre-v0.8.2 behaviour).
		for i, other := range w.Crafts {
			if i == w.ActiveCraftIdx || other == nil {
				continue
			}
			otherPrimaryPos := w.BodyPosition(other.Primary)
			otherPxR := BodyPixelRadius(other.Primary, false, scale)
			otherInertial := otherPrimaryPos.Add(other.State.R)
			otherEl := orbital.ElementsFromState(other.State.R, other.State.V, other.Primary.GravitationalParameter())
			otherOrbitVisible := otherEl.A > 0 && !math.IsNaN(otherEl.A) && !math.IsInf(otherEl.A, 0) && otherEl.Apoapsis()*scale >= minOrbitPixels
			otherColor := lipgloss.Color(render.ColorDim)
			if other.Color != "" {
				otherColor = lipgloss.Color(other.Color)
			}
			if otherOrbitVisible {
				v.canvas.DrawEllipseOffsetOccluded(otherEl, otherPrimaryPos, 180, 5, otherPrimaryPos, otherPxR, otherColor)
			}
			if !v.canvas.IsBehindBody(otherInertial, otherPrimaryPos, otherPxR) {
				v.canvas.PlotColored(otherInertial, otherColor)
				if other.Glyph != "" {
					if g := []rune(other.Glyph); len(g) > 0 {
						v.canvas.SetCellOverlay(otherInertial, g[0])
					}
				}
			}
		}
	}

	// Planned maneuver nodes — cluster glyph at each node's inertial
	// position, plus a dashed predicted trajectory from the first node's
	// post-burn state. Only meaningful when the craft is visible here.
	if w.CraftVisibleHere() {
		v.drawNodes(w)
	}

	// Stamp the active projection in the canvas's bottom-right corner
	// so the indicator stays attached to the view it describes (was a
	// HUD line under FOCUS until v0.7.4 — see orbit.go's renderHUD).
	viewLabel := "view: " + w.ViewMode.String()
	labelCol := v.canvas.Cols() - len([]rune(viewLabel)) - 1
	if labelCol < 0 {
		labelCol = 0
	}
	v.canvas.SetCellLabel(labelCol, v.canvas.Rows()-1, viewLabel)

	canvasStr := v.canvas.String()
	canvasPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.theme.Primary.GetForeground()).
		Render(canvasStr)

	v.totalRows = totalRows
	hud := v.renderHUD(w, selectedIdx, totalCols-v.canvas.Cols()-4)
	// HUD starts at the column right after canvas + its rounded
	// border (1 col left, 1 col right). v0.8.2.x: needed by the
	// HUD-row hit-test below (HitHudNode) so a click in the HUD
	// region routes to the maneuver editor, not body info.
	v.hudColStart = v.canvas.Cols() + 2

	craftChip := ""
	if n := len(w.Crafts); n > 1 {
		craftChip = fmt.Sprintf(" — CRAFT %d/%d", w.ActiveCraftIdx+1, n)
	}
	title := v.renderTitleBar(sys.Name+craftChip, totalCols)
	footer := v.theme.Footer.Render(
		"[q]quit [s]system [←/→]body [+/-]zoom [f/F]focus [g]sys [n]spawn [N]clr [[/]]craft [H]hohmann [P]porkchop [R]refine [m]burn [i]info [?]help [.,]warp [0]pause",
	)

	body := lipgloss.JoinHorizontal(lipgloss.Top, canvasPanel, hud)
	out := title + "\n" + body + "\n" + footer
	// v0.8.2.x: when the rendered output exceeds terminal height
	// the terminal scrolls — top rows fall off-screen and bubbletea
	// reports mouse coords relative to what's visible. Compute the
	// scroll offset so HitHudNode can subtract it. The +1 status-
	// bar overlay is replaced inline so it doesn't add a row.
	totalRendered := strings.Count(out, "\n") + 1
	if v.totalRows > 0 && totalRendered > v.totalRows {
		v.hudScrollOffset = totalRendered - v.totalRows
	} else {
		v.hudScrollOffset = 0
	}
	return out
}

// renderTitleBar composes the orbit-screen title row: the existing
// "terminal-space-program — vX — System" left-aligned, plus
// right-aligned `[Menu]` and `[Missions]` clickable buttons. Stores
// the cell ranges of each button so HitMenuButton / HitMissionsButton
// can map subsequent clicks back to a screen action. v0.7.4+.
func (v *OrbitView) renderTitleBar(systemName string, totalCols int) string {
	left := fmt.Sprintf("terminal-space-program — %s — %s", version.Version, systemName)
	const menuLabel = "[Menu]"
	const missionsLabel = "[Missions]"
	const gap = "  "
	rightPlain := menuLabel + gap + missionsLabel

	// Compute the absolute column where the right group starts so the
	// hit-test ranges match what the player sees on screen.
	leftRunes := len([]rune(left))
	rightRunes := len([]rune(rightPlain))
	pad := totalCols - leftRunes - rightRunes
	if pad < 1 {
		pad = 1
	}
	rightStart := leftRunes + pad

	v.menuColStart = rightStart
	v.menuColEnd = rightStart + len([]rune(menuLabel))
	v.missionsColStart = v.menuColEnd + len([]rune(gap))
	v.missionsColEnd = v.missionsColStart + len([]rune(missionsLabel))

	rendered := v.theme.Title.Render(left) +
		strings.Repeat(" ", pad) +
		v.theme.Primary.Render(menuLabel) +
		gap +
		v.theme.Primary.Render(missionsLabel)
	return rendered
}

// HitMenuButton reports whether a click at (col, row) lands on the
// title bar's `[Menu]` button. Title bar lives on row 0 of the
// rendered orbit view. v0.7.4+.
func (v *OrbitView) HitMenuButton(col, row int) bool {
	return row == 0 && col >= v.menuColStart && col < v.menuColEnd
}

// HitMissionsButton reports whether a click at (col, row) lands on
// the title bar's `[Missions]` button. v0.7.4+.
func (v *OrbitView) HitMissionsButton(col, row int) bool {
	return row == 0 && col >= v.missionsColStart && col < v.missionsColEnd
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

// plotClusterColored is plotCluster with each cell tagged with the
// given color. v0.6.1: maneuver-node markers share the color of
// their resulting orbit leg, so the player sees node N at the
// position where the post-burn orbit (also color N) begins.
func (v *OrbitView) plotClusterColored(center orbital.Vec3, n int, color lipgloss.Color) {
	v.plotClusterTagged(center, n, widgets.CellTag{Color: color})
}

// plotClusterTagged is plotClusterColored that records the supplied
// CellTag (color + hit-test metadata) on every pixel it sets. v0.6.4+
// — node draws use this with NodeIdx so HitAt can resolve a click
// back to the planted node it lands on.
func (v *OrbitView) plotClusterTagged(center orbital.Vec3, n int, tag widgets.CellTag) {
	step := 1.0 / v.canvas.Scale()
	for i := -n / 2; i <= n/2; i++ {
		v.canvas.PlotColoredTagged(center.Add(orbital.Vec3{X: float64(i) * step}), tag)
		v.canvas.PlotColoredTagged(center.Add(orbital.Vec3{Y: float64(i) * step}), tag)
	}
}

// drawNodes plots every planned maneuver node at its projected inertial
// position and draws the post-burn predicted trajectory starting from
// the first node. The trajectory is segmented by SOI: samples inside
// the craft's home SOI use stride-2 (dashed); samples that cross into
// another body's SOI use stride-1 (solid) so the crossing is visually
// distinct at a glance.
func (v *OrbitView) drawNodes(w *sim.World) {
	if len(w.ActiveCraft().Nodes) == 0 || w.ActiveCraft() == nil {
		return
	}
	homeID := w.ActiveCraft().Primary.ID
	for i, n := range w.ActiveCraft().Nodes {
		// Frame-distinct cluster size: home-frame nodes get a tight cross,
		// foreign-frame (heliocentric or destination-SOI) get a larger
		// one so the player can see at a glance which leg is which on
		// auto-planted transfers.
		size := 6
		if n.PrimaryID != "" && n.PrimaryID != homeID {
			size = 10
		}
		// v0.6.1: each node's marker matches the color of its
		// resulting orbit leg, so the cluster glyph and the
		// post-burn dashed orbit read as a matched pair.
		v.plotClusterTagged(w.NodeInertialPosition(n), size, widgets.CellTag{
			Color:   render.ManeuverSegmentColor(i),
			NodeIdx: i + 1, // 0 = none; planted node i is 1+i in the tag.
		})
	}

	// v0.6.1: while a finite burn is firing the live craft state is
	// mutated every integrator step; the dashed trajectory preview
	// would otherwise rotate wildly each frame. Skip the preview and
	// let the live ellipse + active-burn HUD block carry the visual
	// load until the burn completes.
	if w.ActiveCraft().ActiveBurn != nil {
		return
	}

	// v0.6.1: render each post-maneuver leg in its own color so the
	// player can read which orbit belongs to which planted burn.
	// PredictedLegs walks all resolved nodes, rebasing each into the
	// node's intended frame (e.g. Hohmann arrival in Mars frame).
	scale := v.canvas.Scale()
	legs := w.PredictedLegs()
	for _, leg := range legs {
		// Skip legs whose orbit projects too small to convey shape
		// (heliocentric view of a planet-frame leg). Same rule as
		// the live ellipse — keeps the canvas from painting a
		// blob on top of the parent body. Hyperbolic / a≤0 legs
		// always render: their trajectories cover meaningful
		// distance regardless of orbit size.
		legMu := leg.Primary.GravitationalParameter()
		legEl := orbital.ElementsFromState(leg.State.R, leg.State.V, legMu)
		if legEl.A > 0 && !math.IsNaN(legEl.A) && !math.IsInf(legEl.A, 0) &&
			legEl.Apoapsis()*scale < minOrbitPixels {
			continue
		}

		samples := 96
		segs := w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.HorizonSecs, samples)
		legColor := render.ManeuverSegmentColor(leg.NodeIndex)
		for _, seg := range segs {
			stride := 2
			if seg.PrimaryID != homeID {
				stride = 1 // foreign SOI — solid, eye-catching
			}
			for i, p := range seg.Points {
				if stride > 1 && i%stride == 0 {
					continue
				}
				v.canvas.PlotColored(p, legColor)
			}
		}
	}
}

func (v *OrbitView) renderHUD(w *sim.World, selectedIdx int, width int) string {
	if width < 20 {
		width = 20
	}
	// Reset the node-click hits before re-rendering — the NODES
	// block re-populates them as it walks each craft's plan.
	v.hudNodeHits = v.hudNodeHits[:0]
	sys := w.System()

	// section emits a divider + colored section header, used in place of
	// the v0.5.12 plain "" blank-line separators. Visually groups the
	// HUD into scannable blocks. v0.5.13.
	section := func(title string) []string {
		return []string{
			v.theme.Dim.Render(strings.Repeat("─", width-2)),
			v.theme.Primary.Render(title),
		}
	}

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
	lines = append(lines, section("FOCUS")...)
	lines = append(lines, "  "+w.FocusName())

	// Spacecraft block — only in Sol per plan §MVP.
	if w.CraftVisibleHere() {
		c := w.ActiveCraft()
		mu := c.Primary.GravitationalParameter()
		el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
		primaryR := c.Primary.RadiusMeters()
		apoAlt := el.Apoapsis() - primaryR
		periAlt := el.Periapsis() - primaryR
		incDeg := el.I * 180.0 / 3.141592653589793

		// VESSEL + PROPELLANT side-by-side to reduce the HUD's vertical
		// height. Each column gets roughly half the HUD width (with a
		// 2-char gutter between); the section divider + header is per
		// column so each block reads as its own pane. Falls back to
		// stacked rendering when the HUD is too narrow to split (< 36
		// cols of content) — labels would clip otherwise.
		const minSplitWidth = 36
		if width-2 >= minSplitWidth {
			half := (width - 4) / 2 // gutter of 2 chars between columns
			vesselLines := []string{
				v.theme.Dim.Render(strings.Repeat("─", half)),
				v.theme.Primary.Render("VESSEL"),
				"  " + c.Name,
				"  primary:   " + c.Primary.EnglishName,
				fmt.Sprintf("  altitude:  %.1f km", c.Altitude()/1000),
				fmt.Sprintf("  velocity:  %.2f km/s", c.OrbitalSpeed()/1000),
				fmt.Sprintf("  apoapsis:  %.1f km", apoAlt/1000),
				fmt.Sprintf("  periapsis: %.1f km", periAlt/1000),
				fmt.Sprintf("  inclin.:   %.2f°", incDeg),
			}
			if periAlt < 0 && el.A > 0 && !math.IsNaN(el.A) && !math.IsInf(el.A, 0) {
				vesselLines = append(vesselLines, "  "+v.theme.Alert.Render("⚠ PERIAPSIS BELOW SURFACE"))
			}
			propLines := []string{
				v.theme.Dim.Render(strings.Repeat("─", half)),
				v.theme.Primary.Render("PROPELLANT"),
				fmt.Sprintf("  fuel:      %.0f kg", c.Fuel),
				fmt.Sprintf("  mass:      %.0f kg", c.TotalMass()),
				fmt.Sprintf("  Δv budget: %.0f m/s", c.RemainingDeltaV()),
				fmt.Sprintf("  throttle:  %.0f%%", c.EffectiveThrottle()*100),
			}
			if c.MonopropCapacity > 0 {
				propLines = append(propLines,
					fmt.Sprintf("  monoprop:  %.2f kg (%.1f m/s)", c.Monoprop, c.RCSDeltaV()),
				)
			}
			colStyle := lipgloss.NewStyle().Width(half)
			vesselCol := colStyle.Render(strings.Join(vesselLines, "\n"))
			propCol := colStyle.Render(strings.Join(propLines, "\n"))
			combined := lipgloss.JoinHorizontal(lipgloss.Top, vesselCol, "  ", propCol)
			lines = append(lines, strings.Split(combined, "\n")...)
		} else {
			lines = append(lines, section("VESSEL")...)
			lines = append(lines,
				"  "+c.Name,
				"  primary:   "+c.Primary.EnglishName,
				fmt.Sprintf("  altitude:  %.1f km", c.Altitude()/1000),
				fmt.Sprintf("  velocity:  %.2f km/s", c.OrbitalSpeed()/1000),
				fmt.Sprintf("  apoapsis:  %.1f km", apoAlt/1000),
				fmt.Sprintf("  periapsis: %.1f km", periAlt/1000),
				fmt.Sprintf("  inclin.:   %.2f°", incDeg),
			)
			if periAlt < 0 && el.A > 0 && !math.IsNaN(el.A) && !math.IsInf(el.A, 0) {
				lines = append(lines, "  "+v.theme.Alert.Render("⚠ PERIAPSIS BELOW SURFACE"))
			}
			lines = append(lines, section("PROPELLANT")...)
			lines = append(lines,
				fmt.Sprintf("  fuel:      %.0f kg", c.Fuel),
				fmt.Sprintf("  mass:      %.0f kg", c.TotalMass()),
				fmt.Sprintf("  Δv budget: %.0f m/s", c.RemainingDeltaV()),
				fmt.Sprintf("  throttle:  %.0f%%", c.EffectiveThrottle()*100),
			)
			if c.MonopropCapacity > 0 {
				lines = append(lines,
					fmt.Sprintf("  monoprop:  %.2f kg (%.1f m/s)", c.Monoprop, c.RCSDeltaV()),
				)
			}
		}
		// v0.7.3+: held attitude block. Always shows so the player
		// knows what direction the next manual burn will fire in.
		lines = append(lines, section("ATTITUDE")...)
		manualState := "idle"
		if w.ActiveCraft().ManualBurn != nil {
			elapsed := w.Clock.SimTime.Sub(w.ActiveCraft().ManualBurn.StartTime).Seconds()
			manualState = fmt.Sprintf(v.theme.Warning.Render("● firing T+%.1fs"), elapsed)
		}
		lines = append(lines,
			fmt.Sprintf("  hold:      %s", w.ActiveCraft().AttitudeMode.String()),
			fmt.Sprintf("  engine:    %s", w.ActiveCraft().EngineMode.String()),
			fmt.Sprintf("  manual:    %s", manualState),
		)
	} else if w.ActiveCraft() != nil {
		lines = append(lines, "",
			v.theme.Dim.Render("VESSEL (in Sol — [tab] to switch)"),
		)
	}

	// v0.8.1+: walk every craft and surface any in-flight burn so a
	// burn on a non-active craft doesn't silently sneak by. The
	// active craft's burn keeps the prior multi-line treatment for
	// quick scan; non-active burns get one compact line each.
	burning := []int{}
	for i, c := range w.Crafts {
		if c != nil && c.ActiveBurn != nil {
			burning = append(burning, i)
		}
	}
	if len(burning) > 0 {
		lines = append(lines,
			v.theme.Dim.Render(strings.Repeat("─", width-2)),
			v.theme.Warning.Render("● BURNS"),
		)
		for _, i := range burning {
			c := w.Crafts[i]
			ab := c.ActiveBurn
			remaining := ab.EndTime.Sub(w.Clock.SimTime).Seconds()
			if remaining < 0 {
				remaining = 0
			}
			tag := fmt.Sprintf("craft %d", i+1)
			if i == w.ActiveCraftIdx {
				tag = v.theme.Warning.Render(tag + " (active)")
			} else {
				tag = v.theme.Dim.Render(tag)
			}
			lines = append(lines,
				fmt.Sprintf("  %s — %s, Δv %.0f m/s, T-%.0fs",
					tag, ab.Mode.String(), ab.DVRemaining, remaining),
			)
		}
	}

	// v0.7.4+: mission status moved off the orbit HUD into a
	// dedicated [Missions] screen reachable from the title bar
	// button (and via the screenMissions wiring in app.Update).
	// Keeps the right-hand HUD focused on flight state.

	// v0.7.6+: surface upcoming SOI / frame transitions implied by
	// the planted-node chain. Catches the v0.6.3 moon → parent
	// escape's zero-Δv arrival marker (planted in parent frame) and
	// Hohmann arrival burns (planted in destination frame). The
	// section appears only when there's a transition queued so the
	// HUD stays quiet during simple same-frame plans.
	if ft, ok := w.NextFrameTransition(); ok {
		toName := ft.To
		if b, found := bodies.LookupByID(w.Systems, ft.To); found {
			toName = b.EnglishName
		}
		fromName := ft.From
		if b, found := bodies.LookupByID(w.Systems, ft.From); found {
			fromName = b.EnglishName
		}
		dur := ft.When.Sub(w.Clock.SimTime)
		when := v.theme.Warning.Render("now")
		if dur > 0 {
			when = formatCountdown(dur)
		}
		lines = append(lines, section("FRAME TRANSITION")...)
		lines = append(lines,
			fmt.Sprintf("  %s → %s", fromName, v.theme.Warning.Render(toName)),
			fmt.Sprintf("  at %s  (node #%d)", when, ft.NodeIndex+1),
		)
	}

	// v0.8.2.x: arrival inclination preview. Surfaces the post-
	// capture orbit at the last frame-changing planted node so the
	// player catches retrograde-around-target gotchas (a prograde
	// Hohmann to Luna naturally arrives at ~110° lunar inclination)
	// before firing. The inclination is rendered in Warning when
	// > 90° (retrograde capture) or > 30° (significant plane
	// mismatch) so it stands out.
	if cap, ok := w.ArrivalCapturePreview(); ok {
		lines = append(lines, section("CAPTURE PREVIEW")...)
		lines = append(lines, fmt.Sprintf("  primary:    %s", cap.Primary.EnglishName))
		if cap.Approximate {
			// Genuinely degenerate intercept — even with the v0.8.4
			// time-aware propagator, the chained predictor lands
			// inside ~5× target radius of the center (e.g. perfect-
			// aim Hohmann, fuel-out residual, etc.). Surface
			// approach speed + qualitative direction; exact orbit
			// elements would be geometric noise at this distance.
			dirLabel := v.theme.Warning.Render("prograde")
			if cap.RetrogradeCapture {
				dirLabel = v.theme.Alert.Render("retrograde")
			}
			lines = append(lines,
				fmt.Sprintf("  approach:   %.0f m/s relative", cap.ApproachSpeed),
				fmt.Sprintf("  direction:  %s capture predicted", dirLabel),
				v.theme.Dim.Render("  (intercept too central for orbit-element preview)"),
			)
		} else {
			primaryR := cap.Primary.RadiusMeters()
			incDeg := cap.Inclination * 180 / math.Pi
			incLabel := fmt.Sprintf("%.1f°", incDeg)
			switch {
			case cap.Hyperbolic:
				incLabel = v.theme.Alert.Render("escape — capture failed")
			case incDeg > 90:
				incLabel = v.theme.Alert.Render(incLabel + " (retrograde)")
			case incDeg > 30:
				incLabel = v.theme.Warning.Render(incLabel)
			}
			lines = append(lines, fmt.Sprintf("  inclin.:    %s", incLabel))
			if !cap.Hyperbolic {
				lines = append(lines,
					fmt.Sprintf("  apoapsis:   %.0f km alt", (cap.ApoapsisM-primaryR)/1000),
					fmt.Sprintf("  periapsis:  %.0f km alt", (cap.PeriapsisM-primaryR)/1000),
				)
			}
		}
	}

	// v0.8.3+: rendezvous readout — when the active craft shares
	// its primary frame with another craft, surface range +
	// relative velocity to the nearest one. Lets the player RCS-
	// null residuals during proximity ops without guessing. The
	// "DOCK READY" callout fires when both gates are met (the
	// next tick will actually fuse).
	if c := w.ActiveCraft(); c != nil && len(w.Crafts) > 1 {
		nearest, range_, vRel, ok := findNearestSamePrimary(w.Crafts, w.ActiveCraftIdx)
		if ok {
			lines = append(lines, section("RENDEZVOUS")...)
			rangeLabel := fmt.Sprintf("%.0f m", range_)
			if range_ > 1000 {
				rangeLabel = fmt.Sprintf("%.2f km", range_/1000)
			}
			vRelLabel := fmt.Sprintf("%.2f m/s", vRel)
			withinDist := range_ <= sim.DockingDistM
			withinV := vRel <= sim.DockingVMS
			if withinDist {
				rangeLabel = v.theme.Warning.Render(rangeLabel + " ✓")
			}
			if withinV {
				vRelLabel = v.theme.Warning.Render(vRelLabel + " ✓")
			}
			lines = append(lines,
				fmt.Sprintf("  target:    %s", nearest.Name),
				fmt.Sprintf("  range:     %s", rangeLabel),
				fmt.Sprintf("  |v_rel|:   %s", vRelLabel),
			)
			if withinDist && withinV {
				lines = append(lines,
					"  "+v.theme.Alert.Render("● DOCK READY — fusing next tick"),
				)
			}
		}
	}

	// v0.8.1+: list nodes for every craft in the slate. The active
	// craft's nodes appear at full intensity; other crafts' nodes
	// fall in the Dim style with a `craft N:` prefix so the player
	// can see at a glance who has burns queued.
	hasAnyNodes := false
	for _, c := range w.Crafts {
		if c != nil && len(c.Nodes) > 0 {
			hasAnyNodes = true
			break
		}
	}
	if hasAnyNodes {
		lines = append(lines, section("NODES")...)
		multiCraft := len(w.Crafts) > 1
		for ci, c := range w.Crafts {
			if c == nil || len(c.Nodes) == 0 {
				continue
			}
			isActive := ci == w.ActiveCraftIdx
			for i, n := range c.Nodes {
				kind := "imp"
				if n.Duration > 0 {
					kind = fmt.Sprintf("fin %.0fs", n.Duration.Seconds())
				}
				// v0.8.2.x: leading ▸ marker signals "this row is
				// clickable." Post-render the HUD is scanned for
				// the marker character to map each entry to its
				// actual screen row — robust to lipgloss
				// column-padding / wrapping quirks higher up.
				prefix := fmt.Sprintf("%s #%d", hudNodeMarker, i+1)
				if multiCraft {
					prefix = fmt.Sprintf("%s c%d#%d", hudNodeMarker, ci+1, i+1)
				}
				var line string
				if !n.IsResolved() {
					line = fmt.Sprintf("  %s %s  %s  %.0f m/s  %s",
						prefix, n.Event.String(), n.Mode.String(), n.DV, kind)
				} else {
					dt := n.TriggerTime.Sub(w.Clock.SimTime).Seconds()
					line = fmt.Sprintf("  %s T%+.0fs  %s  %.0f m/s  %s",
						prefix, dt, n.Mode.String(), n.DV, kind)
				}
				if !isActive {
					line = v.theme.Dim.Render(line)
				}
				// Record render-order; absolute row is filled in by
				// the post-render scan below. Single-row entries —
				// the v0.8.2.x blank-line spacing was dropped because
				// it wasted vertical space and contributed to HUD
				// overflow on multi-craft slates.
				v.hudNodeHits = append(v.hudNodeHits, hudNodeHit{
					craftIdx: ci,
					nodeIdx:  i,
				})
				lines = append(lines, line)
			}
		}

		// v0.6.1: PROJECTED ORBIT — apo/peri/AN/DN of the orbit after
		// every planted node fires. Hidden when no resolved nodes.
		if state, primary, ok := w.PredictedFinalOrbit(); ok {
			mu := primary.GravitationalParameter()
			ro := orbital.OrbitReadout(state.R, state.V, mu)
			primaryR := primary.RadiusMeters()
			lines = append(lines, section("PROJECTED ORBIT")...)
			lines = append(lines, fmt.Sprintf("  primary:   %s", primary.EnglishName))
			if ro.Hyperbolic {
				lines = append(lines,
					"  "+v.theme.Warning.Render("hyperbolic — escape trajectory"),
					fmt.Sprintf("  periapsis: %.1f km alt", (ro.PeriMeters-primaryR)/1000),
					fmt.Sprintf("  e:         %.3f", ro.Eccentricity),
				)
			} else {
				lines = append(lines,
					fmt.Sprintf("  apoapsis:  %.1f km alt", (ro.ApoMeters-primaryR)/1000),
					fmt.Sprintf("  periapsis: %.1f km alt", (ro.PeriMeters-primaryR)/1000),
					fmt.Sprintf("  inclin.:   %.2f°", ro.Inclination*180/math.Pi),
				)
				const equatorialTol = 1e-3
				if ro.Inclination < equatorialTol || math.Abs(ro.Inclination-math.Pi) < equatorialTol {
					lines = append(lines, v.theme.Dim.Render("  AN/DN:     equatorial (undefined)"))
				} else {
					lines = append(lines,
						fmt.Sprintf("  AN angle:  %.1f°", normalizeDeg(ro.AscNode*180/math.Pi)),
						fmt.Sprintf("  DN angle:  %.1f°", normalizeDeg(ro.DescNode*180/math.Pi)),
					)
				}
			}
		}
	}

	// SYSTEM + SELECTED side-by-side, mirroring the VESSEL +
	// PROPELLANT split a few sections up. SYSTEM is short (2 lines);
	// SELECTED varies — body name + type + a/e/T, plus an optional
	// HOHMANN PREVIEW block. Pairing them recovers another ~3 rows of
	// vertical HUD height in the common case. Falls back to stacked
	// rendering when the HUD is too narrow to split.
	const minSplitWidth = 36
	if width-2 >= minSplitWidth {
		half := (width - 4) / 2
		sysLines := []string{
			v.theme.Dim.Render(strings.Repeat("─", half)),
			v.theme.Primary.Render("SYSTEM"),
			"  " + sys.Name,
			fmt.Sprintf("  %d bodies", len(sys.Bodies)),
		}
		selLines := []string{
			v.theme.Dim.Render(strings.Repeat("─", half)),
			v.theme.Primary.Render("SELECTED"),
		}
		if selectedIdx >= 0 && selectedIdx < len(sys.Bodies) {
			b := sys.Bodies[selectedIdx]
			nameStyle := lipgloss.NewStyle().Foreground(render.ColorFor(b)).Bold(true)
			selLines = append(selLines,
				"  "+nameStyle.Render(b.EnglishName),
				"  "+b.BodyType,
			)
			if b.SemimajorAxis > 0 {
				auVal := b.SemimajorAxisMeters() / bodies.AU
				selLines = append(selLines,
					fmt.Sprintf("  a: %.3f AU", auVal),
					fmt.Sprintf("  e: %.4f", b.Eccentricity),
					fmt.Sprintf("  T: %.1f d", b.SideralOrbit),
				)
				if preview := w.HohmannPreviewFor(selectedIdx); preview.Valid || preview.Note != "" {
					selLines = append(selLines, "", v.theme.Primary.Render("HOHMANN PREVIEW"))
					selLines = append(selLines, preview.Format()...)
				}
			} else {
				selLines = append(selLines, v.theme.Dim.Render("  (primary)"))
			}
		}
		colStyle := lipgloss.NewStyle().Width(half)
		sysCol := colStyle.Render(strings.Join(sysLines, "\n"))
		selCol := colStyle.Render(strings.Join(selLines, "\n"))
		combined := lipgloss.JoinHorizontal(lipgloss.Top, sysCol, "  ", selCol)
		lines = append(lines, strings.Split(combined, "\n")...)
	} else {
		lines = append(lines, section("SYSTEM")...)
		lines = append(lines,
			"  "+sys.Name,
			fmt.Sprintf("  %d bodies", len(sys.Bodies)),
		)
		lines = append(lines, section("SELECTED")...)
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
	}

	content := strings.Join(lines, "\n")
	rendered := v.theme.HUDBox.Width(width).Render(content)
	// Title bar takes row 0 of the final rendered output; HUD's
	// first row is row 1.
	v.scanHudNodeRows(rendered, 1)
	return rendered
}

// findNearestSamePrimary returns the closest non-active craft
// sharing the active craft's primary, plus their separation +
// relative speed. Used by the v0.8.3+ RENDEZVOUS HUD block to
// surface live proximity-ops feedback. Returns ok=false when no
// other craft is in the same frame.
func findNearestSamePrimary(crafts []*spacecraft.Spacecraft, activeIdx int) (*spacecraft.Spacecraft, float64, float64, bool) {
	if activeIdx < 0 || activeIdx >= len(crafts) {
		return nil, 0, 0, false
	}
	a := crafts[activeIdx]
	if a == nil {
		return nil, 0, 0, false
	}
	var (
		best     *spacecraft.Spacecraft
		bestDist = math.Inf(1)
		bestVRel float64
	)
	for i, c := range crafts {
		if i == activeIdx || c == nil {
			continue
		}
		if c.Primary.ID != a.Primary.ID {
			continue
		}
		d := c.State.R.Sub(a.State.R).Norm()
		if d < bestDist {
			bestDist = d
			bestVRel = c.State.V.Sub(a.State.V).Norm()
			best = c
		}
	}
	if best == nil {
		return nil, 0, 0, false
	}
	return best, bestDist, bestVRel, true
}

// scanHudNodeRows walks the rendered HUD line-by-line and matches
// rows containing the hudNodeMarker to the in-order hudNodeHits
// slice. titleOffset is the screen-row of the HUD's first line in
// the final rendered output (1 — title bar at row 0). v0.8.2.x.
func (v *OrbitView) scanHudNodeRows(rendered string, titleOffset int) {
	v.hudNodeRows = v.hudNodeRows[:0]
	if len(v.hudNodeHits) == 0 {
		return
	}
	rows := strings.Split(rendered, "\n")
	for i, row := range rows {
		if !strings.Contains(row, hudNodeMarker) {
			continue
		}
		v.hudNodeRows = append(v.hudNodeRows, i+titleOffset)
	}
}

// normalizeDeg wraps an angle in degrees into [0, 360).
func normalizeDeg(d float64) float64 {
	d = math.Mod(d, 360)
	if d < 0 {
		d += 360
	}
	return d
}

// viewBasis returns the canvas projection basis for the world's
// current ViewMode. Four cardinal cases — Top (XY drop), Right (YZ),
// Bottom (XY mirrored), Left (YZ mirrored) — plus orbit-flat, which
// projects onto the active craft's orbit plane via the perifocal
// (x̂, ŷ) basis so the orbit reads as a clean ellipse regardless of
// inclination. Falls back to Top's basis when the orbit is degenerate
// (no craft, e ≥ 1, a ≤ 0).
//
// Single-craft today; multi-craft will need an active-craft selector
// to disambiguate "the active orbit" (state-of-game.md §2 backlog).
func viewBasis(w *sim.World) widgets.Basis {
	switch w.ViewMode {
	case sim.ViewRight:
		return widgets.Basis{
			X: orbital.Vec3{Y: 1},
			Y: orbital.Vec3{Z: 1},
		}
	case sim.ViewBottom:
		return widgets.Basis{
			X: orbital.Vec3{X: 1},
			Y: orbital.Vec3{Y: -1},
		}
	case sim.ViewLeft:
		return widgets.Basis{
			X: orbital.Vec3{Y: -1},
			Y: orbital.Vec3{Z: 1},
		}
	case sim.ViewOrbitFlat:
		if w.ActiveCraft() == nil {
			return widgets.DefaultBasis()
		}
		mu := w.ActiveCraft().Primary.GravitationalParameter()
		if mu <= 0 {
			return widgets.DefaultBasis()
		}
		el := orbital.ElementsFromState(w.ActiveCraft().State.R, w.ActiveCraft().State.V, mu)
		if el.A <= 0 || el.E >= 1 || math.IsNaN(el.A) || math.IsInf(el.A, 0) {
			return widgets.DefaultBasis()
		}
		xHat, yHat := orbital.PerifocalBasis(el)
		return widgets.Basis{X: xHat, Y: yHat}
	}
	return widgets.DefaultBasis() // ViewTop or any future mode
}
