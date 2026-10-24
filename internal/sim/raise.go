package sim

import (
	"errors"
	"fmt"
	"math"
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
