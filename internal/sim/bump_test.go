package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// bumpWorld returns a world with two S-IVB-1 hulls in a 500 km Earth orbit,
// both pointing along +Y (side by side, so a +X approach misses the noses),
// B sitting gapM metres along +X of A. rel is B's velocity relative to A
// along X (negative = B closing on A). Warp is 1x.
func bumpWorld(t *testing.T, gapM, relX float64) (*World, *spacecraft.Spacecraft, *spacecraft.Spacecraft) {
	t.Helper()
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	a := w.Crafts[0]
	a.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	a.State.V = orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / a.State.R.Norm())}
	a.CurrentAttitudeDir = orbital.Vec3{Y: 1}
	b := spacecraft.NewFromLoadout("S-IVB-1")
	b.Primary = *earth
	b.SystemIdx = a.SystemIdx
	b.State = physics.StateVector{
		R: a.State.R.Add(orbital.Vec3{X: gapM}),
		V: a.State.V.Add(orbital.Vec3{X: relX}),
		M: b.TotalMass(),
	}
	b.CurrentAttitudeDir = orbital.Vec3{Y: 1}
	b.ID = a.ID + 100
	w.Crafts = append(w.Crafts, b)
	w.Clock.WarpIdx = 0
	return w, a, b
}

// opening is how fast B moves away from A along +X (positive = parting).
func opening(a, b *spacecraft.Spacecraft) float64 {
	return b.State.V.Sub(a.State.V).X
}

// ADR 0053 tests table: "1 m/s approach missing the cone leaves the pair
// opening at 0.5 m/s with the caption stamped". Measured, not implied: the
// opening speed after the contact tick is read off the two velocity vectors.
func TestBumpOneMetrePerSecondMissLeavesPairOpeningAtHalf(t *testing.T) {
	w, a, b := bumpWorld(t, 30, -1.0)
	bumped := 0
	for i := 0; i < 2000 && bumped == 0; i++ {
		w.Tick()
		if w.LastBumpEvent != nil {
			bumped++
		}
	}
	if bumped == 0 {
		t.Fatalf("hulls passed through each other: no bump (gap now %.2f m)", b.State.R.Sub(a.State.R).X)
	}
	if got := opening(a, b); math.Abs(got-0.5) > 0.02 {
		t.Errorf("opening speed after the bump = %.3f m/s, want 0.5", got)
	}
	e := w.LastBumpEvent
	if e.PartnerName != b.Name && e.PartnerName != a.Name {
		t.Errorf("caption names %q, want one of the pair", e.PartnerName)
	}
	if math.Abs(e.SpeedMS-1.0) > 0.05 {
		t.Errorf("caption speed = %.3f, want the 1 m/s contact speed", e.SpeedMS)
	}
}

// Momentum is conserved: the pair's mass-weighted velocity is unchanged.
func TestBumpConservesMomentum(t *testing.T) {
	w, a, b := bumpWorld(t, 30, -1.0)
	b.DryMass *= 3
	b.State.M = b.TotalMass()
	for i := 0; i < 2000 && w.LastBumpEvent == nil; i++ {
		w.Tick()
	}
	if w.LastBumpEvent == nil {
		t.Fatal("no bump")
	}
	if got := opening(a, b); math.Abs(got-0.5) > 0.02 {
		t.Errorf("opening = %.3f, want 0.5 with unequal masses", got)
	}
	// The lighter hull took the bigger kick: |dV_a| > |dV_b| when m_a < m_b.
	if a.TotalMass() >= b.TotalMass() {
		t.Fatal("fixture: A should be lighter")
	}
	cm := a.State.V.Scale(a.TotalMass()).Add(b.State.V.Scale(b.TotalMass())).Scale(1 / (a.TotalMass() + b.TotalMass()))
	if math.Abs(a.State.V.Sub(cm).X) <= math.Abs(b.State.V.Sub(cm).X) {
		t.Errorf("lighter hull should move faster relative to the centre of mass")
	}
}

