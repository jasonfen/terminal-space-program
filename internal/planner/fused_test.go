package planner

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

const earthMu = 3.986004418e14 // m³/s²

// propagateTwoBody is an independent two-body propagator (analytic
// Kepler) used to verify a fused-Lambert departure: applying the
// returned Δv to the departure state and coasting for tof must land on
// the target's arrival position.
func propagateTwoBody(t *testing.T, r, v orbital.Vec3, mu, tof float64) orbital.Vec3 {
	t.Helper()
	s, ok := physics.KeplerStep(physics.StateVector{R: r, V: v}, mu, tof)
	if !ok {
		t.Fatalf("KeplerStep failed for r=%v v=%v (hyperbolic/degenerate?)", r, v)
	}
	return s.R
}

// TestPlanIntraPrimaryFusedEccentricCoplanarReachesTarget: the departure
// burn from an eccentric (non-circular) coplanar parking orbit, when
// propagated two-body for the transfer time, lands on the target's
// arrival position. The fused solve handles the eccentric departure with
// no circular-radius assumption.
func TestPlanIntraPrimaryFusedEccentricCoplanarReachesTarget(t *testing.T) {
	mu := earthMu
	// Eccentric, prograde parking orbit in the XY plane (circular speed
	// at 7e6 m is ~7546 m/s; 8600 m/s makes it eccentric).
	rDep := orbital.Vec3{X: 7.0e6}
	vDep := orbital.Vec3{Y: 8600}
	// Target arrival point, coplanar (XY), and its (circular-ish) velocity.
	rArr := orbital.Vec3{X: -42.0e6, Y: 8.0e6}
	vArrSpeed := math.Sqrt(mu / rArr.Norm())
	vArr := orbital.Vec3{X: -rArr.Y, Y: rArr.X}.Unit().Scale(vArrSpeed)
	tof := 9000.0

	plan, err := PlanIntraPrimaryFused(mu, rDep, vDep, rArr, vArr, tof,
		0, "earth", 4.9e12, 1.9e6, "moon")
	if err != nil {
		t.Fatalf("PlanIntraPrimaryFused: %v", err)
	}
	if plan.Departure.BurnDir.Norm() == 0 {
		t.Fatal("departure leg has no BurnDir — fused departure must be a vector")
	}
	if d := math.Abs(plan.Departure.BurnDir.Norm() - 1); d > 1e-9 {
		t.Errorf("BurnDir not unit: |dir|=%.12f", plan.Departure.BurnDir.Norm())
	}

	// v1 = vDep + Δv·dir. Coast two-body for tof; must reach rArr.
	v1 := vDep.Add(plan.Departure.BurnDir.Scale(plan.Departure.DV))
	got := propagateTwoBody(t, rDep, v1, mu, tof)
	if miss := got.Sub(rArr).Norm(); miss > 1000 {
		t.Errorf("arrival miss = %.1f m (got %v, want %v); want < 1 km", miss, got, rArr)
	}
}

// TestPlanIntraPrimaryFusedInclinedReachesTargetPlane: an out-of-plane
// target (arrival position has a Z component) is reached by the single
// fused departure burn — the plane change is folded into the departure,
// not a separate node. And the inclined transfer costs more Δv than the
// equivalent coplanar one.
func TestPlanIntraPrimaryFusedInclinedReachesTargetPlane(t *testing.T) {
	mu := earthMu
	rDep := orbital.Vec3{X: 7.0e6}
	vDep := orbital.Vec3{Y: 7546} // ~circular, equatorial (XY) plane
	tof := 9000.0

	// Coplanar reference target.
	rArrFlat := orbital.Vec3{X: -42.0e6, Y: 8.0e6}
	vFlat := orbital.Vec3{X: -rArrFlat.Y, Y: rArrFlat.X}.Unit().Scale(math.Sqrt(mu / rArrFlat.Norm()))
	flat, err := PlanIntraPrimaryFused(mu, rDep, vDep, rArrFlat, vFlat, tof,
		0, "earth", 4.9e12, 1.9e6, "moon")
	if err != nil {
		t.Fatalf("coplanar fused: %v", err)
	}

	// Inclined target: same horizontal geometry, lifted out of the XY plane.
	rArrIncl := orbital.Vec3{X: -42.0e6, Y: 8.0e6, Z: 12.0e6}
	vIncl := orbital.Vec3{X: -rArrIncl.Y, Y: rArrIncl.X}.Unit().Scale(math.Sqrt(mu / rArrIncl.Norm()))
	incl, err := PlanIntraPrimaryFused(mu, rDep, vDep, rArrIncl, vIncl, tof,
		0, "earth", 4.9e12, 1.9e6, "moon")
	if err != nil {
		t.Fatalf("inclined fused: %v", err)
	}

	// The inclined departure must carry an out-of-plane (Z) component.
	depDV := incl.Departure.BurnDir.Scale(incl.Departure.DV)
	if math.Abs(depDV.Z) < 100 {
		t.Errorf("inclined departure Δv has no out-of-plane component: Δv.Z=%.1f", depDV.Z)
	}

	// Coast two-body; must reach the out-of-plane target.
	v1 := vDep.Add(depDV)
	got := propagateTwoBody(t, rDep, v1, mu, tof)
	if miss := got.Sub(rArrIncl).Norm(); miss > 1000 {
		t.Errorf("inclined arrival miss = %.1f m; want < 1 km", miss)
	}

	// Plane change is not free — the inclined transfer costs more.
	if incl.Departure.DV <= flat.Departure.DV {
		t.Errorf("inclined departure Δv (%.1f) should exceed coplanar (%.1f)",
			incl.Departure.DV, flat.Departure.DV)
	}
}
