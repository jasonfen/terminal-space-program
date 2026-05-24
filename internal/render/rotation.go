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
// rotationEpoch, with respect to the body's own prime meridian.
// Picked so common bodies render with their iconic face when
// viewed from the body's front (camera in the equatorial plane
// at body x-axis direction). Bodies missing here default to 0.
//
// v0.8.5.7+: this is now an offset on body-fixed longitude, not on
// the historical "always equator-on" sub-observer point. View-aware
// projection composes this with the camera direction to compute
// the actual sub-observer (lat, lon) per render.
var bodyEpochOffsetDeg = map[string]float64{
	"earth":   -30.0, // Americas + Atlantic + W. Europe + Africa visible
	"mars":    -45.0, // prime meridian centered, Syrtis Major on right limb
	"jupiter": 25.0,  // Great Red Spot visible
	"saturn":  0.0,   // banded; polar hexagon at ~78°N regardless of lon0
	"neptune": 30.0,  // Great Dark Spot near visible center at epoch
	"uranus":  0.0,   // featureless banding; offset doesn't matter much
	"sun":     0.0,   // sunspot layout doesn't pick a special "iconic" face
}

// Vec3 is the render package's local 3-element inertial-frame
// vector. Defined here rather than imported from orbital to keep
// the render package free of physics-package dependencies (render
// stays a pure leaf). Same convention as orbital.Vec3.
type Vec3 struct{ X, Y, Z float64 }

// CameraDirTop is the world-frame body-to-camera direction for
// the canvas's "top" view (looking down system Z-axis from +Z).
// Defined here so the tui orbit screen can hand the texture
// pipeline a stable camera direction without round-tripping
// through sim.ViewMode and avoiding an import cycle.
var (
	CameraDirTop    = Vec3{0, 0, +1}
	CameraDirBottom = Vec3{0, 0, -1}
	CameraDirRight  = Vec3{+1, 0, 0}
	CameraDirLeft   = Vec3{-1, 0, 0}
)

