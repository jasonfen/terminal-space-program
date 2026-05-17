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
	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
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

	// ascentTrend caches last-frame apoapsis for the active craft so
	// the LAUNCH HUD can show a `(climbing)` / `(falling)` / `(steady)`
	// tag — the launch-flight equivalent of v0.9.3's signed
	// closing-rate readout in the TARGET HUD. Re-baselined whenever
	// the active craft pointer changes (ie spawn, cycle, or fuse), so
	// stale entries can't bleed across crafts. v0.9.4+.
	ascentTrendCraft *spacecraft.Spacecraft
	ascentTrendApoM  float64
	ascentTrendTime  time.Time

	// navSub* is a sticky copy of the navball sub-observer (nose
	// direction) point. SAS holding an attitude leaves the resolved
	// nose direction dithering by a fraction of a degree every frame;
	// feeding that raw into NavballString made the whole disk — and
	// every prograde/normal/radial marker on it — jump a cell between
	// frames (the markers "flicker"). We hold the last value and only
	// adopt the new one when the sub-observer point has moved past a
	// great-circle dead-band, so jitter is absorbed while real
	// attitude changes still track within a sub-cell lag. Also
	// subsumes the older equator-flicker fix (horizon cells no longer
	// flip blue↔orange on jitter, since the input no longer jitters).
	// v0.9.6-polish.
	navSubLatDeg, navSubLonDeg float64
	navSubValid                bool

	// navballControls holds the absolute screen-cell hit boxes for
	// the framed navball panel's clickable controls (mode + axis
	// buttons), recomputed each Render. Empty when the panel isn't
	// shown. App-side mouse routing maps a click through
	// HitNavballControl; the dispatch into SAS-hold / NavMode is the
	// follow-up wiring. v0.9.6-polish.
	navballControls []navballControlBox
}

// navballSubObserverDeadbandDeg is the great-circle angle the nose
// direction must move before the sticky navball sub-observer adopts
// the new value. The 12×6 navball has pxR=12 dots → cell pitch is
// ~8–15° depending on disk position, so a 2° dead-band sits well
// below visible motion granularity while comfortably exceeding the
// sub-degree SAS hold jitter that caused the marker flicker.
const navballSubObserverDeadbandDeg = 2.0

// stickyNavballSubObserver applies the great-circle dead-band to the
// raw (lat, lon) sub-observer point and returns the stabilised value
// the painter should use. Stateful on the OrbitView so it persists
// across frames; handles longitude wrap and the pole degeneracy
// implicitly by comparing the actual unit vectors, not the angles.
func (v *OrbitView) stickyNavballSubObserver(rawLatDeg, rawLonDeg float64) (latDeg, lonDeg float64) {
	if !v.navSubValid {
		v.navSubLatDeg, v.navSubLonDeg = rawLatDeg, rawLonDeg
		v.navSubValid = true
		return v.navSubLatDeg, v.navSubLonDeg
	}
	if latLonAngularSepDeg(v.navSubLatDeg, v.navSubLonDeg, rawLatDeg, rawLonDeg) > navballSubObserverDeadbandDeg {
		v.navSubLatDeg, v.navSubLonDeg = rawLatDeg, rawLonDeg
	}
	return v.navSubLatDeg, v.navSubLonDeg
}

