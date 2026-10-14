package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0053 slice B: docking is a flown port capture, not a 50 m click.
// These tests drive the real Tick (integration, slew, checkDocking) rather
// than calling the scan directly, so a gate that only holds in isolation
// cannot pass.

// portPair is a two-vessel world: A (active, nose along +Y = prograde) and B
// facing it, port to port, gapM apart along A's axis. tiltDeg tips B's nose
// off straight about the world Z axis. relMS closes B on A along the axis.
type portPair struct {
	w    *World
	a, b *spacecraft.Spacecraft
	axis orbital.Vec3 // A's nose direction
}

func newPortPair(t *testing.T, partA, partB string, gapM, relMS, tiltDeg float64) *portPair {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	earth := w.Systems[0].FindBody("Earth")
	a := spacecraft.NewFromStages([]spacecraft.Stage{stageOf(t, partA)})
	b := spacecraft.NewFromStages([]spacecraft.Stage{stageOf(t, partB)})
	a.Name, b.Name = "Alpha", "Bravo"
	a.Primary, b.Primary = *earth, *earth

	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}
	axis := orbital.Vec3{Y: 1}
	a.State = physics.StateVector{R: r, V: v, M: a.TotalMass()}
	a.CurrentAttitudeDir = axis

	bAxis := orbital.Rotate(axis.Scale(-1), orbital.Vec3{Z: 1}, tiltDeg*math.Pi/180).Unit()
	b.CurrentAttitudeDir = bAxis
	// B holds the nose exactly as placed: a SAS slew back to straight would
	// heal a tilt within a second and hide what the magnet itself does.
	b.SlewRateDegPerSec = 1e-9
	// The nose edge a port would sit on, whether or not the part has one.
	noseEdge := func(st []spacecraft.Stage) float64 {
		return float64(StackRows(st))*DockStrideM - dockPortTipInsetM
	}
	pa := r.Add(axis.Scale(noseEdge(a.Stages)))
	offsetB := noseEdge(b.Stages)
	// B's port sits gapM ahead of A's port.
	bR := pa.Add(axis.Scale(gapM)).Sub(bAxis.Scale(offsetB))
	b.State = physics.StateVector{R: bR, V: v.Sub(axis.Scale(relMS)), M: b.TotalMass()}

	w.Crafts = []*spacecraft.Spacecraft{a, b}
	w.ActiveCraftIdx = 0
	w.stampCraftID(a)
	w.stampCraftID(b)
	return &portPair{w: w, a: a, b: b, axis: axis}
}

// tickFor runs the world forward by sim seconds (1x warp, 50 ms steps).
func tickFor(w *World, seconds float64) {
	end := w.Clock.SimTime.Add(time.Duration(seconds * float64(time.Second)))
	for w.Clock.SimTime.Before(end) {
		w.Tick()
	}
}

// Two ported hulls 30 m apart with their noses opposed and no relative
// motion are inside the OLD 50 m / 0.1 m/s click and would have fused on the
// next tick. Nothing docks by itself at a distance now.
func TestDockNoClickAtThirtyMetresNosesOpposed(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 30, 0, 0)
	tickFor(p.w, 60)
	if len(p.w.Crafts) != 2 {
		t.Fatalf("slate = %d vessels after 60 s, want 2: a 30 m pair docked at a distance", len(p.w.Crafts))
	}
}

// Port-less hulls never dock, however well they sit: two bare S-IVBs port to
// port at 0.3 m, aligned, at rest.
func TestDockPortlessPairNeverDocks(t *testing.T) {
	p := newPortPair(t, "s-ivb-1", "s-ivb-1", 0.3, 0, 0)
	tickFor(p.w, 20)
	if len(p.w.Crafts) != 2 {
		t.Fatalf("slate = %d vessels, want 2: two port-less stages docked", len(p.w.Crafts))
	}
}

// The composite stays where the lead's hull was: no mass-centroid jump.
func TestDockCompositeKeepsLeadPosition(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
	lead := p.a.State.R
	p.w.DockCrafts(0, 1)
	if len(p.w.Crafts) != 1 {
		t.Fatalf("DockCrafts left %d vessels", len(p.w.Crafts))
	}
	if d := p.w.Crafts[0].State.R.Sub(lead).Norm(); d > 1e-6 {
		t.Errorf("composite moved %.3f m from the lead's position (centroid jump)", d)
	}
}

