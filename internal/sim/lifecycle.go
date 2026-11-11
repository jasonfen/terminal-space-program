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

	// ChuteOpenAltM is the height above the ground an ARMED parachute
	// opens at (ADR 0069 decision 5, Dragon's 6,000 ft mains), on every body
	// with air. At exactly this height it opens (<=); armed below it, it
	// opens at once. Retunable from playtest.
	ChuteOpenAltM = 1800.0
)

// maybeDeployParachute opens an armed parachute passing ChuteOpenAltM above
// the ground (at it, or already below it when armed). Returns true if it
// transitioned this call. No-op for a stowed, open or cut chute, and on a
// body with no air (a canopy there is only a free soft-landing route). Called
// once per sub-step from integrateOneCraft before the effective-BC read, so
// the deployed BC engages for the sub-step the deploy fires on.
func maybeDeployParachute(c *spacecraft.Spacecraft) bool {
	if c.ChuteState != spacecraft.ChuteArmed || c.Primary.Atmosphere == nil {
		return false
	}
	if c.State.R.Norm()-c.Primary.RadiusMeters() <= ChuteOpenAltM {
		c.ChuteState = spacecraft.ChuteDeployed
		return true
	}
	return false
}

// chuteOpeningWithin finds where one step of h from pre crosses ChuteOpenAltM
// going down: the offset tau and the state there, just above the opening. ok
// is false when pre is already at or under it, or the step ends above it.
// The canopy opens at that instant, not at the start of the step that ends
// below it, so the open does not depend on the step size: at 0.5 s a 950 m/s
// entry opened up to 475 m late (and 48 m at 0.05 s), which moved the
// touchdown by hundreds of metres with the warp (ADR 0069 slice 9).
func chuteOpeningWithin(pre physics.StateVector, h, radius float64, step func(physics.StateVector, float64) physics.StateVector) (tau float64, mid physics.StateVector, ok bool) {
	if pre.R.Norm()-radius <= ChuteOpenAltM || step(pre, h).R.Norm()-radius > ChuteOpenAltM {
		return 0, pre, false
	}
	lo, hi := 0.0, h
	for i := 0; i < 40; i++ {
		m := (lo + hi) / 2
		if step(pre, m).R.Norm()-radius > ChuteOpenAltM {
			lo = m
		} else {
			hi = m
		}
	}
	return lo, step(pre, lo), true
}

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

// classifySurfaceArrival runs the surface-arrival predicate at the
// ClampToSurface site (v0.11.4; chute route added v0.12 Slice 3 / ADR
// 0008). preClampV is the velocity vector before the clamp zeroed it;
// ContactZoneAltM is the altitude under which the chips remind the pilot of the
// contact limits ("(need < 10)").
const ContactZoneAltM = 100.0

// contactSpeedMps is the speed the crash rule compares with CrashVCritMps: the
// speed over the spinning ground (v - omega x r), on both routes. It is what the
// descent readouts and the recorded impact show; the inertial speed carried the
// surface's spin into the test (4.6 m/s at the Moon's equator, ~465 m/s at
// Earth's), so a Moon landing at 8 m/s over the ground crashed (playtest
// 2026-11-08).
func contactSpeedMps(c *spacecraft.Spacecraft, r, v orbital.Vec3) float64 {
	return physics.AirRelativeVelocity(r, v, c.Primary).Norm()
}

// ContactSpeedCrashes reports whether the vessel, inside ContactZoneAltM,
// would crash on contact by speed: the same speed and comparison as
// classifySurfaceArrival, so the chip can never disagree with the crash.
func ContactSpeedCrashes(c *spacecraft.Spacecraft) bool {
	return c.State.R.Norm()-c.Primary.RadiusMeters() <= ContactZoneAltM &&
		contactSpeedMps(c, c.State.R, c.State.V) >= CrashVCritMps
}

// NoseOutsideContactTol reports whether, inside ContactZoneAltM, the nose is
// past the engine route's CrashNoseTol (the same dot and comparison as
// classifySurfaceArrival). A deployed chute waives the nose.
func NoseOutsideContactTol(c *spacecraft.Spacecraft) bool {
	rMag := c.State.R.Norm()
	nose := c.CurrentAttitudeDir
	if rMag == 0 || nose.Norm() == 0 || c.ChuteState == spacecraft.ChuteDeployed ||
		rMag-c.Primary.RadiusMeters() > ContactZoneAltM {
		return false
	}
	return nose.Scale(1/nose.Norm()).Dot(c.State.R.Scale(1/rMag)) < CrashNoseTol
}

