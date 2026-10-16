package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0055 slice 0 (#655): does the PROXIMITY chip's `rel speed:` tell the
// same story as the port-point geometry `lateral:` reads?
//
// Measured answer (see the PR and the vault log for the raw numbers): they
// did NOT agree before this slice. `rel speed:` was |State.V(own) -
// State.V(target)| (stack-bottom velocity), and so was the dock gate's speed
// term, but the port points ride a stack that pivots about its bottom at the
// orbit rate, so the port-to-port separation changed by (Lown + Ltarget) x
// omega more than `rel speed:` said (0.0116 m/s on a tug pair, 0.1214 m/s on
// an Apollo-TLI pair). Jason decided (ADR 0055 amendment): read speed at the
// ports. rel speed:, closing:, the gate's speed term and the ring now read the
// port-point relative velocity, through one reader (portRelVelocity).

// driftReading is one frame of everything the comparison reads.
type driftReading struct {
	st    ProximityState
	delta orbital.Vec3 // target port point minus own port point, world axes
	axis  orbital.Vec3 // own nose
	tSec  float64
	rng   float64 // port range
}

func driftRead(t *testing.T, w *World) driftReading {
	t.Helper()
	st, ok := w.ProximityState()
	if !ok || !st.HasPort {
		t.Fatalf("no port scene: ok=%v hasPort=%v", ok, st.HasPort)
	}
	own := w.ActiveCraft()
	tc, _, _ := w.ResolveTargetCraft()
	pa := st.Own.PortPoint(own.State.R, 0)
	pb := st.Target.PortPoint(tc.State.R, 0)
	return driftReading{st: st, delta: pb.Sub(pa), axis: st.Own.Axis, tSec: float64(w.Clock.SimTime.UnixNano()) / 1e9, rng: st.PortRangeM}
}

// driftSample is the centred finite-difference measurement at one orbit
// phase: window of +-1 tick around the middle reading.
type driftSample struct {
	relSpeed    float64 // the chip's rel speed:, at the centre
	portRate    float64 // |d(port separation)/dt|, centred difference
	stateRate   float64 // |RelVel| from State.V, the readout's own source
	sweep       float64 // |portRate vector - RelVel vector|: what State.V never carries
	predSweep   float64 // (Lown + Ltarget) x omega, from the measured nose rotation rate
	omega       float64 // measured rotation rate of the own nose, rad/s
	closing     float64
	latRateUp   float64 // d(OffAxisUpM)/dt
	latRateRgt  float64
	portClosing float64 // -d(port range)/dt, centred difference
}

func driftMeasure(t *testing.T, w *World, afterTick func()) driftSample {
	t.Helper()
	if afterTick == nil {
		afterTick = func() {}
	}
	a := driftRead(t, w)
	w.Tick()
	afterTick()
	m := driftRead(t, w)
	w.Tick()
	afterTick()
	b := driftRead(t, w)
	dt := b.tSec - a.tSec
	if dt <= 0 {
		t.Fatalf("clock did not advance (dt=%v)", dt)
	}
	portVel := b.delta.Sub(a.delta).Scale(1 / dt)
	omega := math.Acos(math.Min(1, a.axis.Unit().Dot(b.axis.Unit()))) / dt
	lsum := m.st.Own.Ports[0].OffsetM + m.st.Target.Ports[0].OffsetM
	// The port-point separation is target port minus own port, so its rate
	// is RelVel(target - own) = -RelVel(own - target) plus the sweep.
	targetMinusOwn := m.st.RelVel.Scale(-1)
	return driftSample{
		relSpeed:    m.st.VRelMS,
		portRate:    portVel.Norm(),
		stateRate:   m.st.RelVel.Norm(),
		sweep:       portVel.Sub(targetMinusOwn).Norm(),
		predSweep:   lsum * omega,
		omega:       omega,
		closing:     m.st.ClosingMS,
		latRateUp:   (b.st.OffAxisUpM - a.st.OffAxisUpM) / dt,
		latRateRgt:  (b.st.OffAxisRightM - a.st.OffAxisRightM) / dt,
		portClosing: -(b.rng - a.rng) / dt,
	}
}

// driftTolerance is how far the measured sweep may sit from (Lown+Ltarget) x
// omega: 10%. The prediction treats the nose as turning at a constant rate
// over the +-1 tick window and the finite difference has O(dt^2) error; the
// clean runs land within ~1% (see the log lines), so 10% leaves room without
// hiding the 0.05 m/s injection below (which is ~100% on a tug).
const driftTolerance = 0.10

func (s driftSample) unexplained() float64 {
	return math.Abs(s.sweep-s.predSweep) / s.predSweep
}

// driftWorld spawns homeID and an Alongside partner of the same loadout and
// targets the original, the player's "Alongside" case.
func driftWorld(t *testing.T, loadout string) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	old := w.Crafts[0]
	home := spacecraft.NewFromLoadout(loadout)
	home.Primary = old.Primary
	home.State = old.State
	home.State.M = home.TotalMass()
	home.CurrentAttitudeDir = home.State.V.Unit()
	home.Name = "Home"
	w.Crafts[0] = home
	w.stampCraftID(home)
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: loadout, Alongside: true}); err != nil {
		t.Fatal(err)
	}
	w.SetTargetCraft(0)
	// Three ticks of settle: the first slew tick snaps an uninitialised nose,
	// which is not a rotation, so AttitudeRate reads 0 until the second.
	tickFor(w, 0.15)
	return w
}