// A ported pair at 0.4 m / 0.05 m/s / 2 degrees holds the gate: the magnet
// starts, and the dock is the magnet completing five sim-seconds later, not
// the tick the gate first holds.
func TestDockMagnetFiveSecondsThenOneComposite(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	tickFor(p.w, 0.2)
	if p.w.dockMagnet == nil {
		t.Fatal("gate holds (0.4 m, 0.05 m/s, 2 deg) but no magnet started")
	}
	if len(p.w.Crafts) != 2 {
		t.Fatalf("docked the tick the gate held (%d vessels): the dock must wait for the pull", len(p.w.Crafts))
	}
	tickFor(p.w, 3.8) // 4.0 s of sim since the start
	if len(p.w.Crafts) != 2 {
		t.Fatalf("docked after ~4 s of pull (%d vessels), want the full 5 s", len(p.w.Crafts))
	}
	tickFor(p.w, 1.3) // ~5.3 s
	if len(p.w.Crafts) != 1 {
		t.Fatalf("slate = %d after ~5.3 s, want one composite", len(p.w.Crafts))
	}
	if p.w.dockMagnet != nil {
		t.Error("magnet still set after the dock")
	}
	if p.w.LastDockEvent == nil || p.w.LastDockEvent.PartnerName != "Bravo" {
		t.Errorf("LastDockEvent = %+v, want a dock with Bravo", p.w.LastDockEvent)
	}
}

// Each term of the gate refuses on its own.
func TestDockGateEachTermRefuses(t *testing.T) {
	cases := []struct {
		name           string
		gap, rel, tilt float64
		wantMagnet     bool
		note           string
	}{
		{"all inside", 0.4, 0.05, 2, true, ""},
		{"port range 0.6 m", 0.6, 0.05, 2, false, "range term"},
		{"rel speed 0.15 m/s", 0.4, 0.15, 2, false, "speed term"},
		{"6 deg off", 0.4, 0.05, 6, false, "alignment term"},
	}
	for _, c := range cases {
		p := newPortPair(t, "rcs-tug", "rcs-tug", c.gap, c.rel, c.tilt)
		// One tick only: a refused pair must not start a pull (the slew heals
		// a tilt within a second, so look before it does).
		p.w.Tick()
		if got := p.w.dockMagnet != nil; got != c.wantMagnet {
			t.Errorf("%s: magnet started = %v, want %v (%s)", c.name, got, c.wantMagnet, c.note)
		}
	}
}

// The magnet moves one vessel: the one being flown (the active one). The
// other is left alone, and the puller ends port to port with its nose onto
// the port axis.
func TestDockMagnetMovesOnlyThePuller(t *testing.T) {
	pull := newPortPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	// A SAS-held at radial-out: the slew would drag it 90 deg off the port
	// axis if the magnet did not own its nose.
	pull.a.AttitudeMode = spacecraft.BurnRadialOut
	// Control: the identical pair, but two pieces of debris never dock each
	// other, so nothing pulls and B flies its own trajectory untouched.
	ctrl := newPortPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	ctrl.a.Role, ctrl.b.Role = spacecraft.RoleJettisonedStage, spacecraft.RoleJettisonedStage
	// Run the pulled pair up to just short of the dock; the control the same.
	tickFor(pull.w, 4.5)
	tickFor(ctrl.w, 4.5)
	if pull.w.dockMagnet == nil {
		t.Fatal("no magnet running at 4.5 s")
	}
	// Other vessel: the same trajectory as the control, to a micrometre.
	if d := pull.b.State.R.Sub(ctrl.b.State.R).Norm(); d > 1e-6 {
		t.Errorf("the other vessel was moved by %.6f m: the magnet must leave it alone", d)
	}
	// Puller: gap closed most of the way, nose near the port axis.
	ga, gb := VesselPortGeometry(pull.a), VesselPortGeometry(pull.b)
	gap := gb.PortPoint(pull.b.State.R, 0).Sub(ga.PortPoint(pull.a.State.R, 0)).Norm()
	if gap > 0.1 {
		t.Errorf("port gap %.3f m at 4.5 s of a 5 s pull, want it nearly closed", gap)
	}
	if al := PortAlignmentDeg(ga.Axis, gb.Axis); al > 0.5 {
		t.Errorf("alignment %.2f deg at 4.5 s of the pull, want the nose nearly on the port axis", al)
	}
}

