package sim

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// The commanded raise (ADR 0059 decision 4): a full-throttle tangential
// spiral on a low-thrust engine, analytic at any warp. It is not an
// ActiveBurn, so clampedWarp never caps for it.
const (
	raiseMaxEcc   = 0.05 // above this the held-elements spiral is no longer bounded
	raiseMaxAccel = 1e-3 // m/s²; a Hall thruster passes, the Ion Keeper (1.56) does not
	raiseSubstep  = 500.0
	raiseG0       = 9.80665
)

// RaiseEvent is one Event Flash for app.go to drain (ADR 0048): Text is the
// whole line, "Starlink-7: raise complete, 550 km".
type RaiseEvent struct {
	When      time.Time
	CraftName string
	Text      string
}

func (w *World) queueRaiseEvent(c *spacecraft.Spacecraft, format string, a ...any) {
	w.PendingRaiseEvents = append(w.PendingRaiseEvents, RaiseEvent{
		When: w.Clock.SimTime, CraftName: c.Name, Text: c.Name + ": " + fmt.Sprintf(format, a...),
	})
}

// CommandRaise orders craft idx to spiral its (near-round) orbit to altM above
// the surface, now. The error text is the refusal, ready to flash; a refused
// order leaves nothing behind. A raise already under way is replaced.
func (w *World) CommandRaise(idx int, altM float64) error {
	if err := w.RaiseRefusal(idx, altM); err != nil {
		return err
	}
	w.Crafts[idx].Raise = &spacecraft.RaiseOrder{TargetAltM: altM, StartAt: w.Clock.SimTime, Started: true}
	return nil
}

// RaiseRefusal is CommandRaise's checks without the order: nil when craft idx
// could be ordered to altM now. The L key asks it before opening the box and
// again before the card, so a refusal never leaves a box or a card behind.
func (w *World) RaiseRefusal(idx int, altM float64) error {
	if idx < 0 || idx >= len(w.Crafts) || w.Crafts[idx] == nil {
		return errors.New("raise: no vessel")
	}
	c := w.Crafts[idx]
	if c.Landed || c.Crashed || len(c.DockedComponents) > 0 || c.ActiveBurn != nil || c.ManualBurn != nil {
		return errors.New("raise: not free")
	}
	if c.Thrust <= 0 || c.Isp <= 0 || c.ActiveStageFuel() <= 0 {
		return errors.New("raise: no engine")
	}
	if c.Thrust/c.TotalMass() >= raiseMaxAccel {
		return errors.New("raise: thrust too high for a spiral")
	}
	mu := c.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if el.E >= 1 || el.A <= 0 || el.E > raiseMaxEcc+1e-9 {
		return errors.New("raise: orbit not round")
	}
	radius := c.Primary.RadiusMeters()
	// The held eccentricity carries the periapsis with the target, so the
	// periapsis at the target is what must clear the atmosphere.
	if atm := c.Primary.Atmosphere; atm != nil && (radius+altM)*(1-el.E)-radius < atm.CutoffAltitude {
		return errors.New("raise: below the atmosphere")
	}
	sys, _ := w.ephemerisFor(c)
	if parent := sys.ParentOf(c.Primary); parent != nil {
		if soi := physics.SOIRadius(c.Primary, *parent); soi > 0 && altM > soi/2 {
			return fmt.Errorf("raise: outside %s's reach", c.Primary.EnglishName)
		}
	}
	if !w.canCommand(c) {
		return errors.New("NO SIGNAL")
	}
	return nil
}

// RaisePlan is what a raise of craft c to altM costs: the Δv, the propellant
// by the rocket equation, and the thrust time at full throttle. The card
// prints it; it is the same arithmetic raiseChunk integrates.
func RaisePlan(c *spacecraft.Spacecraft, altM float64) (dv, propKg float64, dur time.Duration) {
	mu := c.Primary.GravitationalParameter()
	dv = math.Abs(math.Sqrt(mu/orbitalA(c)) - math.Sqrt(mu/(c.Primary.RadiusMeters()+altM)))
	mdot := c.MassFlowRateAt(1)
	if mdot <= 0 {
		return dv, 0, 0
	}
	ve := c.Thrust / mdot
	m0 := c.TotalMass()
	propKg = m0 * (1 - math.Exp(-dv/ve))
	return dv, propKg, time.Duration(propKg / mdot * float64(time.Second))
}

