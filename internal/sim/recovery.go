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
	"github.com/jasonfen/terminal-space-program/internal/render"
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
	// recoveryThreeAfterFails is how many failed one-engine solves (3 s apart for a
	// ship) the fallback waits out before it prices three engines, unless the solve
	// says no boostback lands at all (the Falcon Heavy core says so from its first
	// solve and its three-engine window is open at once). A plan that is going to
	// close on one engine mostly does within four failures (measured), and the first
	// three-engine plan that closes costs about 1,300 aim flights, more than the
	// ship-climb cap leaves (TestRecoveryReserveSolvesAreCheapAndSpaced).
	recoveryThreeAfterFails = 6
	// RecoveryThreePadKg is RecoveryPadKg for a three-engine cut whose one-engine solve
	// cannot even aim (ErrBoostbackNoSolution). Its three-engine window shuts fast as
	// the stage coasts on after the cut: on the flown +5/+10 degree climb a cut on 2.2 t
	// of slack was refused 1.5 s later, cuts on 3.4 t and up landed for every drop
	// delay from 0 to 15 s. Staging is the pilot's press, so the cut carries the
	// margin. Stages whose one-engine solve aims but lights too high (the Falcon Heavy
	// boosters) keep RecoveryPadKg: a bigger pad cut them before one engine closes.
	RecoveryThreePadKg = 4000.0
	// RecoveryStageGraceS is how long after a three-engine cut the plan must still
	// close: the pilot presses staging when they like (flown: 2 s after the cut).
	RecoveryStageGraceS = 5.0
	// recoveryThreeMaxSec is the longest the three-engine fallback's line goes unsolved.
	recoveryThreeMaxSec = 30.0

	// ThreeEngineLandingThrottle is the landing throttle (ADR 0071 decision 1) a
	// stage falls back to when its one-engine reserve solve cannot close: three
	// engines of nine. Playtest-tuned; the condition is "the one-engine solve
	// fails", tried only after the weight test passed, in recoverySlack.
	ThreeEngineLandingThrottle = 3.0 / 9
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
			km, mode := math.Min(p.ShipKm, RecoveryShipMaxKm), spacecraft.RecoverShip
			if km <= 0 { // `ship auto`: distance 0 on the pick, no new saved field (ADR 0071 decision 3)
				km, mode = RecoveryShipDefaultKm, spacecraft.RecoverShipAuto
			}
			st.RecoverMode, st.RecoverSiteID = mode, w.stationShip(c, latDeg, lonDeg, km, StageHalves(*st))
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
	lost         bool    // a plan closed on this climb and a later solve found none: the cut waits for one to close again
	lostErr      error
	three        threeEngineLine // the fallback's own cutoff line; the fields above are never touched by it
	aimLimited   bool            // the last failed one-engine solve found no boostback that aims at the site
	light        bool            // the weight test has passed: a plan can close from here (N1: warp is capped for a ship)
	shipAt       time.Time       // when the ship last followed the heading (ADR 0071 decision 4)
	onFall       bool            // `ship auto`: a landing has worked, the ship now takes the forecast stop
	autoThree    bool            // `ship auto`: the last closing forecast needed three engines
}

// threeEngineLine is ADR 0071 decision 1's stand-in for the cutoff line while the
// one-engine solve has not closed: the same plan priced on three engines, solved
// whenever the one-engine solve fails (so on its schedule, adding no solves of its
// own beyond that one), with its own slack and rate so a stage whose one-engine plan
// closes later flies exactly as it did before.
type threeEngineLine struct {
	have, havePrev, rising bool
	failed                 int // one-engine solves that failed since the weight test passed
	at, prevAt, nextAt     time.Time
	slack, prevSlack, rate float64
	warmDV                 float64
}

// reached: the line says the reserve is reached within ahead seconds of its last solve.
func (l *threeEngineLine) reached(ahead, pad float64) bool {
	return l.have && l.rate > 0 && l.slack-l.rate*ahead <= pad
}

// pad is the slack the three-engine cut keeps: see RecoveryThreePadKg.
func (rt *recoveryRuntime) threePad() float64 {
	if rt.aimLimited {
		return RecoveryThreePadKg
	}
	return RecoveryPadKg
}

