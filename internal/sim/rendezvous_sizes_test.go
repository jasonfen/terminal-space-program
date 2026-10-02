package sim

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// rendezvousSizesWorld puts the active vessel on a circular orbit at
// altMover and the target on a circular orbit at altHolder, phaseDeg ahead,
// both prograde in the same plane (the grill G4 Q4 geometry: 500 km vs 700 km).
func rendezvousSizesWorld(t *testing.T, altMover, altHolder, phaseDeg float64) *World {
	t.Helper()
	w := rendezvousTwoCraftWorld(t)
	active, target := w.Crafts[0], w.Crafts[1]
	target.Primary = active.Primary
	mu := active.Primary.GravitationalParameter()
	rp := active.Primary.RadiusMeters()
	axis := active.State.R.Cross(active.State.V).Unit()
	rhat := active.State.R.Unit()
	that := axis.Cross(rhat).Unit()
	place := func(c *spacecraft.Spacecraft, alt, phase float64) {
		r := rp + alt
		c.State.R = rotateAboutAxis(rhat, axis, phase).Scale(r)
		c.State.V = rotateAboutAxis(that, axis, phase).Scale(math.Sqrt(mu / r))
	}
	place(active, altMover, 0)
	place(target, altHolder, phaseDeg*math.Pi/180)
	return w
}

// flyRow plants the row through the production planter, then flies it with
// the live tick loop (World.Tick: the real node dispatch at the real trigger
// instant, both vessels integrated by the game's own coast path) and returns
// the minimum separation seen within a window around the advertised arrival
// instant.
//
// impulsive=true zeroes the engine thrust first, which makes the sim degrade
// the node to an impulsive burn (the same branch a thrust-less vessel takes),
// so what is measured is the solver, the planter and the dispatch timing.
// impulsive=false flies the real finite burn (centred on the trigger, 50 ms
// tick quantisation, fixed-nose thrust): a separate effect that the impulsive
// planner model does not carry (see the finite-burn test's note).
func flyRow(t *testing.T, w *World, place planner.RendezvousOrbit, laps int, impulsive bool) (flownCA float64, plan *RendezvousBurnPlan) {
	return flyRowBurnWarp(t, w, place, laps, impulsive, -1)
}

// flyRowBurnWarp is flyRow with the warp index held at burnIdx (0 = 1x,
// 1 = 10x) from 60 s before the burn trigger until the node burn is done;
// burnIdx < 0 keeps flyRow's original policy.
func flyRowBurnWarp(t *testing.T, w *World, place planner.RendezvousOrbit, laps int, impulsive bool, burnIdx int) (flownCA float64, plan *RendezvousBurnPlan) {
	t.Helper()
	if impulsive {
		w.ActiveCraft().Thrust = 0
	}
	plan, err := w.PlanRendezvousBurn(place, laps)
	if err != nil {
		t.Fatalf("laps=%d plant: %v", laps, err)
	}
	c := w.ActiveCraft()
	if len(c.Nodes) != 1 {
		t.Fatalf("laps=%d: %d nodes planted, want 1", laps, len(c.Nodes))
	}
	n := c.Nodes[0]
	arrival := n.TriggerTime.Add(time.Duration(n.RendezvousArrivalSec * float64(time.Second)))
	const window = 300 * time.Second
	flownCA = math.Inf(1)
	for i := 0; i < 400_000; i++ {
		now := w.Clock.SimTime
		if now.After(arrival.Add(window)) {
			break
		}
		toArr := arrival.Sub(now)
		switch {
		case toArr > 1200*time.Second:
			w.Clock.WarpIdx = 3
		case toArr > 150*time.Second:
			w.Clock.WarpIdx = 2
		default:
			w.Clock.WarpIdx = 1
		}
		if burnIdx >= 0 {
			c := w.ActiveCraft()
			sinceTrig := now.Sub(n.TriggerTime)
			if c.ActiveBurn != nil || (sinceTrig > -60*time.Second && sinceTrig < 0) || (sinceTrig >= 0 && sinceTrig < 5*time.Second) {
				w.Clock.WarpIdx = burnIdx
			}
		}
		w.Tick()
		if len(w.Crafts) < 2 { // fused: closest approach is zero
			return 0, plan
		}
		if w.Clock.SimTime.After(arrival.Add(-window)) {
			d := w.Crafts[0].State.R.Sub(w.Crafts[1].State.R).Norm()
			if d < flownCA {
				flownCA = d
			}
		}
	}
	return flownCA, plan
}

