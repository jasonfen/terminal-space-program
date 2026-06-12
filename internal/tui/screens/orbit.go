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
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/settings"
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

	// lastSystemIdx + lastFocus + lastViewMode track the framing context
	// the canvas was last fit to, so we re-FitTo only at a Framing Event
	// (ADR 0021 A: Focus change, ViewMode change, System switch — plus a
	// resize, which invalidates the pixel geometry). Ambient sim-state
	// changes (a pass appearing or vanishing, an approach closing) never
	// move the camera. Per-frame center *tracking* of the focused object
	// continues below — that is what Focus means — but the fit is written
	// exactly once per event, then owned by the player.
	lastSystemIdx int
	lastFocus     sim.Focus
	lastViewMode  sim.ViewMode
	fitted        bool

	// baseScale is the auto-fit pixels-per-meter the Framing-Event fit
	// last computed (FocusZoomRadius). userZoom is the player's manual
	// `+`/`-` multiplier on top of it: the on-screen scale is
	// baseScale × userZoom, re-applied every frame (v0.18.2 mechanism).
	// baseScale only changes at a Framing Event, so on a steady focus
	// manual zoom persists indefinitely; userZoom resets to 1.0 at the
	// next Framing Event (fresh framing context, fresh zoom).
	baseScale float64
	userZoom  float64

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

	// burnFrozen{Legs,Arcs,Rings} snapshot the predicted-trajectory LINE
	// geometry — the dashed post-burn orbit (drawNodes legs), the purple
	// SOI-pass arc, and its dotted SOI ring(s) (drawSOIPass) — captured on
	// the last coasting frame and replayed steady while a finite burn fires
	// so the "where am I heading" purple preview doesn't vanish at ignition.
	// It otherwise would: executeDueNodesFor pops the firing node out of
	// c.Nodes (so PredictedLegs / PlannedSOIPass go empty), and a live
	// recompute off the mutating orbit whirls the geometry around the orbit
	// (the original "crosshair circles the orbit and beyond" bug). Holding
	// the pre-burn snapshot keeps the target on-screen — the player watches
	// the live solid ellipse grow to meet it — without reviving the whirl.
	// Markers (Δ node crosshairs, Perilune / SOI Entry-Exit) are NOT frozen:
	// the crosshair whirl was the bug's subject and marker click/label state
	// goes stale once the node is popped. Reset each coasting frame in
	// drawNodes (the first overlay pass), repopulated by both overlay
	// functions. Mirrors burnFrozenCenter's camera pin.
	burnFrozenLegs  []predictLegDraw
	burnFrozenArcs  []frozenArcLine
	burnFrozenRings []bodies.CelestialBody

	// titleBar tracks the column ranges of the right-aligned [Menu]
	// and [Missions] click targets in the title bar (row 0). Set on
	// each Render so HitAt-style hit-tests stay accurate after
	// terminal resizes. v0.7.4+.
	menuColStart, menuColEnd         int
	missionsColStart, missionsColEnd int
	// burnColStart/End track the [»Burn] Auto-Warp button (v0.16 / ADR
	// 0016), set alongside the others each Render.
	burnColStart, burnColEnd int

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

	// chipRects records the absolute screen-cell rectangle of each Chip
	// composited onto the canvas this frame, so the orbit screen's mouse
	// dispatch can route a click on a Chip (the Nodes chip opens the
	// maneuver screen, ADR 0010). Recomputed every composeChips call.
	chipRects []chipRect

	// settings holds the player's per-Chip default-visibility
	// preferences (ADR 0010, v0.13). Defaults to all-on so the screen
	// behaves exactly as pre-0010 until SetSettings pushes a loaded
	// settings.json. Read by the chip render rule (enabled && relevant
	// && !declutter). The launch screen shares this OrbitView as its
	// hudSource, so it inherits the same preferences.
	settings settings.Settings

	// declutter is the transient F2 "hide all overlays" state (slice 4
	// wires the keybinding). While true the chip render rule suppresses
	// every Chip; the slim HUD column is never affected. Not persisted.
	declutter bool

	// predictCache memoizes the inertial node-marker + dashed-leg
	// geometry drawNodes plots, so the SOI-fidelity predictors (ADR 0017,
	// v0.17.2) don't re-run every render frame — the flip to
	// defaultPredictTuning raised the site-A cost to ~10 ms/call and
	// drawNodes ran it per leg per frame. For a coasting craft (drawNodes
	// returns early during a burn) the geometry is a pure function of
	// (active craft, planted nodes, clock); predictRenderKeyAt buckets the
	// clock to a small fraction of the live orbital period, so a paused or
	// low-warp coast reuses the prediction while a high-warp leap busts the
	// bucket each tick. Re-projection / zoom-skip happen at plot time, so
	// camera moves need no recompute. predictCacheComputes counts misses (a
	// test hook). ADR 0017 decision C.
	predictCache         predictRenderCache
	predictCacheComputes int

	// soiPassCache memoizes the live SOI Pass (ADR 0019) the same way
	// predictCache memoizes the node legs: the forward prediction is a
	// pure function of the live craft + clock bucket (node-independent —
	// the Pass is the *unburned* path), so a coast computes it once per
	// quantized-element/clock-bucket change instead of every frame. The
	// apoapsis-reach guard inside LiveSOIPass keeps a stable LEO's miss
	// path cheap; this keeps a real pass off the per-frame hot path.
	soiPassCache         soiPassRenderCache
	soiPassCacheComputes int
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

// hudNodeMarker is the visible click-affordance prefix on the Nodes
// chip's next-node line (ADR 0010). The marker signals the chip is
// clickable; the click routes to the maneuver screen via HitChip.
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
		userZoom:      1,
		settings:      settings.Default(),
	}
}

// SetSettings replaces the per-Chip visibility preferences this screen
// renders with. tui.New pushes the loaded settings.json at startup and
// the Settings screen (v0.13 slice 3) pushes live edits. The launch
// screen shares this OrbitView as its hudSource, so a single push
// updates both screens' chip visibility.
func (v *OrbitView) SetSettings(s settings.Settings) {
	v.settings = s
}

// Settings returns the current per-Chip visibility preferences, so the
// Settings screen can read the live state before toggling.
func (v *OrbitView) Settings() settings.Settings {
	return v.settings
}

// SetDeclutter sets the transient F2 hide-all-overlays state (slice 4).
// While true every Chip and the navball are suppressed; the slim HUD
// column is unaffected.
func (v *OrbitView) SetDeclutter(on bool) {
	v.declutter = on
}

// Declutter reports the current transient hide-all-overlays state.
func (v *OrbitView) Declutter() bool {
	return v.declutter
}

// Resize forwards terminal dimensions to the canvas. v0.13 playtest move:
// VESSEL/PROPELLANT became a pinned canvas chip, so there is no HUD column
// — the orbit map spans the full terminal width, less the 2 cols its
// rounded border occupies. 3 rows are reserved: the title row plus the
// canvas's top and bottom border. The keybind cheat-sheet footer was
// dropped (the `?` overlay is the source of truth), giving its row to the
// map; transient status / confirm lines now ride the bottom border.
func (v *OrbitView) Resize(totalCols, totalRows int) {
	canvasCols := totalCols - 2
	if canvasCols < 20 {
		canvasCols = 20
	}
	v.canvas.Resize(canvasCols, totalRows-3)
	v.fitted = false // force refit after resize
}

// ZoomIn / ZoomOut are thin wrappers for App to call on +/-. They nudge the
// manual-zoom multiplier (applied over the Framing-Event base scale in
// Render): scale = baseScale × userZoom, so the player's zoom persists
// indefinitely on a steady focus and resets only at the next Framing Event
// (ADR 0021 A). Clamped to a wide but finite range so a held key can't drive
// the scale to a degenerate value.
func (v *OrbitView) ZoomIn()  { v.setUserZoom(v.userZoom * 1.25) }
func (v *OrbitView) ZoomOut() { v.setUserZoom(v.userZoom / 1.25) }

