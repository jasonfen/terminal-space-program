package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #702: a planted burn with a normal or radial part holds the inertial
// direction it was planned in for its whole length; prograde and
// retrograde keep following the velocity. The planner, the chained
// readouts and the executor all fly the same rule, so what the HUD shows
// after a node is the orbit the burn leaves.

type plantedBurnRun struct {
	ignition     orbital.Vec3 // unit thrust direction at the first burning tick
	maxHoldDeg   float64      // largest angle between the nose and the ignition direction while burning
	maxFollowDeg float64      // largest angle between the nose and the velocity (negated for a retrograde burn) while burning
	swingDeg     float64      // angle between the nose at the first and last burning tick
	predicted    physics.StateVector
	flown        physics.StateVector // flown state carried to the node's epoch along its orbit
}

func angleDeg(a, b orbital.Vec3) float64 {
	c := a.Unit().Dot(b.Unit())
	return math.Acos(math.Max(-1, math.Min(1, c))) * 180 / math.Pi
}

// flyPlantedBurn plants one finite node of mode and dv ten minutes ahead on
// the default vessel and flies it tick by tick.
func flyPlantedBurn(t *testing.T, mode spacecraft.BurnMode, dv float64) plantedBurnRun {
	t.Helper()
	w := mustWorld(t)
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	n := spacecraft.ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(10 * time.Minute),
		Mode:        mode, DV: dv, Duration: c.BurnTimeForDV(dv), PrimaryID: c.Primary.ID,
	}
	if n.Duration < 60*time.Second {
		t.Fatalf("burn of %v is too short to tell a held direction from a followed one", n.Duration)
	}
	w.PlanNode(n)
	var run plantedBurnRun
	run.predicted, _ = w.PostBurnState(c.Nodes[0])
	if _, ok := tickUntil(w, 100_000, func() bool { return c.ActiveBurn != nil }); !ok {
		t.Fatal("the node never ignited")
	}
	var first, last orbital.Vec3
	for i := 0; c.ActiveBurn != nil && i < 100_000; i++ {
		if i == 0 {
			run.ignition = c.CurrentAttitudeDir.Unit()
			first = run.ignition
		}
		nose := c.CurrentAttitudeDir.Unit()
		run.maxHoldDeg = math.Max(run.maxHoldDeg, angleDeg(nose, run.ignition))
		along := c.State.V
		if mode == spacecraft.BurnRetrograde {
			along = along.Scale(-1)
		}
		run.maxFollowDeg = math.Max(run.maxFollowDeg, angleDeg(nose, along))
		last = nose
		w.Tick()
	}
	if c.ActiveBurn != nil {
		t.Fatal("the burn never finished")
	}
	run.swingDeg = angleDeg(first, last)
	end, ok := physics.KeplerStep(c.State, mu, n.TriggerTime.Sub(w.Clock.SimTime).Seconds())
	if !ok {
		t.Fatal("cannot carry the flown state to the node epoch")
	}
	run.flown = end
	return run
}

// TestPlantedNormalAndRadialBurnsHoldTheirDirection: the nose stays on the
// direction it had at ignition for the whole burn, though the velocity (and
// so the orbit normal and the radial) turns under it.
func TestPlantedNormalAndRadialBurnsHoldTheirDirection(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode spacecraft.BurnMode
		dv   float64
	}{
		{"Normal+", spacecraft.BurnNormalPlus, 1500},
		{"Normal-", spacecraft.BurnNormalMinus, 1500},
		{"Radial+", spacecraft.BurnRadialOut, 1000},
		{"Radial-", spacecraft.BurnRadialIn, 1000},
	} {
		run := flyPlantedBurn(t, tc.mode, tc.dv)
		t.Logf("%s: nose off its ignition direction by at most %.2f deg, off the velocity by %.1f deg", tc.name, run.maxHoldDeg, run.maxFollowDeg)
		if run.maxHoldDeg > 1.0 {
			t.Errorf("%s: the nose left its ignition direction by %.2f deg; a normal or radial burn holds it", tc.name, run.maxHoldDeg)
		}
	}
}