// recoveryShipFollowKm is how far downrange the vessel must be before its ground
// track from the pad is read as the heading; nearer in, the ship stays where it
// was stationed (due east, the bearing stationShip spawns on).
const recoveryShipFollowKm = 5.0

// followHeading keeps a ship pick's site the typed distance (its distance from the
// pad, unchanged by a move) along the vessel's ground track from the pad, until the
// cutoff (ADR 0071 decision 4, #998). Called on the cutoff's solve ticks, at most
// once per ship solve gap, so no extra solves and a bounded number of ledger writes.
func (w *World) followHeading(c *spacecraft.Spacecraft, st *spacecraft.Stage, rt *recoveryRuntime) {
	if !st.RecoverMode.IsShip() || rt.onFall || !c.HasLaunchSite || w.noShipFollow || w.Clock.SimTime.Sub(rt.shipAt).Seconds() < recoveryMinSec*recoveryShipSlow {
		return
	}
	r := c.Primary.RadiusMeters()
	subLat, subLon := render.WorldToBodyFixed(c.Primary, unitV(c.State.R), w.Clock.SimTime)
	if HaversineM(c.LaunchLatDeg, c.LaunchLonDeg, subLat, subLon, r) < recoveryShipFollowKm*1e3 {
		return
	}
	for i := range w.Sites {
		if s := &w.Sites[i]; s.ID == st.RecoverSiteID {
			dist := HaversineM(c.LaunchLatDeg, c.LaunchLonDeg, s.LatDeg, s.LonDeg, r)
			s.LatDeg, s.LonDeg = downrangePoint(c.LaunchLatDeg, c.LaunchLonDeg, ForwardAzimuthDeg(c.LaunchLatDeg, c.LaunchLonDeg, subLat, subLon), dist, r)
			s.UpdatedAtNano = time.Now().UnixNano()
			rt.shipAt = w.Clock.SimTime
		}
	}
}

// recoveryShipWarpCap is the warp a ship pick's climb is held to between the weight
// test passing and the cutoff. A ship's closing window can be shorter than ten sim
// seconds, so the solves (a real second apart at most) must be 3 sim seconds apart
// there, which is a real second at this warp (N1).
const recoveryShipWarpCap = 3.0

