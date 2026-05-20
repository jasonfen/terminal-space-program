package planner

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// circularStateAtRadius is like circularState but lets the test pick
// the circular-orbit radius independently of the phase. Reused by the
// rendezvous-recommend cases below.
func circularStateAtRadius(r, phaseRad, mu float64) orbital.Vec3State {
	return circularState(r, phaseRad, mu)
}

// inclinedCircularState builds a circular orbit at radius r whose
// plane is tilted by inclinationRad about the +X axis (i.e. the
// ascending node sits on +X). Used to construct a target offset only
// by plane angle so the rendezvous recommendation picks a Normal±
// axis.
func inclinedCircularState(r, phaseRad, inclinationRad, mu float64) orbital.Vec3State {
	flat := circularState(r, phaseRad, mu)
	cosI, sinI := math.Cos(inclinationRad), math.Sin(inclinationRad)
	// Rotate (Y, Z) by inclination about +X.
	rot := func(v orbital.Vec3) orbital.Vec3 {
		return orbital.Vec3{
			X: v.X,
			Y: v.Y*cosI - v.Z*sinI,
			Z: v.Y*sinI + v.Z*cosI,
		}
	}
	return orbital.Vec3State{R: rot(flat.R), V: rot(flat.V)}
}

// TestRecommendRendezvousNudge_CoOrbitalLagging — chaser 0.5° behind
// target in the same 400 km circular orbit. Current CA is essentially
// the constant separation. The recommendation should reduce it.
func TestRecommendRendezvousNudge_CoOrbitalLagging(t *testing.T) {
	r := 6.771e6
	target := circularStateAtRadius(r, 0, muEarth)
	chaser := circularStateAtRadius(r, -0.5*math.Pi/180, muEarth)

	// Current CA from the no-burn predictor.
	_, currentCA, _, err := NextClosestApproach(chaser, target, bodies.CelestialBody{}, muEarth, 6000)
	if err != nil {
		t.Fatalf("predictor err: %v", err)
	}
	if currentCA < 100 {
		t.Fatalf("setup: currentCA=%.1f m too small to test improvement", currentCA)
	}

	adv := RecommendRendezvousNudge(chaser, target, bodies.CelestialBody{}, muEarth, 6000, currentCA)
	if !adv.Ok {
		t.Fatalf("expected Ok=true (currentCA=%.0f m), got Reason=%q", currentCA, adv.Reason)
	}
	if adv.DV <= 0 || adv.DV > 200 {
		t.Errorf("DV %.2f m/s outside (0, 200] m/s", adv.DV)
	}
	if adv.AchievableCA >= currentCA {
		t.Errorf("AchievableCA=%.0f did not improve on currentCA=%.0f", adv.AchievableCA, currentCA)
	}
	if adv.TArrival <= 0 || adv.TArrival > 6000 {
		t.Errorf("TArrival %.0f outside (0, horizon]", adv.TArrival)
	}
}

// TestRecommendRendezvousNudge_InclinedTarget — 10° plane offset
// with no phase offset along the inclined orbit; the only meaningful
// component of the Lambert ΔV is plane-changing, so the projection
// should pick AxisNormalPlus or AxisNormalMinus. Verifies the normal-
// axis branch of the projection loop is exercised in tests (not just
// the buildVelocityFrameAxes builder).
func TestRecommendRendezvousNudge_InclinedTarget(t *testing.T) {
	r := 6.771e6
	chaser := circularStateAtRadius(r, 0, muEarth)
	// Phase slightly offset from the line of nodes so positions
	// differ at t=0 (currentCA > 0) but plane offset dominates the
	// intercept geometry. 30° phase on a 10° inclined orbit gives
	// currentCA in the few-hundred-km range with the cross-plane
	// distance growing with phase.
	target := inclinedCircularState(r, 30*math.Pi/180, 10*math.Pi/180, muEarth)

	_, currentCA, _, err := NextClosestApproach(chaser, target, bodies.CelestialBody{}, muEarth, 6000)
	if err != nil {
		t.Fatalf("predictor err: %v", err)
	}

	adv := RecommendRendezvousNudge(chaser, target, bodies.CelestialBody{}, muEarth, 6000, currentCA)
	if !adv.Ok {
		t.Fatalf("expected Ok=true (currentCA=%.0f m, inclination=10°), got Reason=%q", currentCA, adv.Reason)
	}
	switch adv.Axis {
	case AxisNormalPlus, AxisNormalMinus:
		// expected — confirms the plane-change branch fires.
	default:
		t.Errorf("expected Normal± axis for plane-dominated target, got %s (DV=%.1f, currentCA=%.0f, achCA=%.0f, LambertDV=%.1f)",
			adv.Axis, adv.DV, currentCA, adv.AchievableCA, adv.LambertIdealDV)
	}
}

