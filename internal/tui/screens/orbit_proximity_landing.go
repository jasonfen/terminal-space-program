package screens

// The landing view (ADR 0067 decision 8): Proximity View on ground. One-line
// reason for a new file: the surface branch's drawing and chip, as
// orbit_proximity_ports.go is the ports'.

import (
	"fmt"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

const (
	// landingSiteHintStripText is the strip under the Site+ hold: no arrow
	// legend until ADR 0067 S4 gives the arrows the aim.
	landingSiteHintStripText = "[o] map · [F1] help · [t] target · [./,] warp"
	// landingStopGlyph is where the powered stop ends on the ground.
	landingStopGlyph = '⊙'
	// computerSiteGlyph is the computer's landing site (ADR 0067 decision 4).
	computerSiteGlyph = '⌖'
	// landingGroundSegments is how many edges the ground arc is drawn with.
	landingGroundSegments = 96
)

// landingSiteMark is the glyph at the aimed ground point: a flag for a site,
// the target diamond for a landed vessel or ghost.
func landingSiteMark(w *sim.World) rune {
	if w.Target.Kind == sim.TargetSite {
		return flagGlyph
	}
	return '◆'
}

// computerSiteWorld is where the computer is flying to: the real point shifted
// by its navigation error and the pilot's aim (a modern vessel's ⌖ sits under
// the ⚑).
func computerSiteWorld(st sim.ProximityState) orbital.Vec3 {
	if st.ComputerWorld.Norm() == 0 {
		return st.TargetWorld
	}
	return st.ComputerWorld
}

// drawLandingScene draws the ground, the powered-stop arc, the marks and the
// vessel. The canvas basis is (track, up) with the site at the centre.
func (v *OrbitView) drawLandingScene(w *sim.World, st sim.ProximityState) {
	c := w.ActiveCraft()
	scale := v.canvas.Scale()
	if c == nil || scale <= 0 {
		return
	}
	// The ground: the body's mean radius as an arc in the (track, up) plane
	// through the site, as far as the canvas reaches.
	radius := c.Primary.RadiusMeters()
	centre := st.TargetWorld.Sub(st.Frame.RadialOut.Scale(radius))
	reach := (float64(v.canvas.Cols()*2) + float64(v.canvas.Rows()*4)) / scale
	half := reach / radius
	if half > 1.5 {
		half = 1.5
	}
	pts := make([]orbital.Vec3, 0, 2*landingGroundSegments+1)
	for i := -landingGroundSegments; i <= landingGroundSegments; i++ {
		phi := half * float64(i) / landingGroundSegments
		pts = append(pts, centre.Add(st.Frame.RadialOut.Scale(radius*math.Cos(phi))).Add(st.Frame.AlongTrack.Scale(radius*math.Sin(phi))))
	}
	v.canvas.PlotPolylineClass(pts, render.ColorDim, widgets.ClassReal)

	if arc, ok := w.LandingStopArc(); ok {
		v.canvas.PlotPolylineClass(arc.Points, render.ColorPredictedPath, widgets.ClassPlanned)
		if arc.Stopped {
			v.canvas.SetCellOverlayColored(arc.Points[len(arc.Points)-1], landingStopGlyph, render.ColorPredictedPath)
		}
	}
	// ⌖ first, ⚑ over it: with no nav error they share a cell and only ⚑ shows.
	// When the two differ the ⚑ is drawn only inside SiteRevealRangeM.
	cs := computerSiteWorld(st)
	differs := cs.Sub(st.TargetWorld).Norm() > 0.5
	if w.Target.Kind == sim.TargetSite {
		v.canvas.SetCellOverlayColored(cs, computerSiteGlyph, render.ColorTarget)
	}
	if !differs || st.RangeM <= sim.SiteRevealRangeM {
		v.canvas.SetCellOverlayColored(st.TargetWorld, landingSiteMark(w), render.ColorTarget)
	}
	v.drawProximityOwnVessel(w, st)
}

// landingLatVel is the lateral speed over the ground with the side it drifts
// to, cross:'s own convention (R / L of the track); dead still has no side.
func landingLatVel(mps float64) string {
	s := readout.Speed(math.Abs(mps))
	switch {
	case s == readout.Speed(0): // rounds to nothing: no side
		return s
	case mps > 0:
		return s + " R"
	}
	return s + " L"
}

// landingChipRows is the LANDING chip (ADR 0067 decision 8): PROXIMITY's slot,
// 41 cells wide, the rows a lander pilot reads, one reading per row (the
// mock's paired rows overflowed the 39 cells at three-digit rates). The
// throttle, rcs and monoprop rows and the hold: row are PROXIMITY's own
// builders, so the two cannot drift.
func (v *OrbitView) landingChipRows(w *sim.World, st sim.ProximityState) []string {
	lb, _ := w.LandBesideReading()
	c := w.ActiveCraft()
	lpd := "—"
	if lb.LPDOK {
		lpd = fmt.Sprintf("%.0f°", lb.LPDDeg)
	}
	aim := readout.Distance(0)
	if c != nil && c.Landing != nil {
		aim = landMissLabel(sim.LandBeside{MissOK: true, MissM: c.Landing.AimAlongM})
		if c.Landing.AimAcrossM != 0 {
			aim += "  " + landCrossLabel(sim.LandBeside{MissOK: true, CrossM: c.Landing.AimAcrossM})
		}
	}
	_, stop := v.navigationImpactStopCells(w, c) // NAVIGATION's own stop: cell, so the two cannot drift
	rows := []string{
		v.theme.Primary.Render("LANDING") + "  " + truncateCells(st.TargetName, proximityChipInnerWidth-9),
		chipRow("range:", readout.Distance(st.RangeM)),
		chipRow("alt:", readout.Distance(st.AltM)),
		chipRow("alt rate:", readout.SignedSpeed(st.AltRateMS)+v.landingSpeedNeed(c)),
		chipRow("stop:", truncateCells(stop, proximityChipInnerWidth-chipValueCol)),
		chipRow("fwd vel:", readout.Speed(st.FwdVelMS)),
		chipRow("lat vel:", landingLatVel(st.LatVelMS)),
		chipRow("lpd:", lpd),
		chipRow("miss:", landMissLabel(lb)),
		chipRow("cross:", landCrossLabel(lb)),
		chipRow("aim:", aim),
		proximityHoldRow(w),
		v.landingEngineRow(w),
		chipRow("twr:", engineTWRValue(c)), // ENGINE's figure (P36)
		v.proximityRCSRow(w),
		proximityMonopropRow(w),
	}
	return rows
}

// landingSpeedNeed is "  (need < 10)" in the Alert colour when the vessel,
// inside 100 m, would crash on contact by speed (sim.ContactSpeedCrashes, the
// crash's own test); empty otherwise.
func (v *OrbitView) landingSpeedNeed(c *spacecraft.Spacecraft) string {
	if c == nil || !sim.ContactSpeedCrashes(c) {
		return ""
	}
	return v.theme.Alert.Render(fmt.Sprintf("  (need < %.0f)", sim.CrashVCritMps))
}

// landingEngineRow is proximityEngineRow in the cells a landing leaves it: a
// landing burns for minutes, so the burn clock (the ENGINE box's) and the
// FIRING word (hold: names the phase) give way to a lit ● and the Δv pair, which
// otherwise outgrow the 39 cells and widen the chip. The tank's state still shows.
func (v *OrbitView) landingEngineRow(w *sim.World) string {
	c := w.ActiveCraft()
	if c == nil {
		return chipRow("throttle:", "—")
	}
	label, throttle := engineThrottleTarget(c)
	val := fmt.Sprintf("%.0f%%", throttle*100)
	switch {
	case sim.StackDryArmed(c):
		val += " " + v.theme.Warning.Render("✕")
	case sim.StackMidBurn(c):
		val += " " + v.theme.Warning.Render("●")
	}
	return chipRow(label, val+"  Δv "+deltaVReadout(c))
}

// landingChipCompact is the LANDING chip's Compact Form: name, range, the
// altitude pair and the hold, the readings a descent is flown on.
func (v *OrbitView) landingChipCompact(w *sim.World, st sim.ProximityState) []string {
	return []string{
		v.theme.Primary.Render("LANDING") + "  " + truncateCells(st.TargetName, proximityChipInnerWidth-9),
		chipRow("range:", readout.Distance(st.RangeM)),
		chipRow("alt:", readout.Distance(st.AltM)),
		chipRow("alt rate:", readout.SignedSpeed(st.AltRateMS)),
		proximityHoldRow(w),
	}
}
