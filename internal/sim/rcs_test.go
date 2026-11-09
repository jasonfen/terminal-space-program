package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestCycleEngineMode: starts in EngineMain, toggles to EngineRCS
// and back to EngineMain.
func TestCycleEngineMode(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.ActiveCraft().EngineMode != spacecraft.EngineMain {
		t.Fatalf("default engine = %v, want EngineMain", w.ActiveCraft().EngineMode)
	}
	w.CycleEngineMode()
	if w.ActiveCraft().EngineMode != spacecraft.EngineRCS {
		t.Errorf("after first toggle = %v, want EngineRCS", w.ActiveCraft().EngineMode)
	}
	w.CycleEngineMode()
	if w.ActiveCraft().EngineMode != spacecraft.EngineMain {
		t.Errorf("after second toggle = %v, want EngineMain", w.ActiveCraft().EngineMode)
	}
}

// TestCycleRCSPulseScaleWrapsAndScalesPulse: cycling steps the level
// 0→1→2→0, the reported per-pulse Δv tracks it (0.1 / 0.01 / 0.001),
// and a fired pulse delivers the level's Δv.
func TestCycleRCSPulseScaleWrapsAndScalesPulse(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.EngineMode = spacecraft.EngineRCS

	if c.RCSFineLevel != 0 || math.Abs(c.RCSPulseDV()-0.1) > 1e-12 {
		t.Fatalf("default level %d / dv %g, want 0 / 0.1", c.RCSFineLevel, c.RCSPulseDV())
	}

	want := []struct {
		level int
		dv    float64
	}{{1, 0.01}, {2, 0.001}, {0, 0.1}}
	for i, wcase := range want {
		w.CycleRCSPulseScale()
		if c.RCSFineLevel != wcase.level || math.Abs(c.RCSPulseDV()-wcase.dv) > 1e-12 {
			t.Errorf("cycle %d: level %d / dv %g, want %d / %g",
				i+1, c.RCSFineLevel, c.RCSPulseDV(), wcase.level, wcase.dv)
		}
		v0 := c.OrbitalSpeed()
		if !w.FireRCSPulse(spacecraft.BurnPrograde) {
			t.Fatalf("cycle %d: FireRCSPulse returned false", i+1)
		}
		if got := c.OrbitalSpeed() - v0; math.Abs(got-wcase.dv) > 1e-9 {
			t.Errorf("cycle %d: Δ|v| = %g, want %g", i+1, got, wcase.dv)
		}
	}
}

// TestCycleRCSPulseScaleNilSafe: no active craft → no panic.
func TestCycleRCSPulseScaleNilSafe(t *testing.T) {
	w := &World{}
	w.CycleRCSPulseScale() // must not panic
}

// TestFireRCSPulseGatesOnEngineMode: in EngineMain mode, FireRCSPulse
// should be a no-op even with a fully-fueled craft. Switching to
// EngineRCS and pulsing must change |v|.
func TestFireRCSPulseGatesOnEngineMode(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	v0 := w.ActiveCraft().OrbitalSpeed()

	if w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Error("FireRCSPulse fired in EngineMain mode")
	}
	if w.ActiveCraft().OrbitalSpeed() != v0 {
		t.Errorf("|v| changed on gated pulse: %v → %v", v0, w.ActiveCraft().OrbitalSpeed())
	}

	w.CycleEngineMode() // → EngineRCS
	if !w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Fatal("FireRCSPulse did not fire in EngineRCS mode")
	}
	got := w.ActiveCraft().OrbitalSpeed()
	want := v0 + spacecraft.RCSDvQuantum
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("post-pulse |v| = %.6f, want %.6f", got, want)
	}
}

// TestFireRCSPulsePreservesAttitudeMode: RCS is a 6-axis translation
// tool — firing a pulse should apply Δv without re-pointing the nose.
// The SAS hold (AttitudeMode) must survive an RCS pulse in a different
// direction so the slew system doesn't slew the craft mid-translation.
func TestFireRCSPulsePreservesAttitudeMode(t *testing.T) {
	w, _ := NewWorld()
	before := w.ActiveCraft().AttitudeMode
	w.CycleEngineMode()
	w.FireRCSPulse(spacecraft.BurnRetrograde)
	if w.ActiveCraft().AttitudeMode != before {
		t.Errorf("AttitudeMode changed by RCS pulse: %v → %v, want unchanged",
			before, w.ActiveCraft().AttitudeMode)
	}
}