// StopRaise cancels craft idx's raise, running or waiting; it coasts where it
// is. False when there was none.
func (w *World) StopRaise(idx int) bool {
	if idx < 0 || idx >= len(w.Crafts) || w.Crafts[idx] == nil || w.Crafts[idx].Raise == nil {
		return false
	}
	c := w.Crafts[idx]
	c.Raise = nil
	w.queueRaiseEvent(c, "raise stopped at %.0f km", (orbitalA(c)-c.Primary.RadiusMeters())/1000)
	return true
}

func orbitalA(c *spacecraft.Spacecraft) float64 {
	return orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter()).A
}

// advanceRaise runs craft c's raise for one tick and reports whether it
// consumed the tick. A waiting order starts when SimTime reaches StartAt (the
// part of the tick before that is coasted), a burn pauses the order, and a
// finished or stopped order hands the rest of the tick to the ordinary path
// only when it never ran.
func (w *World) advanceRaise(c *spacecraft.Spacecraft, simDelta time.Duration) bool {
	o := c.Raise
	if c.ActiveBurn != nil || c.ManualBurn != nil {
		return false
	}
	secs := simDelta.Seconds()
	if !o.Started {
		if w.Clock.SimTime.Before(o.StartAt) {
			return false
		}
		o.Started = true
		if lead := o.StartAt.Sub(w.Clock.SimTime.Add(-simDelta)).Seconds(); lead > 0 {
			if canKeplerStepState(c.State, c.Primary.GravitationalParameter(), c.Primary) {
				w.keplerStepWithSOICheck(c, time.Duration(lead*float64(time.Second)))
			}
			secs -= lead
		}
	}
	if secs <= 0 {
		return true
	}
	if !w.chunkedSOIStep(c, secs, func(mu, chunk float64) bool { return w.raiseChunk(c, mu, chunk) }) {
		if c.Raise != nil { // the orbit left the spiral's reach (unbound, or periapsis under the surface)
			w.StopRaise(w.craftIndex(c))
		}
		return false
	}
	return true
}

func (w *World) craftIndex(c *spacecraft.Spacecraft) int {
	for i, x := range w.Crafts {
		if x == c {
			return i
		}
	}
	return -1
}

// raiseChunk advances c by chunk seconds: thrust substeps with the mass
// carried between them, then a coast once the order ends.
func (w *World) raiseChunk(c *spacecraft.Spacecraft, mu, chunk float64) bool {
	for left := chunk; left > 0; {
		dt := math.Min(left, raiseSubstep)
		left -= dt
		o := c.Raise
		if o == nil {
			ns, ok := physics.KeplerStep(c.State, mu, dt)
			if !ok {
				return false
			}
			c.State = ns
			continue
		}
		mdot := c.MassFlowRateAt(1)
		fuel := c.ActiveStageFuel()
		if mdot <= 0 || fuel <= 0 {
			w.endRaise(c, "argon exhausted at %.0f km")
			left += dt
			continue
		}
		targetA := c.Primary.RadiusMeters() + o.TargetAltM
		verb := "raise"
		if targetA < orbitalA(c) {
			verb = "lower"
		}
		tb := math.Min(dt, fuel/mdot) // thrust time the tank allows
		m0 := c.TotalMass()
		// Mean acceleration over tb with the mass falling at mdot: exact
		// rocket-equation Δv / tb, so a 5,000 s tick carries the mass change.
		aEff := c.Thrust / mdot * math.Log(m0/(m0-mdot*tb)) / tb
		ns, used, done, ok := physics.RaiseStep(c.State, mu, aEff, tb, targetA)
		if !ok {
			return false
		}
		c.State = ns
		c.BurnFuel(mdot * used)
		c.State.M = c.TotalMass()
		switch {
		case done:
			w.endRaise(c, verb+" complete, %.0f km")
			// RaiseStep coasted the rest of tb; coast what is left of dt.
			if rest := dt - tb; rest > 0 {
				ns, ok := physics.KeplerStep(c.State, mu, rest)
				if !ok {
					return false
				}
				c.State = ns
			}
		case tb < dt:
			w.endRaise(c, "argon exhausted at %.0f km")
			ns, ok := physics.KeplerStep(c.State, mu, dt-tb)
			if !ok {
				return false
			}
			c.State = ns
		}
	}
	return true
}

