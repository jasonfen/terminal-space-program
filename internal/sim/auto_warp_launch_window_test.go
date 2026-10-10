package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Auto-Warp to the launch window (Jason, 2026-10-08: "when I am launching
// and have an orbiting vessel / body targeted, the launch window is
// incredibly useful, and I'd like the auto-warp button to work to warp to
// the next window"). On the pad with a window, G warps to the window less
// the usual 30 s lead and hands off at 1x, the same shape as a burn.

func padTargetingSeed(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	seedIdx := w.ActiveCraftIdx
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "earth",
		Launchpad: true, Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
	}); err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = len(w.Crafts) - 1
	w.SetTargetCraft(seedIdx)
	return w
}

func TestAutoWarpOnThePadChasesTheLaunchWindow(t *testing.T) {
	w := padTargetingSeed(t)
	lw, ok := w.LaunchWindow()
	if !ok || !lw.Open {
		t.Fatalf("setup: want an open window, got ok=%v %+v", ok, lw)
	}
	if !w.AutoWarpEligible() {
		t.Fatal("Auto-Warp not eligible on the pad with a launch window ahead")
	}
	if !w.EngageAutoWarp() {
		t.Fatal("EngageAutoWarp refused the launch window")
	}
	want := lw.PassAt.Add(-autoWarpLeadTime)
	if !w.AutoWarp.T.Equal(want) {
		t.Fatalf("Auto-Warp aims at %v, want the window less 30 s (%v)", w.AutoWarp.T, want)
	}
	// Fly it through the real tick: released at 1x, about 30 s before the pass.
	for i := 0; i < 200000 && w.AutoWarpEngaged(); i++ {
		w.Tick()
	}
	if w.AutoWarpEngaged() {
		t.Fatalf("still engaged after the window's lead point; clock %v, target %v", w.Clock.SimTime, want)
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("released at warp index %d, want 1x (0)", w.Clock.WarpIdx)
	}
	left := lw.PassAt.Sub(w.Clock.SimTime)
	if left < 25*time.Second || left > 31*time.Second {
		t.Errorf("released %v before the window, want about 30 s", left)
	}
	if !w.ActiveCraft().Landed {
		t.Error("the pad vessel left the ground during the warp")
	}
}

// A target with no exact pass (the pad's latitude is above the plane's
// tilt) still has a best moment on the plan: row; G warps to that.
func TestAutoWarpOnThePadChasesTheBestMomentWithoutAPass(t *testing.T) {
	w := padTargetingSeed(t)
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	lw, ok := w.LaunchWindow()
	if !ok || lw.Open {
		t.Fatalf("setup: want a no-pass (best) window to the Moon, got ok=%v %+v", ok, lw)
	}
	if !w.EngageAutoWarp() {
		t.Fatal("EngageAutoWarp refused the best moment")
	}
	if want := lw.BestAt.Add(-autoWarpLeadTime); !w.AutoWarp.T.Equal(want) {
		t.Errorf("Auto-Warp aims at %v, want the best moment less 30 s (%v)", w.AutoWarp.T, want)
	}
}

// Clearing the target mid-warp leaves nothing to chase: disengage, keep
// the player's own warp.
func TestAutoWarpLaunchWindowDisengagesWhenTheTargetGoes(t *testing.T) {
	w := padTargetingSeed(t)
	if !w.EngageAutoWarp() {
		t.Fatal("engage")
	}
	w.Tick()
	w.ClearTarget()
	w.Tick()
	if w.AutoWarpEngaged() {
		t.Error("Auto-Warp still chasing a window after the target was cleared")
	}
}

// G on the pad also trims the heading to the one the window wants (Jason,
// 2026-10-09: "G should auto trim"), to the whole degree the plan: row
// prints, so after the warp the launch plane IS the target's: no ↑/↓ hunt.
// Pitch trim is the player's and stays put.
func TestAutoWarpOnThePadTrimsTheHeadingToTheWindow(t *testing.T) {
	w := padTargetingSeed(t)
	c := w.ActiveCraft()
	c.PitchTrim = 3 * math.Pi / 180
	lw, ok := w.LaunchWindow()
	if !ok || !lw.Open {
		t.Fatalf("setup: want an open window, got ok=%v %+v", ok, lw)
	}
	if math.Abs(lw.HeadingDeg-90) < 5 {
		t.Fatalf("setup: the window wants %.1f°, too close to due east to tell a trim happened", lw.HeadingDeg)
	}
	if !w.EngageAutoWarp() {
		t.Fatal("EngageAutoWarp refused the launch window")
	}
	gotDeg := (spacecraft.HeadingTrimDueEastRad + c.HeadingTrim) * 180 / math.Pi
	if want := math.Round(lw.HeadingDeg); math.Abs(gotDeg-want) > 1e-9 {
		t.Errorf("commanded heading %.3f°, want the window's %.0f°", gotDeg, want)
	}
	if c.PitchTrim != 3*math.Pi/180 {
		t.Errorf("pitch trim changed to %.3f rad; G trims the heading only", c.PitchTrim)
	}
	// The effect, not the setting: at the pass, that heading's launch plane
	// is the target's.
	spinR := render.BodyRotationAxisWorld(c.Primary)
	spin := orbital.Vec3{X: spinR.X, Y: spinR.Y, Z: spinR.Z}
	n, ok := spacecraft.HeadingOrbitNormal(lw.padDir.Scale(c.Primary.RadiusMeters()), spin, c.HeadingTrim)
	if !ok {
		t.Fatal("no launch plane for the commanded heading")
	}
	if a := foldedPlaneAngleDeg(n, lw.nT); a > 0.5 {
		t.Errorf("launching at the pass on the trimmed heading misses the target's plane by %.2f°", a)
	}
}

// With no pass, G trims to the heading the best figure is measured for
// (due east for a prograde target), undoing a stray manual trim.
func TestAutoWarpWithoutAPassTrimsToTheBestMomentHeading(t *testing.T) {
	w := padTargetingSeed(t)
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	c := w.ActiveCraft()
	c.HeadingTrim = -45 * math.Pi / 180
	lw, ok := w.LaunchWindow()
	if !ok || lw.Open {
		t.Fatalf("setup: want a no-pass window to the Moon, got ok=%v %+v", ok, lw)
	}
	if !w.EngageAutoWarp() {
		t.Fatal("EngageAutoWarp refused the best moment")
	}
	gotDeg := (spacecraft.HeadingTrimDueEastRad + c.HeadingTrim) * 180 / math.Pi
	if want := lw.HeadingDeg; math.Abs(gotDeg-want) > 1e-9 {
		t.Errorf("commanded heading %.3f°, want the best moment's %.0f°", gotDeg, want)
	}
}
