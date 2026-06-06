package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestBurnTimeForDVIgnoresCutThrottle — GH #89 (thrust.go:496, MEDIUM).
// BurnTimeForDV must derive the planned engine-on duration at the burn's
// firing throttle (full), not the craft's live coast throttle. Pre-fix
// it used s.MassFlowRate() (live throttle); with throttle cut to 0 the
// mass flow was 0 so it returned Duration=0, collapsing every auto-plant
// (inclination / circularise / transfer) into an impulsive node that
// then fires the whole Δv instantaneously. The planned duration must be
// independent of the coast throttle.
func TestBurnTimeForDVIgnoresCutThrottle(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")

	full := NewInLEO(*earth)
	wantSecs := full.BurnTimeForDV(100).Seconds()
	if wantSecs <= 0 {
		t.Fatalf("precondition: full-throttle BurnTimeForDV should be >0, got %v", wantSecs)
	}

	cut := NewInLEO(*earth)
	cut.Throttle = 0 // player coasting with throttle cut
	got := cut.BurnTimeForDV(100).Seconds()
	if got <= 0 {
		t.Fatalf("BurnTimeForDV with cut throttle = %v, want the full-throttle duration (~%.3fs) — a cut coast throttle collapsed the planned finite burn to impulsive", got, wantSecs)
	}
	if math.Abs(got-wantSecs) > 1e-6 {
		t.Errorf("BurnTimeForDV(cut throttle) = %.6fs, want %.6fs (independent of live throttle)", got, wantSecs)
	}
}

// TestRemainingDeltaVUsesActiveStageFuel — GH #89 (thrust.go:443, MEDIUM).
// RemainingDeltaV must budget only the active (bottom) stage's burnable
// propellant. Pre-fix it used s.TotalMass()/(DryMass+Monoprop) where
// s.Fuel is the SUMMED fuel across ALL stages, so a multi-stage craft
// with a near-dry bottom stage but full upper stages reported a Δv
// budget that counts upper-stage fuel as burnable through the spent
// bottom engine.
func TestRemainingDeltaVUsesActiveStageFuel(t *testing.T) {
	// 2-stage craft: bottom stage near-dry, upper stage full. The active
	// engine can only burn the bottom stage's small remaining fuel.
	sc := &Spacecraft{
		Isp: 300,
		Stages: []Stage{
			{Name: "S1", DryMass: 1000, FuelMass: 50, Thrust: 100000, Isp: 300},  // bottom, nearly dry
			{Name: "S2", DryMass: 800, FuelMass: 20000, Thrust: 50000, Isp: 300}, // full upper
		},
	}
	sc.SyncFields()

	got := sc.RemainingDeltaV()

	// The honest budget burns only the bottom stage's 50 kg of fuel
	// against the whole stacked mass sitting on top of that engine.
	stacked := sc.TotalMass()
	floorActive := stacked - 50 // burn the 50 kg of active-stage fuel
	want := 300 * g0 * math.Log(stacked/floorActive)

	if math.Abs(got-want) > want*0.02 {
		t.Errorf("RemainingDeltaV = %.1f m/s, want ~%.1f m/s (active bottom stage only); summing all-stage fuel over-reports the budget", got, want)
	}
	// Sanity: the summed-fuel overestimate would be far larger.
	bogus := 300 * g0 * math.Log(stacked/(sc.DryMass+sc.Monoprop))
	if math.Abs(got-bogus) < want {
		t.Errorf("RemainingDeltaV = %.1f m/s looks like the summed-fuel overestimate (%.1f m/s), not the active-stage budget", got, bogus)
	}
}

