package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0053 slice C: the PORT navball frame. Fixtures here put a ported RCS
// Tug 30 m from the active vessel with a deliberately out-of-plane nose, so
// "along the port axis" cannot be confused with any orbit-frame direction.

// portTargetAxis is a nose tilted off the radial, along-track and
// cross-track axes alike (for tests that need an axis with no world).
func portTargetAxis() orbital.Vec3 {
	return orbital.Vec3{X: 0.5, Y: 0.6, Z: 0.62}.Unit()
}

// portPairWorld: active vessel (RCS armed, monoprop) plus a target tug
// that carries a port, nose along portTargetAxis.
func portPairWorld(t *testing.T) *World {
	t.Helper()
	w := proximityPairWorld(t, orbital.Vec3{X: 30}, orbital.Vec3{})
	tgt := w.Crafts[1]
	tgt.Stages = []spacecraft.Stage{stageOf(t, "rcs-tug")}
	tgt.SyncFields()
	// Out of plane in the TARGET'S OWN LVLH frame: components along all of
	// radial, along-track and cross-track, so none of the orbit-frame
	// directions (and neither wording of "right") can pass for another.
	rT, vT, _ := w.TargetStateRelativeToActivePrimary()
	f, ok := orbital.LVLHBasis(rT, vT)
	if !ok {
		t.Fatal("no LVLH frame for the fixture target")
	}
	tgt.CurrentAttitudeDir = f.RadialOut.Scale(0.35).Add(f.AlongTrack.Scale(0.45)).Add(f.CrossTrack.Scale(0.82)).Unit()
	a := w.ActiveCraft()
	a.RCSIsp, a.RCSThrust = 200, 100
	a.Monoprop = 50
	if len(a.Stages) > 0 {
		a.Stages[0].MonopropMass = 50
		a.SyncFields()
	}
	return w
}

func cosAngle(a, b orbital.Vec3) float64 { return a.Unit().Dot(b.Unit()) }

func TestCycleNavModeOffersPortOnlyWithPortedTarget(t *testing.T) {
	w := portPairWorld(t)
	w.NavMode = NavTarget
	if got := w.CycleNavMode(); got != NavPort {
		t.Fatalf("Target -> %v, want PORT with a ported target", got)
	}
	if got := w.CycleNavMode(); got != NavSurface {
		t.Fatalf("Port -> %v, want SURF", got)
	}

	// A port-less target: TGT is offered, PORT is skipped.
	w.Crafts[1].Stages = []spacecraft.Stage{stageOf(t, "s-ivb-1")}
	w.Crafts[1].SyncFields()
	w.NavMode = NavOrbit
	if got := w.CycleNavMode(); got != NavTarget {
		t.Fatalf("Orbit -> %v, want TGT", got)
	}
	if got := w.CycleNavMode(); got != NavSurface {
		t.Fatalf("Target -> %v, want SURF (no port, PORT skipped)", got)
	}

	// No target at all: both TGT and PORT are skipped.
	w.ClearTarget()
	w.NavMode = NavOrbit
	if got := w.CycleNavMode(); got != NavSurface {
		t.Fatalf("Orbit with no target -> %v, want SURF", got)
	}
}

func TestPortNotOfferedForGhostTarget(t *testing.T) {
	w := ghostWorld(t)
	if w.TargetHasPort() {
		t.Fatal("a ghost has no port shape in slice C; TargetHasPort must be false")
	}
	w.NavMode = NavTarget
	if got := w.CycleNavMode(); got != NavSurface {
		t.Fatalf("Target with a ghost -> %v, want SURF (PORT not offered)", got)
	}
	// A stale PORT against a ghost resolves as orbit, never to a port mode.
	w.NavMode = NavPort
	if got := w.ResolveAttitudeIntent(IntentRadialOut); got != spacecraft.BurnRadialOut {
		t.Errorf("PORT with a ghost resolved q to %v, want the orbit-frame Radial+", got)
	}
}

