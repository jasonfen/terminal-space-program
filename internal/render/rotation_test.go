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