// endRaise clears the order and queues its flash; format takes the altitude
// in km (the target on completion, the orbit's own on exhaustion).
func (w *World) endRaise(c *spacecraft.Spacecraft, format string) {
	alt := (orbitalA(c) - c.Primary.RadiusMeters()) / 1000
	if strings.Contains(format, "complete") {
		alt = c.Raise.TargetAltM / 1000
	}
	w.queueRaiseEvent(c, format, alt)
	c.Raise = nil
}

// The spread (ADR 0059 decision 5): L on a released batch gives every member a
// raise order whose start is solved from its own state, so that when the last
// one arrives the members sit evenly around the orbit.
const (
	spreadPlaneTolDeg = 0.5                 // a member this far off the median plane is not "in one plane"
	spreadAtTolM      = 1000.0              // within this of the target, n_k = n_e and no wait can phase it
	spreadHorizon     = 60 * 24 * time.Hour // the longest campaign (last wait plus its climb)
	spreadSlack       = 1e-9                // equal cases at the thresholds pass; float noise must not flip them
)

// SpreadSlot is one member's solved order: its slate index, the wait before it
// starts climbing, and the climb itself.
type SpreadSlot struct {
	Idx  int
	Wait time.Duration
	Dur  time.Duration
}

// SpreadSchedule is a solved spread: what the card prints and SpreadBatch applies.
type SpreadSchedule struct {
	Name   string // the members' common name, "Starlink"
	Slots  []SpreadSlot
	DV     float64 // the first member's climb, for the card
	PropKg float64
}

// First and Last are the earliest and latest start.
func (s SpreadSchedule) First() time.Duration { return s.Slots[s.firstLast(false)].Wait }
func (s SpreadSchedule) Last() time.Duration  { return s.Slots[s.firstLast(true)].Wait }

func (s SpreadSchedule) firstLast(last bool) int {
	best := 0
	for i, sl := range s.Slots {
		if (last && sl.Wait > s.Slots[best].Wait) || (!last && sl.Wait < s.Slots[best].Wait) {
			best = i
		}
	}
	return best
}

// spreadTooSlow reports whether a campaign ending after d is refused; 60 d
// exactly goes.
func spreadTooSlow(d time.Duration) bool { return d > spreadHorizon }

// SpreadMembers are the slate indices of the batch's members: the batch id,
// not the lead.
func (w *World) SpreadMembers(batchID uint64) []int {
	var out []int
	for i, c := range w.Crafts {
		if c != nil && batchID != 0 && c.BatchID == batchID && !c.BatchLead {
			out = append(out, i)
		}
	}
	return out
}

func km(m float64) string { return strconv.FormatFloat(m/1000, 'f', -1, 64) }

// planeOffDeg is the angle between two orbit normals, atan2 so it is exact near 0.
func planeOffDeg(a, b orbital.Vec3) float64 {
	return math.Atan2(a.Cross(b).Norm(), a.Dot(b)) * 180 / math.Pi
}

func medianNormal(hs []orbital.Vec3) orbital.Vec3 {
	med := func(f func(orbital.Vec3) float64) float64 {
		v := make([]float64, len(hs))
		for i, h := range hs {
			v[i] = f(h)
		}
		sort.Float64s(v)
		if n := len(v); n%2 == 0 {
			return (v[n/2-1] + v[n/2]) / 2
		}
		return v[len(v)/2]
	}
	return orbital.Vec3{
		X: med(func(h orbital.Vec3) float64 { return h.X }),
		Y: med(func(h orbital.Vec3) float64 { return h.Y }),
		Z: med(func(h orbital.Vec3) float64 { return h.Z }),
	}.Unit()
}

