package sim

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// planeAngleDeg is the angle between two orbit-normal (or any) vectors,
// in degrees. Shared shape with TestPlanPlaneMatchCoplanarWithMoon's
// local helper of the same name.
func planeAngleDeg(a, b orbital.Vec3) float64 {
	c := a.Dot(b) / (a.Norm() * b.Norm())
	if c > 1 {
		c = 1
	} else if c < -1 {
		c = -1
	}
	return math.Acos(c) * 180 / math.Pi
}

// circularStateAtIncRAAN builds a circular-orbit (R, V) pair at radius
// r, inclination incDeg and RAAN raanDeg (both in degrees, measured in
// the given primary's reference frame), expressed in the world-aligned
// Cartesian frame maneuver.go's craft states use. Mirrors the inline
// builder in TestPlanPlaneMatchCoplanarWithMoon.
func circularStateAtIncRAAN(frame orbital.BodyFrame, mu, r, incDeg, raanDeg float64) (orbital.Vec3, orbital.Vec3) {
	inc := incDeg * math.Pi / 180
	raan := raanDeg * math.Pi / 180
	v := math.Sqrt(mu / r)
	rB := orbital.Vec3{X: r * math.Cos(raan), Y: r * math.Sin(raan)}
	vB := orbital.Vec3{
		X: -v * math.Cos(inc) * math.Sin(raan),
		Y: v * math.Cos(inc) * math.Cos(raan),
		Z: v * math.Sin(inc),
	}
	return frame.ToWorld(rB), frame.ToWorld(vB)
}

// TestPlanVesselPlaneMatchLocalCraftSameInclinationDifferentRAAN is the
// ADR 0045 S4 (#397) headline case: two vessels reading the SAME
// inclination magnitude (51.6°) but different RAAN (0° vs 90°) sit in
// genuinely different planes — the old scalar PlanInclinationChange(51.6°)
// could never fix this for a vessel target, since it only ever drops the
// craft to a fixed magnitude in the primary's frame, ignoring RAAN
// entirely. PlanVesselPlaneMatch must actually bring the two orbit
// normals into agreement.
func TestPlanVesselPlaneMatchLocalCraftSameInclinationDifferentRAAN(t *testing.T) {
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 350e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("expected 2 crafts after spawn, got %d", len(w.Crafts))
	}
	w.ActiveCraftIdx = 0
	active := w.Crafts[0]
	target := w.Crafts[1]
	if active.Primary.ID != target.Primary.ID {
		t.Fatalf("test setup: crafts don't share a primary")
	}

	mu := active.Primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(active.Primary)
	rActive := active.Primary.RadiusMeters() + 300e3
	rTarget := active.Primary.RadiusMeters() + 350e3

	const incDeg = 51.6
	active.State.R, active.State.V = circularStateAtIncRAAN(frame, mu, rActive, incDeg, 0)
	target.State.R, target.State.V = circularStateAtIncRAAN(frame, mu, rTarget, incDeg, 90)
	active.Nodes = nil
	target.Nodes = nil

	// Sanity: same inclination magnitude, but the planes are genuinely
	// different — exactly the case the old scalar match couldn't touch.
	nActive := active.State.R.Cross(active.State.V)
	nTarget := target.State.R.Cross(target.State.V)
	if ang := planeAngleDeg(nActive, nTarget); ang < 20 {
		t.Fatalf("test setup: planes only %.1f° apart, want a real RAAN split", ang)
	}

	w.SetTargetCraft(1)

	plan, err := w.PlanVesselPlaneMatch()
	if err != nil {
		t.Fatalf("PlanVesselPlaneMatch: %v", err)
	}
	if plan.DV <= 0 {
		t.Fatalf("plan.DV = %.2f, want > 0", plan.DV)
	}
	if len(active.Nodes) != 1 {
		t.Fatalf("expected 1 planted node, got %d", len(active.Nodes))
	}

	post, _ := w.PostBurnState(active.Nodes[0])
	// The target's own orbital plane is time-invariant (Keplerian, no
	// perturbations), so target.State's normal is still valid at the
	// burn's later trigger time.
	nTargetHat := target.State.R.Cross(target.State.V)
	if ang := planeAngleDeg(post.R.Cross(post.V), nTargetHat); ang > 0.5 {
		t.Errorf("post-burn plane %.2f° off the target vessel's, want ~0° (dv=%.1f m/s)", ang, plan.DV)
	}
	// Plane-change burn preserves |v| → orbit stays ~circular.
	if el := orbital.ElementsFromState(post.R, post.V, mu); el.E > 5e-3 {
		t.Errorf("post-burn e=%.4f, want ~0", el.E)
	}
}

