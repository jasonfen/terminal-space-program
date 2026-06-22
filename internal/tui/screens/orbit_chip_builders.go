package screens

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// This file holds the chip builders transplanted from renderHUD's
// per-block code (ADR 0010 / v0.13 slice 2). Each returns the chip's
// styled lines (a bare colored header + rows) or nil when the block isn't
// contextually relevant — the "relevant" half of the render rule. The old
// section() divider is dropped: a chip's header doubles as its label.
// Arithmetic and labels mirror the originals so the readouts are
// unchanged; only the placement (canvas corner vs. tall column) differs.

// assembleChips gathers every relevant + enabled Chip for the current
// world state, in composite order. Top-left holds the pinned VESSEL core
// plus the phase-transient stack; top-right holds Orbit metrics with the
// Target readout stacked beneath it; Stages is bottom-left and Nodes is
// bottom-right (above the navball). Declutter is honoured inside
// chipEnabled, so a decluttered frame returns no chips.
func (v *OrbitView) assembleChips(w *sim.World) []builtChip {
	var chips []builtChip
	// Pinned core telemetry — top of the top-left stack. Unlike every
	// other chip it is always rendered: never settings-toggled (core
	// telemetry is fixed, ADR 0010) and never hidden by declutter — F2
	// must not be able to hide fuel/Δv mid-burn. v0.13 playtest move:
	// VESSEL/PROPELLANT left the right-hand column to live on the canvas.
	if lines := v.buildVesselChip(w); lines != nil {
		chips = append(chips, builtChip{corner: cornerTopLeft, lines: lines})
	}
	add := func(id settings.Chip, corner chipCorner, lines []string) {
		if lines == nil || !v.chipEnabled(id) {
			return
		}
		chips = append(chips, builtChip{id: id, corner: corner, lines: lines})
	}
	// The current goal sits directly under the pinned VESSEL chip — "who I am"
	// then "what I'm doing" in the top-left status corner (ADR 0025 / Slice 5).
	add(settings.ChipMissions, cornerTopLeft, v.buildMissionsChip(w))
	// Top-left transient stack (stacking order = listed order, downward).
	// The in-flight ● BURNS readout used to live here; v0.16 folds it into
	// the bottom-right NODES chip (a live burn is the firing head of the
	// burn schedule). See the force-show path below.
	add(settings.ChipFrameTransition, cornerTopLeft, v.buildFrameTransitionChip(w))
	add(settings.ChipCapture, cornerTopLeft, v.buildCaptureChip(w))
	add(settings.ChipLaunch, cornerTopLeft, v.buildLaunchChip(w))
	add(settings.ChipDescent, cornerTopLeft, v.buildDescentChip(w))
	add(settings.ChipChute, cornerTopLeft, v.buildChuteChip(w))
	add(settings.ChipAttitude, cornerTopLeft, v.buildAttitudeChip(w))
	// Top-right stack: Orbit metrics on top, the Target readout beneath it
	// (append order = top-to-bottom). Orbit metrics is always-on (empty id):
	// the current orbit (apo/peri/incl) is never user-hideable from the
	// Settings screen, mirroring the always-on ● BURNS readout — both are
	// too load-bearing to toggle off. F2 declutter still clears them.
	add("", cornerTopRight, v.buildOrbitMetricsChip(w))
	// PROJECTED ORBIT sits to the LEFT of the always-on ORBIT readout (issue
	// #63 follow-up) so current + projected show together during a burn
	// without growing the top-right column's height — leaving vertical room
	// for TARGET to clear the bottom-right NODES chip. Toggleable, unlike the
	// load-bearing live ORBIT beside it. leftOfPrev falls back to normal
	// stacking when ORBIT is suppressed (e.g. ascent), so it's never orphaned.
	if lines := v.buildProjectedOrbitChip(w); lines != nil && v.chipEnabled(settings.ChipProjectedOrbit) {
		chips = append(chips, builtChip{id: settings.ChipProjectedOrbit, corner: cornerTopRight, lines: lines, leftOfPrev: true})
	}
	add(settings.ChipTarget, cornerTopRight, v.buildTargetChip(w))
	// SOI PASS sits beneath TARGET — the upcoming encounter of the live
	// path, always-on and Target-independent (ADR 0019). De-dupes with
	// TARGET inside the builder when they name the same body.
	add(settings.ChipSOIPass, cornerTopRight, v.buildSOIPassChip(w))
	// Remaining fixed corners.
	add(settings.ChipStages, cornerBottomLeft, v.buildStagesChip(w))
	// NODES (bottom-right) now also carries any in-flight burn as its
	// firing head (v0.16). A live burn is safety-critical, so when one is
	// in flight the chip force-shows — bypassing both the ChipNodes
	// Settings toggle and F2 declutter — so it can never be hidden. With
	// nothing burning it honours the toggle + declutter like any chip.
	if lines := v.buildNodesChip(w); lines != nil {
		if v.anyActiveBurn(w) || v.chipEnabled(settings.ChipNodes) {
			chips = append(chips, builtChip{id: settings.ChipNodes, corner: cornerBottomRight, lines: lines})
		}
	}
	return chips
}

