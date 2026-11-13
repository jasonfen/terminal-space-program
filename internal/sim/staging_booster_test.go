package sim

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// slsWorld puts the marked SLS (ADR 0058) in the active slot, in orbit.
func slsWorld(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	sls := spacecraft.NewFromLoadout("SLS-Block1")
	sls.Primary = w.Crafts[0].Primary
	sls.State = w.Crafts[0].State
	w.Crafts[0] = sls
	w.ActiveCraftIdx = 0
	crewTendActive(w)
	sls.SyncFields()
	sls.State.M = sls.TotalMass()
	if !sls.Stages[0].LitWithAbove || sls.Stages[0].FuelType != spacecraft.FuelTypeSolid {
		t.Fatalf("setup: SLS bottom stage not a marked solid: %+v", sls.Stages[0])
	}
	return w, sls
}

func lightSLS(t *testing.T, w *World, c *spacecraft.Spacecraft) {
	t.Helper()
	c.Throttle = 1
	c.EngineMode = spacecraft.EngineMain
	w.StartManualBurn()
	if c.ManualBurn == nil {
		t.Fatal("setup: burn did not start")
	}
}

// Guard 4 (ADR 0058 decision 15): the refusal cases.
func TestStageRefusedWhileLitSolidsBurn(t *testing.T) {
	w, c := slsWorld(t)
	lightSLS(t, w, c)
	n, stages := len(w.Crafts), len(c.Stages)
	_, _, err := w.StageActive(0)
	if !errors.Is(err, ErrStageSolidsLit) {
		t.Fatalf("lit solids mid-burn: err = %v, want ErrStageSolidsLit", err)
	}
	if len(w.Crafts) != n || len(c.Stages) != stages {
		t.Errorf("refusal changed the slate: crafts %d->%d stages %d->%d", n, len(w.Crafts), stages, len(c.Stages))
	}
	if !strings.Contains(ErrStageSolidsLit.Error(), "boosters still burning") {
		t.Errorf("flash text = %q", ErrStageSolidsLit.Error())
	}
}

func TestStageUnlitSolidsDrop(t *testing.T) {
	w, _ := slsWorld(t) // never lit: the pad before b
	if _, _, err := w.StageActive(0); err != nil {
		t.Fatalf("unlit solids: err = %v, want a drop", err)
	}
}

func TestStageDrySolidsDropMidBurn(t *testing.T) {
	w, c := slsWorld(t)
	lightSLS(t, w, c)
	c.Stages[0].FuelMass = 0
	c.SyncFields()
	if _, _, err := w.StageActive(0); err != nil {
		t.Fatalf("dry solids mid-burn: err = %v, want a drop", err)
	}
}

func TestStageLiquidBoosterDropsLitOrDry(t *testing.T) {
	for _, dry := range []bool{false, true} {
		w := mustWorld(t)
		c := w.ActiveCraft()
		c.Stages = []spacecraft.Stage{
			{Name: "LRB", DryMass: 20000, FuelMass: 100000, FuelCapacity: 100000, Thrust: 9.29e6, Isp: 452, FuelType: spacecraft.FuelTypeHydrolox, LitWithAbove: true},
			{Name: "Core", DryMass: 40000, FuelMass: 300000, FuelCapacity: 300000, Thrust: 9.29e6, Isp: 452, FuelType: spacecraft.FuelTypeHydrolox},
		}
		if dry {
			c.Stages[0].FuelMass = 0
		}
		crewTend(c)
		c.SyncFields()
		c.State.M = c.TotalMass()
		lightSLS(t, w, c)
		if _, _, err := w.StageActive(0); err != nil {
			t.Fatalf("liquid booster (dry=%v) mid-burn: err = %v, want a drop", dry, err)
		}
	}
}

