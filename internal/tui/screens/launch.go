package screens

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// LaunchView (v0.11.0+) is the ViewLaunch chase-cam screen — a sibling
// of OrbitView, not an extension. ADR-0002 covers the call. Minimal
// chrome: title, canvas with the chase-cam scene, footer with key
// hints; no sidepanel (the orbit screen's body list / target panel
// are irrelevant during a launch). The HUD launch-readout strip is
// overlaid on the bottom braille row of the canvas (same precedent as
// the orbit screen's status overlay).
type LaunchView struct {
	canvas    *widgets.Canvas
	theme     Theme
	hudSource *OrbitView // reused for the side-HUD chrome (v0.11.0+)

	// lastVZSample caches the previous tick's altitude + sim-time so
	// the HUD can compute v_z (m/s) as a finite difference rather than
	// requiring a sim-side velocity decomposition. Re-keyed on active-
	// craft change so a vessel switch can't bleed a stale baseline.
	vzCraft *spacecraft.Spacecraft
	vzAltM  float64
	vzAtSim time.Time

	// descentStopCache / descentStopCacheComputes: the predict-on-change
	// cache for the descent corridor's integrated stop-burn forecast and
	// "burn at" search (issue #377). See launch_descent_cache.go.
	descentStopCache         descentStopRenderCache
	descentStopCacheComputes int
}

// NewLaunchView constructs the chase-cam screen, paired with the
// supplied OrbitView so the right-side HUD column (VESSEL /
// PROPELLANT / ATTITUDE / LAUNCH / NAVBALL) reuses the OrbitView's
// renderers verbatim. Playtest (Slice 1.6) showed the no-sidepanel
// experiment from the original plan dropped too many readouts
// (altitude, stage fuel, horizontal velocity) for the launch view to
// stay flyable; reusing the orbit HUD keeps the chrome legible.
func NewLaunchView(th Theme, hudSource *OrbitView) *LaunchView {
	return &LaunchView{
		canvas:    widgets.NewCanvas(80, 24),
		theme:     th,
		hudSource: hudSource,
	}
}

// Resize sizes the canvas to span the full terminal width (mirrors
// OrbitView so the two screens line up when cycling). Reserve 3 rows: the
// title plus the canvas's top and bottom border. The keybind footer was
// dropped (matching the orbit screen), giving its row to the scene.
func (v *LaunchView) Resize(totalCols, totalRows int) {
	// v0.13: full-width canvas (no side HUD column) — the launch readouts
	// are canvas chips now, matching the orbit screen. 2 cols for the
	// rounded border, 3 rows for the title + top/bottom border.
	canvasCols := totalCols - 2
	if canvasCols < 20 {
		canvasCols = 20
	}
	v.canvas.Resize(canvasCols, totalRows-3)
}

// CurrentScale returns the metres-per-cell scale the chase-cam is
// currently rendering at — either the player-pinned w.LaunchZoom
// (when non-zero) or the auto-altitude-driven default sized to this
// view's canvas. Callers (App's `+/-` handler) pass it to
// World.NudgeLaunchZoom so the first nudge from auto pins the
// pre-render scale rather than a hardcoded constant.
func (v *LaunchView) CurrentScale(w *sim.World) float64 {
	if w.LaunchZoom > 0 {
		return w.LaunchZoom
	}
	c := w.ActiveCraft()
	if c == nil {
		return 1.0
	}
	return launchAutoScale(c.Altitude(), v.canvas.Rows())
}

// launchAutoScale returns the auto-altitude-driven scale (metres per
// cell) the chase-cam uses when the player hasn't pinned a zoom via
// `+/-`. Formula from plan: scale = max(1.0, altitude / denom), where
// denom is rows minus rows/3 (the bottom third reserved for horizon
// + ground fill). Clamps denom ≥ 1 so a degenerate small canvas
// doesn't divide by zero.
func launchAutoScale(altitudeM float64, rows int) float64 {
	denom := rows - rows/3
	if denom < 1 {
		denom = 1
	}
	s := altitudeM / float64(denom)
	if s < 1.0 {
		return 1.0
	}
	return s
}

// formatLaunchHUD renders the v0.11 Slice 1 launch-readout strip
// overlaid on the chase-cam canvas's bottom braille row. Format:
//
//	T+ HH:MM:SS  v_z ±XXX m/s | downrange X.X km  Q XX.X kPa (max YY.Y)
//
// Inputs in SI units: vZ m/s, downrangeM m, q / qMaxPa Pa.
func formatLaunchHUD(tPlus time.Duration, vZ, downrangeM, qPa, qMaxPa float64) string {
	secs := int(tPlus.Seconds())
	if secs < 0 {
		secs = 0
	}
	h := secs / 3600
	m := (secs / 60) % 60
	s := secs % 60
	return fmt.Sprintf(
		"T+ %02d:%02d:%02d  v_z %+d m/s | downrange %.1f km  Q %.1f kPa (max %.1f)",
		h, m, s,
		int(vZ),
		downrangeM/1000.0,
		qPa/1000.0,
		qMaxPa/1000.0,
	)
}

// isNearHemisphere reports whether a body-relative point lies on the
// camera-facing hemisphere of its primary body, used by the ViewLaunch
// scene to depth-cull body-fixed markers (pad, trail dots) that the
// body's own surface would otherwise occlude. Both inputs are
// positions relative to the body centre. Ties (the limb) render as
// near-side so an edge marker doesn't flicker out as the body rotates
// the launch site across the horizon.
func isNearHemisphere(pointFromBody, cameraFromBody orbital.Vec3) bool {
	return pointFromBody.Dot(cameraFromBody) >= 0
}

