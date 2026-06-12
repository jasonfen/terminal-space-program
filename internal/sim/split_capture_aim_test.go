package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// rotAbout rotates v about the unit axis k by deg degrees (Rodrigues).
func rotAbout(v, k orbital.Vec3, deg float64) orbital.Vec3 {
	th := deg * math.Pi / 180
	s, c := math.Sin(th), math.Cos(th)
	return v.Scale(c).Add(k.Cross(v).Scale(s)).Add(k.Scale(k.Dot(v) * (1 - c)))
}

// TestSplitCaptureAimPhaseSweep (GH #159, ADR 0018 extended to the split):
// a 24-point departure-phase sweep of the coplanar Kern→Cursor transfer.
// Pre-fix, every phase where the dual-strategy plant picked the split
// produced a collision course: the node-aligned phasing rendezvous with
// Cursor's CENTRE (exact when coplanar), so the planned pass perilune sat
// at −186 km altitude and the predicted final orbit was the degenerate
// −200/−200 zero-angular-momentum radial plunge Jason flew into.
//
// Post-fix the split's arrival is trimmed so the moon-frame perilune lands
// at the Capture Orbit radius (Cursor radius + 200 km), prograde — every
// split plant must show a planned pass at a positive, ≈capture altitude
// and a bound, non-degenerate predicted final orbit at Cursor.
//
// The strategy *selection* must stay centre-aimed (ADR 0006 B): the
// combined/split pick per phase is pinned against the pre-fix sweep.
func TestSplitCaptureAimPhaseSweep(t *testing.T) {
	// Strategy per phase measured on main (pre-fix, 4a107a3-era planner) —
	// the capture-safe trim must not flip any of these (ADR 0006 B: the
	// comparison stays centre-aimed for both strategies).
	wantStrategy := [24]string{
		"combined", "combined", "combined", "combined", "combined", "split",
		"split", "split", "split", "combined", "combined", "split",
		"combined", "combined", "combined", "split", "split", "combined",
		"combined", "split", "combined", "split", "split", "split",
	}
	splitSeen := 0
	for i := 0; i < 24; i++ {
		phase := float64(i) * 15
		w := mustWorld(t)
		cursorIdx, _, cursor := setupKernCursor(t, w)
		c := w.ActiveCraft()
		n := c.State.R.Cross(c.State.V).Unit()
		c.State.R = rotAbout(c.State.R, n, phase)
		c.State.V = rotAbout(c.State.V, n, phase)

		if _, err := w.PlanTransfer(cursorIdx); err != nil {
			t.Fatalf("phase %.0f°: PlanTransfer(Cursor): %v", phase, err)
		}
		strategy := w.LastTransfer.Strategy
		if strategy != wantStrategy[i] {
			t.Errorf("phase %.0f°: strategy = %q, want %q (selection must stay centre-aimed, ADR 0006 B)",
				phase, strategy, wantStrategy[i])
		}
		if strategy != "split" {
			continue
		}
		splitSeen++

		pass, ok := w.PlannedSOIPass()
		if !ok {
			t.Errorf("phase %.0f°: split plant produced no PlannedSOIPass", phase)
			continue
		}
		if pass.Body.ID != cursor.ID {
			t.Errorf("phase %.0f°: planned pass at %s, want Cursor", phase, pass.Body.ID)
			continue
		}
		alt := pass.PeriluneAltitude()
		const captureAlt = 200e3
		t.Logf("phase %3.0f°: split planned pass perilune alt = %8.1f km", phase, alt/1e3)
		if alt <= 0 {
			t.Errorf("phase %.0f°: split plant is a collision course (perilune alt %.1f km ≤ 0)", phase, alt/1e3)
		} else if math.Abs(alt-captureAlt) > 0.5*captureAlt {
			t.Errorf("phase %.0f°: split perilune alt %.1f km not near Capture Orbit alt %.1f km",
				phase, alt/1e3, captureAlt/1e3)
		}

		// Predicted final orbit at Cursor: bound and non-degenerate.
		state, primary, okFinal := w.PredictedFinalOrbit()
		if !okFinal {
			t.Errorf("phase %.0f°: PredictedFinalOrbit ok=false after split plant", phase)
			continue
		}
		if primary.ID != cursor.ID {
			t.Errorf("phase %.0f°: predicted final orbit around %s, want Cursor", phase, primary.ID)
			continue
		}
		el := orbital.ElementsFromState(state.R, state.V, cursor.GravitationalParameter())
		rp := el.A * (1 - el.E)
		t.Logf("phase %3.0f°: final orbit e=%.3f a=%.1f km rp=%.1f km", phase, el.E, el.A/1e3, rp/1e3)
		if el.E >= 1 || el.A <= 0 {
			t.Errorf("phase %.0f°: predicted final orbit unbound (e=%.3f, a=%.1f km)", phase, el.E, el.A/1e3)
		}
		if rp <= cursor.RadiusMeters() {
			t.Errorf("phase %.0f°: predicted final orbit periapsis %.1f km inside Cursor (radius %.1f km) — degenerate capture",
				phase, rp/1e3, cursor.RadiusMeters()/1e3)
		}
	}
	if splitSeen == 0 {
		t.Fatal("sweep never selected the split strategy — the regression case is untested")
	}
	t.Logf("split selected on %d of 24 phases", splitSeen)
}
