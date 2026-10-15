package screens

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestPlannedHomeLegInksOnlyAroundThePlanet: a planted burn followed by a
// long phasing coast (a Rendezvous Planner plan: ~15 revolutions in the home
// SOI) must ink its dashed post-burn orbit within the orbit's own extent
// around the planet, in every flat view. The bug: the home-SOI leg was drawn
// at inertial sample positions, which carry Earth's own heliocentric motion
// (~160 Mm per LEO revolution), so the orbit became a helix trailing off the
// planet; edge-on (Right view) it read as a purple wave running away to one
// side (playtest 2026-10-14).
//
// Measured against the real draw path (plotPredictedLegs onto the real
// canvas): count the cells inked farther than 1.3x the orbit radius from the
// planet's drawn centre. Zero is the contract; main inked hundreds.
func TestPlannedHomeLegInksOnlyAroundThePlanet(t *testing.T) {
	for _, vm := range []struct {
		name string
		mode sim.ViewMode
	}{
		{"top", sim.ViewTop},
		{"right", sim.ViewRight},
		{"tilted", sim.ViewTilted},
	} {
		t.Run(vm.name, func(t *testing.T) {
			w, err := sim.NewWorld()
			if err != nil {
				t.Fatalf("NewWorld: %v", err)
			}
			craft := w.ActiveCraft()
			mu := craft.Primary.GravitationalParameter()
			r := craft.State.R.Norm()
			period := 2 * math.Pi * math.Sqrt(r*r*r/mu)
			w.PlanNode(sim.ManeuverNode{
				TriggerTime: w.Clock.SimTime.Add(60 * time.Second),
				Mode:        spacecraft.BurnPrograde,
				DV:          10,
			})
			w.PlanNode(sim.ManeuverNode{
				TriggerTime: w.Clock.SimTime.Add(time.Duration((60 + 15*period) * float64(time.Second))),
				Mode:        spacecraft.BurnPrograde,
				DV:          10,
			})

			v := newSOIPassTestView()
			w.ViewMode = vm.mode
			_ = v.Render(w, 0, 140, 40)

			// Pin the camera to a scale where the orbit is a legible ring.
			v.canvas.Center(w.BodyPosition(craft.Primary))
			v.canvas.SetScale(40 / r)
			data := v.computePredictedRender(w)
			if len(data.legs) == 0 {
				t.Fatal("no predicted legs")
			}
			v.canvas.Clear()
			v.plotPredictedLegs(w, data.legs)

			cx, cy, ok := v.canvas.Project(w.BodyPosition(craft.Primary))
			if !ok {
				t.Fatal("planet centre off canvas")
			}
			// Orbit radius in braille pixels, x1.3 slack for the burn's
			// larger ellipse, plus a few px of dash/rounding slack.
			limit := 40.0*1.3*1.25 + 6
			far, near := 0, 0
			for row, line := range strings.Split(stripANSI(v.canvas.String()), "\n") {
				for col, r := range []rune(line) {
					if r <= 0x2800 || r > 0x28FF {
						continue
					}
					px, py := float64(col*2+1)-float64(cx), float64(row*4+2)-float64(cy)
					if math.Hypot(px, py) > limit {
						far++
					} else {
						near++
					}
				}
			}
			if near == 0 {
				t.Fatal("no leg ink near the planet: the instrument cannot see the orbit")
			}
			if far != 0 {
				t.Errorf("%d cells of the planned leg are inked beyond %.0f px of the planet (%d near): the orbit is smeared along the planet's own motion", far, limit, near)
			}
		})
	}
}