// Render builds the chase-cam frame for the current world state.
// Slice 1 chrome: title + canvas + footer; the canvas carries the
// horizon curve + SurfaceColor fill + pad marker + breadcrumb trail
// + active-vessel glyph; the HUD launch-readout strip is overlaid on
// the bottom braille row.
func (v *LaunchView) Render(w *sim.World, totalCols, totalRows int) string {
	v.Resize(totalCols, totalRows)
	v.canvas.Clear()

	craft := w.ActiveCraft()
	craftName := ""
	if craft != nil {
		craftName = craft.Name
	}
	title := v.theme.Title.Render(fmt.Sprintf("LAUNCH — %s", craftName))

	// The descent half (ADR 0043 §3): one forecast per frame, shared by
	// the scene (dashed arc + ground marker) and the corridor chip, so
	// the picture and the numbers can never disagree about where this
	// trajectory lands. Ballistic-from-now, so an active burn walks it
	// live — see sim.PredictImpact.
	corridor, descending := sim.DescentCorridorFor(craft, sim.DescentPredictHorizon)
	// The expensive half (issue #377): the integrated stop-burn forecast
	// and "burn at" cue, CACHED (unlike the cheap half above, which is
	// fine to redo every frame) — see cachedDescentStop's doc comment for
	// why. Only merged in while actually descending; DescentCorridorFor's
	// own gate already governs whether this block renders at all.
	if descending {
		stopDat := v.cachedDescentStop(w, craft)
		corridor.Stop = stopDat.stop
		corridor.StopOK = stopDat.stopOK
		corridor.BurnAt = stopDat.burnAt
		corridor.HasBurnAt = stopDat.hasBurnAt
		corridor.Margin = sim.DeriveMarginState(stopDat.stop, stopDat.stopOK, corridor.AltitudeM, stopDat.burnAt, stopDat.hasBurnAt)
	}
	// The ascent half (ADR 0043 §3 / issue #348): the mirror-image gate —
	// climbing, not falling — so exactly one of the two instrument sets is
	// ever live for a given state (sim.AscentCueFor's doc comment; pinned
	// by TestAscentCueForGatingMatrix). Bundles the nose/prograde vectors,
	// the predicted path ahead, and the atmosphere/Q band behind one call
	// for the same reason the descent corridor is one call: the scene and
	// the chip must never disagree.
	ascent, ascending := sim.AscentCueFor(w, craft, sim.AscentPredictHorizon)

	if craft != nil && craft.Primary.MeanRadius > 0 {
		v.renderScene(w, craft, corridor, descending, ascent, ascending)
	} else if craft == nil {
		// v0.11.4+ (sub-scope 5): the end-flight path can leave the
		// slate empty mid-session (the player removes the only
		// vessel and stays in ViewLaunch). Without a craft the scene
		// pipeline has nothing to anchor on — the pre-v0.11.4 path
		// rendered an empty canvas with a blank-name title, which
		// reads as a bug. Drop a centered dim message on the canvas
		// instead so the empty state is honest.
		v.renderNoActiveVesselMessage()
	}

	canvasStr := v.canvas.String()
	// v0.11.4+ (sub-scope 6): mini-navball in the bottom-right
	// mirrors its OrbitView placement so the player has heading +
	// roll readout during launch / landing chase-cam — pitch alone
	// (visible via the sprite lean) isn't enough nav info. Reuses
	// the OrbitView's navball widget; no new render path. Composed
	// before the HUD strip overlay so the strip's last-row swap
	// preserves the navball above it.
	if v.hudSource != nil {
		// Declutter (F2, shared via the OrbitView) hides the navball and
		// every chip here too; the slim column stays.
		cCols, cRows := v.canvas.Cols(), v.canvas.Rows()
		nbReserved := 0
		if !v.hudSource.Declutter() {
			canvasStr = v.hudSource.ComposeNavballOverlay(w, canvasStr, cCols, cRows)
			nbReserved = v.hudSource.navballReservedRows(w, cCols, cRows)
		}
		// v0.13 (ADR 0010): the contextual blocks are Chips now, so the
		// launch screen composites the same relevant chips (LAUNCH /
		// STAGES / ATTITUDE / BURNS …) onto its own canvas — the side
		// column is just the slim telemetry block. Canvas content sits 1
		// col / 2 rows in (border + title), matching the orbit screen.
		chips := v.hudSource.assembleChips(w)
		// DESCENT and DESCENT CORRIDOR both answer "how is this landing
		// going", and while the corridor is live they were both on screen
		// — opposite corners, same altitude to two decimals, and the
		// descent rate stated twice with opposite signs (`v_vert: -40.0
		// m/s` against `descent: 40 m/s`). The corridor is the better
		// block (it forecasts the ground contact and says whether the stop
		// is still flyable), so it wins and DESCENT stands down here. Its
		// two non-redundant rows moved into the corridor rather than being
		// dropped — see sim.DescentCorridor's doc comment for which, and
		// why `twr` and `sas` were not among them. The orbit map keeps its
		// DESCENT chip untouched: there is no corridor there to replace it.
		if descending {
			chips = dropChip(chips, settings.ChipDescent)
		}
		// DESCENT CORRIDOR is the surface view's own chip — built here, not
		// in assembleChips, because it's the launch/surface screen's
		// instrument block and the orbit map has no ground line to read it
		// against. Empty id (always-on, F2 declutter still clears it) for
		// the same reason RENDEZVOUS and TIME LOCK are: it states a
		// constraint — whether this descent can still be stopped — that
		// nothing else on screen would say. chipPriorityForced for the
		// same reason DOCKED carries it (#328): a corner that overflows
		// must not silently swallow the one readout saying this descent
		// can no longer be stopped — an overlap is recoverable, a missing
		// alarm is not.
		if descending && v.hudSource.chipEnabled("") {
			chips = append(chips, builtChip{
				corner:   cornerTopRight,
				lines:    v.descentCorridorLines(corridor),
				priority: chipPriorityForced,
			})
		}
		// ATMOSPHERE is the ascent half's own chip (issue #348 §3),
		// mirroring DESCENT CORRIDOR's placement and empty-id treatment —
		// same corner, same "always-on while the gate holds, F2 still
		// clears it" rule. Unlike the corridor's stop-margin alarm, nothing
		// here is safety-critical (there's no "can this still be stopped"
		// question during an ascent), so it competes for space at normal
		// priority instead of chipPriorityForced. Gated additionally on
		// HasQBand — an airless-body ascent gets the nose/prograde markers
		// and the arc but has no atmosphere to chart.
		if ascending && ascent.HasQBand && v.hudSource.chipEnabled("") {
			chips = append(chips, builtChip{
				corner: cornerTopRight,
				lines:  v.ascentQBandLines(ascent.QBand),
			})
		}
		canvasStr = v.hudSource.composeChips(canvasStr, cCols, cRows, nbReserved, 1, 2, chips)
	}
	canvasStr = overlayHUDStrip(canvasStr, v.composeHUDLine(w, craft))

	// Manual rounded-border wrapping. lipgloss.Border().Render() over
	// a string with embedded per-cell ANSI escapes (the case here —
	// FillProjectedSphere tags thousands of cells with SurfaceColor)
	// miscounts visible width and inflates each row ~22×, pushing
	// the side HUD off the right of the terminal. Manual borders
	// give us exact control: use lipgloss.Width per line for the
	// pad math, which strips ANSI before measuring.
	canvasPanel := wrapBorder(canvasStr, v.canvas.Cols(), v.theme.Primary.GetForeground())

	// v0.13 playtest move: the launch-relevant readouts (VESSEL core,
	// LAUNCH, STAGES, ATTITUDE) are all canvas Chips now, composited above,
	// so there's no side HUD column — the launch view spans the full width
	// like the orbit screen. The keybind footer was dropped too, so the
	// scene claims that row (the `?` overlay is the keybinding reference).
	return title + "\n" + canvasPanel
}

// wrapBorder draws a rounded-border frame around a multi-line content
// block. `innerCols` is the visible cell width each content row should
// occupy. Built manually rather than via lipgloss.NewStyle().Border()
// because lipgloss's bordering mis-measures width on strings densely
// embedded with per-cell ANSI escapes (the FillProjectedSphere case),
// inflating rows ~22×.
func wrapBorder(content string, innerCols int, borderFg lipgloss.TerminalColor) string {
	lines := strings.Split(content, "\n")
	borderStyle := lipgloss.NewStyle().Foreground(borderFg)
	top := borderStyle.Render("╭" + strings.Repeat("─", innerCols) + "╮")
	bottom := borderStyle.Render("╰" + strings.Repeat("─", innerCols) + "╯")
	leftEdge := borderStyle.Render("│")
	rightEdge := borderStyle.Render("│")
	rows := make([]string, 0, len(lines)+2)
	rows = append(rows, top)
	for _, line := range lines {
		pad := innerCols - displayWidth(line)
		if pad < 0 {
			pad = 0
		}
		rows = append(rows, leftEdge+line+strings.Repeat(" ", pad)+rightEdge)
	}
	rows = append(rows, bottom)
	return strings.Join(rows, "\n")
}

// ansiEscapeRE matches CSI / SGR escape sequences emitted by lipgloss
// (e.g. `\x1b[38;2;107;142;78m`). Used by displayWidth to strip ANSI
// before measuring terminal-cell width — lipgloss.Width works in
// isolation but mis-counted in the live launch render path (cause
// not fully diagnosed; manual strip is the safe fallback).
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// displayWidth returns the visible terminal-cell width of a string,
// stripping ANSI escapes and counting runes.
func displayWidth(s string) int {
	return utf8.RuneCountInString(ansiEscapeRE.ReplaceAllString(s, ""))
}

// joinHorizontalLines glues two multi-line blocks side-by-side, one
// line at a time, separated by `gap`. Pads the shorter block's rows
// with empty strings so the result is rectangular at the longer
// block's row count. v0.11.0 used lipgloss.JoinHorizontal but on the
// densely-coloured launch canvas (thousands of per-cell ANSI escape
// sequences from FillProjectedSphere) lipgloss mis-measured the
// canvas width and inflated each row to ~22× its intended cell
// count, pushing the HUD column off the right of any terminal.
func joinHorizontalLines(left, right, gap string) string {
	ls := strings.Split(left, "\n")
	rs := strings.Split(right, "\n")
	n := len(ls)
	if len(rs) > n {
		n = len(rs)
	}
	leftPad := visibleWidth(ls)
	out := make([]string, n)
	for i := 0; i < n; i++ {
		var l, r string
		if i < len(ls) {
			l = ls[i]
		}
		if i < len(rs) {
			r = rs[i]
		}
		pad := leftPad - displayWidth(l)
		if pad < 0 {
			pad = 0
		}
		out[i] = l + strings.Repeat(" ", pad) + gap + r
	}
	return strings.Join(out, "\n")
}

