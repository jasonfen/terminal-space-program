package planner

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// EphemerisFn returns the heliocentric (system-primary-centered)
// position and velocity of a body at the given sim-time epoch, in SI
// units (m, m/s). The planner package doesn't know about bodies/orbital
// elements — callers (typically sim.World) adapt their Kepler/
// calculator machinery into this function type.
type EphemerisFn func(epoch float64) (r, v orbital.Vec3)

// PorkchopGrid evaluates a grid of Lambert transfers and returns per-
// cell total Δv (departure + arrival, m/s). NaN marks cells where the
// Lambert solver failed to converge — the TUI can render those as
// "impossible" pixels.
//
// The returned slice is indexed [tofIdx][depIdx] so rendering row-by-
// row in the TUI naturally walks TOF vertically and departure day
// horizontally.
//
// - epoch0: sim-time in seconds at which depDays[0] is measured. The
//   ephemeris is sampled at epoch0 + depDays[i]*86400 for departure and
//   epoch0 + (depDays[i]+tofDays[j])*86400 for arrival.
// - depState, arrState: body ephemerides (heliocentric r, v).
// - muSun: gravitational parameter of the system primary.
// - muDep, rPark: departure body μ + parking-orbit radius (for
//   departure Δv via the patched-conic identity).
// - muArr, rCapture: arrival body μ + capture-orbit radius.
// - retrograde: forwarded to LambertSolve to select the prograde or
//   retrograde transfer branch. The TUI surfaces prograde today;
//   retrograde unblocks multi-rev porkchop work in v0.8+. v0.7.5+.
// - nRev / longBranch: forwarded to LambertSolveRev. nRev=0 +
//   longBranch=false is the legacy single-rev short-only path
//   (byte-identical to pre-v0.10.5). nRev≥1 scores N-revolution
//   transfers; longBranch picks the higher-z (long) root of the
//   two-branch N-rev solution. v0.10.5+.
func PorkchopGrid(
	muSun float64,
	depState, arrState EphemerisFn,
	epoch0 float64,
	depDays, tofDays []float64,
	muDep, rPark float64,
	muArr, rCapture float64,
	retrograde bool,
	nRev int,
	longBranch bool,
) [][]float64 {
	grid := make([][]float64, len(tofDays))
	for j := range grid {
		grid[j] = make([]float64, len(depDays))
		for i := range grid[j] {
			grid[j][i] = math.NaN()
		}
	}

	const secondsPerDay = 86400.0

	for i, dep := range depDays {
		tDep := epoch0 + dep*secondsPerDay
		r1, vDep := depState(tDep)
		for j, tof := range tofDays {
			if tof <= 0 {
				continue
			}
			tArr := tDep + tof*secondsPerDay
			r2, vArr := arrState(tArr)

			v1, v2, err := LambertSolveRev(r1, r2, tof*secondsPerDay, muSun, nRev, retrograde, longBranch)
			if err != nil {
				continue // leave NaN
			}

			vInfDep := v1.Sub(vDep).Norm()
			vInfArr := v2.Sub(vArr).Norm()
			dvDep, err := EscapeBurnDeltaV(vInfDep, muDep, rPark)
			if err != nil {
				continue
			}
			dvArr, err := CaptureBurnDeltaV(vInfArr, muArr, rCapture)
			if err != nil {
				continue
			}
			grid[j][i] = dvDep + dvArr
		}
	}
	return grid
}

// PorkchopMinCell scans a grid and returns the (depIdx, tofIdx, total)
// of the lowest-Δv non-NaN cell. ok=false if the entire grid is NaN.
func PorkchopMinCell(grid [][]float64) (depIdx, tofIdx int, total float64, ok bool) {
	best := math.Inf(1)
	for j := range grid {
		for i, v := range grid[j] {
			if math.IsNaN(v) {
				continue
			}
			if v < best {
				best = v
				depIdx = i
				tofIdx = j
				ok = true
			}
		}
	}
	total = best
	return
}