func (v *OrbitView) setUserZoom(z float64) {
	const minZoom, maxZoom = 1e-4, 1e4
	if z < minZoom {
		z = minZoom
	}
	if z > maxZoom {
		z = maxZoom
	}
	v.userZoom = z
}

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

	// Framing Event resolution (ADR 0021 A): fit the canvas exactly once
	// per Framing Event — a Focus change, ViewMode change, or System
	// switch (plus resize, which clears v.fitted). FocusZoomRadius is the
	// single fit-value resolution; it may read sim state (the
	// encounter-aware ~1.3× parent-relative SOI fit, ADR 0021 F) but it
	// runs only here, never per frame — a pass appearing or vanishing
	// mid-coast leaves center + scale untouched. When focused on a moving
	// target (body or craft) we still re-center every frame below; this
	// path only fires when the framing *context* changes, not when the
	// target moves.
	if v.lastSystemIdx != w.SystemIdx || v.lastFocus != w.Focus ||
		v.lastViewMode != w.ViewMode || !v.fitted {
		v.lastSystemIdx = w.SystemIdx
		v.lastFocus = w.Focus
		v.lastViewMode = w.ViewMode
		v.fitted = true
		v.canvas.FitTo(w.FocusZoomRadius())
		v.baseScale = v.canvas.Scale()
		v.userZoom = 1 // fresh framing context — drop any manual zoom
		// A Framing Event mid-burn re-frames once, then re-freezes: drop
		// the frozen-center snapshot so the burn guard below re-captures
		// it at the new focus center (ADR 0021 watch-point).
		v.burnFrozenCenter = nil
	}

	v.canvas.Clear()
	v.canvas.SetBasis(viewBasis(w))
	center := w.FocusPosition()
	// Burn-frozen camera: hold the center steady while a burn is live so
	// the navball-anchored view doesn't drift. Guarded on a non-nil
	// active craft — after end-flight clears the last vessel the orbit
	// view still renders (a vessel-less system/home-body view), and
	// FocusPosition() already falls through to a body/system center.
	if c := w.ActiveCraft(); c != nil && c.ActiveBurn != nil {
		if v.burnFrozenCenter == nil {
			snapshot := center
			v.burnFrozenCenter = &snapshot
		}
		center = *v.burnFrozenCenter
	} else if v.burnFrozenCenter != nil {
		v.burnFrozenCenter = nil
	}
	// Compose the player's manual `+`/`-` zoom over the Framing-Event base
	// scale, every frame: scale = baseScale × userZoom (v0.18.2 mechanism,
	// ADR 0021 A). baseScale is frozen between Framing Events, so on a
	// steady focus this is the identity until the player zooms — and the
	// player's zoom persists until the next event resets userZoom. The
	// landed/primary zoom cap below still clamps the result.
	v.canvas.SetScale(v.baseScale * v.userZoom)
	v.canvas.Center(center)

	// Dotted orbit ellipses for each body with a nonzero semimajor axis.
	// v0.10.6+: far-side arc renders at stride*2 (visually dashed) in
	// the dedicated dim-grey ColorBodyOrbit — KSP-aligned quiet
	// backdrop. bodyPxR=0 skips disk occlusion; the system primary at
	// origin doesn't meaningfully occlude body orbits at heliocentric
	// zoom. Pre-v0.10.6 this was a flat DrawEllipseDotted with no
	// depth read.
	for i := range sys.Bodies {
		b := sys.Bodies[i]
		if b.SemimajorAxis == 0 {
			continue
		}
		el := orbital.ElementsFromBody(b)
		v.canvas.DrawEllipseOffsetFarSideDashed(el, orbital.Vec3{}, 360, 6, orbital.Vec3{}, 0, render.ColorBodyOrbit)
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
	// v0.9.6: the system's star radius, hoisted once for the
	// per-body eclipse-cone test below. sol.json puts the star at
	// Bodies[0]; fall back to a BodyType scan for non-Sol systems.
	var sunR float64
	for i := range sys.Bodies {
		if sys.Bodies[i].BodyType == "Star" {
			sunR = sys.Bodies[i].RadiusMeters()
			break
		}
	}
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
		// v0.9.6: day/night terminator + eclipse dimming. The Sun is
		// the light source (exempt). The sub-solar point reuses the
		// exact projection path as the camera one — same primMer,
		// same epoch offset — so the (lon − ssLon) difference in
		// SolarLight.FactorAt stays frame-consistent. Sun sits at the
		// inertial origin, so body→Sun is simply −pos;
		// SubObserverPointDeg re-normalizes internally.
		var light *render.SolarLight
		if b.BodyType != "Star" {
			sunDir := render.Vec3{X: -pos.X, Y: -pos.Y, Z: -pos.Z}
			if sunDir.X != 0 || sunDir.Y != 0 || sunDir.Z != 0 {
				ssLat, ssLon := render.SubObserverPointDeg(b, w.Clock.RotationTime, sunDir, primMer)
				light = &render.SolarLight{SubSolarLatDeg: ssLat, SubSolarLonDeg: ssLon, EclipseFactor: 1.0}
				// Phase B: a body inside its parent's umbra/penumbra
				// (lunar-eclipse geometry) dims globally on top of
				// the per-pixel terminator.
				if parent := sys.ParentOf(b); parent != nil && parent.ID != b.ID {
					pPos := w.BodyPosition(*parent)
					light.EclipseFactor = render.EclipseFactor(
						render.Vec3{X: pos.X, Y: pos.Y, Z: pos.Z},
						render.Vec3{X: pPos.X, Y: pPos.Y, Z: pPos.Z},
						b.RadiusMeters(), parent.RadiusMeters(), sunR)
				}
			}
		}
		// v0.11.2+ (ADR 0003): screen-up is the canvas-frame direction
		// onto which body-local-north at the sub-observer projects.
		// For free bodies it is the component of BodyRotationAxisWorld
		// perpendicular to camDir, normalised, then expressed in the
		// canvas (X, Y) basis. Tidally-locked bodies use the simplified
		// (0, 1) — their texture orientation is already a fudge for
		// near-side facing, and a small additional rotation from their
		// (typically small) axial tilt isn't worth the extra geometry.
		// Pole-on cases (n nearly parallel to camDir) collapse to
		// (0, 1); the longitude is undefined at the pole anyway.
		upX, upY := 0.0, 1.0
		if !b.TidallyLocked {
			n := render.BodyRotationAxisWorld(b)
			nDotCam := n.X*camDir.X + n.Y*camDir.Y + n.Z*camDir.Z
			nPerp := render.Vec3{
				X: n.X - nDotCam*camDir.X,
				Y: n.Y - nDotCam*camDir.Y,
				Z: n.Z - nDotCam*camDir.Z,
			}
			mag := math.Sqrt(nPerp.X*nPerp.X + nPerp.Y*nPerp.Y + nPerp.Z*nPerp.Z)
			if mag > 1e-6 {
				inv := 1.0 / mag
				bx := v.canvas.Basis().X
				by := v.canvas.Basis().Y
				upX = (nPerp.X*bx.X + nPerp.Y*bx.Y + nPerp.Z*bx.Z) * inv
				upY = (nPerp.X*by.X + nPerp.Y*by.Y + nPerp.Z*by.Z) * inv
				// Re-normalise inside the canvas plane (basis vectors
				// are orthonormal so |(upX, upY)| should already be 1
				// to FP precision, but guard against rounding).
				m2 := math.Sqrt(upX*upX + upY*upY)
				if m2 > 1e-9 {
					upX /= m2
					upY /= m2
				} else {
					upX, upY = 0, 1
				}
			}
		}
		if tex := render.TextureFor(b, r, subLat, subLon, upX, upY, light); tex != nil {
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
	// are shown by the maneuver-preview SOI-segmented trace when nodes
	// are planted, and by the in-SOI residence pass arc (#157) on a
	// node-free escape — drawSOIPass covers the no-node flyby that used
	// to draw nothing here.
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
			v.canvas.DrawEllipseOffsetFarSideDashed(el, primaryPos, 360, 3, primaryPos, primaryPxR, render.ColorCurrentOrbit)
			peri := primaryPos.Add(orbital.PositionAtTrueAnomaly(el, 0))
			apo := primaryPos.Add(orbital.PositionAtTrueAnomaly(el, math.Pi))
			// Unified single-glyph markers (ADR 0020): ▼ periapsis / ▲
			// apoapsis in their type colours, replacing the chunky FillDisk
			// blobs. Occlusion behind the primary still hides them.
			if !v.canvas.IsBehindBody(peri, primaryPos, primaryPxR) {
				drawMarker(v.canvas, peri, render.MarkerPeriapsis, render.MarkerNominal, "", widgets.CellTag{})
			}
			if !v.canvas.IsBehindBody(apo, primaryPos, primaryPxR) {
				drawMarker(v.canvas, apo, render.MarkerApoapsis, render.MarkerNominal, "", widgets.CellTag{})
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
		// The active craft glyph is stamped AFTER the non-active craft
		// loop below (search "active craft wins its cell") so a just-
		// jettisoned stage sharing the cell can't overdraw it.

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
				v.canvas.PlotColored(origin, render.ColorRCSPuffOrigin)
			}
			if !v.canvas.IsBehindBody(tip, primaryPos, primaryPxR) {
				v.canvas.PlotColored(tip, render.ColorRCSPuffTip)
			}
		}

		// v0.8.2+: render non-active craft with their per-loadout
		// glyph + color so each vessel reads distinctly even at
		// small pixel sizes. The current-orbit ellipse renders in
		// the craft's own color (dim when no Color is set, falling
		// back to ColorDim — preserves pre-v0.8.2 behaviour).
		targetCraftIdx := -1
		if _, idx, ok := w.ResolveTargetCraft(); ok {
			targetCraftIdx = idx
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
				v.canvas.DrawEllipseOffsetFarSideDashed(otherEl, otherPrimaryPos, 180, stride, otherPrimaryPos, otherPxR, otherColor)
			}
			if !v.canvas.IsBehindBody(otherInertial, otherPrimaryPos, otherPxR) {
				v.canvas.PlotColored(otherInertial, otherColor)
				if other.Glyph != "" {
					if g := []rune(other.Glyph); len(g) > 0 {
						// Pin the glyph color (see active-craft note below)
						// so non-active vessels also keep their own color
						// over a body disk.
						v.canvas.SetCellOverlayColored(otherInertial, g[0], otherColor)
					}
				}
			}
		}

		// Active craft wins its cell. Stamped here, after every non-active
		// craft, so a craft sharing the active craft's cell can't overdraw
		// it — the case that motivated this is staging in orbit: the
		// jettisoned stage spawns ~60 m away (sub-pixel at orbital zoom), so
		// its glyph landed in the same cell and the active vessel vanished
		// under the dropped stage. The colored dot underneath also carries
		// the click-on-vessel hit-test tag, so the active craft owns it too.
		if !v.canvas.IsBehindBody(craftInertial, primaryPos, primaryPxR) {
			activeColor := render.ColorCraftMarker
			if c.Color != "" {
				activeColor = lipgloss.Color(c.Color)
			}
			v.canvas.FillColoredDiskTagged(craftInertial, 1, widgets.CellTag{Color: activeColor, IsVessel: true})
			if g := []rune(c.Glyph); len(g) > 0 {
				// Pin the glyph color so it stays the vessel's own color
				// instead of flipping to the body's color when the marker
				// overlaps a body disk (e.g. low orbit over Earth).
				v.canvas.SetCellOverlayColored(craftInertial, g[0], activeColor)
			}
		}
	}

	// Planned maneuver nodes — cluster glyph at each node's inertial
	// position, plus a dashed predicted trajectory from the first node's
	// post-burn state. Only meaningful when the craft is visible here.
	if w.CraftVisibleHere() {
		v.drawNodes(w)
		// Live SOI Pass (ADR 0019): the upcoming foreign-SOI encounter of
		// the *unburned* trajectory, drawn ahead of arrival with a Perilune
		// marker — always-on, independent of the Target slot.
		v.drawSOIPass(w)
	}

	// Stamp the active projection in the canvas's bottom-left corner
	// so the indicator stays attached to the view it describes (was a
	// HUD line under FOCUS until v0.7.4 — see orbit.go's renderHUD).
	// v0.9.6-polish moved it left so it no longer sits under the
	// bottom-right navball panel.
	viewLabel := "view: " + w.ViewMode.String()
	if w.ViewMode == sim.ViewTilted {
		// v0.10.6+: surface θ in degrees when the player has nudged it
		// off the default via shift+↑/↓. Plain "view: tilted" stays
		// the at-default form; "view: tilted 30°" cues that the value
		// is non-default (useful when triaging player reports of
		// "the angle is wrong").
		//
		// v0.10.7+: append "/anchor" when the launch-anchor is active
		// (apoAlt ≤ 200 km), and always show θ in that form
		// ("view: tilted 25°/anchor") so the player has a visible
		// readout of both axes during launch.
		el, ok := activeCraftElements(w)
		_, anchored := sim.LaunchAnchorPhi(w.ActiveCraft(), el, ok)
		switch {
		case anchored:
			viewLabel = fmt.Sprintf("view: tilted %g°/anchor", w.ViewTilt.Theta)
		case w.ViewTilt.Theta != sim.DefaultViewTilt().Theta:
			viewLabel = fmt.Sprintf("view: tilted %g°", w.ViewTilt.Theta)
		}
	}
	v.canvas.SetCellLabelColored(0, v.canvas.Rows()-1, viewLabel, v.theme.Primary.GetForeground())
	// v0.13: the "focus:" indicator moved to the title bar (renderTitleBar)
	// — the canvas top-left corner is now home to the pinned VESSEL chip,
	// and "focus: <craft>" was redundant with the chip's vessel name.

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
	// Declutter (F2) hides the navball alongside every Chip (CONTEXT.md
	// §Declutter); the slim HUD column is the only thing it never hides.
	if !v.declutter {
		canvasStr = v.composeNavballOverlay(w, canvasStr, cCols, cRows, true)
	}
	// v0.13 (ADR 0010): every contextual block is now a Chip composited
	// onto a canvas corner instead of a row in a tall HUD column. Canvas
	// content sits 1 col in and 2 rows down (rounded border + title row),
	// so the recorded chip rects carry those screen offsets for mouse
	// routing (HitChip). Chips paint after the navball so the bottom-right
	// Nodes chip can stack above it (navballReservedRows).
	navballReserved := v.navballReservedRows(w, cCols, cRows)
	canvasStr = v.composeChips(canvasStr, cCols, cRows, navballReserved, 1, 2, v.assembleChips(w))

	canvasPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.theme.Primary.GetForeground()).
		Render(canvasStr)

	craftChip := ""
	if n := len(w.Crafts); n > 1 {
		craftChip = fmt.Sprintf(" — CRAFT %d/%d", w.ActiveCraftIdx+1, n)
	}
	title := v.renderTitleBar(sys.Name+craftChip, w, totalCols)

	// v0.13 playtest move: VESSEL/PROPELLANT became a pinned canvas chip,
	// so there is no right-hand column any more — the orbit map spans the
	// full terminal width. The keybind cheat-sheet footer was dropped too
	// (the `?` overlay is the source of truth), so the map claims that row;
	// transient status / confirm lines ride the canvas bottom border.
	out := title + "\n" + canvasPanel
	return out
}