// anyActiveBurn reports whether any craft in the slate has an in-flight
// finite/planted burn (ActiveBurn). Drives the NODES chip force-show
// (v0.16) so a live burn is never hidden by the toggle or declutter.
func (v *OrbitView) anyActiveBurn(w *sim.World) bool {
	for _, c := range w.Crafts {
		if c != nil && c.ActiveBurn != nil {
			return true
		}
	}
	return false
}

// buildMissionsChip is the in-flight mission checklist chip (ADR 0025
// §"Player surface" / Slice 5). A one-liner: the active mission's name plus
// its current objective and N/M progress, so the player always sees "what
// now" without opening the missions screen (which carries the full ladder +
// hint text). On a mission failure it flashes "✗ <name> failed: <reason>" for
// a few wall-clock seconds (World.MissionFailFlash) before advancing to the
// next mission. Returns nil when no mission is active and nothing is flashing.
// Honours the Settings toggle + F2 declutter like any chip (no force-show —
// a failed mission isn't safety-critical the way a live burn is).
func (v *OrbitView) buildMissionsChip(w *sim.World) []string {
	flash, flashing := w.MissionFailFlash()
	return v.missionChipLines(flash, flashing, w.ActiveMission())
}

// missionChipLines is the pure content selector behind buildMissionsChip,
// split out so both the (World-armed) failure flash and the active-mission
// forms are unit-testable without a live World. A live flash takes precedence
// over the active mission — the mission that just failed is more urgent to
// surface than the next one in the ladder.
func (v *OrbitView) missionChipLines(flash string, flashing bool, m *missions.Mission) []string {
	if flashing {
		return []string{
			v.theme.Alert.Render("MISSION"),
			v.theme.Alert.Render("  ✗ " + flash),
		}
	}
	if m == nil {
		return nil
	}
	header := v.theme.Primary.Render("MISSION") + "  " + m.Name
	obj, ok := m.CurrentObjective()
	if !ok {
		// An InProgress mission always has a current (non-Passed) objective;
		// defend against an empty/all-passed mission slipping through anyway.
		return []string{header}
	}
	passed, total := m.Progress()
	return []string{
		header,
		fmt.Sprintf("  %s %s  %d/%d", hudNodeMarker, obj.Label(), passed, total),
	}
}

// buildAttitudeChip surfaces the held attitude / nav mode / engine mode /
// manual-burn state. Always relevant for a visible craft (the old block
// dropped the hold row during ascent to save column height; a corner chip
// doesn't compete for that height, so it shows the full set).
func (v *OrbitView) buildAttitudeChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !w.CraftVisibleHere() {
		return nil
	}
	manualState := "idle"
	if c.ManualBurn != nil {
		elapsed := w.Clock.SimTime.Sub(c.ManualBurn.StartTime).Seconds()
		manualState = fmt.Sprintf(v.theme.Warning.Render("● firing T+%.1fs"), elapsed)
	}
	return []string{
		v.theme.Primary.Render("ATTITUDE"),
		fmt.Sprintf("  nav:     %s", w.NavMode),
		fmt.Sprintf("  hold:    %s", c.AttitudeMode.String()),
		fmt.Sprintf("  engine:  %s", c.EngineMode.String()),
		fmt.Sprintf("  manual:  %s", manualState),
	}
}

// activeBurnLines renders the in-flight burn entries across the whole
// craft slate — mode, Δv remaining, and a T-countdown (or a STALLED
// warning) — each as a ● firing line in the warning style so it reads as
// safety-critical. Returns nil when nothing is burning. v0.16 folds this
// into buildNodesChip as the firing head of the burn schedule (it was the
// standalone ● BURNS chip); walking all crafts still means a burn on a
// non-active vessel can't sneak by.
func (v *OrbitView) activeBurnLines(w *sim.World) []string {
	var lines []string
	for i, c := range w.Crafts {
		if c == nil || c.ActiveBurn == nil {
			continue
		}
		ab := c.ActiveBurn
		remaining := ab.EndTime.Sub(w.Clock.SimTime).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		tag := fmt.Sprintf("craft %d", i+1)
		if i == w.ActiveCraftIdx {
			tag += " (active)"
		}
		if c.BurnStalled() {
			lines = append(lines,
				v.theme.Warning.Render(fmt.Sprintf("  ● %s — %s, Δv %.0f m/s", tag, ab.Mode.String(), ab.DVRemaining)),
				v.theme.Warning.Render("    ⚠ STALLED — stage to resume (x to cancel)"),
			)
		} else {
			lines = append(lines,
				v.theme.Warning.Render(fmt.Sprintf("  ● %s — %s, Δv %.0f m/s, T-%.0fs",
					tag, ab.Mode.String(), ab.DVRemaining, remaining)),
			)
		}
	}
	return lines
}

