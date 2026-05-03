package render

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// rotationEpoch is the reference instant for sub-observer-longitude
// math. Picked as J2000 (2000-01-01 12:00:00 TT, ≈ UTC for sim
// purposes) so the same secondsSinceEpoch math works across saves
// and so per-body epoch offsets express "where the prime meridian
// sat at J2000" — a stable astronomical convention rather than
// sim-time-zero (which drifts as the player picks new start dates).
var rotationEpoch = time.Date(2000, time.January, 1, 12, 0, 0, 0, time.UTC)

// bodyEpochOffsetDeg is the per-body sub-observer longitude at
// rotationEpoch. Picked so common bodies render with their iconic
// face at sim-time-zero of the default save (matching v0.7.6 /
// v0.8.4 static center-longitude conventions). Bodies missing here
// default to 0 — the disk still spins, just from an arbitrary
// phase.
var bodyEpochOffsetDeg = map[string]float64{
	"earth":   -30.0, // Americas + Atlantic + W. Europe + Africa visible
	"mars":    -45.0, // prime meridian centered, Syrtis Major on right limb
	"jupiter": 25.0,  // Great Red Spot visible
	"saturn":  0.0,   // banded; polar hexagon at ~78°N regardless of lon0
	"neptune": 30.0,  // Great Dark Spot near visible center at epoch
	"uranus":  0.0,   // featureless banding; offset doesn't matter much
	// Moon (and other tidally-locked bodies) use 0 — for tidally-
	// locked rendering the visible face is dictated by orbital
	// phase, so a baseline 0 keeps "near side facing parent" at
	// epoch.
}

// SubObserverLongitudeDeg returns the longitude of the body that
// sits at the visible disk center at simTime, in degrees in
// (-180, 180]. Free bodies advance at 360°/SideralRotationSeconds;
// tidally-locked bodies advance at 360°/SideralOrbitSeconds (same
// face faces the parent, so the heliocentric viewer sees the
// face rotate at the orbital rate). v0.8.5+.
//
// Bodies missing both periods return their epoch offset unchanged
// (effectively "doesn't spin") — consistent with how Pluto-class
// stubs rendered before this slice landed.
func SubObserverLongitudeDeg(b bodies.CelestialBody, simTime time.Time) float64 {
	offset := bodyEpochOffsetDeg[b.ID]
	periodSec := rotationPeriodSeconds(b)
	if periodSec == 0 {
		return wrapDeg180(offset)
	}
	dt := simTime.Sub(rotationEpoch).Seconds()
	// SideralRotation is signed (negative = retrograde, e.g. Venus).
	// Positive period → eastward rotation → sub-observer longitude
	// decreases over time (the surface moves east under the camera).
	return wrapDeg180(offset - 360.0*dt/periodSec)
}

// rotationPeriodSeconds picks the period that drives the body's
// visible face: orbital period for tidally-locked moons, sidereal
// rotation period otherwise. Returns 0 when neither is set.
func rotationPeriodSeconds(b bodies.CelestialBody) float64 {
	if b.TidallyLocked {
		return b.SideralOrbitSeconds()
	}
	return b.SideralRotationSeconds()
}

// wrapDeg180 wraps a longitude into (-180, 180] using the same
// convention as the per-body texture tables. Stable across very
// large positive or negative inputs (high-warp accumulation).
func wrapDeg180(deg float64) float64 {
	deg = math.Mod(deg, 360.0)
	if deg > 180 {
		deg -= 360
	} else if deg <= -180 {
		deg += 360
	}
	return deg
}
