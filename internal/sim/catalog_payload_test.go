package sim

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSpawnComsatCarrierComposite — v0.23 / ADR 0028 C3-3. A catalog carrier
// loadout with a baked nose_payload_plan spawns as a deployable composite (the
// plan is honoured at the catalog spawn path, not just the custom-stack path).
// "Comsat Carrier x3" = an NTR relay-tug core under three relay comsats; each
// Deploy pops one comsat while the carrier — itself a controllable relay tug —
// keeps flying.
func TestSpawnComsatCarrierComposite(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    "Comsat-Carrier-3",
		ParentBodyID: "earth",
		AltitudeM:    400e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(Comsat-Carrier-3): %v", err)
	}

	if len(c.Stages) != 4 {
		t.Fatalf("carrier stages = %d, want 4 (tug + 3 comsats)", len(c.Stages))
	}
	if len(c.DockedComponents) != 4 {
		t.Fatalf("carrier DockedComponents = %d, want 4 (core + 3 payloads)", len(c.DockedComponents))
	}
	// Keeps the authored loadout identity (not its core stage name).
	if !strings.HasPrefix(c.Name, "Comsat Carrier") {
		t.Errorf("carrier name = %q, want a Comsat Carrier* name", c.Name)
	}
	// The carrier core is a probe relay-tug, so it stays controllable even after
	// every payload is dropped (this is why the carrier core carries its own
	// command source).
	if !c.Controllable {
		t.Errorf("carrier not Controllable — core lacks a command source")
	}

	// Deploy pops one comsat; the released craft carries a relay antenna.
	if !w.Deploy(w.ActiveCraftIdx) {
		t.Fatal("Deploy returned false on a loaded carrier")
	}
	if len(c.DockedComponents) != 3 {
		t.Errorf("after one Deploy: %d components, want 3", len(c.DockedComponents))
	}
	released := w.Crafts[len(w.Crafts)-1]
	if released.AntennaKind != spacecraft.AntennaRelay {
		t.Errorf("released comsat antenna = %q, want %q (relay)", released.AntennaKind, spacecraft.AntennaRelay)
	}
	if !released.Controllable {
		t.Errorf("released comsat not Controllable — probe command source not restored")
	}

	// Deploying a second identical comsat must get a distinct slate name, so a
	// constellation of identical payloads stays distinguishable in the HUD.
	first := released.Name
	if !w.Deploy(w.ActiveCraftIdx) {
		t.Fatal("second Deploy returned false")
	}
	second := w.Crafts[len(w.Crafts)-1].Name
	if first == second {
		t.Errorf("two deployed comsats share the name %q — they must be uniquely numbered", first)
	}
}

// TestPayloadLoadoutAttributes — v0.23 / ADR 0028 C3-3. The three starter
// deployable-payload loadouts resolve with the parts that give them their
// emergent roles: relay vs direct antenna, soft-land capability.
func TestPayloadLoadoutAttributes(t *testing.T) {
	relay := spacecraft.NewFromLoadout("Relay-Comsat")
	if relay.AntennaKind != spacecraft.AntennaRelay || relay.AntennaRangeM != spacecraft.AntennaRangeRelayCislunar {
		t.Errorf("Relay-Comsat antenna = %q @ %.0fm, want relay @ %.0f", relay.AntennaKind, relay.AntennaRangeM, spacecraft.AntennaRangeRelayCislunar)
	}
	if !relay.Controllable {
		t.Errorf("Relay-Comsat not Controllable — missing probe command source")
	}

	sci := spacecraft.NewFromLoadout("Science-Probe")
	if sci.AntennaKind != spacecraft.AntennaDirect || sci.AntennaRangeM != spacecraft.AntennaRangeDirectBasic {
		t.Errorf("Science-Probe antenna = %q @ %.0fm, want direct @ %.0f", sci.AntennaKind, sci.AntennaRangeM, spacecraft.AntennaRangeDirectBasic)
	}

	gs := spacecraft.NewFromLoadout("Ground-Station-Lander")
	if gs.AntennaKind != spacecraft.AntennaRelay {
		t.Errorf("Ground-Station-Lander antenna = %q, want relay", gs.AntennaKind)
	}
	if !gs.CanSoftLand {
		t.Errorf("Ground-Station-Lander cannot soft-land — it can't become a landed relay node")
	}
}

// TestGroundStationLanderJoinsCommNet — v0.23 / ADR 0028 C3-3 / decision 5. A
// soft-landed relay-antenna craft is automatically a CommNet node: placed at a
// DSN station it comes out connected through the real graph, with no
// "establish station" step. Mirrors TestRecomputeCommGraphIntegration.
func TestGroundStationLanderJoinsCommNet(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	station := spacecraft.NewFromLoadout("Ground-Station-Lander")
	station.SystemIdx = 0
	gs := w.GroundStations[0]
	sys := w.System()
	body := *sys.FindBody(gs.BodyID)
	surface := w.groundStationWorldPos(gs, body).Sub(w.BodyPosition(body))
	station.Primary = body
	station.State.R = surface.Add(surface.Unit().Scale(200000)) // 200 km above the DSN — clear LOS, in range
	w.Crafts[0] = station
	w.EnsureCraftIDs()

	w.RecomputeCommGraph()
	if !w.CommGraph.HasConnection(station.ID) {
		t.Error("a landed relay station at a DSN site should join the CommNet graph")
	}
}