// driftPhases samples n orbit phases spaced phaseGapS apart (a LEO period is
// ~5400 s, so 8 x 700 s covers one lap).
func driftPhases(t *testing.T, w *World, n int, phaseGapS float64, each func(i int, s driftSample)) {
	t.Helper()
	for i := 0; i < n; i++ {
		each(i, driftMeasure(t, w, nil))
		tickFor(w, phaseGapS)
	}
}

// TestRelSpeedSeesThePortPointSweepOfAPivotingStack is the slice-0 guard.
// Spawned Alongside, over a full lap, for a small and a ~55 m pair:
//   - the port-point separation changes by (Lown+Ltarget) x omega more than
//     the stack-bottom velocity says (the finding), omega being the orbit
//     rate;
//   - rel speed: now equals the measured rate of change of the port-point
//     separation vector (finite difference of the very positions lateral:
//     and port range read), within 2%;
//   - closing: equals -d(port range)/dt, + = closing, within 0.2 mm/s.
//
// Restoring the State.V read (VRelMS = |RelVel|) turns this red at every phase.
func TestRelSpeedSeesThePortPointSweepOfAPivotingStack(t *testing.T) {
	for _, loadout := range []string{"RCS-tug", "Apollo-TLI"} {
		t.Run(loadout, func(t *testing.T) {
			w := driftWorld(t, loadout)
			var maxSweep float64
			driftPhases(t, w, 8, 700, func(i int, s driftSample) {
				t.Logf("phase %d: rel speed=%.5f |port sep rate|=%.5f |State.V rel|=%.5f sweep=%.5f predicted (L1+L2)w=%.5f omega=%.3e closing=%+.5f vs -d(range)/dt=%+.5f",
					i, s.relSpeed, s.portRate, s.stateRate, s.sweep, s.predSweep, s.omega, s.closing, s.portClosing)
				if d := math.Abs(s.relSpeed - s.portRate); d > 0.02*s.portRate {
					t.Errorf("phase %d: rel speed %.5f vs measured port-point rate %.5f (off %.5f, tolerance 2%%)", i, s.relSpeed, s.portRate, d)
				}
				if d := math.Abs(s.closing - s.portClosing); d > 2e-4 {
					t.Errorf("phase %d: closing %+.5f vs -d(port range)/dt %+.5f", i, s.closing, s.portClosing)
				}
				if s.omega < 1e-4 || s.omega > 3e-3 {
					t.Errorf("phase %d: nose turns at %.3e rad/s, want the ~1e-3 orbit rate (hold fixture broken)", i, s.omega)
				}
				if rel := s.unexplained(); rel > driftTolerance {
					t.Errorf("phase %d: port-point motion beyond State.V is %.5f m/s vs (L1+L2)w = %.5f (%.0f%% off, tolerance %.0f%%)", i, s.sweep, s.predSweep, rel*100, driftTolerance*100)
				}
				maxSweep = math.Max(maxSweep, s.sweep)
			})
			if maxSweep < 0.005 {
				t.Errorf("sweep %.5f m/s is below the 5 mm/s the finding rests on", maxSweep)
			}
		})
	}
}

