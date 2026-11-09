package save

import (
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
