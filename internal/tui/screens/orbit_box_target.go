package screens

import (
	"math"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// orbit_box_target.go, the TARGET instrument box (ADR 0051 decision 11,
// slice 2a): vessel-shaped, always, four rows / ten cells (range,
// closing, rel; the target's Ap, Pe, incl; Δincl, lead; TCA, approach),
// dashes for a body target or no target. Replaces buildTargetChip's
// three separate branches (body/vessel/ghost) and its Compact Form,
// this box never compacts (decision 2 at the Design Size); below it the
// stacker drops the whole box like any over-budget chip.
//
// Rule C (decision 9, C6): the whole encounter half, closing, rel,
// lead, TCA, approach, reads dash for any ACTIVE craft that is Landed
// or Crashed, regardless of what's targeted. `!Landed && !Crashed`, not
// the old craftHasOrbit (which reads true for co-rotation wreckage,
// audit C32). The target's own orbital shape (Ap/Pe/incl, Δincl) is
// independent of the active craft's own state and is not withheld here.
type targetBoxCells struct {
	rangeV, closingV, relV string
	apV, peV, inclV        string
	deltaInclV, leadV      string
	tcaV, approachV        string
}

func dashTargetBoxCells() targetBoxCells {
	return targetBoxCells{
		rangeV: "—", closingV: "—", relV: "—",
		apV: "—", peV: "—", inclV: "—",
		deltaInclV: "—", leadV: "—",
		tcaV: "—", approachV: "—",
	}
}

func (v *OrbitView) buildTargetBox(w *sim.World) []string {
	c := w.ActiveCraft()
	name := "—"
	titleBadge := ""
	cells := dashTargetBoxCells()
	if c != nil {
		switch w.Target.Kind {
		case sim.TargetBody:
			name, cells = v.targetBodyCells(w, c)
		case sim.TargetCraft:
			name, cells, titleBadge = v.targetCraftCells(w, c)
		case sim.TargetGhost:
			name, cells = v.targetGhostCells(w, c)
		}
	}
	// Rule C: the encounter half needs the ACTIVE craft's own trajectory
	// to predict against; withhold it uniformly rather than per-branch.
	if c == nil || c.Landed || c.Crashed {
		cells.closingV, cells.relV, cells.leadV = "—", "—", "—"
		cells.tcaV, cells.approachV = "—", "—"
	}
	title := v.theme.Primary.Render("TARGET") + "  " + name + titleBadge
	return []string{
		title,
		chipRow3(targetCols, "range:", cells.rangeV, "closing:", cells.closingV, "rel", cells.relV),
		chipRow3(targetCols, readout.LabelAp, cells.apV, readout.LabelPe, cells.peV, "incl:", cells.inclV),
		chipRow2(targetCols, readout.LabelDeltaIncl, cells.deltaInclV, "lead:", cells.leadV),
		chipRow2(targetCols, readout.LabelTCA, cells.tcaV, "approach:", cells.approachV),
	}
}

// targetBodyCells is TARGET's body-target branch: range and Δincl
// against the body's fixed catalog plane, plus the predicted approach
// (or impact) and TCA along the projected orbit. closing/rel speed are
// not computed for a body target (decision 11: cells that do not apply
// get a dash cell), the pre-ADR-0051 body branch never derived them
// either.
func (v *OrbitView) targetBodyCells(w *sim.World, c *spacecraft.Spacecraft) (string, targetBoxCells) {
	cells := dashTargetBoxCells()
	sysT := w.System()
	if w.Target.BodyIdx <= 0 || w.Target.BodyIdx >= len(sysT.Bodies) {
		return "—", cells
	}
	b := sysT.Bodies[w.Target.BodyIdx]
	nameStyle := lipgloss.NewStyle().Foreground(render.ColorFor(b)).Bold(true)
	name := nameStyle.Render(b.EnglishName)

	mu := c.Primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	ro := orbital.OrbitReadoutInFrame(c.State.R, c.State.V, mu, frame)
	if !ro.Hyperbolic {
		if nCraft, ok := craftOrbitNormalForRelativeIncl(c); ok {
			nTarget := orbital.OrbitNormalWorld(b)
			if diLabel, ok := v.deltaInclLabel(nCraft, nTarget, false); ok {
				cells.deltaInclV = diLabel
			}
		}
	}
	rangeM := w.BodyPosition(b).Sub(w.CraftInertial()).Norm()
	cells.rangeV = readout.Distance(rangeM)

	// The target's own orbital shape (decision 11: Ap/Pe/incl describe
	// WHATEVER is targeted, not only a vessel), a body's is its fixed
	// catalog ellipse around its own gravitational parent, not a live
	// state-vector propagation the way a craft's is.
	if parent := sysT.ParentOf(b); parent != nil {
		aM := b.SemimajorAxis * 1000
		parentR := parent.RadiusMeters()
		peAltM := aM*(1-b.Eccentricity) - parentR
		peLabel := readout.Distance(peAltM)
		if peAltM < 0 {
			peLabel = v.theme.Warning.Render(peLabel)
		}
		cells.apV = readout.Distance(aM*(1+b.Eccentricity) - parentR)
		cells.peV = peLabel
		cells.inclV = readout.Angle(b.Inclination)
	}

	switch {
	case c.ActiveBurn != nil:
		cells.approachV = v.theme.Dim.Render("recomputing…")
	default:
		if ap, ok := w.PredictedTargetApproach(); ok {
			if ap.EntersSOI {
				alt := ap.Dist - b.RadiusMeters()
				if alt <= 0 {
					cells.approachV = v.theme.Warning.Render("IMPACT")
				} else {
					cells.approachV = readout.Distance(alt)
				}
			} else {
				cells.approachV = readout.Distance(ap.Dist)
			}
			cells.tcaV = readout.Countdown(time.Duration(ap.TCA * float64(time.Second)))
		}
	}
	return name, cells
}

// targetCraftCells is TARGET's vessel-target branch. Returns the title
// badge separately (DOCK READY, decision 11) since it rides on the
// title, not a row.
func (v *OrbitView) targetCraftCells(w *sim.World, c *spacecraft.Spacecraft) (string, targetBoxCells, string) {
	cells := dashTargetBoxCells()
	tc, _, ok := w.ResolveTargetCraft()
	if !ok {
		return "—", cells, ""
	}
	name := tc.Name
	if craftHasOrbit(tc) {
		tMu := tc.Primary.GravitationalParameter()
		tFrame := orbital.ReferenceFrameForPrimary(tc.Primary)
		tEl := orbital.ElementsFromStateInFrame(tc.State.R, tc.State.V, tMu, tFrame)
		if tEl.A > 0 && !math.IsNaN(tEl.A) && !math.IsInf(tEl.A, 0) {
			tPrimaryR := tc.Primary.RadiusMeters()
			tPeriAlt := tEl.Periapsis() - tPrimaryR
			peLabel := readout.Distance(tPeriAlt)
			if tPeriAlt < 0 {
				peLabel = v.theme.Warning.Render(peLabel)
			}
			cells.apV = readout.Distance(tEl.Apoapsis() - tPrimaryR)
			cells.peV = peLabel
			cells.inclV = readout.Angle(tEl.I * 180 / math.Pi)
		}
	}
	if nTarget, ok := w.TargetPlaneNormal(); ok {
		if nCraft, ok := craftOrbitNormalForRelativeIncl(c); ok {
			if diLabel, ok := v.deltaInclLabel(nCraft, nTarget, !craftHasOrbit(tc)); ok {
				cells.deltaInclV = diLabel
			}
		}
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
	leadDeg, leadOK := w.TargetLeadAngleDeg()
	cells.rangeV = readout.Distance(rangeM)
	cells.relV = readout.Speed(vRel)
	cells.closingV = readout.SignedSpeed(closing)
	cells.leadV = targetLeadLabel(leadDeg, leadOK)

	badge := ""
	if tc.Primary.ID == c.Primary.ID {
		switch {
		case c.ActiveBurn != nil:
			cells.tcaV = v.theme.Dim.Render("recomputing…")
		case craftHasOrbit(tc):
			cells.tcaV, cells.approachV = v.closestApproachCells(w, c)
		}
		if rangeM < 50 && vRel < 0.1 {
			badge = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#3DDC84")).Bold(true).Render("DOCK READY")
		}
	}
	return name, cells, badge
}

// targetGhostCells is TARGET's remote-player branch: same shape as the
// craft branch, resolved from the ghost slate.
func (v *OrbitView) targetGhostCells(w *sim.World, c *spacecraft.Spacecraft) (string, targetBoxCells) {
	cells := dashTargetBoxCells()
	g, gPrimary, ok := w.ResolveTargetGhost()
	if !ok {
		// #294: the lock survives an unresolved ghost, show the pending
		// state via the name rather than reading as no target at all.
		return w.TargetName() + " (not yet resolved)", cells
	}
	name := w.TargetName()
	gRel := g.Pos.Sub(w.BodyPosition(gPrimary))
	gMu := gPrimary.GravitationalParameter()
	gFrame := orbital.ReferenceFrameForPrimary(gPrimary)
	gEl := orbital.ElementsFromStateInFrame(gRel, g.Vel, gMu, gFrame)
	if gEl.A > 0 && !math.IsNaN(gEl.A) && !math.IsInf(gEl.A, 0) {
		gPrimaryR := gPrimary.RadiusMeters()
		gPeriAlt := gEl.Periapsis() - gPrimaryR
		peLabel := readout.Distance(gPeriAlt)
		if gPeriAlt < 0 {
			peLabel = v.theme.Warning.Render(peLabel)
		}
		cells.apV = readout.Distance(gEl.Apoapsis() - gPrimaryR)
		cells.peV = peLabel
		cells.inclV = readout.Angle(gEl.I * 180 / math.Pi)
	}
	if nTarget, ok := w.TargetPlaneNormal(); ok {
		if nCraft, ok := craftOrbitNormalForRelativeIncl(c); ok {
			if diLabel, ok := v.deltaInclLabel(nCraft, nTarget, false); ok {
				cells.deltaInclV = diLabel
			}
		}
	}
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return name, cells
	}
	rRel := rT.Sub(c.State.R)
	vRelVec := vT.Sub(c.State.V)
	rangeM := rRel.Norm()
	vRel := vRelVec.Norm()
	var closing float64
	if rangeM > 0 {
		closing = -rRel.Dot(vRelVec) / rangeM
	}
	leadDeg, leadOK := w.TargetLeadAngleDeg()
	cells.rangeV = readout.Distance(rangeM)
	cells.relV = readout.Speed(vRel)
	cells.closingV = readout.SignedSpeed(closing)
	cells.leadV = targetLeadLabel(leadDeg, leadOK)
	if gPrimary.ID == c.Primary.ID {
		cells.tcaV, cells.approachV = v.closestApproachCells(w, c)
	}
	return name, cells
}

// closestApproachCells is closestApproachRows' value-returning twin: the
// same NextClosestApproach prediction (shared with the map's ✕ marker
// via closestApproachHorizonSec), returned as bare (TCA, approach)
// values instead of pre-formatted rows, so the flattened box can place
// them in its own two-per-row cells. Gated on rule C by the caller
// before these values are used; "—" for either when the prediction
// isn't available.
func (v *OrbitView) closestApproachCells(w *sim.World, c *spacecraft.Spacecraft) (tcaV, approachV string) {
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return "—", "—"
	}
	active := orbital.Vec3State{R: c.State.R, V: c.State.V}
	target := orbital.Vec3State{R: rT, V: vT}
	mu := c.Primary.GravitationalParameter()
	tCA, distCA, _, err := planner.NextClosestApproach(active, target, c.Primary, mu, closestApproachHorizonSec)
	if err != nil {
		return "—", "—"
	}
	return readout.Countdown(time.Duration(tCA * float64(time.Second))), readout.Distance(distCA)
}