func modTwoPi(x float64) float64 {
	x = math.Mod(x, 2*math.Pi)
	if x < 0 {
		x += 2 * math.Pi
	}
	if x > 2*math.Pi-spreadSlack {
		x = 0
	}
	return x
}

// SpreadPlan solves the spread of batch batchID to altM: ψ_k = λ_k + Θ_k −
// n_e D_k + (n_k − n_e) w_k is member k's final angle in the frame that turns
// with the target orbit, linear in its wait w_k. Slots sit 360/N apart; the
// members take them in cyclic order of ψ_k at w = 0, and the offset is set so
// the anchor member starts now. Each of the N members is tried as the anchor
// for the smallest longest wait, so no wait is negative. (A rotation of the
// slot assignment would not change anything: waitFor is periodic in 2π and N
// slots span a full turn.) The error is the player's refusal.
func (w *World) SpreadPlan(batchID uint64, altM float64) (SpreadSchedule, error) {
	idxs := w.SpreadMembers(batchID)
	n := len(idxs)
	if n < 2 {
		return SpreadSchedule{}, errors.New("spread: not on one orbit")
	}
	first := w.Crafts[idxs[0]]
	hs := make([]orbital.Vec3, n)
	for i, ix := range idxs {
		c := w.Crafts[ix]
		if c.Primary.ID != first.Primary.ID {
			return SpreadSchedule{}, errors.New("spread: not on one orbit")
		}
		hs[i] = c.State.R.Cross(c.State.V).Unit()
	}
	nh := medianNormal(hs)
	for i := range idxs {
		if planeOffDeg(hs[i], nh) > spreadPlaneTolDeg+spreadSlack {
			return SpreadSchedule{}, errors.New("spread: not in one plane")
		}
	}
	for _, ix := range idxs {
		if err := w.RaiseRefusal(ix, altM); err != nil {
			return SpreadSchedule{}, fmt.Errorf("spread: %s: %s", w.Crafts[ix].Name, strings.TrimPrefix(err.Error(), "raise: "))
		}
	}
	mu := first.Primary.GravitationalParameter()
	ae := first.Primary.RadiusMeters() + altM
	ne, ve := math.Sqrt(mu/(ae*ae*ae)), math.Sqrt(mu/ae)
	// In-plane angle measured from the first member's place, in the direction of motion.
	xh := first.State.R.Sub(nh.Scale(first.State.R.Dot(nh))).Unit()
	yh := nh.Cross(xh)

	type mem struct {
		ix           int
		psi0, rate   float64
		dur          time.Duration
		dv, prop, th float64
	}
	ms := make([]mem, n)
	for i, ix := range idxs {
		c := w.Crafts[ix]
		ak := orbitalA(c)
		if math.Abs(ak-ae) <= spreadAtTolM+1e-6 {
			return SpreadSchedule{}, fmt.Errorf("spread: %s already at %s km", c.Name, km(altM))
		}
		dv, prop, dur := RaisePlan(c, altM)
		if dur <= 0 {
			return SpreadSchedule{}, fmt.Errorf("spread: %s: no engine", c.Name)
		}
		vk := math.Sqrt(mu / ak)
		aEff := dv / dur.Seconds()
		theta := math.Abs(math.Pow(vk, 4)-math.Pow(ve, 4)) / (4 * aEff * mu)
		lam := math.Atan2(c.State.R.Dot(yh), c.State.R.Dot(xh))
		ms[i] = mem{ix: ix, dur: dur, dv: dv, prop: prop,
			psi0: modTwoPi(lam + theta - ne*dur.Seconds()),
			rate: math.Sqrt(mu/(ak*ak*ak)) - ne}
	}
	sort.SliceStable(ms, func(a, b int) bool { return ms[a].psi0 < ms[b].psi0 })

	slot := 2 * math.Pi / float64(n)
	waitFor := func(m mem, target float64) float64 {
		if m.rate > 0 {
			return modTwoPi(target-m.psi0) / m.rate
		}
		return modTwoPi(m.psi0-target) / -m.rate
	}
	best, bestMax := make([]float64, n), math.Inf(1)
	cur := make([]float64, n)
	for anchor := 0; anchor < n; anchor++ {
		ref := ms[anchor].psi0 - float64(anchor)*slot
		mx := 0.0
		for j, m := range ms {
			cur[j] = waitFor(m, ref+float64(j)*slot)
			mx = math.Max(mx, cur[j])
		}
		if mx < bestMax {
			bestMax = mx
			copy(best, cur)
		}
	}
	sch := SpreadSchedule{Name: strings.TrimSuffix(strings.TrimRight(first.Name, "0123456789"), "-"), DV: ms[0].dv, PropKg: ms[0].prop}
	var end time.Duration
	for j, m := range ms {
		wait := time.Duration(best[j] * float64(time.Second))
		sch.Slots = append(sch.Slots, SpreadSlot{Idx: m.ix, Wait: wait, Dur: m.dur})
		end = max(end, wait+m.dur)
	}
	if spreadTooSlow(end) {
		return SpreadSchedule{}, fmt.Errorf("spread: too slow from this orbit (%.0f d)", math.Ceil(end.Hours()/24))
	}
	sort.Slice(sch.Slots, func(a, b int) bool { return sch.Slots[a].Idx < sch.Slots[b].Idx })
	return sch, nil
}

