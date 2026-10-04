package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// padWindowWorld spawns a Saturn V on a pad at latDeg (active, Landed)
// and, when orbitIncl > 0, a 400 km vessel at that inclination to aim at.
func padWindowWorld(t *testing.T, latDeg, orbitIncl float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	pad, err := w.SpawnCraft(SpawnSpec{
		LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "earth",
		Launchpad: true, Latitude: latDeg, LongitudeOffset: DefaultLaunchpadLongitudeEast,
	})
	if err != nil || !pad.Landed {
		t.Fatalf("pad spawn: %v landed=%v", err, pad != nil && pad.Landed)
	}
	padIdx := len(w.Crafts) - 1
	if orbitIncl > 0 {
		if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3, Inclination: orbitIncl}); err != nil {
			t.Fatalf("orbit spawn: %v", err)
		}
		w.ActiveCraftIdx = padIdx
		w.SetTargetCraft(len(w.Crafts) - 1)
	}
	w.ActiveCraftIdx = padIdx
	return w, pad
}

// movePad re-pins the Landed craft to its pad at simTime (what
// integrateLanded does each tick) and sets the clock.
func movePad(w *World, c *spacecraft.Spacecraft, at time.Time) {
	w.Clock.SimTime = at
	lat, lon := c.SurfaceLatLon()
	d := render.BodyFixedToWorld(c.Primary, lat, lon, at)
	r := c.Primary.RadiusMeters()
	c.State.R = orbital.Vec3{X: r * d.X, Y: r * d.Y, Z: r * d.Z}
	om := render.BodySpinOmegaWorld(c.Primary)
	c.State.V = orbital.Vec3{X: om.X, Y: om.Y, Z: om.Z}.Cross(c.State.R)
}

func planeAngleAtHeading(c *spacecraft.Spacecraft, bearingDeg float64, nT orbital.Vec3) float64 {
	s := render.BodyRotationAxisWorld(c.Primary)
	spin := orbital.Vec3{X: s.X, Y: s.Y, Z: s.Z}
	h, _ := spacecraft.HeadingOrbitNormal(c.State.R, spin, (bearingDeg-90)*math.Pi/180)
	return foldedPlaneAngleDeg(h, nT)
}

// Pass headings through the PRODUCTION plane normal: at the pass the
// named heading leaves a launch plane that is the target's, and the
// pass rolls to the other heading once it is past.
func TestLaunchWindowPassHeadingMatchesTargetPlane(t *testing.T) {
	w, pad := padWindowWorld(t, DefaultLaunchpadLatitude, 51.6)
	lw, ok := w.LaunchWindow()
	if !ok || !lw.Open {
		t.Fatalf("KSC to a 51.6 deg station must have a window: ok=%v %+v", ok, lw)
	}
	t0 := w.Clock.SimTime
	if d := lw.PassAt.Sub(t0); d <= 0 || d > 24*time.Hour {
		t.Fatalf("pass %v out of (0,24h]", d)
	}
	nT, _ := w.launchWindowTargetNormal()
	first := lw.HeadingDeg
	if math.Abs(first-45) > 0.5 && math.Abs(first-135) > 0.5 {
		t.Errorf("heading %.2f, want 045 or 135 for 51.6 deg from KSC", first)
	}
	movePad(w, pad, lw.PassAt)
	if got := planeAngleAtHeading(pad, lw.HeadingDeg, nT); got > 0.05 {
		t.Errorf("at the pass the named heading leaves %.3f deg off the target plane", got)
	}
	// Once past: rolls to the other pass, which wants the mirror heading.
	movePad(w, pad, lw.PassAt.Add(time.Minute))
	lw2, _ := w.LaunchWindow()
	if !lw2.PassAt.After(lw.PassAt.Add(time.Hour)) {
		t.Errorf("did not roll to the next pass: %v then %v", lw.PassAt, lw2.PassAt)
	}
	if math.Abs((first+lw2.HeadingDeg)-180) > 1 {
		t.Errorf("second pass heading %.2f is not the mirror of %.2f", lw2.HeadingDeg, first)
	}
	movePad(w, pad, lw2.PassAt)
	if got := planeAngleAtHeading(pad, lw2.HeadingDeg, nT); got > 0.05 {
		t.Errorf("second pass heading leaves %.3f deg", got)
	}
}