// TestFireRCSPulseRecordsPuff: a fired pulse should appear in the
// puff buffer for the canvas renderer to surface.
func TestFireRCSPulseRecordsPuff(t *testing.T) {
	w, _ := NewWorld()
	w.CycleEngineMode()
	w.FireRCSPulse(spacecraft.BurnPrograde)
	puffs := w.RCSPuffs()
	if len(puffs) != 1 {
		t.Fatalf("expected 1 puff, got %d", len(puffs))
	}
	if puffs[0].AgeFrac != 0 {
		t.Errorf("AgeFrac on freshly-fired puff = %v, want 0", puffs[0].AgeFrac)
	}
}

// TestStartManualBurnGatesOnEngineMode: in RCS mode, StartManualBurn
// must be a no-op — main-engine sustained fire is gated to EngineMain.
func TestStartManualBurnGatesOnEngineMode(t *testing.T) {
	w, _ := NewWorld()
	w.CycleEngineMode() // → RCS
	w.StartManualBurn()
	if w.ActiveCraft().ManualBurn != nil {
		t.Error("StartManualBurn engaged a sustained burn in RCS mode")
	}
	w.CycleEngineMode() // → main
	w.StartManualBurn()
	if w.ActiveCraft().ManualBurn == nil {
		t.Error("StartManualBurn failed to engage in main mode")
	}
}

// siteRCSWorld is the Lander 50 m up and 50 m short of the flag with the track
// latched east and RCS armed in SITE (ADR 0067 decision 9). The nose points
// somewhere else, so a pulse that followed the nose would show.
func siteRCSWorld(t *testing.T) (*World, *spacecraft.Spacecraft, ProximityState) {
	t.Helper()
	w, c := deorbitWorld(t, 150, 0)
	s := w.Sites[0]
	rT, vT := surfacePointState(c.Primary, s.LatDeg, s.LonDeg, w.Clock.SimTime)
	up := rT.Unit()
	_, east := siteHorizon(c.Primary, up)
	c.State.R = rT.Add(up.Scale(50)).Sub(east.Scale(50))
	c.State.V = vT
	w.landTrack = landTrack{}
	st, ok := w.ProximityState()
	if !ok || !st.Surface {
		t.Fatal("no landing scene")
	}
	c.CurrentAttitudeDir = st.Frame.CrossTrack.Add(st.Frame.RadialOut).Unit()
	w.NavMode = NavSite
	w.CycleEngineMode()
	return w, c, st
}

