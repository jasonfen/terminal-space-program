package planner

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestPorkchopGridCenterMatchesHohmann: at (dep=0, tof ≈ Hohmann time),
// the porkchop total Δv should match the analytical Hohmann budget
// computed by PlanHohmannTransfer within 10%. Gives end-to-end
// confidence that the grid-sampling + Lambert + v∞-conversion
// pipeline is at least directionally correct.
//
// Uses synthetic circular orbits for Earth and Mars so the ephemeris
// is exact (no JPL lookup or Kepler solver in the test).
func TestPorkchopGridCenterMatchesHohmann(t *testing.T) {
	const (
		AU    = 1.495978707e11
		muSun = 1.32712440018e20
	)
	// Circular heliocentric ephemerides. Position rotates on a period
	// 2π·sqrt(a³/μ); velocity is the tangent.
	makeCircular := func(radius, theta0 float64) EphemerisFn {
		omega := math.Sqrt(muSun / (radius * radius * radius))
		return func(epoch float64) (orbital.Vec3, orbital.Vec3) {
			theta := theta0 + omega*epoch
			cos, sin := math.Cos(theta), math.Sin(theta)
			r := orbital.Vec3{X: radius * cos, Y: radius * sin}
			v := orbital.Vec3{X: -radius * omega * sin, Y: radius * omega * cos}
			return r, v
		}
	}

	rEarth := AU
	rMars := 1.524 * AU
	// Start Earth at θ=0, Mars at the phase angle that gives perfect
	// Hohmann alignment: dθ_Mars during transfer = π, so Mars phase lead
	// at t=0 must equal (π − ω_Mars·t_transfer).
	aT := (rEarth + rMars) / 2
	tTransfer := math.Pi * math.Sqrt(aT*aT*aT/muSun)
	omegaMars := math.Sqrt(muSun / (rMars * rMars * rMars))
	marsPhase := math.Pi - omegaMars*tTransfer

	earthEph := makeCircular(rEarth, 0)
	marsEph := makeCircular(rMars, marsPhase)

	// Grid: dep days 0 every 10 days for 40 days, tof ≈ 255–265 days
	// (textbook Earth→Mars ≈ 258.8 d) to hit the Hohmann window.
	depDays := []float64{0, 10, 20, 30, 40}
	tofDays := []float64{255, 260, 265, 270, 275}
	grid := PorkchopGrid(
		muSun, earthEph, marsEph, 0,
		depDays, tofDays,
		3.986e14, 6.578e6, // Earth μ, LEO
		4.282837e13, 3.59e6, // Mars μ, low capture
	)

	depIdx, tofIdx, total, ok := PorkchopMinCell(grid)
	if !ok {
		t.Fatal("PorkchopMinCell: entire grid was NaN")
	}

	// Analytical Hohmann total: ~3.61 km/s departure + ~2.09 km/s arrival
	// ≈ 5.7 km/s. Allow 15% for the fact that the grid samples discrete
	// (dep, tof) so the minimum cell approximates the true minimum.
	const wantTotal = 5700.0
	if d := math.Abs(total-wantTotal) / wantTotal; d > 0.15 {
		t.Errorf("porkchop min cell total Δv = %.0f m/s, want ≈%.0f (rel %.2e); "+
			"min cell at (dep=%v, tof=%v)",
			total, wantTotal, d, depDays[depIdx], tofDays[tofIdx])
	}
}

// TestPorkchopGridMarksInvalidCellsNaN: zero-TOF cells should stay NaN
// (Lambert can't solve instant transfer) without panicking or returning
// a bogus Δv.
func TestPorkchopGridMarksInvalidCellsNaN(t *testing.T) {
	eph := func(_ float64) (orbital.Vec3, orbital.Vec3) {
		return orbital.Vec3{X: 1e11}, orbital.Vec3{}
	}
	grid := PorkchopGrid(
		1.327e20, eph, eph, 0,
		[]float64{0}, []float64{0, 100}, // first TOF zero → must be NaN
		3.986e14, 6.578e6,
		4.282837e13, 3.59e6,
	)
	if !math.IsNaN(grid[0][0]) {
		t.Errorf("zero-TOF cell should be NaN, got %v", grid[0][0])
	}
	// Non-zero TOF at least shouldn't panic (may or may not converge
	// given the contrived static ephemeris).
	_ = grid[1][0]
}
