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
// the constant separation. The recommendation should reduce it. One fixture
// and one advisory, checked three ways.
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

	t.Run("improves CA", func(t *testing.T) {
		if adv.DV <= 0 || adv.DV > 200 {
			t.Errorf("DV %.2f m/s outside (0, 200] m/s", adv.DV)
		}
		if adv.AchievableCA >= currentCA {
			t.Errorf("AchievableCA=%.0f did not improve on currentCA=%.0f", adv.AchievableCA, currentCA)
		}
		if adv.TArrival <= 0 || adv.TArrival > 6000 {
			t.Errorf("TArrival %.0f outside (0, horizon]", adv.TArrival)
		}
	})

	// Guards against future axis-selection refactors silently degrading. The
	// single-axis projection is intentionally lossy vs the full Lambert ΔV
	// (the slice's loop is "iterate until DOCK READY"), so the floor is low
	// enough to catch silent regressions without being aspirational: 20 % on
	// a 0.5° co-orbital phase offset is healthy headroom.
	t.Run("projection quality", func(t *testing.T) {
		improvement := (currentCA - adv.AchievableCA) / currentCA
		if improvement < 0.20 {
			t.Errorf("co-orbital improvement %.1f%% < 20%% (currentCA=%.0f, achCA=%.0f, DV=%.1f, axis=%s, LambertDV=%.1f)",
				improvement*100, currentCA, adv.AchievableCA, adv.DV, adv.Axis, adv.LambertIdealDV)
		}
		// LambertIdealDV is the unprojected magnitude; the projected DV
		// must be ≤ that (projecting onto a unit axis can only shorten).
		if adv.DV > adv.LambertIdealDV+1e-6 {
			t.Errorf("projected DV %.3f exceeds Lambert ideal %.3f", adv.DV, adv.LambertIdealDV)
		}
	})

	// ADR 0039 S1: every successful plant quotes predicted arrival speed as
	// a plain info row. #290 traced the earlier blind spot: a K-plant that
	// read as a clean "success" in fact arrived at 95.4 m/s, 4.6 under the
	// lock gate, invisible at plan time. ArrivalSpeed must be the |v_rel| AT
	// the achieved closest approach (not the pre-burn v_rel), so recompute it
	// independently and compare.
	t.Run("arrival speed", func(t *testing.T) {
		if adv.ArrivalSpeed <= 0 {
			t.Fatalf("ArrivalSpeed = %.3f, want > 0", adv.ArrivalSpeed)
		}
		perturbed := orbital.Vec3State{R: chaser.R, V: chaser.V.Add(adv.AxisUnit.Scale(adv.DV))}
		_, _, wantVRel, err := NextClosestApproach(perturbed, target, bodies.CelestialBody{}, muEarth, 6000)
		if err != nil {
			t.Fatalf("verification predictor err: %v", err)
		}
		if diff := math.Abs(adv.ArrivalSpeed - wantVRel.Norm()); diff > 1e-6 {
			t.Errorf("ArrivalSpeed = %.6f, want %.6f (|v_rel| at achieved CA)", adv.ArrivalSpeed, wantVRel.Norm())
		}
	})
}