// TestApplyImpulsiveCapsAtFuelExhaustion — GH #89 (thrust.go:426, LOW).
// An impulsive node requesting more Δv than the active stage's fuel
// supports must deliver only the deliverable Δv, not the full request.
// Pre-fix the velocity change was applied in full and BurnFuel silently
// clamped the debit, granting free Δv past exhaustion.
func TestApplyImpulsiveCapsAtFuelExhaustion(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)

	budget := sc.RemainingDeltaV()
	if budget <= 0 {
		t.Fatalf("precondition: craft should have a Δv budget, got %v", budget)
	}
	v0 := sc.State.V

	// Request 3× the available budget along prograde.
	sc.ApplyImpulsive(BurnPrograde, budget*3)

	delivered := sc.State.V.Sub(v0).Norm()
	if delivered > budget*1.001 {
		t.Errorf("delivered Δv = %.1f m/s exceeds the fuel budget %.1f m/s — impulsive path granted free Δv past exhaustion", delivered, budget)
	}
	if delivered < budget*0.999 {
		t.Errorf("delivered Δv = %.1f m/s, want ~%.1f m/s (the full available budget)", delivered, budget)
	}
	if sc.ActiveStageFuel() > 1.0 {
		t.Errorf("active stage still holds %.1f kg fuel after an over-budget burn — should be ~empty", sc.ActiveStageFuel())
	}
}

// TestApolloCSMLMCompositeMatchesLoadoutLM — GH #89 (stages_catalog.go:231).
// The "csm-lm" configurator composite is the post-transposition Apollo
// stack, so its LM must carry the same ADR-0009 trimmed propellant as the
// Apollo-Stack loadout — not the untrimmed standalone Lander modules.
// Pre-fix the composite built a 9500/1800 LM vs the loadout's 6310/1269,
// ~0.9 km/s of phantom descent Δv between two ways to fly one mission.
func TestApolloCSMLMCompositeMatchesLoadoutLM(t *testing.T) {
	stages, ok := BuildModule(StageModuleApolloCSMLMID)
	if !ok || len(stages) != 4 {
		t.Fatalf("BuildModule(csm-lm) = %d stages (ok=%v), want 4", len(stages), ok)
	}
	// Order is [SM, CM, Descent, Ascent].
	descent, ascent := stages[2], stages[3]
	if descent.Name != "Descent" || ascent.Name != "Ascent" {
		t.Fatalf("stage order = [%q,%q] for the LM half, want [Descent, Ascent]", descent.Name, ascent.Name)
	}

	// Pull the canonical trimmed LM from the Apollo-Stack loadout.
	lo, ok := Loadouts[LoadoutApolloStackID]
	if !ok {
		t.Fatal("Apollo-Stack loadout missing")
	}
	var loDescent, loAscent Stage
	for _, st := range lo.Stages {
		switch st.Name {
		case "Descent":
			loDescent = st
		case "Ascent":
			loAscent = st
		}
	}

	if descent.FuelMass != loDescent.FuelMass {
		t.Errorf("composite LM descent fuel = %.0f, want %.0f (Apollo-Stack loadout)", descent.FuelMass, loDescent.FuelMass)
	}
	if ascent.FuelMass != loAscent.FuelMass {
		t.Errorf("composite LM ascent fuel = %.0f, want %.0f (Apollo-Stack loadout)", ascent.FuelMass, loAscent.FuelMass)
	}
	// Tanks must read full at the trimmed capacity.
	if descent.FuelMass != descent.FuelCapacity || ascent.FuelMass != ascent.FuelCapacity {
		t.Errorf("trimmed LM stages not full: descent %.0f/%.0f ascent %.0f/%.0f",
			descent.FuelMass, descent.FuelCapacity, ascent.FuelMass, ascent.FuelCapacity)
	}
}

// TestCatalogICPSMatchesStandaloneLoadout — GH #89 (stages_catalog.go:167).
// A catalog part must fly identically to its canonical loadout stage. The
// catalog ICPS thrust must match the standalone ICPS loadout (108000 N),
// not the SLS-embedded variant (110000 N).
func TestCatalogICPSMatchesStandaloneLoadout(t *testing.T) {
	icps, ok := BuildStage(StageModuleICPSID)
	if !ok {
		t.Fatal("catalog ICPS missing")
	}
	lo, ok := Loadouts[LoadoutICPSID]
	if !ok || len(lo.Stages) == 0 {
		t.Fatal("standalone ICPS loadout missing")
	}
	if icps.Thrust != lo.Stages[0].Thrust {
		t.Errorf("catalog ICPS thrust = %.0f N, want %.0f N (standalone ICPS loadout)", icps.Thrust, lo.Stages[0].Thrust)
	}
}
