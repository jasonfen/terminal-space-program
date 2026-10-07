package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// padWorld spawns a Saturn V on the KSC pad (Landed) with the surface
// navball, the state in which the nose sits on the ball's pole.
func padWorld(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        28.6083,
		LongitudeOffset: -80.604,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("pad spawn should be Landed")
	}
	w.NavMode = NavSurface
	return w, c
}

// localFrame returns (east, up, north) at the craft the same way the
// production trims do (spin axis x up), for building expected vectors.
func localFrame(c *spacecraft.Spacecraft) (east, up, north orbital.Vec3) {
	ax := render.BodyRotationAxisWorld(c.Primary)
	spin := orbital.Vec3{X: ax.X, Y: ax.Y, Z: ax.Z}
	up = c.State.R.Scale(1 / c.State.R.Norm())
	east = spin.Cross(up)
	east = east.Scale(1 / east.Norm())
	north = up.Cross(east)
	return
}

func wrap180(d float64) float64 {
	d = math.Mod(d+180, 360)
	if d < 0 {
		d += 360
	}
	return d - 180
}

// TestNavballPadRosePinnedToCommandedHeading (G8 Q5): with the nose within
// a degree of straight up the ball's rotation comes from the commanded
// heading (HeadingTrimDueEastRad + HeadingTrim), not from the float noise
// in atan2 of the near-zero horizontal components. Sub-observer longitude
// is minus the bearing, so due east puts lon at -90 (E straight below the
// centre). Perturbing the nose by 0.006 degrees in any direction must not
// move it. Seam: World.NavballSubObserver, the call the orbit and launch
// views both make.
func TestNavballPadRosePinnedToCommandedHeading(t *testing.T) {
	w, c := padWorld(t)
	w.InstantSAS = false
	basis, ok := w.NavballBasis()
	if !ok {
		t.Fatal("no basis on the pad")
	}
	for _, trimDeg := range []float64{0, 20, -35} {
		c.HeadingTrim = trimDeg * math.Pi / 180
		want := wrap180(-(90 + trimDeg))
		for _, theta := range []float64{0, 1.1, 2.3, 4.0, 5.5} {
			const eps = 1e-4
			c.CurrentAttitudeDir = basis.EZ.Add(basis.EX.Scale(eps * math.Cos(theta))).Add(basis.EY.Scale(eps * math.Sin(theta))).Unit()
			lat, lon, ok := w.NavballSubObserver()
			if !ok {
				t.Fatal("NavballSubObserver ok=false")
			}
			if lat < 89.9 {
				t.Fatalf("setup: lat=%g, want near the pole", lat)
			}
			if d := math.Abs(wrap180(lon - want)); d > 1e-6 {
				t.Errorf("trim %+g theta %g: lon=%g, want pinned %g (noise-dependent rose)", trimDeg, theta, lon, want)
			}
		}
	}
}

// TestNavballPadRoseContinuousAtPitchOver: once the nose pitches over
// toward the commanded bearing the actual longitude takes over, and it is
// the same value the pin was holding, so the rose does not jump.
func TestNavballPadRoseContinuousAtPitchOver(t *testing.T) {
	w, c := padWorld(t)
	w.InstantSAS = false
	east, up, _ := localFrame(c)
	c.HeadingTrim = 0
	// 5 degrees off vertical toward due east.
	tilt := 5 * math.Pi / 180
	c.CurrentAttitudeDir = up.Scale(math.Cos(tilt)).Add(east.Scale(math.Sin(tilt)))
	lat, lon, ok := w.NavballSubObserver()
	if !ok {
		t.Fatal("ok=false")
	}
	if math.Abs(lat-85) > 0.01 {
		t.Errorf("lat=%g, want 85", lat)
	}
	if d := math.Abs(wrap180(lon - (-90))); d > 1e-4 {
		t.Errorf("lon=%g after pitch-over, want -90 (same as the pinned pad value)", lon)
	}
}

// TestNavballMarkersMarkUntrimmedHold (G8 Q6, KSP style): the hold glyphs
// sit at the true hold directions; the trim lives in the nose. With a
// 15 degree pitch trim and the nose snapped to the commanded (trimmed)
// direction, the prograde glyph is ~15 degrees from the disk centre, and
// with no trim it is on it. InstantSAS pins the nose to the commanded
// direction so the gap is exactly the trim.
func TestNavballMarkersMarkUntrimmedHold(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// A pitch trim leans the nose about local north, which is the orbit
	// normal only on an eastbound (equatorial) orbit; the seed is inclined
	// (#566), so name the equatorial case explicitly.
	makeActiveEquatorial(w)
	w.NavMode = NavOrbit
	w.InstantSAS = true
	c := w.ActiveCraft()
	c.AttitudeMode = spacecraft.BurnPrograde

	progradeMarker := func() render.NavballMarker {
		for _, m := range w.NavballMarkers() {
			if m.Glyph == NavballGlyphPrograde {
				return m
			}
		}
		t.Fatal("no prograde marker")
		return render.NavballMarker{}
	}
	sep := func() float64 {
		lat, lon, ok := w.NavballSubObserver()
		if !ok {
			t.Fatal("no sub-observer")
		}
		m := progradeMarker()
		return angSepDeg(lat, lon, m.LatDeg, m.LonDeg)
	}

	c.PitchTrim = 0
	if s := sep(); s > 0.01 {
		t.Errorf("untrimmed: prograde glyph %g deg from the nose, want on it", s)
	}
	c.PitchTrim = 15 * math.Pi / 180
	s := sep()
	if math.Abs(s-15) > 0.5 {
		t.Errorf("trim +15: prograde glyph %g deg from the nose, want ~15 (glyph marks the untrimmed hold)", s)
	}
}