// SpreadBatch spreads batch batchID to altM: every member gets its solved,
// possibly waiting, order (replacing any it had). The order keeps the batch id;
// the notice and Stop the spread? read it back.
func (w *World) SpreadBatch(batchID uint64, altM float64) error {
	sch, err := w.SpreadPlan(batchID, altM)
	if err != nil {
		return err
	}
	now := w.Clock.SimTime
	for _, s := range sch.Slots {
		w.Crafts[s.Idx].Raise = &spacecraft.RaiseOrder{TargetAltM: altM, StartAt: now.Add(s.Wait), Started: s.Wait == 0, BatchID: batchID}
	}
	return nil
}

// StopSpread drops every waiting order of the batch's spread and lets the
// climbing ones finish. It returns how many it dropped.
func (w *World) StopSpread(batchID uint64) int {
	n := 0
	for _, i := range w.SpreadMembers(batchID) {
		if o := w.Crafts[i].Raise; o != nil && o.BatchID == batchID && !o.Started {
			w.Crafts[i].Raise = nil
			n++
		}
	}
	return n
}

// SpreadStatus is the SPREAD notice's numbers, read off the members' orders.
type SpreadStatus struct {
	Name                                       string
	TargetAltM                                 float64
	Total, Arrived, Climbing, Waiting, Stopped int
	NextName                                   string // the soonest waiting member
	NextIn                                     time.Duration
}

// SpreadStatus reads batch batchID's spread; false when no member has a spread
// order, which is when the notice is gone. A member with no order is arrived
// when it sits within 1 km of the target, else stopped.
func (w *World) SpreadStatus(batchID uint64) (SpreadStatus, bool) {
	var st SpreadStatus
	found := false
	for _, i := range w.SpreadMembers(batchID) {
		if o := w.Crafts[i].Raise; o != nil && o.BatchID == batchID {
			st.TargetAltM, found = o.TargetAltM, true
			break
		}
	}
	if !found {
		return st, false
	}
	soonest := time.Time{}
	for _, i := range w.SpreadMembers(batchID) {
		c := w.Crafts[i]
		st.Total++
		st.Name = strings.TrimSuffix(strings.TrimRight(c.Name, "0123456789"), "-")
		switch o := c.Raise; {
		case o != nil && o.BatchID == batchID && o.Started:
			st.Climbing++
		case o != nil && o.BatchID == batchID:
			st.Waiting++
			if soonest.IsZero() || o.StartAt.Before(soonest) {
				soonest, st.NextName = o.StartAt, c.Name
			}
		case math.Abs(orbitalA(c)-(c.Primary.RadiusMeters()+st.TargetAltM)) <= spreadAtTolM:
			st.Arrived++
		default:
			st.Stopped++
		}
	}
	if !soonest.IsZero() {
		st.NextIn = max(soonest.Sub(w.Clock.SimTime), 0)
	}
	return st, true
}
