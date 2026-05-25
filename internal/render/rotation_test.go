package render

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestSubObserverLongitudeAtEpochReturnsBodyOffset(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", SideralRotation: 23.9345}
	got := SubObserverLongitudeDeg(earth, rotationEpoch)
	if math.Abs(got-EarthCenterLonEpoch) > 1e-9 {
		t.Errorf("Earth at epoch = %v, want %v", got, EarthCenterLonEpoch)
	}

	mars := bodies.CelestialBody{ID: "mars", SideralRotation: 24.6229}
	got = SubObserverLongitudeDeg(mars, rotationEpoch)
	if math.Abs(got-MarsCenterLonEpoch) > 1e-9 {
		t.Errorf("Mars at epoch = %v, want %v", got, MarsCenterLonEpoch)
	}
}

func TestSubObserverLongitudeFreeBodyAdvancesEastward(t *testing.T) {
	// A surface point's longitude under the camera decreases as the
	// planet rotates eastward (positive sidereal period). After half
	// a sidereal day Earth should have rotated 180°.
	earth := bodies.CelestialBody{ID: "earth", SideralRotation: 24.0}
	halfDay := rotationEpoch.Add(12 * time.Hour)
	got := SubObserverLongitudeDeg(earth, halfDay)
	want := wrapDeg180(EarthCenterLonEpoch - 180)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("Earth at epoch+½day = %v, want %v", got, want)
	}
}

func TestSubObserverLongitudeRetrogradeBody(t *testing.T) {
	// Venus's sidereal rotation is negative (retrograde). A
	// retrograde body's surface moves west, so the sub-observer
	// longitude should *increase* with time (relative to epoch).
	venus := bodies.CelestialBody{ID: "venus", SideralRotation: -5832.6}
	// Tiny advance to keep the math obvious — at t=1000 s a -5832.6h
	// period rotates by 360 * 1000 / (-5832.6 * 3600) ≈ -0.01715°,
	// so sub-observer = 0 - that = +0.01715°.
	got := SubObserverLongitudeDeg(venus, rotationEpoch.Add(1000*time.Second))
	want := -360.0 * 1000.0 / (-5832.6 * 3600.0)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("Venus at epoch+1000s = %v, want %v", got, want)
	}
}

func TestSubObserverLongitudeTidallyLockedUsesOrbit(t *testing.T) {
	// A tidally-locked moon ignores SideralRotation (which equals
	// SideralOrbit anyway, but the renderer shouldn't depend on
	// that) and rotates at the orbital rate.
	luna := bodies.CelestialBody{
		ID:              "moon",
		SideralRotation: 9999.0, // junk — must be ignored
		SideralOrbit:    27.321661,
		TidallyLocked:   true,
	}
	// After half an orbit, the visible face should have rotated 180°.
	halfOrbit := rotationEpoch.Add(time.Duration(27.321661 / 2 * 86400 * float64(time.Second)))
	got := SubObserverLongitudeDeg(luna, halfOrbit)
	want := wrapDeg180(0 - 180) // Moon's epoch offset is 0
	if math.Abs(got-want) > 1e-3 {
		t.Errorf("Luna at epoch+½orbit = %v, want %v", got, want)
	}
}

func TestSubObserverLongitudeNoPeriodReturnsOffset(t *testing.T) {
	// A body with no rotation period set (dwarf planet stub, etc.)
	// holds at its epoch offset — better than dividing by zero.
	pluto := bodies.CelestialBody{ID: "pluto"}
	got := SubObserverLongitudeDeg(pluto, rotationEpoch.Add(365*24*time.Hour))
	if got != 0 {
		t.Errorf("no-period body advanced from offset: got %v, want 0", got)
	}
}

func TestSubObserverLongitudeWrapsToHalfOpenInterval(t *testing.T) {
	// After many rotations the result must still land in (-180, 180].
	earth := bodies.CelestialBody{ID: "earth", SideralRotation: 23.9345}
	tenYears := rotationEpoch.Add(10 * 365 * 24 * time.Hour)
	got := SubObserverLongitudeDeg(earth, tenYears)
	if got <= -180 || got > 180 {
		t.Errorf("after 10 years, sub-observer = %v, out of (-180, 180]", got)
	}
}

