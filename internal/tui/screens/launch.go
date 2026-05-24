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
)

// LaunchView (v0.11.0+) is the ViewLaunch chase-cam screen — a sibling
// of OrbitView, not an extension. ADR-0002 covers the call. Minimal
// chrome: title, canvas with the chase-cam scene, footer with key
// hints; no sidepanel (the orbit screen's body list / target panel
// are irrelevant during a launch). The HUD launch-readout strip is
// overlaid on the bottom braille row of the canvas (same precedent as
// the orbit screen's status overlay).
type LaunchView struct {
	canvas *widgets.Canvas
	theme  Theme

	// lastVZSample caches the previous tick's altitude + sim-time so
	// the HUD can compute v_z (m/s) as a finite difference rather than
	// requiring a sim-side velocity decomposition. Re-keyed on active-
	// craft change so a vessel switch can't bleed a stale baseline.
	vzCraft *spacecraft.Spacecraft
	vzAltM  float64
	vzAtSim time.Time
}

func NewLaunchView(th Theme) *LaunchView {
	return &LaunchView{
		canvas: widgets.NewCanvas(80, 24),
		theme:  th,
	}
}

// Resize forwards terminal dimensions to the canvas. No sidepanel,
// so the canvas takes the full width minus the rounded border.
// Reserve 4 rows for title + footer + border.
func (v *LaunchView) Resize(totalCols, totalRows int) {
	cols := totalCols - 2
	rows := totalRows - 4
	if cols < 20 {
		cols = 20
	}
	if rows < 10 {
		rows = 10
	}
	v.canvas.Resize(cols, rows)
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

	canvasPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.theme.Primary.GetForeground()).
		Render(canvasStr)

	return title + "\n" + canvasPanel + "\n" + footer
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
	bodyPos := w.BodyPosition(body)
	camWorld := craft.State.R
	camFromBody := camWorld.Sub(bodyPos)
	camDist := camFromBody.Norm()
	if camDist <= 0 {
		return
	}
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
	v.canvas.SetScale(1.0 / scale) // canvas takes pixels-per-metre

	v.canvas.Center(camWorld)

	// Horizon curve + SurfaceColor flood-fill below.
	v.drawHorizonAndFill(body, bodyPos, camFromBody, hAxis, localUp, camDist)

	// Pad marker at the active craft's launch site, depth-culled.
	v.drawPadMarker(w, craft, bodyPos, camFromBody)

	// Breadcrumb trail: each TrailPoint re-projected via
	// BodyFixedToWorld so the trace rotates with the body.
	v.drawTrail(w, body, bodyPos, camFromBody)

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

// drawHorizonAndFill samples N points on the silhouette curve and
// plots them as a faint line; the body's SurfaceColor floods the
// region below the curve. Slice 1 ships a deterministic per-column
// fill from the lowest horizon sample down to the canvas bottom —
// not a true scanline polygon fill, but visually equivalent for
// silhouettes that span the canvas width (the common case).
func (v *LaunchView) drawHorizonAndFill(body bodies.CelestialBody, bodyPos, camFromBody, hAxis, localUp orbital.Vec3, camDist float64) {
	radius := body.RadiusMeters()
	pts := render.HorizonCurve(radius, camDist, 96)
	if pts == nil {
		return
	}
	surfaceColor := lipgloss.Color(body.SurfaceColorHex())
	camWorld := bodyPos.Add(camFromBody)
	for _, p := range pts {
		w := camWorld.Add(hAxis.Scale(p.X)).Add(localUp.Scale(p.Y))
		v.canvas.PlotColored(w, surfaceColor)
	}
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
	downrange := greatCircleDistanceM(c.Primary, c.LaunchLatDeg, c.LaunchLonDeg, c)
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
// body's surface from (lat0, lon0) to the craft's current sub-craft
// point. Returns 0 when the craft has no valid sub-craft direction.
func greatCircleDistanceM(body bodies.CelestialBody, lat0Deg, lon0Deg float64, c *spacecraft.Spacecraft) float64 {
	if c == nil {
		return 0
	}
	r := c.State.R
	if r.Norm() == 0 {
		return 0
	}
	// Convert craft's primary-relative position to body-fixed (lat, lon)
	// via the WorldToBodyFixed inverse. Use the unit direction since
	// magnitude is altitude, not part of the geographic projection.
	rUnit := r.Scale(1.0 / r.Norm())
	latDeg, lonDeg := render.WorldToBodyFixed(body, render.Vec3{X: rUnit.X, Y: rUnit.Y, Z: rUnit.Z}, time.Time{})
	// Note: the launch-site (lat0, lon0) is body-fixed and so is the
	// sub-craft point we just recovered; the simTime drops out of a
	// pure (lat, lon) → (lat, lon) great-circle distance, so passing
	// the zero-value time above is fine.
	lat0 := lat0Deg * math.Pi / 180.0
	lat1 := latDeg * math.Pi / 180.0
	dLon := (lonDeg - lon0Deg) * math.Pi / 180.0
	a := math.Sin((lat1-lat0)/2)*math.Sin((lat1-lat0)/2) +
		math.Cos(lat0)*math.Cos(lat1)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c2 := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return body.RadiusMeters() * c2
}

// dynamicPressurePa returns 0.5·ρ·|v_rel|² for the active craft using
// the body's atmosphere and the craft's air-relative velocity. Returns
// 0 above the atmosphere cutoff (or when the body has no atmosphere).
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
	v := c.State.V.Norm() // Slice 1 uses inertial speed as a proxy
	return 0.5 * rho * v * v
}

// overlayHUDStrip replaces the final braille line of the canvas
// string with the HUD strip, preserving the rendered height. Returns
// canvasStr unchanged when hud is empty.
func overlayHUDStrip(canvasStr, hud string) string {
	if hud == "" {
		return canvasStr
	}
	idx := strings.LastIndex(canvasStr, "\n")
	if idx < 0 {
		return hud
	}
	tail := canvasStr[idx+1:]
	if len(hud) < len(tail) {
		hud = hud + strings.Repeat(" ", len(tail)-len(hud))
	} else if len(hud) > len(tail) {
		hud = hud[:len(tail)]
	}
	return canvasStr[:idx+1] + hud
}

