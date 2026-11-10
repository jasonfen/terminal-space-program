package save

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// A pre-v0.8.2 save carries no LoadoutID, glyph, colour or role, and no
// per-stage breakdown: what it describes is a bare S-IVB, the only loadout
// there was. Loading it must backfill the S-IVB-1 loadout, not today's
// default start vessel (Apollo-TLI, a 55 m mission stack it is not): review
// finding 6.
func TestPreV082SaveBackfillsTheBareSIVBNotTheStartVessel(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutSIVB1ID)
	c.Primary = w.ActiveCraft().Primary
	c.State = w.ActiveCraft().State
	c.State.M = c.TotalMass()
	wc := CraftToWire(c)
	// The pre-v0.8.2 shape: flat fields only.
	wc.Stages = nil
	wc.LoadoutID, wc.Role, wc.Glyph, wc.Color = "", "", "", ""

	loaded, err := CraftFromWire(wc, systems)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LoadoutID != spacecraft.LoadoutSIVB1ID {
		t.Errorf("LoadoutID = %q, want %q (the vessel is a bare S-IVB)", loaded.LoadoutID, spacecraft.LoadoutSIVB1ID)
	}
	if want := spacecraft.Loadouts[spacecraft.LoadoutSIVB1ID].Role; loaded.Role != want {
		t.Errorf("Role = %q, want %q", loaded.Role, want)
	}
	if loaded.Role == spacecraft.Loadouts[spacecraft.LoadoutDefaultID].Role {
		t.Errorf("Role = %q is the start vessel's mission-stack role", loaded.Role)
	}
}

// P31 (ADR 0067): a saved LM (Apollo-TLI's Descent + Ascent, saved before the
// Apollo parts had their antenna) takes the catalog 1e8 antenna on load. Equal
// case: a stage already carrying it is unchanged. A stack whose names do not
// match its loadout, and an unknown loadout id, keep the plain 1e7 backfill.
func TestSavedApolloLMTakesTheCatalogAntennaOnLoad(t *testing.T) {
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	load := func(loadoutID string, strip bool, ids ...string) float64 {
		var st []spacecraft.Stage
		for _, id := range ids {
			s, ok := spacecraft.BuildStage(id)
			if !ok {
				t.Fatalf("no part %q", id)
			}
			st = append(st, s)
		}
		c := spacecraft.NewFromStages(st)
		c.LoadoutID = loadoutID
		c.Primary = w.ActiveCraft().Primary
		c.State = w.ActiveCraft().State
		wc := CraftToWire(c)
		for i := range wc.Stages {
			if strip {
				wc.Stages[i].AntennaKind, wc.Stages[i].AntennaRangeM = "", 0
			}
		}
		got, err := CraftFromWire(wc, systems)
		if err != nil {
			t.Fatal(err)
		}
		return got.AntennaRangeM
	}
	lm := []string{"apollo-descent", "apollo-ascent"}
	if got := load("Apollo-TLI", true, lm...); got != 1e8 {
		t.Errorf("saved LM without an antenna loads at %g, want the catalog 1e8", got)
	}
	if got := load("Apollo-TLI", false, lm...); got != 1e8 {
		t.Errorf("LM already carrying 1e8 loads at %g, want it unchanged", got)
	}
	if got := load("Custom", true, lm...); got != spacecraft.DefaultProbeAntennaRangeM {
		t.Errorf("unknown loadout id loads at %g, want the 1e7 backfill", got)
	}
	if got := load("Apollo-TLI", true, "apollo-ascent", "apollo-descent"); got != spacecraft.DefaultProbeAntennaRangeM {
		t.Errorf("stack out of catalog order loads at %g, want the 1e7 backfill", got)
	}
}

// ADR 0067 decision 9: an LM saved before v0.67.0 (stages on the dry-mass RCS
// scaling, 242 kg across the pair) takes the catalog 890 N / 287 kg on load,
// keeping its fill fraction. Equal cases: a stage already on the catalog figures
// is unchanged; an edited dry mass, and an unknown loadout, are left alone.
func TestSavedLMTakesTheCatalogRCSOnLoad(t *testing.T) {
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	load := func(loadoutID string, old bool, dryDelta float64) []spacecraft.Stage {
		var st []spacecraft.Stage
		for _, id := range []string{"apollo-descent", "apollo-ascent"} {
			s, _ := spacecraft.BuildStage(id)
			s.DryMass += dryDelta
			if old { // as a pre-v0.67.0 save stored it, 40% of the tank spent
				mp, capKg, thr, _ := spacecraft.DefaultRCSLoadout(s.DryMass)
				s.MonopropCap, s.MonopropMass, s.RCSThrust = capKg, mp*0.6, thr
			}
			st = append(st, s)
		}
		c := spacecraft.NewFromStages(st)
		c.LoadoutID = loadoutID
		c.Primary = w.ActiveCraft().Primary
		c.State = w.ActiveCraft().State
		got, err := CraftFromWire(CraftToWire(c), systems)
		if err != nil {
			t.Fatal(err)
		}
		return got.Stages
	}
	st := load("Apollo-TLI", true, 0)
	if cap := st[0].MonopropCap + st[1].MonopropCap; math.Abs(cap-287) > 0.05 || st[0].RCSThrust != 890 || st[1].RCSThrust != 890 {
		t.Errorf("old LM loads with %.1f kg / %.0f N, want 287 / 890", cap, st[0].RCSThrust)
	}
	if got, want := st[0].MonopropMass, 0.6*st[0].MonopropCap; math.Abs(got-want) > 1e-6 {
		t.Errorf("descent monoprop %.2f kg, want the 60%% fill %.2f", got, want)
	}
	st = load("Apollo-TLI", false, 0)
	if st[0].MonopropCap != 193.9 || st[1].MonopropCap != 93.1 {
		t.Errorf("a stage already on the catalog figures changed: %.2f / %.2f", st[0].MonopropCap, st[1].MonopropCap)
	}
	if st = load("Apollo-TLI", true, 50); st[0].MonopropCap == 193.9 {
		t.Errorf("an edited-mass stage was moved to the catalog figures")
	}
	if st = load("Custom", true, 0); st[0].MonopropCap == 193.9 {
		t.Errorf("an unknown loadout was moved to the catalog figures")
	}
}

// L7 follow-up: the landing computer's era survives save->load for stacks the
// catalog run cannot match (VAB-built, edited, no loadout id).
func TestNavSigmaSurvivesSaveLoadWithoutACatalogRun(t *testing.T) {
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	for name, loadoutID := range map[string]string{"no loadout id": "", "unknown loadout": "Custom", "catalog loadout, edited stack": "Apollo-TLI"} {
		d, _ := spacecraft.BuildStage("apollo-descent")
		a, _ := spacecraft.BuildStage("apollo-ascent")
		a.Name = "Edited Ascent" // never matches a catalog run
		c := spacecraft.NewFromStages([]spacecraft.Stage{d, a})
		c.LoadoutID = loadoutID
		c.Primary = w.ActiveCraft().Primary
		c.State = w.ActiveCraft().State
		wa, wb, wok := c.NavSigma()
		if !wok {
			t.Fatalf("%s: setup: no Apollo sigma", name)
		}
		got, err := CraftFromWire(CraftToWire(c), systems)
		if err != nil {
			t.Fatal(err)
		}
		if ga, gb, gok := got.NavSigma(); !gok || ga != wa || gb != wb {
			t.Errorf("%s: sigma %v/%v ok=%v after load, want %v/%v", name, ga, gb, gok, wa, wb)
		}
	}
}
