package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestDragDecaysLowPerigeeOrbit: craft on a 140 km × 200 km elliptical
// orbit — perigee dips into atmosphere (cutoff = 150 km on Earth).
// After one orbital period the apoapsis must shrink measurably from
// the energy bled to drag at perigee. Uses default BC. The 140 km
// perigee is chosen so density (and integrated Δv loss) gives
// detectable decay without a runaway / surface-impact in one orbit.
func TestDragDecaysLowPerigeeOrbit(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	earth := c.Primary
	mu := earth.GravitationalParameter()
	rp := earth.RadiusMeters() + 140e3
	ra := earth.RadiusMeters() + 200e3
	a := 0.5 * (rp + ra)
	vp := math.Sqrt(mu * (2/rp - 1/a))
	c.State = physics.StateVector{
		R: orbital.Vec3{X: rp},
		V: orbital.Vec3{Y: vp},
		M: c.TotalMass(),
	}
	startApo := orbital.ElementsFromState(c.State.R, c.State.V, mu).Apoapsis()

	// Period ≈ 5290s. Step at 10s for 530 ticks ≈ 1 period.
	dt := 10 * time.Second
	for i := 0; i < 530; i++ {
		w.Clock.SimTime = w.Clock.SimTime.Add(dt)
		w.integrateOneCraft(c, dt)
	}

	endEl := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	endApo := endEl.Apoapsis()

	if !(endApo < startApo) {
		t.Errorf("drag should decay apoapsis after one period: start=%.1f km alt, end=%.1f km alt",
			(startApo-earth.RadiusMeters())/1000,
			(endApo-earth.RadiusMeters())/1000)
	}
	// Decay should be at least 1 km — a soft floor that catches a
	// drag-zeroed regression without being so tight it depends on
	// integrator floating-point noise.
	if startApo-endApo < 1000 {
		t.Errorf("expected ≥ 1 km apoapsis decay, got %.0f m (drag effectively absent)", startApo-endApo)
	}
	// Sanity: orbit hasn't gone hyperbolic / NaN.
	if endApo < 0 || math.IsNaN(endApo) || math.IsInf(endApo, 0) {
		t.Errorf("apoapsis %g — integrator blew up", endApo)
	}
}

// TestDragSkippedAboveAtmosphere: a craft at 500 km circular (well above
// the 150 km cutoff) must conserve apoapsis under the new drag-aware
// integrator — drag is zero up there, drift should match plain Verlet.
func TestDragSkippedAboveAtmosphere(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	r0 := c.State.R.Norm()
	mu := c.Primary.GravitationalParameter()

	dt := 1000 * time.Second
	for i := 0; i < 30; i++ {
		w.Clock.SimTime = w.Clock.SimTime.Add(dt)
		w.integrateOneCraft(c, dt)
	}

	rEnd := c.State.R.Norm()
	// Same 1 % tolerance the existing TestPropagateCraftPreservesCircularRadius
	// applies to the predictor: circular LEO holds radius to within
	// the integrator's noise floor.
	if math.Abs(rEnd-r0)/r0 > 0.01 {
		t.Errorf("circular orbit drift > 1%% above atmosphere: r0=%.0f, rEnd=%.0f, mu=%g",
			r0, rEnd, mu)
	}
}

// TestKeplerWarpLockSkippedInsideAtmosphere: a low-periapsis orbit must
// NOT take the analytic Kepler fast path under warp — drag would be
// silently skipped. canKeplerStep should reject when periapsis is
// below atmospheric cutoff.
func TestKeplerWarpLockSkippedInsideAtmosphere(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	rp := c.Primary.RadiusMeters() + 90e3
	ra := c.Primary.RadiusMeters() + 200e3
	a := 0.5 * (rp + ra)
	vp := math.Sqrt(mu * (2/rp - 1/a))
	c.State = physics.StateVector{
		R: orbital.Vec3{X: rp},
		V: orbital.Vec3{Y: vp},
		M: c.TotalMass(),
	}
	// Bump warp so canKeplerStep's warp>1 gate is satisfied.
	w.Clock.WarpUp()

	if w.canKeplerStep(c, time.Second) {
		t.Error("canKeplerStep allowed analytic propagation while perigee is inside Earth's atmosphere — drag would be skipped")
	}
}
