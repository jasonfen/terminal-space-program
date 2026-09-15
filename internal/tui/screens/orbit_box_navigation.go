package screens

import (
	"fmt"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"

	"github.com/charmbracelet/lipgloss"
)

// orbit_box_navigation.go, the NAVIGATION instrument box (ADR 0051
// decisions 1, 9, 10, 12, 14, 15, slice 2a): altitude/vert, horiz/speed,
// Ap/Pe, incl/period, depart/e/dir, impact/stop, and the permanent
// plan: row. Ten total lines (title + 7 rows + 2 borders = 10, matching
// the budget table). Replaces buildOrbitMetricsChip(+Compact),
// buildLandedOrbitChip, buildLaunchChip's Ap/Pe/incl/apo/depart rows,
// buildDescentChip's altitude/vert/horiz rows, and the retired DESCENT
// CORRIDOR box's altitude/descent/horiz/impact/stop rows (folded onto
// the map, decision 12).
//
// NOT in this slice (2a): the plan: row's contents and the plan arrows
// (→ annotations on Ap/Pe/incl/period), both slice 3, per the ADR's own
// proposed build slicing (item 3, "the bay and the annotation"). Here
// plan: always reads a permanent dash.
//
// The title carries three MUTUALLY EXCLUSIVE badges (never overlapping,
// by construction: a craft cannot be both climbing and falling, and
// Landed excludes both): the landed site while Landed (decision 9),
// ORBIT READY while sub-orbital-climbing above the world's Orbit Floor
// (decision 10, re-grill Q6/Q7, no floor number shown), and the
// descent alarm while the corridor is live (decision 12, re-grill Q2,
// shortened to one consistent form per the amendment's open item 2:
// "⚠ TIGHT" or "⚠ NO STOP", never the longer "CAN'T STOP (thrust)").
func (v *OrbitView) buildNavigationBox(w *sim.World) []string {
	c := w.ActiveCraft()
	title := v.navigationTitle(w, c)
	if c == nil {
		return []string{
			title,
			chipRow2(navigationCols, readout.LabelAltitude, "—", readout.LabelVert, "—"),
			chipRow2(navigationCols, readout.LabelHoriz, "—", "speed:", "—"),
			chipRow2(navigationCols, readout.LabelAp, "—", readout.LabelPe, "—"),
			chipRow2(navigationCols, readout.LabelIncl, "—", readout.LabelPeriod, "—"),
			chipRow3(navigationCols, readout.LabelDepart, "—", "e:", "—", "dir:", "—"),
			chipRow2(navigationCols, readout.LabelImpact, "—", "stop:", "—"),
			chipRowAt("plan:", "—", boxValueCol),
		}
	}

	omegaRender := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
	vRel := c.State.V.Sub(omega.Cross(c.State.R))
	var vVert, vHoriz float64
	if rNorm := c.State.R.Norm(); rNorm > 0 {
		rHat := c.State.R.Scale(1 / rNorm)
		vVert = vRel.X*rHat.X + vRel.Y*rHat.Y + vRel.Z*rHat.Z
		vHoriz = vRel.Sub(rHat.Scale(vVert)).Norm()
	}

	apCell, peCell := v.navigationApPeCells(w, c)
	inclCell, periodCell := v.navigationInclPeriodCells(w, c)
	departV, eV, dirV := v.navigationDepartECells(w, c)
	impactCell, stopCell := v.navigationImpactStopCells(w, c)

	// horiz: carries the same CRASH-on-contact alert the retired DESCENT
	// chip's horiz: row did: a sideways speed the vertical-rate check
	// alone says nothing about, and the one that turns a nulled descent
	// rate into a smear across the ground. Matches descentCorridorLines'
	// simplified wording (no threshold parenthetical) for consistency,
	// the same row now renders in both views (decision 3). Gated on the
	// same live-descent-corridor condition as the impact:/stop: row
	// (C3): the retired DESCENT chip only ever rendered near the
	// ground, so this alert never used to fire in a stable orbit; the
	// unification onto an always-present row needs the same gate to
	// avoid a false alert on ordinary orbital speed.
	_, descending := sim.DescentCorridorFor(c, sim.DescentPredictHorizon)
	horizLabel := readout.Speed(vHoriz)
	if descending && vHoriz > sim.CrashVCritMps {
		horizLabel = v.theme.Alert.Render(readout.Speed(vHoriz) + " (CRASH on contact)")
	}

	return []string{
		title,
		chipRow2(navigationCols, readout.LabelAltitude, readout.Distance(c.Altitude()), readout.LabelVert, readout.Speed(vVert)),
		chipRow2(navigationCols, readout.LabelHoriz, horizLabel, "speed:", readout.Speed(c.OrbitalSpeed())),
		chipRow2(navigationCols, readout.LabelAp, apCell, readout.LabelPe, peCell),
		chipRow2(navigationCols, readout.LabelIncl, inclCell, readout.LabelPeriod, periodCell),
		chipRow3(navigationCols, readout.LabelDepart, departV, "e:", eV, "dir:", dirV),
		chipRow2(navigationCols, readout.LabelImpact, impactCell, "stop:", stopCell),
		chipRowAt("plan:", "—", boxValueCol),
	}
}

