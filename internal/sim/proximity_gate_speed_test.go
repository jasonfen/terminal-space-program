package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0055 slice 0: the dock gate's speed term, the chip's rel speed: and the
// ring all read the port-point relative velocity. These tests use two
// Apollo-TLI stacks (55 m, so the pivot sweep is above the 0.1 m/s gate speed
// when both turn at the orbit rate) parked port to port 0.3 m apart, aligned,
// with identical stack-bottom velocity.

type apolloPair struct {
	w    *World
	a, b *spacecraft.Spacecraft
	omg  float64
}

func newApolloPair(t *testing.T) *apolloPair {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	earth := w.Systems[0].FindBody("Earth")
	a := spacecraft.NewFromLoadout("Apollo-TLI")
	b := spacecraft.NewFromLoadout("Apollo-TLI")
	a.Name, b.Name = "Alpha", "Bravo"
	a.Primary, b.Primary = *earth, *earth
	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}
	axis := orbital.Vec3{Y: 1}
	a.State = physics.StateVector{R: r, V: v, M: a.TotalMass()}
	a.CurrentAttitudeDir = axis
	b.CurrentAttitudeDir = axis.Scale(-1)
	ga, gb := VesselPortGeometry(a), VesselPortGeometry(b)
	if !ga.HasPort() || !gb.HasPort() {
		t.Fatal("fixture: Apollo-TLI should carry a port")
	}
	pa := ga.PortPoint(r, 0)
	b.State = physics.StateVector{R: pa.Add(axis.Scale(0.3)).Sub(b.CurrentAttitudeDir.Scale(gb.Ports[0].OffsetM)), V: v, M: b.TotalMass()}
	w.Crafts = []*spacecraft.Spacecraft{a, b}
	w.ActiveCraftIdx = 0
	w.stampCraftID(a)
	w.stampCraftID(b)
	w.SetTargetCraft(1)
	return &apolloPair{w: w, a: a, b: b, omg: math.Sqrt(earth.GravitationalParameter() / (r.Norm() * r.Norm() * r.Norm()))}
}

func (p *apolloPair) rates(wa, wb float64) {
	p.a.AttitudeRate = orbital.Vec3{Z: wa}
	p.b.AttitudeRate = orbital.Vec3{Z: wb}
}

// A parked pair whose noses both turn at the orbit rate has its ports moving
// 2 x 54.7 m x omega = 0.12 m/s relative to each other even though the stacks'
// bottoms are stationary: not dock-ready on speed. With the sweep nulled (the
// noses turning opposite ways, or not at all) it is.
func TestDockGateSpeedReadsThePortSweep(t *testing.T) {
	p := newApolloPair(t)

	p.rates(p.omg, p.omg)
	st, ok := p.w.ProximityState()
	if !ok || !st.HasPort {
		t.Fatal("no port scene")
	}
	if st.RelVel.Norm() > 1e-9 {
		t.Fatalf("fixture: stack bottoms should be at rest relative to each other, got %.2e m/s", st.RelVel.Norm())
	}
	t.Logf("swept: rel speed %.5f m/s, port range %.3f m, align %.2f deg", st.VRelMS, st.PortRangeM, st.AlignDeg)
	if st.VRelMS < 0.11 || st.VRelMS > 0.13 {
		t.Errorf("swept pair rel speed = %.5f, want ~0.12 (2 x L x omega)", st.VRelMS)
	}
	if dockPairGateHolds(p.a, p.b) {
		t.Error("dockPairGateHolds on a pair whose ports sweep 0.12 m/s: want not ready on speed")
	}
	if PortGateTermsHold(st) {
		t.Error("PortGateTermsHold (the ring) holds on a pair whose ports sweep 0.12 m/s")
	}

	p.rates(p.omg, -p.omg)
	st, _ = p.w.ProximityState()
	t.Logf("nulled: rel speed %.2e m/s", st.VRelMS)
	if st.VRelMS > 1e-6 {
		t.Errorf("nulled pair rel speed = %.2e, want ~0", st.VRelMS)
	}
	if !dockPairGateHolds(p.a, p.b) {
		t.Error("dockPairGateHolds false on a parked aligned pair with the sweep nulled")
	}
	if !PortGateTermsHold(st) {
		t.Error("PortGateTermsHold (the ring) false on a parked aligned pair with the sweep nulled")
	}
}