// "No second bump while still overlapping": one caption per contact, however
// many ticks the hulls stay inside each other; a fresh contact bumps again.
func TestBumpIsEdgeTriggeredWhileOverlapping(t *testing.T) {
	w, a, b := bumpWorld(t, 30, -1.0)
	count := 0
	for i := 0; i < 2000 && count == 0; i++ {
		w.Tick()
		if w.LastBumpEvent != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatal("fixture: no first bump")
	}
	// Shove B deep into A's hull and drive it at A again, 60 ticks running.
	b.State.R = a.State.R.Add(orbital.Vec3{X: 3})
	b.State.V = a.State.V.Add(orbital.Vec3{X: -1})
	overlapped := 0
	for i := 0; i < 60; i++ {
		w.LastBumpEvent = nil
		w.Tick()
		if w.LastBumpEvent != nil {
			count++
		}
		if math.Abs(b.State.R.Sub(a.State.R).X) < 6 {
			overlapped++
		}
	}
	if overlapped < 40 {
		t.Fatalf("fixture: hulls were only overlapping %d of 60 ticks", overlapped)
	}
	if count != 1 {
		t.Fatalf("bumps while overlapping = %d, want exactly 1", count)
	}
	// Separate fully, then close again: a new contact bumps again.
	b.State.R = a.State.R.Add(orbital.Vec3{X: 30})
	b.State.V = a.State.V.Add(orbital.Vec3{X: -1})
	for i := 0; i < 2000 && count < 2; i++ {
		w.LastBumpEvent = nil
		w.Tick()
		if w.LastBumpEvent != nil {
			count++
		}
	}
	if count != 2 {
		t.Errorf("a fresh contact after separating should bump again, count = %d", count)
	}
}

// A pair that would pass the dock gate is left to checkBumps' gate hook:
// today the 50 m / 0.1 m/s click, so a 0.05 m/s touch does not bump.
func TestBumpSkipsPairInDockingGate(t *testing.T) {
	// Two RCS tugs, noses pointed at each other along X, the noses
	// overlapping by 0.2 m (hulls touching), closing at 0.05 m/s: port
	// range 0.2 m, aligned, slow, so the port gate holds and the dock
	// takes the touch.
	w, a, _ := bumpWorld(t, 30, 0)
	w.Crafts = w.Crafts[:1]
	mk := func(id uint64, r orbital.Vec3, att orbital.Vec3, v orbital.Vec3) *spacecraft.Spacecraft {
		c := spacecraft.NewFromLoadout("RCS-tug")
		c.Primary = a.Primary
		c.SystemIdx = a.SystemIdx
		c.ID = id
		c.CurrentAttitudeDir = att
		c.State = physics.StateVector{R: r, V: v, M: c.TotalMass()}
		return c
	}
	base := a.State.R.Add(orbital.Vec3{Y: 1000})
	ta := mk(a.ID+200, base, orbital.Vec3{X: 1}, a.State.V)
	ga := VesselPortGeometry(ta)
	tipA := ga.PortPoint(ta.State.R, 0)
	tb := mk(a.ID+201, orbital.Vec3{}, orbital.Vec3{X: -1}, a.State.V.Add(orbital.Vec3{X: -0.05}))
	gb := VesselPortGeometry(tb)
	// Place B so its port sits 0.2 m short of A's port along +X (overlap).
	offB := gb.PortPoint(orbital.Vec3{}, 0) // B's port relative to its R
	tb.State.R = tipA.Add(orbital.Vec3{X: -0.2}).Sub(offB)
	w.Crafts = append(w.Crafts, ta, tb)

	if _, rng, _, _ := dockPairGate(ta, tb); rng > DockPortRangeM {
		t.Fatalf("fixture: port range %.2f m, want <= %.2f", rng, DockPortRangeM)
	}
	if !bumpPairInGate(w, ta, tb) {
		t.Fatal("fixture: the aligned, slow, 0.2 m pair must be inside the port gate")
	}
	w.checkBumps()
	if w.LastBumpEvent != nil {
		t.Error("a pair inside the port gate bumped")
	}
	// The same touch with a re-arm latch up IS a bump: the gate refuses it.
	w.localReArms = append(w.localReArms, localReArm{idA: ta.ID, idB: tb.ID})
	w.checkBumps()
	if w.LastBumpEvent == nil {
		t.Error("a latched (gate-refused) pair should bump on contact")
	}
}

