package screens

import (
	"fmt"
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
	// planned: tcaV/approachV carry the Rendezvous Plan's arrival and
	// predicted separation (labelled plan: / miss:), not the 4 h
	// closest-approach search.
	planned bool
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
		case sim.TargetSite:
			name, cells = v.targetSiteCells(w, c)
		}
	}
	// Rule C: the encounter half needs the ACTIVE craft's own trajectory
	// to predict against; withhold it uniformly rather than per-branch.
	if c == nil || c.Landed || c.Crashed {
		cells.closingV, cells.relV = "—", "—"
		cells.tcaV, cells.approachV = "—", "—"
		cells.planned = false
	}
	// #460 (G6 Q4b): lead: is the one encounter cell lifted off rule C on
	// the pad: it times a launch to arrive near an orbiting target, so it
	// reads LIVE while Landed (a crashed vessel still dashes).
	if c == nil || c.Crashed {
		cells.leadV = "—"
	}
	if c != nil && w.Target.Kind == sim.TargetCraft {
		if _, _, resolves := w.ResolveTargetCraft(); !resolves && name == "—" {
			// ADR 0063 section 3: the dock retired the partner's slate entry; the
			// aim slot still names it, rows stay dashed.
			if n, ok := w.DockedPartnerName(w.Target.CraftID); ok {
				name, titleBadge = n+" · docked", ""
			}
		}
	}
	title := v.theme.Primary.Render("TARGET") + "  " + name + titleBadge
	// ADR 0066 decisions 9 and 10: against a surface target while flying, the
	// Ap/Pe, lead and TCA rows are dead (dashes for ever), so they carry the
	// land-beside picture. The box never moves.
	if c != nil {
		if lb, ok := w.LandBesideReading(); ok {
			return []string{
				title,
				chipRow3(targetCols, "range:", cells.rangeV, "close:", cells.closingV, "rel:", cells.relV),
				chipRow2(targetCols, "ground:", readout.Distance(lb.GroundM), "bearing:", landBearingLabel(lb)),
				landBesideLeadRow(cells, lb),
				landMissRow(lb),
			}
		}
	}
	return []string{
		title,
		// #478 A4: "closing:" -> "close:" and "rel" -> "rel:" (tighter
		// labels, gaps re-derived in targetCols); all three readings stay
		// (rejected: dropping rel while docked, since the box would only
		// narrow in the phase the player is least likely to be reading it).
		chipRow3(targetCols, "range:", cells.rangeV, "close:", cells.closingV, "rel:", cells.relV),
		chipRow3(targetCols, readout.LabelAp, cells.apV, readout.LabelPe, cells.peV, "incl:", cells.inclV),
		chipRow2(targetCols, readout.LabelDeltaIncl, cells.deltaInclV, "lead:", cells.leadV),
		cells.lastRow(),
	}
}

// lastRow is the TCA / approach row, or the plan arrival / plan separation row
// while a Rendezvous Plan is being flown (same columns, so the box never
// shifts).
func (c targetBoxCells) lastRow() string {
	if c.planned {
		return chipRow2(targetCols, "plan:", c.tcaV, "miss:", c.approachV)
	}
	return chipRow2(targetCols, readout.LabelTCA, c.tcaV, "approach:", c.approachV)
}

