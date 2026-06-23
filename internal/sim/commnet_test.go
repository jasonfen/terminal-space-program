package sim

import (
	"errors"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestCommandGateBlocksDisconnectedProbe (C2-5, ADR 0027): the command
// methods refuse to mutate an out-of-contact unmanned probe and raise the
// NO SIGNAL flash; the onboard plan is untouched.
func TestCommandGateBlocksDisconnectedProbe(t *testing.T) {
	w := mustWorld(t)
	probe := spacecraft.NewFromLoadout("Relay-Tug")
	probe.Primary = w.Crafts[0].Primary
	probe.State = w.Crafts[0].State
	w.Crafts[0] = probe
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{}} // force "no connection"

	w.PlanNode(ManeuverNode{TriggerTime: w.Clock.SimTime.Add(time.Hour), DV: 10, Mode: spacecraft.BurnPrograde})
	if len(probe.Nodes) != 0 {
		t.Error("PlanNode must be blocked for a disconnected probe")
	}
	if _, ok := w.CommBlockedFlash(); !ok {
		t.Error("a blocked command must raise the NO SIGNAL flash")
	}
	probe.Throttle = 0.5
	w.SetThrottle(1.0)
	if probe.Throttle != 0.5 {
		t.Error("SetThrottle must be blocked for a disconnected probe")
	}
	if _, _, err := w.StageActive(0); !errors.Is(err, ErrNoSignal) {
		t.Errorf("StageActive on a disconnected probe: got %v, want ErrNoSignal", err)
	}
}

// TestCommandGateBlocksManualBurnAndRCS (hardening, ADR 0027): the gate must
// also cover engine ignition (StartManualBurn, the `b` key) and RCS pulses —
// the two direct-thrust commands the original C2-5 slice missed. A
// disconnected probe is refused on both; a connection lets ignition through,
// proving the craft can burn and the gate was the only blocker.
func TestCommandGateBlocksManualBurnAndRCS(t *testing.T) {
	w := mustWorld(t)
	probe := spacecraft.NewFromLoadout("Relay-Tug")
	probe.Primary = w.Crafts[0].Primary
	probe.State = w.Crafts[0].State
	w.Crafts[0] = probe
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{}} // disconnected

	w.StartManualBurn()
	if probe.ManualBurn != nil {
		t.Error("StartManualBurn must be blocked for a disconnected probe")
	}
	if _, ok := w.CommBlockedFlash(); !ok {
		t.Error("a blocked ignition must raise the NO SIGNAL flash")
	}

	probe.EngineMode = spacecraft.EngineRCS
	if w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Error("FireRCSPulse must be blocked for a disconnected probe")
	}

	// Reconnect → ignition proceeds.
	probe.EngineMode = spacecraft.EngineMain
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{probe.ID: true}}
	w.StartManualBurn()
	if probe.ManualBurn == nil {
		t.Error("a connected probe with fuel+thrust must ignite")
	}
}

// TestCommandGateBlocksClearNodesAndTranspose (hardening, ADR 0027): the
// wipe-all ClearNodes (sibling of the gated DeleteNode) and Transpose
// (sibling of the gated StageActive) must also refuse a disconnected probe.
func TestCommandGateBlocksClearNodesAndTranspose(t *testing.T) {
	w := mustWorld(t)
	probe := spacecraft.NewFromLoadout("Relay-Tug")
	probe.Primary = w.Crafts[0].Primary
	probe.State = w.Crafts[0].State
	probe.Nodes = []ManeuverNode{{TriggerTime: w.Clock.SimTime.Add(time.Hour), DV: 10, Mode: spacecraft.BurnPrograde}}
	w.Crafts[0] = probe
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{}} // disconnected

	w.ClearNodes()
	if len(probe.Nodes) != 1 {
		t.Error("ClearNodes must be blocked for a disconnected probe (node should remain)")
	}

	// A TransposeReady but unmanned, disconnected probe is refused with ErrNoSignal.
	tp := &spacecraft.Spacecraft{
		Controllable: true,
		Crewed:       false,
		Stages: []spacecraft.Stage{
			{Name: "Descent"}, {Name: "Ascent"}, {Name: "SM"}, {Name: "CM"},
		},
	}
	w.Crafts[0] = tp
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{}}
	if err := w.Transpose(0); !errors.Is(err, ErrNoSignal) {
		t.Errorf("Transpose on a disconnected probe: got %v, want ErrNoSignal", err)
	}
}