// A magnet pull on the pair also takes the touch, even when the gate's
// terms have lapsed mid-pull (the magnet decides whether to let go).
func TestBumpSkipsPairWhileMagnetPulls(t *testing.T) {
	w, a, b := bumpWorld(t, 4, -0.05) // port-less S-IVBs: the gate never holds
	if bumpPairInGate(w, a, b) {
		t.Fatal("fixture: port-less pair must be outside the gate")
	}
	w.dockMagnet = &dockMagnet{pullerID: b.ID, otherID: a.ID}
	if !bumpPairInGate(w, a, b) {
		t.Error("a pair under a magnet pull must count as in the gate")
	}
	w.dockMagnet = &dockMagnet{pullerID: b.ID, otherID: a.ID + 999}
	if bumpPairInGate(w, a, b) {
		t.Error("a magnet on a different pair must not cover this one")
	}
}

// Opening hulls (a freshly staged stage sitting on its parent) never bump,
// and debris never bumps debris, and Landed vessels never bump.
func TestBumpIgnoresOpeningDebrisAndLanded(t *testing.T) {
	w, a, b := bumpWorld(t, 3, +1.0) // overlapping but parting
	w.checkBumps()
	if w.LastBumpEvent != nil {
		t.Error("parting hulls bumped")
	}
	b.State.V = a.State.V.Add(orbital.Vec3{X: -1})
	a.Role, b.Role = spacecraft.RoleJettisonedStage, spacecraft.RoleJettisonedStage
	w.checkBumps()
	if w.LastBumpEvent != nil {
		t.Error("debris bumped debris")
	}
	a.Role, b.Role = "", ""
	b.Landed = true
	w.checkBumps()
	if w.LastBumpEvent != nil {
		t.Error("a Landed vessel bumped")
	}
	b.Landed = false
	w.checkBumps()
	if w.LastBumpEvent == nil {
		t.Error("control: the same overlap closing at 1 m/s between flying vessels must bump")
	}
}

// Nose-on-nose contact is still a bump today (no gate holds at 1 m/s), and
// the segment test sees hull LENGTH: a hull end-on at 10 m centre-to-centre
// touches because the S-IVB is 18 m long (centres 18 m apart).
func TestBumpContactUsesHullLengthNotCentres(t *testing.T) {
	w, a, b := bumpWorld(t, 40, 0)
	// B ahead of A along +Y (A's nose axis), 25 m up, closing 1 m/s: centres
	// are 25 m apart but A's hull reaches ~17 m up and B's starts at its own R.
	b.State.R = a.State.R.Add(orbital.Vec3{Y: 25})
	b.State.V = a.State.V.Add(orbital.Vec3{Y: -1})
	for i := 0; i < 400 && w.LastBumpEvent == nil; i++ {
		w.Tick()
	}
	if w.LastBumpEvent == nil {
		t.Fatal("end-on hulls never bumped")
	}
	// A's nose is 17.25 m up and B's tail 0.75 m under its R, flat ends: they
	// touch when the nose meets the tail, at a centre distance of 18 m (a
	// capsule would say 24 m, a test from the centres alone 6 m).
	if d := b.State.R.Sub(a.State.R).Norm(); d < 17.8 || d > 18.2 {
		t.Errorf("end-on contact at centre distance %.2f m, want about 18 m", d)
	}
}

// ADR 0053 tests table: "warp selected 100x inside 50 m of another hull
// steps at 10x".
func TestWarpCapsAtTenInsideFiftyMetresOfAnotherHull(t *testing.T) {
	w, a, b := bumpWorld(t, 30, 0)
	w.Clock.WarpIdx = 2 // 100x
	if got := w.EffectiveWarp(); got != 10 {
		t.Errorf("100x selected, 30 m from another hull: effective warp = %v, want 10", got)
	}
	b.State.R = a.State.R.Add(orbital.Vec3{X: 80})
	if got := w.EffectiveWarp(); got != 100 {
		t.Errorf("80 m apart: effective warp = %v, want the selected 100", got)
	}
	// Actually steps at 10x: sim time advances 10 base steps in one tick.
	b.State.R = a.State.R.Add(orbital.Vec3{X: 30})
	before := w.Clock.SimTime
	w.Tick()
	if got := w.Clock.SimTime.Sub(before); got != 10*w.Clock.BaseStep {
		t.Errorf("one tick advanced %v, want 10 base steps (%v)", got, 10*w.Clock.BaseStep)
	}
}

