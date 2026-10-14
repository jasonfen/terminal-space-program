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