// buildFrameTransitionChip surfaces the next SOI / frame transition implied
// by the planted-node chain. Returns nil when none is queued.
func (v *OrbitView) buildFrameTransitionChip(w *sim.World) []string {
	ft, ok := w.NextFrameTransition()
	if !ok {
		return nil
	}
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
	return []string{
		v.theme.Primary.Render("FRAME TRANSITION"),
		fmt.Sprintf("  %s → %s", fromName, v.theme.Warning.Render(toName)),
		fmt.Sprintf("  at %s  (node #%d)", when, ft.NodeIndex+1),
	}
}

// buildCaptureChip surfaces the post-capture orbit at the last frame-
// changing planted node so the player catches retrograde-capture gotchas
// before firing. Returns nil when no arrival preview is available.
func (v *OrbitView) buildCaptureChip(w *sim.World) []string {
	cap, ok := w.ArrivalCapturePreview()
	if !ok {
		return nil
	}
	lines := []string{
		v.theme.Primary.Render("CAPTURE PREVIEW"),
		fmt.Sprintf("  primary:    %s", cap.Primary.EnglishName),
	}
	if cap.Approximate {
		dirLabel := v.theme.Warning.Render("prograde")
		if cap.RetrogradeCapture {
			dirLabel = v.theme.Alert.Render("retrograde")
		}
		lines = append(lines,
			fmt.Sprintf("  approach:   %.0f m/s relative", cap.ApproachSpeed),
			fmt.Sprintf("  direction:  %s capture predicted", dirLabel),
			v.theme.Dim.Render("  (intercept too central for orbit-element preview)"),
		)
		return lines
	}
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
			fmt.Sprintf("  Ap:         %.0f km alt", (cap.ApoapsisM-primaryR)/1000),
			fmt.Sprintf("  Pe:         %.0f km alt", (cap.PeriapsisM-primaryR)/1000),
		)
	}
	return lines
}