// renderTitleBar composes the orbit-screen title row: the existing
// "terminal-space-program — vX — System" left-aligned, plus
// right-aligned `[Menu]` and `[Missions]` clickable buttons. Stores
// the cell ranges of each button so HitMenuButton / HitMissionsButton
// can map subsequent clicks back to a screen action. v0.7.4+.
func (v *OrbitView) renderTitleBar(systemName string, w *sim.World, totalCols int) string {
	left := fmt.Sprintf("terminal-space-program — %s — %s", version.Version, systemName)
	// v0.13: the "focus:" readout moved here from the canvas top-left
	// corner (now home to the pinned VESSEL chip). It still says what the
	// camera follows — a body or the whole system, not just your craft.
	if fn := w.FocusName(); fn != "" {
		left += " — focus: " + fn
	}
	const menuLabel = "[Menu]"
	const missionsLabel = "[Missions]"
	const gap = "  "
	const clockGap = "    " // 4-space gap between clock chip and the buttons

	// v0.10.3+: clock + warp + pause chip, placed between the left
	// title and the right-aligned buttons (was a HUD CLOCK block).
	// Warp shows the effective rate when the integrator clamps it
	// (e.g. ≤10x during burns) so the player sees the actual
	// propagation speed, not the requested one.
	clockChip := "T+" + w.Clock.SimTime.Format("2006-01-02") + "  "
	// v0.16 / ADR 0016: while Auto-Warp is engaged the warp readout morphs
	// to AUTO → <effective>x  <time-to-target> so the player sees the
	// driver and the live rate, not the untouched Selected Warp. The chip
	// stays emoji-free (single-width runes only) so the rune-counted
	// button hit-tests below stay aligned.
	if secs, ok := w.AutoWarpSecondsToTarget(); ok {
		dur := time.Duration(secs * float64(time.Second))
		clockChip += fmt.Sprintf("AUTO →%.0fx  %s", w.EffectiveWarp(), compactDuration(dur))
	} else {
		reqWarp := w.Clock.Warp()
		if eff := w.EffectiveWarp(); eff < reqWarp {
			clockChip += fmt.Sprintf("warp %.0fx→%.0fx", reqWarp, eff)
		} else {
			clockChip += fmt.Sprintf("warp %.0fx", reqWarp)
		}
	}
	clockChipRendered := v.theme.Dim.Render(clockChip)
	if w.AutoWarpEngaged() {
		clockChipRendered = v.theme.Primary.Render(clockChip)
	}
	pauseChipPlain := ""
	pauseChipRendered := ""
	if w.Clock.Paused {
		pauseChipPlain = "  PAUSED"
		pauseChipRendered = "  " + v.theme.Warning.Render("PAUSED")
	}

	// v0.16 / ADR 0016: the [»Burn] Auto-Warp button. [■Burn] highlighted
	// while engaged, dimmed when no burn is eligible. The » / ■ runes are
	// East-Asian *ambiguous* width, so the column math below measures
	// terminal cells with lipgloss.Width (the package convention) rather
	// than rune counts — the two diverge under EastAsianWidth mode and a
	// rune count would drift every button's hit-test off its glyph.
	burnLabel := "[»Burn]"
	if w.AutoWarpEngaged() {
		burnLabel = "[■Burn]"
	}

	rightPlain := clockChip + pauseChipPlain + clockGap + burnLabel + gap + menuLabel + gap + missionsLabel

	// Compute the absolute column where the right group starts so the
	// hit-test ranges match what the player sees on screen.
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(rightPlain)
	pad := totalCols - leftWidth - rightWidth
	if pad < 1 {
		pad = 1
	}
	rightStart := leftWidth + pad
	buttonsStart := rightStart + lipgloss.Width(clockChip+pauseChipPlain+clockGap)

	v.burnColStart = buttonsStart
	v.burnColEnd = v.burnColStart + lipgloss.Width(burnLabel)
	v.menuColStart = v.burnColEnd + lipgloss.Width(gap)
	v.menuColEnd = v.menuColStart + lipgloss.Width(menuLabel)
	v.missionsColStart = v.menuColEnd + lipgloss.Width(gap)
	v.missionsColEnd = v.missionsColStart + lipgloss.Width(missionsLabel)

	burnRendered := v.theme.Primary.Render(burnLabel)
	switch {
	case w.AutoWarpEngaged():
		burnRendered = v.theme.Warning.Render(burnLabel) // amber: a time mode is on
	case !w.AutoWarpEligible():
		burnRendered = v.theme.Dim.Render(burnLabel)
	}

	rendered := v.theme.Title.Render(left) +
		strings.Repeat(" ", pad) +
		clockChipRendered +
		pauseChipRendered +
		clockGap +
		burnRendered +
		gap +
		v.theme.Primary.Render(menuLabel) +
		gap +
		v.theme.Primary.Render(missionsLabel)
	return rendered
}