// View-aware (v0.8.5.7+) tests below — verify that camera direction
// + axial tilt produce the expected sub-observer (lat, lon) for each
// canonical view. Pre-v0.8.5.7 SubObserverLongitudeDeg tests above
// stay green because that function now wraps SubObserverPointDeg
// with a fixed CameraDirRight.

func TestSubObserverPointTopViewNoTiltIsPolar(t *testing.T) {
	// Untilted body viewed from "top" (camera at +Z) — sub-observer
	// is at the body's north pole, lat = 90°.
	b := bodies.CelestialBody{ID: "x", SideralRotation: 24.0, AxialTilt: 0}
	subLat, _ := SubObserverPointDeg(b, rotationEpoch, CameraDirTop, Vec3{})
	if math.Abs(subLat-90) > 1e-6 {
		t.Errorf("ViewTop on tilt=0 body: subLat = %v, want 90", subLat)
	}
}

func TestSubObserverPointSideViewNoTiltIsEquator(t *testing.T) {
	// Untilted body viewed from "right" (camera at +X) — sub-observer
	// is on the equator, lat = 0°.
	b := bodies.CelestialBody{ID: "x", SideralRotation: 24.0, AxialTilt: 0}
	subLat, _ := SubObserverPointDeg(b, rotationEpoch, CameraDirRight, Vec3{})
	if math.Abs(subLat) > 1e-6 {
		t.Errorf("ViewRight on tilt=0 body: subLat = %v, want 0", subLat)
	}
}

func TestSubObserverPointEarthTopShowsArctic(t *testing.T) {
	// Earth has 23.44° axial tilt. ViewTop sees the body axis
	// projecting up out of the orbital plane — sub-observer lat ≈
	// 90 - 23.44 = 66.56°. (Inside the Arctic Circle by a hair.)
	earth := bodies.CelestialBody{ID: "earth", SideralRotation: 24.0, AxialTilt: 23.44}
	subLat, _ := SubObserverPointDeg(earth, rotationEpoch, CameraDirTop, Vec3{})
	want := 90 - 23.44
	if math.Abs(subLat-want) > 1e-3 {
		t.Errorf("ViewTop on Earth: subLat = %v, want %v", subLat, want)
	}
}

// TestSubObserverPointPoleOnViewSubLonAdvancesMonotonically: when the
// camera direction equals the body spin axis (orbit-flat view over a
// body-equatorial orbit), cx² + cy² is mathematically zero — the
// pole-on guard kicks in and substitutes a stable phase-driven
// fallback so subLon advances monotonically with simTime instead of
// jittering on atan2(noise, noise). v0.8.6+ regression for the orbit-
// flat low-warp jitter bug.
func TestSubObserverPointPoleOnViewSubLonAdvancesMonotonically(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", SideralRotation: 23.9345, AxialTilt: 23.44}
	// Camera direction = body spin axis (the worst-case orbit-flat
	// view for the jitter bug).
	n := BodyRotationAxisWorld(earth)
	camDir := Vec3{X: n.X, Y: n.Y, Z: n.Z}
	// Sample subLon every 30 sim-minutes for 4 hours; verify each
	// step is in (0, 360°) modulo wrapping (i.e. unwrapped angle is
	// strictly increasing for prograde rotation, allowing the
	// natural ±180° boundary jump).
	const stepMin = 30
	const steps = 8
	prev := math.NaN()
	cumulative := 0.0
	for i := 0; i < steps; i++ {
		t0 := rotationEpoch.Add(time.Duration(i*stepMin) * time.Minute)
		_, lon := SubObserverPointDeg(earth, t0, camDir, Vec3{})
		if !math.IsNaN(prev) {
			d := lon - prev
			for d > 180 {
				d -= 360
			}
			for d <= -180 {
				d += 360
			}
			// Earth rotates eastward → sub-observer lon decreases at
			// ~360°/24h. Per 30 min: ~7.5°. Allow some slack for the
			// fallback's exact phase definition; the key invariant is
			// strict monotonicity (no back-and-forth jerks).
			if d >= 0 {
				t.Errorf("step %d: subLon Δ = %.3f° (expected negative for prograde rotation, no jitter)", i, d)
			}
			cumulative += d
		}
		prev = lon
	}
	// 4 hours of Earth rotation ≈ -60° cumulative.
	if cumulative > -45 || cumulative < -75 {
		t.Errorf("4 h cumulative subLon Δ = %.2f°, want ≈ -60°", cumulative)
	}
}