// navigationTitle composes NAVIGATION's title: the primary, then at most
// one of the three mutually-exclusive badges.
func (v *OrbitView) navigationTitle(w *sim.World, c *spacecraft.Spacecraft) string {
	title := v.theme.Primary.Render("NAVIGATION")
	if c == nil {
		return title
	}
	title += "  " + c.Primary.EnglishName
	switch {
	case c.Landed:
		lat, lon := c.SurfaceLatLon()
		title += "  landed " + readout.Angle(lat) + ", " + readout.Angle(lon)
	case isSubOrbitalClimb(c):
		if _, apoAltM, _, ok := craftLiveElements(c); ok {
			if apoAltM > sim.OrbitFloorForCraft(c) {
				orbitReadyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3DDC84")).Bold(true)
				title += "  " + orbitReadyStyle.Render("● ORBIT READY [C]")
			}
		}
	default:
		if corridor, descending := sim.DescentCorridorFor(c, sim.DescentPredictHorizon); descending {
			stopDat := v.cachedDescentStop(w, c)
			corridor.Stop, corridor.StopOK = stopDat.stop, stopDat.stopOK
			corridor.BurnAt, corridor.HasBurnAt = stopDat.burnAt, stopDat.hasBurnAt
			corridor.Margin = sim.DeriveMarginState(stopDat.stop, stopDat.stopOK, corridor.AltitudeM, stopDat.burnAt, stopDat.hasBurnAt)
			if alarm, ok := navigationDescentAlarm(corridor); ok {
				title += "  " + alarm
			}
		}
	}
	return title
}

// navigationDescentAlarm renders the title's descent alarm badge in one
// of two SHORT forms (re-grill Q2's amendment, open item 2): "⚠ TIGHT"
// (Warning colour) when the margin is tight but stoppable, "⚠ NO STOP"
// (Alert colour) for every unstoppable outcome (crashed, fuel-limited,
// or the integration refusing to resolve at all), one consistent short
// form rather than a label that changes with the limiter, since the
// limiter and the number both survive on the stop: cell itself (Q2).
// ok is false when the corridor isn't currently alarming (comfortable
// margin), so the title carries nothing.
func navigationDescentAlarm(dc sim.DescentCorridor) (string, bool) {
	if !dc.StopOK {
		return "⚠ NO STOP", true
	}
	switch dc.Stop.Outcome {
	case sim.StopStopped:
		if dc.Margin.State == sim.MarginTight {
			return "⚠ TIGHT", true
		}
		return "", false
	case sim.StopCrashed, sim.StopFuelLimited:
		return "⚠ NO STOP", true
	}
	return "", false
}

