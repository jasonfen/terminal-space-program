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
	subLat, _ := SubObserverPointDeg(b, rotationEpoch, CameraDirTop)
	if math.Abs(subLat-90) > 1e-6 {
		t.Errorf("ViewTop on tilt=0 body: subLat = %v, want 90", subLat)
	}
}

func TestSubObserverPointSideViewNoTiltIsEquator(t *testing.T) {
	// Untilted body viewed from "right" (camera at +X) — sub-observer
	// is on the equator, lat = 0°.
	b := bodies.CelestialBody{ID: "x", SideralRotation: 24.0, AxialTilt: 0}
	subLat, _ := SubObserverPointDeg(b, rotationEpoch, CameraDirRight)
	if math.Abs(subLat) > 1e-6 {
		t.Errorf("ViewRight on tilt=0 body: subLat = %v, want 0", subLat)
	}
}

func TestSubObserverPointEarthTopShowsArctic(t *testing.T) {
	// Earth has 23.44° axial tilt. ViewTop sees the body axis
	// projecting up out of the orbital plane — sub-observer lat ≈
	// 90 - 23.44 = 66.56°. (Inside the Arctic Circle by a hair.)
	earth := bodies.CelestialBody{ID: "earth", SideralRotation: 24.0, AxialTilt: 23.44}
	subLat, _ := SubObserverPointDeg(earth, rotationEpoch, CameraDirTop)
	want := 90 - 23.44
	if math.Abs(subLat-want) > 1e-3 {
		t.Errorf("ViewTop on Earth: subLat = %v, want %v", subLat, want)
	}
}

func TestSubObserverPointUranusRollsAlongOrbit(t *testing.T) {
	// Uranus's 97.77° tilt makes it roll pole-on along its orbit.
	// ViewTop (camera at +Z) sees the body axis nearly in the
	// orbital plane — sub-observer lat is small.
	// ViewRight (camera at +X) sees the body axis almost pointing
	// at the camera — sub-observer lat is large (near pole).
	uranus := bodies.CelestialBody{ID: "uranus", SideralRotation: -17.24, AxialTilt: 97.77}
	subLatTop, _ := SubObserverPointDeg(uranus, rotationEpoch, CameraDirTop)
	subLatRight, _ := SubObserverPointDeg(uranus, rotationEpoch, CameraDirRight)
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

func TestProjectionRoundTripsSubObserverPoint(t *testing.T) {
	// Pixel at disk center (0, 0) must project back to the
	// sub-observer point itself, regardless of subLat / subLon.
	cases := []struct{ subLat, subLon float64 }{
		{0, 0}, {0, -30}, {30, 60}, {-45, 90}, {66.5, -120},
	}
	for _, c := range cases {
		lat, lon, ok := projectPixelToLatLon(0, 0, 32, c.subLat, c.subLon)
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
			lat, lon, ok := projectPixelToLatLon(nx, ny, r, 0, subLon)
			if !ok {
				continue
			}
			fnx := float64(nx) / float64(r)
			fny := float64(ny) / float64(r)
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