// TestCommandGateAllowsConnectedAndCrewed (C2-5): a connected probe and a
// crewed vessel both accept commands.
func TestCommandGateAllowsConnectedAndCrewed(t *testing.T) {
	w := mustWorld(t)
	probe := spacecraft.NewFromLoadout("Relay-Tug")
	probe.Primary = w.Crafts[0].Primary
	probe.State = w.Crafts[0].State
	w.Crafts[0] = probe
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{probe.ID: true}}
	w.PlanNode(ManeuverNode{TriggerTime: w.Clock.SimTime.Add(time.Hour), DV: 10, Mode: spacecraft.BurnPrograde})
	if len(probe.Nodes) != 1 {
		t.Error("a connected probe must accept a node")
	}

	crew := spacecraft.NewFromLoadout(spacecraft.LoadoutCapsuleID)
	crew.Primary = w.Crafts[0].Primary
	crew.State = w.Crafts[0].State
	w.Crafts = append(w.Crafts, crew)
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(1)
	w.CommGraph = nil // even with no graph, a crewed craft bypasses connectivity
	w.SetThrottle(0.7)
	if crew.Throttle != 0.7 {
		t.Error("a crewed craft must accept commands regardless of connectivity")
	}
}

// station / probe / relay node builders for the synthetic-graph tests.
// Positions are in meters along the X axis; powers chosen so the default
// CommRangePerWatt makes "near" links close and "far" links break.
func station(id uint64, x, p float64) commNode {
	return commNode{pos: orbital.Vec3{X: x}, powerW: p, forwards: true, station: true}
}
func probeNode(id uint64, x, p float64, relay bool) commNode {
	return commNode{pos: orbital.Vec3{X: x}, powerW: p, probe: true, craftID: id, forwards: relay}
}
func relayNode(x, p float64) commNode {
	return commNode{pos: orbital.Vec3{X: x}, powerW: p, forwards: true}
}

func TestConnectivityDirectLink(t *testing.T) {
	// Probe (1 km from a 100kW station, direct antenna 3kW): well in range,
	// clear LOS → connected.
	nodes := []commNode{
		station(0, 0, 100000),
		probeNode(1, 1000, 3000, false),
	}
	got := connectivity(nodes, nil)
	if !got[1] {
		t.Error("a probe in range with clear LOS should be connected")
	}
}

func TestConnectivityOutOfRange(t *testing.T) {
	// min(power) = 3000 → range = 3000 * CommRangePerWatt. Place the probe
	// just past it.
	rng := commLinkRangeM(100000, 3000)
	nodes := []commNode{
		station(0, 0, 100000),
		probeNode(1, rng*1.1, 3000, false),
	}
	if connectivity(nodes, nil)[1] {
		t.Error("a probe beyond link range should not be connected")
	}
}

func TestConnectivityOccludedNeedsRelay(t *testing.T) {
	// A body sits between station (x=-1000) and probe (x=+1000); the direct
	// link is blocked. A relay parked off-axis with LOS to both bridges it.
	occ := []physics.OccluderBody{{Center: orbital.Vec3{}, Radius: 100}}
	st := station(0, -1000, 100000)
	pr := probeNode(1, 1000, 3000, false)

	// Without a relay: blocked.
	if connectivity([]commNode{st, pr}, occ)[1] {
		t.Error("a probe occluded by a body with no relay should be disconnected")
	}
	// With an off-axis relay (y=2000) that clears the radius-100 body: linked.
	relay := commNode{pos: orbital.Vec3{Y: 2000}, powerW: 100000, forwards: true}
	if !connectivity([]commNode{st, pr, relay}, occ)[1] {
		t.Error("an off-axis relay with LOS to both should bridge the occluded probe")
	}
}