// buildLaunchChip is the ascent instrument cluster (altitude / vertical &
// horizontal velocity / flight-path angle / TWR / SAS / trim plus the live
// ap/pe/Δv→circ prediction). Returns nil when the craft isn't ascending.
// Transplanted verbatim from renderHUD's LAUNCH block; the ascent-trend
// cache (v.ascentTrend*) is mutated here exactly as before.
func (v *OrbitView) buildLaunchChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !shouldShowLaunchHUD(c) {
		return nil
	}
	omegaRender := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
	vRel := c.State.V.Sub(omega.Cross(c.State.R))
	rNorm := c.State.R.Norm()
	var vVert, vHoriz, fpaDeg, fpaOrbitDeg float64
	hasFPA := false
	hasFPAOrbit := false
	if rNorm > 0 {
		rHat := c.State.R.Scale(1 / rNorm)
		vVert = vRel.X*rHat.X + vRel.Y*rHat.Y + vRel.Z*rHat.Z
		vHorizVec := vRel.Sub(rHat.Scale(vVert))
		vHoriz = vHorizVec.Norm()
		if vRel.Norm() > 1.0 {
			fpaDeg = math.Atan2(vVert, vHoriz) * 180 / math.Pi
			hasFPA = true
		}
		vOrbit := c.State.V
		if vOrbit.Norm() > 1.0 {
			vVertOrbit := vOrbit.X*rHat.X + vOrbit.Y*rHat.Y + vOrbit.Z*rHat.Z
			vHorizOrbit := vOrbit.Sub(rHat.Scale(vVertOrbit)).Norm()
			fpaOrbitDeg = math.Atan2(vVertOrbit, vHorizOrbit) * 180 / math.Pi
			hasFPAOrbit = true
		}
	}
	twrLabel := "—"
	if c.Thrust > 0 && c.TotalMass() > 0 {
		gSurface := c.Primary.GravitationalParameter() / (c.Primary.RadiusMeters() * c.Primary.RadiusMeters())
		twr := c.Thrust * c.EffectiveThrottle() / (c.TotalMass() * gSurface)
		twrLabel = fmt.Sprintf("%.2f", twr)
		if twr < 1.0 {
			twrLabel = v.theme.Alert.Render(twrLabel + " (will not lift)")
		}
	}
	altAGL := c.Altitude()
	altLabel := fmt.Sprintf("%.0f m", nzero(altAGL, 0))
	if altAGL >= 1000 {
		altLabel = fmt.Sprintf("%.2f km", altAGL/1000)
	}
	sasLabel := c.AttitudeMode.String()
	trimDeg := c.PitchTrim * 180 / math.Pi
	trimLabel := fmt.Sprintf("%+.1f°", nzero(trimDeg, 1))
	if math.Abs(trimDeg) > 0.05 {
		trimLabel = v.theme.Warning.Render(trimLabel)
	}
	fpaLabel := "—"
	if hasFPA {
		fpaLabel = fmt.Sprintf("%.0f° (90 = up, 0 = horiz)", nzero(fpaDeg, 0))
	}
	fpaOrbitLabel := "—"
	if hasFPAOrbit {
		fpaOrbitLabel = fmt.Sprintf("%.0f° (inertial)", nzero(fpaOrbitDeg, 0))
	}
	lines := []string{
		v.theme.Primary.Render("SURFACE"),
		fmt.Sprintf("  altitude:   %s", altLabel),
		fmt.Sprintf("  v_vert:     %.1f m/s", nzero(vVert, 1)),
		fmt.Sprintf("  v_horiz:    %.0f m/s (surface-rel)", vHoriz),
		fmt.Sprintf("  fpa:        %s", fpaLabel),
		fmt.Sprintf("  fpa_orbit:  %s", fpaOrbitLabel),
		fmt.Sprintf("  twr:        %s", twrLabel),
		fmt.Sprintf("  sas:        %s", sasLabel),
		fmt.Sprintf("  trim:       %s", trimLabel),
	}
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
	inclLabel := "—"
	inclRowLabel := "incl.:      "
	if !math.IsNaN(el.I) && !math.IsInf(el.I, 0) {
		inclLabel = fmt.Sprintf("%.2f°", el.I*180/math.Pi)
	}
	if c.Landed {
		inclRowLabel = "launch lat: "
		inclLabel = fmt.Sprintf("%.1f° (locked)", c.LaunchLatDeg)
	}
	apLabel, peLabel, ttaLabel, dvCircLabel, tBurnLabel := "—", "—", "—", "—", "—"
	trendLabel := ""
	var dvCirc float64
	// While Landed the craft sits at the apoapsis of its co-rotation
	// pseudo-orbit (apoapsis ≈ the launch radius), so apoAlt and rApo hover
	// at exactly zero and the apoAlt>0 / rApo>primaryR gates flip on
	// numerical noise tick-to-tick — flashing ap / t_to_apo / Δv→circ
	// between a value and "—". The pad pseudo-orbit isn't a real orbit, so
	// suppress these predictions until the craft actually lifts off; the
	// pad cares about TWR / launch-lat / SAS, which render regardless.
	if apoFinite && !c.Landed {
		apLabel = formatAltKm(apoAlt)
		peLabel = formatAltKm(periAlt)
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
		if apoAlt > 0 {
			ttaSec := orbital.TimeToApoapsis(orbital.Vec3State{R: c.State.R, V: c.State.V}, mu)
			if ttaSec > 0 {
				ttaLabel = formatDurationShort(ttaSec)
			}
		}
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
		v.ascentTrendCraft = nil
	}
	if dvCirc > 0 && c.Thrust > 0 && c.TotalMass() > 0 {
		thrust := c.Thrust * c.EffectiveThrottle()
		if thrust <= 0 {
			thrust = c.Thrust
		}
		tBurnSec := dvCirc * c.TotalMass() / thrust
		tBurnLabel = formatDurationShort(tBurnSec)
	}
	lines = append(lines,
		fmt.Sprintf("  ap:         %s%s", apLabel, trendLabel),
		fmt.Sprintf("  pe:         %s", peLabel),
		fmt.Sprintf("  %s%s", inclRowLabel, inclLabel),
		fmt.Sprintf("  t_to_apo:   %s", ttaLabel),
		fmt.Sprintf("  Δv→circ:    %s", dvCircLabel),
		fmt.Sprintf("  t_burn:     %s", tBurnLabel),
	)
	if apoFinite && !c.Landed && apoAlt > launchMissionFloorM {
		orbitStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3DDC84")).Bold(true)
		lines = append(lines, "  "+orbitStyle.Render("● ORBIT READY — coast to ap, press C to plant circularise"))
	}
	if apoFinite && !c.Landed {
		if progress := launchMissionProgress(w, c, periAlt); progress != "" {
			lines = append(lines, "  "+progress)
		}
	}
	return lines
}