// visibleWidth returns the widest display-cell width across the given
// lines after stripping ANSI escapes (see displayWidth).
func visibleWidth(lines []string) int {
	w := 0
	for _, line := range lines {
		if lw := displayWidth(line); lw > w {
			w = lw
		}
	}
	return w
}

// renderNoActiveVesselMessage stamps a centered "no active vessel"
// message into v.canvas when the active slot is empty (sub-scope 5).
// Reachable today via the end-flight path (sub-scope 3) — removing
// the only vessel from the slate empties ActiveCraft while leaving
// the player parked in ViewLaunch. Pre-v0.11.4 this rendered a
// blank canvas with the unhelpful title `LAUNCH — `; the centered
// message keeps the empty state honest. v0.11.4+.
func (v *LaunchView) renderNoActiveVesselMessage() {
	const msg = "no active vessel"
	rows := v.canvas.Rows()
	cols := v.canvas.Cols()
	if rows <= 0 || cols <= 0 {
		return
	}
	row := rows / 2
	col := (cols - len(msg)) / 2
	if col < 0 {
		col = 0
	}
	v.canvas.SetCellLabel(col, row, msg)
}

// renderScene draws the horizon, surface fill, pad marker, trail dots,
// and active-vessel glyph into v.canvas. Caller guarantees craft is
// non-nil and craft.Primary has a non-zero radius.
//
// Camera basis (per ADR-0002 + plan): X = h_axis (commanded-attitude
// projected onto the local-horizontal plane, falling back to surface-
// frame east when the commanded direction is near-vertical); Y =
// local-up (radial from body centre). Depth axis points laterally —
// useful for hemisphere culling. ViewTilt.Theta is suppressed inside
// ViewLaunch per ADR-0002.
func (v *LaunchView) renderScene(w *sim.World, craft *spacecraft.Spacecraft, corridor sim.DescentCorridor, descending bool, ascent sim.AscentCue, ascending bool) {
	body := craft.Primary
	// craft.State.R is primary-relative (Earth-centred for a LEO craft,
	// Moon-centred for a Luna-orbiting craft, etc.). The render layer
	// works in a primary-centred frame so we keep the body at the
	// origin — heliocentric BodyPosition isn't needed here. v0.11.0
	// verification surfaced a mix-up where camWorld was treated as
	// world-frame and subtracted from BodyPosition, producing the
	// craft's offset from the Sun instead of from Earth.
	camFromBody := craft.State.R
	camDist := camFromBody.Norm()
	if camDist <= 0 {
		return
	}
	bodyCentre := orbital.Vec3{}
	camWorld := camFromBody
	localUp := camFromBody.Scale(1.0 / camDist)
	hAxis := chaseHorizontalAxis(craft, body, camFromBody, localUp)

	basis := widgets.Basis{X: hAxis, Y: localUp}
	v.canvas.SetBasis(basis)

	rows := v.canvas.Rows()
	altitudeM := craft.Altitude()
	scale := w.LaunchZoom
	if scale <= 0 {
		scale = launchAutoScale(altitudeM, rows)
	}
	// Canvas takes pixels-per-metre; launchAutoScale / LaunchZoom are
	// metres-per-cell. The OrbitView precedent treats scale as a
	// scalar (1/m/cell) without the 4x braille-row correction —
	// matching that keeps `+/-` zoom multipliers symmetric with the
	// orbit screen's behaviour, and the body fills the right cell-band.
	v.canvas.SetScale(1.0 / scale)

	v.canvas.Center(camWorld)

	// Horizon curve + SurfaceColor flood-fill below.
	v.drawHorizonAndFill(body, bodyCentre)

	// Current-orbit ellipse, rendered exactly as the orbit-map screens
	// do (same DrawEllipseClass Real-class path + apo/peri markers).
	// Drawn after the body fill so the near arc paints over the disk and
	// the far arc is depth-culled behind it; drawn before the surface
	// markers + rocket so those layer on top. v0.14+.
	v.drawOrbitPath(craft, bodyCentre)

	// Descent half (ADR 0043 §3): the dashed arc down to the ground and
	// the impact point where it lands. Drawn after the orbit ellipse (a
	// descent arc is the near-term detail of that same orbit and should
	// win where they overlap) and before the surface markers + rocket, so
	// the pad, the tower and the vessel still layer on top.
	//
	// Ascent half (ADR 0043 §3 / issue #348): the dashed path ahead of a
	// climbing vessel, same layering rule. descending/ascending are
	// mutually exclusive (sim.AscentCueFor's doc comment), so at most one
	// of these ever draws.
	if descending {
		v.drawDescentArc(bodyCentre, camFromBody, corridor)
	} else if ascending {
		v.drawAscentArc(bodyCentre, ascent.Arc)
	}

	// Pad marker at the active craft's launch site, depth-culled.
	v.drawPadMarker(w, craft, bodyCentre, camFromBody)

	// Launch tower (Slice 2): body-fixed multi-cell sprite at the pad.
	v.drawLaunchTower(w, craft, bodyCentre, camFromBody, scale)

	// Breadcrumb trail: each TrailPoint re-projected via
	// BodyFixedToWorld so the trace rotates with the body.
	v.drawTrail(w, body, bodyCentre, camFromBody)

	// Sibling vessels in the active craft's SOI (Slice 2): dropped
	// stages, sister crafts, anything sharing the primary. Drawn
	// after the trail so an exact-overlap stage glyph wins over a
	// trail dot.
	v.drawSOICraft(w, craft, bodyCentre, camFromBody, basis, scale)

	// Active vessel at the camera centre. v0.11.3 (Slice 4):
	// composed-from-stages sprite + amber pulsed flame below
	// Stages[0]. Falls back to the legacy single-glyph render
	// when no stage carries a LaunchSprite (custom NewFromStages
	// crafts or sprite-less catalog overlays).
	if !v.drawComposedRocket(craft, camWorld, basis, scale) {
		glyph := '+'
		if craft.Glyph != "" {
			for _, r := range craft.Glyph {
				glyph = r
				break
			}
		}
		v.canvas.SetCellOverlay(camWorld, glyph)
	}

	// Ascent half (ADR 0043 §3 / issue #348): nose-vs-prograde vector
	// stubs anchored right on the sprite, drawn after the rocket so they
	// read against it rather than under it. Only meaningful while
	// ascending — HasAttitude is additionally false pre-first-tick or
	// while sitting dead still on the pad (AttitudeVectorsFor).
	if ascending && ascent.HasAttitude {
		v.drawAscentAttitudeMarkers(ascent.Attitude, camWorld, scale)
	}

	// RCS puffs (v0.11.5 sub-scope 5): visible in the chase-cam so
	// the player reads thruster activity inside the launch view, not
	// just OrbitView. Same renderer shape as orbit.go — bright-white
	// origin + dim-grey tip — but translated from world-inertial to
	// the LaunchView's body-relative frame (body at origin).
	v.drawRCSPuffs(w, craft, bodyCentre, scale)
}

// drawRCSPuffs paints the active world's recent RCS puffs into the
// chase-cam scene as a bright-white origin pixel + dim-grey tip,
// matching the OrbitView render. Puff.Inertial sits in world-inertial
// coords (primary's BodyPosition + craft.State.R); the LaunchView's
// canvas is body-relative (body at orbital.Vec3{}), so we subtract
// the primary's BodyPosition to land each puff in the right frame.
// v0.11.5 sub-scope 5.
func (v *LaunchView) drawRCSPuffs(w *sim.World, active *spacecraft.Spacecraft, bodyPos orbital.Vec3, scaleMPerPx float64) {
	if scaleMPerPx <= 0 {
		return
	}
	primaryWorld := w.BodyPosition(active.Primary)
	puffStep := 5.0 * scaleMPerPx
	for _, p := range w.RCSPuffs() {
		if p.AgeFrac >= 0.75 {
			continue
		}
		bodyRel := p.Inertial.Sub(primaryWorld).Add(bodyPos)
		origin := bodyRel.Add(p.Exhaust.Scale(puffStep))
		tip := bodyRel.Add(p.Exhaust.Scale(2 * puffStep))
		v.canvas.PlotColored(origin, render.ColorRCSPuffOrigin)
		v.canvas.PlotColored(tip, render.ColorRCSPuffTip)
	}
}

