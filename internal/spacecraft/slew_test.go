package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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

func TestBodyFrameOrthonormalAndHeadsUp(t *testing.T) {
	sc := &Spacecraft{
		CurrentAttitudeDir: orbital.Vec3{X: 1},                           // nose +X
		State:              physics.StateVector{R: orbital.Vec3{Z: 7e6}}, // radial-out +Z
	}
	nose, up, right, ok := sc.BodyFrame()
	if !ok {
		t.Fatal("BodyFrame ok=false on a valid craft")
	}
	for name, d := range map[string]float64{
		"nose·up": nose.Dot(up), "nose·right": nose.Dot(right), "up·right": up.Dot(right),
	} {
		if math.Abs(d) > 1e-9 {
			t.Errorf("%s not orthogonal: %.3e", name, d)
		}
	}
	for name, v := range map[string]orbital.Vec3{"nose": nose, "up": up, "right": right} {
		if math.Abs(v.Norm()-1) > 1e-9 {
			t.Errorf("%s not unit: |v|=%.9f", name, v.Norm())
		}
	}
	// Roll 0 ⇒ up is the radial-out direction (heads-up / belly-down).
	if d := up.Sub(orbital.Vec3{Z: 1}).Norm(); d > 1e-9 {
		t.Errorf("roll-0 up should be radial-out +Z; got %+v", up)
	}
	if d := right.Sub(up.Cross(nose)).Norm(); d > 1e-9 {
		t.Errorf("right should equal up×nose")
	}
}

func TestBodyFrameRollRotatesUp(t *testing.T) {
	sc := &Spacecraft{
		CurrentAttitudeDir: orbital.Vec3{X: 1},
		State:              physics.StateVector{R: orbital.Vec3{Z: 7e6}},
		CurrentRollDeg:     90,
	}
	_, up, _, ok := sc.BodyFrame()
	if !ok {
		t.Fatal("BodyFrame ok=false")
	}
	// up0 = +Z rotated 90° about nose +X ⇒ −Y.
	if d := up.Sub(orbital.Vec3{Y: -1}).Norm(); d > 1e-9 {
		t.Errorf("roll 90° about +X should put up at −Y; got %+v", up)
	}
}

func TestBodyFrameZeroNose(t *testing.T) {
	sc := &Spacecraft{State: physics.StateVector{R: orbital.Vec3{Z: 7e6}}}
	if _, _, _, ok := sc.BodyFrame(); ok {
		t.Error("BodyFrame should be ok=false with uninitialised nose")
	}
}

func TestBodyFrameNoseParallelRadialFallsBack(t *testing.T) {
	// Nose pointing straight up (∥ radial) — heads-up reference must
	// fall back to a defined perpendicular, not blow up.
	sc := &Spacecraft{
		CurrentAttitudeDir: orbital.Vec3{Z: 1},
		State:              physics.StateVector{R: orbital.Vec3{Z: 7e6}},
	}
	nose, up, right, ok := sc.BodyFrame()
	if !ok {
		t.Fatal("BodyFrame ok=false at nose∥radial")
	}
	if math.Abs(nose.Dot(up)) > 1e-9 || math.Abs(up.Norm()-1) > 1e-9 {
		t.Errorf("fallback up not a valid unit ⟂ nose: up=%+v", up)
	}
	_ = right
}

func TestRollTowardConvergesShortestPath(t *testing.T) {
	sc := &Spacecraft{SlewRateDegPerSec: 5, CurrentRollDeg: -170, CommandedRollDeg: 170}
	prev := 20.0 // shortest signed distance is −20°, |diff| starts at 20
	for i := 0; i < 100; i++ {
		sc.RollToward(1.0)
		d := math.Abs(wrapDeg180(sc.CommandedRollDeg - sc.CurrentRollDeg))
		if d > prev+1e-9 {
			t.Fatalf("step %d: roll error grew %.3f → %.3f (took the long way)", i, prev, d)
		}
		prev = d
		if d < 1e-9 {
			if i > 6 { // 20°/5°·s ⇒ ~4 ticks; must not be ~68 (long way)
				t.Fatalf("converged in %d ticks — took the 340° long way", i+1)
			}
			return
		}
	}
	t.Fatalf("RollToward did not converge; residual %.3f", prev)
}

func TestSlewRateRadDefaultFallback(t *testing.T) {
	sc := &Spacecraft{} // unset rate
	want := DefaultSlewRateDegPerSec * math.Pi / 180
	if got := sc.SlewRateRad(); math.Abs(got-want) > 1e-12 {
		t.Errorf("SlewRateRad default = %.6f, want %.6f", got, want)
	}
}