func TestReconcileDropsPortWhenTargetChangesOrLosesPort(t *testing.T) {
	w := portPairWorld(t)
	w.NavMode = NavPort
	w.reconcileNavMode()
	if w.NavMode != NavPort {
		t.Fatalf("reconcile dropped PORT for a ported target: %v", w.NavMode)
	}
	// The target sheds its port (a stack whose top is port-less): the next
	// tick drops the frame.
	w.Crafts[1].Stages = []spacecraft.Stage{stageOf(t, "s-ivb-1")}
	w.Crafts[1].SyncFields()
	w.Tick()
	if w.NavMode != NavOrbit {
		t.Errorf("target lost its port: NavMode = %v, want ORBIT", w.NavMode)
	}

	// The target changes: clearing it drops PORT.
	w = portPairWorld(t)
	w.NavMode = NavPort
	w.ClearTarget()
	if w.NavMode != NavOrbit {
		t.Errorf("ClearTarget left NavMode = %v, want ORBIT", w.NavMode)
	}
}

// The self-switch only ever writes Surface or Orbit. A ported target is
// bound and PORT is a legal frame, so a stray NavPort write on either edge
// would survive reconcile and show here.
func TestNavAutoNeverWritesPort(t *testing.T) {
	w := navPadWorld(t)
	padIdx := w.ActiveCraftIdx
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	tgtIdx := len(w.Crafts) - 1
	w.Crafts[tgtIdx].Stages = []spacecraft.Stage{stageOf(t, "rcs-tug")}
	w.Crafts[tgtIdx].SyncFields()
	w.Crafts[tgtIdx].CurrentAttitudeDir = portTargetAxis()
	w.ActiveCraftIdx = padIdx
	w.SetTargetCraft(tgtIdx)
	if !w.TargetHasPort() {
		t.Fatal("setup: target must have a port")
	}
	w.NavMode = NavSurface
	w.Tick() // seed below the line
	up := w.NavAutoAscentAltitudeFor(w.ActiveCraft().Primary)
	liftTo(t, w, up*1.2) // ascent edge
	if w.NavMode != NavOrbit {
		t.Fatalf("ascent edge wrote NavMode = %v, want ORBIT", w.NavMode)
	}
	liftTo(t, w, up*0.3) // descent edge
	if w.NavMode != NavSurface {
		t.Fatalf("descent edge wrote NavMode = %v, want SURF", w.NavMode)
	}
}

func TestResolveAttitudeIntentPortMapping(t *testing.T) {
	w := portPairWorld(t)
	w.NavMode = NavPort
	cases := []struct {
		intent AttitudeIntent
		want   spacecraft.BurnMode
	}{
		{IntentRadialOut, spacecraft.BurnPortIn},      // q
		{IntentRadialIn, spacecraft.BurnPortOut},      // e
		{IntentPrograde, spacecraft.BurnPortUp},       // w
		{IntentRetrograde, spacecraft.BurnPortDown},   // s
		{IntentNormalPlus, spacecraft.BurnPortLeft},   // a
		{IntentNormalMinus, spacecraft.BurnPortRight}, // d
	}
	for _, c := range cases {
		if got := w.ResolveAttitudeIntent(c.intent); got != c.want {
			t.Errorf("PORT intent %v: got %v, want %v", c.intent, got, c.want)
		}
	}
	// A port frame whose port is gone resolves as orbit (never a zero hold).
	w.Crafts[1].Stages = []spacecraft.Stage{stageOf(t, "s-ivb-1")}
	w.Crafts[1].SyncFields()
	if got := w.ResolveAttitudeIntent(IntentRadialOut); got != spacecraft.BurnRadialOut {
		t.Errorf("PORT without a port resolved q to %v, want Radial+", got)
	}
}

// pulseDV fires one RCS pulse for the intent and returns the velocity
// change the active vessel received.
func pulseDV(t *testing.T, w *World, intent AttitudeIntent) orbital.Vec3 {
	t.Helper()
	a := w.ActiveCraft()
	a.EngineMode = spacecraft.EngineRCS
	v0 := a.State.V
	if !w.FireRCSPulse(w.ResolveAttitudeIntent(intent)) {
		t.Fatalf("RCS pulse for intent %v refused", intent)
	}
	return a.State.V.Sub(v0)
}

