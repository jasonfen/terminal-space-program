package physics

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

const (
	raiseMu = 3.986004418e14
	raiseRe = 6.371e6
)

func circularEquatorial(altM float64) StateVector {
	r := raiseRe + altM
	return StateVector{R: orbital.Vec3{X: r}, V: orbital.Vec3{Y: math.Sqrt(raiseMu / r)}, M: 800}
}

// raiseTo steps a spiral to targetA in ticks of dt and returns the end state,
// the thrust seconds, the unwrapped angle swept and whether it finished.
func raiseTo(t *testing.T, s StateVector, aT, dt, targetA float64) (StateVector, float64, float64, bool) {
	t.Helper()
	var used, swept float64
	prev := math.Atan2(s.R.Y, s.R.X)
	for i := 0; i < 2_000_000; i++ {
		n, u, done, ok := RaiseStep(s, raiseMu, aT, dt, targetA)
		if !ok {
			t.Fatalf("RaiseStep ok=false at step %d", i)
		}
		s, used = n, used+u
		a := math.Atan2(s.R.Y, s.R.X)
		d := a - prev
		for d < 0 {
			d += 2 * math.Pi
		}
		swept += d // a tick sweeps under 2π here (dt ≤ 5000 s)
		prev = a
		if done {
			return s, used, swept, true
		}
	}
	return s, used, swept, false
}

// Guard 4 (ADR 0059 decision 13): the spiral's end speed, time and swept
// angle against a numeric quadrature of ∫ n dt that shares no code with the
// closed form, at ticks from 50 s to 5,000 s.
func TestRaiseStepSpiralMatchesQuadratureAtAnyTick(t *testing.T) {
	const aT = 0.2103e-3
	start, target := circularEquatorial(290e3), raiseRe+550e3
	v0, vT := math.Sqrt(raiseMu/(raiseRe+290e3)), math.Sqrt(raiseMu/target)

	// Quadrature: v falls linearly, n = v³/μ.
	tEnd := (v0 - vT) / aT
	const steps = 4_000_000
	var theta float64
	for i := 0; i < steps; i++ {
		v := v0 - aT*(float64(i)+0.5)*tEnd/steps
		theta += v * v * v / raiseMu * tEnd / steps
	}

	for _, dt := range []float64{50, 5000} {
		end, used, swept, done := raiseTo(t, start, aT, dt, target)
		if !done {
			t.Fatalf("dt %v: never finished", dt)
		}
		if math.Abs(used-tEnd) > 1e-3 {
			t.Errorf("dt %v: thrust time %.3f s, want %.3f", dt, used, tEnd)
		}
		if got := end.V.Norm(); math.Abs(got-vT) > 0.5 {
			t.Errorf("dt %v: end speed %.3f, want %.3f", dt, got, vT)
		}
		if math.Abs(end.R.Norm()-target) > 1 {
			t.Errorf("dt %v: end radius %.3f, want %.3f", dt, end.R.Norm(), target)
		}
		// the tick that finishes also coasts the remainder of its dt, so the
		// total sweep is theta plus n·(dt·ticks − tEnd); compare the spiral only
		// by integer-aligned geometry: sweep minus the coast.
		coast := math.Sqrt(raiseMu/(target*target*target)) * (math.Ceil(tEnd/dt)*dt - tEnd)
		if math.Abs(swept-coast-theta) > 1e-6 {
			t.Errorf("dt %v: swept %.9f rad, want %.9f", dt, swept-coast, theta)
		}
	}
	// ADR 0059 guard 4 says 699,000 ± 1,000 s, but that divides the rounded
	// 147 m/s by aT; the unrounded 146.7 m/s gives 697,5xx s.
	if math.Abs(tEnd-(v0-vT)/aT) > 0 || tEnd < 697e3 || tEnd > 698e3 {
		t.Errorf("tEnd %.0f s, want about 697,500", tEnd)
	}
}

// Guard 4: one 5,000 s step and 100,000 steps of 0.05 s end in the same state
// to 1 m and 1 mm/s.
func TestRaiseStepFineAndCoarseTicksAgree(t *testing.T) {
	const aT = 0.2103e-3
	s0 := circularEquatorial(290e3)
	target := raiseRe + 550e3
	coarse, _, _, _ := RaiseStep(s0, raiseMu, aT, 5000, target)
	fine := s0
	for i := 0; i < 100_000; i++ {
		fine, _, _, _ = RaiseStep(fine, raiseMu, aT, 0.05, target)
	}
	if d := coarse.R.Sub(fine.R).Norm(); d > 1 {
		t.Errorf("positions differ by %.3f m", d)
	}
	if d := coarse.V.Sub(fine.V).Norm(); d > 1e-3 {
		t.Errorf("velocities differ by %.6f m/s", d)
	}
}

// A lowering is the same spiral with the speed rising.
func TestRaiseStepLowers(t *testing.T) {
	const aT = 0.2e-3
	start, target := circularEquatorial(550e3), raiseRe+200e3
	end, used, _, done := raiseTo(t, start, aT, 1000, target)
	vT := math.Sqrt(raiseMu / target)
	want := (vT - math.Sqrt(raiseMu/(raiseRe+550e3))) / aT
	if !done || math.Abs(used-want) > 1e-3 || math.Abs(end.V.Norm()-vT) > 0.5 {
		t.Errorf("done=%v used=%.1f want %.1f v=%.2f want %.2f", done, used, want, end.V.Norm(), vT)
	}
}

func TestRaiseStepRefusesUnbound(t *testing.T) {
	hyper := StateVector{R: orbital.Vec3{X: 7e6}, V: orbital.Vec3{Y: 12e3}}
	if _, _, _, ok := RaiseStep(hyper, raiseMu, 1e-4, 10, 8e6); ok {
		t.Error("hyperbolic state stepped")
	}
	if _, _, _, ok := RaiseStep(circularEquatorial(300e3), raiseMu, 0, 10, 8e6); ok {
		t.Error("zero thrust stepped")
	}
}
