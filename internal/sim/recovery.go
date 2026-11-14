// Package sim - booster recovery (ADR 0069 decisions 12, 13, 15): the pad's
// `recover:` choice per stage, the drone ship stationed downrange, the flight
// home that starts at the drop, and the cutoff at the landing reserve. One-line
// reason for a new file: a new production concept beside the staging chain and
// the LZ model, not a slice of either.

package sim

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Playtest-tuned constants (ADR 0069 decisions 12, 13, 15).
const (
	// RecoveryLZMaxM is how far from the pad the nearest LZ site may be and
	// still be offered on the `recover:` row (LC-13 is 14.8 km from the Cape pad).
	RecoveryLZMaxM = 100e3
	// RecoveryShipDefaultKm is the typed distance a new `ship` choice starts at.
	RecoveryShipDefaultKm = 250.0
	// RecoveryShipMaxKm bounds the typed distance: past a quarter of the body
	// round the downrange point stops meaning anything.
	RecoveryShipMaxKm = 3000.0
	// RecoveryCutLeadS cuts the engines this long before the reserve is
	// predicted to be reached, so a tick never lands past it.
	RecoveryCutLeadS = 0.25
	// RecoveryPadKg is spare propellant the cutoff keeps over the plan's own
	// margin. The solve's slack is noisy by about 0.5 t (its tolerances are metres
	// on a 600 km fall) and the real N at the drop reads a little less than the
	// copy the cutoff solved (the booster's own slew, the computer's site error): a
	// stage cut on 1 t was refused at the drop for Falcon Heavy's boosters.
	RecoveryPadKg = 2000.0
	// recoveryRetrySec is how long the climb waits to solve again while the
	// stage is still too heavy to land (no plan closes yet); inside the window
	// the interval follows the slack, down to recoveryMinSec.
	recoveryRetrySec = 3.0
	recoveryHeavySec = 1.0 // the cheap weight test is repeated this often
	recoveryMinSec   = 1.0
	recoveryShipSlow = 3.0 // multiplies the two intervals above for a ship
	recoveryMaxSec   = 6.0
)

// RecoverPick is the pad's choice for one stage (ADR 0069 decision 12).
type RecoverPick = spacecraft.RecoverPick

// RecoverableStages are the indices of the stages the `recover:` row offers:
// the bottom of the stack up to the last stage that can land under its own power
// (decision 11: legs, a command source and an engine), never the top stage (it
// is what keeps flying). A Saturn's lander stage sits above stages that cannot
// land, so it is not offered; Falcon Heavy offers its side boosters and its core.
func RecoverableStages(stages []spacecraft.Stage) []int {
	var out []int
	for i := 0; i < len(stages)-1; i++ {
		if !stages[i].CanRecover() {
			break
		}
		out = append(out, i)
	}
	return out
}

// RecoveryLZNear is the LZ site the pad's `LZ` choice means: the nearest site
// (yours or a shipped zone, never a wreck) on the body within RecoveryLZMaxM of
// the pad. ok is false when there is none, and the row then offers ship and none.
// need is how many LZs of that site must be free (one per half of a booster pair,
// 0 for the site whether or not it has room): a full site is not an LZ choice (R2).
func (w *World) RecoveryLZNear(bodyID string, latDeg, lonDeg float64, need int) (Site, bool) {
	sys := w.System()
	b := sys.FindBody(bodyID)
	if b == nil {
		return Site{}, false
	}
	best, bestD := Site{}, RecoveryLZMaxM
	for _, s := range w.AllSites() {
		if s.Body != b.EnglishName || s.System != sys.Name || s.Kind == SiteWreck || s.Owner != "" {
			continue
		}
		if d := HaversineM(latDeg, lonDeg, s.LatDeg, s.LonDeg, b.RadiusMeters()); d <= bestD {
			best, bestD = s, d
		}
	}
	return best, best.ID != 0 && w.freeLZs(best) >= need
}

// freeLZs counts the LZs of s nobody holds.
func (w *World) freeLZs(s Site) (free int) {
	for n := 1; n <= s.LZs(); n++ {
		if !w.lzTaken(s, n, nil) {
			free++
		}
	}
	return free
}

