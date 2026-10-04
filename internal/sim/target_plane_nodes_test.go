package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestTargetPlaneNodePositions_TiltedAboutXAxis sets up a hand-computable
// geometry: the target orbits a circle of radius r in the primary's
// XY-plane (orbit normal = +Z), and the active craft orbits the SAME
// circle tilted 30 degrees about the +X axis. Rotating a circle about an
// axis leaves the two points that already sit ON that axis fixed, so the
// active orbit's crossings of the target's XY-plane (its Ascending /
// Descending Node pair against the TARGET's plane) are exactly (+r, 0, 0)
// and (-r, 0, 0) in the primary's frame — independent of TimeToNodeCrossing's
// own internals, so this checks the map-facing wiring (frame-from-normal +
// propagate-to-crossing) rather than re-asserting the node-crossing math
// TimeToNodeCrossing already owns.
func TestTargetPlaneNodePositions_TiltedAboutXAxis(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]
	mu := active.Primary.GravitationalParameter()
	const r = 6.771e6 // 400 km LEO
	baseV := math.Sqrt(mu / r)

	// Target: circular, in the primary's XY-plane (h ∥ +Z).
	target.State.R = orbital.Vec3{X: r}
	target.State.V = orbital.Vec3{Y: baseV}
	target.Primary = active.Primary

	// Active: the same circle, started 90° around it, then the whole
	// state tilted 30° about +X — the line of nodes with the target's
	// plane is exactly the X-axis.
	preR := orbital.Vec3{Y: r}
	preV := orbital.Vec3{X: -baseV}
	theta := 30 * math.Pi / 180
	axis := orbital.Vec3{X: 1}
	active.State.R = rotateAboutAxis(preR, axis, theta)
	active.State.V = rotateAboutAxis(preV, axis, theta)

	anPos, dnPos, hasAN, hasDN := w.TargetPlaneNodePositions()
	if !hasAN || !hasDN {
		t.Fatalf("expected both nodes resolvable, hasAN=%v hasDN=%v", hasAN, hasDN)
	}

	primaryPos := w.BodyPosition(active.Primary)
	wantA := primaryPos.Add(orbital.Vec3{X: r})
	wantB := primaryPos.Add(orbital.Vec3{X: -r})

	const tol = 50.0 // metres — analytic Kepler propagation, not sample-grid
	matches := func(got, want orbital.Vec3) bool { return got.Sub(want).Norm() < tol }
	okSet := (matches(anPos, wantA) && matches(dnPos, wantB)) ||
		(matches(anPos, wantB) && matches(dnPos, wantA))
	if !okSet {
		t.Errorf("node positions = {%v, %v}, want the pair {%v, %v} (line of nodes = ±X at radius %.0f)",
			anPos, dnPos, wantA, wantB, r)
	}
}

// TestTargetPlaneNodePositions_Coplanar verifies the degenerate case: an
// active craft coplanar with its target has no defined line of nodes, so
// TimeToNodeCrossing's own equatorial-tolerance gate returns -1 for both
// crossings and TargetPlaneNodePositions must report hasAN=hasDN=false
// rather than fabricating a point.
func TestTargetPlaneNodePositions_Coplanar(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]
	// rendezvousTwoCraftWorld's default spawn already puts both craft in
	// the same equatorial plane; make it explicit / robust to spawn
	// defaults changing by forcing both onto the primary's XY-plane.
	mu := active.Primary.GravitationalParameter()
	const r1, r2 = 6.771e6, 6.971e6
	target.State.R = orbital.Vec3{X: r1}
	target.State.V = orbital.Vec3{Y: math.Sqrt(mu / r1)}
	target.Primary = active.Primary
	active.State.R = orbital.Vec3{Y: r2}
	active.State.V = orbital.Vec3{X: -math.Sqrt(mu / r2)}

	_, _, hasAN, hasDN := w.TargetPlaneNodePositions()
	if hasAN || hasDN {
		t.Errorf("coplanar craft: expected no defined nodes, got hasAN=%v hasDN=%v", hasAN, hasDN)
	}
}

// TestTargetPlaneNodePositions_LandedAtPole_NotMeaningful pins ADR 0050
// decision 8: a target landed at a pole (the shipped North Pole preset)
// has |rT x vT| ~= 3e-7, not exactly zero, so the pre-decision-8 exact
// `Norm() == 0` guard let it through and drew a node-marker pair off
// pure floating-point noise. Reuse the same shared guard
// PlanVesselPlaneMatch refuses on (vessel_plane_match_test.go's "target
// landed at the pole" case).
func TestTargetPlaneNodePositions_LandedAtPole_NotMeaningful(t *testing.T) {
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{Launchpad: true, Latitude: 90}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	if _, _, hasAN, hasDN := w.TargetPlaneNodePositions(); hasAN || hasDN {
		t.Error("expected no node markers against a target landed at the pole")
	}
}