// compactDuration renders a positive duration as a two-unit chip
// ("2d4h", "3h12m", "5m30s", "28s") for the Auto-Warp HUD readout —
// the prefix-free sibling of formatCountdown. v0.16 / ADR 0016.
func compactDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSecs := int64(d.Seconds())
	days := totalSecs / 86400
	hours := (totalSecs % 86400) / 3600
	mins := (totalSecs % 3600) / 60
	secs := totalSecs % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm%ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
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

// HitBurnButton reports whether a click at (col, row) lands on the
// title bar's `[»Burn]` Auto-Warp button. v0.16 / ADR 0016.
func (v *OrbitView) HitBurnButton(col, row int) bool {
	return row == 0 && col >= v.burnColStart && col < v.burnColEnd
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
	case sim.ViewTilted, sim.ViewOrbitFlat:
		// Depth axis points out of screen toward the camera, which
		// is exactly the body-to-camera direction the sub-observer
		// math wants. For ViewOrbitFlat on a near-equatorial orbit
		// this is approximately +Z; for inclined orbits it tips
		// accordingly. ViewTilted (v0.10.6+) feeds through the same
		// path because viewBasis has already encoded the θ/φ tilt
		// into the canvas basis — the texture pipeline just reads
		// whatever depth axis came out.
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

// drawMarker stamps a single unified orbital marker (ADR 0020) at an
// inertial point: one glyph rune whose colour encodes the marker type and
// whose brightness encodes its state, drawn through SetCellOverlayColored
// the same way Vessels and Bodies collapse to a glyph. When tag carries
// hit-test metadata (a NodeIdx for maneuver nodes, a vessel/body flag), a
// single tagged pixel is plotted under the glyph so a click on the cell
// still resolves to the sim object — the glyph overlay then paints over
// it, so the pixel is invisible but selectable. base supplies the
// positional colour for MarkerManeuver and is ignored for every other
// type. Occlusion is the caller's call: apsis markers gate on
// IsBehindBody before calling; maneuver markers do not.
func drawMarker(canvas *widgets.Canvas, pos orbital.Vec3, t render.MarkerType, state render.MarkerState, base lipgloss.Color, tag widgets.CellTag) {
	color := render.MarkerColor(t, state, base)
	if tag.NodeIdx != 0 || tag.IsVessel || tag.BodyID != "" {
		tag.Color = color
		canvas.PlotColoredTagged(pos, tag)
	}
	canvas.SetCellOverlayColored(pos, render.MarkerGlyph(t), color)
}

// drawNodes plots every planned maneuver node at its projected inertial
// position and draws the post-burn predicted trajectory starting from
// the first node. The trajectory is segmented by SOI: samples inside
// the craft's home SOI use stride-2 (dashed); samples that cross into
// another body's SOI use stride-1 (solid) so the crossing is visually
// distinct at a glance.
// predictRenderCache holds the last computed dashed-orbit geometry and
// the key it was computed for (ADR 0017 decision C — predict-on-change).
type predictRenderCache struct {
	has  bool
	key  predictRenderKey
	data predictRenderData
}

// soiPassRenderCache memoizes the dual-arc SOI Pass result (ADR 0019 D)
// under the predict-on-change discipline.
type soiPassRenderCache struct {
	has bool
	key soiPassKey
	arc soiDualArc
}

// soiDualArc is the per-frame SOI Pass geometry the canvas + chip share.
// With no node planted, only `counterfactual` is populated (it equals the
// live pass) and is drawn bright. With a node planted, `counterfactual` is
// the no-burn path (capped at the first node, drawn dim) and `planned` is
// the node-modified path's pass (drawn as a bright Perilune marker over the
// node legs) — the dual arc.
type soiDualArc struct {
	counterfactual sim.SOIPass
	cfOK           bool
	planned        sim.SOIPass
	plOK           bool
	hasNodes       bool
}

// soiPassKey fingerprints what the dual-arc SOI Pass depends on: the active
// craft, its primary, the clock (bucketed), the live orbital elements
// (quantized), and the planted-node fingerprint. The node fingerprint is
// included because the *planned* (node-modified) arc depends on the nodes
// (ADR 0019 D) — the no-burn counterfactual is also capped at the first
// node, so a node edit legitimately busts this cache.
type soiPassKey struct {
	craftID     uint64
	primaryID   string
	nodeFinger  uint64
	clockBucket int64
	aQ          int64
	eQ          int64
	iQ          int64
	omegaQ      int64
	argQ        int64
}

// cachedSOIPass returns the dual-arc SOI Pass for the active craft,
// recomputing (the forward predictor + apoapsis-reach guard) only when the
// key changes — ADR 0019 / ADR 0017 C.
func (v *OrbitView) cachedSOIPass(w *sim.World) soiDualArc {
	if w.ActiveCraft() == nil {
		return soiDualArc{}
	}
	key := v.soiPassKeyAt(w, w.Clock.SimTime)
	if v.soiPassCache.has && v.soiPassCache.key == key {
		return v.soiPassCache.arc
	}
	var arc soiDualArc
	arc.hasNodes = len(w.ActiveCraft().Nodes) > 0
	arc.counterfactual, arc.cfOK = w.CounterfactualSOIPass()
	if arc.hasNodes {
		arc.planned, arc.plOK = w.PlannedSOIPass()
	}
	v.soiPassCache = soiPassRenderCache{has: true, key: key, arc: arc}
	v.soiPassCacheComputes++
	return arc
}

// soiPassKeyAt builds the SOI-pass cache key for the active craft at clock t.
func (v *OrbitView) soiPassKeyAt(w *sim.World, t time.Time) soiPassKey {
	c := w.ActiveCraft()
	key := soiPassKey{
		craftID:     c.ID,
		primaryID:   c.Primary.ID,
		nodeFinger:  nodeFingerprint(c.Nodes),
		clockBucket: t.UnixNano() / v.predictClockBucketNanos(w),
	}
	if el, ok := activeCraftElements(w); ok {
		key.aQ = quantize(el.A, 1000)
		key.eQ = quantize(el.E, 1e-4)
		key.iQ = quantize(el.I, 1e-4)
		key.omegaQ = quantize(el.Omega, 1e-4)
		key.argQ = quantize(el.Arg, 1e-4)
	}
	return key
}

// predictRenderKey is the cheap fingerprint of everything the predicted
// node markers + dashed legs depend on for a coasting craft: which craft,
// its primary, the planted nodes, the clock (bucketed), and the live
// orbital elements (quantized). The elements are conserved under
// ballistic coast but change the instant any thrust — a manual main burn,
// an RCS pulse, a firing planted node — alters the orbit; folding them in
// busts the cache on thrust (whose R/V deltas are NOT clock-derived and so
// aren't captured by clockBucket alone) without enumerating every thrust
// source. All fields comparable, so a cache check is a single struct ==.
type predictRenderKey struct {
	craftID     uint64
	primaryID   string
	nodeFinger  uint64
	clockBucket int64
	aQ          int64 // semimajor axis, 1 km quanta
	eQ          int64 // eccentricity, 1e-4 quanta
	iQ          int64 // inclination, ~0.006° quanta
	omegaQ      int64 // RAAN Ω
	argQ        int64 // argument of periapsis ω
}

// predictRenderData is the projection-independent geometry to plot:
// inertial node markers and per-leg colored SOI segments. Re-projected
// and zoom-skipped at plot time every frame, so camera / zoom changes
// need no recompute.
type predictRenderData struct {
	markers []predictMarker
	legs    []predictLegDraw
}

type predictMarker struct {
	pos orbital.Vec3
	tag widgets.CellTag
}

type predictLegDraw struct {
	segs  []sim.SOISegment
	color lipgloss.Color
	// apoapsisM > 0 ⇒ apply the minOrbitPixels zoom-skip at plot time;
	// ≤ 0 (hyperbolic / a≤0 / non-finite) ⇒ always draw.
	apoapsisM float64
}

const (
	// predictBucketPeriodFrac sizes the cache's clock bucket at 0.3 % of
	// the live orbital period — ~1° of arc, below visible granularity —
	// so a low-warp coast reuses the dashed line for a bounded window.
	// predictBucketMin/Max clamp it: Max bounds worst-case staleness to
	// ~1 min regardless of orbit (long-period / hyperbolic / escape legs,
	// where 0.3 % of the period would be many minutes).
	predictBucketPeriodFrac = 0.003
	predictBucketMin        = int64(time.Second)
	predictBucketMax        = int64(60 * time.Second)
)

// predictClockBucketNanos is the cache's staleness-tolerance window in
// nanoseconds: a small fraction of the live orbital period, clamped. An
// unbound (a ≤ 0 / non-finite) orbit evolves slowly relative to a frame,
// so it gets the generous max.
func (v *OrbitView) predictClockBucketNanos(w *sim.World) int64 {
	c := w.ActiveCraft()
	if c == nil {
		return predictBucketMax
	}
	el, ok := activeCraftElements(w)
	if !ok || el.A <= 0 || math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return predictBucketMax
	}
	mu := c.Primary.GravitationalParameter()
	period := 2 * math.Pi * math.Sqrt(el.A*el.A*el.A/mu)
	b := int64(period * predictBucketPeriodFrac * float64(time.Second))
	if b < predictBucketMin {
		b = predictBucketMin
	}
	if b > predictBucketMax {
		b = predictBucketMax
	}
	return b
}