// applyRecovery stamps the pad's choices on the stages of a freshly spawned
// vessel: an LZ choice resolves to the nearest LZ site, a ship choice stations
// a ship (decision 13). A pick for a stage that cannot be recovered, or an LZ
// with no site near the pad, is dropped (recovery none).
func (w *World) applyRecovery(c *spacecraft.Spacecraft, picks []RecoverPick, latDeg, lonDeg float64) {
	elig := RecoverableStages(c.Stages)
	claimed := map[uint64]int{} // LZs the earlier stages of this vessel already count on, per site
	for _, i := range elig {
		if i >= len(picks) {
			break
		}
		st := &c.Stages[i]
		switch p := picks[i]; p.Mode {
		case spacecraft.RecoverLZ:
			halves := StageHalves(*st)
			if s, ok := w.RecoveryLZNear(c.Primary.ID, latDeg, lonDeg, halves); ok && w.freeLZs(s) >= claimed[s.ID]+halves {
				st.RecoverMode, st.RecoverSiteID = spacecraft.RecoverLZ, s.ID
				claimed[s.ID] += halves
			}
		case spacecraft.RecoverShip:
			if p.ShipKm > 0 {
				st.RecoverMode, st.RecoverSiteID = spacecraft.RecoverShip, w.stationShip(c, latDeg, lonDeg, math.Min(p.ShipKm, RecoveryShipMaxKm), StageHalves(*st))
			}
		}
	}
}

// StageHalves is how many vessels a stage drops as: two for a booster pair.
func StageHalves(st spacecraft.Stage) int {
	if st.LitWithAbove {
		return 2
	}
	return 1
}

// stationShip creates a drone ship km downrange of the pad along the launch
// heading (due east, the way the pad's ascent goes) as a site with one LZ, named
// "Drone Ship" and numbered on repeat. It does not move (decision 13). A ship for
// a booster pair has an LZ per half (R1).
func (w *World) stationShip(c *spacecraft.Spacecraft, latDeg, lonDeg, km float64, lzs int) uint64 {
	lat, lon := downrangePoint(latDeg, lonDeg, 90, km*1e3, c.Primary.RadiusMeters())
	base := "Drone Ship"
	if w.NameCounters == nil {
		w.NameCounters = map[string]uint64{}
	}
	n := w.NameCounters[base] + 1
	w.NameCounters[base] = n
	name := base
	if n > 1 {
		name = fmt.Sprintf("%s %d", base, n)
	}
	sys := w.System()
	if c.SystemIdx >= 0 && c.SystemIdx < len(w.Systems) {
		sys = w.Systems[c.SystemIdx]
	}
	w.NextSiteID++
	now := time.Now().UnixNano()
	w.Sites = append(w.Sites, Site{
		ID: w.NextSiteID, Body: c.Primary.EnglishName, System: sys.Name,
		LatDeg: lat, LonDeg: lon, Kind: SitePoint, Name: name, CraftID: c.ID, LZCount: lzs,
		PlantedAtNano: now, UpdatedAtNano: now,
	})
	return w.NextSiteID
}

// downrangePoint is the point dist metres from (lat, lon) along the great circle
// leaving at bearingDeg (clockwise from north), on a sphere of the given radius.
func downrangePoint(latDeg, lonDeg, bearingDeg, dist, radius float64) (lat, lon float64) {
	d, th, p1, l1 := dist/radius, bearingDeg*rad, latDeg*rad, lonDeg*rad
	p2 := math.Asin(math.Sin(p1)*math.Cos(d) + math.Cos(p1)*math.Sin(d)*math.Cos(th))
	l2 := l1 + math.Atan2(math.Sin(th)*math.Sin(d)*math.Cos(p1), math.Cos(d)-math.Sin(p1)*math.Sin(p2))
	return p2 / rad, wrapDeg(l2 / rad)
}

// RecoveryEvent is the Event Flash for a stage that could not be sent home at
// the drop: the refusal N gave, a fact.
type RecoveryEvent struct {
	CraftName string
	Refusal   string
}