// TestTargetPlaneNodePositions_DifferentPrimaries_NotMeaningful mirrors
// TargetLeadAngleDeg's cross-SOI refusal: a target orbiting a different
// primary has no shared plane to measure a line of nodes against.
func TestTargetPlaneNodePositions_DifferentPrimaries_NotMeaningful(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	sister := w.Crafts[1]
	other := otherBody(t, w)
	sister.Primary = other

	if _, _, hasAN, hasDN := w.TargetPlaneNodePositions(); hasAN || hasDN {
		t.Error("expected no nodes when the target craft orbits a different primary")
	}
}

// TestTargetPlaneNodePositions_BodyTarget (#460, G6 Q5 option 1): a body
// target gets the same ◇/◆ crossing points as a vessel target, against
// the body's catalog plane. Both points lie on the craft's own orbit
// radius and in the body's plane (n . r ~ 0), and are 180 degrees apart.
func TestTargetPlaneNodePositions_BodyTarget(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	moonIdx := -1
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Fatal("no moon")
	}
	w.SetTargetBody(moonIdx)
	an, dn, hasAN, hasDN := w.TargetPlaneNodePositions()
	if !hasAN || !hasDN {
		t.Fatalf("body target: want both nodes, got AN=%v DN=%v (craft landed=%v)", hasAN, hasDN, c.Landed)
	}
	n := orbital.OrbitNormalWorld(w.System().Bodies[moonIdx]).Unit()
	prim := w.BodyPosition(c.Primary)
	for name, p := range map[string]orbital.Vec3{"AN": an, "DN": dn} {
		rel := p.Sub(prim)
		if off := math.Abs(n.Dot(rel)) / rel.Norm(); off > 1e-3 {
			t.Errorf("%s is %.4f (sin) off the body's plane", name, off)
		}
	}
	if cosang := an.Sub(prim).Dot(dn.Sub(prim)) / (an.Sub(prim).Norm() * dn.Sub(prim).Norm()); cosang > -0.99 {
		t.Errorf("AN and DN not opposite: cos=%.3f", cosang)
	}
}

// TestTargetPlaneNodePositions_LandedVessel_NoMarkers (#460): the pad's
// co-rotation pseudo-orbit used to find "crossings" and plant both
// markers on the ground; a Landed vessel has no orbit to cross on.
func TestTargetPlaneNodePositions_LandedVessel_NoMarkers(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.Landed = true
	w.SetTargetBody(1)
	if _, _, hasAN, hasDN := w.TargetPlaneNodePositions(); hasAN || hasDN {
		t.Error("Landed vessel must not get plane-node markers")
	}
}

// TestTargetPlaneNodePositions_GhostTarget mirrors the craft-target
// tilted-orbit case through the ghost path (ADR 0034), which the map
// draws through the same Ghosts slate as a local craft target.
func TestTargetPlaneNodePositions_GhostTarget(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	mu := active.Primary.GravitationalParameter()
	const r = 6.771e6
	baseV := math.Sqrt(mu / r)

	ghostR := orbital.Vec3{X: r}
	ghostV := orbital.Vec3{Y: baseV}
	g := ghostShapedLike(w, active.Primary, ghostR, ghostV, "SHA256:gern", "gern", 99)
	w.Ghosts = []Ghost{g}
	w.SetTargetGhost(g.Owner, g.CraftID)

	preR := orbital.Vec3{Y: r}
	preV := orbital.Vec3{X: -baseV}
	theta := 30 * math.Pi / 180
	axis := orbital.Vec3{X: 1}
	active.State.R = rotateAboutAxis(preR, axis, theta)
	active.State.V = rotateAboutAxis(preV, axis, theta)

	anPos, dnPos, hasAN, hasDN := w.TargetPlaneNodePositions()
	if !hasAN || !hasDN {
		t.Fatalf("expected both nodes resolvable for a ghost target, hasAN=%v hasDN=%v", hasAN, hasDN)
	}
	primaryPos := w.BodyPosition(active.Primary)
	wantA := primaryPos.Add(orbital.Vec3{X: r})
	wantB := primaryPos.Add(orbital.Vec3{X: -r})
	const tol = 50.0
	matches := func(got, want orbital.Vec3) bool { return got.Sub(want).Norm() < tol }
	okSet := (matches(anPos, wantA) && matches(dnPos, wantB)) ||
		(matches(anPos, wantB) && matches(dnPos, wantA))
	if !okSet {
		t.Errorf("ghost-target node positions = {%v, %v}, want the pair {%v, %v}", anPos, dnPos, wantA, wantB)
	}
}

// TestTargetPlaneNodesSolveOncePerOrbit (#548 review line 84): the ◇/◆
// markers are asked for every frame, but the crossings are properties of
// the craft's orbit and the target plane, not of the clock. 100 frames of
// real Ticks (Verlet free flight, craft coasting in LEO, Moon targeted)
// must run the solver once, and the cached positions must match a fresh
// solve.
func TestTargetPlaneNodesSolveOncePerOrbit(t *testing.T) {
	w := mustWorld(t)
	w.SetTargetBody(moonIndex(w))
	w.TargetPlaneNodePositions()
	for i := 0; i < 100; i++ {
		w.Tick()
		w.TargetPlaneNodePositions()
	}
	if got := w.TargetPlaneSolves(); got != 1 {
		t.Errorf("%d solves over 101 frames of an unchanged orbit, want 1", got)
	}
}