// buildDescentChip is the airless-body terminal-approach cluster
// (altitude / v_vert / v_horiz / fpa / twr / sas). Returns nil unless the
// craft is in a powered descent. Mutually exclusive with the LAUNCH chip
// via the same Atmosphere gate the originals used.
func (v *OrbitView) buildDescentChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !shouldShowDescentHUD(c) {
		return nil
	}
	altAGL := c.Altitude()
	omegaRender := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
	vRel := c.State.V.Sub(omega.Cross(c.State.R))
	rNorm := c.State.R.Norm()
	var vVert, vHoriz, fpaDeg float64
	hasFPA := false
	if rNorm > 0 {
		rHat := c.State.R.Scale(1 / rNorm)
		vVert = vRel.X*rHat.X + vRel.Y*rHat.Y + vRel.Z*rHat.Z
		vHorizVec := vRel.Sub(rHat.Scale(vVert))
		vHoriz = vHorizVec.Norm()
		if vRel.Norm() > 1.0 {
			fpaDeg = math.Atan2(vVert, vHoriz) * 180 / math.Pi
			hasFPA = true
		}
	}
	twrLabel := "—"
	if c.Thrust > 0 && c.TotalMass() > 0 {
		gSurface := c.Primary.GravitationalParameter() / (c.Primary.RadiusMeters() * c.Primary.RadiusMeters())
		twr := c.Thrust * c.EffectiveThrottle() / (c.TotalMass() * gSurface)
		twrLabel = fmt.Sprintf("%.2f", twr)
		if twr < 1.0 {
			twrLabel = v.theme.Alert.Render(twrLabel + " (can't hover)")
		}
	}
	altLabel := fmt.Sprintf("%.0f m", nzero(altAGL, 0))
	if altAGL >= 1000 {
		altLabel = fmt.Sprintf("%.2f km", altAGL/1000)
	}
	fpaLabel := "—"
	if hasFPA {
		fpaLabel = fmt.Sprintf("%.0f° (0 = horiz, −90 = straight down)", nzero(fpaDeg, 0))
	}
	vHorizLabel := fmt.Sprintf("%.0f m/s (surface-rel)", vHoriz)
	if vHoriz > sim.CrashVCritMps {
		vHorizLabel = v.theme.Alert.Render(
			fmt.Sprintf("%.0f m/s (> %.0f = CRASH on contact)", vHoriz, sim.CrashVCritMps))
	}
	return []string{
		v.theme.Primary.Render("DESCENT"),
		fmt.Sprintf("  altitude:   %s", altLabel),
		fmt.Sprintf("  v_vert:     %.1f m/s", nzero(vVert, 1)),
		fmt.Sprintf("  v_horiz:    %s", vHorizLabel),
		fmt.Sprintf("  fpa:        %s", fpaLabel),
		fmt.Sprintf("  twr:        %s", twrLabel),
		fmt.Sprintf("  sas:        %s", c.AttitudeMode.String()),
	}
}

// buildChuteChip surfaces the parachute deploy state + surface-relative
// descent rate (the only window onto the canopy until ViewLanding lands).
// Returns nil for craft without a chute in flight.
func (v *OrbitView) buildChuteChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !shouldShowChuteHUD(c) {
		return nil
	}
	stateLabel := c.ChuteState.String()
	switch c.ChuteState {
	case spacecraft.ChuteDeployed:
		stateLabel = v.theme.Primary.Render(stateLabel)
	case spacecraft.ChuteArmed:
		stateLabel = v.theme.Warning.Render(stateLabel)
	default:
		stateLabel = v.theme.Dim.Render(stateLabel)
	}
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	var descentRate float64
	if rNorm := c.State.R.Norm(); rNorm > 0 {
		rHat := c.State.R.Scale(1 / rNorm)
		descentRate = -(vRel.X*rHat.X + vRel.Y*rHat.Y + vRel.Z*rHat.Z)
	}
	rateLabel := fmt.Sprintf("%.1f m/s", descentRate)
	if vRel.Norm() >= sim.CrashVCritMps {
		rateLabel = v.theme.Alert.Render(
			fmt.Sprintf("%.1f m/s (|v_rel| > %.0f = CRASH on contact)", descentRate, sim.CrashVCritMps))
	}
	lines := []string{
		v.theme.Primary.Render("CHUTE"),
		fmt.Sprintf("  state:        %s", stateLabel),
		fmt.Sprintf("  descent rate: %s", rateLabel),
	}
	if c.ChuteState == spacecraft.ChuteStowed {
		lines = append(lines, v.theme.Dim.Render("  [space] arms the chute on a bare capsule"))
	}
	return lines
}

// buildOrbitMetricsChip is the always-on top-right ORBIT readout: the
// live current orbit shape (altitude / apo / peri / t→apo / t→peri /
// inclination / direction). Suppressed during ascent — the LAUNCH chip
// already carries ap/pe there — and for degenerate / hyperbolic states.
// The projected post-burn orbit is a SEPARATE chip
// (buildProjectedOrbitChip, issue #63 follow-up) so the player sees the
// current and projected orbits side by side while planning a burn,
// instead of the live orbit being replaced by the projection.
func (v *OrbitView) buildOrbitMetricsChip(w *sim.World) []string {
	if !w.CraftVisibleHere() {
		return nil
	}
	c := w.ActiveCraft()
	if c == nil {
		return nil
	}
	// Live current orbit shape. Suppressed during ascent (LAUNCH chip
	// carries ap/pe) and for degenerate/hyperbolic states.
	if shouldShowLaunchHUD(c) {
		return nil
	}
	mu := c.Primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	el := orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
	if math.IsNaN(el.A) || math.IsInf(el.A, 0) || el.A <= 0 || el.E >= 1 {
		return nil
	}
	primaryR := c.Primary.RadiusMeters()
	apoAlt := el.Apoapsis() - primaryR
	periAlt := el.Periapsis() - primaryR
	st := orbital.Vec3State{R: c.State.R, V: c.State.V}
	lines := []string{
		v.theme.Primary.Render("ORBIT"),
		chipRow("altitude:", fmt.Sprintf("%.1f km", c.Altitude()/1000)),
		chipRow("Ap:", fmt.Sprintf("%.1f km", apoAlt/1000)),
	}
	if tApo := orbital.TimeToApoapsis(st, mu); tApo >= 0 {
		lines = append(lines, chipRow("t→Ap:", formatDurationShort(tApo)))
	}
	lines = append(lines, chipRow("Pe:", fmt.Sprintf("%.1f km", periAlt/1000)))
	if tPeri := orbital.TimeToPeriapsis(st, mu); tPeri >= 0 {
		lines = append(lines, chipRow("t→Pe:", formatDurationShort(tPeri)))
	}
	lines = append(lines, chipRow("inclin.:", fmt.Sprintf("%.2f°", el.I*180/math.Pi)))
	lines = append(lines, chipRow("direction:", v.orbitDirectionLabel(el.I)))
	if periAlt < 0 {
		lines = append(lines, "  "+v.theme.Alert.Render("⚠ PERIAPSIS BELOW SURFACE"))
	}
	return lines
}