// recoveryWarpCap is the ceiling the climb's ship pick puts on warp, 0 for none.
func (w *World) recoveryWarpCap() float64 {
	for _, c := range w.Crafts {
		if c == nil || len(c.Stages) == 0 || !c.Stages[0].RecoverMode.IsShip() || c.Stages[0].HeldKg > 0 || c.ManualBurn == nil {
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
	return tooHeavyAt(b, b.TotalMass())
}

func tooHeavyAt(b *spacecraft.Spacecraft, m float64) bool {
	r := b.Primary.RadiusMeters()
	if b.Thrust <= 0 || b.Isp <= 0 || r <= 0 {
		return false
	}
	hover := b.Thrust * brakeThrottle(b) / (b.Primary.GravitationalParameter() / (r * r))
	return m*math.Exp(-entryDVMax/(b.Isp*stdGravityMps2)) > hover
}

// heavyAfterBoostback is recoveryTooHeavy with the boostback's propellant gone: the
// stage's mass after the least Δv a boostback can burn (the horizontal speed over
// the ground; the equator's rotation is the most the ground can take off it, so the
// floor is under any plan's). It only gates the three-engine probe: a stage the
// one-engine test calls heavy but this does not may have a three-engine window open.
func heavyAfterBoostback(b *spacecraft.Spacecraft, st physics.StateVector, ahead float64) bool {
	r := st.R.Norm()
	if r <= 0 || b.Isp <= 0 {
		return false
	}
	vr := st.V.Dot(st.R) / r
	dv := math.Sqrt(math.Max(0, st.V.Dot(st.V)-vr*vr))
	if t := b.Primary.SideralRotationSeconds(); t > 0 {
		dv -= 2 * math.Pi * b.Primary.RadiusMeters() / t
	}
	m := b.TotalMass()
	// ahead: also still heavy that many sim seconds of burn later, so the one-engine
	// solve's own first (counted) solve is never within a real second of a probe
	if ahead > 0 && !tooHeavyAt(b, m-ahead*b.Thrust/(b.Isp*stdGravityMps2)) {
		return true
	}
	return tooHeavyAt(b, math.Max(m*math.Exp(-math.Max(0, dv)/(b.Isp*stdGravityMps2)), m-b.ActiveStageFuel()))
}

// recoverySlack solves the way home for the bottom stage as if it were dropped
// now with fuel in its tank: decision 14's boostback solve, run on a copy.
// slack is the propellant the plan leaves above the landing margin; err says why
// no plan closes. It plants nothing and does not run the shortfall search.
func (w *World) recoverySlack(c *spacecraft.Spacecraft, fuel, warm float64, rising bool) (slack, dv float64, risen bool, err error) {
	return w.recoverySlackAt(c, 0, fuel, warm, rising, 0, 0)
}

// recoverySlackAt is recoverySlack with the stage's landing throttle set to thr
// on the solve's copy (thr 0 leaves the stage's own): the three-engine fallback
// prices the plan without committing the stage to it. coast is seconds the stage
// flies on unpowered before the plan starts: the pilot's press on staging.
func (w *World) recoverySlackAt(c *spacecraft.Spacecraft, thr, fuel, warm float64, rising bool, coast, probeAhead float64) (slack, dv float64, risen bool, err error) {
	st := c.Stages[0]
	st.FuelMass = fuel
	if thr > 0 && c.Stages[0].LandingThrottle > 0 && c.Stages[0].LandingThrottle < thr && heavyAfterBoostback(buildJettisonedCraft([]spacecraft.Stage{st}, c), c.State, probeAhead) {
		return 0, 0, rising, errRecoveryHeavy
	}
	if thr > 0 {
		st.LandingThrottle = thr
	}
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
	lead := boostbackLead(b) + coast
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

// recoverySlackAuto is the cutoff's question for `ship auto` (ADR 0071 decisions 3
// and 4): dropped RecoveryStageGraceS from now with no boostback, does a landing
// work from this fall, and how much propellant does it leave above the margin. One
// engine is asked first and three only when one cannot (decision 1). When a landing
// works the ship moves to where the fall stops. err says why none does.
func (w *World) recoverySlackAuto(c *spacecraft.Spacecraft, fuel float64, rt *recoveryRuntime) (float64, error) {
	site, ok := w.siteByID(c.Stages[0].RecoverSiteID)
	if !ok {
		return 0, ErrDeorbitNoSite
	}
	var lastErr error = errRecoveryHeavy
	for _, thr := range []float64{0, ThreeEngineLandingThrottle} {
		st := c.Stages[0]
		st.FuelMass = fuel
		if thr > 0 {
			if st.LandingThrottle <= 0 || st.LandingThrottle >= thr {
				break
			}
			st.LandingThrottle = thr
		}
		b := buildJettisonedCraft([]spacecraft.Stage{st}, c)
		if recoveryTooHeavy(b) {
			continue
		}
		w.recoverySolves++
		b.State = c.State
		b.State.M = b.TotalMass()
		mu := b.Primary.GravitationalParameter()
		lead := boostbackLead(b) + RecoveryStageGraceS
		tS := w.Clock.SimTime.Add(time.Duration(lead * float64(time.Second)))
		st0, ok := physics.KeplerStep(b.State, mu, lead)
		if !ok {
			lastErr = ErrBoostbackNoSolution
			continue
		}
		st0.M = b.TotalMass()
		lat, lon := site.lzLatLon(1, b.Primary.RadiusMeters())
		_, off, err := w.solveNoBoost(b, st0, tS, [2]float64{lat, lon})
		if err != nil {
			lastErr = err
			continue
		}
		if off.fuelLeft < BoostbackFuelMarginKg {
			lastErr = ErrBoostbackNoSolution
			continue
		}
		rt.autoThree, rt.onFall = thr > 0, true
		if !w.noShipFollow {
			for i := range w.Sites {
				if s := &w.Sites[i]; s.ID == site.ID {
					s.LatDeg, s.LonDeg, s.UpdatedAtNano = off.aimLat, off.aimLon, time.Now().UnixNano()
				}
			}
		}
		return off.fuelLeft - BoostbackFuelMarginKg, nil
	}
	return 0, lastErr
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
	if st.RecoverMode.IsShip() {
		tickWarp = math.Min(tickWarp, recoveryShipWarpCap) // the climb is capped from here on (N1)
	}
	near := func() bool { // the line says the reserve is reached within this tick plus the lead
		ahead := now.Sub(rt.at).Seconds() + simDelta.Seconds() + RecoveryCutLeadS
		return rt.haveCut && rt.rate > 0 && rt.slack-rt.rate*ahead <= RecoveryPadKg
	}
	if !now.Before(rt.nextSolve) {
		w.followHeading(c, st, rt)
		// `ship auto`: the no-boostback forecast is asked first. Until it has closed the
		// ordinary solve to the ship's place (250 km along the heading) runs beside it, so a
		// climb whose fall can never land without a boostback (a steep one) still cuts off;
		// the first forecast that closes takes over for the rest of the climb.
		auto := st.RecoverMode == spacecraft.RecoverShipAuto
		var slack, dv float64
		var rising bool
		var err error
		viaFall := false
		if auto {
			was := rt.onFall
			slack, err = w.recoverySlackAuto(c, st.FuelMass, rt)
			if viaFall = err == nil; viaFall && !was { // the line so far was the boostback's: start it again
				rt.haveCut, rt.havePrev, rt.rate, rt.lost, rt.seenFeasible, rt.three = false, false, 0, false, false, threeEngineLine{}
			}
		}
		if !viaFall && !rt.onFall {
			slack, dv, rising, err = w.recoverySlack(c, st.FuelMass, rt.warmDV, rt.rising)
		}
		if err != nil && !rt.onFall && !errors.Is(err, errRecoveryHeavy) && !errors.Is(err, ErrDeorbitNoSite) {
			rt.three.failed++
			rt.aimLimited = errors.Is(err, ErrBoostbackNoSolution)
		}
		// ADR 0071 decision 1: the one-engine solve ran and did not close, and has not
		// closed on this climb. Price the landing burn on three engines; that plan only
		// stands in for the cutoff line, and the stage commits to three engines when it
		// reaches it (a stage whose one-engine plan closes first never changes).
		// While the weight test says one engine cannot hover after the plan, three
		// engines (3x the hover) still can, and their window opens first (flight 4: it was
		// open 28 s before the one-engine test let the cutoff look). So the shadow line is
		// also priced in that phase, every recoveryRetrySec, with no failure count to wait for.
		heavy, probeAhead := errors.Is(err, errRecoveryHeavy), 0.0
		if heavy {
			probeAhead = recoveryMinSec * float64(simDelta) / float64(w.Clock.BaseStep) // a real second (the ship cap is not on yet)
		}
		lt := st.LandingThrottle
		if err != nil && !rt.onFall && !rt.seenFeasible && lt > 0 && lt < ThreeEngineLandingThrottle && !errors.Is(err, ErrDeorbitNoSite) &&
			(heavy || rt.three.failed >= recoveryThreeAfterFails || errors.Is(err, ErrBoostbackNoSolution)) && !now.Before(rt.three.nextAt) {
			l := &rt.three
			s3, d3, r3, err3 := w.recoverySlackAt(c, ThreeEngineLandingThrottle, st.FuelMass, l.warmDV, l.rising, 0, probeAhead)
			if err3 != nil {
				*l = threeEngineLine{rising: r3, failed: l.failed} // the scan window is learned from failing solves too
				if heavy {
					l.nextAt = now.Add(time.Duration(math.Max(recoveryRetrySec, probeAhead) * float64(time.Second)))
				}
			} else {
				if l.have {
					l.prevSlack, l.prevAt, l.havePrev = l.slack, l.at, true
				}
				l.slack, l.at, l.have, l.warmDV, l.rising = s3, now, true, d3, r3
				if l.havePrev && now.After(l.prevAt) {
					l.rate = (l.prevSlack - s3) / now.Sub(l.prevAt).Seconds()
				}
				if l.rate <= 0 {
					l.rate = 0.4 * st.Thrust / (st.Isp * stdGravityMps2)
				}
				// far from its line the plan is asked again after half the time to it (a
				// failing one-engine solve is every 3 s: this is what keeps the fallback's
				// cost down), never sooner than that failing solve comes round again
				gap := math.Max(recoveryRetrySec, math.Min(recoveryThreeMaxSec, 0.5*(s3-rt.threePad())/l.rate))
				if !l.havePrev { // one point has no rate yet: take the second soon
					gap = recoveryRetrySec
				}
				l.nextAt = now.Add(time.Duration(gap * float64(time.Second)))
			}
		}
		rt.rising = rising
		rt.light = rt.light || !errors.Is(err, errRecoveryHeavy)
		switch {
		case err == nil:
			if rt.haveCut {
				rt.prevSlack, rt.prevAt, rt.havePrev = rt.slack, rt.at, true
			}
			rt.slack, rt.at, rt.haveCut, rt.warmDV = slack, now, true, dv
			rt.seenFeasible, rt.three = true, threeEngineLine{}
			if rt.lost { // the plan closed again, on this very state: the drop will take it
				w.cutAtReserve(c)
				return
			}
			if rt.havePrev && now.After(rt.prevAt) {
				rt.rate = (rt.prevSlack - slack) / now.Sub(rt.prevAt).Seconds()
			}
			if rt.rate <= 0 { // first solve: the plan loses about 0.4 of each kg the stage burns
				rt.rate = 0.4 * st.Thrust / (st.Isp * stdGravityMps2)
			}
			// a ship's solve scans for its root (0.4 s against an LZ's 0.1 s), so it is
			// asked less often; the pad covers what the longer gap costs in precision
			slow := 1.0
			if st.RecoverMode.IsShip() {
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
			// a plan closed before and none does now. Whether one closes is not smooth in
			// the stage's state (the same stage closes at 61 t, not at 50, closes at 45), so a
			// cut on this solve is a refused drop. Solve again each real second; the first
			// that closes cuts. Reaching the reserve line first cuts anyway and says why.
			rt.lost, rt.lostErr = true, err
			rt.nextSolve = now.Add(time.Duration(recoveryMinSec * tickWarp * float64(time.Second)))
		case errors.Is(err, errRecoveryHeavy):
			rt.nextSolve = now.Add(time.Duration(recoveryHeavySec * float64(time.Second)))
		default:
			rt.nextSolve = now.Add(time.Duration(math.Max(recoveryRetrySec, recoveryMinSec*tickWarp) * float64(time.Second)))
		}
	}
	if near() {
		if rt.lost && rt.lostErr != nil {
			w.PendingRecoveryEvents = append(w.PendingRecoveryEvents, RecoveryEvent{c.Name, rt.lostErr.Error()})
			w.recordRowFor(c, "recovery: "+rt.lostErr.Error())
		}
		w.cutAtReserve(c)
	} else if !rt.seenFeasible && rt.three.reached(now.Sub(rt.three.at).Seconds()+simDelta.Seconds()+RecoveryCutLeadS, rt.threePad()) {
		// the line is an extrapolation and a three-engine window can shut inside one solve
		// gap (12 t of slack, 5.5 t, then none): cut only on a plan that closes now, or the
		// drop is refused. If it shut, the stage burns on, as it did before the fallback.
		l := &rt.three
		// (a solve from the last real second is fresh enough: solves stay a second apart)
		if now.Sub(l.at).Seconds() >= recoveryMinSec*tickWarp {
			if _, _, _, err3 := w.recoverySlackAt(c, ThreeEngineLandingThrottle, st.FuelMass, l.warmDV, l.rising, 0, 0); err3 != nil {
				*l = threeEngineLine{failed: l.failed}
				return
			}
		}
		// staging is the pilot's press, not the cut's: the plan must still close after
		// the stage coasts RecoveryStageGraceS (a plan that closes only this tick, or
		// is shut a tick later, was refused at the drop)
		if _, _, _, err3 := w.recoverySlackAt(c, ThreeEngineLandingThrottle, st.FuelMass, l.warmDV, l.rising, RecoveryStageGraceS, 0); err3 != nil {
			*l = threeEngineLine{failed: l.failed}
			return
		}
		st.LandingThrottle = ThreeEngineLandingThrottle // saved with the stage, copied to the dropped one
		w.cutAtReserve(c)
	}
}

// cutAtReserve holds the bottom stage's remaining propellant back and tells the
// recording: MECO for a stage of the core, BECO for side boosters.
func (w *World) cutAtReserve(c *spacecraft.Spacecraft) {
	st := &c.Stages[0]
	word := CutoffWord(*st)
	if rt := w.recRT[c.ID]; rt != nil && rt.autoThree {
		st.LandingThrottle = ThreeEngineLandingThrottle // the forecast that closed was on three engines: saved with the stage, copied to the dropped one
	}
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