// SubObserverPointDeg returns the (lat, lon) on body b — in body-
// fixed degrees — that sits at the visible disk center for an
// observer at the world-frame direction camDir, at simTime.
//
// camDir is the unit vector pointing from the body to the camera
// in the world inertial frame; the canvas's "top" view passes
// CameraDirTop, etc. The orbit-flat view passes the active
// orbit-plane normal.
//
// primMerDir is an optional override for the body's prime
// meridian direction in the world frame. When non-zero, it
// supersedes the rotation-phase model — used for tidally-locked
// moons, where the prime meridian (lon=0) always points at the
// parent body. Caller passes the unit vector from the moon
// toward its parent at simTime; the function projects this onto
// the equatorial plane to get the body x-axis. When primMerDir
// is the zero vector, the function falls back to the inertial-
// frame rotation-phase model (correct for free bodies and for
// tidally-locked bodies whose parent isn't tracked here).
//
// The body's spin axis is modelled as lying in the world X-Z
// plane (azimuth 0 in the inertial frame): n = (sin(tilt), 0,
// cos(tilt)). v0.8.5.7+ replaces the v0.8.5 "always equator-on"
// lon0 formulation with full view-aware geometry. ViewTop on a
// tilted body now reveals polar regions; Uranus's 97° tilt makes
// it roll pole-on along its orbit; tidally-locked moons keep the
// same face pointed at the parent regardless of orbit phase.
func SubObserverPointDeg(b bodies.CelestialBody, simTime time.Time, camDir Vec3, primMerDir Vec3) (subLatDeg, subLonDeg float64) {
	camDir = normalize(camDir)
	// Body axis in world frame (picks up AxialTilt + AxialAzimuth).
	n := BodyRotationAxisWorld(b)

	// subLat: angle between camera direction and equatorial plane,
	// signed positive when camera is on the body's northern side.
	cz := dot(camDir, n)
	if cz > 1 {
		cz = 1
	} else if cz < -1 {
		cz = -1
	}
	subLatDeg = math.Asin(cz) * 180.0 / math.Pi

	// Body x-axis (lon=0 meridian) in world frame at simTime.
	// Two paths:
	//   1. Caller-supplied primMerDir → project onto equatorial
	//      plane (the supplied direction may have a component along
	//      the spin axis we need to discard). This is the correct
	//      model for tidally-locked bodies — primMerDir comes from
	//      the moon → parent direction, so the lon=0 meridian
	//      always points at the parent.
	//   2. Default → rotate the body's reference x-axis (projection
	//      of world +X onto the equatorial plane, fallback +Y for
	//      degenerate cases) by the rotation phase since
	//      rotationEpoch. Correct for free bodies; defensible (but
	//      inertial-frame-fixed) for tidally-locked bodies without
	//      parent info.
	var xt Vec3
	if primMerDirNonzero(primMerDir) {
		dirN := normalize(primMerDir)
		proj := add(dirN, scale(n, -dot(dirN, n)))
		if dot(proj, proj) < 1e-12 {
			xt = bodyXAxisAtPhase(b, n, rotationPhaseRad(b, simTime))
		} else {
			xt = normalize(proj)
		}
	} else {
		xt = bodyXAxisAtPhase(b, n, rotationPhaseRad(b, simTime))
	}
	yt := cross(n, xt)

	// Sub-observer longitude is the angle between the camera
	// direction's projection onto the equatorial plane and the
	// body x-axis at time t.
	cx := dot(camDir, xt)
	cy := dot(camDir, yt)
	// v0.8.6+: pole-on degeneracy guard (defensive). When camDir is
	// exactly parallel to the spin axis — e.g. orbit-flat view over
	// a body-equatorial orbit if camDir lands on n with zero float
	// residue — cx and cy are both 0 and Go's atan2(0, 0) returns 0.
	// subLon would then be a constant (epoch offset only) and the
	// polar texture would show no rotation at all. Substitute a
	// phase-driven fallback in that exact-pole-on case so the
	// rendered polar view rotates with the body's spin period.
	//
	// In practice the orbit-flat case rarely lands on exact zero —
	// camDir comes from PerifocalBasis cross products with ε-level
	// residue, and once ElementsFromState's circular-orbit ω-snap
	// (kepler.go circularTol) keeps that residue stable per frame,
	// atan2(ε·cos(φ−γ), ε'·cos(φ−γ')) is itself smooth in φ. So this
	// guard is belt-and-braces — the actual fix for the
	// user-reported orbit-flat jitter is the ω-snap. Kept because
	// future code paths (e.g. an explicit polar-view mode) might
	// hand us a camDir that *is* exactly the spin axis.
	const poleOnTol = 1e-6 // |equatorial projection|² threshold
	if cx*cx+cy*cy < poleOnTol {
		// Fallback: project the body x-axis at epoch (x0) onto the
		// rotated equatorial basis (xt, yt). dot(x0, xt) = cos(phase),
		// dot(x0, yt) = -sin(phase), so atan2 returns -phase —
		// linearly advancing with simTime.
		x0, _ := BodyRingBasisWorld(b)
		cx = dot(x0, xt)
		cy = dot(x0, yt)
	}
	subLonDeg = math.Atan2(cy, cx) * 180.0 / math.Pi

	// Apply per-body epoch offset (post-rotation) so iconic features
	// land at the visible centre for the canonical reference view.
	subLonDeg = wrapDeg180(subLonDeg + bodyEpochOffsetDeg[b.ID])
	return subLatDeg, subLonDeg
}

// bodyXAxisAtPhase returns the body x-axis (lon=0 meridian) in
// world frame for a given rotation phase θ — used when the caller
// hasn't supplied a primary-meridian direction. The reference
// x-axis at θ=0 is the projection of world +X onto the equatorial
// plane (or +Y when the axis is too close to +X to safely project
// from). It rotates counterclockwise around n at the body's
// sidereal rate (or orbital rate for tidally-locked).
func bodyXAxisAtPhase(b bodies.CelestialBody, n Vec3, phase float64) Vec3 {
	x0, _ := BodyRingBasisWorld(b)
	y0 := cross(n, x0)
	sP := math.Sin(phase)
	cP := math.Cos(phase)
	return add(scale(x0, cP), scale(y0, sP))
}

