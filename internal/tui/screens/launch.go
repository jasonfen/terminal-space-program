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

// Resize sizes the canvas to leave the right ~30% for the HUD column
// (mirrors OrbitView's split so the two screens line up when
// cycling). Reserve 4 rows for title + footer + border.
func (v *LaunchView) Resize(totalCols, totalRows int) {
	canvasCols := totalCols * 7 / 10
	if canvasCols < 20 {
		canvasCols = 20
	}
	v.canvas.Resize(canvasCols, totalRows-4)
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
	footer := v.theme.Footer.Render(
		"[?]help [esc]menu [+/-]zoom [v]cycle-view [.,]warp [0]pause",
	)

	if craft != nil && craft.Primary.MeanRadius > 0 {
		v.renderScene(w, craft)
	}

	canvasStr := v.canvas.String()
	canvasStr = overlayHUDStrip(canvasStr, v.composeHUDLine(w, craft))

	// Manual rounded-border wrapping. lipgloss.Border().Render() over
	// a string with embedded per-cell ANSI escapes (the case here —
	// FillProjectedSphere tags thousands of cells with SurfaceColor)
	// miscounts visible width and inflates each row ~22×, pushing
	// the side HUD off the right of the terminal. Manual borders
	// give us exact control: use lipgloss.Width per line for the
	// pad math, which strips ANSI before measuring.
	canvasPanel := wrapBorder(canvasStr, v.canvas.Cols(), v.theme.Primary.GetForeground())

	// Side HUD: reuse OrbitView's column so the launch-relevant
	// readouts (altitude, stage fuel, v_vert, v_horiz, fpa, twr) are
	// the same blocks the player already knows.
	body := canvasPanel
	if v.hudSource != nil {
		hudWidth := totalCols - v.canvas.Cols() - 4
		if hudWidth < 20 {
			hudWidth = 20
		}
		hud := v.hudSource.RenderHUDColumn(w, 0, hudWidth)
		body = joinHorizontalLines(canvasPanel, hud, "  ")
	}

	return title + "\n" + body + "\n" + footer
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
func (v *LaunchView) renderScene(w *sim.World, craft *spacecraft.Spacecraft) {
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

	// Pad marker at the active craft's launch site, depth-culled.
	v.drawPadMarker(w, craft, bodyCentre, camFromBody)

	// Breadcrumb trail: each TrailPoint re-projected via
	// BodyFixedToWorld so the trace rotates with the body.
	v.drawTrail(w, body, bodyCentre, camFromBody)

	// Active vessel at the camera centre — Slice 3 swaps for the
	// composed-from-stages sprite; Slice 1 reuses the existing glyph.
	glyph := '+'
	if craft.Glyph != "" {
		for _, r := range craft.Glyph {
			glyph = r
			break
		}
	}
	v.canvas.SetCellOverlay(camWorld, glyph)
}

// chaseHorizontalAxis computes the projection-plane horizontal axis:
// the commanded (CurrentAttitudeDir) projection onto the local-
// horizontal plane when its magnitude is well-defined, falling back
// to surface-frame east at the craft's surface point when the
// attitude is near-vertical (rocket on the pad / just after liftoff).
func chaseHorizontalAxis(c *spacecraft.Spacecraft, body bodies.CelestialBody, camFromBody, localUp orbital.Vec3) orbital.Vec3 {
	cmd := c.CurrentAttitudeDir
	if cmd.Norm() > 0 {
		horiz := cmd.Sub(localUp.Scale(cmd.Dot(localUp)))
		if n := horiz.Norm(); n > 1e-9 {
			return horiz.Scale(1.0 / n)
		}
	}
	east := render.BodyFrameEast(body, render.Vec3{X: camFromBody.X, Y: camFromBody.Y, Z: camFromBody.Z})
	return orbital.Vec3{X: east.X, Y: east.Y, Z: east.Z}
}

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