// TestPlanVesselPlaneMatchGhostTarget mirrors the local-craft case
// against a remote player's ghost (TargetGhost, v0.27/ADR 0034) — the
// acceptance criterion that a vessel plane match works against a ghost,
// not just a local craft.
func TestPlanVesselPlaneMatchGhostTarget(t *testing.T) {
	w := mustWorld(t)
	active := w.ActiveCraft()
	mu := active.Primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(active.Primary)
	r := active.Primary.RadiusMeters() + 300e3

	const incDeg = 28.5
	active.State.R, active.State.V = circularStateAtIncRAAN(frame, mu, r, incDeg, 0)
	active.Nodes = nil

	ghostR, ghostV := circularStateAtIncRAAN(frame, mu, r, incDeg, 120)
	primaryPos := w.BodyPosition(active.Primary)
	w.Ghosts = []Ghost{{
		Owner: "SHA256:peer", CraftID: 42, PrimaryID: active.Primary.ID,
		Pos: primaryPos.Add(ghostR),
		Vel: ghostV,
	}}
	w.SetTargetGhost("SHA256:peer", 42)

	nActive := active.State.R.Cross(active.State.V)
	nGhost := ghostR.Cross(ghostV)
	if ang := planeAngleDeg(nActive, nGhost); ang < 20 {
		t.Fatalf("test setup: planes only %.1f° apart, want a real RAAN split", ang)
	}

	plan, err := w.PlanVesselPlaneMatch()
	if err != nil {
		t.Fatalf("PlanVesselPlaneMatch (ghost): %v", err)
	}
	if len(active.Nodes) != 1 {
		t.Fatalf("expected 1 planted node, got %d", len(active.Nodes))
	}

	post, _ := w.PostBurnState(active.Nodes[0])
	if ang := planeAngleDeg(post.R.Cross(post.V), nGhost); ang > 0.5 {
		t.Errorf("post-burn plane %.2f° off the ghost's, want ~0° (dv=%.1f m/s)", ang, plan.DV)
	}
}

// TestPlanVesselPlaneMatchRefusals covers the issue's explicit refusal
// list: no vessel target bound, orbits already coplanar (nothing to
// do), and a degenerate target relative state (no defined plane).
func TestPlanVesselPlaneMatchRefusals(t *testing.T) {
	t.Run("no target", func(t *testing.T) {
		w := mustWorld(t)
		w.ClearTarget()
		if _, err := w.PlanVesselPlaneMatch(); !errors.Is(err, ErrRendezvousNoTarget) {
			t.Errorf("err = %v, want ErrRendezvousNoTarget", err)
		}
	})

	t.Run("already coplanar", func(t *testing.T) {
		w := mustWorld(t)
		if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
			t.Fatalf("SpawnCraft: %v", err)
		}
		w.ActiveCraftIdx = 0
		// Both crafts spawn equatorial by default — same plane already.
		w.SetTargetCraft(1)
		if _, err := w.PlanVesselPlaneMatch(); !errors.Is(err, planner.ErrInclinationNoOp) {
			t.Errorf("err = %v, want planner.ErrInclinationNoOp", err)
		}
	})

	t.Run("target landed at the pole (ADR 0050 decision 8)", func(t *testing.T) {
		// The pre-decision-8 guard was an exact `nTarget.Norm() == 0`
		// test, which a real pole never trips: floating-point residue in
		// the lat/lon-to-Cartesian conversion leaves |rT x vT| ~= 3e-7,
		// not exactly zero, at the shipped North Pole preset. Unguarded,
		// PlanVesselPlaneMatch plants a burn out of that noise (the ADR
		// measured 8127..13099 m/s across a day with no period) instead
		// of refusing the way it already does for the exact-zero case
		// above.
		w := mustWorld(t)
		if _, err := w.SpawnCraft(SpawnSpec{
			Launchpad: true,
			Latitude:  90,
		}); err != nil {
			t.Fatalf("SpawnCraft: %v", err)
		}
		w.ActiveCraftIdx = 0
		w.SetTargetCraft(1)
		if _, err := w.PlanVesselPlaneMatch(); !errors.Is(err, errPlaneMatchDegenerateTarget) {
			t.Errorf("err = %v, want errPlaneMatchDegenerateTarget (a landed-at-the-pole target has no trustworthy plane)", err)
		}
	})

	t.Run("degenerate target relative state", func(t *testing.T) {
		w := mustWorld(t)
		active := w.ActiveCraft()
		primaryPos := w.BodyPosition(active.Primary)
		// Ghost sits exactly on top of the active craft's own position
		// with zero relative velocity — rT × vT is the zero vector, no
		// defined target plane.
		w.Ghosts = []Ghost{{
			Owner: "SHA256:peer", CraftID: 7, PrimaryID: active.Primary.ID,
			Pos: primaryPos.Add(active.State.R),
			Vel: orbital.Vec3{},
		}}
		w.SetTargetGhost("SHA256:peer", 7)
		if _, err := w.PlanVesselPlaneMatch(); !errors.Is(err, errPlaneMatchDegenerateTarget) {
			t.Errorf("err = %v, want errPlaneMatchDegenerateTarget", err)
		}
	})
}