// One reader: the chip's rel speed:, the pair gate and the ring's input are
// the same number for every rate combination, including a stack-bottom
// velocity offset, so they cannot drift apart.
func TestChipGateAndRingShareOnePortVelocityReader(t *testing.T) {
	p := newApolloPair(t)
	p.b.State.V = p.b.State.V.Add(orbital.Vec3{X: 0.02, Y: -0.01})
	for _, c := range [][2]float64{{0, 0}, {p.omg, p.omg}, {p.omg, -p.omg}, {2 * p.omg, 0.3 * p.omg}} {
		p.rates(c[0], c[1])
		st, ok := p.w.ProximityState()
		if !ok || !st.HasPort {
			t.Fatal("no port scene")
		}
		_, _, gateSpeed, _ := dockPairGate(p.a, p.b)
		if st.VRelMS != gateSpeed {
			t.Errorf("rates %v: chip rel speed %.9f != gate speed %.9f", c, st.VRelMS, gateSpeed)
		}
		if got := st.RelPortVel.Norm(); got != st.VRelMS {
			t.Errorf("rates %v: RelPortVel %.9f != VRelMS %.9f", c, got, st.VRelMS)
		}
		if want := PortGateTermsHold(st); want != dockPairGateHolds(p.a, p.b) {
			t.Errorf("rates %v: ring terms %v != pair gate %v", c, want, dockPairGateHolds(p.a, p.b))
		}
	}
}

// The magnet (ADR 0053 decision 5) still starts once the gate holds and does
// not start while the ports sweep too fast, driven through the real Tick (the
// sim measures AttitudeRate itself). Bravo holds retrograde (turns with the
// orbit, 0.12 m/s sweep with Alpha) or is frozen (Alpha turns until the pull suspends its slew).
func TestMagnetStartsOnlyWhenThePortSweepIsInsideTheGate(t *testing.T) {
	run := func(bravoTurns bool) (*apolloPair, bool) {
		p := newApolloPair(t)
		if bravoTurns {
			p.b.AttitudeMode = spacecraft.BurnRetrograde
		} else {
			p.b.SlewRateDegPerSec = 1e-9
		}
		// Settle the attitude rates 5 m out (the first slew tick snaps a nose and
		// reads rate 0), then close to 0.3 m, so the gate meets steady rates.
		p.b.State.R = p.b.State.R.Add(orbital.Vec3{Y: 4.7})
		for i := 0; i < 4; i++ {
			p.w.Tick()
		}
		p.b.State.R = p.b.State.R.Sub(orbital.Vec3{Y: 4.7})
		started := false
		for i := 0; i < 40; i++ { // 2 s
			p.w.Tick()
			if p.w.dockMagnet != nil {
				started = true
			}
		}
		return p, started
	}
	p, started := run(true)
	_, _, speed, _ := dockPairGate(p.a, p.b)
	t.Logf("both turn: gate speed %.4f m/s, magnet started=%v", speed, started)
	if started {
		t.Error("magnet started while the ports sweep ~0.12 m/s relative")
	}
	p, started = run(false)
	_, _, speed, _ = dockPairGate(p.a, p.b)
	t.Logf("only Alpha turns: gate speed %.4f m/s, magnet started=%v", speed, started)
	if !started {
		t.Fatal("magnet did not start with the gate inside 0.1 m/s")
	}
	tickFor(p.w, 6)
	if len(p.w.Crafts) != 1 {
		t.Errorf("slate = %d vessels 6 s after the magnet started, want the dock (1)", len(p.w.Crafts))
	}
}
