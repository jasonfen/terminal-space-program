package sim

import (
	"fmt"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// buildStack docks n craft into a single composite at w.Crafts[0] and
// returns it. Craft start close enough together (within DockingDistM,
// matched velocity) that repeated checkDocking passes fuse them one pair at
// a time, exactly the way a player accretes a multi-component stack in play
// (dock #1, then dock #2 onto the resulting composite, ...).
func buildStack(t *testing.T, w *World, n int) *spacecraft.Spacecraft {
	t.Helper()
	if n < 2 {
		t.Fatalf("buildStack: n must be >= 2, got %d", n)
	}
	earth := w.Systems[0].FindBody("Earth")
	a := w.Crafts[0]
	a.Name = "Core"
	a.Primary = *earth
	a.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := math.Sqrt(earth.GravitationalParameter() / a.State.R.Norm())
	a.State.V = orbital.Vec3{Y: v}

	for i := 1; i < n; i++ {
		b := spacecraft.NewFromLoadout(spacecraft.LoadoutSIVB1ID)
		b.Name = "Comp"
		b.Primary = *earth
		// Within DockingDistM (50 m) and matched velocity — fuses on the
		// next checkDocking pass regardless of which craft it lands next to.
		b.State = physics.StateVector{
			R: w.Crafts[0].State.R.Add(orbital.Vec3{X: 5}),
			V: w.Crafts[0].State.V,
			M: b.TotalMass(),
		}
		w.Crafts = append(w.Crafts, b)
		if _, _, ok := w.checkDocking(); !ok {
			t.Fatalf("buildStack: dock #%d failed to fuse", i)
		}
	}
	if len(w.Crafts) != 1 {
		t.Fatalf("buildStack: expected 1 composite after %d docks, got %d craft", n, len(w.Crafts))
	}
	if got := len(w.Crafts[0].DockedComponents); got != n {
		t.Fatalf("buildStack: composite has %d components, want %d", got, n)
	}
	return w.Crafts[0]
}

// TestUndockSeparationClearsDockingGates is the #342 regression test:
// undocking a stack of n components must not immediately re-fuse, at every n
// from a plain two-component split up through the five-component case a
// loaded carrier (itself already a 3+ stack) can produce. Pre-fix, the local
// split spread the restored components across a FIXED TOTAL SPAN (±35 m)
// rather than a fixed adjacent gap, so the gap between neighbours shrank to
// 70/(n-1) m as n grew — 35 m at n=3, 17.5 m at n=5 — both inside
// DockingDistM (50 m) and DockingVMS (0.1 m/s), so checkDocking re-fused the
// composite on the very next tick for n >= 3.
func TestUndockSeparationClearsDockingGates(t *testing.T) {
	for n := 2; n <= 5; n++ {
		n := n
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			w, _ := NewWorld()
			buildStack(t, w, n)

			if !w.Undock(0) {
				t.Fatalf("Undock returned false on a %d-component composite", n)
			}
			if len(w.Crafts) != n {
				t.Fatalf("expected %d craft after undock, got %d", n, len(w.Crafts))
			}

			// Measure BEFORE the regression check below — checkDocking
			// mutates the slate on a match, so logging after it would
			// describe the post-refuse state, not what Undock actually
			// produced.
			minSep, minRel := math.Inf(1), math.Inf(1)
			for i := 0; i < len(w.Crafts); i++ {
				for j := i + 1; j < len(w.Crafts); j++ {
					sep := w.Crafts[i].State.R.Sub(w.Crafts[j].State.R).Norm()
					rel := w.Crafts[i].State.V.Sub(w.Crafts[j].State.V).Norm()
					if sep < minSep {
						minSep = sep
					}
					if rel < minRel {
						minRel = rel
					}
				}
			}
			t.Logf("n=%d: min pairwise separation=%.2fm (gate %.0fm), min pairwise rel speed=%.4fm/s (gate %.2fm/s)",
				n, minSep, DockingDistM, minRel, DockingVMS)

			// The regression check: checkDocking must NOT find an
			// eligible pair. A pre-fix n>=3 split re-fuses here, collapsing
			// the slate back toward 1 craft.
			if _, _, ok := w.checkDocking(); ok {
				t.Errorf("n=%d: checkDocking re-fused the just-undocked stack (slate now %d craft) — adjacent-gap separation was not wide enough", n, len(w.Crafts))
			}
			if len(w.Crafts) != n {
				t.Errorf("n=%d: slate count = %d after the post-undock checkDocking pass, want %d (unchanged)", n, len(w.Crafts), n)
			}
		})
	}
}