// The default Earth pad with the Moon aimed has no pass (floor above the
// plane's tilt): the row reports the best angle and when, and the angle
// is the one the production normal reads at that instant.
func TestLaunchWindowNoWindowReportsBest(t *testing.T) {
	w, pad := padWindowWorld(t, DefaultLaunchpadLatitude, 0)
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	lw, ok := w.LaunchWindow()
	if !ok {
		t.Fatal("no window reading for a body target")
	}
	if lw.Open {
		t.Fatalf("KSC to the Moon must have no pass (floor 28.61 > tilt): %+v", lw)
	}
	nT, _ := w.launchWindowTargetNormal()
	tilt := math.Acos(math.Abs(nT.Dot(orbital.Vec3{X: render.BodyRotationAxisWorld(pad.Primary).X, Y: render.BodyRotationAxisWorld(pad.Primary).Y, Z: render.BodyRotationAxisWorld(pad.Primary).Z}))) * 180 / math.Pi
	if !(DefaultLaunchpadLatitude > tilt) {
		t.Fatalf("fixture: floor %.2f must exceed tilt %.2f", DefaultLaunchpadLatitude, tilt)
	}
	if math.Abs(lw.BestDeg-(DefaultLaunchpadLatitude-tilt)) > 0.05 {
		t.Errorf("best %.3f, want floor-tilt %.3f", lw.BestDeg, DefaultLaunchpadLatitude-tilt)
	}
	movePad(w, pad, lw.BestAt)
	if got := planeAngleAtHeading(pad, lw.HeadingDeg, nT); math.Abs(got-lw.BestDeg) > 0.01 {
		t.Errorf("production normal reads %.3f at BestAt, solver said %.3f", got, lw.BestDeg)
	}
	if lw.BestAt.Sub(w.Clock.SimTime) < 0 {
		t.Error("BestAt in the past")
	}
}

// An equatorial pad reaches the Moon's plane: window exists, heading
// is the production-verified one.
func TestLaunchWindowEquatorialPadToMoon(t *testing.T) {
	w, pad := padWindowWorld(t, 0, 0)
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	lw, ok := w.LaunchWindow()
	if !ok || !lw.Open {
		t.Fatalf("equatorial pad must reach the Moon's plane: %+v", lw)
	}
	nT, _ := w.launchWindowTargetNormal()
	movePad(w, pad, lw.PassAt)
	if got := planeAngleAtHeading(pad, lw.HeadingDeg, nT); got > 0.05 {
		t.Errorf("heading %.2f leaves %.3f deg at the pass", lw.HeadingDeg, got)
	}
}

// The plan row asks every frame; the solver must run once per key
// (#460 "recompute is cached"). Past the cached pass and for a new
// target it re-solves; a fresh World (load) starts with an empty cache
// and agrees.
func TestLaunchWindowSolverRunsOncePerKey(t *testing.T) {
	w, pad := padWindowWorld(t, DefaultLaunchpadLatitude, 51.6)
	lw, _ := w.LaunchWindow()
	for i := 0; i < 50; i++ {
		w.LaunchWindow()
	}
	if got := w.LaunchWindowSolves(); got != 1 {
		t.Fatalf("51 reads, %d solves, want 1", got)
	}
	// Clock moves (still before the pass): no re-solve.
	movePad(w, pad, w.Clock.SimTime.Add(10*time.Minute))
	w.LaunchWindow()
	if got := w.LaunchWindowSolves(); got != 1 {
		t.Errorf("clock advance re-solved: %d", got)
	}
	// Heading trim does not move the window (G6 Q1).
	pad.HeadingTrim += 10 * math.Pi / 180
	lwTrim, _ := w.LaunchWindow()
	if !lwTrim.PassAt.Equal(lw.PassAt) || w.LaunchWindowSolves() != 1 {
		t.Errorf("trim moved the window or re-solved (%d)", w.LaunchWindowSolves())
	}
	// Past the pass: a new solve.
	movePad(w, pad, lw.PassAt.Add(time.Minute))
	w.LaunchWindow()
	if got := w.LaunchWindowSolves(); got != 2 {
		t.Errorf("past the pass: %d solves, want 2", got)
	}
	// New target plane: a new solve.
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	w.LaunchWindow()
	if got := w.LaunchWindowSolves(); got != 3 {
		t.Errorf("new target: %d solves, want 3", got)
	}
}