// drawComposedRocket plots a vessel's composed-from-stages launch
// sprite + flame at anchorWorld via the launch-render basis and
// scale (v0.11.3 Slice 4). Returns false when no stage carries a
// LaunchSprite — caller falls back to the legacy single-glyph
// render. Flame frame index derives from wall-clock for a stable
// ~100 ms pulse cadence regardless of sim warp.
//
// Per-sub-pixel stride is FIXED real-world metres (vesselSubPixelM),
// not zoom-scaled — same precedent as drawLaunchTower's
// lutRowHeightM / lutColWidthM (v0.11.5-followup). The original v0.11.3
// cut passed `scaleMPerPx` (the autozoom m/cell) through as the
// sub-pixel stride, so the sprite occupied the same canvas area
// regardless of altitude. Pinning the stride to vesselSubPixelM lets
// the rocket shrink on screen as the autozoom grows.
//
// Flame gating: Throttle is the loadout-default engine-power setting
// (typically 1.0 on a pad-spawned vessel), NOT a sign that the
// engine is firing. Flame renders only when the vessel has an
// active burn — either a player-engaged ManualBurn or a planted
// ActiveBurn — so a pad-spawned rocket doesn't paint amber flame
// into the body fill before the player presses `b`.
func (v *LaunchView) drawComposedRocket(craft *spacecraft.Spacecraft, anchorWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64) bool {
	// v0.16: floor the vessel at its glyph. The sprite stride is fixed
	// real-world metres (vesselSubPixelM), so as the chase-cam autozoom
	// grows the silhouette shrinks on screen; past the point where the
	// whole sprite would fit inside a single terminal cell it degrades to
	// one or two lit braille dots — illegible. Below that floor, return
	// false so the caller paints the glyph cell overlay instead, and a
	// distant vessel always reads as at least its glyph, never a bare dot.
	if vesselSpriteBelowCellFloor(craft.Stages, scaleMPerPx) {
		return false
	}
	sprite := ComposeLaunchSprite(craft.Stages, craft.CurrentAttitudeDir, basis, vesselSubPixelM)
	if sprite == nil {
		return false
	}
	flameThrottle := 0.0
	if craft.ManualBurn != nil || craft.ActiveBurn != nil {
		flameThrottle = craft.Throttle
	}
	frameIdx := int(time.Now().UnixMilli()/flameFrameMs) % 2
	bellWidth := EngineBellWidth(craft.Stages)
	bell := ComposeEngineBell(craft.Stages, craft.CurrentAttitudeDir, basis, vesselSubPixelM)
	legs := ComposeLegs(craft.Stages, craft.CurrentAttitudeDir, basis, vesselSubPixelM)
	flame := ComposeFlame(craft.Stages, craft.CurrentAttitudeDir, basis, vesselSubPixelM, flameThrottle, frameIdx, bellWidth)
	// v0.12 Slice 3 (ADR 0008): a deployed parachute paints a canopy
	// above the top stage, giving the chute a visual identity for the
	// Shift+V manual jump and the test-lob cases.
	var canopy []SpritePixel
	if craft.ChuteState == spacecraft.ChuteDeployed {
		canopy = ComposeCanopy(craft.Stages, craft.CurrentAttitudeDir, basis, vesselSubPixelM)
	}
	// Plot each pixel as a braille sub-cell dot via PlotColored.
	// No SetCellOverlay glyph: braille dots are direction-agnostic,
	// so a tilted rocket renders smoothly at any pitch — the
	// gravity-turn smear the v0.11.3 ASCII first-cut produced is
	// gone. ClearCellOverlay after each plot removes the LUT's
	// body-fixed overlay glyphs in cells the rocket occupies, so
	// the braille dots show through at the pad (otherwise the
	// LUT's SetCellOverlay `║ ╤ █` would mask the rocket).
	for _, p := range sprite {
		world := anchorWorld.Add(p.OffsetWorld)
		v.canvas.PlotColored(world, p.Color)
		v.canvas.ClearCellOverlay(world)
	}
	for _, p := range bell {
		world := anchorWorld.Add(p.OffsetWorld)
		v.canvas.PlotColored(world, p.Color)
		v.canvas.ClearCellOverlay(world)
	}
	for _, p := range legs {
		world := anchorWorld.Add(p.OffsetWorld)
		v.canvas.PlotColored(world, p.Color)
		v.canvas.ClearCellOverlay(world)
	}
	for _, p := range flame {
		world := anchorWorld.Add(p.OffsetWorld)
		v.canvas.PlotColored(world, p.Color)
		v.canvas.ClearCellOverlay(world)
	}
	for _, p := range canopy {
		world := anchorWorld.Add(p.OffsetWorld)
		v.canvas.PlotColored(world, p.Color)
		v.canvas.ClearCellOverlay(world)
	}
	return true
}

// flameFrameMs is the wall-clock period (ms) per flame animation
// frame. Two frames swap at this cadence → full pulse cycle is
// 2 × flameFrameMs (~200 ms). Tied to wall-clock so warp doesn't
// speed up or slow down the visual pulse.
const flameFrameMs = 100

// chaseHorizontalAxis computes the projection-plane horizontal axis:
// the commanded (CurrentAttitudeDir) projection onto the local-
// horizontal plane when its magnitude is well-defined, falling back
// to surface-frame east at the craft's surface point when the
// attitude is near-vertical (rocket on the pad / just after liftoff).
//
// Threshold: |horiz| > sin(~0.6°) ≈ 0.01. v0.11.0 shipped at 1e-9
// which filtered only floating-point dust — but the integrator's
// per-tick snap leaves CurrentAttitudeDir lagging localUp by the
// rocket's per-tick rotation in inertial frame (ω·Δt at engine
// ignition: 3.6e-6 rad at Earth's spin × 50 ms base step). That lag
// is ~3600× above 1e-9, so `horiz` picked up the lag vector and
// normalised it to a unit west-ish direction during pure vertical
// climb, flipping the chase-cam east↔west until the player applied
// pitch trim. v0.11.1 raises the floor above the warp-scaled lag
// (≤ ~1e-4 rad at the 10× burn warp cap) but well below the
// smallest meaningful pitch trim (one step = 5° = 0.087 rad) — the camera
// orients east during vertical climb (intent), then swings to the
// pitch direction as the player gravity-turns.
func chaseHorizontalAxis(c *spacecraft.Spacecraft, body bodies.CelestialBody, camFromBody, localUp orbital.Vec3) orbital.Vec3 {
	cmd := c.CurrentAttitudeDir
	if cmd.Norm() > 0 {
		horiz := cmd.Sub(localUp.Scale(cmd.Dot(localUp)))
		if n := horiz.Norm(); n > chaseHorizEpsilon {
			return horiz.Scale(1.0 / n)
		}
	}
	east := render.BodyFrameEast(body, render.Vec3{X: camFromBody.X, Y: camFromBody.Y, Z: camFromBody.Z})
	return orbital.Vec3{X: east.X, Y: east.Y, Z: east.Z}
}

// chaseHorizEpsilon — sin(~0.6°). See chaseHorizontalAxis docstring
// for the slew-lag vs. pitch-trim noise-floor derivation.
const chaseHorizEpsilon = 0.01

