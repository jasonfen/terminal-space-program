package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// #251: the degrade watchdog thresholds a Kepler PREDICTION of the approach
// at τ, whose error scales with the remaining horizon — it is worst at coast
// start, exactly when the baseline is captured. A fixed 10 km bar against
// that baseline false-fired at 0.18% deviation on a 5,605 km encounter:
// ~1 m/s of relayed-velocity error is tens of km of position error at τ
// across a multi-hour coast, with neither player maneuvering. These tests
// pin the fix's two halves — a bar that scales with the encounter, and a
// baseline that re-bases while healthy so estimator convergence is never
// read as drift — plus the property that a real step-change still fires.

// coastPartner returns the anchor's own orbit phase-shifted lead seconds
// ahead: a coasting partner on the same circular orbit, so the TRUE approach
// at τ is a constant chord at every measurement tick — any movement the
// watchdog sees is pure estimator error.
func coastPartner(t *testing.T, w *World, lead float64) physics.StateVector {
	t.Helper()
	a := w.ActiveCraft()
	p, ok := physics.KeplerStep(physics.StateVector{R: a.State.R, V: a.State.V, M: 1},
		a.Primary.GravitationalParameter(), lead)
	if !ok {
		t.Fatalf("KeplerStep(lead=%v s) failed", lead)
	}
	return p
}

// driveNoisyCoast engages a rendezvous coast toward a partner lead seconds
// ahead on the anchor's orbit and drives it tick by tick across total
// sim-time. Both craft coast exactly (truth is Kepler-propagated each tick);
// the partner's RELAYED velocity carries a constant `noise` m/s prograde
// error — report/subspace-gap uncertainty, not a maneuver. Returns the tick
// indices on which the degrade flag was up (want: none).
func driveNoisyCoast(t *testing.T, w *World, lead, noise float64, step, total time.Duration) []int {
	t.Helper()
	a := w.ActiveCraft()
	mu := a.Primary.GravitationalParameter()
	a0 := physics.StateVector{R: a.State.R, V: a.State.V, M: 1}
	p0 := coastPartner(t, w, lead)
	st := w.Clock.SimTime
	tau := st.Add(total + 10*time.Minute)
	if !w.EngageRendezvousWarp("SHA256:gern", "gern", tau, p0.R.Sub(a0.R).Norm()) {
		t.Fatal("EngageRendezvousWarp refused")
	}
	var fired []int
	for i := 0; time.Duration(i)*step <= total; i++ {
		now := st.Add(time.Duration(i) * step)
		w.Clock.SimTime = now
		dt := now.Sub(st).Seconds()
		mine, ok := physics.KeplerStep(a0, mu, dt)
		if !ok {
			t.Fatalf("anchor KeplerStep(dt=%v) failed", dt)
		}
		a.State.R, a.State.V = mine.R, mine.V
		theirs, ok := physics.KeplerStep(p0, mu, dt)
		if !ok {
			t.Fatalf("partner KeplerStep(dt=%v) failed", dt)
		}
		relayedV := theirs.V.Add(theirs.V.Scale(noise / theirs.V.Norm()))
		w.DriveRendezvousWarp([]CoWarpPeer{{
			Owner: "SHA256:gern", Handle: "gern", SubspaceTime: now, EffWarp: 50,
			ArmedTowardViewer: true,
			Crafts:            []CoWarpCraft{{Primary: a.Primary.ID, R: theirs.R, V: relayedV}},
		}})
		if !w.rendezvousWarpEngaged() {
			t.Fatalf("coast not engaged at tick %d", i)
		}
		if w.RendezvousDegraded {
			fired = append(fired, i)
		}
	}
	return fired
}

// A distant committed encounter (~4,500 km — same family as the playtest's
// 5,605 km) with 1 m/s of relayed-velocity noise and NO maneuver must never
// degrade: the τ-estimate walks ~30 km across the 3 h coast while the real
// approach is a constant chord. The proportional bar absorbs it.
func TestRendezvousDegradeToleratesRelayNoiseAtDistance(t *testing.T) {
	w, _, _ := anchorWorld(t)
	fired := driveNoisyCoast(t, w, 600, 1.0, 3*time.Minute, 3*time.Hour)
	if len(fired) > 0 {
		t.Errorf("degrade false-fired on estimator noise at ticks %v (distant encounter, no maneuver)", fired)
	}
}

// A close committed encounter (~60 km chord): the proportional bar sits at
// its 10 km floor, so scaling alone cannot absorb the ~30 km the estimate
// converges by as the horizon shrinks — only re-basing against a RECENT
// measure keeps slow convergence from accumulating into a trip.
func TestRendezvousDegradeRebasesConvergingEstimate(t *testing.T) {
	w, _, _ := anchorWorld(t)
	fired := driveNoisyCoast(t, w, 8, 1.0, 3*time.Minute, 3*time.Hour)
	if len(fired) > 0 {
		t.Errorf("degrade false-fired on estimator convergence at ticks %v (close encounter, no maneuver)", fired)
	}
}

// Re-basing must not swallow a genuine maneuver: after a healthy stretch of
// coast (long enough for the baseline to have re-based), a step-change in
// the partner's relayed state — the signature of a burn — still fires
// against the recent baseline within the same tick.
func TestRendezvousDegradeStillFiresOnStepChange(t *testing.T) {
	w, _, _ := anchorWorld(t)
	a := w.ActiveCraft()
	mu := a.Primary.GravitationalParameter()
	a0 := physics.StateVector{R: a.State.R, V: a.State.V, M: 1}
	p0 := coastPartner(t, w, 8) // ~60 km chord: bar at the 10 km floor
	st := w.Clock.SimTime
	tau := st.Add(3 * time.Hour)
	if !w.EngageRendezvousWarp("SHA256:gern", "gern", tau, p0.R.Sub(a0.R).Norm()) {
		t.Fatal("EngageRendezvousWarp refused")
	}
	step := 3 * time.Minute
	tick := func(i int, dR float64) {
		now := st.Add(time.Duration(i) * step)
		w.Clock.SimTime = now
		dt := now.Sub(st).Seconds()
		mine, _ := physics.KeplerStep(a0, mu, dt)
		a.State.R, a.State.V = mine.R, mine.V
		theirs, _ := physics.KeplerStep(p0, mu, dt)
		// dR displaces the relayed position radially — a burn's step-change.
		r := theirs.R.Add(theirs.R.Scale(dR / theirs.R.Norm()))
		w.DriveRendezvousWarp([]CoWarpPeer{{
			Owner: "SHA256:gern", Handle: "gern", SubspaceTime: now, EffWarp: 50,
			ArmedTowardViewer: true,
			Crafts:            []CoWarpCraft{{Primary: a.Primary.ID, R: r, V: theirs.V}},
		}})
	}
	// A healthy 30 min of coast — several re-base windows.
	for i := 0; i <= 10; i++ {
		tick(i, 0)
		if w.RendezvousDegraded {
			t.Fatalf("degraded during the healthy coast at tick %d", i)
		}
	}
	// The partner burns: 50 km of instantaneous displacement.
	tick(11, 50_000)
	if !w.RendezvousDegraded {
		t.Error("no degrade after a 50 km step-change — re-basing swallowed a real maneuver")
	}
}
