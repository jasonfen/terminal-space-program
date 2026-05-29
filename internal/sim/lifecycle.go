// Package sim — surface-arrival lifecycle (v0.11.4+, per ADR 0004).
//
// `physics.ClampToSurface` has been the placeholder surface-handler
// since v0.8.5: it projects R back to the mean radius along r̂ and
// zeros V, leaving the vessel sitting with Landed=false. Without a
// real outcome, the clamp re-fires every tick.
//
// v0.11.4 differentiates the two outcomes the `CONTEXT.md`
// Touchdown / Crash glossary entry has been pre-declaring:
//
//   - **Touchdown** (controlled): the vessel is designed to land
//     (CanSoftLand) AND arrived below V_CRIT AND nose-aligned with
//     local-up (NOSE_TOL). Sets Landed=true at the touchdown
//     sub-craft point so the next tick routes through
//     integrateLanded (co-rotates with the surface).
//
//   - **Crash** (destructive): everything else. Sets Crashed=true;
//     the vessel skips integration and renders dimmed. End-flight
//     `[E]` removes the wreckage from the world.
//
// The predicate runs in the World wrapper around ClampToSurface
// (integrateOneCraft), so the physics package stays predicate-free
// and the soft-land/crash branching has access to the live
// World.Clock for the sub-craft-point inverse projection.

package sim

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const (
	// CrashVCritMps is the impact-velocity ceiling for a soft
	// landing. Falcon 9 / Apollo LM real touchdowns settle around
	// 1–2 m/s; uncontrolled free-fall onto a body at low orbital
	// altitudes is hundreds of m/s. 10 m/s is the starting value
	// (retunable from playtest).
	CrashVCritMps = 10.0

	// CrashNoseTol is the minimum nose-alignment dot product
	// (CurrentAttitudeDir · localUp) for a soft landing. 0.7 ≈ 45°
	// off local vertical — a Falcon 9 with the nose past 45° from
	// up isn't going to land softly; flag as crash.
	CrashNoseTol = 0.7
)

// surfaceArrivalOutcome encodes what the predicate decided. Local
// to the lifecycle dispatch site; not exported. Every Surface
// Contact resolves to exactly one of these two — v0.12.0 deleted
// the vestigial third "fallback" bucket (zero-V, neither flag) that
// ADR 0004 shipped as a defensive placeholder, since the predicate
// always classifies a contact as Landed or Crashed (a non-CanSoftLand
// vessel that grazes the surface is Crashed, not a third state). The
// classifier is exhaustive: classifySurfaceArrival always returns
// one of these.
type surfaceArrivalOutcome int

const (
	outcomeLanded surfaceArrivalOutcome = iota
	outcomeCrashed
)

// classifySurfaceArrival runs the v0.11.4 predicate at the
// ClampToSurface site. preClampV is the velocity vector before the
// clamp zeroed it; preClampR is the position at which contact was
// detected. Returns:
//
//   - outcomeLanded with the sub-craft (lat, lon) when the vessel
//     qualifies for a soft touchdown (CanSoftLand + |V| < V_CRIT +
//     nose alignment > NOSE_TOL).
//   - outcomeCrashed with zero (lat, lon) when it doesn't —
//     including the historical case of a non-CanSoftLand vessel
//     hitting the surface at any speed.
//
// Pure (no World state mutation) so unit tests can exercise the
// branching with hand-built inputs. The integrateOneCraft caller
// is responsible for applying the result to the Spacecraft and
// stopping integration.
func classifySurfaceArrival(
	c *spacecraft.Spacecraft,
	preClampR, preClampV orbital.Vec3,
	simTime time.Time,
) (surfaceArrivalOutcome, float64, float64) {
	vImpact := preClampV.Norm()
	if !c.CanSoftLand || vImpact >= CrashVCritMps {
		return outcomeCrashed, 0, 0
	}
	rMag := preClampR.Norm()
	if rMag == 0 {
		return outcomeCrashed, 0, 0
	}
	localUp := preClampR.Scale(1 / rMag)
	nose := c.CurrentAttitudeDir
	if nose.Norm() == 0 {
		return outcomeCrashed, 0, 0
	}
	nose = nose.Scale(1 / nose.Norm())
	cosNose := nose.Dot(localUp)
	if cosNose <= CrashNoseTol {
		return outcomeCrashed, 0, 0
	}
	dir := render.Vec3{X: localUp.X, Y: localUp.Y, Z: localUp.Z}
	latDeg, lonDeg := render.WorldToBodyFixed(c.Primary, dir, simTime)
	return outcomeLanded, latDeg, lonDeg
}

// applySurfaceArrival mutates c per the classifier outcome. Called
// from integrateOneCraft once the sub-step loop detects a hit.
// Caller is responsible for stopping the sub-step loop after
// invocation (the clamped state is already on c.State).
func applySurfaceArrival(c *spacecraft.Spacecraft, clamped physics.StateVector, outcome surfaceArrivalOutcome, lat, lon float64) {
	c.State = clamped
	switch outcome {
	case outcomeLanded:
		c.Landed = true
		c.LandedLatDeg = lat
		c.LandedLonDeg = lon
		// Soft-landed vessels do NOT route to ViewLaunch (OnPad is
		// cleared on liftoff and stays cleared after touchdown).
		// Reset CurrentAttitudeDir to local-up so the next tick's
		// integrateLanded starts with a sane commanded nose; the
		// pad-warp slew sync covers post-touchdown attitude.
		if rMag := c.State.R.Norm(); rMag > 0 {
			c.CurrentAttitudeDir = c.State.R.Scale(1 / rMag)
		}
	case outcomeCrashed:
		c.Crashed = true
	}
}