// preClampR is the position at which contact was detected. Returns:
//
//   - outcomeLanded with the sub-craft (lat, lon) when the vessel
//     qualifies for a soft touchdown via either the engine route
//     (CanSoftLand + |V_inertial| < V_CRIT + nose alignment > NOSE_TOL)
//     or the chute route (ChuteDeployed + |v_rel| < V_CRIT, nose
//     waived — air-relative speed, since a chute nulls velocity
//     relative to the co-rotating atmosphere, not the inertial frame).
//   - outcomeCrashed with the impact (lat, lon) when it doesn't —
//     including the historical case of a vessel with neither
//     CanSoftLand nor a deployed chute hitting the surface at any speed.
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
	rMag := preClampR.Norm()
	if rMag == 0 {
		return outcomeCrashed, 0, 0
	}
	localUp := preClampR.Scale(1 / rMag)
	lat, lon := render.WorldToBodyFixed(c.Primary, render.Vec3{X: localUp.X, Y: localUp.Y, Z: localUp.Z}, simTime)

	// Two routes qualify a contact as a soft Touchdown, each with its
	// own gates (ADR 0004 engine route + ADR 0008 chute route). They
	// differ on the capability gate, the nose-alignment requirement, AND
	// the reference frame of the velocity ceiling:
	//
	//   engine route:  CanSoftLand   && |v_rel| < V_CRIT && cosNose > NOSE_TOL
	//   chute route:   ChuteDeployed && |v_rel| < V_CRIT                  (nose waived)
	//
	// The chute route measures *air-relative* speed v_rel = V − ω×r, not
	// inertial |V|. A parachute nulls the vessel's velocity relative to
	// the co-rotating atmosphere, not the inertial frame — so on a fast-
	// rotating body a perfectly-descending capsule still carries the
	// surface co-rotation velocity (~465 m/s at Earth's equator) in the
	// inertial frame. Testing inertial |V| would crash every Earth
	// splashdown despite an ideal canopy descent; v_rel is what the chute
	// actually controls (ADR 0008 — amended after Slice 3 verification).
	// The engine route measures v_rel too (2026-11-08): it used to keep
	// inertial |V| on the theory that the Moon's ω×r is negligible, but
	// 4.6 m/s at the equator is half the 10 m/s margin, and on Earth the
	// spin alone (~465 m/s) crashed every powered landing.
	//
	// The chute route also waives NOSE_TOL: under a canopy the chute is
	// the stabiliser, the player isn't flying attitude, and demanding a
	// nose angle for a passive descent is the artificial part. The engine
	// route keeps its nose check (a Falcon 9 thrusting 60° off-vertical
	// is still a crash).
	switch {
	case c.ChuteState == spacecraft.ChuteDeployed:
		// chute route — air-relative velocity ceiling; nose waived.
		if contactSpeedMps(c, preClampR, preClampV) >= CrashVCritMps {
			return outcomeCrashed, lat, lon
		}
	case c.CanSoftLand:
		// engine route — ground-relative velocity ceiling + nose alignment.
		if contactSpeedMps(c, preClampR, preClampV) >= CrashVCritMps {
			return outcomeCrashed, lat, lon
		}
		nose := c.CurrentAttitudeDir
		if nose.Norm() == 0 {
			return outcomeCrashed, lat, lon
		}
		nose = nose.Scale(1 / nose.Norm())
		// Strictly-below rejects; at-or-above NOSE_TOL lands. The route
		// comments document "land when nose > NOSE_TOL" and the velocity
		// gates use >= (reject at-or-above), so a craft sitting exactly at
		// the 0.7 minimum is the boundary that should land. (#90)
		if nose.Dot(localUp) < CrashNoseTol {
			return outcomeCrashed, lat, lon
		}
	default:
		// neither capability — Crash regardless of how gentle (a non-
		// CanSoftLand vessel with no deployed chute).
		return outcomeCrashed, lat, lon
	}
	return outcomeLanded, lat, lon
}

// applySurfaceArrival mutates c per the classifier outcome. Called
// from integrateOneCraft once the sub-step loop detects a hit.
// Caller is responsible for stopping the sub-step loop after
// invocation (the clamped state is already on c.State).
func (w *World) applySurfaceArrival(c *spacecraft.Spacecraft, clamped physics.StateVector, outcome surfaceArrivalOutcome, lat, lon float64) {
	c.State = clamped
	if c.Landing == nil || !c.Landing.Engaged {
		c.LZClaim = spacecraft.LZClaim{} // landed or crashed: the claim is spent (ADR 0068 decision 5)
	} // an engaged autopilot spends it in landingEnded, after its flash reads it
	switch outcome {
	case outcomeLanded:
		lat, lon = w.setDownBeside(c, lat, lon) // ADR 0068 decision 10
		c.Landed = true
		c.LandedLatDeg = lat
		c.LandedLonDeg = lon
		// ADR 0068 decision 7: a touchdown inside an LZ plants no flag
		if s, n, d, in := w.lzAt(c, lat, lon); in {
			if ap := c.Landing; !ap.Owns() { // the autopilot's own flash says it
				w.PendingLandingEvents = append(w.PendingLandingEvents,
					LandingEvent{CraftName: c.Name, SiteName: s.lzLabel(n), DistanceM: d, Background: w.ActiveCraft() != c})
			}
		} else {
			w.plantSite(c, SiteFlag, "site", lat, lon) // ADR 0066 decision 3
		}
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
		// ADR 0066 decision 8: remember the crater; the wreck is pinned to it
		// and a wreck site outlives the vessel.
		c.LandedLatDeg = lat
		c.LandedLonDeg = lon
		w.plantSite(c, SiteWreck, "wreck", lat, lon)
	}
}