func TestWarpCapIgnoresDebrisPairsAndOtherPrimaries(t *testing.T) {
	w, a, b := bumpWorld(t, 30, 0)
	w.Clock.WarpIdx = 2
	b.Role = spacecraft.RoleJettisonedStage
	if got := w.EffectiveWarp(); got != 100 {
		t.Errorf("ADR 0053 decision 8 says another NON-debris hull: a shed stage 30 m off must not cap, got %v", got)
	}
	a.Role = spacecraft.RoleJettisonedStage
	b.Role = ""
	if got := w.EffectiveWarp(); got != 10 {
		t.Errorf("a shed stage flown next to a real vessel caps: got %v, want 10", got)
	}
	a.Role = ""
	b.Role = ""
	b.Landed = true
	if got := w.EffectiveWarp(); got != 100 {
		t.Errorf("a Landed neighbour must not cap: got %v, want 100", got)
	}
}

// Staging must not read as a bump: the shed stage spawns 60 m back and
// opening, so a normal stage separation never stamps a caption.
func TestBumpNotTriggeredByStaging(t *testing.T) {
	w, _ := NewWorld()
	w.Crafts[0].Stages = nil
	for _, id := range []string{"s-ic", "s-ii"} {
		st, ok := spacecraft.BuildStage(id)
		if !ok {
			t.Fatalf("no part %s", id)
		}
		w.Crafts[0].Stages = append(w.Crafts[0].Stages, st)
	}
	w.Crafts[0].CurrentAttitudeDir = orbital.Vec3{Y: 1}
	if _, _, err := w.StageActive(0); err != nil {
		t.Skipf("staging fixture refused: %v", err)
	}
	for i := 0; i < 400; i++ {
		w.Tick()
		if w.LastBumpEvent != nil {
			t.Fatalf("staging produced a bump caption on tick %d", i)
		}
	}
}

// Measures (and logs, for the PR): the bump's feel at 10x with a 1 m/s approach
// (ADR 0053 "Not verified"). Logs the overlap in ticks and in sim seconds
// at 1x and 10x so the PR can quote it.
func TestBumpFeelAtTenTimesStaysShallow(t *testing.T) {
	for _, idx := range []int{0, 1} {
		w, a, b := bumpWorld(t, 30, -1.0)
		w.Clock.WarpIdx = idx
		bumps, overlapTicks, deepest := 0, 0, 0.0
		var simAtBump, simLeave float64
		start := w.Clock.SimTime
		for i := 0; i < 4000; i++ {
			w.LastBumpEvent = nil
			w.Tick()
			gap := b.State.R.Sub(a.State.R).X
			if w.LastBumpEvent != nil {
				bumps++
				simAtBump = w.Clock.SimTime.Sub(start).Seconds()
			}
			if bumps > 0 && gap < 6 {
				overlapTicks++
				simLeave = w.Clock.SimTime.Sub(start).Seconds()
				if 6-gap > deepest {
					deepest = 6 - gap
				}
			}
			if bumps > 0 && gap > 8 {
				break
			}
		}
		t.Logf("warp %vx: bumps=%d ticks overlapping after the bump=%d, deepest penetration=%.2f m, bump at sim %.2fs, clear at sim %.2fs, opening=%.3f m/s",
			w.Clock.Warp(), bumps, overlapTicks, deepest, simAtBump, simLeave, opening(a, b))
		// The cap is enough at a 1 m/s approach: one bump, a deepest push
		// into the 6 m contact radius under a metre, gone within 3 ticks.
		if bumps != 1 || deepest > 1.0 || overlapTicks > 3 {
			t.Errorf("warp %vx: bumps=%d deepest=%.2f m overlapTicks=%d, want 1 bump, <1 m, <=3 ticks", w.Clock.Warp(), bumps, deepest, overlapTicks)
		}
	}
}

// ---- flat-ended hull contact (PR #643 fix) --------------------------------

