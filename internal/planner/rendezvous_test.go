package planner

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// muEarth is reused from transfer_test.go (same package).

// circularState builds a circular-orbit Vec3State at radius r with
// the velocity rotated by `phaseRad` from the +X axis (so two craft
// with different phases sit at different points on the same orbit).
func circularState(r, phaseRad, mu float64) orbital.Vec3State {
	cos, sin := math.Cos(phaseRad), math.Sin(phaseRad)
	v := math.Sqrt(mu / r)
	return orbital.Vec3State{
		R: orbital.Vec3{X: r * cos, Y: r * sin},
		V: orbital.Vec3{X: -v * sin, Y: v * cos},
	}
}

// TestNextClosestApproachCoOrbital — two craft on the same circular
// orbit, phase-offset 90°. Their separation is constant at r·√2;
// closest approach distance ≈ that value, t≈0 (or anywhere — the
// minimum is degenerate so we accept any t in [0, horizon]).
func TestNextClosestApproachCoOrbital(t *testing.T) {
	r := 6.771e6 // 400 km LEO
	a := circularState(r, 0, muEarth)
	b := circularState(r, math.Pi/2, muEarth)

	tCA, dist, vRel, err := NextClosestApproach(a, b, bodies.CelestialBody{}, muEarth, 6000)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := r * math.Sqrt(2)
	if math.Abs(dist-want)/want > 0.02 {
		t.Errorf("co-orbital separation: dist=%.0f, want≈%.0f", dist, want)
	}
	// |v_rel| for two circular co-orbits at phase π/2 is √2·v_circ.
	vCirc := math.Sqrt(muEarth / r)
	gotVRel := vRel.Norm()
	wantVRel := math.Sqrt(2) * vCirc
	if math.Abs(gotVRel-wantVRel)/wantVRel > 0.05 {
		t.Errorf("|v_rel|=%.1f, want≈%.1f", gotVRel, wantVRel)
	}
	if tCA < 0 || tCA > 6000 {
		t.Errorf("t out of horizon: %v", tCA)
	}
}

// TestNextClosestApproachIdenticalStatesIsZero — same state for both
// craft → distance is 0 at t=0.
func TestNextClosestApproachIdenticalStatesIsZero(t *testing.T) {
	r := 6.771e6
	s := circularState(r, 0, muEarth)
	tCA, dist, _, err := NextClosestApproach(s, s, bodies.CelestialBody{}, muEarth, 3600)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if tCA != 0 {
		t.Errorf("identical states: t=%v, want 0", tCA)
	}
	if dist > 1.0 {
		t.Errorf("identical states: dist=%v, want ≈0", dist)
	}
}

// TestNextClosestApproachCatchesUp — A trails B by a small phase
// offset on the same orbit; A is given a slight prograde boost so
// it catches up. NextClosestApproach should return a positive t with
// dist near zero (intercept).
//
// 100 m/s prograde on a 400 km LEO drops semimajor axis a tiny bit
// and shifts mean motion — the phase closes over multiple orbits.
// Horizon: 4 hours, plenty for catch-up.
func TestNextClosestApproachCatchesUp(t *testing.T) {
	r := 6.771e6
	// A at phase 0, B at phase +0.05 rad (≈340 km ahead).
	a := circularState(r, 0, muEarth)
	b := circularState(r, 0.05, muEarth)
	// Boost A prograde by 5 m/s (small, so the orbit barely changes
	// shape but mean motion shifts enough to close the phase gap).
	progradeUnit := orbital.Vec3{X: a.V.X, Y: a.V.Y}
	n := progradeUnit.Norm()
	progradeUnit = progradeUnit.Scale(1 / n)
	a.V = a.V.Add(progradeUnit.Scale(5))

	_, dist, _, err := NextClosestApproach(a, b, bodies.CelestialBody{}, muEarth, 4*3600)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Initial separation ≈ 339 km; expect closest approach to drop
	// significantly below that as the phase closes.
	if dist > 5e5 {
		t.Errorf("catch-up: dist at closest approach %.0f m, expected to drop below 500 km", dist)
	}
}

// TestNextClosestApproachStableUnderLiveRefresh — the HUD recomputes
// this every frame from freshly-integrated state. Advancing both
// craft by a small Δt (one "frame") must move the predicted time-to-
// closest-approach by ≈ that Δt (the event is the same, just nearer)
// — NOT snap by a whole ~period/50 grid step. Regression guard for
// the "readout varies widely" report: pre-refinement the answer was
// grid-snapped (~111 s for LEO) so a 45 s advance left tCA unchanged
// or jumped a full cell; with parabolic refinement it tracks
// continuously.
func TestNextClosestApproachStableUnderLiveRefresh(t *testing.T) {
	r := 6.771e6
	a := circularState(r, 0, muEarth)
	b := circularState(r, 0.05, muEarth)
	pu := orbital.Vec3{X: a.V.X, Y: a.V.Y}
	pu = pu.Scale(1 / pu.Norm())
	a.V = a.V.Add(pu.Scale(5))

	const horizon = 4 * 3600.0
	t0, d0, _, err := NextClosestApproach(a, b, bodies.CelestialBody{}, muEarth, horizon)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if t0 < 120 || t0 > horizon-120 {
		t.Fatalf("setup: closest approach not interior (t0=%.1f)", t0)
	}

	const adv = 45.0
	sA := physics.StateVector{R: a.R, V: a.V}
	sB := physics.StateVector{R: b.R, V: b.V}
	for k := 0; k < int(adv); k++ {
		sA = physics.StepVerlet(sA, muEarth, 1)
		sB = physics.StepVerlet(sB, muEarth, 1)
	}
	aAdv := orbital.Vec3State{R: sA.R, V: sA.V}
	bAdv := orbital.Vec3State{R: sB.R, V: sB.V}

	t1, d1, _, err := NextClosestApproach(aAdv, bAdv, bodies.CelestialBody{}, muEarth, horizon)
	if err != nil {
		t.Fatalf("err (advanced): %v", err)
	}

	if drift := math.Abs((t0 - adv) - t1); drift > 8 {
		t.Errorf("tCA not continuous under refresh: t0=%.2f t1=%.2f want≈%.2f (drift %.2f s, old grid step ≈111 s)",
			t0, t1, t0-adv, drift)
	}
	if rel := math.Abs(d1-d0) / d0; rel > 0.05 {
		t.Errorf("closest-approach distance unstable under refresh: d0=%.0f d1=%.0f (%.1f%%)",
			d0, d1, rel*100)
	}
}

// TestNextClosestApproachInvalidHorizon — non-positive horizon → err.
func TestNextClosestApproachInvalidHorizon(t *testing.T) {
	r := 6.771e6
	s := circularState(r, 0, muEarth)
	if _, _, _, err := NextClosestApproach(s, s, bodies.CelestialBody{}, muEarth, 0); err == nil {
		t.Error("zero horizon: expected err")
	}
	if _, _, _, err := NextClosestApproach(s, s, bodies.CelestialBody{}, muEarth, -100); err == nil {
		t.Error("negative horizon: expected err")
	}
}

// TestNextClosestApproachInvalidMu — non-positive mu → err.
func TestNextClosestApproachInvalidMu(t *testing.T) {
	r := 6.771e6
	s := circularState(r, 0, muEarth)
	if _, _, _, err := NextClosestApproach(s, s, bodies.CelestialBody{}, 0, 3600); err == nil {
		t.Error("zero mu: expected err")
	}
}
