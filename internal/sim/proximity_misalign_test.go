package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// leanOwnNose points the active vessel's nose at -target axis, tipped upDeg
// toward the port face's up and rightDeg toward its right (ADR 0055
// decision 3 sign convention).
func leanOwnNose(t *testing.T, w *World, upDeg, rightDeg float64) {
	t.Helper()
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("no proximity state")
	}
	up, right, ok := PortFaceBasis(st.Target.Axis, st.Frame)
	if !ok {
		t.Fatal("no port face basis")
	}
	u, r := upDeg*math.Pi/180, rightDeg*math.Pi/180
	w.ActiveCraft().CurrentAttitudeDir = st.Target.Axis.Scale(-1).
		Add(up.Scale(math.Tan(u))).Add(right.Scale(math.Tan(r))).Unit()
}

// ADR 0055 decision 3: the misalign pair is the nose's lean across the port
// face, positive up / right. A sabotage that derives the pair from the
// surface horizon, or flips a sign, fails here.
func TestMisalignPairIsLeanAcrossThePortFace(t *testing.T) {
	cases := []struct{ up, right float64 }{{0, 0}, {3, 0}, {0, -2}, {-3, 2}}
	for _, c := range cases {
		w := portPairWorld(t)
		leanOwnNose(t, w, c.up, c.right)
		st, ok := w.ProximityState()
		if !ok || !st.HasPort {
			t.Fatal("fixture has no port pair")
		}
		if math.Abs(st.MisalignUpDeg-c.up) > 0.01 || math.Abs(st.MisalignRightDeg-c.right) > 0.01 {
			t.Errorf("lean (%v up, %v right): pair = (%.3f, %.3f)", c.up, c.right, st.MisalignUpDeg, st.MisalignRightDeg)
		}
		// AlignDeg is the angle between the axes: consistent with the pair.
		want := math.Hypot(c.up, c.right)
		if math.Abs(st.AlignDeg-want) > 0.05 {
			t.Errorf("lean (%v, %v): AlignDeg = %.3f, want about %.3f", c.up, c.right, st.AlignDeg, want)
		}
	}
}

// Without a port pair the pair is zero (it is meaningful only with HasPort).
func TestMisalignPairZeroWithoutPorts(t *testing.T) {
	w := portPairWorld(t)
	w.Crafts[1].Stages = []spacecraft.Stage{stageOf(t, "s-ivb-1")}
	w.Crafts[1].SyncFields()
	st, _ := w.ProximityState()
	if st.HasPort || st.MisalignUpDeg != 0 || st.MisalignRightDeg != 0 {
		t.Errorf("portless pair: HasPort=%v pair=(%v,%v)", st.HasPort, st.MisalignUpDeg, st.MisalignRightDeg)
	}
}

// ADR 0063 §1 (supersedes ADR 0055's Prograde): PORT dropping resets a Port
// hold to Inertial; any other hold is left alone.
func TestReconcileDropResetsPortHoldToInertial(t *testing.T) {
	for _, m := range []spacecraft.BurnMode{spacecraft.BurnPortIn, spacecraft.BurnPortOut, spacecraft.BurnPortUp,
		spacecraft.BurnPortDown, spacecraft.BurnPortLeft, spacecraft.BurnPortRight} {
		w := portPairWorld(t)
		w.NavMode = NavPort
		w.ActiveCraft().AttitudeMode = m
		w.ClearTarget()
		if got := w.ActiveCraft().AttitudeMode; got != spacecraft.BurnInertial {
			t.Errorf("hold %v after PORT dropped = %v, want Inertial", m, got)
		}
	}
	// A non-port hold survives the drop.
	w := portPairWorld(t)
	w.NavMode = NavPort
	w.ActiveCraft().AttitudeMode = spacecraft.BurnRetrograde
	w.ClearTarget()
	if got := w.ActiveCraft().AttitudeMode; got != spacecraft.BurnRetrograde {
		t.Errorf("Retrograde hold changed to %v on PORT drop", got)
	}
	// Dropping through a tick (the target sheds its port) resets too.
	w = portPairWorld(t)
	w.NavMode = NavPort
	w.ActiveCraft().AttitudeMode = spacecraft.BurnPortIn
	w.Crafts[1].Stages = []spacecraft.Stage{stageOf(t, "s-ivb-1")}
	w.Crafts[1].SyncFields()
	w.Tick()
	if got := w.ActiveCraft().AttitudeMode; got != spacecraft.BurnInertial {
		t.Errorf("hold after target shed its port = %v, want Inertial", got)
	}
}

// `;` reaching PORT never sets a hold on any vessel (ADR 0055 amendment).
func TestCycleNavModeIntoPortSetsNoHold(t *testing.T) {
	for _, part := range []string{"rcs-tug", "command-module"} {
		w := portPairWorld(t)
		a := w.ActiveCraft()
		a.Stages = []spacecraft.Stage{stageOf(t, part)}
		a.SyncFields()
		a.AttitudeMode = spacecraft.BurnPrograde
		w.NavMode = NavTarget
		if got := w.CycleNavMode(); got != NavPort {
			t.Fatalf("%s: cycle gave %v, want PORT", part, got)
		}
		if a.AttitudeMode != spacecraft.BurnPrograde {
			t.Errorf("%s: reaching PORT set hold %v", part, a.AttitudeMode)
		}
	}
	_ = orbital.Vec3{}
}
