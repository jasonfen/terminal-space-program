package physics

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestKeplerStepNearCircularConsistency — for a near-circular orbit with
// e in the band (1e-9, 1e-6), the old elliptic branch computed
// cosE = (1 − r/a)/e, amplifying float noise by ~1/e (1e6–1e9×) until
// the clamp pinned cosE to ±1, forcing E0 = 0 and M0 ≈ 0. That snapped
// the craft back to periapsis on every warp-locked Kepler step — a
// visible teleport. Raising the circular threshold to match orbital's
// circularTol (1e-6) routes this band through the perifocal-angle path,
// which is noise-free. Here a craft one radian past periapsis must
// advance ~n·dt further along, not jump back toward periapsis. (#90)
func TestKeplerStepNearCircularConsistency(t *testing.T) {
	const mu = 3.986004418e14
	el := orbital.Elements{A: 7.0e6, E: 5e-7, I: 0.5, Omega: 0.3, Arg: 0.7}

	nu0 := 1.0 // one radian past periapsis — far from the periapsis snap point
	r0 := orbital.PositionAtTrueAnomaly(el, nu0)
	v0 := orbital.VelocityAtTrueAnomaly(el, nu0, mu)

	out, ok := KeplerStep(StateVector{R: r0, V: v0}, mu, 100)
	if !ok {
		t.Fatalf("KeplerStep returned ok=false for an elliptic near-circular orbit")
	}

	// Expected forward advance over 100 s.
	n := math.Sqrt(mu / (el.A * el.A * el.A))
	wantAdvance := n * 100 // ≈ 0.108 rad

	// Angular separation between the start and stepped position.
	cosSep := r0.Dot(out.R) / (r0.Norm() * out.R.Norm())
	if cosSep > 1 {
		cosSep = 1
	} else if cosSep < -1 {
		cosSep = -1
	}
	sep := math.Acos(cosSep)

	// A teleport back to periapsis would put `out` ~nu0 (≈1 rad) away
	// from r0; a correct step advances by ~wantAdvance. Allow generous
	// slack but well below the ~0.89 rad a snap-to-periapsis produces.
	if math.Abs(sep-wantAdvance) > 0.05 {
		t.Errorf("stepped position moved %.4f rad from start, want ≈%.4f (advance by n·dt); large gap indicates a periapsis-snap teleport", sep, wantAdvance)
	}
}