// Lead at the pass, against FLOWN truth: tick the real World (warp 1e5,
// the Kepler-locked free-flight path for the target, landed pin for the
// pad) to the pass instant, then measure pad-to-target in the target's
// plane with explicit vectors.
func TestLaunchWindowLeadAtPassMatchesFlownTruth(t *testing.T) {
	w, pad := padWindowWorld(t, DefaultLaunchpadLatitude, 51.6)
	lw, _ := w.LaunchWindow()
	lead, ok := w.LaunchWindowLeadDeg(lw)
	if !ok {
		t.Fatal("no lead for a vessel target")
	}
	for i := 0; i < 5; i++ {
		w.Clock.WarpUp()
	}
	w.Clock.WarpIdx = len(WarpFactors) - 1
	n := 0
	for w.Clock.SimTime.Before(lw.PassAt) && n < 200000 {
		if rem := lw.PassAt.Sub(w.Clock.SimTime); rem < w.Clock.BaseStep*time.Duration(w.Clock.Warp()) {
			w.Clock.WarpIdx = 0
		}
		w.Tick()
		n++
	}
	if d := w.Clock.SimTime.Sub(lw.PassAt); d < -time.Second || d > time.Minute {
		t.Fatalf("flew to %v, wanted the pass %v (ticks %d)", w.Clock.SimTime, lw.PassAt, n)
	}
	tgt := w.Crafts[len(w.Crafts)-1]
	nT, _ := w.launchWindowTargetNormal()
	h := nT
	if h.Dot(tgt.State.R.Cross(tgt.State.V)) < 0 {
		h = h.Scale(-1)
	}
	proj := func(v orbital.Vec3) orbital.Vec3 { return v.Sub(h.Scale(h.Dot(v))) }
	a, b := proj(pad.State.R), proj(tgt.State.R)
	truth := math.Atan2(h.Dot(a.Cross(b)), a.Dot(b)) * 180 / math.Pi
	if math.Abs(truth-lead) > 0.5 {
		t.Errorf("lead at pass %.3f, flown truth %.3f (Kepler-warp path through Tick)", lead, truth)
	}
}

// TestLaunchWindowBestDoesNotStickOnZero (#548 review line 85): with no
// pass, the row used to read `best 9.17° T-0s` and re-solve every frame for
// about 90 s after the best moment, then jump a day. The sample grid starts
// at "now", so which side of the grid the minimum fell on depended on the
// phase: scan every second from 1 s to 400 s past the best moment, each
// solved cold (cache emptied), and require the answer to be the NEXT best
// moment (hours away), never a countdown stuck on zero. Then walk the
// player's path (warm cache, 1 s frames across the moment) and bound the
// solves.
func TestLaunchWindowBestDoesNotStickOnZero(t *testing.T) {
	w, pad := padWindowWorld(t, DefaultLaunchpadLatitude, 0)
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	first, ok := w.LaunchWindow()
	if !ok || first.Open {
		t.Fatalf("fixture: want a no-pass window, got %+v ok=%v", first, ok)
	}
	best := first.BestAt
	stuck := 0
	for s := 1; s <= 400; s++ {
		w.lwCache = launchWindowCache{}
		movePad(w, pad, best.Add(time.Duration(s)*time.Second))
		lw, ok := w.LaunchWindow()
		if !ok {
			t.Fatalf("t+%ds: no reading", s)
		}
		if d := lw.BestAt.Sub(w.Clock.SimTime); d < 6*time.Hour {
			if stuck == 0 {
				t.Errorf("t+%ds past the best moment the row counts down to %v, want the next day's", s, d)
			}
			stuck++
		}
	}
	if stuck > 0 {
		t.Errorf("%d of 400 phases stuck near zero", stuck)
	}

	w.lwCache = launchWindowCache{}
	startSolves := w.LaunchWindowSolves()
	for s := -60; s <= 400; s++ {
		movePad(w, pad, best.Add(time.Duration(s)*time.Second))
		if _, ok := w.LaunchWindow(); !ok {
			t.Fatalf("walk t%+ds: no reading", s)
		}
	}
	if n := w.LaunchWindowSolves() - startSolves; n > 3 {
		t.Errorf("%d solves across 461 one-second frames over the best moment, want at most 3", n)
	}
}
