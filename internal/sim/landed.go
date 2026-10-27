// Package sim — landed-craft integration (v0.9.2+).
//
// A craft with Landed=true is parked on its primary's surface and
// co-rotates with the ground. Gravity / drag / thrust integration
// is bypassed; per tick we **recompute** R from the craft's stored
// body-fixed coords — LandedLatDeg / LandedLonDeg for a soft
// touchdown, else LaunchLatDeg / LaunchLonDeg for a launchpad spawn —
// using render.BodyFixedToWorld at the current simTime. This keeps
// the craft visually pinned to the
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

// integrateLanded recomputes R from the craft's body-fixed coords
// (LandedLatDeg/LonDeg if soft-landed, else LaunchLatDeg/LonDeg) and
// simTime using the renderer's body-fixed-to-world projection,
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
	// v0.11.4 shipped LandedLatDeg/LonDeg (soft-touchdown coords) but
	// never wired them in here — a soft-landed craft was re-pinned to
	// its launch site projected onto the arrival body (or (0,0) for an
	// orbit-spawned craft). SurfaceLatLon prefers the touchdown coords
	// when set, else the launchpad-spawn coords (v0.21: shared with the
	// mission evaluator so both pin to the same point).
	lat, lon := c.SurfaceLatLon()
	dirRender := render.BodyFixedToWorld(c.Primary, lat, lon, w.Clock.SimTime)
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

// seatedThrottle is the core throttle the craft's running burn fires at, and a
// func that undoes any override. A planted node fires at its captured throttle
// with the player's booster throttle ignored, exactly as stepThrust runs it
// (#734); a manual burn uses the live throttle.
func seatedThrottle(c *spacecraft.Spacecraft) (core float64, restore func()) {
	if c.ActiveBurn == nil {
		return c.EffectiveThrottle(), func() {}
	}
	saved := c.BoosterThrottle
	c.BoosterThrottle = nil
	core = c.ActiveBurn.Throttle
	if core <= 0 {
		core = 1
	}
	return core, func() { c.BoosterThrottle = saved }
}

// canLiftOff reports whether the landed craft's thrust along local up beats
// its weight at its burn's throttle. #716: below that the engine runs but the
// vessel stays seated; releasing it into free integration sank it into the
// ground and classifySurfaceArrival called that a crash.
func (w *World) canLiftOff(c *spacecraft.Spacecraft) bool {
	m, r := c.TotalMass(), c.State.R.Norm()
	if m <= 0 || r <= 0 {
		return true
	}
	nose := w.commandedDirFor(c) // what integrateLanded will pin the nose to
	if nose.Norm() == 0 {
		nose = c.CurrentAttitudeDir
	}
	up := c.State.R.Scale(1 / r)
	core, restore := seatedThrottle(c)
	defer restore()
	lift := c.Thrust * c.LitThrottle(core) / m * nose.Unit().Dot(up)
	return lift > c.Primary.GravitationalParameter()/(r*r)
}

// leavePad releases a landed craft into free flight. v0.11.4+ (ADR 0004): the
// first liftoff clears OnPad so a later soft-landing's Landed false to true
// transition doesn't fire the ViewLaunch auto-route (which gates on OnPad).
// ADR 0062 decision 2: that first liftoff is also stamped as LiftoffSimTime,
// the zero of the Flight Recorder's T+ clock; a soft-landed craft (OnPad
// already false) lifting again stamps nothing.
func (w *World) leavePad(c *spacecraft.Spacecraft) {
	c.Landed = false
	if c.OnPad {
		c.OnPad = false
		c.LiftoffSimTime = w.Clock.SimTime
	}
}

// burnSeated runs a manual burn that cannot yet lift the craft: propellant is
// spent and mass falls (so TWR rises), the craft stays pinned. A dry tank ends
// the burn exactly as it does in flight. A planted burn's window is held (as a
// fuel-stalled one is) so it still has its Δv to deliver once it lifts (#734).
func (w *World) burnSeated(c *spacecraft.Spacecraft, simDelta time.Duration) {
	core, restore := seatedThrottle(c)
	defer restore()
	c.BurnFuelAt(core, simDelta.Seconds())
	if c.ActiveBurn != nil {
		c.ActiveBurn.EndTime = c.ActiveBurn.EndTime.Add(simDelta)
	} else if c.ActiveStageFuel() <= 0 {
		c.ManualBurn = nil
		c.DryOrder = true
	} else if endsCutBurn(c) {
		c.ManualBurn = nil
	}
}