// adrRight is ADR 0053 decision 7's wording of "right": CrossTrack with
// the port-axis component removed, normalised.
func adrRight(axis orbital.Vec3, f orbital.LVLHFrame) orbital.Vec3 {
	return f.CrossTrack.Sub(axis.Scale(f.CrossTrack.Dot(axis))).Unit()
}

// q in PORT with RCS fires along -a_target (the docking approach), e along
// +a_target; w/s and a/d along the port face. Vector tests: the pulse's
// direction is measured against independently built unit vectors.
func TestPortRCSPulsesFireAlongThePortFrame(t *testing.T) {
	w := portPairWorld(t)
	axis := w.Crafts[1].CurrentAttitudeDir
	w.NavMode = NavPort
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		t.Fatal("target did not resolve")
	}
	frame, ok := orbital.LVLHBasis(rT, vT)
	if !ok {
		t.Fatal("no LVLH frame")
	}
	// Expected unit vectors, built here from the ADR's words, not from the
	// function under test.
	wantUp := frame.RadialOut.Sub(axis.Scale(frame.RadialOut.Dot(axis))).Unit()
	wantRight := adrRight(axis, frame)

	in := pulseDV(t, w, IntentRadialOut)
	if c := cosAngle(in, axis.Scale(-1)); c < 0.99999 {
		t.Errorf("q: dv cos with -a_target = %.6f (dv %+v), want 1", c, in)
	}
	if c := cosAngle(in, frame.RadialOut); math.Abs(c) > 0.95 {
		t.Errorf("q fired along orbit radial (cos %.3f)", c)
	}
	out := pulseDV(t, w, IntentRadialIn)
	if c := cosAngle(out, axis); c < 0.99999 {
		t.Errorf("e: dv cos with +a_target = %.6f, want 1", c)
	}
	up := pulseDV(t, w, IntentPrograde)
	if c := cosAngle(up, wantUp); c < 0.99999 {
		t.Errorf("w: dv cos with projected RadialOut = %.6f, want 1", c)
	}
	down := pulseDV(t, w, IntentRetrograde)
	if c := cosAngle(down, wantUp.Scale(-1)); c < 0.99999 {
		t.Errorf("s: dv cos with -projected RadialOut = %.6f, want 1", c)
	}
	// d pushes right: along the projected CrossTrack. The basis is
	// PortFaceBasis's (orthogonal to up); ADR 0053's literal wording differs
	// from it by a measured angle for an out-of-plane axis.
	right := pulseDV(t, w, IntentNormalMinus)
	_, basisRight, _ := orbital.PortFaceBasis(axis, frame)
	if c := cosAngle(right, basisRight); c < 0.99999 {
		t.Errorf("d: dv cos with PortFaceBasis right = %.6f, want 1", c)
	}
	// ADR 0053's literal wording of right differs from the shared basis for
	// this out-of-plane axis; it stays the same general direction.
	t.Logf("d: PortFaceBasis right vs ADR-worded projected CrossTrack: cos %.4f", cosAngle(right, wantRight))
	if c := cosAngle(right, wantRight); c < 0.7 {
		t.Errorf("d: dv cos with ADR-worded projected CrossTrack = %.6f, want >= 0.7", c)
	}
	left := pulseDV(t, w, IntentNormalPlus)
	if c := cosAngle(left, basisRight.Scale(-1)); c < 0.99999 {
		t.Errorf("a: dv cos with -right = %.6f, want 1", c)
	}
	// The face directions are orthogonal to the axis and to each other.
	if d := math.Abs(cosAngle(up, axis)); d > 1e-6 {
		t.Errorf("up not orthogonal to the port axis: cos %g", d)
	}
	if d := math.Abs(cosAngle(right, axis)); d > 1e-6 {
		t.Errorf("right not orthogonal to the port axis: cos %g", d)
	}
	if d := math.Abs(cosAngle(right, up)); d > 1e-6 {
		t.Errorf("right not orthogonal to up: cos %g", d)
	}
}

