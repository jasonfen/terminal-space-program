package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestBurnVectorActiveBurnCommandsCapturedDir: when a BurnVector burn is
// in flight, the craft's commanded attitude is the captured inertial
// direction — not anything derived from (r, v). Wires attitudeContext →
// commandedDirFor → BurnDirectionForBurn for the BurnVector ActiveBurn.
func TestBurnVectorActiveBurnCommandsCapturedDir(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	dir := orbital.Vec3{X: 1, Y: -2, Z: 2} // arbitrary, not unit
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnVector,
		DVRemaining: 50,
		EndTime:     w.Clock.SimTime.Add(time.Minute),
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
		BurnDirUnit: dir,
	}
	got := w.commandedDirFor(c)
	if got.Sub(dir.Unit()).Norm() > 1e-12 {
		t.Errorf("commanded dir = %v, want captured %v", got, dir.Unit())
	}
}

// TestBurnVectorImpulsiveFiresAlongCapturedDir: an impulsive BurnVector
// node applies its Δv along the captured inertial direction, regardless
// of the craft's velocity orientation. Captured dir is radial-out (not
// prograde) so a velocity-derived mode would give a different answer.
func TestBurnVectorImpulsiveFiresAlongCapturedDir(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.Thrust = 0 // force the impulsive path (BurnTimeForDV == 0)

	dir := c.State.R.Unit() // radial-out, ⟂ to the (circular) velocity
	const dv = 100.0
	vBefore := c.State.V
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime,
		Mode:        spacecraft.BurnVector,
		DV:          dv,
		PrimaryID:   c.Primary.ID,
		BurnDirUnit: dir,
	})
	w.Tick() // advances sim-time past TriggerTime → node fires

	dV := c.State.V.Sub(vBefore)
	// The applied Δv should be dir·dv plus a small one-tick gravity/coast
	// perturbation. Check alignment and magnitude.
	if cos := dV.Unit().Dot(dir); cos < 0.999 {
		t.Errorf("Δv not aligned with captured dir: cos=%.5f (Δv=%v, dir=%v)", cos, dV, dir)
	}
	if d := math.Abs(dV.Norm() - dv); d > 1.0 {
		t.Errorf("|Δv| = %.3f, want ≈ %.0f", dV.Norm(), dv)
	}
}

// TestBurnVectorFiniteRaisesApoapsis: a finite BurnVector node along the
// craft's prograde direction, fired under the default slew path
// (InstantSAS off), delivers thrust — the orbit's apoapsis rises. Wires
// the BurnVector ActiveBurn through the slew integrator (commandedDirFor)
// and ThrustAccelFnFixedDir(CurrentAttitudeDir).
func TestBurnVectorFiniteRaisesApoapsis(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	apoBefore := orbital.ElementsFromState(c.State.R, c.State.V, mu).Apoapsis()

	dir := c.State.V.Unit() // prograde — CurrentAttitudeDir already ≈ here
	const dv = 80.0
	dur := c.BurnTimeForDV(dv)
	if dur <= 0 {
		t.Fatal("expected a finite burn duration for the default craft")
	}
	now := w.Clock.SimTime
	w.PlanNode(ManeuverNode{
		TriggerTime: now.Add(dur), // burn-center far enough out that BurnStart ≥ now
		Mode:        spacecraft.BurnVector,
		DV:          dv,
		Duration:    dur,
		PrimaryID:   c.Primary.ID,
		BurnDirUnit: dir,
	})

	burnEnd := now.Add(dur).Add(dur) // TriggerTime + Duration/2 + margin
	for tick := 0; tick < 200000; tick++ {
		w.Tick()
		if w.Clock.SimTime.After(burnEnd) && c.ActiveBurn == nil {
			break
		}
	}
	apoAfter := orbital.ElementsFromState(c.State.R, c.State.V, mu).Apoapsis()
	if apoAfter <= apoBefore+1000 {
		t.Errorf("apoapsis did not rise: before=%.0f after=%.0f", apoBefore, apoAfter)
	}
}