// latLonAngularSepDeg returns the great-circle angle in degrees
// between two (lat, lon) points on the unit sphere. Used for the
// navball sub-observer dead-band; wrap- and pole-safe because it
// works on the Cartesian directions, not raw angle differences.
func latLonAngularSepDeg(lat1, lon1, lat2, lon2 float64) float64 {
	const d2r = math.Pi / 180.0
	p1, l1 := lat1*d2r, lon1*d2r
	p2, l2 := lat2*d2r, lon2*d2r
	x1, y1, z1 := math.Cos(p1)*math.Cos(l1), math.Cos(p1)*math.Sin(l1), math.Sin(p1)
	x2, y2, z2 := math.Cos(p2)*math.Cos(l2), math.Cos(p2)*math.Sin(l2), math.Sin(p2)
	dot := x1*x2 + y1*y2 + z1*z2
	if dot > 1 {
		dot = 1
	} else if dot < -1 {
		dot = -1
	}
	return math.Acos(dot) * 180.0 / math.Pi
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
	v.fitted = false                         // force refit after resize
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
	canvasReach := v.canvas.Cols()*2 + v.canvas.Rows()*4
	// v0.8.5: when the focused craft is landed (altitude ≤ 0) — or the
	// focused body is its primary — cap the zoom so the body's
	// projected radius stays within canvas reach. Above this cap the
	// body's drawn disk (capped at canvasReach px) can no longer reach
	// back from its off-canvas center to the craft's pixel position,
	// leaving the landed craft floating in empty space. Clamping every
	// frame is intentional — manual [+] past the cap is silently
	// ignored so the surface contact stays visually consistent with
	// the HUD altitude readout. Skipped when the focus is unrelated
	// (system view or a different body) so an unrelated zoom-in isn't
	// surprisingly capped.
	if c := w.ActiveCraft(); c != nil && c.Altitude() <= 0 {
		focused := false
		switch w.Focus.Kind {
		case sim.FocusCraft:
			focused = true
		case sim.FocusBody:
			if w.Focus.BodyIdx >= 0 && w.Focus.BodyIdx < len(sys.Bodies) &&
				sys.Bodies[w.Focus.BodyIdx].ID == c.Primary.ID {
				focused = true
			}
		}
		if focused {
			if r := c.Primary.RadiusMeters(); r > 0 {
				maxScale := float64(canvasReach) / r
				if v.canvas.Scale() > maxScale {
					v.canvas.SetScale(maxScale)
				}
			}
		}
	}
	scale := v.canvas.Scale()
	for i := range sys.Bodies {
		b := sys.Bodies[i]
		pos := w.BodyPosition(b)
		r := BodyPixelRadius(b, i == 0, scale, canvasReach)
		color := render.ColorFor(b)
		// v0.6.4: tag body pixels with BodyID so HitAt resolves
		// mouse clicks back to the body for click-to-focus.
		bodyTag := widgets.CellTag{Color: color, BodyID: b.ID}
		// v0.8.5.7+: compute view-aware sub-observer point per body
		// per frame. Camera direction comes from the canvas's
		// current ViewMode; body axis tilt + sim-time rotation
		// drive the (lat, lon) at the visible center. For tidally-
		// locked bodies we also pass the body→parent direction so
		// the prime meridian (lon=0) tracks the parent — keeps
		// Luna's near side facing Earth as it orbits, instead of
		// being fixed in inertial frame.
		primMer := render.Vec3{}
		camDir := v.cameraDirForView(w.ViewMode)
		if b.TidallyLocked {
			if parent := sys.ParentOf(b); parent != nil && parent.ID != b.ID {
				parentPos := w.BodyPosition(*parent)
				rel := parentPos.Sub(pos)
				primMer = render.Vec3{X: rel.X, Y: rel.Y, Z: rel.Z}
				// Tidally-locked bodies always show their
				// parent-facing side regardless of the canvas view
				// mode — the player's mental model is "Luna always
				// shows its near-side", and the iconic mare pattern
				// matters more than geometric consistency with the
				// canvas's projection. Free bodies still pick up
				// the canvas view direction as expected.
				camDir = primMer
			}
		}
		subLat, subLon := render.SubObserverPointDeg(b, w.Clock.RotationTime, camDir, primMer)
		if tex := render.TextureFor(b, r, subLat, subLon); tex != nil {
			pxR := r
			v.canvas.FillTexturedDiskTagged(pos, r, func(dx, dy int) lipgloss.Color {
				return tex(dx, dy, pxR)
			}, bodyTag)
		} else {
			v.canvas.FillColoredDiskTagged(pos, r, bodyTag)
		}
		// v0.8.5.7+: stars get a faint two-ring corona halo so the
		// disk reads as "this is a luminous body" instead of a
		// generic colored circle. Replaces the i == 0 crosshair-
		// style ring + center dot the orbit screen used pre-v0.8.5.7.
		if b.BodyType == "Star" {
			for _, mult := range []float64{1.4, 1.8} {
				cpx := int(float64(r) * mult)
				if cpx > r && cpx < canvasReach {
					v.canvas.RingColoredOutline(pos, cpx, render.ColorSunCorona)
				}
			}
		}
		// Draw rings for ringed bodies (v0.5.11). World-scale ring
		// radii project to pixel radii via the canvas scale; only
		// draw when the outer ring would visibly clear the body's
		// rendered disk. v0.5.15: skip if outerPx is beyond a sane
		// canvas multiple — at extreme zoom the ring projects to
		// millions of pixels and is entirely off-canvas anyway.
		// v0.8.5.7+: ring lies in the body's equatorial plane, not
		// the screen plane (foreshortens per camera direction +
		// AxialTilt) AND splits into per-band annuli (Saturn's C /
		// B / A / F rings with the Cassini Division as a visible
		// gap), drawn as filled concentric outlines through each
		// band so the band reads as a coherent surface rather than
		// a single perimeter.
		if bands := render.BodyRingBands(b.ID); len(bands) > 0 {
			_, outerR, _ := render.BodyRings(b.ID)
			outerPx := int(outerR * scale)
			if outerPx > r && outerPx < canvasReach {
				e1, e2 := render.BodyRingBasisWorld(b)
				oe1 := vec3FromRender(e1)
				oe2 := vec3FromRender(e2)
				for _, band := range bands {
					// Fill each band by drawing concentric outlines
					// at 1-pixel-screen-spacing radii. Cap per-band
					// outline count so a deeply-zoomed ring system
					// doesn't blow the loop budget; the canvas
					// already caps samples-per-outline.
					widthPx := int((band.OuterR - band.InnerR) * scale)
					if widthPx < 1 {
						widthPx = 1
					}
					n := widthPx
					if n > 64 {
						n = 64
					}
					for i := 0; i < n; i++ {
						t := (float64(i) + 0.5) / float64(n)
						bandR := band.InnerR + t*(band.OuterR-band.InnerR)
						v.canvas.RingTiltedOutline(pos, oe1, oe2, bandR, band.Color)
					}
				}
			}
		}
		// v0.8.4: atmospheric haze ring at (cutoff + scale-height)
		// outside the body, floored to bodyPx + AtmosphereMinHaloPx
		// so the halo always reads as a thin ring just outside the
		// disk regardless of zoom.
		if render.AtmosphereVisible(b, r) {
			outerPx := render.AtmosphereOuterPx(b, scale, r)
			if outerPx > r && outerPx < canvasReach {
				v.canvas.RingColoredOutline(pos, outerPx, render.AtmosphereHazeColor(b))
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

	// v0.9.3 polish: vessel position trail no longer renders by
	// default. With multiple craft + the v0.9.3 target-orbit
	// highlight, trails read as clutter in the multi-orbit views.
	// `World.CraftTrail()` still ticks behind the scenes — fast
	// reintroduction as a toggle is trivial if it turns out players
	// miss the breadcrumb cue.

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
		primaryPxR := BodyPixelRadius(c.Primary, false, scale, canvasReach)
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
		targetCraftIdx := -1
		if w.Target.Kind == sim.TargetCraft {
			targetCraftIdx = w.Target.CraftIdx
		}
		for i, other := range w.Crafts {
			if i == w.ActiveCraftIdx || other == nil {
				continue
			}
			otherPrimaryPos := w.BodyPosition(other.Primary)
			otherPxR := BodyPixelRadius(other.Primary, false, scale, canvasReach)
			otherInertial := otherPrimaryPos.Add(other.State.R)
			otherEl := orbital.ElementsFromState(other.State.R, other.State.V, other.Primary.GravitationalParameter())
			otherOrbitVisible := otherEl.A > 0 && !math.IsNaN(otherEl.A) && !math.IsInf(otherEl.A, 0) && otherEl.Apoapsis()*scale >= minOrbitPixels
			isTarget := i == targetCraftIdx
			otherColor := lipgloss.Color(render.ColorDim)
			if other.Color != "" {
				otherColor = lipgloss.Color(other.Color)
			}
			if isTarget {
				// v0.9.3 polish: target's orbit + glyph render in the
				// dedicated TARGET green so the player can pick out
				// which non-active craft is the bound target at a
				// glance, even with multiple craft sharing the view.
				otherColor = render.ColorTarget
			}
			if otherOrbitVisible {
				// Target gets denser sampling (every 3rd vs every 5th
				// for plain non-active craft) so its track reads as
				// brighter / more prominent than peers.
				stride := 5
				if isTarget {
					stride = 3
				}
				v.canvas.DrawEllipseOffsetOccluded(otherEl, otherPrimaryPos, 180, stride, otherPrimaryPos, otherPxR, otherColor)
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

	// Stamp the active projection in the canvas's bottom-left corner
	// so the indicator stays attached to the view it describes (was a
	// HUD line under FOCUS until v0.7.4 — see orbit.go's renderHUD).
	// v0.9.6-polish moved it left so it no longer sits under the
	// bottom-right navball panel.
	viewLabel := "view: " + w.ViewMode.String()
	v.canvas.SetCellLabel(0, v.canvas.Rows()-1, viewLabel)

	canvasStr := v.canvas.String()

	// Framed navball panel, composited into the bottom-right corner
	// of the canvas (v0.9.6-polish). Gated on an active craft with a
	// defined nose direction and a canvas big enough that the opaque
	// panel doesn't crowd out the map. Lifted one row off the bottom
	// so the canvas's right-aligned "view:" label on the last row
	// stays visible underneath it. The sub-observer keeps the sticky
	// dead-band that killed the marker flicker.
	v.navballControls = v.navballControls[:0]
	cCols, cRows := v.canvas.Cols(), v.canvas.Rows()
	if w.CraftVisibleHere() &&
		cCols >= navballPanelW+2 && cRows >= navballPanelH+2 {
		if rawLat, rawLon, ok := w.NavballSubObserver(); ok {
			subLat, subLon := v.stickyNavballSubObserver(rawLat, rawLon)
			disk := navballPanelDisk(w, subLat, subLon)
			panel, boxes := v.buildNavballPanel(disk, w.NavMode, w.RCSActive())
			atCol := cCols - navballPanelW
			atRow := cRows - navballPanelH - 1
			lines := strings.Split(canvasStr, "\n")
			lines = overlayStyledBlock(lines, panel, atRow, atCol, cCols)
			canvasStr = strings.Join(lines, "\n")
			// Screen offset: title row (1) + canvas top border (1)
			// for rows; canvas left border (1) for cols.
			for _, b := range boxes {
				v.navballControls = append(v.navballControls, navballControlBox{
					id:       b.id,
					colStart: atCol + b.colStart + 1,
					colEnd:   atCol + b.colEnd + 1,
					row:      atRow + b.row + 2,
				})
			}
		}
	}

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

// HitNavballControl maps a screen-space click to a navball-panel
// control, returning navballControlNone (ok=false) on a miss. Hit
// boxes are recomputed every Render in absolute screen coordinates.
// The app's mouse handler calls this; the dispatch from each control
// id into the NavMode cycle / SAS-hold intent is the follow-up
// wiring step (the panel layout intentionally reserves the room).
// v0.9.6-polish.
func (v *OrbitView) HitNavballControl(col, row int) (NavballControlID, bool) {
	for _, b := range v.navballControls {
		if row == b.row && col >= b.colStart && col < b.colEnd {
			return b.id, true
		}
	}
	return navballControlNone, false
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
func BodyPixelRadius(b bodies.CelestialBody, isPrimary bool, scale float64, maxPx int) int {
	const trueSizeThreshold = 4
	// v0.8.4: cap is now passed in. Render callers thread canvas
	// reach so the body disk can grow to fill the canvas at close
	// zoom — without that, an altitude-0 craft renders visibly
	// outside the disk, contradicting the HUD altitude readout.
	// maxPx ≤ 0 falls back to a safe legacy default for tests and
	// non-render callers (hit-test math).
	cap := 512
	if maxPx > 0 {
		cap = maxPx
	}
	r := b.RadiusMeters()
	if scale > 0 && r > 0 {
		truePx := int(math.Round(r * scale))
		if truePx >= trueSizeThreshold {
			if truePx > cap {
				truePx = cap
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

// vec3FromRender adapts a render.Vec3 to an orbital.Vec3 for code
// that crosses the package boundary (the canvas widget operates
// on orbital.Vec3; render computes its own Vec3 to stay free of
// orbital imports). Both types share the same {X, Y, Z float64}
// shape; this is just a name-change hop.
func vec3FromRender(v render.Vec3) orbital.Vec3 {
	return orbital.Vec3{X: v.X, Y: v.Y, Z: v.Z}
}

// cameraDirForView maps a sim.ViewMode to the world-frame body-to-
// camera direction the texture pipeline uses to compute the
// sub-observer point. For the cardinal views the direction is a
// fixed world-axis unit vector; for ViewOrbitFlat it's the
// canvas's current depth axis — i.e. the active craft's orbit-
// plane normal that the canvas already configured for the orbit-
// flat projection. v0.8.5.7+: orbit-flat now picks up the
// dynamically-computed camera direction instead of falling back
// to top.
func (v *OrbitView) cameraDirForView(view sim.ViewMode) render.Vec3 {
	switch view {
	case sim.ViewTop:
		return render.CameraDirTop
	case sim.ViewBottom:
		return render.CameraDirBottom
	case sim.ViewRight:
		return render.CameraDirRight
	case sim.ViewLeft:
		return render.CameraDirLeft
	case sim.ViewOrbitFlat:
		// Depth axis points out of screen toward the camera, which
		// is exactly the body-to-camera direction the sub-observer
		// math wants. For a craft on a near-equatorial orbit this is
		// approximately +Z; for inclined orbits it tips accordingly.
		d := v.canvas.Basis().DepthAxis()
		return render.Vec3{X: d.X, Y: d.Y, Z: d.Z}
	}
	return render.CameraDirTop
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
		segs := w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, samples)
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
		// Inclination/Ω/ω are quoted in the primary's reference frame
		// (body-equatorial for non-Sun primaries, ecliptic for the
		// Sun) — matches the operational convention every mission
		// planner uses. v0.8.6+.
		frame := orbital.ReferenceFrameForPrimary(c.Primary)
		el := orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
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
			fmt.Sprintf("  nav:       %s", w.NavMode),
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

	// v0.9.2+: LAUNCH HUD — visible when the active craft hasn't yet
	// achieved a stable orbit (periapsis below the primary radius)
	// AND it's still inside or just outside the atmosphere. Surfaces
	// the manual gravity-turn instruments: altitude AGL, vertical-v,
	// horizontal-v relative to the rotating surface, downrange from
	// the launch point, and TWR (active stage thrust / current mass /
	// surface gravity). Vanishes once the craft circularises so the
	// orbit-relative HUD blocks below take over.
	if c := w.ActiveCraft(); c != nil && shouldShowLaunchHUD(c) {
		altAGL := c.Altitude()
		// Vertical / horizontal split of velocity in the body's
		// rotating frame: vertical = v · r̂, horizontal = |v - v_vert·r̂|
		// after subtracting the surface co-rotation ω×r so a craft
		// sitting on the pad reads 0 m/s on both axes. Use the
		// tilted spin axis (matches the launchpad spawn frame); the
		// Z-aligned physics.AtmosphereOmega leaves a ~150 m/s
		// residual at Earth's 23.5° axial tilt.
		omegaRender := render.BodySpinOmegaWorld(c.Primary)
		omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
		vRel := c.State.V.Sub(omega.Cross(c.State.R))
		rNorm := c.State.R.Norm()
		var vVert, vHoriz, fpaDeg, fpaOrbitDeg float64
		hasFPA := false
		hasFPAOrbit := false
		if rNorm > 0 {
			rHat := c.State.R.Scale(1 / rNorm)
			// vRel · rHat (radial component) — orbital.Vec3 has no
			// Dot method; inline as X*X + Y*Y + Z*Z.
			vVert = vRel.X*rHat.X + vRel.Y*rHat.Y + vRel.Z*rHat.Z
			vHorizVec := vRel.Sub(rHat.Scale(vVert))
			vHoriz = vHorizVec.Norm()
			// Flight-path angle: 90° = straight up, 0° = horizontal.
			// Only meaningful once moving — atan2 is undefined when
			// both components are zero (craft on the pad).
			if vRel.Norm() > 1.0 {
				fpaDeg = math.Atan2(vVert, vHoriz) * 180 / math.Pi
				hasFPA = true
			}
			// Orbit-frame fpa: same split, but on the inertial velocity
			// vector (no surface co-rotation subtracted). What matters
			// for the upper-stage circularization burn — surface
			// prograde diverges from orbit prograde once Earth's
			// rotation contribution becomes a small fraction of the
			// total speed.
			vOrbit := c.State.V
			if vOrbit.Norm() > 1.0 {
				vVertOrbit := vOrbit.X*rHat.X + vOrbit.Y*rHat.Y + vOrbit.Z*rHat.Z
				vHorizOrbit := vOrbit.Sub(rHat.Scale(vVertOrbit)).Norm()
				fpaOrbitDeg = math.Atan2(vVertOrbit, vHorizOrbit) * 180 / math.Pi
				hasFPAOrbit = true
			}
		}
		// TWR: active-stage thrust / (mass · surface gravity).
		twrLabel := "—"
		if c.Thrust > 0 && c.TotalMass() > 0 {
			gSurface := c.Primary.GravitationalParameter() / (c.Primary.RadiusMeters() * c.Primary.RadiusMeters())
			twr := c.Thrust * c.EffectiveThrottle() / (c.TotalMass() * gSurface)
			twrLabel = fmt.Sprintf("%.2f", twr)
			if twr < 1.0 {
				twrLabel = v.theme.Alert.Render(twrLabel + " (will not lift)")
			}
		}
		altLabel := fmt.Sprintf("%.0f m", altAGL)
		if altAGL >= 1000 {
			altLabel = fmt.Sprintf("%.2f km", altAGL/1000)
		}
		// v0.9.2+: SAS row + pitch-trim row so the player can see
		// what the autopilot is holding and how much east-trim is
		// stacked on top.
		sasLabel := c.AttitudeMode.String()
		trimDeg := c.PitchTrim * 180 / math.Pi
		trimLabel := fmt.Sprintf("%+.1f°", trimDeg)
		if math.Abs(trimDeg) > 0.05 {
			trimLabel = v.theme.Warning.Render(trimLabel)
		}
		lines = append(lines, section("LAUNCH")...)
		fpaLabel := "—"
		if hasFPA {
			fpaLabel = fmt.Sprintf("%.0f° (90 = up, 0 = horiz)", fpaDeg)
		}
		fpaOrbitLabel := "—"
		if hasFPAOrbit {
			fpaOrbitLabel = fmt.Sprintf("%.0f° (inertial)", fpaOrbitDeg)
		}
		lines = append(lines,
			fmt.Sprintf("  altitude:   %s", altLabel),
			fmt.Sprintf("  v_vert:     %.1f m/s", vVert),
			fmt.Sprintf("  v_horiz:    %.0f m/s (surface-rel)", vHoriz),
			fmt.Sprintf("  fpa:        %s", fpaLabel),
			fmt.Sprintf("  fpa_orbit:  %s", fpaOrbitLabel),
			fmt.Sprintf("  twr:        %s", twrLabel),
			fmt.Sprintf("  sas:        %s", sasLabel),
			fmt.Sprintf("  trim:       %s", trimLabel),
		)
		// v0.9.4+: live ascent prediction — apoapsis / periapsis / time-
		// to-apoapsis / Δv-to-circularise. Mirrors v0.9.3's TARGET HUD
		// pattern (live numbers shrink as the player nudges throttle)
		// so the player can fly the gravity turn by watching ap climb
		// instead of memorising a profile. Computed once per frame from
		// orbital.ElementsFromStateInFrame; trend tag is finite-
		// differenced against the last frame.
		mu := c.Primary.GravitationalParameter()
		primaryR := c.Primary.RadiusMeters()
		frame := orbital.ReferenceFrameForPrimary(c.Primary)
		el := orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
		var (
			apoAlt, periAlt float64
			apoFinite       bool
		)
		if !math.IsNaN(el.A) && !math.IsInf(el.A, 0) && el.A > 0 && el.E < 1 {
			apoAlt = el.Apoapsis() - primaryR
			periAlt = el.Periapsis() - primaryR
			apoFinite = true
		}
		apLabel := "—"
		peLabel := "—"
		ttaLabel := "—"
		dvCircLabel := "—"
		tBurnLabel := "—"
		trendLabel := ""
		var dvCirc float64
		if apoFinite {
			apLabel = formatAltKm(apoAlt)
			peLabel = formatAltKm(periAlt)
			// Δapo/dt finite-diff against last frame on this craft.
			// First frame on a craft (or after the active craft pointer
			// changes) seeds the cache; subsequent frames produce a
			// tag. Threshold of 1 m/s renders as `(steady)`.
			now := w.Clock.SimTime
			if v.ascentTrendCraft == c && !v.ascentTrendTime.IsZero() {
				dt := now.Sub(v.ascentTrendTime).Seconds()
				if dt > 1e-6 {
					rate := (el.Apoapsis() - v.ascentTrendApoM) / dt
					switch {
					case rate > 1.0:
						trendLabel = " (climbing)"
					case rate < -1.0:
						trendLabel = " (falling)"
					default:
						trendLabel = " (steady)"
					}
				}
			}
			v.ascentTrendCraft = c
			v.ascentTrendApoM = el.Apoapsis()
			v.ascentTrendTime = now

			// t_to_apo only meaningful once apoapsis clears the
			// surface; on a sub-orbital arc the "next apoapsis" math
			// resolves but the readout is misleading (apoapsis is
			// underground).
			if apoAlt > 0 {
				ttaSec := orbital.TimeToApoapsis(orbital.Vec3State{R: c.State.R, V: c.State.V}, mu)
				if ttaSec > 0 {
					ttaLabel = formatDurationShort(ttaSec)
				}
			}
			// Δv→circ at apoapsis. Vis-viva at r_apo gives current
			// along-track speed there; circular speed is sqrt(mu/r_apo);
			// the difference is the prograde Δv. Labelled (impulsive)
			// because the calc assumes an instantaneous on-axis burn —
			// real low-TWR upper-stage burns eat 15–40% more from
			// finite-burn time, gravity loss, and drift past apoapsis.
			// The companion t_burn row surfaces the duration so the
			// player can sanity-check whether finite-burn losses will
			// be substantial.
			rApo := el.Apoapsis()
			if rApo > primaryR && el.A > 0 {
				vAtApo := math.Sqrt(mu * (2/rApo - 1/el.A))
				vCircAtApo := math.Sqrt(mu / rApo)
				dvCirc = vCircAtApo - vAtApo
				if dvCirc > 0 {
					dvCircLabel = fmt.Sprintf("%.0f m/s (impulsive)", dvCirc)
				}
			}
		} else {
			// Hyperbolic / degenerate — clear the trend cache so the
			// next bound state seeds fresh.
			v.ascentTrendCraft = nil
		}
		// t_burn: how long the active engine would take to deliver the
		// impulsive Δv at current thrust + mass. Mass-flow is small
		// over a single burn relative to mass, so we approximate with
		// the constant-mass form Δv·m/F. Hidden when no Δv→circ value
		// (apoapsis below surface) or no thrust (engine cut, dry stage).
		if dvCirc > 0 && c.Thrust > 0 && c.TotalMass() > 0 {
			thrust := c.Thrust * c.EffectiveThrottle()
			if thrust <= 0 {
				thrust = c.Thrust // ignore throttle when off — show what full burn would take
			}
			tBurnSec := dvCirc * c.TotalMass() / thrust
			tBurnLabel = formatDurationShort(tBurnSec)
		}
		lines = append(lines,
			fmt.Sprintf("  ap:         %s%s", apLabel, trendLabel),
			fmt.Sprintf("  pe:         %s", peLabel),
			fmt.Sprintf("  t_to_apo:   %s", ttaLabel),
			fmt.Sprintf("  Δv→circ:    %s", dvCircLabel),
			fmt.Sprintf("  t_burn:     %s", tBurnLabel),
		)
		// v0.9.4+: ORBIT READY callout — fires when apoapsis is above
		// the mission floor, signalling "your apoapsis is in space,
		// coast there and press C to plant the circularisation node."
		// Mirrors v0.9.3's DOCK READY pattern (live signal + bold
		// green styling) but gates on apoapsis (the actionable
		// threshold while still ascending), not periapsis (which
		// crosses the floor only at circularisation, by which time
		// the LAUNCH HUD is one frame from vanishing because the
		// mission has passed).
		if apoFinite && apoAlt > launchMissionFloorM {
			orbitStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3DDC84")).Bold(true)
			lines = append(lines,
				"  "+orbitStyle.Render("● ORBIT READY — coast to ap, press C to plant circularise"),
			)
		}
		// v0.9.4+: live mission progress — surfaces the
		// saturn-v-pad-to-leo floor distance below the predictive
		// rows so the player sees a single number to chase ("pe 130 km
		// / 200 km target") instead of guessing whether they're close.
		// Reads off the primary's periapsis altitude in the same units
		// as the mission predicate.
		if apoFinite {
			progress := launchMissionProgress(w, c, periAlt)
			if progress != "" {
				lines = append(lines, "  "+progress)
			}
		}
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

	// v0.9.0+: TARGET block — surfaces the unified World.Target slot
	// that `H` planted-Hohmann and `I` plane-match consume in place of
	// the pre-v0.9 implicit body cursor. Hidden when no target is set.
	// For TargetBody: name, body-equatorial Δi vs active craft, current
	// range. For TargetCraft: name + role, current range, |v_rel|.
	// v0.9.3 will extend the craft path with live closest-approach
	// countdown + DOCK READY indicator (today's RENDEZVOUS block,
	// rebuilt on the explicit target rather than implicit-nearest).
	if c := w.ActiveCraft(); c != nil && w.Target.Kind != sim.TargetNone {
		switch w.Target.Kind {
		case sim.TargetBody:
			sysT := w.System()
			if w.Target.BodyIdx > 0 && w.Target.BodyIdx < len(sysT.Bodies) {
				b := sysT.Bodies[w.Target.BodyIdx]
				lines = append(lines, section("TARGET")...)
				nameStyle := lipgloss.NewStyle().Foreground(render.ColorFor(b)).Bold(true)
				lines = append(lines,
					"  body:   "+nameStyle.Render(b.EnglishName),
				)
				// Δi in the active craft's primary frame. Active
				// craft's inclination via OrbitReadoutInFrame; target
				// body's plane via PlaneMatchInclination — both reduce
				// to a single scalar in [0, π], so |a − b| is a
				// meaningful "how far off the target plane am I" delta
				// even without comparing Ω.
				mu := c.Primary.GravitationalParameter()
				frame := orbital.ReferenceFrameForPrimary(c.Primary)
				ro := orbital.OrbitReadoutInFrame(c.State.R, c.State.V, mu, frame)
				if !ro.Hyperbolic {
					tgtIncl := orbital.PlaneMatchInclination(b, frame)
					di := math.Abs(ro.Inclination-tgtIncl) * 180 / math.Pi
					diLabel := fmt.Sprintf("%.2f°", di)
					if di > 30 {
						diLabel = v.theme.Warning.Render(diLabel)
					}
					lines = append(lines, fmt.Sprintf("  Δi:     %s", diLabel))
				}
				rangeM := w.BodyPosition(b).Sub(w.CraftInertial()).Norm()
				rangeLabel := fmt.Sprintf("%.0f m", rangeM)
				switch {
				case rangeM > bodies.AU/10:
					rangeLabel = fmt.Sprintf("%.3f AU", rangeM/bodies.AU)
				case rangeM > 1e6:
					rangeLabel = fmt.Sprintf("%.0f km", rangeM/1000)
				case rangeM > 1000:
					rangeLabel = fmt.Sprintf("%.2f km", rangeM/1000)
				}
				lines = append(lines, fmt.Sprintf("  range:  %s", rangeLabel))
			}
		case sim.TargetCraft:
			if w.Target.CraftIdx >= 0 && w.Target.CraftIdx < len(w.Crafts) {
				if tc := w.Crafts[w.Target.CraftIdx]; tc != nil {
					lines = append(lines, section("TARGET")...)
					nameLine := "  craft:  " + tc.Name
					if tc.Role != "" {
						nameLine += v.theme.Dim.Render(" — " + tc.Role)
					}
					lines = append(lines, nameLine)
					// Target orbit shape: apo/peri altitudes around its
					// own primary. Always meaningful (target's state is
					// already primary-relative), regardless of whether
					// active craft shares the primary.
					tMu := tc.Primary.GravitationalParameter()
					tFrame := orbital.ReferenceFrameForPrimary(tc.Primary)
					tEl := orbital.ElementsFromStateInFrame(tc.State.R, tc.State.V, tMu, tFrame)
					if tEl.A > 0 && !math.IsNaN(tEl.A) && !math.IsInf(tEl.A, 0) {
						tPrimaryR := tc.Primary.RadiusMeters()
						lines = append(lines,
							fmt.Sprintf("  apoapsis:  %.1f km", (tEl.Apoapsis()-tPrimaryR)/1000),
							fmt.Sprintf("  periapsis: %.1f km", (tEl.Periapsis()-tPrimaryR)/1000),
						)
					}
					// Range / |v_rel|: use primary-frame deltas when
					// they share a primary (the common rendezvous
					// scenario), inertial otherwise (cross-SOI
					// targeting works but reads as a long-distance
					// pointer — v0.9.3 closest-approach maths will
					// make this useful).
					var rRel, vRelVec orbital.Vec3
					if tc.Primary.ID == c.Primary.ID {
						rRel = tc.State.R.Sub(c.State.R)
						vRelVec = tc.State.V.Sub(c.State.V)
					} else {
						tcInertial := w.BodyPosition(tc.Primary).Add(tc.State.R)
						rRel = tcInertial.Sub(w.CraftInertial())
						vRelVec = w.CraftInertialVelocity(tc).Sub(w.CraftInertialVelocity(c))
					}
					rangeM := rRel.Norm()
					vRel := vRelVec.Norm()
					// Closing rate: positive = approaching, negative =
					// receding. Sign tells the player whether they're
					// faster or slower along-track than target.
					var closing float64
					if rangeM > 0 {
						closing = -rRel.Dot(vRelVec) / rangeM
					}
					rangeLabel := fmt.Sprintf("%.0f m", rangeM)
					switch {
					case rangeM > 1e6:
						rangeLabel = fmt.Sprintf("%.0f km", rangeM/1000)
					case rangeM > 1000:
						rangeLabel = fmt.Sprintf("%.2f km", rangeM/1000)
					}
					lines = append(lines,
						fmt.Sprintf("  range:   %s", rangeLabel),
						fmt.Sprintf("  |v_rel|: %.2f m/s", vRel),
						fmt.Sprintf("  closing: %+.2f m/s", closing),
					)
					// v0.9.3+: rendezvous block — time + distance to next
					// closest approach, DOCK READY indicator. Only when
					// both craft share a primary; cross-SOI rendezvous is
					// out of scope for the manual loop. Horizon: 4 hours
					// covers 2–3 LEO periods, which is the typical
					// player-controlled rendezvous window.
					if tc.Primary.ID == c.Primary.ID {
						rT, vT, ok := w.TargetStateRelativeToActivePrimary()
						if ok {
							active := orbital.Vec3State{R: c.State.R, V: c.State.V}
							target := orbital.Vec3State{R: rT, V: vT}
							mu := c.Primary.GravitationalParameter()
							const horizon = 4 * 3600.0
							if tCA, distCA, _, err := planner.NextClosestApproach(active, target, c.Primary, mu, horizon); err == nil {
								tcaLabel := fmt.Sprintf("%.0fs", tCA)
								switch {
								case tCA >= 3600:
									tcaLabel = fmt.Sprintf("%.2fh", tCA/3600)
								case tCA >= 60:
									tcaLabel = fmt.Sprintf("%.1fmin", tCA/60)
								}
								caLabel := fmt.Sprintf("%.0f m", distCA)
								switch {
								case distCA > 1e6:
									caLabel = fmt.Sprintf("%.0f km", distCA/1000)
								case distCA > 1000:
									caLabel = fmt.Sprintf("%.2f km", distCA/1000)
								}
								lines = append(lines,
									fmt.Sprintf("  TCA:    %s", tcaLabel),
									fmt.Sprintf("  CA:     %s", caLabel),
								)
							}
						}
						// DOCK READY: current range < 50 m && |v_rel| <
						// 0.1 m/s. Gates on v0.8.3 DockCrafts which the
						// player invokes once the indicator lights.
						if rangeM < 50 && vRel < 0.1 {
							dockStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3DDC84")).Bold(true)
							lines = append(lines, "  "+dockStyle.Render("DOCK READY"))
						}
					}
				}
			}
		}
	}

	// v0.9.3 polish: the v0.8.3 RENDEZVOUS block (nearest-craft
	// auto-surface) was dropped — the v0.9.0 TARGET HUD block now
	// covers the same range / |v_rel| / DOCK READY readouts and
	// adds closing rate, TCA, CA, and target apo/peri. Player must
	// bind a target with `H`/`I` to see proximity-ops feedback now,
	// but the duplication was reading as noise on the HUD.

	// v0.9.1+: STAGES block — lists per-stage thrust / Isp / fuel%
	// for the active craft when it has more than one stage. Top-of-
	// list is the bottom (currently-firing) stage; subsequent rows
	// are the upper-stage chain. Hidden for single-stage craft (the
	// existing PROPELLANT block already covers them). The bottom
	// stage is highlighted in Warning so the player sees at a
	// glance which engine `b` will fire / `space` will jettison.
	if c := w.ActiveCraft(); c != nil && len(c.Stages) > 1 {
		lines = append(lines, section("STAGES")...)
		for i, st := range c.Stages {
			fuelPct := 0.0
			if st.FuelCapacity > 0 {
				fuelPct = 100 * st.FuelMass / st.FuelCapacity
			}
			label := st.Name
			if label == "" {
				label = st.LoadoutID
			}
			if label == "" {
				label = fmt.Sprintf("stage %d", i)
			}
			thrustLabel := "RCS-only"
			if st.Thrust > 0 {
				thrustLabel = fmt.Sprintf("%.0fkN @ Isp %.0fs", st.Thrust/1000, st.Isp)
			}
			row := fmt.Sprintf("  %-8s  %-22s  fuel %5.1f%%", label, thrustLabel, fuelPct)
			if i == 0 {
				row = v.theme.Warning.Render("▸ " + row[2:])
			} else {
				row = v.theme.Dim.Render(row)
			}
			lines = append(lines, row)
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
			frame := orbital.ReferenceFrameForPrimary(primary)
			ro := orbital.OrbitReadoutInFrame(state.R, state.V, mu, frame)
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

	// NAVBALL moved out of the HUD column in v0.9.6-polish — it now
	// renders as a framed panel composited bottom-center over the
	// canvas (see Render / buildNavballPanel), so it no longer
	// competes with TARGET / NODES for HUD height.

	content := strings.Join(lines, "\n")
	rendered := v.theme.HUDBox.Width(width).Render(content)
	// Title bar takes row 0 of the final rendered output; HUD's
	// first row is row 1.
	v.scanHudNodeRows(rendered, 1)
	return rendered
}

// shouldShowLaunchHUD returns true when the active craft is in
// "ascent" mode — defined v0.9.4+ as "periapsis below the
// circularize-from-pad mission floor" (200 km altitude). Visible
// for the whole pad → coast → circularise journey, vanishing only
// once the mission-floor periapsis is achieved (= LEO is captured).
// v0.9.2+ originally hid the HUD as soon as periapsis cleared the
// surface or the craft left the atmosphere; that hid the very
// signals (ap, pe, Δv→circ, ORBIT READY) the player needs during
// coast and circularisation, so v0.9.4+ keeps it up through the
// whole ascent phase. Hyperbolic / degenerate states keep the HUD
// up too (they read "—" rather than misleading numbers).
func shouldShowLaunchHUD(c *spacecraft.Spacecraft) bool {
	if c == nil {
		return false
	}
	if c.Primary.Atmosphere == nil {
		// No atmosphere → no launch profile to display. (Also
		// covers most moons, where ground launch is reachable but
		// the manual loop is short enough to not need a dedicated
		// HUD block.)
		return false
	}
	mu := c.Primary.GravitationalParameter()
	if mu == 0 {
		return false
	}
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if el.E >= 1 || el.A <= 0 {
		// Hyperbolic or degenerate: still on an ascent-class
		// trajectory (or thrown off it). Show the HUD with `—`
		// placeholders rather than silently hiding.
		return true
	}
	primaryR := c.Primary.RadiusMeters()
	periAlt := el.Periapsis() - primaryR
	return periAlt < launchMissionFloorM
}

// launchMissionFloorM is the periapsis altitude (m) at which the
// saturn-v-pad-to-leo mission passes — also the threshold at which
// the LAUNCH HUD vanishes (ascent complete) and the ORBIT READY
// callout's pe gate fires. Lives at the package boundary so the
// LAUNCH HUD block (orbit.go:1158-) and shouldShowLaunchHUD agree
// on a single floor. Mirrors the JSON value at
// internal/missions/missions.json:40. v0.9.4+.
const launchMissionFloorM = 200_000.0

// formatAltKm renders an altitude in metres as a signed kilometre
// string with a sign that's friendly to ascent flight ("−2.8 km"
// reads better than "−2840 m" for sub-orbital periapsis). Used by
// the LAUNCH HUD's ap / pe rows. v0.9.4+.
func formatAltKm(altM float64) string {
	km := altM / 1000
	switch {
	case math.Abs(km) >= 1000:
		return fmt.Sprintf("%+.0f km", km)
	case math.Abs(km) >= 1:
		return fmt.Sprintf("%+.1f km", km)
	default:
		return fmt.Sprintf("%+.0f m", altM)
	}
}

// formatDurationShort renders seconds as a short human label —
// "12s" / "3m45s" / "1h22m". Used by the LAUNCH HUD's t_to_apo
// row and the rendezvous TCA readout (v0.9.3 patterns kept
// consistent across both ascent and rendezvous flows). v0.9.4+.
func formatDurationShort(sec float64) string {
	if sec < 60 {
		return fmt.Sprintf("%.0fs", sec)
	}
	if sec < 3600 {
		m := int(sec) / 60
		s := int(sec) % 60
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	h := int(sec) / 3600
	m := (int(sec) % 3600) / 60
	return fmt.Sprintf("%dh%02dm", h, m)
}

// launchMissionProgress returns the pe-altitude-vs-mission-floor
// row for the LAUNCH HUD when the active craft is flying a
// circularize_from_pad mission for its current primary. Empty
// string when no such mission is in flight. v0.9.4+.
func launchMissionProgress(w *sim.World, c *spacecraft.Spacecraft, periAltM float64) string {
	for _, m := range w.Missions {
		if m.Type != missions.TypeCircularizeFromPad {
			continue
		}
		if m.Status == missions.Passed {
			continue
		}
		if m.Params.PrimaryID != c.Primary.ID {
			continue
		}
		target := m.Params.MinPeriapsisAltM
		if target <= 0 {
			target = launchMissionFloorM
		}
		return fmt.Sprintf("mission:    pe %s / %s target",
			formatAltKm(periAltM), formatAltKm(target))
	}
	return ""
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
		// v0.9.2+: a Landed craft co-rotates with the body, so its
		// (r, v) gives an orbit plane that also co-rotates. Using
		// that as the canvas's orbit-flat basis would lock the
		// camera to the body and hide its rotation entirely (the
		// body's surface texture appears frozen). Fall back to the
		// top-view basis for parked crafts so the player can see
		// Earth turning under them. Once they engage the engine
		// (Landed → false) the live orbit picks up.
		if w.ActiveCraft().Landed {
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