// TestNavballNoseReading (G8 Q3): the measured pitch/heading of the slewed
// nose, the same in every nav mode, not waiting for any dead-band.
func TestNavballNoseReading(t *testing.T) {
	w, c := padWorld(t)
	w.InstantSAS = false
	east, up, north := localFrame(c)
	pitch, hdg := 62*math.Pi/180, 88*math.Pi/180
	horiz := east.Scale(math.Sin(hdg)).Add(north.Scale(math.Cos(hdg)))
	c.CurrentAttitudeDir = horiz.Scale(math.Cos(pitch)).Add(up.Scale(math.Sin(pitch)))
	for _, nm := range []NavMode{NavSurface, NavOrbit} {
		w.NavMode = nm
		p, h, ok := w.NavballNoseReading()
		if !ok {
			t.Fatalf("%v: ok=false", nm)
		}
		if math.Abs(p-62) > 1e-6 || math.Abs(h-88) > 1e-6 {
			t.Errorf("%v: reading pitch=%g hdg=%g, want 62 / 88", nm, p, h)
		}
	}
}

// TestNavballNoseReadingVerticalUsesCommandedHeading: straight up has no
// bearing, so the heading half falls back to the commanded one (the same
// bearing the pad rose is pinned to).
func TestNavballNoseReadingVerticalUsesCommandedHeading(t *testing.T) {
	w, c := padWorld(t)
	w.InstantSAS = false
	_, up, _ := localFrame(c)
	c.CurrentAttitudeDir = up
	c.HeadingTrim = 20 * math.Pi / 180
	p, h, ok := w.NavballNoseReading()
	if !ok {
		t.Fatal("ok=false")
	}
	if math.Abs(p-90) > 1e-6 || math.Abs(h-110) > 1e-6 {
		t.Errorf("pad reading pitch=%g hdg=%g, want 90 / 110", p, h)
	}
}

func angSepDeg(lat1, lon1, lat2, lon2 float64) float64 {
	const d = math.Pi / 180
	v := func(lat, lon float64) orbital.Vec3 {
		return orbital.Vec3{X: math.Cos(lat*d) * math.Cos(lon*d), Y: math.Cos(lat*d) * math.Sin(lon*d), Z: math.Sin(lat * d)}
	}
	a, b := v(lat1, lon1), v(lat2, lon2)
	dot := a.Dot(b)
	if dot > 1 {
		dot = 1
	} else if dot < -1 {
		dot = -1
	}
	return math.Acos(dot) / d
}

// TestNavballOrbitPitchTrimAxes pins the observed ORBIT-mode margin (wave C
// review, LOW): a pitch trim rotates the nose about local north, so the
// readout's elevation moves by the trim while the ORBIT ball's latitude
// (out-of-plane angle) stays on the equator. The three readings (readout
// pitch, GUIDANCE trim, ball gap) are one input on different axes; the F1
// glossary row says so. If this test moves, revisit that row.
func TestNavballOrbitPitchTrimAxes(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// A pitch trim leans the nose about local north, which is the orbit
	// normal only on an eastbound (equatorial) orbit; the seed is inclined
	// (#566), so name the equatorial case explicitly.
	makeActiveEquatorial(w)
	w.NavMode = NavOrbit
	w.InstantSAS = true
	c := w.ActiveCraft()
	c.AttitudeMode = spacecraft.BurnPrograde
	c.PitchTrim = 15 * math.Pi / 180
	lat, _, ok := w.NavballSubObserver()
	if !ok {
		t.Fatal("no sub-observer")
	}
	pitch, _, ok := w.NavballNoseReading()
	if !ok {
		t.Fatal("no reading")
	}
	t.Logf("trim +15 in ORBIT: ball lat %.2f, readout pitch %.2f", lat, pitch)
	if math.Abs(lat) > 1 {
		t.Errorf("ORBIT ball latitude %.2f, want ~0 (trim moves the nose along the ball's equator)", lat)
	}
	if math.Abs(math.Abs(pitch)-15) > 1 {
		t.Errorf("readout pitch %.2f, want magnitude ~15", pitch)
	}
}
