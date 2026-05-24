// Package sim — launch-anchor for ViewTilted (v0.10.7+).
//
// While the active craft is in the "launch band" (apoapsis altitude
// ≤ LaunchMissionFloorM), re-anchor ViewTilted's yaw φ to the craft's
// local-vertical so the rocket rides straight up the screen and the
// planet rotates behind it (chase-plane orientation on launch).
//
// Predicate is the inverse of the ORBIT READY callout: anchor active
// while apoapsis altitude is finite and ≤ 200 km. Releases at the
// exact moment the player sees "ORBIT READY — coast to ap, press C
// to plant circularise." Atmosphere-agnostic — works on Moon too.
//
// On the launchpad the predicate fires naturally: a Landed craft
// co-rotates with the body (V = ω × R, so V ≈ 465 m/s at Earth's
// equator), giving a bound "orbit" with apoapsis right at the surface
// (apoAlt ≈ 0 ≤ 200 km).
//
// The anchor itself is render-computed-on-read: World.ViewTilt.Phi
// stays at the player's value (currently always 0; player-φ controls
// deferred to a post-playtest signal). screens.viewBasis calls
// LaunchAnchorPhi and overrides φ locally for the basis it builds.
// Future shift+←→ player wiring won't collide with per-tick anchor
// writes because the anchor never writes.

package sim

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// LaunchMissionFloorM is the apoapsis altitude (m) at which the
// launch-anchor releases. Same value as the saturn-v-pad-to-leo
// mission floor and the ORBIT READY callout's gate — single source
// of truth lives here. Mirrors internal/missions/missions.json:40.
// orbit.go's LAUNCH HUD vanish-gate (`shouldShowLaunchHUD`) reads
// this same constant via a sim import.
const LaunchMissionFloorM = 200_000.0

// LaunchAnchorPhi returns the chase-plane yaw φ (radians) that aligns
// the craft's local-vertical with screen-up under the v0.10.6
// perspective tilt, and an `active` bool that signals whether the
// anchor should be applied.
//
// Caller (screens.viewBasis) passes the (el, ok) pair it already
// computed via activeCraftElements:
//   - ok == true  → craft has a usable Keplerian orbit; φ is computed
//     in the perifocal frame from R projected onto (x̂, ŷ).
//   - ok == false → craft is Landed or its orbit is degenerate; φ is
//     computed in world XY from atan2(R.Y, R.X).
//
// Both paths yield φ = (angle of R̂ in the base plane) − π/2, which
// is exactly the rotation that puts R̂'s base-plane projection along
// the yawed ŷ axis (= screen-Y after polar tilt). Math derived in
// the v0.10.7 grilling pass; tests in launch_anchor_test.go confirm
// the canvas-X component of R̂ vanishes at the returned φ.
//
// Returns (0, false) when the craft is nil, μ is zero, or the
// apoapsis predicate fails (hyperbolic / degenerate / apoAlt above
// the launch-mission floor).
func LaunchAnchorPhi(c *spacecraft.Spacecraft, el orbital.Elements, ok bool) (phi float64, active bool) {
	if c == nil {
		return 0, false
	}
	mu := c.Primary.GravitationalParameter()
	if mu <= 0 {
		return 0, false
	}
	// Recompute elements from state so the predicate fires identically
	// in both branches (ok=true uses the same elements the caller
	// already validated; ok=false needs its own derivation because the
	// caller skipped past the perifocal path).
	pe := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if pe.E >= 1 || pe.A <= 0 || math.IsNaN(pe.A) || math.IsInf(pe.A, 0) {
		return 0, false
	}
	apoAlt := pe.Apoapsis() - c.Primary.RadiusMeters()
	if apoAlt > LaunchMissionFloorM {
		return 0, false
	}

	// Active. Compute φ in whichever base plane viewBasis is about to
	// use, so the projection of R̂ ends up along yawed-ŷ.
	rNorm := c.State.R.Norm()
	if rNorm == 0 {
		return 0, false
	}
	rHat := c.State.R.Unit()

	if ok {
		// Perifocal case (TiltedPerifocalBasis path).
		xHat, yHat := orbital.PerifocalBasis(el)
		rx := rHat.Dot(xHat) // = cos ν
		ry := rHat.Dot(yHat) // = sin ν
		// φ = ν − π/2 = atan2(−cos ν, sin ν)
		return math.Atan2(-rx, ry), true
	}
	// World/pad case (TiltedWorldBasis path). Project R̂ onto world XY
	// and rotate so its base-plane component aligns with world Y (which
	// becomes yawed-ŷ at φ = atan2(R.Y, R.X) − π/2).
	return math.Atan2(-c.State.R.X, c.State.R.Y), true
}
