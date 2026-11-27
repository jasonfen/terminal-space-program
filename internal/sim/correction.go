package sim

import (
	"fmt"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/recorder"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

const (
	// CorrectionMinErrM: a boostback that leaves the stop this far from the site
	// gets one short correction burn behind it. The boostback is planned in air
	// the solver does not model, so it is never exact.
	CorrectionMinErrM = 150.0
	// correctionProbeMps is the impulse the two probe forecasts use.
	correctionProbeMps = 5.0
)

// correctionPlan is one short burn that moves the stop onto the site.
type correctionPlan struct {
	dir            orbital.Vec3
	dv             float64
	lead           float64
	beforeM, after float64 // the stop's error before and as forecast after
}

// pendingEntryBurn is the entry burn still queued behind the boostback, as the
// solve's entryBurn (zero when none).
func pendingEntryBurn(c *spacecraft.Spacecraft, now time.Time) entryBurn {
	for _, n := range c.Nodes {
		if n.AdvisoryKey != AdvisoryKeyEntry {
			continue
		}
		start := n.TriggerTime.Sub(now) - n.Duration/2
		st, ok := physics.KeplerStep(c.State, c.Primary.GravitationalParameter(), math.Max(start.Seconds(), 0))
		if !ok {
			return entryBurn{}
		}
		return entryBurn{dv: n.DV, alt: st.R.Norm() - c.Primary.RadiusMeters()}
	}
	return entryBurn{}
}

// correctionFor forecasts where c now stops (the stop forecast, with the entry
// burn still to fly) and, when that is over CorrectionMinErrM from the site,
// finds the impulse that zeroes it: a baseline and two probe forecasts give the
// slope, one forecast checks the answer, one more refines it. A burn the tank
// cannot pay for is cut to what it can pay (the plan's margin stays). ok=false:
// no correction due, or none that helps.
func (w *World) correctionFor(c *spacecraft.Spacecraft, site [2]float64) (cp correctionPlan, ok bool) {
	mu, now := c.Primary.GravitationalParameter(), w.Clock.SimTime
	lead := BoostbackMinLeadSec
	tS := now.Add(time.Duration(lead * float64(time.Second)))
	st0, kok := physics.KeplerStep(c.State, mu, lead)
	if !kok || c.Isp <= 0 {
		return cp, false
	}
	st0.M = c.TotalMass()
	entry := pendingEntryBurn(c, now)
	up := st0.R.Unit()
	tH := st0.V.Sub(up.Scale(st0.V.Dot(up))).Unit()
	hH := st0.R.Cross(st0.V).Unit()
	ve := c.Isp * stdGravityMps2
	probe := func(a, b float64) (l, x, fuel float64, ok bool) {
		imp := tH.Scale(a).Add(hH.Scale(b))
		s := st0
		s.V = s.V.Add(imp)
		s.M = st0.M * math.Exp(-imp.Norm()/ve)
		w.aimEvals++
		off, status := w.aimFrom(c, st0, tS, s, tS, afterBurnCraft(c, s), true, boostAim, entry, site)
		return off.long, off.cross, off.fuelLeft, status == 0 && off.valid
	}
	l0, x0, fuel0, ok0 := probe(0, 0)
	if !ok0 || math.Hypot(l0, x0) < CorrectionMinErrM {
		return cp, false
	}
	e := correctionProbeMps
	l1, x1, _, ok1 := probe(e, 0)
	l2, x2, _, ok2 := probe(0, e)
	if !ok1 || !ok2 {
		return cp, false
	}
	j11, j21, j12, j22 := (l1-l0)/e, (x1-x0)/e, (l2-l0)/e, (x2-x0)/e
	det := j11*j22 - j12*j21
	if math.Abs(det) < 1e-9 {
		return cp, false
	}
	solve := func(l, x float64) (da, db float64) { // J d = -(l, x)
		return (-l*j22 + x*j12) / det, (-x*j11 + l*j21) / det
	}
	a, b := solve(l0, x0)
	best, bestA, bestB := math.Hypot(l0, x0), 0.0, 0.0
	for i := 0; i < 2; i++ {
		l, x, _, okp := probe(a, b)
		if !okp {
			break
		}
		if r := math.Hypot(l, x); r < best {
			best, bestA, bestB = r, a, b
		}
		if math.Hypot(l, x) < CorrectionMinErrM/3 {
			break
		}
		da, db := solve(l, x)
		a, b = a+da, b+db
	}
	if bestA == 0 && bestB == 0 {
		return cp, false
	}
	dv := math.Hypot(bestA, bestB)
	if burned := st0.M * (1 - math.Exp(-dv/ve)); burned > 0 {
		allowed := fuel0 - BoostbackFuelMarginKg
		if allowed <= 0 {
			return cp, false
		}
		if burned > allowed {
			k := allowed / burned
			bestA, bestB, dv = bestA*k, bestB*k, dv*k
		}
	}
	dir := tH.Scale(bestA).Add(hH.Scale(bestB)).Unit()
	return correctionPlan{dir: dir, dv: dv, lead: lead, beforeM: math.Hypot(l0, x0), after: best}, true
}

// planCorrection plants the correction burn for c toward the site (lat, lon),
// when one is due, and records it. The caller has made c the Active Vessel.
func (w *World) planCorrection(c *spacecraft.Spacecraft, lat, lon float64) {
	lat, lon = w.computerSiteLatLon(c, lat, lon)
	cp, ok := w.correctionFor(c, [2]float64{lat, lon})
	if !ok || cp.dv <= 0 {
		return
	}
	// the burn starts once the nose has slewed to it, as the boostback's does
	cur := c.CurrentAttitudeDir.Unit()
	lead := cp.lead
	if cur.Norm() != 0 {
		ang := math.Acos(math.Max(-1, math.Min(1, cur.Dot(cp.dir))))
		lead = math.Max(BoostbackMinLeadSec, nodeLeadSlack*ang/c.SlewRateRad()+BoostbackLeadPadSec)
	}
	tS := w.Clock.SimTime.Add(time.Duration(lead * float64(time.Second)))
	n := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnVector,
		BurnDirUnit: cp.dir,
		DV:          cp.dv,
		Event:       spacecraft.TriggerAbsolute,
		PrimaryID:   c.Primary.ID,
		AdvisoryKey: AdvisoryKeyCorrection,
	}
	n.Duration = c.BurnTimeForDV(cp.dv)
	n.TriggerTime = tS.Add(n.Duration / 2)
	w.replaceAdvisoryNode(c, AdvisoryKeyCorrection)
	w.PlanNode(n)
	w.recordRow(fmt.Sprintf("correction: Δv %s m/s in %s, stop %s off", recorder.Grouped(cp.dv),
		recorder.TPlus(time.Duration(lead*float64(time.Second))), readout.Distance(cp.beforeM)))
}

// correctAfterBoostback is the one look at the fall once the boostback is flown
// (the autopilot's coast tick): a stop over CorrectionMinErrM off the site gets
// its correction burn planted.
func (w *World) correctAfterBoostback(c *spacecraft.Spacecraft, lat, lon float64) {
	if !recovering(c) { // N's deorbit-from-orbit carries the boostback's key (#1010)
		return
	}
	rt := w.apRuntimeFor(c)
	if _, pending := deorbitNode(c); pending {
		rt.sawBoost = true
		return
	}
	if !rt.sawBoost || rt.corrected || c.Landing == nil || c.Landing.Nose {
		return
	}
	rt.corrected = true
	w.asVessel(c, func() { w.planCorrection(c, lat, lon) })
}