// navigationApPeCells: the Ap cell carries the live apoapsis altitude, a
// ↑/↓ trend glyph (nothing when steady, re-grill Q3) and a T- countdown
// to apoapsis when the apsis is defined; Pe is the bare periapsis
// altitude, Warning-coloured when negative (sub-surface). Both dash
// together outside a real orbit (Landed, or a degenerate/hyperbolic
// state).
func (v *OrbitView) navigationApPeCells(w *sim.World, c *spacecraft.Spacecraft) (apCell, peCell string) {
	el, apoAltM, periAltM, ok := craftLiveElements(c)
	if !ok {
		return "—", "—"
	}
	mu := c.Primary.GravitationalParameter()
	now := w.Clock.SimTime
	trend := ""
	if v.ascentTrendCraft == c && !v.ascentTrendTime.IsZero() {
		dt := now.Sub(v.ascentTrendTime).Seconds()
		if dt > 1e-6 {
			rate := (el.Apoapsis() - v.ascentTrendApoM) / dt
			switch {
			case rate > 1.0:
				trend = " ↑"
			case rate < -1.0:
				trend = " ↓"
			}
		}
	}
	v.ascentTrendCraft = c
	v.ascentTrendApoM = el.Apoapsis()
	v.ascentTrendTime = now

	apCell = readout.Distance(apoAltM) + trend
	if orbital.ApsisDefined(el.E) {
		if tta := orbital.TimeToApoapsis(orbital.Vec3State{R: c.State.R, V: c.State.V}, mu); tta >= 0 {
			apCell += " " + readout.Countdown(secondsToDuration(tta))
		}
	}
	peCell = readout.Distance(periAltM)
	if periAltM < 0 {
		peCell = v.theme.Warning.Render(peCell)
	}
	return apCell, peCell
}

// navigationInclPeriodCells: incl: reverts to the live orbital element
// once airborne (decision 9: "once airborne... incl: reverts to the
// live orbital element"); while Landed it carries the pad's own
// heading-derived launch-window reading with its "(min N°)" floor
// suffix. period: is the live orbit's full period, dash while Landed or
// with no valid orbit.
func (v *OrbitView) navigationInclPeriodCells(w *sim.World, c *spacecraft.Spacecraft) (inclCell, periodCell string) {
	if c.Landed {
		return v.landedInclValue(c), "—"
	}
	el, _, _, ok := craftLiveElements(c)
	if !ok {
		return "—", "—"
	}
	inclCell = readout.Angle(el.I * 180 / math.Pi)
	mu := c.Primary.GravitationalParameter()
	period := 2 * math.Pi * math.Sqrt(el.A*el.A*el.A/mu)
	periodCell = readout.Period(secondsToDuration(period))
	return inclCell, periodCell
}

// navigationDepartECells: depart: carries the pad's launch-window angle
// while Landed (with its "(best N°)" suffix), or the live orbit's own
// departure-plane angle once airborne, dash where the sweep is frozen
// (C1) or the reference plane/normal isn't resolvable. e:/dir: are the
// live orbit's eccentricity and prograde/retrograde direction, dash
// while Landed or with no valid orbit (they have no pad-window
// equivalent the way depart: does).
func (v *OrbitView) navigationDepartECells(w *sim.World, c *spacecraft.Spacecraft) (departV, eV, dirV string) {
	departV = "—"
	if c.Landed {
		departV = v.landedDepartValue(c)
	} else if refNormal, ok := departReferenceNormal(c.Primary); ok {
		spinAxisR := render.BodyRotationAxisWorld(c.Primary)
		spinAxis := orbital.Vec3{X: spinAxisR.X, Y: spinAxisR.Y, Z: spinAxisR.Z}
		if _, hidden := departRowHidden(spinAxis, refNormal); !hidden {
			if nCraft, ok := craftOrbitNormalForRelativeIncl(c); ok {
				if deg, ok := unfoldedPlaneAngleDeg(nCraft, refNormal); ok {
					departV = readout.Angle(deg)
				}
			}
		}
	}
	el, _, _, ok := craftLiveElements(c)
	if !ok {
		return departV, "—", "—"
	}
	eV = fmt.Sprintf("%.4f", el.E)
	dirV = v.orbitDirectionLabel(el.I)
	return departV, eV, dirV
}

