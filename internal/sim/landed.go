// Package sim — landed-craft integration (v0.9.2+).
//
// A craft with Landed=true is parked on its primary's surface and
// co-rotates with the ground. Gravity / drag / thrust integration
// is bypassed; per tick we just rotate R about world +Z by ω·dt
// (matching the v0.8.4 drag and v0.9.2 spawn convention that treats
// the spin axis as +Z, ignoring axial tilt). V is recomputed as
// ω × R so a future `Landed=false` transition lands the craft with
// surface co-rotation velocity, not stale state.
//
// Cleared by:
//   - World.StartManualBurn (engine ignition via `b`)
//   - Planted-burn fire-time (ActiveBurn becomes non-nil)
// Both transitions release the craft into normal integration with
// the surface velocity it had at the moment of ignition.

package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// integrateLanded advances a Landed craft's state by simDelta:
// rotates R about world +Z at the primary's spin rate; sets V =
// ω × R. No-op when ω = 0 (primary doesn't rotate — tidally
// locked moons or catalog miss); the craft just sits.
func integrateLanded(c *spacecraft.Spacecraft, simDelta time.Duration) {
	omega := physics.AtmosphereOmega(c.Primary)
	if omega.Z == 0 {
		// No rotation defined — leave R, V untouched.
		c.State.V = omega.Cross(c.State.R) // = zero vector
		return
	}
	dt := simDelta.Seconds()
	angle := omega.Z * dt
	cosA, sinA := math.Cos(angle), math.Sin(angle)
	r := c.State.R
	c.State.R = orbital.Vec3{
		X: r.X*cosA - r.Y*sinA,
		Y: r.X*sinA + r.Y*cosA,
		Z: r.Z,
	}
	c.State.V = omega.Cross(c.State.R)
	c.State.M = c.TotalMass()
}
