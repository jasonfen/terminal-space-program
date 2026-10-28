package sim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// spawnStarlinkDesign spawns a player's VAB design through the real design
// spawn path (SpawnSpec.DesignID): F9-S1, F9-S2 and three Relay Comsats with
// nose_payload_plan [1,1,1], so the carrier core is the TWO-stage F9 and each
// comsat is its own nose payload. testdata/designs/starlink.json is a verbatim
// copy of the design from the 2026-10-17 playtest report.
func spawnStarlinkDesign(t *testing.T, body string, altM float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "terminal-space-program", "designs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "designs", "starlink.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "starlink.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", cfg)
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{DesignID: "starlink", ParentBodyID: body, AltitudeM: altM})
	if err != nil {
		t.Fatalf("SpawnCraft(starlink design): %v", err)
	}
	return w, c
}

// TestUndockAfterStagingKeepsPayloadAntennas pins the 2026-10-17 playtest: a
// Falcon 9 with three comsats on top flew to the Moon, and when the comsats
// came off "the rocket lost its communication link ... it said the antenna
// wasn't sufficient" (COMMS: "⚠ NO SIGNAL: needs a stronger antenna").
//
// Measured cause: staging the F9-S1 away shortens c.Stages but leaves the
// carrier-core DockedComponent claiming two stages (F9-S1 + F9-S2). The
// components then sum to 5 stages against 4 live ones, so Undock drops to its
// legacy single-stage rebuild, which carries no command source or antenna:
// every comsat comes back as a "Comsat" with the backfilled 1e7 m direct
// antenna instead of its 1e9 m relay, and the active F9-S2 gets the same 1e7 m
// antenna. At the Moon (~3.8e8 m out) a 1e7 m antenna reaches the 5e9 m DSN to
// only sqrt(1e7*5e9) = 2.2e8 m, and none of the comsats forwards, so the whole
// group reads OutOfRange. Without the S1 staging (useBreakdown path) the same
// Undock keeps the relays and the F9-S2 connects through them.
func TestUndockAfterStagingKeepsPayloadAntennas(t *testing.T) {
	w, c := spawnStarlinkDesign(t, "moon", 100e3)
	idx := w.ActiveCraftIdx

	if _, _, err := w.StageActive(idx); err != nil { // drop the F9-S1, as every real flight does
		t.Fatalf("stage F9-S1: %v", err)
	}
	// The component breakdown must still tile the live stack after staging.
	sum := 0
	for _, comp := range c.DockedComponents {
		sum += len(comp.Stages)
	}
	if sum != len(c.Stages) {
		t.Errorf("after staging F9-S1: docked components claim %d stages, live stack has %d (stale carrier-core breakdown)",
			sum, len(c.Stages))
	}

	undockAll(t, w, idx)
	w.RecomputeCommGraph()

	comsats := 0
	for _, k := range w.Crafts {
		if k == nil || k.SystemIdx != w.SystemIdx || len(k.Stages) != 1 {
			continue
		}
		if k.Stages[0].Name != "Relay Comsat" && k.Name != "Comsat" {
			continue
		}
		comsats++
		if k.AntennaKind != spacecraft.AntennaRelay || k.AntennaRangeM != spacecraft.AntennaRangeRelayCislunar {
			t.Errorf("released %q: antenna %s/%.0e m, want relay/%.0e m (the Relay Comsat part's own antenna)",
				k.Name, k.AntennaKind, k.AntennaRangeM, spacecraft.AntennaRangeRelayCislunar)
		}
	}
	if comsats != 3 {
		t.Errorf("found %d released comsats, want 3", comsats)
	}

	active := w.ActiveCraft()
	if !w.CommGraph.HasConnection(active.ID) {
		t.Errorf("active %q in lunar orbit after undock: no connection (reason %d, antenna %s/%.0e m); want it relayed through the comsats beside it",
			active.Name, w.CommGraph.Reason(active.ID), active.AntennaKind, active.AntennaRangeM)
	}
}