// buildProjectedOrbitChip is the PROJECTED ORBIT readout — the projected
// post-burn orbit once resolved nodes (or a live burn) are planted,
// expressed in the primary's reference frame. Returns nil when no
// projection is available, so it surfaces only while a burn is
// planned/in flight, stacked beneath the always-on ORBIT chip. Split out
// of buildOrbitMetricsChip (issue #63 follow-up) so the current and
// projected orbits show simultaneously rather than the projection
// replacing the live readout.
func (v *OrbitView) buildProjectedOrbitChip(w *sim.World) []string {
	if !w.CraftVisibleHere() || w.ActiveCraft() == nil {
		return nil
	}
	state, primary, ok := w.PredictedFinalOrbit()
	if !ok {
		return nil
	}
	mu := primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(primary)
	ro := orbital.OrbitReadoutInFrame(state.R, state.V, mu, frame)
	primaryR := primary.RadiusMeters()
	lines := []string{
		v.theme.Primary.Render("PROJECTED ORBIT"),
		fmt.Sprintf("  primary:   %s", primary.EnglishName),
	}
	if ro.Hyperbolic {
		lines = append(lines,
			"  "+v.theme.Warning.Render("hyperbolic — escape"),
			fmt.Sprintf("  Pe:        %.1f km alt", (ro.PeriMeters-primaryR)/1000),
			fmt.Sprintf("  e:         %.3f", ro.Eccentricity),
		)
	} else {
		lines = append(lines,
			fmt.Sprintf("  Ap:        %.1f km alt", (ro.ApoMeters-primaryR)/1000),
			fmt.Sprintf("  Pe:        %.1f km alt", (ro.PeriMeters-primaryR)/1000),
			fmt.Sprintf("  inclin.:   %.2f°", ro.Inclination*180/math.Pi),
			fmt.Sprintf("  direction: %s", v.orbitDirectionLabel(ro.Inclination)),
		)
		const equatorialTol = 1e-3
		if ro.Inclination < equatorialTol || math.Abs(ro.Inclination-math.Pi) < equatorialTol {
			lines = append(lines, v.theme.Dim.Render("  AN/DN:     equatorial"))
		} else {
			lines = append(lines,
				fmt.Sprintf("  AN angle:  %.1f°", normalizeDeg(ro.AscNode*180/math.Pi)),
				fmt.Sprintf("  DN angle:  %.1f°", normalizeDeg(ro.DescNode*180/math.Pi)),
			)
		}
	}
	return lines
}