// drawHorizonAndFill paints the body's projected silhouette below the
// horizon with SurfaceColor. In the chase-cam basis (h_axis,
// local_up), the body sphere projects orthographically to a circle of
// radius bodyRadius centred at the body's projected position — its
// upper edge IS the horizon (naturally flat at low altitude, naturally
// curved at altitude, because the canvas window slices a chord of a
// large circle). Uses Canvas.FillProjectedSphere so work is bounded by
// canvas size, not sphere size (planet radius * scale can be millions
// of cells at low zoom).
func (v *LaunchView) drawHorizonAndFill(body bodies.CelestialBody, bodyPos orbital.Vec3) {
	v.canvas.FillProjectedSphere(bodyPos, body.RadiusMeters(), lipgloss.Color(body.SurfaceColorHex()))
}

// drawOrbitPath plots the active craft's live Keplerian ellipse into
// the chase-cam scene, matching the orbit-map screens' render so the
// orbit reads identically whether the player is in ViewLaunch or a
// cardinal/tilted orbit view. The launch canvas already works in the
// primary-relative frame (body at bodyCentre = origin, craft at
// craft.State.R), and `el` is derived from the same primary-relative
// state vectors, so the offset is bodyCentre and the body-occlusion
// anchor is bodyCentre too — identical to orbit.go's primary-frame
// call once that screen translates into the system frame.
//
// Gating mirrors orbit.go: only bound (a > 0), numerically valid
// orbits whose apoapsis projects to ≥ minOrbitPixels render, and the
// Landed skip matches the orbit screen's activeCraftElements ok=false
// — a vessel co-rotating with the surface has a degenerate ellipse
// (apoapsis ≈ body radius) that clears the pixel gate at launch zoom
// and would paint a phantom arc through the planet. The orbit fades in
// as the ascent builds real orbital velocity and persists through a
// descent until touchdown clears it, the same as the map view shows.
func (v *LaunchView) drawOrbitPath(craft *spacecraft.Spacecraft, bodyCentre orbital.Vec3) {
	if craft.Landed {
		return
	}
	mu := craft.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(craft.State.R, craft.State.V, mu)
	scale := v.canvas.Scale()
	if !(el.A > 0) || math.IsNaN(el.A) || math.IsInf(el.A, 0) || el.Apoapsis()*scale < minOrbitPixels {
		return
	}
	canvasReach := v.canvas.Cols()*2 + v.canvas.Rows()*4
	primaryPxR := BodyPixelRadius(craft.Primary, false, scale, canvasReach)
	// Real class, bright (ADR 0041 §2) — same treatment as the orbit map's
	// own-craft ellipse.
	v.canvas.DrawEllipseClass(el, bodyCentre, 360, widgets.ClassReal, bodyCentre, primaryPxR, render.ColorCurrentOrbit)
	peri := bodyCentre.Add(orbital.PositionAtTrueAnomaly(el, 0))
	apo := bodyCentre.Add(orbital.PositionAtTrueAnomaly(el, math.Pi))
	// Unified single-glyph apsis markers (ADR 0020) — same ▼/▲ as the
	// orbit map, replacing the FillDisk blobs.
	if !v.canvas.IsBehindBody(peri, bodyCentre, primaryPxR) {
		drawMarker(v.canvas, peri, render.MarkerPeriapsis, render.MarkerNominal, "", widgets.CellTag{})
	}
	if !v.canvas.IsBehindBody(apo, bodyCentre, primaryPxR) {
		drawMarker(v.canvas, apo, render.MarkerApoapsis, render.MarkerNominal, "", widgets.CellTag{})
	}
}

// drawDescentArc inks the predicted path down to the ground plus the
// impact point where it lands (ADR 0043 §3 / issue #348).
//
// The arc is ClassPlanned — dashed — because it is a PLAN, not a live
// orbit: the consequence of flying the current state without further
// input (ADR 0041 §2's line-style vocabulary, PR #353). Colour is the
// separate, semantic axis: planned-cyan normally, alert-red once the
// burn margin says this descent can no longer be stopped, so the whole
// line carries the alarm rather than only the four-character label in
// the corridor chip.
//
// The impact marker is depth-culled by the same near-hemisphere test the
// pad and tower use — a contact point on the far side of the body would
// otherwise draw straight through the planet.
func (v *LaunchView) drawDescentArc(bodyCentre, camFromBody orbital.Vec3, dc sim.DescentCorridor) {
	if len(dc.Impact.Path) < 2 {
		return
	}
	alarm := dc.Margin.State == sim.MarginInsufficient
	arcColor := render.ColorPlannedNode
	if alarm {
		arcColor = render.ColorAlert
	}
	pts := make([]orbital.Vec3, len(dc.Impact.Path))
	for i, p := range dc.Impact.Path {
		pts[i] = bodyCentre.Add(p)
	}
	v.canvas.PlotPolylineClass(pts, arcColor, widgets.ClassPlanned)

	if !isNearHemisphere(dc.Impact.Point, camFromBody) {
		return
	}
	state := render.MarkerNominal
	if alarm {
		state = render.MarkerAlarm
	}
	drawMarker(v.canvas, bodyCentre.Add(dc.Impact.Point), render.MarkerImpact, state, "", widgets.CellTag{})
}

// descentCorridorLines renders the DESCENT CORRIDOR instrument block —
// issue #377's pinned row layout: altitude, descent rate, v_horiz, time
// to impact, the `burn at` cue (while a future start is safe and the
// burn hasn't started), and `stop margin` (always, while descending).
// `fpa` was folded out of this block in the same pass — the corridor's
// job is "can this still be stopped", and issue #377's row layout
// answers that with burn-at/stop-margin instead; DescentCorridor still
// carries FlightPathAngleDeg/HasFPA (descentKinematics computes them for
// free) for any other consumer that wants them.
//
// `stop margin` is the alarm surface — it flips label AND colour
// together (a bare "X up" green → amber TIGHT → red CAN'T STOP), because
// a state a player can miss reads as no state at all. The parenthesised
// limiter on the alarm states says which capability bound it (thrust vs
// fuel), so the alarm names the fix instead of only the fault.
func (v *LaunchView) descentCorridorLines(dc sim.DescentCorridor) []string {
	// v_horiz is folded in from the DESCENT chip this block replaces on
	// this screen (see the dropChip call in Render). It keeps its CRASH
	// styling verbatim: crossing the ground fast enough sideways wrecks
	// the vessel however gently the vertical rate has been nulled, and
	// that is not something the corridor's other numbers imply.
	horizLabel := fmt.Sprintf("%.0f m/s (surface-rel)", dc.HorizontalRateMps)
	if dc.HorizontalRateMps > sim.CrashVCritMps {
		horizLabel = v.theme.Alert.Render(
			fmt.Sprintf("%.0f m/s (> %.0f = CRASH on contact)", dc.HorizontalRateMps, sim.CrashVCritMps))
	}
	lines := []string{
		v.theme.Primary.Render("DESCENT CORRIDOR"),
		fmt.Sprintf("  altitude:   %s", formatAltitude(dc.AltitudeM)),
		fmt.Sprintf("  descent:    %.0f m/s", dc.DescentRateMps),
		fmt.Sprintf("  v_horiz:    %s", horizLabel),
		fmt.Sprintf("  impact in:  %s (%.0f m/s)",
			compactDuration(dc.Impact.TimeToImpact), dc.Impact.SpeedMps),
	}
	if dc.HasBurnAt {
		lines = append(lines, fmt.Sprintf("  burn at:    %s — in %s",
			formatAltitude(dc.BurnAt.AltitudeM), compactDuration(secondsToDuration(dc.BurnAt.InSec))))
	}
	lines = append(lines, fmt.Sprintf("  stop margin: %s", v.stopMarginLabel(dc)))
	return lines
}

// secondsToDuration converts a float64 seconds reading (sim.BurnAtCue /
// sim.PoweredStopPrediction both use float64 seconds, not time.Duration,
// since they're arithmetic results from an integration loop) into a
// time.Duration for compactDuration.
func secondsToDuration(s float64) time.Duration {
	if s < 0 {
		s = 0
	}
	return time.Duration(s * float64(time.Second))
}