// The instrument must be able to go red. The target's nose is turned an extra
// 0.05 / Ltarget rad/s about the orbit normal each tick: a 0.05 m/s port-point
// velocity that State.V (and so rel speed:) does not carry and that the
// (Lown+Ltarget) x omega prediction, built from the own nose's rotation,
// cannot explain. The measurement must flag it, and must also leave rel
// speed: unmoved by it (the readout really is blind to it).
func TestDriftInstrumentSeesAnInjectedPortPointVelocity(t *testing.T) {
	clean := driftWorld(t, "RCS-tug")
	cleanS := driftMeasure(t, clean, nil)

	w := driftWorld(t, "RCS-tug")
	target, _, _ := w.ResolveTargetCraft()
	own := w.ActiveCraft()
	ltarget := VesselPortGeometry(target).Ports[0].OffsetM
	tick := w.Clock.BaseStep.Seconds()
	inject := func() {
		n := own.State.R.Cross(own.State.V).Unit()
		ang := 0.05 / ltarget * tick
		ax := target.CurrentAttitudeDir.Unit()
		target.CurrentAttitudeDir = ax.Scale(math.Cos(ang)).Add(n.Cross(ax).Scale(math.Sin(ang))).Add(n.Scale(n.Dot(ax) * (1 - math.Cos(ang))))
	}
	s := driftMeasure(t, w, inject)
	t.Logf("clean: unexplained=%.1f%% sweep=%.5f rel speed=%.6f", cleanS.unexplained()*100, cleanS.sweep, cleanS.relSpeed)
	t.Logf("injected 0.05 m/s: unexplained=%.1f%% sweep=%.5f predicted=%.5f rel speed=%.6f", s.unexplained()*100, s.sweep, s.predSweep, s.relSpeed)
	if cleanS.unexplained() > driftTolerance {
		t.Fatalf("clean control is already %.0f%% off", cleanS.unexplained()*100)
	}
	if s.unexplained() <= driftTolerance {
		t.Errorf("injected 0.05 m/s port-point velocity read as only %.1f%% unexplained (<= %.0f%%): the instrument cannot go red", s.unexplained()*100, driftTolerance*100)
	}
	if d := math.Abs(s.relSpeed - s.portRate); d < 0.02*s.portRate {
		t.Errorf("rel speed %.5f matches the injected port rate %.5f: the readout comparison cannot go red", s.relSpeed, s.portRate)
	}
	if d := math.Abs(cleanS.relSpeed - cleanS.portRate); d > 0.02*cleanS.portRate+1e-6 {
		t.Errorf("clean control: rel speed %.5f vs port rate %.5f", cleanS.relSpeed, cleanS.portRate)
	}
	if math.Abs(s.relSpeed-cleanS.relSpeed) > 1e-6 {
		t.Errorf("rel speed moved %.6f -> %.6f under a nose-only injection (the readout reads the sim-measured AttitudeRate, which the injection bypasses)", cleanS.relSpeed, s.relSpeed)
	}
}
