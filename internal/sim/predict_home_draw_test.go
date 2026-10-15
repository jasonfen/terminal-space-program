package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestPlannedHomeLegDrawsAsClosedOrbit: a planted burn's post-burn leg that
// stays in the craft's home SOI must DRAW as a closed ellipse around the home
// body, however many revolutions the horizon spans. The inertial sample
// positions carry the home body's own heliocentric drift (Earth moves
// ~160 Mm per LEO revolution), so drawing them unchanged stretched the orbit
// into a helix that edge-on reads as a wave running away from the planet.
// Measured property: every drawn point sits within the orbit's own extent of
// the home body's CURRENT position (the Local-to-Body anchor), for a
// multi-revolution horizon.
func TestPlannedHomeLegDrawsAsClosedOrbit(t *testing.T) {
	w := mustWorld(t)
	craft := w.ActiveCraft()
	home := craft.Primary
	mu := home.GravitationalParameter()
	period := orbitalPeriod(craft.State, mu)

	// Two nodes ~15 revolutions apart: leg 0 coasts 15 revolutions in the
	// home SOI (the phasing-coast shape of a Rendezvous Planner plan).
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(60 * time.Second),
		Mode:        spacecraft.BurnPrograde,
		DV:          10,
	})
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(time.Duration((60 + 15*period) * float64(time.Second))),
		Mode:        spacecraft.BurnPrograde,
		DV:          10,
	})
	legs := w.PredictedLegs()
	if len(legs) < 1 {
		t.Fatal("no predicted legs")
	}
	leg := legs[0]
	segs := w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, leg.Samples)

	// Orbit extent: the post-burn orbit's apoapsis radius from the focus.
	r := leg.State.R.Norm()
	v := leg.State.V.Norm()
	a := 1 / (2/r - v*v/mu)
	maxR := 2*a - r // r is perigee-ish for a circular start; 2a-r bounds apoapsis
	if maxR < r {
		maxR = r
	}

	anchor := w.BodyPosition(home)
	var worst float64
	n := 0
	for _, seg := range segs {
		if seg.PrimaryID != home.ID {
			continue
		}
		for _, p := range w.PlannedSegmentDrawPoints(seg) {
			d := p.Sub(anchor).Norm()
			if d > worst {
				worst = d
			}
			n++
		}
	}
	if n == 0 {
		t.Fatal("no home-SOI draw points")
	}
	if worst > 1.05*maxR {
		t.Errorf("home leg draws %.3g m from the home body, orbit extent is %.3g m: the orbit is smeared by the body's own motion (%.1fx)",
			worst, maxR, worst/maxR)
	}
}