// buildTargetChip surfaces the unified Target slot — a body (name, Δi,
// range) or a craft (name/role, orbit shape, range, |v_rel|, closing,
// closest-approach, rendezvous advisory, DOCK READY). Returns nil when no
// target is set. Transplanted from renderHUD's TARGET block.
func (v *OrbitView) buildTargetChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || w.Target.Kind == sim.TargetNone {
		return nil
	}
	switch w.Target.Kind {
	case sim.TargetBody:
		sysT := w.System()
		if w.Target.BodyIdx <= 0 || w.Target.BodyIdx >= len(sysT.Bodies) {
			return nil
		}
		b := sysT.Bodies[w.Target.BodyIdx]
		nameStyle := lipgloss.NewStyle().Foreground(render.ColorFor(b)).Bold(true)
		lines := []string{
			v.theme.Primary.Render("TARGET"),
			chipRow("body:", nameStyle.Render(b.EnglishName)),
		}
		mu := c.Primary.GravitationalParameter()
		frame := orbital.ReferenceFrameForPrimary(c.Primary)
		ro := orbital.OrbitReadoutInFrame(c.State.R, c.State.V, mu, frame)
		if !ro.Hyperbolic {
			nCraft := c.State.R.Cross(c.State.V)
			nTarget := orbital.OrbitNormalWorld(b)
			var di float64
			if nCraft.Norm() > 0 && nTarget.Norm() > 0 {
				cos := nCraft.Dot(nTarget) / (nCraft.Norm() * nTarget.Norm())
				if cos > 1 {
					cos = 1
				} else if cos < -1 {
					cos = -1
				}
				ang := math.Acos(cos) * 180 / math.Pi
				di = math.Min(ang, 180-ang)
			}
			diLabel := fmt.Sprintf("%.2f°", di)
			if di > 30 {
				diLabel = v.theme.Warning.Render(diLabel)
			}
			lines = append(lines, chipRow("Δi:", diLabel))
		}
		rangeM := w.BodyPosition(b).Sub(w.CraftInertial()).Norm()
		lines = append(lines, chipRow("range:", formatRangeM(rangeM)))
		// Predicted closest approach along the projected orbit — updates live
		// as the player hand-flies a correction, so they can judge where the
		// transfer actually passes the target rather than eyeballing the
		// dashed curve. Perilune altitude when the path enters the SOI
		// (negative ⇒ surface impact), else the flyby miss distance.
		if ap, ok := w.PredictedTargetApproach(); ok {
			if ap.EntersSOI {
				alt := ap.Dist - b.RadiusMeters()
				if alt <= 0 {
					lines = append(lines, chipRow("perilune:", v.theme.Warning.Render("IMPACT")))
				} else {
					lines = append(lines, chipRow("perilune:", fmt.Sprintf("%.0f km", alt/1000)))
				}
			} else {
				lines = append(lines, chipRow("approach:", formatRangeM(ap.Dist)))
			}
			lines = append(lines, chipRow("TCA:", formatTCA(ap.TCA)))
		}
		return lines
	case sim.TargetCraft:
		tc, _, ok := w.ResolveTargetCraft()
		if !ok {
			return nil
		}
		lines := []string{v.theme.Primary.Render("TARGET"), chipRow("vessel:", tc.Name)}
		tMu := tc.Primary.GravitationalParameter()
		tFrame := orbital.ReferenceFrameForPrimary(tc.Primary)
		tEl := orbital.ElementsFromStateInFrame(tc.State.R, tc.State.V, tMu, tFrame)
		if tEl.A > 0 && !math.IsNaN(tEl.A) && !math.IsInf(tEl.A, 0) {
			tPrimaryR := tc.Primary.RadiusMeters()
			lines = append(lines,
				chipRow("Ap:", fmt.Sprintf("%.1f km", (tEl.Apoapsis()-tPrimaryR)/1000)),
				chipRow("Pe:", fmt.Sprintf("%.1f km", (tEl.Periapsis()-tPrimaryR)/1000)),
				chipRow("inclin.:", fmt.Sprintf("%.2f°", tEl.I*180/math.Pi)),
			)
		}
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
		var closing float64
		if rangeM > 0 {
			closing = -rRel.Dot(vRelVec) / rangeM
		}
		lines = append(lines,
			chipRow("range:", formatRangeM(rangeM)),
			chipRow("|v_rel|:", fmt.Sprintf("%.2f m/s", vRel)),
			chipRow("closing:", fmt.Sprintf("%+.2f m/s", closing)),
		)
		if tc.Primary.ID == c.Primary.ID {
			if rT, vT, ok := w.TargetStateRelativeToActivePrimary(); ok {
				active := orbital.Vec3State{R: c.State.R, V: c.State.V}
				target := orbital.Vec3State{R: rT, V: vT}
				mu := c.Primary.GravitationalParameter()
				const horizon = 4 * 3600.0
				if tCA, distCA, _, err := planner.NextClosestApproach(active, target, c.Primary, mu, horizon); err == nil {
					lines = append(lines,
						chipRow("TCA:", formatTCA(tCA)),
						chipRow("CA:", formatRangeM(distCA)),
					)
				}
			}
			if rangeM < 50 && vRel < 0.1 {
				dockStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3DDC84")).Bold(true)
				lines = append(lines, "  "+dockStyle.Render("DOCK READY"))
			}
		}
		return lines
	}
	return nil
}

