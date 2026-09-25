package screens

import (
	"fmt"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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
		// ADR 0038 S4 part 3 ("badged panels"), ported from the retired
		// ORBIT/VESSEL chips (audit: TestDockGuestOrbitChipShowsBadgedShape,
		// TestDockGuestVesselChipShowsBadgedFlightData): while riding as a
		// guest in another player's stack, this player's own Crafts slate
		// is empty, so there is nothing for w.ActiveCraft() to return,
		// exactly when there IS an orbit worth showing: the stack's. The
		// ghost report carries a state vector (RelPos/Vel) but no
		// fuel/mass/TWR/Δv (never reported over the wire), so only the
		// rows NAVIGATION already owns from the retired VESSEL box
		// (primary, speed) and the retired ORBIT box (Ap/Pe/incl/period)
		// have anything to show; the rest stay dash, same as the no-craft
		// case below.
		if lines, ok := v.navigationDockGuestBox(w); ok {
			return lines
		}
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

	// plan: row + the → annotations on Ap/Pe/incl/period (ADR 0051 slice
	// 3 item 4, re-grill Q1/Q3): both driven off the same
	// PredictedFinalOrbit call, so the row and the arrows can never
	// disagree about whether a plan exists.
	predState, predPrimary, predOK := w.PredictedFinalOrbit()
	if predOK {
		apCell, peCell, inclCell, periodCell = v.appendPlanArrows(predState, predPrimary, apCell, peCell, inclCell, periodCell)
	}
	planRow := v.navigationPlanRow(c, predState, predPrimary, predOK)

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
		planRow,
	}
}

