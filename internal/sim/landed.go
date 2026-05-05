// Package sim — landed-craft integration (v0.9.2+).
//
// A craft with Landed=true is parked on its primary's surface and
// co-rotates with the ground. Gravity / drag / thrust integration
// is bypassed; per tick we rotate R about the body's tilted spin
// axis by ω·dt (matching the v0.8.5 texture renderer's frame so
// a craft on Florida stays on Florida as Earth rotates). V is
// recomputed as ω × R so a future `Landed=false` transition lands
// the craft with surface co-rotation velocity, not stale state.
//
// The rotation axis is the body's actual spin axis (BodyRotationAxisWorld)
// — picks up AxialTilt + AxialAzimuth so Earth's 23.44° tilt is
// honoured. Differs from physics.AtmosphereOmega which approximates
// the spin axis as world +Z for drag.
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
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// integrateLanded advances a Landed craft's state by simDelta:
// rotates R about the primary's tilted spin axis by ω·dt (Rodrigues'
// formula), sets V = ω × R. No-op when ω = 0 (primary doesn't
// rotate — catalog miss); the craft just sits.
func integrateLanded(c *spacecraft.Spacecraft, simDelta time.Duration) {
	omegaRender := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
	omegaMag := omega.Norm()
	if omegaMag == 0 {
		c.State.V = orbital.Vec3{}
		return
	}
	axis := omega.Scale(1 / omegaMag)
	angle := omegaMag * simDelta.Seconds()
	c.State.R = rodriguesRotate(c.State.R, axis, angle)
	c.State.V = omega.Cross(c.State.R)
	c.State.M = c.TotalMass()
}

// rodriguesRotate rotates v about unit axis k by angle θ:
//   v' = v cos θ + (k × v) sin θ + k (k · v) (1 − cos θ)
// Used for landed-craft integration about the body's tilted spin
// axis. Pure math; no package-level state. v0.9.2+.
func rodriguesRotate(v, k orbital.Vec3, theta float64) orbital.Vec3 {
	cosT := math.Cos(theta)
	sinT := math.Sin(theta)
	kDotV := k.X*v.X + k.Y*v.Y + k.Z*v.Z
	kCrossV := k.Cross(v)
	return orbital.Vec3{
		X: v.X*cosT + kCrossV.X*sinT + k.X*kDotV*(1-cosT),
		Y: v.Y*cosT + kCrossV.Y*sinT + k.Y*kDotV*(1-cosT),
		Z: v.Z*cosT + kCrossV.Z*sinT + k.Z*kDotV*(1-cosT),
	}
}