// asVessel runs fn with c as the Active Vessel (target and frame included) and
// puts the pilot's vessel back: the way N is pressed on a booster the pilot is
// not flying. Nothing the switch key does beyond that (recording swap, camera)
// happens.
func (w *World) asVessel(c *spacecraft.Spacecraft, fn func()) {
	idx := -1
	for i, x := range w.Crafts {
		if x == c {
			idx = i
		}
	}
	if idx < 0 {
		return
	}
	prevIdx, prevTarget, prevNav := w.ActiveCraftIdx, w.Target, w.NavMode
	if out := w.ActiveCraft(); out != nil {
		out.Target, out.NavFrame = w.Target, int(w.NavMode)
	}
	w.ActiveCraftIdx, w.Target, w.NavMode = idx, c.Target, NavMode(c.NavFrame)
	fn()
	c.Target, c.NavFrame = w.Target, int(w.NavMode)
	w.ActiveCraftIdx, w.Target, w.NavMode = prevIdx, prevTarget, prevNav
}

// flyHome is the drop's half of decision 12: the stage takes its recovery place
// as its own target and N is pressed on it, with the core's target untouched. A
// refusal leaves the stage a vessel with its target set (], t, N is as today)
// and says what N said.
func (w *World) flyHome(b *spacecraft.Spacecraft) {
	st := b.Stages[0]
	if st.RecoverMode == spacecraft.RecoverNone {
		return
	}
	if _, ok := w.siteByID(st.RecoverSiteID); !ok {
		w.PendingRecoveryEvents = append(w.PendingRecoveryEvents, RecoveryEvent{b.Name, "recovery site gone"})
		return
	}
	w.RecomputeCommGraph() // the stage is a new vessel: its link is part of N's refusals
	w.skipShortfall = true // R6: the drop's refusal is the plain one; N on the stage gives the figure
	defer func() { w.skipShortfall = false }()
	w.asVessel(b, func() {
		w.SetTargetSite(st.RecoverSiteID)
		if _, err := w.PlanDeorbitAndEngage(); err != nil {
			w.PendingRecoveryEvents = append(w.PendingRecoveryEvents, RecoveryEvent{b.Name, err.Error()})
			w.recordRowFor(b, "recovery: "+err.Error())
		}
	})
}

// recoveryRuntime is one vessel's cutoff solver state (transient, never saved).
type recoveryRuntime struct {
	nextSolve    time.Time
	seenFeasible bool // a plan has closed on this climb: a refusal now means the reserve is passed
	at, prevAt   time.Time
	slack        float64 // kg of propellant the plan had to spare at `at`
	prevSlack    float64
	havePrev     bool
	rate         float64 // kg of slack lost per second, measured between solves
	haveCut      bool    // a closing plan has been solved: the cutoff can be predicted
	warmDV       float64 // the last closing plan's burn size: the next solve starts there
	rising       bool    // the aim needed the full scan (a ship): the next solves window it
	light        bool    // the weight test has passed: a plan can close from here (N1: warp is capped for a ship)
}

// recoveryShipWarpCap is the warp a ship pick's climb is held to between the weight
// test passing and the cutoff. A ship's closing window can be shorter than ten sim
// seconds, so the solves (a real second apart at most) must be 3 sim seconds apart
// there, which is a real second at this warp (N1).
const recoveryShipWarpCap = 3.0

// recoveryWarpCap is the ceiling the climb's ship pick puts on warp, 0 for none.
func (w *World) recoveryWarpCap() float64 {
	for _, c := range w.Crafts {
		if c == nil || len(c.Stages) == 0 || c.Stages[0].RecoverMode != spacecraft.RecoverShip || c.Stages[0].HeldKg > 0 || c.ManualBurn == nil {
			continue
		}
		if rt := w.recRT[c.ID]; rt != nil && rt.light {
			return recoveryShipWarpCap
		}
	}
	return 0
}

func (w *World) recoveryRTFor(c *spacecraft.Spacecraft) *recoveryRuntime {
	if w.recRT == nil {
		w.recRT = map[uint64]*recoveryRuntime{}
	}
	rt := w.recRT[c.ID]
	if rt == nil {
		rt = &recoveryRuntime{}
		w.recRT[c.ID] = rt
	}
	return rt
}