// primMerDirNonzero reports whether the supplied direction has
// non-trivial magnitude. Helper for the optional-arg pattern.
func primMerDirNonzero(v Vec3) bool {
	return v.X*v.X+v.Y*v.Y+v.Z*v.Z > 1e-12
}

// SubObserverLongitudeDeg is the v0.8.5 entry point — lon at the
// visible centre for an equator-on view. Retained as a thin wrapper
// over SubObserverPointDeg(camDir = CameraDirRight) so callers that
// only care about longitude (tests, simple debug paths) keep
// working. New view-aware code should call SubObserverPointDeg.
func SubObserverLongitudeDeg(b bodies.CelestialBody, simTime time.Time) float64 {
	_, lon := SubObserverPointDeg(b, simTime, CameraDirRight, Vec3{})
	return lon
}

// BodyRotationAxisWorld returns the body's spin axis as a unit
// vector in the world inertial frame, derived from AxialTilt and
// AxialAzimuth. The axis is
//
//	n = (sin(tilt)·cos(azimuth), sin(tilt)·sin(azimuth), cos(tilt))
//
// where tilt is the obliquity to the world Z-axis (orbital-plane
// normal) and azimuth is the longitude of the axis's projection
// onto the X-Y plane. With AxialAzimuth = 0 (the default for all
// bodies populated through v0.8.5.7) this collapses to the
// X-Z-plane convention `(sin(tilt), 0, cos(tilt))` the earlier
// v0.8.5.7 work used.
func BodyRotationAxisWorld(b bodies.CelestialBody) Vec3 {
	tiltRad := b.AxialTilt * math.Pi / 180.0
	azRad := b.AxialAzimuth * math.Pi / 180.0
	sinT := math.Sin(tiltRad)
	cosT := math.Cos(tiltRad)
	return Vec3{sinT * math.Cos(azRad), sinT * math.Sin(azRad), cosT}
}

// BodyRingBasisWorld returns two orthonormal basis vectors that
// span the body's equatorial plane (perpendicular to its spin
// axis), expressed in the world inertial frame. Caller can sample
// a circular ring as
//
//	ringPoint(θ) = R·(ê1·cos(θ) + ê2·sin(θ))
//
// and project each sample through the canvas to draw the ring as
// an ellipse that correctly foreshortens for the current view.
// Built directly from the spin-axis vector via Gram-Schmidt
// against world +X (or +Y when degenerate), so it picks up
// AxialAzimuth automatically. At AxialTilt = 90° aligned with
// world +X the convention degenerates and we fall back to
// (ê1, ê2) = (world +Y, world +Z).
func BodyRingBasisWorld(b bodies.CelestialBody) (Vec3, Vec3) {
	n := BodyRotationAxisWorld(b)
	// Pick a reference vector that's not parallel to n — world +X
	// works for almost every body; fall back to world +Y when the
	// spin axis is too close to +X (high-tilt + azimuth-0 bodies).
	ref := Vec3{1, 0, 0}
	if math.Abs(dot(n, ref)) > 0.999 {
		ref = Vec3{0, 1, 0}
	}
	// Gram-Schmidt: project ref onto equatorial plane and normalise.
	e1 := add(ref, scale(n, -dot(n, ref)))
	if dot(e1, e1) < 1e-12 {
		// Catastrophically degenerate (shouldn't happen given the
		// fallback above) — emit a sensible default so the ring
		// rendering doesn't crash on a NaN.
		return Vec3{0, 1, 0}, Vec3{0, 0, 1}
	}
	e1 = normalize(e1)
	e2 := cross(n, e1)
	return e1, e2
}

// rotationPhaseRad returns the body's spin angle (radians) at
// simTime, signed for prograde (+) / retrograde (-) rotation.
// Tidally-locked bodies use their orbital period.
func rotationPhaseRad(b bodies.CelestialBody, simTime time.Time) float64 {
	period := rotationPeriodSeconds(b)
	if period == 0 {
		return 0
	}
	dt := simTime.Sub(rotationEpoch).Seconds()
	return 2 * math.Pi * dt / period
}