func TestSubObserverPointUranusRollsAlongOrbit(t *testing.T) {
	// Uranus's 97.77° tilt makes it roll pole-on along its orbit.
	// ViewTop (camera at +Z) sees the body axis nearly in the
	// orbital plane — sub-observer lat is small.
	// ViewRight (camera at +X) sees the body axis almost pointing
	// at the camera — sub-observer lat is large (near pole).
	uranus := bodies.CelestialBody{ID: "uranus", SideralRotation: -17.24, AxialTilt: 97.77}
	subLatTop, _ := SubObserverPointDeg(uranus, rotationEpoch, CameraDirTop, Vec3{})
	subLatRight, _ := SubObserverPointDeg(uranus, rotationEpoch, CameraDirRight, Vec3{})
	if math.Abs(subLatTop) >= math.Abs(subLatRight) {
		t.Errorf("Uranus rolls: ViewTop |lat| (%v) should be smaller than ViewRight |lat| (%v)",
			subLatTop, subLatRight)
	}
	if math.Abs(subLatRight) < 80 {
		t.Errorf("Uranus ViewRight: subLat = %v, want > 80° (near-pole)", subLatRight)
	}
	if math.Abs(subLatTop) > 10 {
		t.Errorf("Uranus ViewTop: subLat = %v, want |lat| < 10° (near-equator)", subLatTop)
	}
}

func TestSubObserverPointTidallyLockedTracksParent(t *testing.T) {
	// Tidally-locked moon with a primMerDir override should keep the
	// near-side meridian (lon=0) pointed at the parent regardless of
	// orbit phase. Concretely: as the moon orbits the parent, the
	// camera at a fixed direction sees subLon shift by the angle
	// between (camDir) and (moon→parent direction) projected on the
	// equatorial plane, NOT by the rotation phase from epoch.
	luna := bodies.CelestialBody{
		ID:            "moon",
		SideralOrbit:  27.321661,
		TidallyLocked: true,
		AxialTilt:     0,
	}
	cam := CameraDirRight // (+1, 0, 0)

	// Case A: parent is at +X direction from moon (moon is at -X
	// from parent). Camera at +X is pointed at the moon's near
	// side → subLon = 0.
	parentAtPlusX := Vec3{1, 0, 0}
	_, lonA := SubObserverPointDeg(luna, rotationEpoch, cam, parentAtPlusX)
	if math.Abs(lonA) > 1e-6 {
		t.Errorf("near-side facing camera: subLon = %v, want 0", lonA)
	}

	// Case B: parent is at +Y direction from moon. The near-side
	// meridian now points at +Y. Camera at +X is 90° from the
	// near-side meridian → subLon = -90° (camera sees lon=-90
	// because the body x-axis is at +Y and the camera is at +X,
	// which is one quarter clockwise from +Y in the equatorial
	// plane).
	parentAtPlusY := Vec3{0, 1, 0}
	_, lonB := SubObserverPointDeg(luna, rotationEpoch, cam, parentAtPlusY)
	if math.Abs(lonB-(-90)) > 1e-6 {
		t.Errorf("parent at +Y, camera at +X: subLon = %v, want -90", lonB)
	}

	// Case C: parent at -X — moon's near side faces away from
	// camera at +X → subLon = ±180.
	parentAtMinusX := Vec3{-1, 0, 0}
	_, lonC := SubObserverPointDeg(luna, rotationEpoch, cam, parentAtMinusX)
	if math.Abs(math.Abs(lonC)-180) > 1e-6 {
		t.Errorf("far-side facing camera: subLon = %v, want ±180", lonC)
	}
}

