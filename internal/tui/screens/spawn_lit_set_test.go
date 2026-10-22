package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0058 S1 review fix: the spawn form reads the marked SLS's lit SET
// (41.29 MN, 295 s), not the SRBs alone. On a g = 13 body the SRBs' 32 MN
// against 2.56 Mt (TWR 0.96) cannot lift off, the set's 41.29 MN (TWR 1.24)
// can; the grid row and the module label read the summed engine too.
func TestSpawnFormReadsTheLitSet(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{superEarthLike()}, "superearth", nil, "", "", nil)
	s.loadoutIdx = loadoutIndex(t, s, "SLS-Block1")
	if !s.launchpadAllowed() {
		t.Error("launchpadAllowed false: SLS lit set 41.29 MN lifts 2.56 Mt on g=13; the gate read the SRBs alone")
	}
	stages := spacecraft.Loadouts["SLS-Block1"].Stages
	rows := layoutCatalog([]catalogRow{{name: "SLS", stages: stages}}, 0)
	if len(rows) != 1 || !strings.Contains(rows[0].head, "41290kN @ Isp 295s") {
		t.Errorf("grid row head = %q, want 41290kN @ Isp 295s", rows[0].head)
	}
}
