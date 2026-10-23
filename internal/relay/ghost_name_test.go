package relay

import (
	"testing"
	"unicode"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func TestGhostNamesStripControlCharacters(t *testing.T) {
	viewer, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := viewer.ActiveCraft()
	rep := CraftReport{
		Owner: "SHA256:o", SubspaceTime: viewer.Clock.SimTime,
		Crafts: []CraftState{{
			ID: 1, Name: "Evil\x1b[2J\nOne", System: viewer.System().Name,
			Primary: c.Primary.ID, R: c.State.R, V: c.State.V,
			Ports: []PortState{{Name: "dock\x1b[2J"}},
		}},
	}
	gs := GhostsFor(viewer, []CraftReport{rep}, nil)
	if len(gs) != 1 {
		t.Fatalf("ghosts = %d, want 1", len(gs))
	}
	check := func(what, s string) {
		for _, r := range s {
			if !unicode.IsPrint(r) {
				t.Errorf("%s %q holds non-printable rune %U", what, s, r)
			}
		}
	}
	check("ghost name", gs[0].Name)
	for _, p := range gs[0].Ports {
		check("ghost port name", p.Name)
	}
	if len(gs[0].Ports) == 0 {
		t.Log("note: ghost has no ports in this fixture (hull unknown); port check vacuous")
	}
}