// TestRecommendRendezvousNudge_InclinedTarget — small plane offset
// dominates a tiny phase offset; the only meaningful component of
// the Lambert ΔV is plane-changing, so the projection should pick
// AxisNormalPlus or AxisNormalMinus. Verifies the normal-axis branch
// of the projection loop is exercised in tests (not just the
// buildVelocityFrameAxes builder). v0.10.3+: dropped from (30°, 10°)
// to (2°, 2°) so the recommendation stays under the nudge-scale
// ceiling AND the plane component still dominates the phasing
// component in the projection — a 10° plane change at LEO is
// ~1.3 km/s, which belongs in the manual planner, not a K-plant.
func TestRecommendRendezvousNudge_InclinedTarget(t *testing.T) {
	r := 6.771e6
	chaser := circularStateAtRadius(r, 0, muEarth)
	target := inclinedCircularState(r, 2*math.Pi/180, 2*math.Pi/180, muEarth)

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

// TestRecommendRendezvousNudge_Refusals — every way the advisory declines to
// plant. Each row builds its own pair; wantReasons lists the accepted Reason
// tags (empty: any non-empty Reason is enough).
func TestRecommendRendezvousNudge_Refusals(t *testing.T) {
	const r = 6.771e6 // ~400 km circular LEO
	none := bodies.CelestialBody{}
	cases := []struct {
		name        string
		chaser      orbital.Vec3State
		target      orbital.Vec3State
		primary     bodies.CelestialBody
		horizon     float64
		currentCA   float64 // < 0: measure it with the no-burn predictor
		wantReasons []string
		// wantDVAbove, when set, requires the refused burn to exceed it so the
		// row really exercises the ceiling.
		wantDVAbove float64
		zeroMu      bool
	}{
		// Identical states: currentCA == 0, no possible improvement (the
		// two-prong floor cannot pass: relative improvement is undefined and
		// the absolute is <= 0).
		{name: "no improvement", chaser: circularStateAtRadius(r, 0, muEarth), target: circularStateAtRadius(r, 0, muEarth),
			primary: none, horizon: 6000, currentCA: 0},
		// Horizon below the shortest lookahead-fraction of target period: no
		// Lambert solve completes ("no lambert convergence").
		{name: "horizon too short", chaser: circularStateAtRadius(r, 0, muEarth), target: circularStateAtRadius(r, -0.5*math.Pi/180, muEarth),
			primary: none, horizon: 10, currentCA: 1000},
		// Zero horizon / zero mu: nothing to look ahead over. Same tag as a
		// too-short horizon, not "no lambert convergence" (which means the
		// lookahead ran and failed).
		{name: "zero horizon", chaser: circularStateAtRadius(r, 0, muEarth), target: circularStateAtRadius(r, -0.5*math.Pi/180, muEarth),
			primary: none, horizon: 0, currentCA: 1000, wantReasons: []string{"horizon too short"}},
		{name: "zero mu", chaser: circularStateAtRadius(r, 0, muEarth), target: circularStateAtRadius(r, -0.5*math.Pi/180, muEarth),
			primary: none, horizon: 6000, currentCA: 1000, wantReasons: []string{"horizon too short"}, zeroMu: true},
		// Chaser in LEO, target ~3x the radius: a major orbit change worth
		// thousands of m/s, not a nudge. The ceiling (v0.10.3+) rejects it so
		// the player is pointed at the manual planner instead of a 1.7 km/s
		// K-burn.
		{name: "burn too large", chaser: circularStateAtRadius(r, 0, muEarth), target: circularStateAtRadius(20.0e6, math.Pi/4, muEarth),
			primary: none, horizon: 50000, currentCA: -1,
			wantReasons: []string{"burn too large — use H/I/m"}, wantDVAbove: maxNudgeDV},
		// Chaser far ahead of the target in the same circular LEO: Lambert
		// finds a fast "drop and swoop" whose single-axis projection drops
		// periapsis dramatically (observed live as a 25 km periapsis arc).
		// The orbit-safety gate rejects burns that leave periapsis under
		// primary+50 km or drop it by more than 100 km; the ceiling reason is
		// accepted when the DV exceeds it first.
		{name: "periapsis gate", chaser: circularStateAtRadius(r, 30*math.Pi/180, muEarth), target: circularStateAtRadius(r, 0, muEarth),
			primary: bodies.CelestialBody{MeanRadius: 6378}, horizon: 10000, currentCA: -1,
			wantReasons: []string{"burn drops periapsis unsafely", "burn too large — use H/I/m"}},
		// ADR 0039 S1 / #290: a sharply eccentric chaser (e≈0.69) against a
		// circular target at the same periapsis radius is the shape iterated
		// K-nudges drifted into live: CA *worsened* after the burn because a
		// single-axis nudge cannot correct a shape mismatch. The gate refuses
		// before the Lambert lookahead, with a reason distinct from every
		// other tag so the HUD can point at C/H instead of retrying K.
		{name: "shape mismatch", chaser: eccentricStateAtRadius(r, -0.5*math.Pi/180, 1.3, muEarth), target: circularStateAtRadius(r, 0, muEarth),
			primary: none, horizon: 6000, currentCA: 1_000_000, wantReasons: []string{"orbit shape mismatch"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			currentCA := c.currentCA
			if currentCA < 0 {
				var err error
				if _, currentCA, _, err = NextClosestApproach(c.chaser, c.target, c.primary, muEarth, c.horizon); err != nil {
					t.Fatalf("predictor err: %v", err)
				}
			}
			mu := muEarth
			if c.zeroMu {
				mu = 0
			}
			adv := RecommendRendezvousNudge(c.chaser, c.target, c.primary, mu, c.horizon, currentCA)
			if adv.Ok {
				t.Fatalf("expected Ok=false, got DV=%.2f axis=%s achCA=%.0f", adv.DV, adv.Axis, adv.AchievableCA)
			}
			if adv.Reason == "" {
				t.Errorf("expected non-empty Reason")
			}
			if len(c.wantReasons) > 0 {
				found := false
				for _, w := range c.wantReasons {
					found = found || adv.Reason == w
				}
				if !found {
					t.Errorf("Reason = %q, want one of %q (DV=%.0f, axis=%s, achCA=%.0f, currentCA=%.0f)",
						adv.Reason, c.wantReasons, adv.DV, adv.Axis, adv.AchievableCA, currentCA)
				}
			}
			if c.wantDVAbove > 0 && adv.DV <= c.wantDVAbove {
				t.Errorf("setup: expected DV > %.0f m/s to exercise the ceiling; got %.1f", c.wantDVAbove, adv.DV)
			}
		})
	}
}

// eccentricStateAtRadius builds a state at radius r, phase phaseRad, whose
// velocity is purely tangential at magnitude k times the local circular
// speed. k > 1 puts r at PERIAPSIS of the resulting ellipse, with
// eccentricity e = k² − 1 (derived from p = r·k² and r = p/(1+e) at
// periapsis). k == 1 reproduces circularStateAtRadius exactly.
func eccentricStateAtRadius(r, phaseRad, k, mu float64) orbital.Vec3State {
	s := circularStateAtRadius(r, phaseRad, mu)
	return orbital.Vec3State{R: s.R, V: s.V.Scale(k)}
}

// TestRecommendRendezvousNudge_NearMatchedShapePasses — a small
// eccentricity delta (e≈0.01, well under the mismatch threshold) must
// NOT trip the new gate: this is K's actual working domain — a small
// phase/altitude offset between near-identical orbits (#278's repro:
// 551×499.8 km vs 500×500 km, e-delta of a few thousandths at LEO
// scale) — and the existing burn-too-large / periapsis-safety gates
// downstream already cover it appropriately depending on geometry. This
// guards the threshold isn't so wide it swallows K's real domain.
func TestRecommendRendezvousNudge_NearMatchedShapePasses(t *testing.T) {
	r := 6.771e6
	target := circularStateAtRadius(r, 0, muEarth)
	chaser := eccentricStateAtRadius(r, -0.5*math.Pi/180, 1.005, muEarth) // e≈0.01

	_, currentCA, _, err := NextClosestApproach(chaser, target, bodies.CelestialBody{}, muEarth, 6000)
	if err != nil {
		t.Fatalf("predictor err: %v", err)
	}

	adv := RecommendRendezvousNudge(chaser, target, bodies.CelestialBody{}, muEarth, 6000, currentCA)
	if adv.Reason == "orbit shape mismatch" {
		t.Errorf("near-matched shapes (e-delta≈0.01) tripped the mismatch gate; got DV=%.1f", adv.DV)
	}
}

// TestRecommendRendezvousNudge_HorizonEqualToShortestLookaheadIsUsable: the
// shortest Lambert lookahead is 0.15 of the target's period. A horizon of
// exactly that is not "too short": the lookahead at the horizon itself must
// still run (LambertIdealDV is only set when a solve completed).
func TestRecommendRendezvousNudge_HorizonEqualToShortestLookaheadIsUsable(t *testing.T) {
	r := 6.771e6
	target := circularStateAtRadius(r, 0, muEarth)
	chaser := circularStateAtRadius(r, -0.5*math.Pi/180, muEarth)
	pB := 2 * math.Pi * math.Sqrt(r*r*r/muEarth)
	horizon := 0.15 * pB // same expression the advisory uses for T
	adv := RecommendRendezvousNudge(chaser, target, bodies.CelestialBody{}, muEarth, horizon, 1000)
	if adv.Reason == "no lambert convergence" || adv.LambertIdealDV <= 0 {
		t.Errorf("horizon == 0.15 P skipped the lookahead: Reason=%q LambertIdealDV=%.3f", adv.Reason, adv.LambertIdealDV)
	}
}