// buildSOIPassChip surfaces the always-on SOI Pass readout (ADR 0019): the
// upcoming foreign-SOI encounter of the live trajectory — independent of the
// Target slot. With no node planted it shows the single live pass (body,
// Perilune altitude or IMPACT, Time to Perilune). With a node planted it
// stacks the dual arc (ADR 0019 D): a `planned` line (the node-modified
// path's safe periapsis) and a `no-burn` line (the counterfactual Impact the
// burn corrects). Returns nil when there is no pass, or when the Pass Body is
// also the current body Target — the TARGET chip already covers it, so the
// readouts de-dupe into one (ADR 0019 E).
func (v *OrbitView) buildSOIPassChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !w.CraftVisibleHere() {
		return nil
	}
	arc := v.cachedSOIPass(w)

	// The body the chip names: the planned pass when present (the path the
	// craft will fly), else the counterfactual/live pass.
	var body bodies.CelestialBody
	switch {
	case arc.plOK:
		body = arc.planned.Body
	case arc.cfOK:
		body = arc.counterfactual.Body
	default:
		return nil
	}

	// De-dupe with TARGET: if the player has targeted the very body the
	// pass crosses, the TARGET chip's peri/TCA rows already cover it.
	if w.Target.Kind == sim.TargetBody {
		sysT := w.System()
		if w.Target.BodyIdx > 0 && w.Target.BodyIdx < len(sysT.Bodies) &&
			sysT.Bodies[w.Target.BodyIdx].ID == body.ID {
			return nil
		}
	}

	nameStyle := lipgloss.NewStyle().Foreground(render.ColorFor(body)).Bold(true)
	lines := []string{
		v.theme.Primary.Render("SOI PASS"),
		chipRow("body:", nameStyle.Render(body.EnglishName)),
	}
	periValue := func(p sim.SOIPass) string {
		if p.Impact {
			return v.theme.Warning.Render("IMPACT")
		}
		return fmt.Sprintf("%.0f km", p.PeriluneAltitude()/1000)
	}
	if arc.hasNodes {
		// Dual arc: planned (bright path) + no-burn (counterfactual). The
		// planned path's SOI-entry clock rides under it (ADR 0021 C: the
		// Entry glyph marks where, the chip carries when).
		if arc.plOK {
			lines = append(lines, chipRow("planned:", periValue(arc.planned)))
			if arc.planned.HasEntryTime {
				lines = append(lines, chipRow("  T-entry:", formatTCA(arc.planned.TimeToEntry)))
			}
			lines = append(lines, chipRow("  T-peri:", formatTCA(arc.planned.TimeToPerilune)))
		}
		if arc.cfOK {
			lines = append(lines, chipRow("no-burn:", periValue(arc.counterfactual)))
		}
		return lines
	}
	// Single live pass (no node planted). T-entry is the predicted SOI-entry
	// clock — the ring crossing the Entry glyph marks (ADR 0021 C).
	if arc.counterfactual.HasEntryTime {
		lines = append(lines, chipRow("T-entry:", formatTCA(arc.counterfactual.TimeToEntry)))
	}
	lines = append(lines,
		chipRow("perilune:", periValue(arc.counterfactual)),
		chipRow("TCA:", formatTCA(arc.counterfactual.TimeToPerilune)))
	return lines
}

// chipValueCol is the display column a chip row's value begins at, shared
// by the ORBIT and TARGET chips so the two line up when stacked in the same
// corner. The buildOrbitMetricsChip rows are hand-padded to this column.
const chipValueCol = 13

// chipRow formats a "  label   value" telemetry row with the value pinned
// to chipValueCol regardless of label width — so a chip's values share one
// column instead of drifting per label. Padding is measured in display
// cells (lipgloss.Width), so multibyte labels like "Δi:" and styled values
// align correctly where byte-counted %-Ns padding would not.
// orbitDirectionLabel renders the prograde/retrograde orbit-direction
// readout for an equatorial-frame inclination (radians). i > 90° means
// the orbit runs retrograde — against the primary's spin. This is the
// instrument that disambiguates a genuine orbit reversal from a
// projection / day-night-shading artifact near the disk edge (issue
// #63): the on-screen position can mislead, but the direction label is
// ground truth. Prograde is the unremarkable case (plain text);
// retrograde is flagged.
func (v *OrbitView) orbitDirectionLabel(incRad float64) string {
	if incRad > math.Pi/2 {
		return v.theme.Alert.Render("retrograde")
	}
	return "prograde"
}

func chipRow(label, value string) string {
	prefix := "  " + label
	pad := chipValueCol - lipgloss.Width(prefix)
	if pad < 1 {
		pad = 1
	}
	return prefix + strings.Repeat(" ", pad) + value
}

// formatRangeM renders a distance with AU / km / m bands matching the
// thresholds the TARGET block used inline.
func formatRangeM(rangeM float64) string {
	switch {
	case rangeM > bodies.AU/10:
		return fmt.Sprintf("%.3f AU", rangeM/bodies.AU)
	case rangeM > 1e6:
		return fmt.Sprintf("%.0f km", rangeM/1000)
	case rangeM > 1000:
		return fmt.Sprintf("%.2f km", rangeM/1000)
	default:
		return fmt.Sprintf("%.0f m", rangeM)
	}
}

// nzero snaps a value whose magnitude rounds to zero at `decimals` places
// to +0, so a quantity that jitters across zero (v_vert / fpa / altitude
// on the pad, where the co-rotation state carries sub-unit noise) doesn't
// flicker a "-0" / "-0.0" sign each frame. Only the sign of an
// already-zero display changes; non-zero values pass through untouched.
func nzero(x float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	if math.Round(x*scale) == 0 {
		return 0
	}
	return x
}

// formatTCA renders a time-to-closest-approach with s / min / h bands.
func formatTCA(sec float64) string {
	switch {
	case sec >= 3600:
		return fmt.Sprintf("%.2fh", sec/3600)
	case sec >= 60:
		return fmt.Sprintf("%.1fmin", sec/60)
	default:
		return fmt.Sprintf("%.0fs", sec)
	}
}