// BodyFixedToWorld converts body-fixed (lat, lon) to a unit
// world-frame vector that the texture pipeline renders AT the same
// canvas pixel where the body's (lat, lon) cell is drawn. v0.9.2+
// (fix-4 in the v0.9.2 playtest cycle).
//
// Implementation note: this uses Snyder forward orthographic
// projection (the **inverse** of the texture pipeline's
// projectPixelToLatLon) at ViewTop's sub-observer point, then maps
// the resulting (nx, ny, z) camera-frame coords into world frame
// using ViewTop's canvas basis (canvas_X = world+X, canvas_Y =
// world+Y, canvas_depth = world+Z).
//
// Why ViewTop specifically: the v0.8.5+ texture pipeline's
// orthographic projection is internally self-consistent per view
// but cross-view inconsistent — Snyder forward at ViewTop's
// sub-observer point and at ViewRight's sub-observer point project
// the same body-fixed (lat, lon) to different world positions.
// ViewTop is the natural "looking down at Earth" view and the
// player's default; aligning spawn there agrees with the most
// common case. A proper renderer fix (apply screen rotation so
// the texture's east/north axes match canvas X/Y) is a v0.9.5+
// scope; documented in the v0.9.2 fix-4 commit body.
//
// Latitude in degrees north positive. Longitude in degrees east
// positive (real-Earth-style). Multiply the returned unit vector
// by primary radius for surface position.
func BodyFixedToWorld(b bodies.CelestialBody, latDeg, lonDeg float64, simTime time.Time) Vec3 {
	subLatDeg, subLonDeg := SubObserverPointDeg(b, simTime, CameraDirTop, Vec3{})

	phi := latDeg * math.Pi / 180.0
	lam := lonDeg * math.Pi / 180.0
	phi0 := subLatDeg * math.Pi / 180.0
	lam0 := subLonDeg * math.Pi / 180.0

	sP, cP := math.Sin(phi), math.Cos(phi)
	sP0, cP0 := math.Sin(phi0), math.Cos(phi0)
	dlam := lam - lam0
	sL, cL := math.Sin(dlam), math.Cos(dlam)

	// Snyder forward orthographic at sub-observer (φ₀, λ₀):
	//   nx = cos(φ)·sin(λ−λ₀)         (east at sub-observer)
	//   ny = cos(φ₀)·sin(φ) − sin(φ₀)·cos(φ)·cos(λ−λ₀)   (north)
	//   z  = sin(φ₀)·sin(φ) + cos(φ₀)·cos(φ)·cos(λ−λ₀)   (toward camera)
	// For ViewTop the camera frame's (east, north, toward-camera)
	// axes equal the canvas's (X, Y, depth) = world (+X, +Y, +Z).
	nx := cP * sL
	ny := cP0*sP - sP0*cP*cL
	z := sP0*sP + cP0*cP*cL
	return Vec3{X: nx, Y: ny, Z: z}
}

