// Package sim — landed-craft integration (v0.9.2+).
//
// A craft with Landed=true is parked on its primary's surface and
// co-rotates with the ground. Gravity / drag / thrust integration
// is bypassed; per tick we **recompute** R from the craft's stored
// LaunchLatDeg / LaunchLonDeg using render.BodyFixedToWorld at the
// current simTime. This keeps the craft visually pinned to the
// texture's rendered (lat, lon) cell as the body rotates, even
// across the v0.8.5+ texture pipeline's view-dependent rotation
// quirks (see render.BodyFixedToWorld doc).
//
// V is set to ω × R using the body's tilted spin axis so a future
// `Landed=false` transition lands the craft with surface co-
// rotation velocity, not stale state.
//
// Cleared by:
//   - World.StartManualBurn (engine ignition via `b`)
//   - Planted-burn fire-time (ActiveBurn becomes non-nil)
// Both transitions release the craft into normal integration with
// the surface velocity it had at the moment of ignition.

package sim

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// integrateLanded recomputes R from (LaunchLatDeg, LaunchLonDeg,
// simTime) using the renderer's body-fixed-to-world projection,
// then sets V = ω × R using the tilted spin axis. No-op when ω = 0
// (primary doesn't rotate); the craft just sits.
//
// integrateLanded is called from World.integrateOneCraft, which
// passes simDelta but the function ignores it — R is regenerated
// from absolute simTime, not advanced incrementally. The current
// simTime comes from c.Primary's rotation phase as accessed via
// render.BodyFixedToWorld.
func integrateLanded(w *World, c *spacecraft.Spacecraft, simDelta time.Duration) {
	radius := c.Primary.RadiusMeters()
	dirRender := render.BodyFixedToWorld(c.Primary, c.LaunchLatDeg, c.LaunchLonDeg, w.Clock.SimTime)
	c.State.R = orbital.Vec3{
		X: radius * dirRender.X,
		Y: radius * dirRender.Y,
		Z: radius * dirRender.Z,
	}
	omegaRender := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
	c.State.V = omega.Cross(c.State.R)
	c.State.M = c.TotalMass()

	// v0.10.0: a landed craft is rigidly bolted to the pad pointing
	// per its AttitudeMode; as it co-rotates with the body its nose
	// co-rotates too. The slew integrator is skipped while Landed
	// (this function returns before it), so without this sync a long
	// warp on the pad leaves CurrentAttitudeDir frozen at the spawn-
	// time vector — on liftoff the engine then thrusts along that
	// stale (now sideways / sub-horizon) nose and the craft can't
	// leave the pad. Track the commanded direction instead; skip a
	// zero/undefined commanded (e.g. surface mode pre-liftoff) so we
	// hold the last good nose rather than blanking it.
	if cmd := w.commandedDirFor(c); cmd.Norm() != 0 {
		c.CurrentAttitudeDir = cmd
	}
}