func TestSubObserverPointZeroPrimMerFallsBackToPhase(t *testing.T) {
	// Passing the zero vector for primMerDir must reproduce the
	// earlier rotation-phase behaviour exactly — protects callers
	// that don't supply a parent direction (free bodies, or
	// tidally-locked bodies with no parent metadata).
	earth := bodies.CelestialBody{ID: "earth", SideralRotation: 24.0}
	t1 := rotationEpoch.Add(6 * time.Hour) // ¼ day → ~90° spin
	_, lonOverride := SubObserverPointDeg(earth, t1, CameraDirRight, Vec3{})
	lonLegacy := SubObserverLongitudeDeg(earth, t1)
	if math.Abs(lonOverride-lonLegacy) > 1e-9 {
		t.Errorf("primMerDir=zero diverges from phase model: %v vs %v",
			lonOverride, lonLegacy)
	}
}

func TestBodyAxisAzimuthRotatesAxisAroundZ(t *testing.T) {
	// Same tilt at azimuth 0° and 90° gives spin axes that share
	// their Z component but lie along world +X vs world +Y.
	bX := bodies.CelestialBody{ID: "x", AxialTilt: 30, AxialAzimuth: 0}
	bY := bodies.CelestialBody{ID: "x", AxialTilt: 30, AxialAzimuth: 90}
	nX := BodyRotationAxisWorld(bX)
	nY := BodyRotationAxisWorld(bY)
	if math.Abs(nX.Z-nY.Z) > 1e-9 {
		t.Errorf("Z components diverged across azimuth: %v vs %v", nX.Z, nY.Z)
	}
	if math.Abs(nX.X-math.Sin(30*math.Pi/180)) > 1e-9 {
		t.Errorf("azimuth 0 → axis.X = %v, want sin(30°)", nX.X)
	}
	if math.Abs(nX.Y) > 1e-9 {
		t.Errorf("azimuth 0 → axis.Y = %v, want 0", nX.Y)
	}
	if math.Abs(nY.X) > 1e-9 {
		t.Errorf("azimuth 90 → axis.X = %v, want 0", nY.X)
	}
	if math.Abs(nY.Y-math.Sin(30*math.Pi/180)) > 1e-9 {
		t.Errorf("azimuth 90 → axis.Y = %v, want sin(30°)", nY.Y)
	}
}

func TestSubObserverPointAzimuthShiftsViewSide(t *testing.T) {
	// Tilt 30°, azimuth 0° (axis tips toward +X) viewed from +Z:
	// subLat ≈ 60° (= 90° - 30°). Same tilt, azimuth 90° (axis tips
	// toward +Y), still viewed from +Z: same subLat — both are
	// equally polar from the top because the axis Z component is
	// the same. But viewed from +X, azimuth 0 sees ~equator while
	// azimuth 90 sees the axis edge-on (subLat closer to 0 too).
	tilt := 30.0
	bX := bodies.CelestialBody{ID: "x", AxialTilt: tilt, AxialAzimuth: 0}
	bY := bodies.CelestialBody{ID: "x", AxialTilt: tilt, AxialAzimuth: 90}
	topLatX, _ := SubObserverPointDeg(bX, rotationEpoch, CameraDirTop, Vec3{})
	topLatY, _ := SubObserverPointDeg(bY, rotationEpoch, CameraDirTop, Vec3{})
	if math.Abs(topLatX-topLatY) > 1e-3 {
		t.Errorf("ViewTop should be tilt-only, ignoring azimuth: %v vs %v",
			topLatX, topLatY)
	}
	rightLatX, _ := SubObserverPointDeg(bX, rotationEpoch, CameraDirRight, Vec3{})
	rightLatY, _ := SubObserverPointDeg(bY, rotationEpoch, CameraDirRight, Vec3{})
	// azimuth 0 from +X: camera direction is along the body's tilt
	// projection, sees the *high*-lat side (subLat = +tilt).
	// azimuth 90 from +X: camera is perpendicular to tilt direction,
	// sees the equator (subLat ≈ 0).
	if math.Abs(rightLatX-tilt) > 1e-3 {
		t.Errorf("azimuth 0, ViewRight: subLat = %v, want %v", rightLatX, tilt)
	}
	if math.Abs(rightLatY) > 1e-3 {
		t.Errorf("azimuth 90, ViewRight: subLat = %v, want ~0", rightLatY)
	}
}