func TestConnectivityCannotRelayThroughDirectCraft(t *testing.T) {
	// station --- directCraft --- probe, collinear, each hop in range but
	// the body blocks the direct station↔probe link. The middle craft has a
	// DIRECT antenna (not a relay), so it cannot forward → probe stays
	// disconnected.
	occ := []physics.OccluderBody{{Center: orbital.Vec3{}, Radius: 100}}
	nodes := []commNode{
		station(0, -1000, 100000),
		probeNode(2, 1000, 100000, false), // the probe we test (id 2)
		// a direct-only craft sitting off-axis with LOS to both, but a
		// non-forwarder (forwards=false):
		{pos: orbital.Vec3{Y: 2000}, powerW: 100000, forwards: false, craftID: 9},
	}
	if connectivity(nodes, occ)[2] {
		t.Error("a direct-only craft must not relay traffic through itself")
	}
}

// TestCanCommandCraftSemantics (C2-4): crewed → always; debris → never;
// unmanned probe → gated on the connectivity graph.
func TestCanCommandCraftSemantics(t *testing.T) {
	w := &World{}

	crewed := &spacecraft.Spacecraft{Controllable: true, Crewed: true}
	if !w.CanCommandCraft(crewed) {
		t.Error("a crewed vessel must always be commandable")
	}

	debris := &spacecraft.Spacecraft{Controllable: false}
	if w.CanCommandCraft(debris) {
		t.Error("passive debris must never be commandable")
	}
	if w.CanCommandCraft(nil) {
		t.Error("nil craft must not be commandable")
	}

	probe := &spacecraft.Spacecraft{ID: 7, Controllable: true, Crewed: false}
	// Inject a graph: probe 7 disconnected → not commandable.
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{}}
	if w.CanCommandCraft(probe) {
		t.Error("a disconnected unmanned probe must not be commandable")
	}
	// Now connected → commandable.
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{7: true}}
	if !w.CanCommandCraft(probe) {
		t.Error("a connected unmanned probe must be commandable")
	}
}

// TestRecomputeCommGraphIntegration (C2-4): a probe spawned near the home
// DSN ring in LEO comes out connected through the real position pipeline.
func TestRecomputeCommGraphIntegration(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// A relay probe (graph node + BFS source) placed 200 km directly above
	// the first DSN station — guaranteed clear radial LOS + in range,
	// independent of the J2000 rotation phase.
	tug := spacecraft.NewFromLoadout("Relay-Tug")
	tug.SystemIdx = 0
	gs := w.GroundStations[0]
	sys := w.System()
	body := *sys.FindBody(gs.BodyID)
	offset := w.groundStationWorldPos(gs, body).Sub(w.BodyPosition(body)) // surface point, body-relative
	tug.Primary = body
	tug.State.R = offset.Add(offset.Unit().Scale(200000)) // 200 km above the station
	w.Crafts[0] = tug
	w.EnsureCraftIDs()

	w.RecomputeCommGraph()
	if !w.CommGraph.HasConnection(tug.ID) {
		t.Error("a relay probe directly above a DSN station should be connected")
	}
	if !w.CanCommandCraft(tug) {
		t.Error("a connected probe should be commandable")
	}

	// Sanity: the same probe with NO antenna (strip it) is not a node and
	// has no connection.
	tug.Stages[len(tug.Stages)-1].AntennaKind = spacecraft.AntennaNone
	tug.SyncFields()
	w.RecomputeCommGraph()
	if w.CommGraph.HasConnection(tug.ID) {
		t.Error("a probe with no antenna cannot have a connection")
	}
}
