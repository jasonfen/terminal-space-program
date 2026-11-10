package sim

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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

// TestHighWarpEntryStaysBounded: a lunar return (apoapsis 615,900 km,
// perigee 70 km) entering at 94 km with warp at the top. The period-based
// sub-step (period/100, ~17,500 s here) gave one 50 s step per tick at
// 1000x, which dove into dense air and crashed the CSM at 528,345 m/s.
// Inside the atmosphere warp is held at 1000x and each step is short, so
// the vessel only ever slows; at the cutoff itself neither clamp applies.
func TestHighWarpEntryStaysBounded(t *testing.T) {
	w := mustWorld(t)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	c := w.ActiveCraft()
	// The test is about step size, not the hull: hold the drag at the old
	// stock figure (the default stack's size-and-mass figure is 30x lower and
	// never reaches the ground in 2000 ticks).
	c.Stages[0].BallisticCoefficient = spacecraft.DefaultBallisticCoefficient
	earth := c.Primary
	mu := earth.GravitationalParameter()
	R := earth.RadiusMeters()
	rp, ra := R+70e3, R+615_900e3
	a, e := (rp+ra)/2, (ra-rp)/(ra+rp)
	p := a * (1 - e*e)
	place := func(alt float64) {
		r := R + alt
		nu := -math.Acos((p/r - 1) / e) // inbound
		h := math.Sqrt(mu * p)
		c.State.R = orbital.Vec3{X: r * math.Cos(nu), Y: r * math.Sin(nu)}
		c.State.V = orbital.Vec3{X: -mu / h * math.Sin(nu), Y: mu / h * (e + math.Cos(nu))}
	}

	place(earth.Atmosphere.CutoffAltitude)
	if eff := w.EffectiveWarp(); eff <= atmosphereWarpCap {
		t.Errorf("at the cutoff altitude warp is %.0f, want above the %.0fx atmosphere cap", eff, atmosphereWarpCap)
	}
	place(94e3)
	if eff := w.EffectiveWarp(); eff != atmosphereWarpCap {
		t.Fatalf("entering at 94 km warp is %.0f, want %.0f", eff, atmosphereWarpCap)
	}
	dir := recAttach(t, w)
	entry := c.State.V.Norm()
	tick := time.Duration(atmosphereWarpCap * 0.05 * float64(time.Second)) // one 20 Hz tick at the cap
	for i := 0; i < 2000 && !c.Crashed && !c.Landed; i++ {
		w.Clock.SimTime = w.Clock.SimTime.Add(tick)
		w.integrateOneCraft(c, tick)
		if v := c.State.V.Norm(); v > entry+200 || math.IsNaN(v) {
			t.Fatalf("tick %d at %.1f km: speed %.0f m/s, entered at %.0f", i, c.Altitude()/1e3, v, entry)
		}
	}
	// The surface clamp zeroes V, so the impact speed is read off the crash row.
	found := false
	for _, ev := range allEvents(t, dir) {
		var lat, lon string
		var v float64
		if n, _ := fmt.Sscanf(strings.ReplaceAll(ev, ",", ""), "crash: Earth %s %s %f m/s", &lat, &lon, &v); n == 3 {
			found = true
			t.Logf("%s (entered at %.0f m/s)", ev, entry)
			if v > entry {
				t.Errorf("hit the ground at %.0f m/s after entering at %.0f (%s)", v, entry, ev)
			}
		}
	}
	if !found {
		t.Fatal("no crash row: the entry never reached the ground, so the impact speed went unchecked")
	}
}