// predictRenderKeyAt builds the cache key for the active craft at clock t.
func (v *OrbitView) predictRenderKeyAt(w *sim.World, t time.Time) predictRenderKey {
	c := w.ActiveCraft()
	key := predictRenderKey{
		craftID:     c.ID,
		primaryID:   c.Primary.ID,
		nodeFinger:  nodeFingerprint(c.Nodes),
		clockBucket: t.UnixNano() / v.predictClockBucketNanos(w),
	}
	// Quantize the live orbital elements (coast-invariant; thrust-sensitive
	// — see predictRenderKey). Non-finite reads quantize to 0 (stable
	// across a frame), so a degenerate orbit just falls back to clock-only.
	if el, ok := activeCraftElements(w); ok {
		key.aQ = quantize(el.A, 1000)
		key.eQ = quantize(el.E, 1e-4)
		key.iQ = quantize(el.I, 1e-4)
		key.omegaQ = quantize(el.Omega, 1e-4)
		key.argQ = quantize(el.Arg, 1e-4)
	}
	return key
}

// quantize rounds x to integer multiples of step, returning 0 for a
// non-finite x (keeps the cache key deterministic and comparable).
func quantize(x, step float64) int64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	return int64(math.Round(x / step))
}

// nodeFingerprint folds every planted-node field the predicted trajectory
// depends on into one comparable hash (FNV-1a), so any edit — Δv, timing,
// mode, target, plane angle, fused-burn direction — busts the cache while
// a pure clock advance does not.
func nodeFingerprint(nodes []spacecraft.ManeuverNode) uint64 {
	h := uint64(14695981039346656037)
	h = mixU64(h, uint64(len(nodes)))
	for _, n := range nodes {
		h = mixU64(h, n.ID)
		h = mixU64(h, uint64(n.TriggerTime.UnixNano()))
		h = mixU64(h, uint64(n.Mode))
		h = mixU64(h, math.Float64bits(n.DV))
		h = mixU64(h, uint64(n.Duration))
		h = mixStr(h, n.PrimaryID)
		h = mixU64(h, uint64(n.Event))
		h = mixU64(h, math.Float64bits(n.Throttle))
		h = mixU64(h, n.TargetCraftID)
		h = mixU64(h, math.Float64bits(n.PlaneChangeRad))
		h = mixU64(h, math.Float64bits(n.BurnDirUnit.X))
		h = mixU64(h, math.Float64bits(n.BurnDirUnit.Y))
		h = mixU64(h, math.Float64bits(n.BurnDirUnit.Z))
	}
	return h
}

func mixU64(h, val uint64) uint64 { return (h ^ val) * 1099511628211 }

func mixStr(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h = mixU64(h, uint64(s[i]))
	}
	return h
}

// cachedPredictedRender returns the dashed-orbit geometry for the active
// craft, recomputing (the SOI-fidelity predictors) only when the cache key
// changes — ADR 0017 decision C. Callers (drawNodes) must already have
// ruled out the no-craft / no-nodes / active-burn cases.
func (v *OrbitView) cachedPredictedRender(w *sim.World) predictRenderData {
	key := v.predictRenderKeyAt(w, w.Clock.SimTime)
	if v.predictCache.has && v.predictCache.key == key {
		return v.predictCache.data
	}
	data := v.computePredictedRender(w)
	v.predictCache = predictRenderCache{has: true, key: key, data: data}
	v.predictCacheComputes++
	return data
}

// computePredictedRender runs the expensive node-marker + dashed-leg
// predictions — the cache-miss path. Mirrors the old inline drawNodes
// loops but returns plot-ready geometry instead of plotting.
func (v *OrbitView) computePredictedRender(w *sim.World) predictRenderData {
	c := w.ActiveCraft()
	var data predictRenderData

	for i, n := range c.Nodes {
		// v0.6.1: each node's marker matches the color of its resulting
		// orbit leg, so the glyph and the post-burn dashed orbit read as a
		// matched pair. ADR 0020: a single Δ glyph, drawn at plot time;
		// the per-leg colour rides on the tag.
		data.markers = append(data.markers, predictMarker{
			pos: w.NodeInertialPosition(n),
			tag: widgets.CellTag{
				Color:   render.ManeuverSegmentColor(i),
				NodeIdx: i + 1, // 0 = none; planted node i is 1+i in the tag.
			},
		})
	}

	// v0.6.1: render each post-maneuver leg in its own color so the player
	// can read which orbit belongs to which planted burn. PredictedLegs
	// walks all resolved nodes, rebasing each into the node's intended
	// frame (e.g. Hohmann arrival in Mars frame).
	for _, leg := range w.PredictedLegs() {
		legMu := leg.Primary.GravitationalParameter()
		legEl := orbital.ElementsFromState(leg.State.R, leg.State.V, legMu)
		// apoapsisM drives the plot-time minOrbitPixels skip; -1 marks a
		// hyperbolic / a≤0 / non-finite leg that always renders (its
		// trajectory covers meaningful distance regardless of orbit size).
		apo := -1.0
		if legEl.A > 0 && !math.IsNaN(legEl.A) && !math.IsInf(legEl.A, 0) {
			apo = legEl.Apoapsis()
		}
		// Sample budget is adaptive (v0.10.3): ~96 points per orbital
		// period the leg's horizon spans, so a long inter-node horizon at
		// high warp no longer smears the dashed orbit.
		data.legs = append(data.legs, predictLegDraw{
			segs:      w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, leg.Samples),
			color:     render.ManeuverSegmentColor(leg.NodeIndex),
			apoapsisM: apo,
		})
	}
	return data
}

// frozenArcLine is one captured SOI-pass arc line: the segments, the
// anchor body ID to rebase them against (Local-to-Body, ADR 0021 B —
// with the #157 root substitution already applied), and the colour they
// were drawn in (bright or dim, per ADR 0020 B). Replayed steady during a
// burn by drawSOIPass; see burnFrozenArcs.
type frozenArcLine struct {
	segs   []sim.SOISegment
	anchor string
	color  lipgloss.Color
}