// Hulls are flat-ended cylinders, not capsules: radius W/2 applies alongside
// a hull, and two noses touch only when they actually meet axially. S-IVB
// hulls here: 17.25 m + 0.75 m tail, 6 m wide.
func hullPair(t *testing.T, offX, noseGap float64) (a0, a1, b0, b1 orbital.Vec3, wa, wb float64) {
	t.Helper()
	sa := spacecraft.NewFromLoadout("S-IVB-1")
	sa.CurrentAttitudeDir = orbital.Vec3{Y: 1}
	sa.State.R = orbital.Vec3{}
	a0, a1, wa, _ = hullSegment(sa)
	sb := spacecraft.NewFromLoadout("S-IVB-1")
	sb.CurrentAttitudeDir = orbital.Vec3{Y: -1}
	// B hangs above A, nose down: its nose sits noseGap above A's nose.
	sb.State.R = orbital.Vec3{X: offX, Y: a1.Y + noseGap + 17.25}
	b0, b1, wb, _ = hullSegment(sb)
	return
}

func TestHullsCoaxialNosesDoNotTouchAtAnyPositiveGap(t *testing.T) {
	for _, gap := range []float64{5, 2, 1, 0.5, 0.25, 0.1, 0.05} {
		a0, a1, b0, b1, wa, wb := hullPair(t, 0, gap)
		if touch, _ := hullsTouch(a0, a1, wa, b0, b1, wb); touch {
			t.Errorf("coaxial noses %.2f m apart read as touching", gap)
		}
	}
}

func TestHullsPassingNosePastNoseOnlyTouchWithAxialOverlap(t *testing.T) {
	rsum := 6.0
	// Noses level (gap 0) and offset sideways beyond the radii: miss.
	a0, a1, b0, b1, wa, wb := hullPair(t, rsum+0.5, -1) // 1 m axial overlap
	if touch, _ := hullsTouch(a0, a1, wa, b0, b1, wb); touch {
		t.Error("hulls offset 6.5 m sideways touched")
	}
	// Offset inside the radii, with axial overlap: touch.
	a0, a1, b0, b1, wa, wb = hullPair(t, rsum-1, -1)
	if touch, _ := hullsTouch(a0, a1, wa, b0, b1, wb); !touch {
		t.Error("hulls 5 m apart sideways with 1 m axial overlap did not touch")
	}
	// Offset inside the radii but a positive nose gap: noses have not met.
	a0, a1, b0, b1, wa, wb = hullPair(t, rsum-1, 1)
	if touch, _ := hullsTouch(a0, a1, wa, b0, b1, wb); touch {
		t.Error("offset noses 1 m apart axially touched")
	}
}

func TestHullsSideOnHitStillTouchesAtTheSurface(t *testing.T) {
	// A is a vertical hull on the origin; B lies horizontal, its nose pointing
	// into A's flank, 8 m up the hull.
	sa := spacecraft.NewFromLoadout("S-IVB-1")
	sa.CurrentAttitudeDir = orbital.Vec3{Y: 1}
	a0, a1, wa, _ := hullSegment(sa)
	for _, c := range []struct {
		x     float64
		touch bool
	}{{6.5, false}, {5.5, true}} {
		sb := spacecraft.NewFromLoadout("S-IVB-1")
		sb.CurrentAttitudeDir = orbital.Vec3{X: -1}
		// B's nose is 17.25 m from its R along -X.
		sb.State.R = orbital.Vec3{X: c.x + 17.25, Y: 8}
		b0, b1, wb, _ := hullSegment(sb)
		if touch, _ := hullsTouch(a0, a1, wa, b0, b1, wb); touch != c.touch {
			t.Errorf("flank hit with nose %.1f m from the axis: touch = %v, want %v", c.x, touch, c.touch)
		}
	}
}