// stopMarginLabel styles the `stop margin` row per PredictPoweredStop's
// outcome and the derived alarm state (dc.Margin, from
// sim.DeriveMarginState). Negative margin reads as "short by", never as
// a negative altitude (issue #377 §3).
func (v *LaunchView) stopMarginLabel(dc sim.DescentCorridor) string {
	if !dc.StopOK {
		return v.theme.Dim.Render("—")
	}
	switch dc.Stop.Outcome {
	case sim.StopStopped:
		label := fmt.Sprintf("%s up   (full thrust now)", formatAltitude(dc.Stop.MarginM))
		if dc.Margin.State == sim.MarginTight {
			return v.theme.Warning.Render(label + " TIGHT")
		}
		return v.theme.Primary.Render(label)
	case sim.StopCrashed:
		return v.theme.Alert.Render(fmt.Sprintf("short by %s (impact %.0f m/s) CAN'T STOP (%s)",
			formatAltitude(-dc.Stop.MarginM), dc.Stop.ImpactSpeedMps, dc.Margin.Limiter))
	case sim.StopFuelLimited:
		return v.theme.Alert.Render(fmt.Sprintf("fuel-limited at %s CAN'T STOP (%s)",
			formatAltitude(dc.Stop.MarginM), dc.Margin.Limiter))
	}
	return v.theme.Dim.Render("—")
}

// formatAltitude renders metres as m below 1 km and km above, matching
// the DESCENT chip's existing altitude row so the two readouts agree
// digit for digit when both are on screen.
func formatAltitude(m float64) string {
	if m >= 1000 {
		return fmt.Sprintf("%.2f km", m/1000)
	}
	return fmt.Sprintf("%.0f m", m)
}

// drawAscentArc inks the predicted path ahead of a climbing vessel (ADR
// 0043 §3 / issue #348) — the ascent mirror of drawDescentArc. ClassPlanned
// (dashed): a PLAN, not a live orbit (ADR 0041 §2, PR #353), same
// treatment as the descent arc. Unlike the descent arc there's no
// alarm/margin state to promote the colour with — an ascent has nothing
// analogous to "can this stop in time" — so the arc is always the plain
// planned-cyan.
//
// Nothing here needs to detect "the path exits the view" or "the vessel
// reaches orbit" explicitly: the canvas's own segment clipping
// (walkPixelDashSegment / clipSegmentToCanvas) already bounds the drawn
// run to what's on-screen, and sim.PredictAscentPath already returns a
// path capped to the ascent horizon whether or not it ever finds ground
// contact.
func (v *LaunchView) drawAscentArc(bodyCentre orbital.Vec3, arc sim.AscentPath) {
	if len(arc.Path) < 2 {
		return
	}
	pts := make([]orbital.Vec3, len(arc.Path))
	for i, p := range arc.Path {
		pts[i] = bodyCentre.Add(p)
	}
	v.canvas.PlotPolylineClass(pts, render.ColorPlannedNode, widgets.ClassPlanned)
}

// ascentMarkerStubPx is the stub length, in canvas sub-pixels, each
// attitude vector extends from the sprite (issue #348 §3). In the same
// units drawRCSPuffs uses for its own short direction indicators
// (puffStep = 5 sub-pixels) — bumped slightly so the nose/prograde
// divergence during a gravity turn reads as two separated stubs rather
// than overlapping dots at small pitch angles.
const ascentMarkerStubPx = 6.0

// drawAscentAttitudeMarkers plots the nose and prograde vector stubs from
// the active vessel's screen position (ADR 0043 §3 / issue #348): a
// gravity turn is exactly the story of these two directions drifting
// apart and then converging again near MECO, and putting them right on
// the sprite makes that story readable without reading the corner
// navball. Colors reuse the navball's own vocabulary
// (render.ColorNavballMarkerNoseFront / ColorNavballMarkerPrograde) so
// "nose" and "prograde" mean the same hue everywhere in the UI.
func (v *LaunchView) drawAscentAttitudeMarkers(vec sim.AttitudeVectors, anchorWorld orbital.Vec3, scaleMPerPx float64) {
	if scaleMPerPx <= 0 || vec.NoseDir.Norm() == 0 || vec.ProgradeDir.Norm() == 0 {
		return
	}
	step := ascentMarkerStubPx * scaleMPerPx
	v.canvas.PlotDenseLineColored(anchorWorld, anchorWorld.Add(vec.NoseDir.Scale(step)), render.ColorNavballMarkerNoseFront, 1)
	v.canvas.PlotDenseLineColored(anchorWorld, anchorWorld.Add(vec.ProgradeDir.Scale(step)), render.ColorNavballMarkerPrograde, 1)
}

// ascentQBandRows is the number of altitude bands the ATMOSPHERE chip's
// vertical scale divides the atmosphere into — top row is the cutoff
// altitude (the top of the modelled atmosphere), bottom row is the
// ground. Six is enough to place the current-altitude and max-Q marks
// distinctly without making the chip taller than the DESCENT CORRIDOR
// chip it never coexists with.
const ascentQBandRows = 6

// Glyphs for the ATMOSPHERE chip's vertical scale: the vessel's current
// band, the band the peak-Q-so-far was measured in, and a bare tick for
// every other band. Single-cell, no wide/combining runes — the chip's
// padChipBlock right-pads by rune count, and a double-width glyph here
// would throw that off (the same "no %-Ns padding" trap the launch HUD
// strip already documents for byte-vs-rune widths).
const (
	ascentQBandCraftGlyph = "▶"
	ascentQBandMaxQGlyph  = "✕"
	ascentQBandTickGlyph  = "│"
)

// qBandRowIndex maps an altitude within [0, cutoffM] onto one of `rows`
// bands, top row 0 = cutoffM (the atmosphere's outer edge), bottom row
// rows-1 = the ground. Altitudes outside the range clamp to the nearest
// edge — a vessel already well clear of the atmosphere still reads at
// the top row rather than an undefined or out-of-range index.
func qBandRowIndex(altM, cutoffM float64, rows int) int {
	if cutoffM <= 0 || rows <= 0 {
		return 0
	}
	frac := altM / cutoffM
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}
	idx := int((1 - frac) * float64(rows))
	if idx >= rows {
		idx = rows - 1
	}
	if idx < 0 {
		idx = 0
	}
	return idx
}

// ascentQBandLines renders the ATMOSPHERE chip: a vertical scale from the
// atmosphere's outer edge down to the ground, the vessel's current
// position on it, and the altitude of the peak dynamic pressure measured
// so far this session. See sim.AscentQBand's doc comment for why the mark
// is "the peak measured so far" rather than a forecast eventual peak —
// the ballistic-from-now ascent arc has no future thrust program to
// integrate a real forecast from.
func (v *LaunchView) ascentQBandLines(qb sim.AscentQBand) []string {
	curRow := qBandRowIndex(qb.CurrentAltM, qb.AtmosphereDepthM, ascentQBandRows)
	maxRow := -1
	if qb.HasMaxQ {
		maxRow = qBandRowIndex(qb.MaxQAltM, qb.AtmosphereDepthM, ascentQBandRows)
	}
	lines := []string{v.theme.Primary.Render("ATMOSPHERE")}
	for i := 0; i < ascentQBandRows; i++ {
		switch {
		case i == curRow && i == maxRow:
			lines = append(lines, fmt.Sprintf("  %s %s (max Q)", ascentQBandCraftGlyph, formatAltitude(qb.CurrentAltM)))
		case i == curRow:
			lines = append(lines, fmt.Sprintf("  %s %s", ascentQBandCraftGlyph, formatAltitude(qb.CurrentAltM)))
		case i == maxRow:
			lines = append(lines, fmt.Sprintf("  %s %s (max Q)", ascentQBandMaxQGlyph, formatAltitude(qb.MaxQAltM)))
		default:
			lines = append(lines, "  "+ascentQBandTickGlyph)
		}
	}
	lines = append(lines, fmt.Sprintf("  Q:     %.1f kPa", qb.CurrentQPa/1000))
	if qb.HasMaxQ {
		lines = append(lines, fmt.Sprintf("  max Q: %.1f kPa", qb.MaxQPa/1000))
	}
	return lines
}

