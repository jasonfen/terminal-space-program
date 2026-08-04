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

// TestTargetPlaneNodePositions_BodyTarget_NotMeaningful: a body target
// (not a craft/ghost) must never produce plane-node markers.
func TestTargetPlaneNodePositions_BodyTarget_NotMeaningful(t *testing.T) {
	w := mustWorld(t)
	w.SetTargetBody(1)
	if _, _, hasAN, hasDN := w.TargetPlaneNodePositions(); hasAN || hasDN {
		t.Error("expected no nodes for a body target")
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