// Nose to nose at 1 m/s, outside any gate (the latch stands in for B's
// gate): the noses meet and the pair bounces, opening at 0.5 m/s.
func TestBumpNoseToNoseHeadOnOpensAtHalf(t *testing.T) {
	w, a, b := bumpWorld(t, 0, 0)
	b.State.R = a.State.R.Add(orbital.Vec3{Y: 60})
	b.CurrentAttitudeDir = orbital.Vec3{Y: -1}
	b.State.V = a.State.V.Add(orbital.Vec3{Y: -1})
	w.localReArms = append(w.localReArms, localReArm{idA: a.ID, idB: b.ID})
	var gapAtBump float64
	// Stepped by hand: World.Tick would re-point both hulls prograde (their
	// attitude mode), which is a different fixture. Positions move at the
	// relative velocity, 50 ms a step, and the contact test runs each step.
	for i := 0; i < 3000 && w.LastBumpEvent == nil; i++ {
		a.State.R = a.State.R.Add(a.State.V.Scale(0.05))
		b.State.R = b.State.R.Add(b.State.V.Scale(0.05))
		w.checkBumps()
	}
	if w.LastBumpEvent == nil {
		t.Fatal("nose-to-nose hulls passed through each other")
	}
	// A's nose is at R+17.25; B's nose is at B.R-17.25 (it points down).
	gapAtBump = (b.State.R.Y - 17.25) - (a.State.R.Y + 17.25)
	if gapAtBump > 0.1 || gapAtBump < -0.3 {
		t.Errorf("bumped with the noses %.2f m apart (negative = interpenetrating), want the moment they meet", gapAtBump)
	}
	if got := b.State.V.Sub(a.State.V).Y; math.Abs(got-0.5) > 0.03 {
		t.Errorf("opening after the nose-to-nose bump = %.3f m/s, want 0.5", got)
	}
}

// Feel of the nose-to-nose bump (flat ends): the noses meet and interpenetrate
// by at most one step of travel (1 m/s x 0.05 s at 1x, x10 at 10x) before the
// bump opens the pair. Stepped by hand as in the head-on test above.
func TestBumpNoseToNoseFeelAtTenTimesStaysShallow(t *testing.T) {
	for _, step := range []float64{0.05, 0.5} {
		w, a, b := bumpWorld(t, 0, 0)
		b.State.R = a.State.R.Add(orbital.Vec3{Y: 60.23})
		b.CurrentAttitudeDir = orbital.Vec3{Y: -1}
		b.State.V = a.State.V.Add(orbital.Vec3{Y: -1})
		w.localReArms = append(w.localReArms, localReArm{idA: a.ID, idB: b.ID})
		bumps, ticksIn, deepest := 0, 0, 0.0
		for i := 0; i < 3000; i++ {
			a.State.R = a.State.R.Add(a.State.V.Scale(step))
			b.State.R = b.State.R.Add(b.State.V.Scale(step))
			w.LastBumpEvent = nil
			w.checkBumps()
			if w.LastBumpEvent != nil {
				bumps++
			}
			gap := (b.State.R.Y - 17.25) - (a.State.R.Y + 17.25)
			if bumps > 0 && gap < 0 {
				ticksIn++
				deepest = math.Max(deepest, -gap)
			}
			if bumps > 0 && gap > 2 {
				break
			}
		}
		t.Logf("step %.2f s (%vx): bumps=%d steps interpenetrating=%d deepest=%.2f m opening=%.3f m/s", step, step/0.05, bumps, ticksIn, deepest, b.State.V.Sub(a.State.V).Y)
		if bumps != 1 || deepest > 1.0 {
			t.Errorf("step %.2f: bumps=%d deepest=%.2f m, want 1 bump and < 1 m", step, bumps, deepest)
		}
	}
}

// A slow touch outside the port gate is a bump: under the old 50 m /
// 0.1 m/s click any slow pair within 50 m counted as docking, so two
// port-less stages drifting into each other at 0.05 m/s passed through.
func TestBumpSlowTouchOutsidePortGateBumps(t *testing.T) {
	w, a, b := bumpWorld(t, 4, -0.05) // port-less S-IVBs side by side, 4 m apart, closing
	if bumpPairInGate(w, a, b) {
		t.Fatal("fixture: a port-less pair must be outside the gate")
	}
	w.checkBumps()
	if w.LastBumpEvent == nil {
		t.Fatal("a slow touch between port-less hulls must bump")
	}
	if got := opening(a, b); got < 0.05-1e-9 {
		t.Errorf("opening after a 0.05 m/s touch = %.3f m/s, want the 0.05 m/s floor", got)
	}
}