// During the pull the puller's hands are off: RCS pulses and attitude
// commands are ignored, and warp is held at 1x.
func TestDockMagnetIgnoresHandsAndHoldsWarp(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	tickFor(p.w, 0.3)
	if p.w.dockMagnet == nil {
		t.Fatal("no magnet")
	}
	p.a.EngineMode = spacecraft.EngineRCS
	v0 := p.a.State.V
	mode0 := p.a.AttitudeMode
	if p.w.FireRCSPulse(spacecraft.BurnRadialOut) {
		t.Error("FireRCSPulse succeeded on the puller mid-pull")
	}
	if p.a.State.V != v0 {
		t.Errorf("RCS pulse changed the puller's velocity mid-pull: %+v -> %+v", v0, p.a.State.V)
	}
	p.w.SetAttitudeMode(spacecraft.BurnRadialIn)
	if p.a.AttitudeMode != mode0 {
		t.Errorf("attitude key changed the puller's mode mid-pull: %v -> %v", mode0, p.a.AttitudeMode)
	}
	p.w.Clock.WarpIdx = 4 // 10000x selected
	if got := p.w.clampedWarp(); got != 1 {
		t.Errorf("clampedWarp = %v during the pull, want 1", got)
	}
}

// If the other vessel leaves the gate mid-pull (here it slews 10 degrees off
// straight), the magnet lets go: both are back in free flight, nothing fused.
func TestDockMagnetLetsGoWhenTargetSlewsOut(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	tickFor(p.w, 2)
	if p.w.dockMagnet == nil {
		t.Fatal("no magnet at 2 s")
	}
	// Turn B 10 deg about its own port, so only the alignment term moves.
	gb := VesselPortGeometry(p.b)
	pivot := gb.PortPoint(p.b.State.R, 0)
	p.b.CurrentAttitudeDir = orbital.Rotate(p.b.CurrentAttitudeDir, orbital.Vec3{Z: 1}, 10*math.Pi/180).Unit()
	p.b.State.R = p.b.State.R.Add(pivot.Sub(VesselPortGeometry(p.b).PortPoint(p.b.State.R, 0)))
	p.w.Tick()
	if p.w.dockMagnet != nil {
		t.Error("magnet still pulling after the target slewed 10 deg out of the gate")
	}
	if len(p.w.Crafts) != 2 {
		t.Errorf("slate = %d after the target left the gate, want 2: it docked anyway", len(p.w.Crafts))
	}
}

// ...and the same if the other vessel pulses away (relative speed past 0.1).
func TestDockMagnetLetsGoWhenTargetPulsesAway(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	tickFor(p.w, 2)
	if p.w.dockMagnet == nil {
		t.Fatal("no magnet at 2 s")
	}
	p.b.State.V = p.b.State.V.Add(p.axis.Scale(0.5))
	p.w.Tick()
	if p.w.dockMagnet != nil {
		t.Error("magnet still pulling after the target pulsed away at 0.5 m/s")
	}
}

// The pulled vessel is the lighter one when the active vessel is not in the
// pair.
func TestDockMagnetPullsLighterWhenActiveIsNotInPair(t *testing.T) {
	p := newPortPair(t, "ntr-tug", "rcs-tug", 0.4, 0.05, 2)
	third := spacecraft.NewFromStages([]spacecraft.Stage{stageOf(t, "rcs-tug")})
	third.Primary = p.a.Primary
	third.State = physics.StateVector{R: p.a.State.R.Add(orbital.Vec3{X: 50000}), V: p.a.State.V, M: third.TotalMass()}
	p.w.Crafts = append(p.w.Crafts, third)
	p.w.stampCraftID(third)
	p.w.ActiveCraftIdx = 2
	tickFor(p.w, 0.2)
	if p.w.dockMagnet == nil {
		t.Fatal("no magnet for a pair the active vessel is not in")
	}
	lighter := p.b
	if p.a.TotalMass() < p.b.TotalMass() {
		lighter = p.a
	}
	if p.w.dockMagnet.pullerID != lighter.ID {
		t.Errorf("puller = %d, want the lighter vessel %d", p.w.dockMagnet.pullerID, lighter.ID)
	}
}

// placePortToPort puts b ahead of a, port to port gapM apart with its nose
// facing back at a, and matches a's velocity: the state a pair "drifting back
// together" is in. a's nose must be initialised.
func placePortToPort(a, b *spacecraft.Spacecraft, gapM float64) {
	axis := a.CurrentAttitudeDir.Unit()
	noseEdge := func(st []spacecraft.Stage) float64 {
		return float64(StackRows(st))*DockStrideM - dockPortTipInsetM
	}
	b.CurrentAttitudeDir = axis.Scale(-1)
	b.SlewRateDegPerSec = 1e-9 // hold the facing: a SAS slew to the default would turn it away
	b.State.R = a.State.R.Add(axis.Scale(noseEdge(a.Stages) + gapM + noseEdge(b.Stages)))
	b.State.V = a.State.V
}

// startsMagnet runs one dock scan and reports whether it began a capture,
// which is what "would have fused" means now that the dock is the magnet
// completing.
func startsMagnet(w *World) bool {
	w.checkDocking()
	return w.dockMagnet != nil
}