// Guard 5: a marked stage drops as two debris vessels, half the stage each.
func TestBoosterDropSpawnsTwoDebris(t *testing.T) {
	w, c := slsWorld(t)
	lightSLS(t, w, c)
	c.Stages[0].FuelMass = 0 // burned out, so the gate lets it go
	c.SyncFields()
	parentName := c.Name
	full := c.Stages[0]
	if _, _, err := w.StageActive(0); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if len(w.Crafts) != 3 {
		t.Fatalf("slate = %d vessels, want 3 (core + two boosters)", len(w.Crafts))
	}
	b1, b2 := w.Crafts[1], w.Crafts[2]
	if b1.Name != parentName+"-booster1" || b2.Name != parentName+"-booster2" {
		t.Errorf("names = %q, %q, want %q-booster1/2", b1.Name, b2.Name, parentName)
	}
	for _, b := range []*spacecraft.Spacecraft{b1, b2} {
		if b.Role != spacecraft.RoleJettisonedStage {
			t.Errorf("%s role = %q", b.Name, b.Role)
		}
		if b.Controllable {
			t.Errorf("%s has a command source", b.Name)
		}
		if len(b.Stages) != 1 {
			t.Fatalf("%s stages = %d", b.Name, len(b.Stages))
		}
		s := b.Stages[0]
		// 198,000/2 = 99,000 kg dry; 32.0e6/2 = 16.0e6 N; Isp and BC unchanged.
		if math.Abs(s.DryMass-99000) > 1e-6 || math.Abs(s.Thrust-16.0e6) > 1e-3 {
			t.Errorf("%s dry %v kg thrust %v N, want 99000 / 16.0e6", b.Name, s.DryMass, s.Thrust)
		}
		if s.Isp != full.Isp || s.BallisticCoefficient != full.BallisticCoefficient {
			t.Errorf("%s Isp %v BC %v changed from %v / %v", b.Name, s.Isp, s.BallisticCoefficient, full.Isp, full.BallisticCoefficient)
		}
		if math.Abs(s.FuelCapacity-full.FuelCapacity/2) > 1e-6 || math.Abs(s.MonopropCap-full.MonopropCap/2) > 1e-6 {
			t.Errorf("%s capacity %v / monoprop cap %v not halved", b.Name, s.FuelCapacity, s.MonopropCap)
		}
		if s.LitWithAbove {
			t.Errorf("%s still marked", b.Name)
		}
	}
	// Separation: 120 m apart, each 60*sqrt(2) = 84.85 m from the core.
	core := c.State.R
	d12 := b1.State.R.Sub(b2.State.R).Norm()
	d1, d2 := b1.State.R.Sub(core).Norm(), b2.State.R.Sub(core).Norm()
	if math.Abs(d12-120) > 0.01 {
		t.Errorf("booster-booster = %.3f m, want 120", d12)
	}
	want := 60 * math.Sqrt2
	if math.Abs(d1-want) > 0.01 || math.Abs(d2-want) > 0.01 {
		t.Errorf("booster-core = %.3f, %.3f m, want %.3f", d1, d2, want)
	}
	for k := 0; k < 3; k++ {
		w.Tick()
		if _, _, docked := w.checkDocking(); docked {
			t.Fatalf("debris re-docked on tick %d", k)
		}
	}
	if len(w.Crafts) != 3 {
		t.Errorf("slate after ticks = %d, want 3", len(w.Crafts))
	}
}

func TestBoosterNamesSuffixOnlyWhenTaken(t *testing.T) {
	w, c := slsWorld(t)
	c.Stages[0].FuelMass = 0
	c.SyncFields()
	base := c.Name // StageActive renames c to its new bottom stage
	taken := *spacecraft.NewFromLoadout("SLS-Block1")
	taken.Name = base + "-booster1"
	w.Crafts = append(w.Crafts, &taken)
	if _, _, err := w.StageActive(0); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, v := range w.Crafts {
		names[v.Name] = true
	}
	if !names[base+"-booster2"] {
		t.Errorf("free name was suffixed: %v", names)
	}
	if !names[base+"-booster1-1"] {
		t.Errorf("taken name did not get the usual -N suffix: %v", names)
	}
}

func TestBoostersBurnedOutPredicate(t *testing.T) {
	w, c := slsWorld(t)
	if BoosterSolidsLit(c) {
		t.Error("lit before ignition")
	}
	lightSLS(t, w, c)
	if !BoosterSolidsLit(c) {
		t.Fatal("not lit with solids burning")
	}
	c.Stages[0].FuelMass = 0
	if BoosterSolidsLit(c) {
		t.Error("still lit with a dry tank")
	}
}

// ADR 0058 decision 10: each dropped half is one side column of the
// pair, so its sprite width is max(1, w/2): SRB 5 -> 2, a 3-wide liquid
// booster -> 1, and a width of 1 (or the unset 0) never halves below 1.
func TestBoosterHalvesHalveTheSpriteWidth(t *testing.T) {
	for _, tc := range []struct{ w, want int }{{5, 2}, {3, 1}, {2, 1}, {1, 1}} {
		halves := splitBoosterStage(spacecraft.Stage{LaunchSpriteWidthPx: tc.w})
		for k, h := range halves {
			if h.LaunchSpriteWidthPx != tc.want {
				t.Errorf("width %d: half %d = %d, want %d", tc.w, k, h.LaunchSpriteWidthPx, tc.want)
			}
		}
	}
	_, c := slsWorld(t)
	if got := splitBoosterStage(c.Stages[0])[0].LaunchSpriteWidthPx; got != 2 {
		t.Errorf("SLS SRB half width = %d, want 2 (full stage is 5)", got)
	}
}