func (v *OrbitView) drawNodes(w *sim.World) {
	c := w.ActiveCraft()
	if c == nil {
		v.clearBurnFrozenLines()
		return
	}
	// While a finite burn fires, replay the dashed post-burn orbit frozen
	// from the last coasting frame instead of suppressing it (the v0.10.1
	// behaviour). The firing node is popped from c.Nodes at ignition
	// (executeDueNodesFor) and PredictedLegs short-circuits to nil under an
	// ActiveBurn, so a live recompute is empty; the older inline recompute
	// instead whirled the markers around the orbit. Holding the snapshot
	// steady keeps the purple preview on-screen without reviving the whirl.
	// Only the LEGS replay — the Δ node crosshairs stay off (they were the
	// whirl's subject and carry click tags the burn has already popped).
	if c.ActiveBurn != nil {
		v.plotPredictedLegs(w, v.burnFrozenLegs)
		return
	}
	// Coasting: drawNodes runs first in the overlay pass (orbit.go ~865), so
	// reset the whole frozen-lines snapshot here; drawSOIPass repopulates the
	// arc + rings after us.
	v.clearBurnFrozenLines()
	if len(c.Nodes) == 0 {
		return
	}

	// The node markers + dashed legs are a pure function of (craft, nodes,
	// clock) for a coasting craft; cachedPredictedRender recomputes them
	// only when that changes (ADR 0017 decision C). Projection and the
	// zoom-skip run every frame from the cached inertial geometry.
	data := v.cachedPredictedRender(w)

	for _, m := range data.markers {
		// Unified marker (ADR 0020): a single Δ glyph in the node's
		// per-leg colour (the documented positional-colour exception),
		// over a tagged pixel that carries NodeIdx so a click still
		// resolves to the planted node. Maneuver markers are not
		// occlusion-culled (a node behind the body still shows), so we
		// don't gate on IsBehindBody here.
		drawMarker(v.canvas, m.pos, render.MarkerManeuver, render.MarkerNominal, m.tag.Color, m.tag)
	}
	v.plotPredictedLegs(w, data.legs)
	// Snapshot the dashed legs so an imminent ignition can keep them visible
	// (replayed above). data.legs is cache-owned and not recomputed during a
	// burn (drawNodes returns early before cachedPredictedRender), so holding
	// the slice header is safe.
	v.burnFrozenLegs = data.legs
}

// plotPredictedLegs plots the dashed post-burn legs onto the canvas: home-SOI
// samples at their inertial positions, foreign-SOI samples rebased
// Local-to-Body (ADR 0021 B). Shared by the live coasting path and the frozen
// burn-time replay.
func (v *OrbitView) plotPredictedLegs(w *sim.World, legs []predictLegDraw) {
	if w.ActiveCraft() == nil {
		return
	}
	homeID := w.ActiveCraft().Primary.ID
	scale := v.canvas.Scale()
	for _, leg := range legs {
		// Skip legs whose orbit projects too small to convey shape
		// (heliocentric view of a planet-frame leg) — same rule as the
		// live ellipse, keeping the canvas from painting a blob on top of
		// the parent body. apoapsisM ≤ 0 marks a hyperbolic / a≤0 leg that
		// always renders.
		if leg.apoapsisM > 0 && leg.apoapsisM*scale < minOrbitPixels {
			continue
		}
		for _, seg := range leg.segs {
			stride := 2
			if seg.PrimaryID != homeID {
				stride = 1 // foreign SOI — solid, eye-catching
			}
			for i, p := range w.SegmentDrawPoints(seg, homeID) {
				if stride > 1 && i%stride == 0 {
					continue
				}
				v.canvas.PlotColored(p, leg.color)
			}
		}
	}
}

// plotArcLine plots one captured SOI-pass arc line (purple), rebasing its
// segments Local-to-Body against the stored anchor. Shared by the live
// coasting path and the frozen burn-time replay.
func (v *OrbitView) plotArcLine(w *sim.World, line frozenArcLine) {
	for _, seg := range line.segs {
		for _, p := range w.SegmentDrawPoints(seg, line.anchor) {
			v.canvas.PlotColored(p, line.color)
		}
	}
}

// clearBurnFrozenLines drops the frozen trajectory-line snapshot. Called at
// the top of the coasting overlay pass (drawNodes) and whenever there's no
// active craft, so a stale preview can't replay into the next burn.
func (v *OrbitView) clearBurnFrozenLines() {
	v.burnFrozenLegs = nil
	v.burnFrozenArcs = v.burnFrozenArcs[:0]
	v.burnFrozenRings = v.burnFrozenRings[:0]
}

// soiCounterfactualDim scales the no-burn arc's colour when a node is
// planted, so the dim counterfactual reads as visibly secondary to the
// bright node-modified path drawn in the same SOI (ADR 0019 D / ADR 0020 B).
const soiCounterfactualDim = 0.5

// soiRingDim scales the foreign-SOI hue for the SOI Ring (ADR 0021 C) —
// dimmer than the counterfactual arc's 0.5, so the dotted boundary reads
// as a quiet backdrop under both the bright pass arc and the dim no-burn
// arc that cross it.
const soiRingDim = 0.4

// soiRingMinPixels skips the SOI Ring when its projected radius is too
// small to read as a boundary (heliocentric zoom) — a handful of dots on
// top of the body's own disk would smear, not inform.
const soiRingMinPixels = 4

// drawSOIRing draws the dim dotted SOI Ring around a pass Body (ADR 0021
// C): a screen-space circle at the body's parent-relative SOI radius
// (issue #143), anchored at the Body's CURRENT position — the same anchor
// SegmentDrawPoints rebases the Local-to-Body Arc onto, so the arc
// visibly enters and exits on the ring and the hyperbola gets scale.
// Quiet bodies (no active pass) never reach here: drawSOIPass calls this
// only for bodies a live / counterfactual / planned pass crosses.
func (v *OrbitView) drawSOIRing(w *sim.World, b bodies.CelestialBody) {
	soi := w.BodySOIRadius(b)
	if soi <= 0 {
		return
	}
	pxR := int(math.Round(soi * v.canvas.Scale()))
	if pxR < soiRingMinPixels {
		return
	}
	v.canvas.RingDottedColored(w.BodyPosition(b), pxR, render.Shade(render.ColorForeignSOI, soiRingDim))
}

// drawSOIPass renders the live trajectory's upcoming SOI Pass (ADR 0019):
// the foreign-SOI arc as a single bright solid leg, plus a Perilune ⊕
// marker at closest approach — or an Impact glyph (red+bright, the marker
// system's alarm state) when perilune dips below the surface. Always-on and
// independent of the Target slot; the apoapsis-reach guard inside the
// cached predictor keeps a stable orbit that reaches no SOI free.
func (v *OrbitView) drawSOIPass(w *sim.World) {
	// While a finite burn fires, replay the purple SOI-pass arc + its dotted
	// SOI ring(s) frozen from the last coasting frame (lines only — no
	// Perilune / Entry / Exit markers). Same rationale as drawNodes: the live
	// pass is recomputed from the mutating orbit (and the planted node is
	// gone), so it would vanish or jump; the frozen line keeps the encounter
	// preview steady through the burn. drawNodes (the prior overlay pass) has
	// already reset the snapshot on coasting frames.
	if c := w.ActiveCraft(); c != nil && c.ActiveBurn != nil {
		for _, b := range v.burnFrozenRings {
			v.drawSOIRing(w, b)
		}
		for _, line := range v.burnFrozenArcs {
			v.plotArcLine(w, line)
		}
		return
	}

	arc := v.cachedSOIPass(w)

	// SOI Ring (ADR 0021 C): every body with an active pass — and only
	// those — wears its dotted boundary, drawn first so the arcs and
	// marker glyphs paint over the ring's dots.
	if arc.cfOK {
		v.drawSOIRing(w, arc.counterfactual.Body)
		v.burnFrozenRings = append(v.burnFrozenRings, arc.counterfactual.Body)
	}
	if arc.plOK && (!arc.cfOK || arc.planned.Body.ID != arc.counterfactual.Body.ID) {
		v.drawSOIRing(w, arc.planned.Body)
		v.burnFrozenRings = append(v.burnFrozenRings, arc.planned.Body)
	}

	// The no-burn arc. With no node planted it IS the live pass, drawn
	// bright; with a node planted it's the counterfactual ("what you'll hit
	// if you don't burn"), drawn dim and capped at the node so it never
	// trails past the burn (ADR 0019 D). brightness = state (ADR 0020 B).
	if arc.cfOK {
		arcColor := render.ColorForeignSOI
		markerState := render.MarkerNominal
		if arc.hasNodes {
			arcColor = render.Shade(render.ColorForeignSOI, soiCounterfactualDim)
			markerState = render.MarkerCounterfactual
		}
		// The arc and its Perilune marker draw Local-to-Body (ADR 0021 B):
		// body-relative samples anchored at the pass Body's current
		// position — the same rebase the planted-node legs get, so the dim
		// counterfactual and the bright planned ink at one body agree.
		//
		// homeID trap (#157): for the in-SOI residence pass the pass Body
		// IS the craft's primary, so the arc segments carry PrimaryID ==
		// Primary.ID — and SegmentDrawPoints short-circuits home segments
		// to their inertial sample positions, which would reintroduce the
		// #147 smear for exactly this arc. Anchoring against the system
		// root instead rebases the in-SOI leg at the Body's current
		// position. A sibling pass never triggers the substitution — its
		// Body is a sibling of the primary, not the primary itself.
		homeID := w.ActiveCraft().Primary.ID
		if arc.counterfactual.Body.ID == homeID {
			homeID = w.System().Bodies[0].ID
		}
		// The arc line + its in-SOI residence continuation (#157): the whole
		// onward path the craft will fly, drawn in one colour/brightness.
		// Foreign (parent-frame) segments rebase Local-to-Body; root-frame
		// segments draw at their inertial samples. Captured as one frozen line
		// so a burn replays the same geometry steady.
		line := frozenArcLine{
			segs:   append(append([]sim.SOISegment{}, arc.counterfactual.ArcSegments...), arc.counterfactual.OnwardSegments...),
			anchor: homeID,
			color:  arcColor,
		}
		v.plotArcLine(w, line)
		v.burnFrozenArcs = append(v.burnFrozenArcs, line)
		if arc.counterfactual.HasPerilunePt {
			st := markerState
			if arc.counterfactual.Impact {
				st = render.MarkerAlarm // sub-surface perilune → Impact (red+bright even when counterfactual)
			}
			drawMarker(v.canvas, w.PerilunePosition(arc.counterfactual), render.MarkerPerilune, st, "", widgets.CellTag{})
		}
		// SOI Entry / Exit glyphs at the arc's ring crossings (ADR 0021 C),
		// in the same brightness=state vocabulary as the Perilune: dim when
		// this arc is the counterfactual under a planted node, bright when
		// it IS the live path. No Exit when the arc never leaves the SOI
		// (impact / horizon-truncated / node-capped).
		if arc.counterfactual.HasEntry {
			drawMarker(v.canvas, w.EntryPosition(arc.counterfactual), render.MarkerSOIEntry, markerState, "", widgets.CellTag{})
		}
		if arc.counterfactual.HasExit {
			drawMarker(v.canvas, w.ExitPosition(arc.counterfactual), render.MarkerSOIExit, markerState, "", widgets.CellTag{})
		}
	}

	// The planned (node-modified) path's markers, drawn bright over the
	// node legs (which drawNodes already paints): the Perilune — the safe
	// periapsis the burn produces, against the dim no-burn Impact (ADR
	// 0019 D) — plus its own SOI Entry / Exit ring crossings. Drawn after
	// the counterfactual so the bright planned glyph wins a shared cell.
	if arc.plOK {
		if arc.planned.HasPerilunePt {
			st := render.MarkerNominal
			if arc.planned.Impact {
				st = render.MarkerAlarm
			}
			drawMarker(v.canvas, w.PerilunePosition(arc.planned), render.MarkerPerilune, st, "", widgets.CellTag{})
		}
		if arc.planned.HasEntry {
			drawMarker(v.canvas, w.EntryPosition(arc.planned), render.MarkerSOIEntry, render.MarkerNominal, "", widgets.CellTag{})
		}
		if arc.planned.HasExit {
			drawMarker(v.canvas, w.ExitPosition(arc.planned), render.MarkerSOIExit, render.MarkerNominal, "", widgets.CellTag{})
		}
	}
}