// (launchOrbitSamples retired by ADR 0042 §3.) The chase-cam used to size
// its own ellipse sample count from the projected orbit circumference,
// because the orbit map's fixed 360 samples scattered at most a handful of
// dots onto the magnified visible arc. That was a second sampling policy
// living beside the map's; the canvas now flattens every ellipse adaptively
// and inks it at a constant on-screen dot spacing, so the magnified view
// fills in from the shared path and drawOrbitPath just passes the map's own
// arguments.

// drawPadMarker plots the launch site as a `+` glyph in ColorAccent
// when the pad is on the camera-facing hemisphere.
func (v *LaunchView) drawPadMarker(w *sim.World, craft *spacecraft.Spacecraft, bodyPos, camFromBody orbital.Vec3) {
	body := craft.Primary
	dir := render.BodyFixedToWorld(body, craft.LaunchLatDeg, craft.LaunchLonDeg, w.Clock.SimTime)
	padFromBody := orbital.Vec3{X: dir.X, Y: dir.Y, Z: dir.Z}.Scale(body.RadiusMeters())
	if !isNearHemisphere(padFromBody, camFromBody) {
		return
	}
	padWorld := bodyPos.Add(padFromBody)
	// Pad accent: ColorPlannedNode (cyan) distinct from trail dim
	// grey + craft yellow + warning amber. Plan called for an
	// unnamed "ColorAccent"; the palette doesn't have one and
	// cyan reads as "neutral reference marker."
	v.canvas.PlotColored(padWorld, render.ColorPlannedNode)
	v.canvas.SetCellOverlay(padWorld, '+')
}

// LUT silhouette dimensions in real-world metres. Stylised at half
// the real LC-39A LUT (~135 m crawler-tower height); chose 60 m
// total so the tower reads tall-but-finite at pad zoom and shrinks
// smoothly as the autoscale zooms out. 9-row sprite has 8 above-base
// rows × 7.5 m = 60 m total height. Width is 2 cols × 4 m = 8 m
// (real LC-39A MLP is ~50 m square but the silhouette is stylised
// narrower). Fixed regardless of zoom — see drawLaunchTower comment.
const (
	lutRowHeightM = 7.5
	lutColWidthM  = 4.0
)

// vesselSubPixelM (v0.11.5-followup) pins the launch-sprite sub-pixel
// stride to a real-world metre value, so the rocket / bell / legs /
// flame shrink on screen as the chase-cam autozoom grows — same
// precedent as the LUT (lutRowHeightM / lutColWidthM, commit b73c54b).
//
// Pre-followup: drawComposedRocket passed `scale` (m/cell) into
// ComposeLaunchSprite as the per-sub-pixel stride. As altitude grew,
// the autozoom grew, so per-sub-pixel metres scaled with the canvas
// — sprite occupied the same canvas area regardless of zoom. The
// rocket "stayed super huge" through ascent. Pinning to 1.5 m/sub-pixel
// makes the Saturn V silhouette (~56 sub-pixel rows) read at ~84 m
// world height — close to the real Saturn V's 110 m — and the
// canvas projects that fixed world height through the autozoom
// (px/m = 1 / scale) so the rocket gets smaller as altitude grows.
const vesselSubPixelM = 1.5

// lutSprite is the v0.11.1 Slice 2 generic mobile-launcher silhouette.
// 2 cells wide; bottom row is the MLP base, top row is the crown
// (swing-arm hint). Row 0 = top, last row = base; each pair is
// (left-column, right-column). A zero rune ('\x00') means "no glyph
// at this cell" — used to draw a sparse outline at the crown row
// (the swing-arm sits in the right column only).
var lutSprite = [][2]rune{
	{'╤', 0},
	{'║', '╤'},
	{'║', '║'},
	{'║', '║'},
	{'║', '║'},
	{'║', '║'},
	{'║', '║'},
	{'╤', '╤'},
	{'█', '█'},
}

// drawLaunchTower stamps the generic mobile-launcher sprite at the
// active craft's launch site. The base row coincides with the pad's
// world position; rows step upward by one terminal-cell of screen
// along the pad's local-up; the second column steps east by one
// terminal-cell. Both axes are body-fixed (independent of the
// chase-cam's hAxis) so the tower's two columns stay geographically
// anchored even as the camera swings during the gravity turn.
//
// World-units-per-screen-cell: `scaleMPerPx` here is the live
// `renderScene` scalar (output of launchAutoScale / LaunchZoom; named
// m/cell in the plan but functionally m/px because renderScene passes
// `1/scale` straight into Canvas.SetScale which expects px/m). Each
// terminal cell is `canvasCellPxH` × `canvasCellPxW` braille pixels,
// so the per-cell world stride is `scaleMPerPx · canvasCellPx{H,W}`.
// Slice-2-as-shipped omitted the pixel-to-cell correction and the
// 9-row sprite collapsed into ~2 screen cells at altitude > a few m.
//
// Each glyph cell is depth-culled by the same isNearHemisphere check
// the pad marker uses, so when the body rotates the launch site to
// the far hemisphere the tower vanishes.
func (v *LaunchView) drawLaunchTower(w *sim.World, craft *spacecraft.Spacecraft, bodyPos, camFromBody orbital.Vec3, scaleMPerPx float64) {
	body := craft.Primary
	dir := render.BodyFixedToWorld(body, craft.LaunchLatDeg, craft.LaunchLonDeg, w.Clock.SimTime)
	padFromBody := orbital.Vec3{X: dir.X, Y: dir.Y, Z: dir.Z}.Scale(body.RadiusMeters())

	padUp := orbital.Vec3{X: dir.X, Y: dir.Y, Z: dir.Z}
	east := render.BodyFrameEast(body, render.Vec3{X: padFromBody.X, Y: padFromBody.Y, Z: padFromBody.Z})
	padEast := orbital.Vec3{X: east.X, Y: east.Y, Z: east.Z}

	// Per-cell stride is FIXED real-world metres, not zoom-scaled.
	// The original v0.11.1 cut used `scaleMPerPx · canvasCellPx{H,W}`,
	// which meant the LUT's world height grew with the chase-cam
	// autozoom: as the rocket gained altitude, scaleMPerPx grew
	// proportionally to altitude, so LUT-top-altitude = (4/3) ×
	// rocket-altitude — the rocket could never clear the LUT.
	// Fixing per-row stride to a real-world value (stylised at
	// ~7 m/row → ~60 m total tower height, half the real LC-39A
	// LUT) means the LUT shrinks on screen as the autozoom grows,
	// which is the correct perspective behaviour. _ = scaleMPerPx
	// retains the parameter signature for caller compatibility.
	_ = scaleMPerPx
	cellWorldY := lutRowHeightM
	cellWorldX := lutColWidthM

	rows := len(lutSprite)
	for r := 0; r < rows; r++ {
		rowAbove := float64(rows - 1 - r) // base at row 0 world height
		for col := 0; col < 2; col++ {
			glyph := lutSprite[r][col]
			if glyph == 0 {
				continue
			}
			cellFromBody := padFromBody.
				Add(padUp.Scale(rowAbove * cellWorldY)).
				Add(padEast.Scale(float64(col) * cellWorldX))
			if !isNearHemisphere(cellFromBody, camFromBody) {
				continue
			}
			cellWorld := bodyPos.Add(cellFromBody)
			v.canvas.PlotColored(cellWorld, render.ColorDim)
			v.canvas.SetCellOverlay(cellWorld, glyph)
		}
	}
}

// Canvas cell pixel dimensions — mirrors widgets.Canvas.Resize, which
// allocates `cols*2 × rows*4` braille pixels. Local copies so the
// screen layer doesn't reach into widgets for constants that don't
// shift release-to-release. If widgets changes its braille mapping,
// this constant changes with it.
const (
	canvasCellPxW = 2
	canvasCellPxH = 4
)