// dropWorld puts catalog loadout id in the active slot, in orbit, burns its
// bottom stage down to fuel kg (so the booster gate lets it go) and drops it.
// Returns the world and the vessels the drop added.
func dropWorld(t *testing.T, id string, fuel float64) (*World, []*spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	c := spacecraft.NewFromLoadout(id)
	c.Primary = w.Crafts[0].Primary
	c.State = w.Crafts[0].State
	w.Crafts[0] = c
	w.ActiveCraftIdx = 0
	crewTendActive(w)
	c.Stages[0].FuelMass = fuel
	c.SyncFields()
	c.State.M = c.TotalMass()
	n := len(w.Crafts)
	if _, _, err := w.StageActive(0); err != nil {
		t.Fatalf("%s: StageActive: %v", id, err)
	}
	return w, w.Crafts[n:]
}

// ADR 0069 decision 11 (slice 8): a dropped stage whose part has legs, an
// engine and a command source keeps its core and antenna and is a vessel; any
// other stage is debris as before. Table covers both sides of the rule.
func TestDroppedStageThatCanLandIsAVessel(t *testing.T) {
	for _, tc := range []struct {
		id      string
		n       int
		vessel  bool
		wantAnt bool
	}{
		{"Falcon-9", 1, true, true},
		{"Falcon-Heavy", 2, true, true},
		{"Packet-9", 1, true, true},
		{"Saturn-V", 1, false, false},
		{"SLS-Block1", 2, false, false},
		{"Raster-LS", 2, false, false},
		{"Vector-V", 1, false, false},
	} {
		w, drops := dropWorld(t, tc.id, 1000)
		if len(drops) != tc.n {
			t.Fatalf("%s: %d vessels dropped, want %d", tc.id, len(drops), tc.n)
		}
		for _, d := range drops {
			if d.Controllable != tc.vessel {
				t.Errorf("%s %s: Controllable = %v, want %v", tc.id, d.Name, d.Controllable, tc.vessel)
			}
			if (d.AntennaKind != spacecraft.AntennaNone) != tc.wantAnt {
				t.Errorf("%s %s: antenna %q, want present=%v", tc.id, d.Name, d.AntennaKind, tc.wantAnt)
			}
			if tc.vessel && !w.CanCommandCraft(d) {
				t.Errorf("%s %s: no link, a vessel next to the core should have one", tc.id, d.Name)
			}
			if !tc.vessel && w.CanCommandCraft(d) {
				t.Errorf("%s %s: debris takes commands", tc.id, d.Name)
			}
		}
	}
}

// The effect, not the flag: switch to the dropped stage with the vessel keys,
// throttle it, aim it at a site and press N. A vessel gets no capability
// refusal; the Saturn S-IC, the control for this check, gets NO SIGNAL.
func TestDroppedStageTakesCommandsAndN(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want bool
	}{{"Falcon-9", true}, {"Falcon-Heavy", true}, {"Packet-9", true}, {"Saturn-V", false}} {
		w, drops := dropWorld(t, tc.id, 1000)
		d := drops[0]
		w.CycleActiveCraft(1)
		if w.ActiveCraft() != d {
			t.Fatalf("%s: [ ] cycle did not reach the dropped stage", tc.id)
		}
		var lc13 Site
		for _, z := range shippedZones {
			if z.Name == "LC-13" {
				lc13 = z
			}
		}
		w.SetTargetSiteLZ(lc13.ID, 1)
		_, err := w.PlanDeorbitAndEngage()
		refused := errors.Is(err, ErrDeorbitNoSignal) || errors.Is(err, ErrDeorbitNoEngine) || errors.Is(err, ErrDeorbitNoSite)
		if tc.want && refused {
			t.Errorf("%s: N refused a vessel: %v", tc.id, err)
		}
		if !tc.want && !errors.Is(err, ErrDeorbitNoSignal) {
			t.Errorf("%s: debris N err = %v, want NO SIGNAL (the check must be able to refuse)", tc.id, err)
		}
		d.Throttle = 0
		w.SetThrottle(0.5)
		if got := d.Throttle == 0.5; got != tc.want {
			t.Errorf("%s: throttle took = %v, want %v", tc.id, got, tc.want)
		}
	}
}

// ADR 0070: a half carries half the catalog thrust, so its engine still reads
// the half thrust after the next tick sets it for the air.
func TestBoosterHalvesHalveTheCatalogThrust(t *testing.T) {
	_, c := slsWorld(t)
	full := c.Stages[0]
	half := splitBoosterStage(full)[0]
	if full.ThrustVac <= 0 || half.ThrustVac != full.ThrustVac/2 {
		t.Fatalf("half ThrustVac = %v, want %v", half.ThrustVac, full.ThrustVac/2)
	}
	half.SetAmbient(0)
	if want := full.ThrustVac / 2; math.Abs(half.Thrust-want) > 1e-6*want {
		t.Errorf("half in vacuum thrusts %v, want %v", half.Thrust, want)
	}
}