// TestProjectionDyConvention: regression test for the v0.8.5.7 fix
// where canvas dy>0 (below body center on screen) was being treated
// as positive latitude (north). Locks down "dy<0 = north" so a
// future refactor doesn't silently flip Earth upside-down again.
func TestProjectionDyConvention(t *testing.T) {
	// Sub-observer at lat 0, lon 0 (equator-on view). Pixel
	// directly above the body center on screen (dy<0) should
	// project to a northern latitude.
	latAbove, _, ok := projectPixelToLatLon(0, -10, 32, 0, 0, 0, 1)
	if !ok {
		t.Fatal("projection failed for valid disk pixel")
	}
	if latAbove <= 0 {
		t.Errorf("dy=-10 (above center on screen) → lat=%v, want > 0 (north)", latAbove)
	}
	// Pixel directly below body center on screen (dy>0) should
	// project to a southern latitude.
	latBelow, _, ok := projectPixelToLatLon(0, 10, 32, 0, 0, 0, 1)
	if !ok {
		t.Fatal("projection failed for valid disk pixel")
	}
	if latBelow >= 0 {
		t.Errorf("dy=+10 (below center on screen) → lat=%v, want < 0 (south)", latBelow)
	}
}

func TestProjectionRoundTripsSubObserverPoint(t *testing.T) {
	// Pixel at disk center (0, 0) must project back to the
	// sub-observer point itself, regardless of subLat / subLon.
	cases := []struct{ subLat, subLon float64 }{
		{0, 0}, {0, -30}, {30, 60}, {-45, 90}, {66.5, -120},
	}
	for _, c := range cases {
		lat, lon, ok := projectPixelToLatLon(0, 0, 32, c.subLat, c.subLon, 0, 1)
		if !ok {
			// Sub-observer at the pole can be degenerate; skip.
			continue
		}
		if math.Abs(lat-c.subLat) > 1e-6 {
			t.Errorf("center pixel for sub-observer (%v,%v): lat = %v, want %v",
				c.subLat, c.subLon, lat, c.subLat)
		}
		if math.Abs(lon-c.subLon) > 1e-6 {
			t.Errorf("center pixel for sub-observer (%v,%v): lon = %v, want %v",
				c.subLat, c.subLon, lon, c.subLon)
		}
	}
}

func TestProjectionEquatorOnMatchesV0Point8Point5(t *testing.T) {
	// At subLat=0, projectPixelToLatLon should match the v0.8.5
	// inline projection: lat = asin(ny), lon = subLon + asin(nx/cosLat).
	r := 32
	subLon := -30.0
	for ny := -r + 4; ny <= r-4; ny += 8 {
		for nx := -r + 4; nx <= r-4; nx += 8 {
			if nx*nx+ny*ny > (r-1)*(r-1) {
				continue
			}
			lat, lon, ok := projectPixelToLatLon(nx, ny, r, 0, subLon, 0, 1)
			if !ok {
				continue
			}
			fnx := float64(nx) / float64(r)
			// projectPixelToLatLon flips dy (canvas screen-down → ny up).
			fny := -float64(ny) / float64(r)
			wantLat := math.Asin(fny) * 180 / math.Pi
			cosLat := math.Sqrt(1 - fny*fny)
			wantLon := wrapDeg180(subLon + math.Asin(fnx/cosLat)*180/math.Pi)
			if math.Abs(lat-wantLat) > 1e-6 {
				t.Errorf("(%d,%d) lat = %v, want %v", nx, ny, lat, wantLat)
			}
			if math.Abs(lon-wantLon) > 1e-3 {
				t.Errorf("(%d,%d) lon = %v, want %v", nx, ny, lon, wantLon)
			}
		}
	}
}