// TestPlantedProgradeAndRetrogradeBurnsStillFollowTheVelocity: the same
// flight for the modes the rule leaves alone. Without this the hold could
// "pass" by freezing every burn.
func TestPlantedProgradeAndRetrogradeBurnsStillFollowTheVelocity(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode spacecraft.BurnMode
	}{{"Prograde", spacecraft.BurnPrograde}, {"Retrograde", spacecraft.BurnRetrograde}} {
		run := flyPlantedBurn(t, tc.mode, 1500)
		t.Logf("%s: nose off the velocity by at most %.2f deg, swung %.1f deg over the burn", tc.name, run.maxFollowDeg, run.swingDeg)
		if run.maxFollowDeg > 1.0 {
			t.Errorf("%s: the nose left the velocity by %.2f deg; a prograde or retrograde burn follows it", tc.name, run.maxFollowDeg)
		}
		if run.swingDeg < 3 {
			t.Errorf("%s: the nose swung only %.1f deg over the burn; it follows the velocity round the orbit", tc.name, run.swingDeg)
		}
		if run.maxHoldDeg < 3 {
			t.Errorf("%s: the nose stayed within %.1f deg of its ignition direction; it should turn with the velocity", tc.name, run.maxHoldDeg)
		}
	}
}

// TestPredictedNodeStateIsTheFlownOne: for each mode the chained readouts'
// post-burn state (PostBurnState, walkPlantedChain's nodeBurnState) is the
// state the flown burn leaves, carried to the node epoch.
func TestPredictedNodeStateIsTheFlownOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode spacecraft.BurnMode
		dv   float64
	}{
		{"Prograde", spacecraft.BurnPrograde, 1500},
		{"Retrograde", spacecraft.BurnRetrograde, 1000},
		{"Normal+", spacecraft.BurnNormalPlus, 1500},
		{"Normal-", spacecraft.BurnNormalMinus, 1500},
		{"Radial+", spacecraft.BurnRadialOut, 1000},
		{"Radial-", spacecraft.BurnRadialIn, 1000},
	} {
		run := flyPlantedBurn(t, tc.mode, tc.dv)
		dV := run.predicted.V.Sub(run.flown.V).Norm()
		dR := run.predicted.R.Sub(run.flown.R).Norm()
		t.Logf("%s: predicted vs flown at the node epoch: velocity %.2f m/s, position %.2f km", tc.name, dV, dR/1e3)
		if dV > 3 || dR > 10e3 {
			t.Errorf("%s: predicted post-burn state is %.2f m/s and %.1f km from the flown one", tc.name, dV, dR/1e3)
		}
	}
}

// TestPlaneChangeBurnKeepsItsSpeedAndDirectionInFlight: a planted plane
// change delivers its planted delta-v along the direction the node gives at
// its epoch and so keeps the speed; the velocity-following burn it replaces
// bled it (57 deg of a 1500 m/s velocity: 1500 -> about 1000 m/s).
func TestPlaneChangeBurnKeepsItsSpeedAndDirectionInFlight(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	// Far from the planet (gravity moves the velocity by a few m/s over
	// the burn) with the delta-v a large part of the speed.
	c.State.V = c.State.V.Unit().Scale(1500)
	c.State.R = c.State.R.Unit().Scale(200e6)
	theta := 57 * math.Pi / 180
	dv := 2 * 1500 * math.Sin(theta/2)
	w.PlanNode(spacecraft.ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(10 * time.Minute), Mode: spacecraft.BurnPlaneChange, DV: dv,
		Duration: c.BurnTimeForDV(dv), PrimaryID: c.Primary.ID, PlaneChangeRad: theta,
	})
	if _, ok := tickUntil(w, 1_000_000, func() bool { return c.ActiveBurn != nil }); !ok {
		t.Fatal("the plane change never ignited")
	}
	vStart := c.State.V
	ign := c.ActiveBurn.BurnDirUnit
	if ign.Norm() == 0 {
		t.Fatal("the plane change resolved no held direction at ignition")
	}
	if _, ok := tickUntil(w, 1_000_000, func() bool { return c.ActiveBurn == nil }); !ok {
		t.Fatal("the plane change never finished")
	}
	got := c.State.V.Sub(vStart)
	t.Logf("|v| 1500 -> %.0f m/s; delivered %.0f of %.0f m/s, %.1f deg off the held direction", c.State.V.Norm(), got.Norm(), dv, angleDeg(got, ign))
	if math.Abs(c.State.V.Norm()-1500) > 0.03*1500 {
		t.Errorf("plane change left |v| = %.0f m/s from 1500: a rotation keeps the speed", c.State.V.Norm())
	}
	if a := angleDeg(got, ign); a > 3 {
		t.Errorf("delivered delta-v is %.1f deg off the planted direction", a)
	}
}