// WorldToBodyFixed is the inverse of BodyFixedToWorld: given a unit
// world-frame vector representing a point on (or above) the body at
// simTime, recover the body-fixed (lat, lon) of that direction.
// v0.11.0+. Used by the ViewLaunch trail sampler to convert the
// active craft's primary-relative direction into a geographic
// (lat, lon) sample.
//
// Caller is responsible for normalising the input (only the direction
// matters) and for computing altitude separately as |R| - mean radius.
//
// Algebraic derivation: BodyFixedToWorld writes
//   nx = cos(φ)·sin(λ−λ₀)
//   ny = cos(φ₀)·sin(φ) − sin(φ₀)·cos(φ)·cos(λ−λ₀)
//   z  = sin(φ₀)·sin(φ) + cos(φ₀)·cos(φ)·cos(λ−λ₀)
// Combining the ny + z system: cos(φ₀)·ny + sin(φ₀)·z = sin(φ),
// giving φ directly. Then cos(φ)·cos(λ−λ₀) = (z − sin(φ)·sin(φ₀))
// / cos(φ₀) and cos(φ)·sin(λ−λ₀) = nx, so λ−λ₀ = atan2(...).
//
// Output longitude is wrapped to (-180, 180].
func WorldToBodyFixed(b bodies.CelestialBody, vWorld Vec3, simTime time.Time) (latDeg, lonDeg float64) {
	subLatDeg, subLonDeg := SubObserverPointDeg(b, simTime, CameraDirTop, Vec3{})
	phi0 := subLatDeg * math.Pi / 180.0
	lam0 := subLonDeg * math.Pi / 180.0
	sP0, cP0 := math.Sin(phi0), math.Cos(phi0)

	nx, ny, z := vWorld.X, vWorld.Y, vWorld.Z

	// sin(φ) = sin(φ₀)·z + cos(φ₀)·ny  (clamped for floating-point
	// drift past ±1 on near-degenerate inputs).
	sP := sP0*z + cP0*ny
	if sP > 1 {
		sP = 1
	} else if sP < -1 {
		sP = -1
	}
	phi := math.Asin(sP)

	// λ − λ₀ from atan2(cos(φ)·sin(λ−λ₀), cos(φ)·cos(λ−λ₀)). Substitute:
	//   numerator   = nx (= cos(φ)·sin(λ−λ₀))
	//   denominator = (z − sin(φ)·sin(φ₀)) / cos(φ₀)
	// At cos(φ₀) ≈ 0 (pole-on view) the denominator form collapses;
	// guard with a tiny-cP0 fallback using ny directly.
	var dlam float64
	if math.Abs(cP0) > 1e-12 {
		dlam = math.Atan2(nx, (z-sP*sP0)/cP0)
	} else {
		// Pole-on observer: ny = sP0·sP (drops out), so use the raw
		// nx/(-ny·sign) couple from the original system. With cP0=0,
		// sP0=±1: nx = cos(φ)·sin(λ−λ₀), z = sP0·sP — z is fully
		// determined by φ. Use ny = -sP0·cos(φ)·cos(λ−λ₀).
		dlam = math.Atan2(nx, -sP0*ny)
	}
	lam := lam0 + dlam

	latDeg = phi * 180.0 / math.Pi
	lonDeg = lam * 180.0 / math.Pi
	// Wrap to (-180, 180].
	lonDeg = math.Mod(lonDeg+540, 360) - 180
	return latDeg, lonDeg
}

// BodySpinOmegaWorld returns the body's spin angular-velocity
// vector in the world frame: ω = (2π/period) · n_hat. Direction
// matches BodyRotationAxisWorld (tilted, picks up AxialTilt +
// AxialAzimuth). Returns the zero vector when the body has no
// rotation period set. v0.9.2+. Used by sim's launchpad spawn
// (surface co-rotation velocity = ω × r) and landed-craft
// integration (rotates R about the tilted axis per tick). Differs
// from physics.AtmosphereOmega which is Z-aligned for the drag
// approximation; the launchpad path uses the tilted ω because it
// has to agree with the texture renderer's frame.
func BodySpinOmegaWorld(b bodies.CelestialBody) Vec3 {
	period := rotationPeriodSeconds(b)
	if period == 0 {
		return Vec3{}
	}
	mag := 2 * math.Pi / period
	n := BodyRotationAxisWorld(b)
	return Vec3{X: mag * n.X, Y: mag * n.Y, Z: mag * n.Z}
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

// Tiny vector helpers — kept package-local so the render package
// stays free of orbital imports.

func dot(a, b Vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func add(a, b Vec3) Vec3    { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func scale(a Vec3, s float64) Vec3 {
	return Vec3{a.X * s, a.Y * s, a.Z * s}
}
func cross(a, b Vec3) Vec3 {
	return Vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}
func normalize(v Vec3) Vec3 {
	n := math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
	if n < 1e-12 {
		return Vec3{0, 0, 1}
	}
	return Vec3{v.X / n, v.Y / n, v.Z / n}
}