// The measured gap between ADR 0053 decision 7's wording of "right" and
// the shared PortFaceBasis, for an in-plane axis (zero) and the tilted
// fixture axis (the angle that slice C reports).
func TestPortFaceBasisVsADRWording(t *testing.T) {
	rT := orbital.Vec3{X: 6778e3}
	vT := orbital.Vec3{Y: 7670}
	frame, _ := orbital.LVLHBasis(rT, vT)
	for _, tc := range []struct {
		name string
		axis orbital.Vec3
		maxD float64
	}{
		{"in-plane axis", orbital.Vec3{X: 0.6, Y: 0.8}, 1e-9},
		{"tilted fixture axis", portTargetAxis(), 40},
	} {
		_, right, ok := orbital.PortFaceBasis(tc.axis.Unit(), frame)
		if !ok {
			t.Fatal("no basis")
		}
		adr := adrRight(tc.axis.Unit(), frame)
		deg := math.Acos(math.Min(1, right.Dot(adr))) * 180 / math.Pi
		t.Logf("%s: PortFaceBasis right vs ADR wording differ by %.2f deg", tc.name, deg)
		if deg > tc.maxD {
			t.Errorf("%s: differ by %.2f deg, want <= %.2f", tc.name, deg, tc.maxD)
		}
	}
}

// Main engine selected, q holds the nose on -a_target (the exact docking
// attitude); the commanded direction follows the target as it turns (the
// mode is frame-locked, resolved live).
func TestPortPlusHoldCommandsNoseOnPortAxis(t *testing.T) {
	w := portPairWorld(t)
	w.NavMode = NavPort
	a := w.ActiveCraft()
	a.EngineMode = spacecraft.EngineMain
	w.SetAttitudeMode(w.ResolveAttitudeIntent(IntentRadialOut))
	if a.AttitudeMode != spacecraft.BurnPortIn {
		t.Fatalf("AttitudeMode = %v, want Port+", a.AttitudeMode)
	}
	if c := cosAngle(w.commandedDirFor(a), w.Crafts[1].CurrentAttitudeDir.Scale(-1)); c < 0.99999 {
		t.Errorf("commanded dir cos with -a_target = %.6f, want 1", c)
	}
	// The target turns: the hold follows.
	turned := orbital.Vec3{X: -0.2, Y: 0.9, Z: 0.1}.Unit()
	w.Crafts[1].CurrentAttitudeDir = turned
	if c := cosAngle(w.commandedDirFor(a), turned.Scale(-1)); c < 0.99999 {
		t.Errorf("after the target turned, cos = %.6f, want 1", c)
	}
	// The port goes away: the hold degrades to an orbit-frame prograde,
	// never a zero or stale direction.
	w.Crafts[1].Stages = []spacecraft.Stage{stageOf(t, "s-ivb-1")}
	w.Crafts[1].SyncFields()
	if w.commandedDirFor(a).Norm() == 0 {
		t.Error("hold with a vanished port commanded a zero direction")
	}
}

func TestPortFrameNavballMarkersMarkOnlyInAndOut(t *testing.T) {
	w := portPairWorld(t)
	w.NavMode = NavPort
	if _, ok := w.NavballBasis(); !ok {
		t.Fatal("PORT navball basis must exist")
	}
	ms := w.NavballMarkers()
	glyphs := map[rune]bool{}
	for _, m := range ms {
		glyphs[m.Glyph] = true
	}
	if !glyphs[NavballGlyphTarget] || !glyphs[NavballGlyphAntiTarget] {
		t.Errorf("PORT markers lack the in/out pair: %v", glyphs)
	}
	for _, g := range []rune{NavballGlyphPrograde, NavballGlyphRetrograde, NavballGlyphNormalPlus, NavballGlyphNormalMinus, NavballGlyphRadialOut, NavballGlyphRadialIn} {
		if glyphs[g] {
			t.Errorf("PORT drew the orbit-frame glyph %q, which would lie about what w/s/a/d mark", g)
		}
	}
}

func TestNavSpeedPortReadsRelativeSpeed(t *testing.T) {
	w := portPairWorld(t)
	w.Crafts[1].State.V = w.ActiveCraft().State.V.Add(orbital.Vec3{X: 0.3})
	w.NavMode = NavPort
	v, frame := w.NavSpeed()
	if frame != NavPort || math.Abs(v-0.3) > 1e-9 {
		t.Errorf("NavSpeed = %g in %v, want 0.3 in PORT", v, frame)
	}
}
