// Package sim — the ascent half of the surface view (issue #348 §3, ADR
// 0043), the mirror image of descent.go's descent half (PR #354). Three
// pure forecasts the launch/surface screen renders while a vessel is
// climbing:
//
//   - AttitudeVectorsFor: the nose-vs-prograde pair a gravity turn pulls
//     apart, then brings back together near MECO.
//   - PredictAscentPath: the ballistic-from-now path ahead of the sprite,
//     sharing forwardBallisticPath (descent.go) with the descent arc so
//     the two halves never integrate the same curve twice (the #66
//     two-drift-sites lesson).
//   - AscentQBandFor: the atmosphere depth / dynamic-pressure band, with
//     the peak Q actually measured so far this session (World.LaunchMaxQ)
//     marked on it — see AscentQBand's doc comment for why "measured so
//     far" rather than a forecast eventual peak.
//
// AscentCueFor gates all three behind one climbing predicate — the
// mirror image of DescentCorridorFor's falling gate — so the surface
// view can never show the ascent cues and the descent corridor at once.
package sim

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// AscentPredictHorizon bounds how far ahead the ascent arc forecasts.
// Shorter than DescentPredictHorizon (30 min): the ascent arc's job is
// "what does the immediate flight path look like from here", drawn close
// enough to the sprite to stay legible on the chase-cam canvas — not a
// full orbital-period lookahead. A craft still governed by atmosphere or
// gravity typically resolves (apoapsis, or a fall back to ground) well
// inside 10 minutes; one that reaches a stable orbit inside the horizon
// just draws a partial arc of it (PredictAscentPath's no-impact branch).
const AscentPredictHorizon = 10 * time.Minute

// climbRateFloorMps is the ascent mirror of descentRateFloorMps
// (descent.go): the surface-relative climb rate below which the ascent
// cues stand down, so a vessel at zero net vertical rate (coasting near
// apoapsis, sitting on the pad before liftoff) doesn't flicker the cues
// on numerical dust. Same magnitude as its descent counterpart — there
// is no physical reason the two floors should differ.
const climbRateFloorMps = 0.1

// AttitudeVectors is the nose-vs-prograde pair the ascent cues plot as
// vector stubs on the sprite (issue #348 §3): the vessel's commanded
// pointing direction and its current direction of travel through the
// air. A gravity turn is exactly the story of these two vectors drifting
// apart and then converging again near MECO — showing both anchored to
// the vessel, rather than only on the corner navball, makes that story
// readable at a glance.
type AttitudeVectors struct {
	// NoseDir is the vessel's physical pointing direction
	// (Spacecraft.CurrentAttitudeDir), unit length, in the same
	// primary-relative frame State.R lives in.
	NoseDir orbital.Vec3
	// ProgradeDir is the surface-relative velocity direction
	// (physics.AirRelativeVelocity, unit length) — the vector a gravity
	// turn actually tracks. Air-relative rather than inertial keeps a
	// launchpad-co-rotating vessel's prograde undefined-at-rest instead
	// of pointing along the planet's spin, matching every other surface
	// view instrument (Q, the descent corridor's descent rate).
	ProgradeDir orbital.Vec3
}

// AttitudeVectorsFor resolves a craft's current nose and prograde unit
// vectors, or ok=false when either is undefined: no commanded attitude
// yet (pre-first-tick), or zero air-relative velocity (sitting still on
// the pad — surface-relative prograde has no direction until the vessel
// actually moves relative to the ground it's leaving).
func AttitudeVectorsFor(c *spacecraft.Spacecraft) (AttitudeVectors, bool) {
	if c == nil {
		return AttitudeVectors{}, false
	}
	nose := c.CurrentAttitudeDir
	noseN := nose.Norm()
	if noseN == 0 {
		return AttitudeVectors{}, false
	}
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	vN := vRel.Norm()
	if vN == 0 {
		return AttitudeVectors{}, false
	}
	return AttitudeVectors{
		NoseDir:     nose.Scale(1 / noseN),
		ProgradeDir: vRel.Scale(1 / vN),
	}, true
}

// AscentPath is the sampled forward-ballistic trajectory the ascent arc
// draws ahead of the sprite (issue #348 §3). Unlike ImpactPrediction it
// is populated even when the horizon never finds ground contact — a
// climbing vessel, or one that has already reached a stable-enough orbit,
// still has a path worth drawing; it just doesn't end at an impact
// marker.
type AscentPath struct {
	// Path is the sampled trajectory from the craft's current position
	// (Path[0]) forward. Ends at ground contact when the forecast finds
	// one inside the horizon, otherwise ends wherever the horizon ran
	// out (or the vessel settled into an orbit that never dips back to
	// the surface).
	Path []orbital.Vec3
	// Impact is the ground-contact point; meaningful only when
	// ImpactFound is true (a lofted or suborbital ascent can still arc
	// back down inside the horizon).
	Impact      orbital.Vec3
	ImpactFound bool
}