// freshNodes is the uncached truth: the same call with the cache emptied.
func freshNodes(w *World) (an, dn orbital.Vec3, hasAN, hasDN bool) {
	w.tpnCache = planeCrossingCache{}
	return w.TargetPlaneNodePositions()
}

func sameNodes(t *testing.T, label string, w *World) {
	t.Helper()
	an, dn, ha, hd := w.TargetPlaneNodePositions()
	fan, fdn, fha, fhd := freshNodes(w)
	if ha != fha || hd != fhd {
		t.Fatalf("%s: has AN/DN %v/%v, fresh %v/%v", label, ha, hd, fha, fhd)
	}
	r := w.ActiveCraft().State.R.Norm()
	if d := an.Sub(fan).Norm() / r; d > 1e-3 {
		t.Errorf("%s: cached AN is %.2e of the orbit radius from a fresh solve", label, d)
	}
	if d := dn.Sub(fdn).Norm() / r; d > 1e-3 {
		t.Errorf("%s: cached DN is %.2e of the orbit radius from a fresh solve", label, d)
	}
}

// TestTargetPlaneNodesCacheInvalidates: a retarget, a burn and a vessel
// target each change the answer, and the cache must not serve the old one.
// Every step compares the cached call with a fresh solve.
func TestTargetPlaneNodesCacheInvalidates(t *testing.T) {
	w := mustWorld(t)
	moon := moonIndex(w)
	w.SetTargetBody(moon)
	sameNodes(t, "moon, first", w)
	for i := 0; i < 50; i++ {
		w.Tick()
	}
	sameNodes(t, "moon, after 50 ticks", w)

	// Retarget to another body: a different plane normal.
	other := -1
	for i, b := range w.System().Bodies {
		if i > 0 && i != moon && b.ID != w.ActiveCraft().Primary.ID {
			other = i
			break
		}
	}
	an0, _, _, _ := w.TargetPlaneNodePositions()
	w.SetTargetBody(other)
	an1, _, _, _ := w.TargetPlaneNodePositions()
	if an0 == an1 {
		t.Error("retargeting served the old target's node")
	}
	sameNodes(t, "retargeted", w)

	// A burn: rotate the velocity plane by adding out-of-plane delta-v.
	w.SetTargetBody(moon)
	sameNodes(t, "back to moon", w)
	c := w.ActiveCraft()
	s0 := w.TargetPlaneSolves()
	c.State.V = c.State.V.Add(c.State.R.Cross(c.State.V).Unit().Scale(400))
	sameNodes(t, "after 400 m/s plane change", w)
	if w.TargetPlaneSolves() == s0 {
		t.Error("a plane-change burn did not trigger a new solve")
	}
}

// TestTargetPlaneNodes_BodyTargetOfOwnPrimaryOrAncestorDrawsNothing (#548
// review line 89): a vessel in Moon orbit targeting Earth used to draw its
// Moon-orbit crossings of Earth's heliocentric plane, which nobody can fly
// to. The primary itself and every ancestor of it are not plane targets.
// Positive controls: the same Moon-orbit vessel does get nodes against a
// sibling planet's plane, and an Earth-orbit vessel against the Moon.
func TestTargetPlaneNodes_BodyTargetOfOwnPrimaryOrAncestorDrawsNothing(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	moon := moonIndex(w)
	earth := -1
	other := -1
	for i, b := range w.System().Bodies {
		switch {
		case b.ID == "earth":
			earth = i
		case i > 0 && b.ID != "moon" && b.ID != "earth" && other < 0:
			other = i
		}
	}
	if moon < 0 || earth < 0 || other < 0 {
		t.Fatalf("fixture: moon %d earth %d other %d", moon, earth, other)
	}

	// Control: Earth-orbit vessel, Moon targeted, both nodes.
	w.SetTargetBody(moon)
	if _, _, ha, hd := w.TargetPlaneNodePositions(); !ha || !hd {
		t.Fatalf("control failed: Earth-orbit vs Moon plane AN=%v DN=%v", ha, hd)
	}

	// Park the vessel in an inclined Moon orbit.
	moonBody := w.System().Bodies[moon]
	c.Primary = moonBody
	r := moonBody.RadiusMeters() + 100e3
	v := math.Sqrt(moonBody.GravitationalParameter() / r)
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: v * math.Cos(0.6), Z: v * math.Sin(0.6)}

	w.SetTargetBody(earth)
	if _, _, ha, hd := w.TargetPlaneNodePositions(); ha || hd {
		t.Errorf("Moon-orbit vessel targeting Earth drew nodes AN=%v DN=%v", ha, hd)
	}
	w.SetTargetBody(other)
	if _, _, ha, hd := w.TargetPlaneNodePositions(); !ha || !hd {
		t.Errorf("control failed: Moon-orbit vessel vs a sibling planet's plane AN=%v DN=%v", ha, hd)
	}
}