func TestBodyRingBasisOrthonormalAndPerpendicularToAxis(t *testing.T) {
	// For any tilt, the ring basis vectors must be unit-length,
	// mutually orthogonal, and both perpendicular to the body's
	// spin axis. The canvas's RingTiltedOutline relies on this
	// invariant.
	cases := []float64{0, 23.44, 26.73, 90, 97.77, 177.36}
	for _, tilt := range cases {
		b := bodies.CelestialBody{ID: "x", AxialTilt: tilt}
		e1, e2 := BodyRingBasisWorld(b)
		n := BodyRotationAxisWorld(b)
		if math.Abs(dot(e1, e1)-1) > 1e-9 {
			t.Errorf("tilt=%v: |e1| = %v, want 1", tilt, dot(e1, e1))
		}
		if math.Abs(dot(e2, e2)-1) > 1e-9 {
			t.Errorf("tilt=%v: |e2| = %v, want 1", tilt, dot(e2, e2))
		}
		if math.Abs(dot(e1, e2)) > 1e-9 {
			t.Errorf("tilt=%v: e1·e2 = %v, want 0", tilt, dot(e1, e2))
		}
		if math.Abs(dot(e1, n)) > 1e-9 {
			t.Errorf("tilt=%v: e1·n = %v, want 0", tilt, dot(e1, n))
		}
		if math.Abs(dot(e2, n)) > 1e-9 {
			t.Errorf("tilt=%v: e2·n = %v, want 0", tilt, dot(e2, n))
		}
	}
}

func TestSaturnRingForeshorteningTopVsSide(t *testing.T) {
	// Saturn's 26.73° axial tilt foreshortens the ring differently
	// from each cardinal view. Sample 4 points on the ring at angles
	// 0°/90°/180°/270° and project (orthographically along view
	// direction); the bounding-box dimensions should match the
	// face-on / edge-on geometry.
	saturn := bodies.CelestialBody{ID: "saturn", AxialTilt: 26.73}
	e1, e2 := BodyRingBasisWorld(saturn)
	const R = 1.0
	for _, view := range []struct {
		name string
		c    Vec3
	}{
		{"top", CameraDirTop},
		{"right", CameraDirRight},
	} {
		// Build screen basis from camera direction. For a view where
		// camDir's y-component is 0, screen-up = world +Y; otherwise
		// pick anything orthogonal.
		var screenUp Vec3
		if math.Abs(view.c.Y) < 1e-9 {
			screenUp = Vec3{0, 1, 0}
		} else {
			screenUp = Vec3{0, 0, 1}
		}
		screenRight := normalize(cross(screenUp, view.c))
		screenUp = normalize(cross(view.c, screenRight))

		var minX, maxX, minY, maxY float64
		for _, theta := range []float64{0, math.Pi / 2, math.Pi, 3 * math.Pi / 2} {
			p := add(scale(e1, R*math.Cos(theta)), scale(e2, R*math.Sin(theta)))
			sx := dot(p, screenRight)
			sy := dot(p, screenUp)
			if theta == 0 {
				minX, maxX, minY, maxY = sx, sx, sy, sy
				continue
			}
			if sx < minX {
				minX = sx
			}
			if sx > maxX {
				maxX = sx
			}
			if sy < minY {
				minY = sy
			}
			if sy > maxY {
				maxY = sy
			}
		}
		bboxWidth := maxX - minX
		bboxHeight := maxY - minY
		// Top view: ring's screen-Y axis stays full length R, screen-X
		// is foreshortened by cos(tilt) ≈ 0.893 → bbox width 2R, height
		// 2R·cos(tilt). Aspect ratio (height/width) ≈ cos(tilt).
		// Right view: ring's screen-X stays full length R, screen-Y
		// is foreshortened by sin(tilt) ≈ 0.450 → aspect ≈ sin(tilt).
		// So both cases foreshorten the ring; the foreshortening
		// magnitude differs.
		switch view.name {
		case "top":
			// Expect ratio ≈ cos(26.73°) = 0.893 (height < width or vice
			// versa depending on basis choice — accept either side).
			ratio := math.Min(bboxWidth, bboxHeight) / math.Max(bboxWidth, bboxHeight)
			want := math.Cos(26.73 * math.Pi / 180)
			if math.Abs(ratio-want) > 1e-3 {
				t.Errorf("top view: ring aspect = %v, want %v", ratio, want)
			}
		case "right":
			ratio := math.Min(bboxWidth, bboxHeight) / math.Max(bboxWidth, bboxHeight)
			want := math.Sin(26.73 * math.Pi / 180)
			if math.Abs(ratio-want) > 1e-3 {
				t.Errorf("right view: ring aspect = %v, want %v", ratio, want)
			}
		}
	}
}