// PredictAscentPath forward-propagates the craft's current state exactly
// like PredictImpact — same forwardBallisticPath loop, same sub-step
// policy — but always returns the sampled path. PredictImpact discards
// its path on the "no impact inside the horizon" branch, which is
// exactly the common ascent case (still climbing, or already in an orbit
// stable enough that the ballistic-from-now forecast never touches
// ground). ok is false only for the same degenerate-input cases
// PredictImpact refuses.
func PredictAscentPath(c *spacecraft.Spacecraft, horizon time.Duration) (AscentPath, bool) {
	fc, setupOK := forwardBallisticPath(c, horizon)
	if !setupOK {
		return AscentPath{}, false
	}
	return AscentPath{Path: fc.Path, Impact: fc.Impact, ImpactFound: fc.ImpactFound}, true
}

// AscentQBand is the atmosphere/Q instrument the ascent cues draw as a
// vertical scale (issue #348 §3): where the vessel currently sits
// against the atmosphere's depth, its current dynamic pressure, and the
// altitude of the peak Q observed so far this session.
//
// Design note on "max Q": the ascent forecast (PredictAscentPath) is
// deliberately ballistic-from-now, exactly like PredictImpact — it has
// no modelled future thrust or pitch program to integrate forward, so a
// genuine FORECAST of the eventual max-Q altitude isn't something this
// physics can honestly produce; it would have to assume a throttle and
// attitude program the player hasn't committed to yet. What the sim DOES
// know honestly is the peak Q actually measured so far: World.LaunchMaxQ
// / LaunchMaxQAltM, ratcheted every session tick since v0.11
// (updateLaunchMaxQ, view_launch.go). So the band marks THAT — the
// altitude of the peak already passed, updating live as a new peak is
// measured — rather than a predicted peak that doesn't exist yet on a
// fresh climb.
type AscentQBand struct {
	AtmosphereDepthM float64
	CurrentAltM      float64
	CurrentQPa       float64
	MaxQPa           float64
	MaxQAltM         float64
	// HasMaxQ is false until the session has actually measured a
	// positive Q — before that there is no peak to mark, and drawing one
	// at altitude 0 would read as "max Q happened at the pad".
	HasMaxQ bool
}

// AscentQBandFor builds the Q-band instrument for a craft, or ok=false
// when the primary has no atmosphere at all — issue #348 §3's gate ("Q
// band only on bodies WITH atmosphere"). Independent of whether the
// vessel is CURRENTLY inside the atmosphere: a vessel that has already
// climbed clear of the cutoff still has a meaningful band (it reads
// "past the top", clamped by qBandRowIndex) carrying the max-Q mark from
// the climb through it.
func AscentQBandFor(w *World, c *spacecraft.Spacecraft) (AscentQBand, bool) {
	if w == nil || c == nil || c.Primary.Atmosphere == nil {
		return AscentQBand{}, false
	}
	atm := c.Primary.Atmosphere
	return AscentQBand{
		AtmosphereDepthM: atm.CutoffAltitude,
		CurrentAltM:      c.Altitude(),
		CurrentQPa:       physics.DynamicPressure(c.State.R, c.State.V, c.Primary),
		MaxQPa:           w.LaunchMaxQ,
		MaxQAltM:         w.LaunchMaxQAltM,
		HasMaxQ:          w.LaunchMaxQ > 0,
	}, true
}

// AscentCue bundles the three ascent-half instruments issue #348 §3 asks
// for — nose-vs-prograde attitude, the predicted path ahead, and the
// atmosphere/Q band — behind ONE gate, mirroring DescentCorridor's shape
// so the surface view composes them the same way.
type AscentCue struct {
	ClimbRateMps float64
	Attitude     AttitudeVectors
	HasAttitude  bool
	Arc          AscentPath
	QBand        AscentQBand
	HasQBand     bool
}

// AscentCueFor gates the ascent cues on CLIMBING — the mirror image of
// DescentCorridorFor's falling gate (issue #348 §3 / ADR 0043). Climbing
// and falling are opposite signs of the same surface-relative radial
// rate (climbRateFloorMps / descentRateFloorMps share both magnitude and
// meaning), so the two instrument sets are naturally mutually exclusive
// by construction — climbRate ≥ floor forces the descent gate's
// descentRate (= −climbRate) below its own floor, and vice versa.
// Nothing here needs to consult DescentCorridorFor's result to avoid
// stacking on top of it.
func AscentCueFor(w *World, c *spacecraft.Spacecraft, horizon time.Duration) (AscentCue, bool) {
	if c == nil || c.Landed || c.Crashed {
		return AscentCue{}, false
	}
	mu := c.Primary.GravitationalParameter()
	if mu <= 0 || c.Primary.RadiusMeters() <= 0 {
		return AscentCue{}, false
	}
	rNorm := c.State.R.Norm()
	alt := c.Altitude()
	if rNorm <= 0 || alt <= 0 {
		return AscentCue{}, false
	}
	rHat := c.State.R.Scale(1 / rNorm)
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	climbRate := vRel.Dot(rHat)
	if !(climbRate >= climbRateFloorMps) {
		return AscentCue{}, false
	}

	attitude, hasAttitude := AttitudeVectorsFor(c)
	arc, _ := PredictAscentPath(c, horizon)
	qband, hasQBand := AscentQBandFor(w, c)

	return AscentCue{
		ClimbRateMps: climbRate,
		Attitude:     attitude,
		HasAttitude:  hasAttitude,
		Arc:          arc,
		QBand:        qband,
		HasQBand:     hasQBand,
	}, true
}
