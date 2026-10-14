package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// tugAtStartVessel is the first dock a new player flies: the default Apollo
// TLI start vessel (a 55.5 m stack) and an RCS Tug spawned alongside, moved
// to portGapM from the stack's CM port, straight and at rest.
func tugAtStartVessel(t *testing.T, portGapM float64) (*World, int) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	home := w.Crafts[0]
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: "RCS-tug", Alongside: true}); err != nil {
		t.Fatal(err)
	}
	tug := w.ActiveCraft()
	gh, gt := VesselPortGeometry(home), VesselPortGeometry(tug)
	if !gh.HasPort() || !gt.HasPort() {
		t.Fatalf("fixture: start vessel port %v, tug port %v", gh.HasPort(), gt.HasPort())
	}
	portHome := gh.PortPoint(home.State.R, 0)
	portTug := portHome.Add(gh.Axis.Scale(portGapM))
	tug.CurrentAttitudeDir = gh.Axis.Scale(-1)
	tug.State.R = portTug.Sub(tug.CurrentAttitudeDir.Scale(gt.Ports[0].OffsetM))
	tug.State.V = home.State.V
	w.Clock.WarpIdx = 0
	return w, len(w.Crafts) - 1
}

// The 50 m zone is hull to hull, not stack bottom to stack bottom (review
// finding 4): at a 0.4 m port gap against the 55.5 m start stack the two
// anchors are 60 m apart, but the hulls are 0.4 m apart.
func TestZoneIsHullToHullAtTheStartVesselPort(t *testing.T) {
	w, _ := tugAtStartVessel(t, 0.4)
	home, tug := w.Crafts[0], w.ActiveCraft()
	if d := tug.State.R.Sub(home.State.R).Norm(); d < 55 {
		t.Fatalf("fixture: anchors only %.1f m apart", d)
	}
	if !w.activeNearOtherHull() {
		t.Error("activeNearOtherHull = false at a 0.4 m hull gap (anchors > 50 m apart)")
	}
	// The warp readout: 100x selected must clamp to the burn cap.
	for i, f := range WarpFactors {
		if f >= 100 {
			w.Clock.WarpIdx = i
			break
		}
	}
	if got := w.EffectiveWarp(); got > burnWarpCap {
		t.Errorf("effective warp = %vx at a 0.4 m hull gap, want <= %vx", got, burnWarpCap)
	}
}

// ...and a hull-to-hull touch outside the gate in that same geometry bumps:
// the pre-filter used to skip the pair at 60 m between anchors.
func TestBumpScanReachesTheFarEndOfALongStack(t *testing.T) {
	w, _ := tugAtStartVessel(t, -0.2) // 0.2 m past the noses
	home, tug := w.Crafts[0], w.ActiveCraft()
	g := VesselPortGeometry(home)
	// 1 m lateral miss: off the port, nose-first.
	tug.State.R = tug.State.R.Add(g.Axis.Cross(tug.State.R).Unit().Scale(1))
	tug.State.V = tug.State.V.Sub(g.Axis.Scale(0.8)) // closing at 0.8 m/s
	w.localReArms = append(w.localReArms, localReArm{idA: home.ID, idB: tug.ID})
	w.checkBumps()
	if w.LastBumpEvent == nil {
		t.Fatal("no bump: the scan skipped a pair whose hulls overlap because the anchors are 60 m apart")
	}
}

// HullDistanceM is a surface gap: the port gap for a coaxial nose-to-nose
// pair, 0 for hulls touching side by side, and never the anchor distance.
func TestHullDistanceIsASurfaceGap(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 0.4, 0, 0)
	if got := craftHullDistanceM(p.a, p.b); !near(got, 0.4, 1e-6) {
		t.Errorf("coaxial pair 0.4 m apart: hull distance = %.4f m", got)
	}
	w, a, b := bumpWorld(t, 4, 0) // two parallel S-IVBs, axes 4 m apart, widths 6 m: touching
	_ = w
	if got := craftHullDistanceM(a, b); got != 0 {
		t.Errorf("hulls overlapping side by side: hull distance = %.3f m, want 0", got)
	}
	b.State.R = a.State.R.Add(orbital.Vec3{X: 26}) // 26 m axis distance, 6 m radii sum: 20 m of air
	if got := craftHullDistanceM(a, b); !near(got, 20, 1e-6) {
		t.Errorf("side by side 26 m between axes: hull distance = %.3f m, want 20", got)
	}
}
