package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestBurnVectorNodeDirectionIsCaptured: a BurnVector node's thrust
// direction is the captured unit vector, independent of the craft's
// instantaneous (r, v) — that is the point of the mode (a fixed inertial
// 3D departure Δv that no derived prograde/normal/radial mode expresses).
func TestBurnVectorNodeDirectionIsCaptured(t *testing.T) {
	dir := orbital.Vec3{X: 1, Y: 2, Z: -2} // not unit
	n := ManeuverNode{Mode: BurnVector, BurnDirUnit: dir}

	// Two unrelated states must yield the same (captured) direction.
	r1 := orbital.Vec3{X: 7e6}
	v1 := orbital.Vec3{Y: 7500}
	r2 := orbital.Vec3{Y: -8e6, Z: 1e6}
	v2 := orbital.Vec3{X: 6000, Z: 200}

	want := dir.Unit()
	got1 := NodeBurnDirection(n, r1, v1)
	got2 := NodeBurnDirection(n, r2, v2)
	if got1.Sub(want).Norm() > 1e-12 {
		t.Errorf("state 1: got %v, want %v", got1, want)
	}
	if got2.Sub(want).Norm() > 1e-12 {
		t.Errorf("state 2: got %v, want %v", got2, want)
	}
	if d := math.Abs(got1.Norm() - 1); d > 1e-12 {
		t.Errorf("direction not unit: |dir|=%.12f", got1.Norm())
	}
}

// TestBurnDirectionForBurnVector: the Spacecraft firing-path resolver
// returns the captured vector for BurnVector and delegates other modes
// to the standard plane-aware resolver.
func TestBurnDirectionForBurnVector(t *testing.T) {
	s := &Spacecraft{Primary: testEarth()}
	s.State.R = orbital.Vec3{X: 7e6}
	s.State.V = orbital.Vec3{Y: 7500}
	dir := orbital.Vec3{X: 0, Y: 0, Z: 3} // +Z, not unit
	got := s.BurnDirectionForBurn(BurnVector, orbital.Vec3{}, orbital.Vec3{}, 0, dir)
	if got.Sub(orbital.Vec3{Z: 1}).Norm() > 1e-12 {
		t.Errorf("BurnVector: got %v, want +Z unit", got)
	}
	// Prograde still resolves from state (delegates through).
	pg := s.BurnDirectionForBurn(BurnPrograde, orbital.Vec3{}, orbital.Vec3{}, 0, orbital.Vec3{})
	if pg.Sub(orbital.Vec3{Y: 1}).Norm() > 1e-9 {
		t.Errorf("Prograde via BurnDirectionForBurn: got %v, want +Y", pg)
	}
}

// TestBurnVectorNotInModeCycle: BurnVector is plant-only — it must not
// appear in the m-form mode cycle (the player reaches it only through the
// fused [H] auto-plant), and it has a HUD label.
func TestBurnVectorNotInModeCycle(t *testing.T) {
	for _, m := range AllBurnModes {
		if m == BurnVector {
			t.Fatal("BurnVector must not be in AllBurnModes (plant-only)")
		}
	}
	if BurnVector.String() == "?" || BurnVector.String() == "" {
		t.Errorf("BurnVector needs a HUD label, got %q", BurnVector.String())
	}
}
