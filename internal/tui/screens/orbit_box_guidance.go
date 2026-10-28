package screens

import (
	"fmt"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// orbit_box_guidance.go, the GUIDANCE instrument box (ADR 0051
// decision 1, decision 13c, slice 2a): hold with nav, heading with trim,
// fpa with orbit fpa. Replaces buildAttitudeChip's nav:/hold: rows and
// buildLaunchChip/buildDescentChip's heading:/trim:/fpa:/orbit fpa: rows.
//
// L7 fix (review side lead, folded into this slice per the ADR's own
// recommendation): fpa: and "orbit fpa:" used to gate on two DIFFERENT
// velocities' thresholds (surface-relative vs inertial), so a Landed
// craft could read a dashed fpa: cell beside a populated "orbit
// fpa: 0°", the inertial co-rotation speed clears the 1 m/s floor even
// while the surface-relative speed sits at zero. Both cells now dash
// together while Landed, one threshold in the sense that matters:
// neither fpa concept means anything standing still.
func (v *OrbitView) buildGuidanceBox(w *sim.World) []string {
	title := v.theme.Primary.Render("GUIDANCE")
	c := w.ActiveCraft()
	if c == nil {
		return []string{
			title,
			chipRow2(guidanceCols, readout.LabelHold, "—", "nav:", "—"),
			chipRow2(guidanceCols, "heading:", "—", "trim:", "—"),
			chipRow2(guidanceCols, readout.LabelFPA, "—", readout.LabelOrbitFPA, "—"),
		}
	}
	// A hold keeps the frame it was pressed in (ADR 0050), so after an
	// automatic frame switch the nav: cell can name a different frame from
	// the hold. Say so: the hold's frame word reads in Warning whenever it
	// differs from the navball's, so a surface hold under nav: ORBIT looks
	// deliberate, not stale (review #629 M1, Jason 2026-10-12).
	holdName, holdFrame := attitudeHoldParts(w, c.AttitudeMode)
	frameWord := navModeLabel(holdFrame)
	if holdFrame != w.NavMode {
		frameWord = v.theme.Warning.Render(frameWord)
	}
	holdLabel := fmt.Sprintf("%s (%s)", holdName, frameWord)
	navLabel := fmt.Sprintf("%s", w.NavMode)

	headingAbsDeg := (spacecraft.HeadingTrimDueEastRad + c.HeadingTrim) * 180 / math.Pi
	if c.AttitudeMode == spacecraft.BurnInertial { // ADR 0063: the arrows turn the hold, so read where the nose points
		if _, hdg, ok := w.NavballNoseReading(); ok {
			headingAbsDeg = hdg
		}
	}
	headingLabel := readout.Heading(headingAbsDeg)
	trimDeg := w.TrimNoseDownDeg(c)
	trimLabel := readout.TrimAngle(trimDeg)
	if math.Abs(trimDeg) > 0.05 {
		trimLabel = v.theme.Warning.Render(trimLabel)
	}

	fpaLabel, orbitFPALabel := v.guidanceFPALabels(c)

	return []string{
		title,
		chipRow2(guidanceCols, readout.LabelHold, holdLabel, "nav:", navLabel),
		chipRow2(guidanceCols, "heading:", headingLabel, "trim:", trimLabel),
		chipRow2(guidanceCols, readout.LabelFPA, fpaLabel, readout.LabelOrbitFPA, orbitFPALabel),
	}
}

// guidanceFPASpeedFloorMps mirrors sim's own unexported fpaSpeedFloorMps
// (internal/sim/descent.go): below a metre per second of motion, the
// ratio of two near-zero velocity components is noise, not a heading.
// Duplicated as a small float constant rather than exporting the sim
// package's internal threshold just for this one read.
const guidanceFPASpeedFloorMps = 1.0

// guidanceFPALabels computes GUIDANCE's fpa:/orbit fpa: cells: the
// surface-relative flight-path angle and the inertial one, both dashed
// together while Landed (L7 fix, see this file's doc comment) and each
// individually dashed below its own 1 m/s speed floor (guidanceFPASpeedFloorMps)
// otherwise, below that floor the angle is numerical noise, not a
// heading, the same rule the retired DESCENT/SURFACE chips used.
func (v *OrbitView) guidanceFPALabels(c *spacecraft.Spacecraft) (fpaLabel, orbitFPALabel string) {
	if c.Landed {
		return "—", "—"
	}
	omegaRender := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
	vRel := c.State.V.Sub(omega.Cross(c.State.R))
	rNorm := c.State.R.Norm()
	fpaLabel, orbitFPALabel = "—", "—"
	if rNorm == 0 {
		return
	}
	rHat := c.State.R.Scale(1 / rNorm)
	vVert := vRel.X*rHat.X + vRel.Y*rHat.Y + vRel.Z*rHat.Z
	vHoriz := vRel.Sub(rHat.Scale(vVert)).Norm()
	if vRel.Norm() > guidanceFPASpeedFloorMps {
		fpaLabel = readout.FPA(math.Atan2(vVert, vHoriz) * 180 / math.Pi)
	}
	vOrbit := c.State.V
	if vOrbit.Norm() > guidanceFPASpeedFloorMps {
		vVertOrbit := vOrbit.X*rHat.X + vOrbit.Y*rHat.Y + vOrbit.Z*rHat.Z
		vHorizOrbit := vOrbit.Sub(rHat.Scale(vVertOrbit)).Norm()
		orbitFPALabel = readout.FPA(math.Atan2(vVertOrbit, vHorizOrbit) * 180 / math.Pi)
	}
	return
}