// The six letters in SITE with RCS armed push along the site axes by one
// RCSPulseDV, never along the nose, and burn monoprop (not main fuel).
func TestSiteRCSPulsesAlongSiteAxes(t *testing.T) {
	cases := []struct {
		name   string
		intent AttitudeIntent
		axis   func(ProximityState) (x, y, z float64) // want: (fwd, left, up) components of dv
	}{
		{"q fwd", IntentRadialOut, func(ProximityState) (float64, float64, float64) { return 1, 0, 0 }},
		{"e back", IntentRadialIn, func(ProximityState) (float64, float64, float64) { return -1, 0, 0 }},
		{"w up", IntentPrograde, func(ProximityState) (float64, float64, float64) { return 0, 0, 1 }},
		{"s down", IntentRetrograde, func(ProximityState) (float64, float64, float64) { return 0, 0, -1 }},
		{"a left", IntentNormalPlus, func(ProximityState) (float64, float64, float64) { return 0, 1, 0 }},
		{"d right", IntentNormalMinus, func(ProximityState) (float64, float64, float64) { return 0, -1, 0 }},
	}
	for _, tc := range cases {
		w, c, st := siteRCSWorld(t)
		c.PitchTrim, c.HeadingTrim, c.PortPitchTrim, c.PortYawTrim = 0.3, 0.4, 0.2, 0.2 // trims turn holds, never a site pulse
		v0, hold, nose, mono0, fuel0 := c.State.V, c.AttitudeMode, c.CurrentAttitudeDir, c.Monoprop, c.Fuel
		if !w.FireRCSPulse(w.ResolveKeyIntent(tc.intent)) {
			t.Errorf("%s: pulse refused", tc.name)
			continue
		}
		dv := c.State.V.Sub(v0)
		fwd, left, up := dv.Dot(st.Frame.AlongTrack), dv.Dot(st.Frame.CrossTrack), dv.Dot(st.Frame.RadialOut)
		wx, wy, wz := tc.axis(st)
		q := c.RCSPulseDV()
		if math.Abs(fwd-wx*q) > 1e-4 || math.Abs(left-wy*q) > 1e-4 || math.Abs(up-wz*q) > 1e-4 {
			t.Errorf("%s: dv (fwd, left, up) = (%.4f, %.4f, %.4f), want %.1f x (%g, %g, %g)", tc.name, fwd, left, up, q, wx, wy, wz)
		}
		if c.AttitudeMode != hold || c.CurrentAttitudeDir != nose {
			t.Errorf("%s: a pulse moved the hold or the nose", tc.name)
		}
		if c.Monoprop >= mono0 || c.Fuel != fuel0 {
			t.Errorf("%s: monoprop %.4f -> %.4f, fuel %.1f -> %.1f; want monoprop down, fuel untouched", tc.name, mono0, c.Monoprop, fuel0, c.Fuel)
		}
	}
}

// Letters keep their meaning outside the one exception: unarmed in SITE they
// select holds (q is Site+), armed in ORBIT they fire the orbit frame.
func TestSiteRCSLettersOnlyWithRCSArmedInSite(t *testing.T) {
	w, c, _ := siteRCSWorld(t)
	w.CycleEngineMode() // back to main
	if m := w.ResolveKeyIntent(IntentRadialOut); m != spacecraft.BurnSite {
		t.Errorf("q unarmed in SITE = %v, want Site+", m)
	}
	w.CycleEngineMode()
	w.NavMode = NavOrbit
	if m := w.ResolveKeyIntent(IntentRadialOut); m != spacecraft.BurnRadialOut {
		t.Errorf("q armed in ORBIT = %v, want Radial+", m)
	}
	_ = c
}

// Site+ auto (N) owns the engine and refuses a pulse; the q nose hold does not
// (Owns is Engaged and not Nose), so the hand procedure may pulse under it.
func TestSiteRCSRefusedWhileSitePlusOwnsEngine(t *testing.T) {
	w, c, _ := siteRCSWorld(t)
	c.Landing = &spacecraft.LandingAutopilot{Engaged: true}
	v0 := c.State.V
	if w.FireRCSPulse(w.ResolveKeyIntent(IntentNormalPlus)) || c.State.V != v0 {
		t.Error("pulse fired while Site+ auto owned the engine")
	}
	c.Landing.Nose = true // the q hold
	if !w.FireRCSPulse(w.ResolveKeyIntent(IntentNormalPlus)) || c.State.V == v0 {
		t.Error("pulse refused under the q nose hold")
	}
}

// A pulse is a Δv quantum: RCSThrust only gates "has RCS" and is never a force
// (ADR 0067 decision 9: no thrust-based RCS in this ADR).
func TestSiteRCSPulseIgnoresRCSThrust(t *testing.T) {
	dv := func(thrust float64) float64 {
		w, c, _ := siteRCSWorld(t)
		c.Stages[c.LitStage()].RCSThrust, c.RCSThrust = thrust, thrust // not SyncFields: it would drop the fixture's command
		v0 := c.State.V
		if !w.FireRCSPulse(w.ResolveKeyIntent(IntentNormalPlus)) {
			t.Fatalf("pulse refused at RCSThrust %g", thrust)
		}
		return c.State.V.Sub(v0).Norm()
	}
	if a, b := dv(890), dv(1); math.Abs(a-b) > 1e-12 {
		t.Errorf("pulse dv %.9f at 890 N vs %.9f at 1 N, want equal", a, b)
	}
}