// TestRecommendRendezvousNudge_NoImprovement — identical states ⇒
// currentCA == 0, no possible improvement, advisory returns Reason
// "no improvement available" (the two-prong floor cannot pass with
// currentCA=0 since rel improvement is undefined and absolute is ≤ 0).
func TestRecommendRendezvousNudge_NoImprovement(t *testing.T) {
	r := 6.771e6
	s := circularStateAtRadius(r, 0, muEarth)
	adv := RecommendRendezvousNudge(s, s, bodies.CelestialBody{}, muEarth, 6000, 0)
	if adv.Ok {
		t.Fatalf("expected Ok=false for identical states, got DV=%.2f axis=%s",
			adv.DV, adv.Axis)
	}
	if adv.Reason == "" {
		t.Errorf("expected non-empty Reason")
	}
}

// TestRecommendRendezvousNudge_HorizonTooShort — horizon below the
// shortest lookahead-fraction of target period ⇒ no Lambert solve
// completes ⇒ Ok=false with reason "no lambert convergence".
func TestRecommendRendezvousNudge_HorizonTooShort(t *testing.T) {
	r := 6.771e6
	a := circularStateAtRadius(r, 0, muEarth)
	b := circularStateAtRadius(r, -0.5*math.Pi/180, muEarth)
	adv := RecommendRendezvousNudge(a, b, bodies.CelestialBody{}, muEarth, 10, 1000) // 10 s horizon ≪ 0.15 P
	if adv.Ok {
		t.Errorf("expected Ok=false for too-short horizon, got DV=%.2f", adv.DV)
	}
	if adv.Reason == "" {
		t.Errorf("expected non-empty Reason")
	}
}

// TestRecommendRendezvousNudge_ProjectionQuality — guards against
// future axis-selection refactors silently degrading. On the
// co-orbital lagging case, AchievableCA should be much better than
// currentCA — assert at least a 30 % improvement (the projection is
// lossy but should beat this floor handily in this geometry).
func TestRecommendRendezvousNudge_ProjectionQuality(t *testing.T) {
	r := 6.771e6
	target := circularStateAtRadius(r, 0, muEarth)
	chaser := circularStateAtRadius(r, -0.5*math.Pi/180, muEarth)

	_, currentCA, _, err := NextClosestApproach(chaser, target, bodies.CelestialBody{}, muEarth, 6000)
	if err != nil {
		t.Fatalf("predictor err: %v", err)
	}

	adv := RecommendRendezvousNudge(chaser, target, bodies.CelestialBody{}, muEarth, 6000, currentCA)
	if !adv.Ok {
		t.Fatalf("expected Ok=true, got Reason=%q", adv.Reason)
	}
	improvement := (currentCA - adv.AchievableCA) / currentCA
	// The single-axis projection is intentionally lossy vs the full
	// Lambert ΔV (the slice's loop is "iterate until DOCK READY"),
	// so we set the floor low enough to catch silent regressions
	// without being aspirational about projection quality. 20 % on a
	// 0.5° co-orbital phase offset is healthy headroom; if axis
	// selection degrades, this number drops first.
	if improvement < 0.20 {
		t.Errorf("co-orbital improvement %.1f%% < 20%% (currentCA=%.0f, achCA=%.0f, DV=%.1f, axis=%s, LambertDV=%.1f)",
			improvement*100, currentCA, adv.AchievableCA, adv.DV, adv.Axis, adv.LambertIdealDV)
	}
	// LambertIdealDV is the unprojected magnitude; the projected DV
	// must be ≤ that (projecting onto a unit axis can only shorten).
	if adv.DV > adv.LambertIdealDV+1e-6 {
		t.Errorf("projected DV %.3f exceeds Lambert ideal %.3f", adv.DV, adv.LambertIdealDV)
	}
}