func TestWrapDeg180Boundaries(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 0},
		{180, 180},
		{-180, 180}, // -180 wraps to +180 since interval is half-open
		{181, -179},
		{-181, 179},
		{540, 180},     // 540 - 360 = 180 (half-open keeps it)
		{720.5, 0.5},
		{-720.5, -0.5},
	}
	for _, c := range cases {
		got := wrapDeg180(c.in)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("wrapDeg180(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestWorldToBodyFixedRoundTrip — v0.11.0+ inverse of BodyFixedToWorld.
// A trail sample taken in world-frame must recover the body-fixed
// (lat, lon) the renderer would re-project for it. Forward composed
// with inverse must be identity on a representative grid spanning
// both hemispheres of the visible sphere. Tolerance is generous
// (0.001°) because the projection is closed-form analytic, not
// pixel-discrete like the navball.
func TestWorldToBodyFixedRoundTrip(t *testing.T) {
	earth := bodies.CelestialBody{
		ID:              "earth",
		SideralRotation: 23.9345,
		MeanRadius:      6371.0,
	}
	// Mid-sidereal-day simTime — exercises a non-zero rotation phase
	// so the test catches inverse implementations that drop simTime
	// dependence.
	simTime := rotationEpoch.Add(6 * time.Hour)

	cases := []struct {
		lat, lon float64
	}{
		{0, 0},
		{28.6, -80.6}, // KSC
		{45, 90},
		{-45, -90},
		{60, 170},
		{-60, -170},
		{10, 0},
	}
	for _, tc := range cases {
		v := BodyFixedToWorld(earth, tc.lat, tc.lon, simTime)
		gotLat, gotLon := WorldToBodyFixed(earth, v, simTime)
		if math.Abs(gotLat-tc.lat) > 1e-3 {
			t.Errorf("(%g,%g) → lat round-tripped to %g", tc.lat, tc.lon, gotLat)
		}
		dlon := math.Mod(gotLon-tc.lon+540, 360) - 180
		if math.Abs(dlon) > 1e-3 {
			t.Errorf("(%g,%g) → lon round-tripped to %g (wrapped diff %g)",
				tc.lat, tc.lon, gotLon, dlon)
		}
	}
}

// TestBodyFixedToWorldIsPureSpinAxisRotation pins ADR 0003. The
// world-frame direction of a body-fixed (lat, lon) point must evolve
// as a pure rotation about BodyRotationAxisWorld at rate
// |BodySpinOmegaWorld|. Pre-v0.11.2 the Snyder-at-ViewTop construction
// failed this for tilted bodies — the implicit rotation axis was
// world +Z, not the physical spin axis, so a 1-hour Δt yielded a
// world-frame vector ~32° off from the rotation-about-n prediction
// on Earth.
func TestBodyFixedToWorldIsPureSpinAxisRotation(t *testing.T) {
	earth := bodies.CelestialBody{
		ID:              "earth",
		SideralRotation: 23.9345,
		AxialTilt:       23.44,
		MeanRadius:      6371.0,
	}
	t0 := rotationEpoch.Add(1 * time.Hour)
	const dtSec = 3600.0
	t1 := t0.Add(time.Duration(dtSec) * time.Second)

	n := BodyRotationAxisWorld(earth)
	omegaVec := BodySpinOmegaWorld(earth)
	omegaMag := math.Sqrt(omegaVec.X*omegaVec.X + omegaVec.Y*omegaVec.Y + omegaVec.Z*omegaVec.Z)
	phase := omegaMag * dtSec

	cases := []struct{ lat, lon float64 }{
		{0, 0},
		{45, 30},
		{-30, 120},
		{28.6, -80.6}, // KSC
		{0, 180},
	}
	for _, tc := range cases {
		v0 := BodyFixedToWorld(earth, tc.lat, tc.lon, t0)
		v1 := BodyFixedToWorld(earth, tc.lat, tc.lon, t1)
		rotated := rodriguesRotate(v0, n, phase)
		d := Vec3{rotated.X - v1.X, rotated.Y - v1.Y, rotated.Z - v1.Z}
		err := math.Sqrt(d.X*d.X + d.Y*d.Y + d.Z*d.Z)
		if err > 1e-9 {
			t.Errorf("(lat=%g, lon=%g): rotation about spin axis disagrees with BodyFixedToWorld Δt: |err|=%g\n  rotated(v0,n,φ)=%+v\n  v1=%+v",
				tc.lat, tc.lon, err, rotated, v1)
		}
	}
}

// TestBodyFixedToWorldEpochOffsetCentersIconicFace pins ADR 0003's
// invariant for the bodyEpochOffsetDeg sweep — the per-body offsets
// continue to mean "body-lon at the canonical-reference-view (camera
// at body-x-axis) disk centre at epoch is bodyEpochOffsetDeg." The
// new spin-axis BodyFixedToWorld must place that (lat=visible-disk-
// centre, lon=bodyEpochOffsetDeg) point at world +X.
//
// For Earth at AxialTilt=23.44° the visible disk centre is at body
// lat 23.44° (not 0°) — the tilt shifts the sub-observer northward.
// So body-fixed (23.44°, -30°) should land at world +X at epoch.
func TestBodyFixedToWorldEpochOffsetCentersIconicFace(t *testing.T) {
	earth := bodies.CelestialBody{
		ID:              "earth",
		SideralRotation: 23.9345,
		AxialTilt:       23.44,
	}
	// At canonical reference view, the sub-observer lat is the tilt
	// magnitude (because the camera sits in the equatorial plane).
	subLat, subLon := SubObserverPointDeg(earth, rotationEpoch, CameraDirRight, Vec3{})
	v := BodyFixedToWorld(earth, subLat, subLon, rotationEpoch)
	// Must equal world +X to FP precision: that's where the canonical
	// view's camera looks, so the disk centre is exactly camDir.
	if math.Abs(v.X-1) > 1e-9 || math.Abs(v.Y) > 1e-9 || math.Abs(v.Z) > 1e-9 {
		t.Errorf("canonical view disk centre at epoch: BodyFixedToWorld(%g, %g) = %+v, want (1, 0, 0)",
			subLat, subLon, v)
	}
	// Sanity: subLon must equal Earth's bodyEpochOffsetDeg (= -30).
	if math.Abs(subLon-bodyEpochOffsetDeg["earth"]) > 1e-9 {
		t.Errorf("canonical view subLon = %v, want %v (bodyEpochOffsetDeg)", subLon, bodyEpochOffsetDeg["earth"])
	}
}

// rodriguesRotate rotates v about unit axis k by angle (radians).
func rodriguesRotate(v, k Vec3, angle float64) Vec3 {
	c := math.Cos(angle)
	s := math.Sin(angle)
	vDotK := dot(v, k)
	kCrossV := cross(k, v)
	return Vec3{
		X: v.X*c + kCrossV.X*s + k.X*vDotK*(1-c),
		Y: v.Y*c + kCrossV.Y*s + k.Y*vDotK*(1-c),
		Z: v.Z*c + kCrossV.Z*s + k.Z*vDotK*(1-c),
	}
}
