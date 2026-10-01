package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// capture57 is one row of the 2026-09-13 readout-overlap review's
// 57-moon-descent-burn-N captures (G5 grounding A): Apollo LM over the
// Moon, retrograde hold, 100% throttle.
type capture57 struct {
	name     string
	altM     float64
	descent  float64 // surface-relative, positive = falling
	horiz    float64
	fuelKg   float64 // descent stage propellant
	captured string  // what the live HUD printed
}

var captures57 = []capture57{
	{"57-5", 8478, 60.3, 83.5, 1363, "stop margin 7.879 km up (green)"},
	{"57-6", 7528, 11.6, 63.5, 477, "stop margin 7.450 km up (green)"},
	{"57-7", 5340, 79.9, 65.8, 0, "CAN'T STOP (fuel)"},
}

// lm57Craft puts the trimmed Apollo LM [Descent, Ascent] over the Moon in
// the capture's state.
func lm57Craft(t *testing.T, w *World, cp capture57) *spacecraft.Spacecraft {
	t.Helper()
	c := w.ActiveCraft()
	c.Primary = bodyForTest(t, w, "moon")
	c.Landed, c.Crashed = false, false
	stages, _ := spacecraft.BuildModule(spacecraft.StageModuleApolloCSMLMID)
	c.Stages = stages[2:] // the LM alone: Descent + Ascent
	c.Stages[0].FuelMass = cp.fuelKg
	crewTend(c)
	c.SyncFields()
	r := orbital.Vec3{X: c.Primary.RadiusMeters() + cp.altM}
	vRel := orbital.Vec3{X: -cp.descent, Y: cp.horiz}
	c.State.R = r
	c.State.V = vRel.Add(physics.AtmosphereOmega(c.Primary).Cross(r))
	c.State.M = c.TotalMass()
	c.Throttle = 1
	return c
}

// TestReplay57StopForecast is G5 grounding C.1: replay the 57-5..57-7
// states through PredictPoweredStop and record what the forecast said.
// It pins the mechanism (57-6 is a genuine StopStopped with ~85 m/s of
// stop cost, so the green was "can halt", not a lag) and logs the numbers.
func TestReplay57StopForecast(t *testing.T) {
	for _, cp := range captures57 {
		w := mustWorld(t)
		c := lm57Craft(t, w, cp)
		stop, ok := PredictPoweredStop(c, DescentPredictHorizon)
		corr, _ := DescentCorridorFor(c, DescentPredictHorizon)
		m := DeriveMarginState(stop, ok, corr.AltitudeM, BurnAtCue{}, false)
		t.Logf("%s: captured=%q | stage Δv=%.0f m/s | forecast ok=%v outcome=%v margin=%.0f m dvUsed=%.1f elapsed=%.1fs | state=%v limiter=%v",
			cp.name, cp.captured, c.RemainingDeltaV(), ok, stop.Outcome, stop.MarginM, stop.DVUsedMps, stop.ElapsedSec, m.State, m.Limiter)
		switch cp.name {
		case "57-6":
			if !ok || stop.Outcome != StopStopped {
				t.Fatalf("57-6: forecast %v ok=%v, want StopStopped (the green was a true halt)", stop.Outcome, ok)
			}
			if stop.DVUsedMps < 60 || stop.DVUsedMps > 110 {
				t.Errorf("57-6: stop cost %.1f m/s, grounding expects ~85", stop.DVUsedMps)
			}
		case "57-7":
			if stop.Outcome != StopFuelLimited {
				t.Errorf("57-7: outcome %v, want StopFuelLimited (dry stage)", stop.Outcome)
			}
		}
	}
}

// TestStopMarginTurnsBeforeDry (#465, G5 Q3): the green must go before the
// tank is dry. 57-5 (comfortable) stays OK; by 57-6 the stage can halt but
// barely land, so it reads TIGHT; a minute of near-hover burn later (fuel
// still aboard, 57-7 minus a bit) it reads red with the LANDING limiter,
// not fuel, while the forecast still says StopStopped.
func TestStopMarginTurnsBeforeDry(t *testing.T) {
	type step struct {
		name string
		cp   capture57
		want MarginState
	}
	steps := []step{
		{"57-5", captures57[0], MarginOK},
		{"57-6", captures57[1], MarginTight},
		// Hover continues at 57-6's position; the tank drains toward empty.
		{"57-6 hover, 250 kg left", capture57{"hover250", 7528, 11.6, 63.5, 250, ""}, MarginInsufficient},
		{"57-6 hover, 60 kg left", capture57{"hover60", 7528, 11.6, 63.5, 60, ""}, MarginInsufficient},
	}
	for _, s := range steps {
		w := mustWorld(t)
		c := lm57Craft(t, w, s.cp)
		stop, ok := PredictPoweredStop(c, DescentPredictHorizon)
		corr, _ := DescentCorridorFor(c, DescentPredictHorizon)
		m := DeriveMarginState(stop, ok, corr.AltitudeM, BurnAtCue{}, false)
		t.Logf("%s: outcome=%v margin=%.0f m landDV=%.0f stageDVafter=%.0f stageDV=%.0f -> state=%v limiter=%v",
			s.name, stop.Outcome, stop.MarginM, stop.LandDVMps, stop.StageDVAfterStopMps, stop.StageDVMps, m.State, m.Limiter)
		if m.State != s.want {
			t.Errorf("%s: state %v, want %v", s.name, m.State, s.want)
		}
		if s.want == MarginInsufficient && stop.Outcome == StopStopped && m.Limiter != LimitLanding {
			t.Errorf("%s: limiter %v, want landing", s.name, m.Limiter)
		}
	}
}