// composeNavballOverlay paints the framed navball into the bottom-
// right corner of canvasStr and returns the modified string.
// recordControls=true also captures the panel's clickable hit
// boxes into v.navballControls for the orbit screen's mouse
// dispatch (the launch screen doesn't take navball clicks, so it
// passes false). When the active craft has no defined nose
// direction, or the canvas is too small to hold the panel, the
// canvas is returned unchanged.
//
// Extracted from orbit.Render in v0.11.4+ so the LaunchView can
// composite the same panel in its bottom-right (sub-scope 6).
func (v *OrbitView) composeNavballOverlay(w *sim.World, canvasStr string, cCols, cRows int, recordControls bool) string {
	if !w.CraftVisibleHere() ||
		cCols < navballPanelW+2 || cRows < navballPanelH+2 {
		return canvasStr
	}
	rawLat, rawLon, ok := w.NavballSubObserver()
	if !ok {
		return canvasStr
	}
	subLat, subLon := v.stickyNavballSubObserver(rawLat, rawLon)
	disk := navballPanelDisk(w, subLat, subLon)
	panel, boxes := v.buildNavballPanel(disk, w.NavMode, w.InstantSAS, w.RCSActive())
	atCol := cCols - navballPanelW
	atRow := cRows - navballPanelH - 1
	lines := strings.Split(canvasStr, "\n")
	lines = overlayStyledBlock(lines, panel, atRow, atCol, cCols)
	out := strings.Join(lines, "\n")
	if recordControls {
		for _, b := range boxes {
			v.navballControls = append(v.navballControls, navballControlBox{
				id:       b.id,
				colStart: atCol + b.colStart + 1,
				colEnd:   atCol + b.colEnd + 1,
				row:      atRow + b.row + 2,
			})
		}
	}
	return out
}

// ComposeNavballOverlay is the exported entry the LaunchView calls
// to drop the same bottom-right navball panel into its canvas
// (sub-scope 6 / v0.11.4+). recordControls is forced false — the
// launch screen doesn't take navball clicks; the orbit-screen
// path keeps its hit-box capture by going through the private
// helper directly.
func (v *OrbitView) ComposeNavballOverlay(w *sim.World, canvasStr string, cCols, cRows int) string {
	return v.composeNavballOverlay(w, canvasStr, cCols, cRows, false)
}

// crashedVesselNameLabel decorates a Crashed vessel name with a
// `[CR]` ASCII prefix dimmed by the theme. Live vessels render
// without the prefix. The marker is plain ASCII rather than a
// glyph because the v0.11.4 catalog sweep doesn't audit
// emoji/Unicode support in the render palette — the safer choice
// when adding a status indicator that may appear on any terminal.
// v0.11.4+ (ADR 0004).
func crashedVesselNameLabel(th Theme, c *spacecraft.Spacecraft) string {
	if c == nil || !c.Crashed {
		if c != nil {
			return c.Name
		}
		return ""
	}
	return th.Dim.Render("[CR] ") + c.Name
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
		// Hyperbolic or degenerate. This counts as an ascent only while
		// the craft is still LOW — a straight-up pad climb reads as a
		// radial/degenerate orbit, and an over-burned ascent can flicker
		// hyperbolic. But a hyperbolic trajectory HIGH above the body is
		// a departure (an over-energetic TLI escaping Earth) or a
		// flyby/approach to another atmosphered body (a hyperbolic Mars
		// arrival) — not a launch. Gate on altitude so the ascent HUD
		// doesn't pop back far from any launch; show the `—` placeholders
		// only while genuinely near the surface.
		return c.Altitude() < c.Primary.Atmosphere.CutoffAltitude
	}
	primaryR := c.Primary.RadiusMeters()
	periAlt := el.Periapsis() - primaryR
	// Hide once the orbit is stable — the periapsis has climbed clear of
	// the atmosphere, so the ascent is finished and drag can no longer
	// decay the orbit. The ascent instruments (TWR, FPA, downrange) are
	// done; the orbit-relative HUD takes over. The old gate compared
	// against the 200 km mission floor, which kept the launch HUD pinned
	// over a perfectly good sub-200 km parking orbit (e.g. a 186 × 186 km
	// circular orbit reads periAlt 186 km < 200 km and never cleared).
	// The mission floor stays the threshold for the ORBIT READY callout
	// + progress row above; this is only the show/hide gate.
	return periAlt < c.Primary.Atmosphere.CutoffAltitude
}

// launchMissionFloorM is the package-local alias for the canonical
// sim.LaunchMissionFloorM (200 km). Pre-v0.10.7 this lived here as a
// package-private const; v0.10.7 hoisted it into sim so the launch-
// anchor predicate can read it without crossing the screens→sim layer
// boundary. The original orbit.go callsites (LAUNCH HUD block,
// shouldShowLaunchHUD, ORBIT READY gate) keep their local name; the
// JSON mirror at internal/missions/missions.json:40 is unchanged.
const launchMissionFloorM = sim.LaunchMissionFloorM

// descentHUDAltitudeM is the altitude threshold (above the airless
// primary's mean radius) below which the DESCENT block lights up.
// 50 km gives the player a full warp-10× tick worth of warning at
// orbital-class lateral speeds before reaching the surface; smaller
// values would let an impactor approach pass the surface inside one
// readout-update without giving the v_horiz row time to register.
const descentHUDAltitudeM = 50_000.0

// shouldShowDescentHUD returns true when the active craft is in
// surface-proximity flight on an airless body — the regime where
// the v_vert / v_horiz split (not the scalar orbital velocity) is
// what determines whether the next surface contact is a soft
// touchdown or a high-|V| Crashed. Counterpart to shouldShowLaunchHUD
// for airless bodies; the two are mutually exclusive via the
// Atmosphere == nil gate (LAUNCH is atmospheric, DESCENT is airless).
//
// v0.11.4-followup: added after the playtest report of "vessel
// suddenly crashed during controlled moon descent" — the always-on
// VESSEL block renders altitude and scalar `velocity` only, so a
// craft with ~1 km/s residual orbital lateral velocity at 5–10 km
// altitude reads "low and slow" by altitude alone but flips to
// Crashed (|V| ≫ CrashVCritMps) on the next surface contact. The
// new block surfaces v_vert / v_horiz / fpa / twr so the lateral
// component is legible while there's still room to bleed it.
//
// Trigger: airless primary AND (current altitude < descentHUDAltitudeM
// OR the bound orbit's periapsis sits below the surface — an impactor
// trajectory still 100 km up benefits from the readout as much as
// one at 5 km).
//
// Crashed / Landed crafts never reach the surrounding HUD path so the
// predicate doesn't need to gate on them.
// shouldShowChuteHUD gates the v0.12 Slice 3 CHUTE block: shown for any
// chute-bearing vessel (HasParachute mirror true) that is still in
// flight — not Landed (the descent is over) and not Crashed (no chute
// could save it). Independent of the LAUNCH / DESCENT atmosphere gates;
// the chute readout is relevant for the whole descent, and STOWED state
// reminds the player they can still arm it.
func shouldShowChuteHUD(c *spacecraft.Spacecraft) bool {
	if c == nil {
		return false
	}
	return c.HasParachute && !c.Landed && !c.Crashed
}