// drawSOICraft renders every craft in the active craft's SOI other
// than the active itself, so dropped stages (passive Spacecraft spawned
// on decouple, v0.9.1+) and sister vessels become visible during the
// launch session. Filter is `c.Primary == active.Primary`; depth-cull
// via the same near-hemisphere check the pad marker / tower use; canvas
// bounds handle the off-frame case via Project's ok=false return inside
// SetCellOverlay. No age or distance cull (Slice 2 grill resolution).
//
// v0.11.3 Slice 4: dropped stages render via the same composed-sprite
// path as the active vessel (now single-stage stacks) and inherit their
// CurrentAttitudeDir from the parent at decouple time. Falls back to
// the single-glyph render for crafts with no LaunchSprite.
func (v *LaunchView) drawSOICraft(w *sim.World, active *spacecraft.Spacecraft, bodyPos, camFromBody orbital.Vec3, basis widgets.Basis, scaleMPerPx float64) {
	for _, c := range w.Crafts {
		if c == nil || c == active {
			continue
		}
		// Bodies compare by value; the loaded catalog round-trips
		// pointer-equal copies, so a simple field comparison suffices.
		if c.Primary.ID != active.Primary.ID {
			continue
		}
		fromBody := c.State.R // primary-relative (same frame as camFromBody)
		if !isNearHemisphere(fromBody, camFromBody) {
			continue
		}
		cellWorld := bodyPos.Add(fromBody)
		if v.drawComposedRocket(c, cellWorld, basis, scaleMPerPx) {
			continue
		}
		v.canvas.PlotColored(cellWorld, render.ColorDim)
		glyph := '·'
		for _, r := range c.Glyph {
			glyph = r
			break
		}
		v.canvas.SetCellOverlay(cellWorld, glyph)
	}
}

// drawTrail re-projects each TrailPoint via BodyFixedToWorld at the
// CURRENT sim-time so the trail visibly rotates with the body. Same
// near-hemisphere depth check as the pad marker.
func (v *LaunchView) drawTrail(w *sim.World, body bodies.CelestialBody, bodyPos, camFromBody orbital.Vec3) {
	radius := body.RadiusMeters()
	for _, p := range w.LaunchTrail {
		dir := render.BodyFixedToWorld(body, p.LatDeg, p.LonDeg, w.Clock.SimTime)
		rMag := radius + p.AltM
		ptFromBody := orbital.Vec3{X: dir.X, Y: dir.Y, Z: dir.Z}.Scale(rMag)
		if !isNearHemisphere(ptFromBody, camFromBody) {
			continue
		}
		v.canvas.PlotColored(bodyPos.Add(ptFromBody), render.ColorDim)
	}
}

// composeHUDLine assembles the launch-readout strip from the current
// world / craft state. Returns an empty string when there's no active
// craft (the overlay no-ops, leaving the canvas's bottom row alone).
func (v *LaunchView) composeHUDLine(w *sim.World, c *spacecraft.Spacecraft) string {
	if c == nil {
		return ""
	}
	tPlus := time.Duration(0)
	if w.LaunchSessionActive && !w.LaunchT0.IsZero() {
		tPlus = w.Clock.SimTime.Sub(w.LaunchT0)
	}
	vZ := v.sampleVerticalSpeed(c, w.Clock.SimTime)
	downrange := greatCircleDistanceM(c.Primary, c.LaunchLatDeg, c.LaunchLonDeg, c, w.Clock.SimTime)
	q := dynamicPressurePa(c)
	return formatLaunchHUD(tPlus, vZ, downrange, q, w.LaunchMaxQ)
}

// sampleVerticalSpeed returns a finite-difference altitude rate (m/s)
// for the active craft. Re-baselined on craft change so a vessel
// switch doesn't bleed a stale altitude into the readout. The first
// call after a re-baseline returns 0 m/s.
func (v *LaunchView) sampleVerticalSpeed(c *spacecraft.Spacecraft, simTime time.Time) float64 {
	alt := c.Altitude()
	if v.vzCraft != c || v.vzAtSim.IsZero() {
		v.vzCraft = c
		v.vzAltM = alt
		v.vzAtSim = simTime
		return 0
	}
	dt := simTime.Sub(v.vzAtSim).Seconds()
	if dt <= 0 {
		return 0
	}
	dv := (alt - v.vzAltM) / dt
	v.vzAltM = alt
	v.vzAtSim = simTime
	return dv
}

// greatCircleDistanceM returns the great-circle distance over the
// body's surface from the launch site (lat0, lon0) to the craft's
// current sub-craft point. Requires `simTime` because WorldToBodyFixed
// is rotation-phase-aware: passing the zero-value time computes the
// sub-craft point at year 0001, which puts the rotation phase
// arbitrarily far from the real value (verification surfaced a
// ~4600 km phantom downrange from the J2000 epoch mismatch). Returns
// 0 when the craft has no valid sub-craft direction.
func greatCircleDistanceM(body bodies.CelestialBody, lat0Deg, lon0Deg float64, c *spacecraft.Spacecraft, simTime time.Time) float64 {
	if c == nil {
		return 0
	}
	r := c.State.R
	rNorm := r.Norm()
	if rNorm == 0 {
		return 0
	}
	rUnit := r.Scale(1.0 / rNorm)
	latDeg, lonDeg := render.WorldToBodyFixed(body, render.Vec3{X: rUnit.X, Y: rUnit.Y, Z: rUnit.Z}, simTime)
	lat0 := lat0Deg * math.Pi / 180.0
	lat1 := latDeg * math.Pi / 180.0
	dLon := (lonDeg - lon0Deg) * math.Pi / 180.0
	a := math.Sin((lat1-lat0)/2)*math.Sin((lat1-lat0)/2) +
		math.Cos(lat0)*math.Cos(lat1)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c2 := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return body.RadiusMeters() * c2
}

// dynamicPressurePa returns 0.5·ρ·|v_rel|² for the active craft using
// the body's atmosphere and the craft's air-relative velocity (same
// v_rel = v − ω × r the drag integrator uses, so a launchpad-co-
// rotating craft reads Q = 0 not the inertial-speed phantom). Returns
// 0 above the atmosphere cutoff or when the body has no atmosphere.
func dynamicPressurePa(c *spacecraft.Spacecraft) float64 {
	if c == nil || c.Primary.Atmosphere == nil {
		return 0
	}
	alt := c.Altitude()
	atm := c.Primary.Atmosphere
	if alt < 0 || alt > atm.CutoffAltitude {
		return 0
	}
	rho := atm.SurfaceDensity * math.Exp(-alt/atm.ScaleHeight)
	vRel := c.State.V.Sub(physics.AtmosphereOmega(c.Primary).Cross(c.State.R))
	vMag := vRel.Norm()
	return 0.5 * rho * vMag * vMag
}

// overlayHUDStrip replaces the final braille line of the canvas
// string with the HUD strip, preserving the rendered height. Compares
// RUNE counts (display widths), not byte lengths — a braille glyph is
// 3 UTF-8 bytes but one display cell, and padding by byte-length
// inflated the row by ~280 chars at canvas-width 140 (slice-1.7
// playtest bug; lipgloss then padded every other row to match,
// stretching the bordered panel to ~4× its intended width and
// pushing the side HUD off the right of the terminal).
func overlayHUDStrip(canvasStr, hud string) string {
	if hud == "" {
		return canvasStr
	}
	idx := strings.LastIndex(canvasStr, "\n")
	if idx < 0 {
		return hud
	}
	// displayWidth strips ANSI escape sequences before counting runes —
	// a fully-coloured canvas row is ~3000 raw chars but ~140 visible
	// cells, and the v0.11 slice-1.7 launch render's HUD strip was
	// getting padded to the inflated raw width, pushing every joined
	// row off the right of the terminal.
	tailWidth := displayWidth(canvasStr[idx+1:])
	hudRunes := []rune(hud)
	if len(hudRunes) < tailWidth {
		hud = hud + strings.Repeat(" ", tailWidth-len(hudRunes))
	} else if len(hudRunes) > tailWidth {
		hud = string(hudRunes[:tailWidth])
	}
	return canvasStr[:idx+1] + hud
}