// navigationImpactStopCells is the descent corridor's impact:/stop: row
// (decision 12, re-grill Q2): both dash outside a live descent forecast.
// The stop: cell keeps its own number and colour per outcome (Q2's cell
// forms); the alarm WORDS live on the title only (navigationDescentAlarm),
// not here.
func (v *OrbitView) navigationImpactStopCells(w *sim.World, c *spacecraft.Spacecraft) (impactV, stopV string) {
	corridor, descending := sim.DescentCorridorFor(c, sim.DescentPredictHorizon)
	if !descending {
		return "—", "—"
	}
	stopDat := v.cachedDescentStop(w, c)
	corridor.Stop, corridor.StopOK = stopDat.stop, stopDat.stopOK
	corridor.BurnAt, corridor.HasBurnAt = stopDat.burnAt, stopDat.hasBurnAt
	corridor.Margin = sim.DeriveMarginState(stopDat.stop, stopDat.stopOK, corridor.AltitudeM, stopDat.burnAt, stopDat.hasBurnAt)

	impactV = fmt.Sprintf("%s (%s)", readout.Countdown(corridor.Impact.TimeToImpact), readout.Speed(corridor.Impact.SpeedMps))
	stopV = v.navigationStopCell(corridor)
	return impactV, stopV
}

// navigationStopCell renders the stop: cell's number+colour per outcome
// (re-grill Q2): the alarm WORDS are gone from here (they live on the
// title, navigationDescentAlarm) but the cell keeps its own colour and,
// for an unstoppable outcome, the limiter that bound it.
func (v *OrbitView) navigationStopCell(dc sim.DescentCorridor) string {
	if !dc.StopOK {
		return v.theme.Alert.Render(fmt.Sprintf("unresolved (%s)", dc.Margin.Limiter))
	}
	switch dc.Stop.Outcome {
	case sim.StopStopped:
		label := readout.Distance(dc.Stop.MarginM) + " up"
		if dc.Margin.State == sim.MarginTight {
			return v.theme.Warning.Render(label)
		}
		return v.theme.Primary.Render(label)
	case sim.StopCrashed:
		return v.theme.Alert.Render(fmt.Sprintf("short by %s (%s)", readout.Distance(-dc.Stop.MarginM), readout.Speed(dc.Stop.ImpactSpeedMps)))
	case sim.StopFuelLimited:
		return v.theme.Alert.Render(fmt.Sprintf("fuel-limited at %s", readout.Distance(dc.Stop.MarginM)))
	}
	return v.theme.Dim.Render("—")
}

// landedInclValue / landedDepartValue are landedInclHeadingRows' incl:/
// depart: VALUES (not pre-formatted rows): NAVIGATION needs to place
// them in its own two/three-per-row cells rather than as their own
// dedicated rows the way the retired SURFACE/DESCENT chips did. Δincl:
// is deliberately NOT reproduced here, decision 9's relocation table
// moves it to TARGET's own Δincl: cell only, ending the pad duplicate
// (F11).
func (v *OrbitView) landedInclValue(c *spacecraft.Spacecraft) string {
	spinAxisR := render.BodyRotationAxisWorld(c.Primary)
	spinAxis := orbital.Vec3{X: spinAxisR.X, Y: spinAxisR.Y, Z: spinAxisR.Z}
	padInclLabel := "—"
	if deg, ok := spacecraft.HeadingInclinationDeg(c.State.R, spinAxis, c.HeadingTrim); ok {
		padInclLabel = readout.Angle(deg)
	}
	floorLat, _ := c.SurfaceLatLon()
	floorLabel := readout.Angle(math.Abs(floorLat))
	return padInclLabel + " (min " + floorLabel + ")"
}

func (v *OrbitView) landedDepartValue(c *spacecraft.Spacecraft) string {
	if !landedPlaneNormalOK(c) {
		return "—"
	}
	spinAxisR := render.BodyRotationAxisWorld(c.Primary)
	spinAxis := orbital.Vec3{X: spinAxisR.X, Y: spinAxisR.Y, Z: spinAxisR.Z}
	refNormal, ok := departReferenceNormal(c.Primary)
	if !ok {
		return "—"
	}
	eps, hidden := departRowHidden(spinAxis, refNormal)
	if hidden {
		return "—"
	}
	padNormal, ok := spacecraft.HeadingOrbitNormal(c.State.R, spinAxis, c.HeadingTrim)
	if !ok {
		return "—"
	}
	deg, degOK := unfoldedPlaneAngleDeg(padNormal, refNormal)
	i, iOK := unfoldedPlaneAngleDeg(padNormal, spinAxis)
	if !degOK || !iOK {
		return "—"
	}
	_, _, best := departSwing(i, eps)
	return readout.Angle(deg) + " (best " + readout.Angle(best) + ")"
}
