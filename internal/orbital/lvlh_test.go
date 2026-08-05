package orbital

import (
	"math"
	"testing"
)

const lvlhEps = 1e-12

func lvlhVecClose(t *testing.T, label string, got, want Vec3, eps float64) {
	t.Helper()
	if math.Abs(got.X-want.X) > eps || math.Abs(got.Y-want.Y) > eps || math.Abs(got.Z-want.Z) > eps {
		t.Errorf("%s = (%g, %g, %g), want (%g, %g, %g)", label, got.X, got.Y, got.Z, want.X, want.Y, want.Z)
	}
}

// TestLVLHBasisCircularOrbit pins the frame contract on the simplest
// seeded case: a circular equatorial orbit with the target on +X moving
// toward +Y. Prograde must come out as the along-track (screen-right)
// axis and the outward radial as the radial-out (screen-up) axis, so
// "toward the primary" is screen-DOWN.
func TestLVLHBasisCircularOrbit(t *testing.T) {
	r := Vec3{X: 7_000_000}
	v := Vec3{Y: 7546}

	f, ok := LVLHBasis(r, v)
	if !ok {
		t.Fatal("LVLHBasis refused a well-formed circular orbit")
	}
	// Screen-right is the target's prograde direction.
	lvlhVecClose(t, "AlongTrack", f.AlongTrack, Vec3{Y: 1}, lvlhEps)
	// Screen-up is radially OUT — so the primary (at the origin, i.e. in
	// the -X direction from the target) lies screen-DOWN.
	lvlhVecClose(t, "RadialOut", f.RadialOut, Vec3{X: 1}, lvlhEps)
	lvlhVecClose(t, "CrossTrack", f.CrossTrack, Vec3{Z: 1}, lvlhEps)

	// The primary direction projects to a NEGATIVE radial-out component,
	// which is what the canvas turns into "further down the screen".
	towardPrimary := r.Scale(-1).Unit()
	if got := f.RadialOut.Dot(towardPrimary); got >= 0 {
		t.Errorf("primary direction · RadialOut = %g, want < 0 (primary is screen-down)", got)
	}
}

// TestLVLHBasisOrthonormal checks the invariant the canvas depends on:
// the three axes are unit-length and mutually perpendicular, and
// right-handed (AlongTrack × RadialOut = CrossTrack is the convention
// that makes ĥ point out of the screen). Uses an inclined, eccentric
// state so the projection step (not just the trivial case) is exercised.
func TestLVLHBasisOrthonormal(t *testing.T) {
	r := Vec3{X: 4_100_000, Y: -2_300_000, Z: 1_900_000}
	v := Vec3{X: 1200, Y: 6100, Z: -800}

	f, ok := LVLHBasis(r, v)
	if !ok {
		t.Fatal("LVLHBasis refused a well-formed inclined orbit")
	}
	const eps = 1e-9
	for _, tc := range []struct {
		name string
		vec  Vec3
	}{
		{"AlongTrack", f.AlongTrack},
		{"RadialOut", f.RadialOut},
		{"CrossTrack", f.CrossTrack},
	} {
		if math.Abs(tc.vec.Norm()-1) > eps {
			t.Errorf("|%s| = %g, want 1", tc.name, tc.vec.Norm())
		}
	}
	if d := f.AlongTrack.Dot(f.RadialOut); math.Abs(d) > eps {
		t.Errorf("AlongTrack · RadialOut = %g, want 0", d)
	}
	if d := f.AlongTrack.Dot(f.CrossTrack); math.Abs(d) > eps {
		t.Errorf("AlongTrack · CrossTrack = %g, want 0", d)
	}
	if d := f.RadialOut.Dot(f.CrossTrack); math.Abs(d) > eps {
		t.Errorf("RadialOut · CrossTrack = %g, want 0", d)
	}
	// Right-handed ordering is (RadialOut, AlongTrack, CrossTrack) — the
	// sign that puts ĥ INTO the screen once the canvas draws along-track
	// right and radial-out up, i.e. the standard V-bar / R-bar viewpoint.
	lvlhVecClose(t, "RadialOut × AlongTrack", f.RadialOut.Cross(f.AlongTrack), f.CrossTrack, 1e-9)

	// Along-track still points the same way as velocity (it is v with the
	// radial part removed), so a prograde push always reads screen-right.
	if d := f.AlongTrack.Dot(v.Unit()); d <= 0 {
		t.Errorf("AlongTrack · v̂ = %g, want > 0 (along-track must not flip prograde)", d)
	}
	// And it carries none of the radial component.
	if d := f.AlongTrack.Dot(r.Unit()); math.Abs(d) > eps {
		t.Errorf("AlongTrack · r̂ = %g, want 0 (radial component must be projected out)", d)
	}
}