// planCells reads the active vessel's Rendezvous Plan (World.RendezvousPlanReading)
// as TARGET cells. The 4 h closest-approach search cannot see a rendezvous
// further out than its window, so while a plan is planted or was the last
// burn flown toward this target, the pilot reads the plan (Wave B review
// MEDIUM 2).
func (v *OrbitView) planCells(w *sim.World) (tcaV, sepV string, ok bool) {
	arrival, sep, ok := w.RendezvousPlanReading()
	if !ok {
		return "", "", false
	}
	return readout.Countdown(arrival.Sub(w.Clock.SimTime)), readout.Distance(sep), true
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
		planT, planSep, planOK := v.planCells(w)
		switch {
		case planOK:
			cells.tcaV, cells.approachV, cells.planned = planT, planSep, true
		case c.ActiveBurn != nil:
			cells.tcaV = v.theme.Dim.Render("recomputing…")
		case craftHasOrbit(tc):
			cells.tcaV, cells.approachV = v.closestApproachCells(w, c)
		}
		// ADR 0053: DOCK READY is the port gate holding (0.5 m / 0.1 m/s /
		// 4 degrees at the ports), the predicate the Proximity ring and the
		// dock itself read, not the old 50 m / 0.1 m/s pair.
		if st, ok := w.ProximityState(); ok && w.ProximityDockGateReady(st) {
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
	// A parked ghost's V = omega x R only looks like an ellipse (the
	// craftHasOrbit trap, #375): Ap/Pe/incl stay dashed (ADR 0066).
	if !g.Landed && gEl.A > 0 && !math.IsNaN(gEl.A) && !math.IsInf(gEl.A, 0) {
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
			if diLabel, ok := v.deltaInclLabel(nCraft, nTarget, g.Landed); ok {
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
	if gPrimary.ID == c.Primary.ID && !g.Landed {
		if planT, planSep, planOK := v.planCells(w); planOK {
			cells.tcaV, cells.approachV, cells.planned = planT, planSep, true
		} else {
			cells.tcaV, cells.approachV = v.closestApproachCells(w, c)
		}
	}
	return name, cells
}

// closestApproachCells is closestApproachRows' value-returning twin: the
// same NextClosestApproach prediction (shared with the map's ✕ marker
// via closestApproachHorizonSec), returned as bare (TCA, approach)
// values instead of pre-formatted rows, so the flattened box can place
// them in its own two-per-row cells. Gated on rule C by the caller
// before these values are used: each reads the dash placeholder when
// the prediction isn't available.
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
	if planner.ApproachBeyond(tCA, closestApproachHorizonSec) {
		return "> 4h", "—" // still closing when the search ends
	}
	return readout.Countdown(time.Duration(tCA * float64(time.Second))), readout.Distance(distCA)
}

// targetSiteCells is TARGET's surface-site branch (ADR 0066): range, closing and
// rel against the site's surface point, and the plane difference; no orbit of
// its own, so Ap/Pe/incl stay dashes. The landed branch's rows are the
// land-beside readouts, swapped in by buildTargetBox.
func (v *OrbitView) targetSiteCells(w *sim.World, c *spacecraft.Spacecraft) (string, targetBoxCells) {
	cells := dashTargetBoxCells()
	name := w.TargetName()
	if !w.TargetSiteResolves() {
		return name + " (not yet resolved)", cells
	}
	if nTarget, ok := w.TargetPlaneNormal(); ok {
		if nCraft, ok := craftOrbitNormalForRelativeIncl(c); ok {
			if diLabel, ok := v.deltaInclLabel(nCraft, nTarget, true); ok {
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
	var closing float64
	if rangeM > 0 {
		closing = -rRel.Dot(vRelVec) / rangeM
	}
	cells.rangeV = readout.Distance(rangeM)
	cells.relV = readout.Speed(vRelVec.Norm())
	cells.closingV = readout.SignedSpeed(closing)
	return name, cells
}

// landBesideLeadRow is the Δincl / lead row; the lead: cell becomes lpd: the
// moment the path meets the ground (a landed target still times a launch, so
// before that it reads lead: as today).
func landBesideLeadRow(cells targetBoxCells, lb sim.LandBeside) string {
	if !lb.ImpactOK {
		return chipRow2(targetCols, readout.LabelDeltaIncl, cells.deltaInclV, "lead:", cells.leadV)
	}
	lpd := "—"
	if lb.LPDOK {
		lpd = fmt.Sprintf("%.0f°", lb.LPDDeg)
	}
	return chipRow2(targetCols, readout.LabelDeltaIncl, cells.deltaInclV, "lpd:", lpd)
}

func landBearingLabel(lb sim.LandBeside) string {
	if !lb.BearingOK {
		return "—"
	}
	return fmt.Sprintf("%.0f°", lb.BearingDeg)
}

func landMissRow(lb sim.LandBeside) string {
	return chipRow2(targetCols, "miss:", landMissLabel(lb), "cross:", landCrossLabel(lb))
}

// landMissLabel reads the impact point along the track: + beyond the site
// (long), U+2212 short of it; exactly on it has no side, like cross:. A sign,
// not a word, so cross: keeps the column bearing: starts on at 10 km and up.
func landMissLabel(lb sim.LandBeside) string {
	if !lb.MissOK {
		return "—"
	}
	switch {
	case roundsToZero(lb.MissM):
	case lb.MissM > 0:
		return "+" + readout.Distance(lb.MissM)
	case lb.MissM < 0:
		return "\u2212" + readout.Distance(-lb.MissM)
	}
	return readout.Distance(0)
}

// roundsToZero: the value prints as 0 m, so it has no side (round first, then
// pick the sign: no "\u22120 m").
func roundsToZero(m float64) bool { return readout.Distance(math.Abs(m)) == readout.Distance(0) }

// planTag marks miss:/cross: that read a planted burn's stop point, not yet flown.
func planTag(lb sim.LandBeside) string {
	if lb.Planned {
		return " (plan)"
	}
	return ""
}

func landCrossLabel(lb sim.LandBeside) string {
	if !lb.MissOK {
		return "—"
	}
	switch {
	case roundsToZero(lb.CrossM):
	case lb.CrossM > 0:
		return readout.Distance(lb.CrossM) + " R" + planTag(lb)
	case lb.CrossM < 0:
		return readout.Distance(-lb.CrossM) + " L" + planTag(lb)
	}
	return readout.Distance(0) + planTag(lb)
}