// TestActiveLandedVesselAtPoleRefusesPlaneMatchAndDrawsNoMarkers is
// review r1 F3's follow-up check: does PlanVesselPlaneMatch, or the
// map's ◇/◆ node markers (TargetPlaneNodePositions), consume the
// ACTIVE vessel's own landed normal the same fragile way
// spacecraft.HeadingOrbitNormal did (the bug F3 fixed on the pad's own
// depart:/Δincl: rows)? Measured (round 2 review, R2-F2, correcting an
// earlier version of this comment that claimed a pole-independent
// mechanism): no, but only at the pole, and not for the reason
// previously stated here. PlanVesselPlaneMatch returns
// ErrInclinationNoOp for a Landed active craft at all three latitudes
// checked (pole, one metre off it, an ordinary 28.6° pad), that part
// holds everywhere, but the ◇/◆ markers do NOT stay withheld off the
// pole: TargetPlaneNodePositions draws them (AN/DN both true) one metre
// off the pole and at 28.6°, so orbital.TimeToNodeCrossing does find a
// crossing for the Landed pseudo-orbit away from the pole, contradicting
// the "no node crossing" claim this comment used to make. The refusal
// this test actually pins is the pole case only: at lat 90 the near-zero
// angular momentum of the landed co-rotation pseudo-orbit degenerates
// the crossing search itself, and separately (unmeasured here) whatever
// PlanVesselPlaneMatch checks after the crossing search still refuses
// at every latitude. Pinned so a future change that starts drawing
// markers or planning a burn for a Landed active craft AT THE POLE
// specifically doesn't silently reopen this class of bug; it says
// nothing about the mechanism off the pole.
func TestActiveLandedVesselAtPoleRefusesPlaneMatchAndDrawsNoMarkers(t *testing.T) {
	poleWorld := func(t *testing.T) *World {
		t.Helper()
		w := mustWorld(t)
		if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3, Inclination: 45}); err != nil {
			t.Fatalf("SpawnCraft (orbiter): %v", err)
		}
		if _, err := w.SpawnCraft(SpawnSpec{Launchpad: true, Latitude: 90}); err != nil {
			t.Fatalf("SpawnCraft (pole): %v", err)
		}
		w.ActiveCraftIdx = 2 // the pole-landed craft.
		w.SetTargetCraft(1) // the orbiting craft.
		if w.Target.Kind != TargetCraft {
			t.Fatalf("setup: expected TargetCraft, got %v", w.Target.Kind)
		}
		return w
	}

	t.Run("PlanVesselPlaneMatch refuses", func(t *testing.T) {
		w := poleWorld(t)
		if plan, err := w.PlanVesselPlaneMatch(); err == nil {
			t.Errorf("PlanVesselPlaneMatch() = (%+v, nil), want a refusal for an active vessel landed at the pole", plan)
		}
	})

	t.Run("TargetPlaneNodePositions draws no markers", func(t *testing.T) {
		w := poleWorld(t)
		if _, _, hasAN, hasDN := w.TargetPlaneNodePositions(); hasAN || hasDN {
			t.Error("expected no node markers for an active vessel landed at the pole")
		}
	})
}
