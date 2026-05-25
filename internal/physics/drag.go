package physics

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// AtmosphericDensity returns ρ at altitude h above primary's surface
// using the body's exponential-density model:
// ρ(h) = ρ₀ · exp(-h / H) for h < cutoff, 0 above. Returns 0 when the
// primary has no atmosphere.
func AtmosphericDensity(primary bodies.CelestialBody, altitude float64) float64 {
	atm := primary.Atmosphere
	if atm == nil || atm.SurfaceDensity <= 0 || atm.ScaleHeight <= 0 {
		return 0
	}
	if altitude < 0 {
		// Defense in depth: callers should run ClampToSurface after
		// each step (v0.8.5) so a craft never persists below the
		// surface, but if a sub-step momentarily dips below before the
		// clamp, returning zero density keeps the drag term finite.
		// A real "crashed" state with destruction is deferred to v0.9+.
		return 0
	}
	if altitude >= atm.CutoffAltitude {
		return 0
	}
	return atm.SurfaceDensity * math.Exp(-altitude/atm.ScaleHeight)
}

// AtmosphereOmega returns the body's spin angular-velocity vector
// (rad/s) in the world inertial frame. Tilted along the body's
// physical spin axis per AxialTilt + AxialAzimuth, matching
// render.BodyRotationAxisWorld so the drag frame agrees with the
// integrator's surface-co-rotation frame and the renderer's body-
// fixed projection (ADR 0003). Returns zero when the body has no
// rotation period set.
//
// Period selection mirrors render.rotationPeriodSeconds: tidally-
// locked bodies use SideralOrbit, free bodies use SideralRotation
// (both stored as hours / days respectively per catalog convention).
//
// Pre-v0.11.2 this was a Z-aligned approximation ("the atmosphere
// co-rotates about world +Z"). The Z-aligned shortcut diverged from
// the integrator's tilted-axis surface velocity for any body with
// non-zero AxialTilt; ADR 0003 unifies the convention.
func AtmosphereOmega(primary bodies.CelestialBody) orbital.Vec3 {
	periodSec := atmospherePeriodSeconds(primary)
	if periodSec == 0 {
		return orbital.Vec3{}
	}
	mag := 2 * math.Pi / periodSec
	tiltRad := primary.AxialTilt * math.Pi / 180.0
	azRad := primary.AxialAzimuth * math.Pi / 180.0
	sinT := math.Sin(tiltRad)
	cosT := math.Cos(tiltRad)
	return orbital.Vec3{
		X: mag * sinT * math.Cos(azRad),
		Y: mag * sinT * math.Sin(azRad),
		Z: mag * cosT,
	}
}

// atmospherePeriodSeconds picks the period that drives the body's
// physical rotation: orbital period for tidally-locked moons,
// sidereal rotation period otherwise. Mirrors render.rotationPeriodSeconds
// so the two layers share the same convention.
func atmospherePeriodSeconds(primary bodies.CelestialBody) float64 {
	if primary.TidallyLocked {
		return primary.SideralOrbitSeconds()
	}
	return primary.SideralRotationSeconds()
}

// DragAccel returns the atmospheric-drag acceleration vector on a craft
// with state (r, v) relative to primary, given a ballistic coefficient
// BC = C_D · A / m (m²/kg). Returns zero outside the body's atmosphere
// (no Atmosphere defined, altitude above cutoff, or BC ≤ 0).
//
// Drag direction opposes the craft's velocity *relative to the
// atmosphere*: v_rel = v - ω × r where ω is the body's spin vector.
// That distinction matters for low-altitude flight — Earth's surface
// rotates ~465 m/s eastward at the equator, so a craft sitting at LEO
// orbital speed (~7800 m/s) feels drag against ~7335 m/s of relative
// flow, not the full inertial speed.
//
// Magnitude: a = 0.5 · ρ · |v_rel|² · BC, applied along -v̂_rel.
// v0.8.4+.
func DragAccel(r, v orbital.Vec3, primary bodies.CelestialBody, bc float64) orbital.Vec3 {
	if bc <= 0 || primary.Atmosphere == nil {
		return orbital.Vec3{}
	}
	rMag := r.Norm()
	if rMag == 0 {
		return orbital.Vec3{}
	}
	altitude := rMag - primary.RadiusMeters()
	rho := AtmosphericDensity(primary, altitude)
	if rho == 0 {
		return orbital.Vec3{}
	}
	vRel := v.Sub(AtmosphereOmega(primary).Cross(r))
	vRelMag := vRel.Norm()
	if vRelMag == 0 {
		return orbital.Vec3{}
	}
	// |a| = 0.5 ρ v² BC, direction = -v̂_rel.
	// Combine the unit-vector division and magnitude into one scale
	// factor: -(0.5 · ρ · v · BC) so we multiply by vRel directly.
	factor := -0.5 * rho * vRelMag * bc
	return vRel.Scale(factor)
}