// TestNodePlannedInEarthsFrameBurnsInEarthsTermsInsideTheMoonsSOI (#701):
// a node planted in Earth's frame that ignites after the vessel has crossed
// into the Moon's SOI resolves its held direction from the Earth-relative
// state, the frame the planner and the chained readouts used, not from the
// Moon-relative velocity (which would rotate a different velocity).
func TestNodePlannedInEarthsFrameBurnsInEarthsTermsInsideTheMoonsSOI(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	mi := moonIndex(w)
	moon := w.System().Bodies[mi]
	earth := c.Primary
	now := w.Clock.SimTime
	moonR := w.BodyPositionAt(moon, now).Sub(w.BodyPositionAt(earth, now))
	// 50,000 km from the Moon (inside its 66,000 km SOI), 300 m/s across
	// the Earth-Moon line.
	rel := physics.StateVector{R: moonR.Unit().Cross(orbital.Vec3{Z: 1}).Unit().Scale(50e6), V: moonR.Unit().Scale(300), M: c.State.M}
	c.Primary = moon
	c.State = rel
	theta := 0.9
	dv := 100.0
	n := spacecraft.ManeuverNode{
		TriggerTime: now.Add(10 * time.Minute), Mode: spacecraft.BurnPlaneChange, DV: dv,
		Duration: 20 * time.Second, PrimaryID: earth.ID, PlaneChangeRad: theta,
	}
	c.Nodes = append(c.Nodes, n)
	c.Thrust, c.Isp = 5000, 300
	if _, ok := tickUntil(w, 100_000, func() bool { return c.ActiveBurn != nil }); !ok {
		t.Fatal("the node never ignited")
	}
	if c.Primary.ID != "moon" {
		t.Fatalf("the vessel left the Moon's frame (%s): the setup does not test a burn inside the SOI", c.Primary.ID)
	}
	got := c.ActiveBurn.BurnDirUnit
	// Independent expectation: the Earth-relative state now, carried to the
	// node epoch, in Earth's frame.
	at := w.Clock.SimTime
	eR := c.State.R.Add(w.BodyPositionAt(moon, at).Sub(w.BodyPositionAt(earth, at)))
	eV := c.State.V.Add(w.bodyInertialVelocityAt(moon, at).Sub(w.bodyInertialVelocityAt(earth, at)))
	es, ok := physics.KeplerStep(physics.StateVector{R: eR, V: eV, M: c.State.M}, earth.GravitationalParameter(), n.TriggerTime.Sub(at).Seconds())
	if !ok {
		t.Fatal("kepler step failed")
	}
	want := spacecraft.NodeBurnDirection(n, es.R, es.V)
	inMoon := spacecraft.NodeBurnDirection(n, c.State.R, c.State.V)
	t.Logf("held direction %.2f deg from the Earth-frame direction, %.1f deg from the Moon-frame one", angleDeg(got, want), angleDeg(got, inMoon))
	if angleDeg(want, inMoon) < 5 {
		t.Fatalf("setup: the Earth-frame and Moon-frame directions agree to %.1f deg, the test cannot tell them apart", angleDeg(want, inMoon))
	}
	if a := angleDeg(got, want); a > 1 {
		t.Errorf("the plane change planted in Earth's frame resolved its direction %.1f deg from Earth's frame's (and %.1f deg from the Moon-frame direction)", a, angleDeg(got, inMoon))
	}
}

// TestPlanChainMemoCoversTheHoldRule: the chain memo's key must cover what
// the hold rule reads. Changing a node's mode from a velocity-following one
// to a held one inside a frame re-walks the chain and predicts a different
// post-burn state, rather than replaying the old walk.
func TestPlanChainMemoCoversTheHoldRule(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	dv := 1500.0
	w.PlanNode(spacecraft.ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(10 * time.Minute), Mode: spacecraft.BurnPrograde, DV: dv,
		Duration: c.BurnTimeForDV(dv), PrimaryID: c.Primary.ID,
	})
	final := func() physics.StateVector {
		st, _, ok := w.PredictedFinalOrbit()
		if !ok {
			t.Fatal("no predicted final orbit")
		}
		return st
	}
	a := final()
	final()
	base := w.PlanChainWalks()
	c.Nodes[0].Mode = spacecraft.BurnNormalPlus
	b := final()
	if w.PlanChainWalks() != base+1 {
		t.Errorf("a mode change did not re-walk the chain (walks %d, want %d): stale memo", w.PlanChainWalks(), base+1)
	}
	if b.V.Sub(a.V).Norm() < 100 {
		t.Errorf("a prograde and a normal burn predict states %.1f m/s apart: the walk is not reading the mode", b.V.Sub(a.V).Norm())
	}
	base = w.PlanChainWalks()
	c.Nodes[0].PlaneChangeRad = 0.3
	c.Nodes[0].Mode = spacecraft.BurnPlaneChange
	final()
	if w.PlanChainWalks() != base+1 {
		t.Errorf("a plane-change angle change did not re-walk the chain: stale memo")
	}
}