func shouldShowDescentHUD(c *spacecraft.Spacecraft) bool {
	if c == nil {
		return false
	}
	if c.Primary.Atmosphere != nil {
		return false
	}
	if c.Primary.RadiusMeters() <= 0 {
		return false
	}
	if c.Altitude() < descentHUDAltitudeM {
		return true
	}
	mu := c.Primary.GravitationalParameter()
	if mu == 0 {
		return false
	}
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if el.E < 1 && el.A > 0 {
		periAlt := el.Periapsis() - c.Primary.RadiusMeters()
		if periAlt < 0 {
			return true
		}
	}
	return false
}

// FlightPhase is a coarse classification of where a vessel is in its
// flight — pad / ascent / cruise / transfer / approach / descent / landed.
// It consolidates the signals the chip-relevance predicates above already
// read (atmosphere, altitude bands, orbit eccentricity, radial-velocity
// sign, the Landed flag) into one named vocabulary.
//
// v0.16.1 scaffolding (consolidation only): nothing renders against this
// yet, so deriving it changes no on-screen behaviour. It exists so a later
// chip timing+content slice can gate chips on a single phase value instead
// of re-deriving the regime per chip. To stay a true consolidation rather
// than a second source of truth, the atmospheric-ascent and airless-descent
// branches defer to the authoritative shouldShowLaunchHUD /
// shouldShowDescentHUD predicates rather than re-implementing their (subtle,
// well-commented) gates.
type FlightPhase int

const (
	// PhaseCoast is the zero value: a near-circular bound parking / cruise
	// orbit, and the safe fallback for nil / degenerate state.
	PhaseCoast FlightPhase = iota
	PhasePrelaunch
	PhaseAscent
	PhaseTransfer
	PhaseApproach
	PhaseDescent
	PhaseLanded
)

func (p FlightPhase) String() string {
	switch p {
	case PhasePrelaunch:
		return "prelaunch"
	case PhaseAscent:
		return "ascent"
	case PhaseTransfer:
		return "transfer"
	case PhaseApproach:
		return "approach"
	case PhaseDescent:
		return "descent"
	case PhaseLanded:
		return "landed"
	default:
		return "coast"
	}
}

// transferEccThreshold is the eccentricity above which a bound orbit reads
// as a transfer/encounter arc rather than a parking-orbit Coast. 0.2 keeps
// a slightly-elliptical LEO classed as Coast while a Hohmann transfer
// ellipse (e ≈ 0.7 LEO→GEO, higher for lunar) reads as Transfer/Approach.
const transferEccThreshold = 0.2

// prelaunchAltM is the altitude below which an ascending atmospheric vessel
// reads as just off the pad (Prelaunch) rather than in active climb
// (Ascent). Matches the tower-clear scale used by the ascent guidance. Note
// a vessel actually *on* the ground is PhaseLanded (the Landed flag wins);
// Prelaunch is the brief airborne, below-tower-clear window right after
// release. The on-ground state can't be told apart from a post-mission
// landing by state alone, so both collapse to Landed.
const prelaunchAltM = 1000.0

// deriveFlightPhase classifies a vessel's current flight phase from its own
// state. Pure (no World dependency) so it can be unit-tested in isolation.
// See FlightPhase for the consolidation-only intent.
func deriveFlightPhase(c *spacecraft.Spacecraft) FlightPhase {
	if c == nil {
		return PhaseCoast
	}
	if c.Landed {
		return PhaseLanded
	}
	// Radial velocity sign separates outbound (climbing) from inbound
	// (falling toward periapsis / an encounter).
	var vUp float64
	if r := c.State.R; r.Norm() > 0 {
		vUp = c.State.V.Dot(r.Unit())
	}
	// Atmospheric ascent — defer to the authoritative launch predicate, then
	// split pad-bound Prelaunch from active Ascent. A descending vessel low
	// in the atmosphere (re-entry) is suborbital and would also satisfy the
	// launch predicate, so require non-descending to count as ascent.
	if shouldShowLaunchHUD(c) && vUp >= 0 {
		if c.Altitude() < prelaunchAltM {
			return PhasePrelaunch
		}
		return PhaseAscent
	}
	// Airless surface-proximity — defer to the authoritative descent predicate.
	if shouldShowDescentHUD(c) {
		return PhaseDescent
	}
	// Otherwise we're on an orbit / transfer arc; classify by shape + direction.
	mu := c.Primary.GravitationalParameter()
	if mu == 0 {
		return PhaseCoast
	}
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	bound := el.E < 1 && el.A > 0
	if !bound || el.E >= transferEccThreshold {
		if vUp < 0 {
			return PhaseApproach // inbound — falling toward periapsis / encounter
		}
		return PhaseTransfer // outbound — climbing toward apoapsis
	}
	return PhaseCoast // near-circular parking / cruise orbit
}

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

// normalizeDeg wraps an angle in degrees into [0, 360).
func normalizeDeg(d float64) float64 {
	d = math.Mod(d, 360)
	if d < 0 {
		d += 360
	}
	return d
}

// viewBasis returns the canvas projection basis for the world's
// current ViewMode. Six cases — ViewTilted (v0.10.6+ default;
// perifocal tilt with TiltedWorldBasis fallback), four cardinals
// (Top XY-drop, Right YZ, Bottom XY mirrored, Left YZ mirrored),
// and orbit-flat (perifocal x̂/ŷ for a clean ellipse regardless
// of inclination). Orbit-flat falls back to Top's basis when the
// orbit is degenerate (no craft, Landed, e ≥ 1, a ≤ 0); ViewTilted
// falls back to TiltedWorldBasis in the same cases so the depth
// cue stays alive on the pad.
//
// Single-craft today; multi-craft will need an active-craft selector
// to disambiguate "the active orbit" (state-of-game.md §2 backlog).
func viewBasis(w *sim.World) widgets.Basis {
	switch w.ViewMode {
	case sim.ViewTilted:
		thetaRad := w.ViewTilt.Theta * math.Pi / 180
		phiRad := w.ViewTilt.Phi * math.Pi / 180
		el, ok := activeCraftElements(w)
		// v0.10.7+: launch-anchor overrides φ with local-vertical while
		// the active craft is in the launch band (apoAlt ≤ 200 km).
		// Render-computed on-read — World.ViewTilt.Phi stays at the
		// player's value (currently always 0; player-φ controls deferred
		// to a post-playtest signal) so future shift+←→ wiring won't
		// collide with per-tick anchor writes.
		if anchorPhi, active := sim.LaunchAnchorPhi(w.ActiveCraft(), el, ok); active {
			phiRad = anchorPhi
		}
		if !ok {
			return widgets.TiltedWorldBasis(thetaRad, phiRad)
		}
		return widgets.TiltedPerifocalBasis(el, thetaRad, phiRad)
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
		el, ok := activeCraftElements(w)
		if !ok {
			return widgets.DefaultBasis()
		}
		xHat, yHat := orbital.PerifocalBasis(el)
		return widgets.Basis{X: xHat, Y: yHat}
	}
	return widgets.DefaultBasis() // ViewTop or any future mode
}

// activeCraftElements returns the active craft's heliocentric-or-
// primary-centric Keplerian elements when they're meaningful for
// basis selection, and ok=false when the perifocal basis is
// undefined: no active craft, Landed (co-rotating "orbit" would
// lock the camera to the body and freeze the surface texture),
// hyperbolic (e ≥ 1), or degenerate (a ≤ 0 / NaN / Inf). Shared by
// ViewTilted's perifocal-vs-world fallback and ViewOrbitFlat's
// perifocal-vs-DefaultBasis fallback so both modes apply the same
// "is the orbit basis usable?" rule. v0.10.6+.
func activeCraftElements(w *sim.World) (orbital.Elements, bool) {
	c := w.ActiveCraft()
	if c == nil {
		return orbital.Elements{}, false
	}
	if c.Landed {
		return orbital.Elements{}, false
	}
	mu := c.Primary.GravitationalParameter()
	if mu <= 0 {
		return orbital.Elements{}, false
	}
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if el.A <= 0 || el.E >= 1 || math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return orbital.Elements{}, false
	}
	return el, true
}
