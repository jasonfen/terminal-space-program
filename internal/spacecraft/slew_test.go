package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

func angBetween(a, b orbital.Vec3) float64 {
	c := a.Unit().Dot(b.Unit())
	if c > 1 {
		c = 1
	} else if c < -1 {
		c = -1
	}
	return math.Acos(c)
}

func TestSlewTowardConvergesInAngleOverRate(t *testing.T) {
	sc := &Spacecraft{
		CurrentAttitudeDir: orbital.Vec3{X: 1},
		SlewRateDegPerSec:  5,
	}
	cmd := orbital.Vec3{Y: 1} // 90° away
	dt := 1.0                 // 5°/tick

	prev := math.Pi / 2
	steps := 0
	for angBetween(sc.CurrentAttitudeDir, cmd) > 1e-6 {
		sc.SlewToward(cmd, dt)
		cur := angBetween(sc.CurrentAttitudeDir, cmd)
		// Monotone non-increasing, and the per-step movement never
		// exceeds the cap (SlewRate·dt).
		if cur > prev+1e-12 {
			t.Fatalf("step %d: angle increased %.9f -> %.9f", steps, prev, cur)
		}
		if moved := prev - cur; moved > sc.SlewRateRad()*dt+1e-9 {
			t.Fatalf("step %d: moved more than cap (Δ=%.6f cap=%.6f)",
				steps, moved, sc.SlewRateRad()*dt)
		}
		prev = cur
		steps++
		if steps > 100 {
			t.Fatal("did not converge in 100 steps")
		}
	}
	// 90° / 5° = 18 steps exactly.
	if steps != 18 {
		t.Errorf("converged in %d steps, want 18 (90°/5°)", steps)
	}
	if d := sc.CurrentAttitudeDir.Sub(cmd).Norm(); d > 1e-6 {
		t.Errorf("final dir off by %.3e", d)
	}
}

func TestSlewTowardZeroInitSnaps(t *testing.T) {
	sc := &Spacecraft{SlewRateDegPerSec: 5} // CurrentAttitudeDir zero
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	sc.SlewToward(cmd, 0.001) // tiny dt: would barely move if it slewed
	if d := sc.CurrentAttitudeDir.Sub(cmd).Norm(); d > 1e-12 {
		t.Errorf("zero-init did not snap to commanded: off by %.3e", d)
	}
}

func TestSlewTowardUndefinedCommandedHolds(t *testing.T) {
	start := orbital.Vec3{X: 1}
	sc := &Spacecraft{CurrentAttitudeDir: start, SlewRateDegPerSec: 5}
	sc.SlewToward(orbital.Vec3{}, 1.0) // zero commanded => hold
	if sc.CurrentAttitudeDir != start {
		t.Errorf("undefined commanded changed attitude: %+v", sc.CurrentAttitudeDir)
	}
}

func TestSlewTowardAntiparallelNoNaN(t *testing.T) {
	sc := &Spacecraft{
		CurrentAttitudeDir: orbital.Vec3{X: 1},
		SlewRateDegPerSec:  10,
	}
	cmd := orbital.Vec3{X: -1} // exactly 180°
	for i := 0; i < 50; i++ {
		sc.SlewToward(cmd, 1.0)
		d := sc.CurrentAttitudeDir
		if math.IsNaN(d.X) || math.IsNaN(d.Y) || math.IsNaN(d.Z) {
			t.Fatalf("NaN attitude at step %d: %+v", i, d)
		}
		if n := d.Norm(); math.Abs(n-1) > 1e-9 {
			t.Fatalf("step %d: non-unit attitude |d|=%.9f", i, n)
		}
	}
	if d := angBetween(sc.CurrentAttitudeDir, cmd); d > 1e-6 {
		t.Errorf("did not converge from antiparallel: residual %.6f rad", d)
	}
}

func TestSlewRateRadDefaultFallback(t *testing.T) {
	sc := &Spacecraft{} // unset rate
	want := DefaultSlewRateDegPerSec * math.Pi / 180
	if got := sc.SlewRateRad(); math.Abs(got-want) > 1e-12 {
		t.Errorf("SlewRateRad default = %.6f, want %.6f", got, want)
	}
}