// TestLVLHBasisRotatesWithOrbit: the frame is derived from the target's
// CURRENT state, so stepping the target a quarter turn around a circular
// orbit rotates the whole basis a quarter turn with it. This is what
// makes relative drift curve like the physics instead of unrolling into
// an inertial spiral.
func TestLVLHBasisRotatesWithOrbit(t *testing.T) {
	const rMag, vMag = 7_000_000.0, 7546.0
	at := func(theta float64) (LVLHFrame, bool) {
		r := Vec3{X: rMag * math.Cos(theta), Y: rMag * math.Sin(theta)}
		v := Vec3{X: -vMag * math.Sin(theta), Y: vMag * math.Cos(theta)}
		return LVLHBasis(r, v)
	}
	f0, ok := at(0)
	if !ok {
		t.Fatal("LVLHBasis refused at θ=0")
	}
	fQ, ok := at(math.Pi / 2)
	if !ok {
		t.Fatal("LVLHBasis refused at θ=90°")
	}
	// A quarter turn later, radial-out has swung from +X to +Y and
	// along-track from +Y to -X — the basis tracked the target.
	lvlhVecClose(t, "RadialOut @90°", fQ.RadialOut, Vec3{Y: 1}, 1e-9)
	lvlhVecClose(t, "AlongTrack @90°", fQ.AlongTrack, Vec3{X: -1}, 1e-9)
	// The orbit normal is the one axis that does NOT move — the plane
	// hasn't changed, only the position within it.
	lvlhVecClose(t, "CrossTrack @90°", fQ.CrossTrack, f0.CrossTrack, 1e-9)
}

// TestLVLHBasisRefusesDegenerate: the two states where the frame is
// undefined must refuse rather than silently fall back to world axes —
// a picture whose V-bar / R-bar labels lie is worse than no picture.
func TestLVLHBasisRefusesDegenerate(t *testing.T) {
	if _, ok := LVLHBasis(Vec3{}, Vec3{Y: 7546}); ok {
		t.Error("accepted a zero position vector (no local vertical)")
	}
	if _, ok := LVLHBasis(Vec3{X: 7_000_000}, Vec3{}); ok {
		t.Error("accepted a zero velocity (no local horizontal)")
	}
	// Purely radial motion: v ∥ r, so h = 0.
	if _, ok := LVLHBasis(Vec3{X: 7_000_000}, Vec3{X: -1200}); ok {
		t.Error("accepted a purely radial fall (no local horizontal)")
	}
}

// TestLVLHToFrame: a chaser trailing the target along -V and sitting
// below it decomposes into the components a pilot would name — negative
// along-track (behind), negative radial (below), zero cross-track.
func TestLVLHToFrame(t *testing.T) {
	f, ok := LVLHBasis(Vec3{X: 7_000_000}, Vec3{Y: 7546})
	if !ok {
		t.Fatal("LVLHBasis refused a well-formed circular orbit")
	}
	// 500 m behind (-Y in world here) and 20 m below (-X in world).
	rel := Vec3{X: -20, Y: -500}
	lvlhVecClose(t, "ToFrame", f.ToFrame(rel), Vec3{X: -500, Y: -20}, 1e-9)
}