// navigationDockGuestBox is buildNavigationBox's rider-view sibling (ADR
// 0038 S4 part 3, ported from the retired buildDockGuestOrbitChip and
// buildVesselChip's badged branch): while riding as a guest with no
// local craft, derives whatever NAVIGATION's rows can show from the
// stack owner's ghost report: primary and speed (the retired VESSEL
// box's identity fields, decision 6's removal table) and the orbit
// shape Ap/Pe/incl/period (the retired ORBIT chip's own badged
// sibling), headered with the owner's handle so the numbers are never
// mistaken for this player's own ship. ok=false with no DockGuest, no
// ghost report yet, or a degenerate/hyperbolic resolved orbit, in which
// case the caller falls back to the ordinary all-dash no-craft box.
func (v *OrbitView) navigationDockGuestBox(w *sim.World) ([]string, bool) {
	g, primary, ok := w.DockGuestStackGhost()
	if !ok {
		return nil, false
	}
	mu := primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(*primary)
	el := orbital.ElementsFromStateInFrame(g.RelPos, g.Vel, mu, frame)
	if math.IsNaN(el.A) || math.IsInf(el.A, 0) || el.A <= 0 || el.E >= 1 {
		return nil, false
	}
	primaryR := primary.RadiusMeters()
	apoAlt, periAlt := el.Apoapsis()-primaryR, el.Periapsis()-primaryR

	title := v.theme.Primary.Render("NAVIGATION") + "  " + primary.EnglishName
	if w.DockGuest.OwnerHandle != "" {
		title += "  " + v.theme.Warning.Render(w.DockGuest.OwnerHandle+"'s stack")
	}

	peCell := readout.Distance(periAlt)
	if periAlt < 0 {
		peCell = v.theme.Warning.Render(peCell)
	}
	period := 2 * math.Pi * math.Sqrt(el.A*el.A*el.A/mu)

	return []string{
		title,
		chipRow2(navigationCols, readout.LabelAltitude, "—", readout.LabelVert, "—"),
		chipRow2(navigationCols, readout.LabelHoriz, "—", "speed:", readout.Speed(g.Vel.Norm())),
		chipRow2(navigationCols, readout.LabelAp, readout.Distance(apoAlt), readout.LabelPe, peCell),
		chipRow2(navigationCols, readout.LabelIncl, readout.Angle(el.I*180/math.Pi), readout.LabelPeriod, readout.Period(secondsToDuration(period))),
		chipRow3(navigationCols, readout.LabelDepart, "—", "e:", "—", "dir:", "—"),
		chipRow2(navigationCols, readout.LabelImpact, "—", "stop:", "—"),
		chipRowAt("plan:", "—", boxValueCol),
	}, true
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
//
// The sampler is idempotent per sim instant (review finding 3,
// 2026-09-25): a repeat call at the SAME w.Clock.SimTime reuses the
// trend already decided for that instant (v.ascentTrendLast) instead of
// recomputing against dt=0, which would silently erase it. This matters
// because the LAUNCH view builds NAVIGATION twice in one frame
// (airScaleColumnBound measures it, composeChips builds it for real);
// without idempotency the second, DISPLAYED build always sees dt=0
// against the timestamp the first, discarded build just wrote, so the
// arrow never reaches the screen during an Earth ascent even while
// apoapsis is genuinely climbing. Any caller that samples more than once
// at one instant (today: the LAUNCH view; potentially a future one) gets
// the correct, shared answer instead of racing itself.
func (v *OrbitView) navigationApPeCells(w *sim.World, c *spacecraft.Spacecraft) (apCell, peCell string) {
	el, apoAltM, periAltM, ok := craftLiveElements(c)
	if !ok {
		return "—", "—"
	}
	mu := c.Primary.GravitationalParameter()
	now := w.Clock.SimTime
	sameInstant := v.ascentTrendCraft == c && !v.ascentTrendTime.IsZero() && now.Equal(v.ascentTrendTime)
	var trend string
	switch {
	case sameInstant:
		trend = v.ascentTrendLast
	case v.ascentTrendCraft == c && !v.ascentTrendTime.IsZero():
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
	if !sameInstant {
		v.ascentTrendCraft = c
		v.ascentTrendApoM = el.Apoapsis()
		v.ascentTrendTime = now
		v.ascentTrendLast = trend
	}

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

// planElementsInPrimaryFrame converts PredictedFinalOrbit's raw
// state/primary into orbital elements using the SAME per-body reference
// frame convention every other NAVIGATION cell reads its own live
// elements in (craftLiveElements, navigationDockGuestBox): body-bound
// orbits in the primary's own equatorial frame, heliocentric ones
// ecliptic-relative (internal/orbital/frame.go). AN/DN and the plan's
// own inclination are only meaningful measured in that same frame.
func planElementsInPrimaryFrame(state physics.StateVector, primary bodies.CelestialBody) orbital.Elements {
	mu := primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(primary)
	return orbital.ElementsFromStateInFrame(state.R, state.V, mu, frame)
}

// appendPlanArrows (ADR 0051 slice 3 item 4, re-grill Q1/Q3): once a
// burn is planted, Ap:/Pe:/incl:/period: each gain a trailing "→
// becomes" annotation naming PredictedFinalOrbit's own value in that
// cell's own units. → means only "becomes", never a trend (re-grill
// Q3): the Ap cell's own ↑/↓ trend glyph (navigationApPeCells) and its
// T- countdown are untouched and sit BEFORE the arrow, exactly re-grill
// Q3's own measured example ("Ap: 300.7 km T-3m29s → 860.4 km"). Ap/Pe/
// period stay unannotated for a plan that resolves hyperbolic (an
// escaping or high-energy-flyby final orbit, el.E >= 1): there is no
// final apoapsis, periapsis, or period to name, only a shape. incl:
// still gets its arrow there: orientation is well-defined even
// hyperbolic.
func (v *OrbitView) appendPlanArrows(state physics.StateVector, primary bodies.CelestialBody, apCell, peCell, inclCell, periodCell string) (string, string, string, string) {
	el := planElementsInPrimaryFrame(state, primary)
	if math.IsNaN(el.I) {
		return apCell, peCell, inclCell, periodCell
	}
	inclCell += " → " + readout.Angle(el.I*180/math.Pi)
	if math.IsNaN(el.A) || math.IsInf(el.A, 0) || el.A <= 0 || el.E >= 1 {
		return apCell, peCell, inclCell, periodCell
	}
	primaryR := primary.RadiusMeters()
	mu := primary.GravitationalParameter()
	apCell += " → " + readout.Distance(el.Apoapsis()-primaryR)
	peCell += " → " + readout.Distance(el.Periapsis()-primaryR)
	period := 2 * math.Pi * math.Sqrt(el.A*el.A*el.A/mu)
	periodCell += " → " + readout.Period(secondsToDuration(period))
	return apCell, peCell, inclCell, periodCell
}

// navigationPlanEquatorialToleranceDeg: an orbit within this many
// degrees of the reference plane (0° or 180°, a retrograde-equatorial
// orbit) has no well-defined ascending/descending node to name: the
// orbit IS the reference plane, so re-grill Q1's "equatorial" word
// replaces the angles rather than printing a numerically-unstable
// near-arbitrary AN/DN pair.
const navigationPlanEquatorialToleranceDeg = 0.05

// navigationPlanRow builds NAVIGATION's permanent tenth row (re-grill
// Q1, decision 15): a bare dash cell with no plan; otherwise the world
// the planned numbers are measured from, then either the node angles or,
// for an equatorial plan, the word "equatorial" in their place.
// Precedence (checked in this order, matching the three named forms):
// a plan that ends at a DIFFERENT primary than the craft's current one
// is always "<world> encounter", regardless of whether that arrival
// resolves elliptical or hyperbolic (a flyby is still an encounter); a
// plan that stays at the SAME primary but resolves hyperbolic is
// "<world> escape" with no angles at all; otherwise it is "<world>
// orbit" with angles. "Earth orbit" / "Earth escape" wording is the
// orchestrator's own assumption (told to Jason), implemented as stated.
func (v *OrbitView) navigationPlanRow(c *spacecraft.Spacecraft, state physics.StateVector, primary bodies.CelestialBody, ok bool) string {
	if !ok {
		return chipRowAt("plan:", "—", boxValueCol)
	}
	el := planElementsInPrimaryFrame(state, primary)
	encounter := primary.ID != c.Primary.ID
	hyperbolic := math.IsNaN(el.A) || math.IsInf(el.A, 0) || el.A <= 0 || el.E >= 1

	var value string
	switch {
	case encounter:
		value = primary.EnglishName + " encounter"
	case hyperbolic:
		return chipRowAt("plan:", primary.EnglishName+" escape", boxValueCol)
	default:
		value = primary.EnglishName + " orbit"
	}
	if math.IsNaN(el.I) || math.IsNaN(el.Omega) {
		return chipRowAt("plan:", value, boxValueCol)
	}
	incDeg := el.I * 180 / math.Pi
	if incDeg < navigationPlanEquatorialToleranceDeg || incDeg > 180-navigationPlanEquatorialToleranceDeg {
		value += "  equatorial"
	} else {
		anDeg := math.Mod(el.Omega*180/math.Pi+360, 360)
		dnDeg := math.Mod(anDeg+180, 360)
		value += fmt.Sprintf("  AN %s  DN %s", readout.Angle(anDeg), readout.Angle(dnDeg))
	}
	return chipRowAt("plan:", value, boxValueCol)
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