// RecoveryCutIn is the seconds until the stage's engines cut off at the landing
// reserve, for the badge's T-. ok is false until a solve has found the window.
func (w *World) RecoveryCutIn(c *spacecraft.Spacecraft) (secs float64, ok bool) {
	if c == nil || w.recRT == nil || len(c.Stages) == 0 || c.Stages[0].RecoverMode == spacecraft.RecoverNone {
		return 0, false
	}
	rt := w.recRT[c.ID]
	if rt == nil || !rt.haveCut || rt.rate <= 0 {
		return 0, false
	}
	return math.Max(0, rt.slack/rt.rate-w.Clock.SimTime.Sub(rt.at).Seconds()), true
}

// errRecoveryHeavy: the stage is too heavy for any plan to land it, so the
// solve was not run.
var errRecoveryHeavy = errors.New("too heavy to land")

// recoveryTooHeavy is the cheap test run before the solve (the solve costs 50
// to 300 ms and most of a climb is spent above the window): the plan's entry
// burn is at most entryDVMax, so a stage that is still heavier than one landing
// engine can hover after it cannot be landed by any plan. It is a necessary
// condition only; the solve decides the rest.
func recoveryTooHeavy(b *spacecraft.Spacecraft) bool {
	r := b.Primary.RadiusMeters()
	if b.Thrust <= 0 || b.Isp <= 0 || r <= 0 {
		return false
	}
	hover := b.Thrust * brakeThrottle(b) / (b.Primary.GravitationalParameter() / (r * r))
	return b.TotalMass()*math.Exp(-entryDVMax/(b.Isp*stdGravityMps2)) > hover
}

// recoverySlack solves the way home for the bottom stage as if it were dropped
// now with fuel in its tank: decision 14's boostback solve, run on a copy.
// slack is the propellant the plan leaves above the landing margin; err says why
// no plan closes. It plants nothing and does not run the shortfall search.
func (w *World) recoverySlack(c *spacecraft.Spacecraft, fuel, warm float64, rising bool) (slack, dv float64, risen bool, err error) {
	st := c.Stages[0]
	st.FuelMass = fuel
	if st.LitWithAbove { // a pair drops as two halves; one half has the same need
		st = splitBoosterStage(st)[0]
	}
	site, ok := w.siteByID(st.RecoverSiteID)
	if !ok {
		return 0, 0, rising, ErrDeorbitNoSite
	}
	b := buildJettisonedCraft([]spacecraft.Stage{st}, c)
	if recoveryTooHeavy(b) {
		return 0, 0, rising, errRecoveryHeavy
	}
	w.recoverySolves++ // the solve itself, not the weight test
	b.State = c.State
	b.State.M = b.TotalMass()
	mu := b.Primary.GravitationalParameter()
	lead := boostbackLead(b)
	tS := w.Clock.SimTime.Add(time.Duration(lead * float64(time.Second)))
	st0, ok := physics.KeplerStep(b.State, mu, lead)
	if !ok {
		return 0, 0, rising, ErrBoostbackNoSolution
	}
	st0.M = b.TotalMass()
	lat, lon := site.lzLatLon(1, b.Primary.RadiusMeters())
	w.boostWarmDV, w.boostRising, w.boostRisingSeen = warm, rising, false
	sol, err := w.solveBoostback(b, st0, tS, [2]float64{lat, lon})
	risen = rising || w.boostRisingSeen
	w.boostWarmDV, w.boostRising, w.boostRisingSeen = 0, false, false
	if err != nil {
		return 0, 0, risen, err
	}
	return sol.off.fuelLeft - BoostbackFuelMarginKg, sol.dv, risen, nil
}