// #407 / #406 acceptance (G4 Q4): a 500 km vs 700 km pair gets rows, and
// flying each plantable row with the LIVE integrator closes to the advertised
// distance within the planner's own Ok gate (0.1% of the orbit radius).
// The burn is dispatched impulsively by the real tick (see flyRow).
func TestRendezvousDifferentSizes_FlownCAMatchesAdvertised(t *testing.T) {
	cases := []struct {
		name        string
		mover, hold float64
		phasesDeg   []float64
	}{
		{"raise 500 to 700", 500e3, 700e3, []float64{40, 200}},
		// Lowering burns dip the periapsis, so most rows are gated unsafe;
		// these phases are the ones where some lap count stays plantable.
		{"lower 700 to 500", 700e3, 500e3, []float64{210, 240, 270, 300, 330}},
		{"raise 500 to 520", 500e3, 520e3, []float64{120}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			okRows := 0
			for _, ph := range tc.phasesDeg {
				w := rendezvousSizesWorld(t, tc.mover, tc.hold, ph)
				ladder, err := w.RecommendRendezvousLadder(planner.RendezvousTheirOrbit)
				if err != nil {
					t.Fatalf("phase %.0f: ladder refused a different-size pair: %v", ph, err)
				}
				tol := 0.001 * (w.ActiveCraft().Primary.RadiusMeters() + tc.mover)
				for _, row := range ladder.Rows {
					if !row.Ok {
						continue
					}
					okRows++
					fw := rendezvousSizesWorld(t, tc.mover, tc.hold, ph)
					flown, plan := flyRow(t, fw, planner.RendezvousTheirOrbit, row.Laps, true)
					t.Logf("phase %.0f laps=%d dv=%.1f m/s: flown CA %.0f m, advertised %.2f m (gate %.0f m)", ph, row.Laps, row.DV, flown, plan.AchievableCA, tol)
					if flown > tol {
						t.Errorf("phase %.0f laps=%d: flown CA %.0f m, advertised %.0f m, want < %.0f m", ph, row.Laps, flown, plan.AchievableCA, tol)
					}
				}
			}
			if okRows == 0 {
				t.Fatalf("no Ok rows on a %s pair at phases %v", tc.name, tc.phasesDeg)
			}
		})
	}
}

// #537: the same rows flown with the real finite burn (centred on the
// trigger, fixed nose, 50 ms ticks), at 1x and at the 10x burn cap, close to
// the advertised distance within the planner's own Ok gate (0.1% of the orbit
// radius). Before the final burn tick was split at DVRemaining the last tick
// over-delivered up to 1 m/s (1x) or 10 m/s (10x) and these rows missed by
// 17 km to 2.7 Mm.
func TestRendezvousDifferentSizes_FiniteBurnFlown_MatchesAdvertised(t *testing.T) {
	for _, burnIdx := range []int{0, 1} {
		w := rendezvousSizesWorld(t, 500e3, 700e3, 200)
		ladder, err := w.RecommendRendezvousLadder(planner.RendezvousTheirOrbit)
		if err != nil {
			t.Fatalf("ladder refused: %v", err)
		}
		for _, row := range ladder.Rows {
			if !row.Ok {
				continue
			}
			fw := rendezvousSizesWorld(t, 500e3, 700e3, 200)
			flown, plan := flyRowBurnWarp(t, fw, planner.RendezvousTheirOrbit, row.Laps, false, burnIdx)
			t.Logf("finite burn at %gx: laps=%d dv=%.1f m/s: flown CA %.0f m, advertised %.2f m", WarpFactors[burnIdx], row.Laps, row.DV, flown, plan.AchievableCA)
			if math.IsInf(flown, 1) {
				t.Fatalf("laps=%d: no separation sample taken", row.Laps)
			}
			tol := 0.001 * (fw.ActiveCraft().Primary.RadiusMeters() + 500e3)
			if flown > tol {
				t.Errorf("finite burn at %gx laps=%d: flown CA %.0f m, want < %.0f m (the Ok gate)", WarpFactors[burnIdx], row.Laps, flown, tol)
			}
		}
	}
}

// G4 Q5 / Contradiction 2: [H] plans transfers to bodies and refuses vessel
// targets, so no refusal a vessel-target pilot can read may send them to it.
// The shape refusal names circularize [C].
func TestRendezvousRefusalsNeverPointAtHohmannForVessels(t *testing.T) {
	for name, err := range map[string]error{
		"shape":    ErrRendezvousShapeMismatch,
		"too big":  ErrRendezvousBurnTooLarge,
		"unsafe":   ErrRendezvousUnsafePeriapsis,
		"crossing": ErrRendezvousNoCrossing,
		"plane":    ErrRendezvousPlaneMismatch,
	} {
		if strings.Contains(err.Error(), "[H") {
			t.Errorf("%s refusal %q points at [H], a dead end for a vessel target", name, err.Error())
		}
	}
	if !strings.Contains(ErrRendezvousShapeMismatch.Error(), "[C]") {
		t.Errorf("shape refusal %q must point at circularize [C]", ErrRendezvousShapeMismatch.Error())
	}
}

// A stretched partner orbit reaches the player as the shape refusal through
// the real sim entry point (not the planner error), with [C] in its text.
func TestRendezvousDifferentShape_SimRefusalPointsAtCircularize(t *testing.T) {
	w := rendezvousSizesWorld(t, 500e3, 700e3, 200)
	target := w.Crafts[1]
	// Stretch the partner: same position, 12% more speed, so it is no longer round.
	target.State.V = target.State.V.Scale(1.12)
	_, err := w.RecommendRendezvousLadder(planner.RendezvousTheirOrbit)
	if err == nil || !errors.Is(err, ErrRendezvousShapeMismatch) {
		t.Fatalf("err = %v, want ErrRendezvousShapeMismatch", err)
	}
}