// recoveryCutoff is decision 15, run once a tick on every vessel: while the
// bottom stage burns with a recovery place set, solve the way home at most once
// a second (sparser while far from the reserve, see the intervals above), predict
// from the slack where the reserve is reached, and cut the engines there. The
// reserve is moved out of the tank (Stage.HoldReserve) so the engine reads dry;
// `space` still drops the stage and the drop gives it back.
func (w *World) recoveryCutoff(c *spacecraft.Spacecraft, simDelta time.Duration) {
	if len(c.Stages) == 0 || c.Stages[0].RecoverMode == spacecraft.RecoverNone || c.Landed || c.Crashed || c.ManualBurn == nil {
		return
	}
	st := &c.Stages[0]
	if st.FuelMass <= 0 || st.Thrust <= 0 || st.Isp <= 0 || (len(c.DecouplePlan) > 0 && c.DecouplePlan[0] > 1) {
		return
	}
	if site, ok := w.siteByID(st.RecoverSiteID); ok && w.freeLZs(site) < StageHalves(*st) {
		return // R2: the LZs filled since the pad; a held reserve would buy a refused drop
	}
	rt := w.recoveryRTFor(c)
	now := w.Clock.SimTime
	tickWarp := float64(simDelta) / float64(w.Clock.BaseStep) // sim seconds per real second at this tick's warp
	if st.RecoverMode == spacecraft.RecoverShip {
		tickWarp = math.Min(tickWarp, recoveryShipWarpCap) // the climb is capped from here on (N1)
	}
	near := func() bool { // the line says the reserve is reached within this tick plus the lead
		ahead := now.Sub(rt.at).Seconds() + simDelta.Seconds() + RecoveryCutLeadS
		return rt.haveCut && rt.rate > 0 && rt.slack-rt.rate*ahead <= RecoveryPadKg
	}
	if !now.Before(rt.nextSolve) {
		slack, dv, rising, err := w.recoverySlack(c, st.FuelMass, rt.warmDV, rt.rising)
		rt.rising = rising
		rt.light = rt.light || !errors.Is(err, errRecoveryHeavy)
		switch {
		case err == nil:
			if rt.haveCut {
				rt.prevSlack, rt.prevAt, rt.havePrev = rt.slack, rt.at, true
			}
			rt.slack, rt.at, rt.seenFeasible, rt.haveCut, rt.warmDV = slack, now, true, true, dv
			if rt.havePrev && now.After(rt.prevAt) {
				rt.rate = (rt.prevSlack - slack) / now.Sub(rt.prevAt).Seconds()
			}
			if rt.rate <= 0 { // first solve: the plan loses about 0.4 of each kg the stage burns
				rt.rate = 0.4 * st.Thrust / (st.Isp * stdGravityMps2)
			}
			// a ship's solve scans for its root (0.4 s against an LZ's 0.1 s), so it is
			// asked less often; the pad covers what the longer gap costs in precision
			slow := 1.0
			if st.RecoverMode == spacecraft.RecoverShip {
				slow = recoveryShipSlow
			}
			dt := recoveryMinSec * slow
			if rt.rate > 0 {
				dt = math.Min(recoveryMaxSec, math.Max(recoveryMinSec*slow, 0.4*slack/rt.rate))
			}
			// the schedule is sim time, a solve stalls the tick in real time: at 10x
			// the same gap came ten times as often (R5). One per real second at most,
			// a tick being BaseStep of real time.
			dt = math.Max(dt, recoveryMinSec*tickWarp)
			rt.nextSolve = now.Add(time.Duration(dt * float64(time.Second)))
		case rt.seenFeasible:
			// a plan closed before and none does now: the reserve was passed between
			// solves (a load mid-climb, a throttle change). Cut at once.
			w.cutAtReserve(c)
			return
		case errors.Is(err, errRecoveryHeavy):
			rt.nextSolve = now.Add(time.Duration(recoveryHeavySec * float64(time.Second)))
		default:
			rt.nextSolve = now.Add(time.Duration(math.Max(recoveryRetrySec, recoveryMinSec*tickWarp) * float64(time.Second)))
		}
	}
	if near() {
		w.cutAtReserve(c)
	}
}

// cutAtReserve holds the bottom stage's remaining propellant back and tells the
// recording: MECO for a stage of the core, BECO for side boosters.
func (w *World) cutAtReserve(c *spacecraft.Spacecraft) {
	st := &c.Stages[0]
	word := CutoffWord(*st)
	st.HoldReserve()
	c.SyncFields()
	c.State.M = c.TotalMass()
	delete(w.recRT, c.ID)
	w.recordRowFor(c, fmt.Sprintf("%s: %.0f kg held", word, st.HeldKg))
}

// CutoffWord is the real call-out for a stage's engines cutting off: BECO for
// side boosters (the marked stage), MECO for the core.
func CutoffWord(st spacecraft.Stage) string {
	if st.LitWithAbove {
		return "BECO"
	}
	return "MECO"
}
