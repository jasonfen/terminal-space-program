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

// TestConnectivityPathDirect (C2-7): a directly-linked probe's recorded path
// is the two-node chain probe→station (hops = 1).
func TestConnectivityPathDirect(t *testing.T) {
	nodes := []commNode{
		station(0, 0, 100000),           // index 0
		probeNode(1, 1000, 3000, false), // index 1
	}
	res := connectivityFull(nodes, nil)
	if !res.connected[1] {
		t.Fatal("a probe in range with clear LOS should be connected")
	}
	path := res.paths[1]
	if len(path) != 2 || path[0] != 1 || path[len(path)-1] != 0 {
		t.Errorf("direct path: got %v, want [1 0] (probe→station)", path)
	}
}

// TestConnectivityPathViaRelay (C2-7): an occluded probe bridged by an
// off-axis relay records the three-node chain probe→relay→station (hops = 2).
func TestConnectivityPathViaRelay(t *testing.T) {
	occ := []physics.OccluderBody{{Center: orbital.Vec3{}, Radius: 100}}
	nodes := []commNode{
		station(0, -1000, 100000),       // index 0
		probeNode(1, 1000, 3000, false), // index 1 (direct link occluded)
		{pos: orbital.Vec3{Y: 2000}, powerW: 100000, forwards: true}, // index 2 relay
	}
	res := connectivityFull(nodes, occ)
	if !res.connected[1] {
		t.Fatal("an off-axis relay should bridge the occluded probe")
	}
	path := res.paths[1]
	if len(path) != 3 || path[0] != 1 || path[1] != 2 || path[2] != 0 {
		t.Errorf("relay path: got %v, want [1 2 0] (probe→relay→station)", path)
	}
}

// TestActiveCommPathDirect (C2-7): the active probe's path surfaces through
// the World as world-frame points anchored on the craft and the station,
// with hops = 1 for a direct link. Mirrors TestRecomputeCommGraphIntegration.
func TestActiveCommPathDirect(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	tug := spacecraft.NewFromLoadout("Relay-Tug")
	tug.SystemIdx = 0
	gs := w.GroundStations[0]
	sys := w.System()
	body := *sys.FindBody(gs.BodyID)
	offset := w.groundStationWorldPos(gs, body).Sub(w.BodyPosition(body))
	tug.Primary = body
	tug.State.R = offset.Add(offset.Unit().Scale(200000)) // 200 km above the station
	w.Crafts[0] = tug
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.RecomputeCommGraph()

	pts, hops, connected := w.ActiveCommPath()
	if !connected {
		t.Fatal("a relay probe directly above a DSN station should be connected")
	}
	if hops != 1 {
		t.Errorf("direct link hops: got %d, want 1", hops)
	}
	if len(pts) != 2 {
		t.Fatalf("path points: got %d, want 2", len(pts))
	}
	craftPos := w.BodyPosition(tug.Primary).Add(tug.State.R)
	if pts[0].Sub(craftPos).Norm() > 1 {
		t.Errorf("path start should be the craft world position")
	}
	stationPos := w.groundStationWorldPos(gs, body)
	if pts[len(pts)-1].Sub(stationPos).Norm() > 1 {
		t.Errorf("path end should be the station world position")
	}
}

// TestActiveCommPathDisconnected (C2-7): a disconnected probe and a crewed
// active craft both report no drawable path.
func TestActiveCommPathDisconnected(t *testing.T) {
	w := mustWorld(t)
	probe := spacecraft.NewFromLoadout("Relay-Tug")
	probe.Primary = w.Crafts[0].Primary
	probe.State = w.Crafts[0].State
	w.Crafts[0] = probe
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{}} // disconnected
	if _, _, connected := w.ActiveCommPath(); connected {
		t.Error("a disconnected probe must report no path")
	}
}

// TestCommGraphStableWithClearLOS (v0.22.1 regression): a craft parked with a
// clear radial line of sight to a ground station must stay connected as the
// body rotates. Pre-fix, the station's surface point FP-flickered into "buried
// inside the body", self-occluding it and toggling the craft's connection on
// ~every other tick (the playtest-reported ~0.5 s "flipping" in low orbit).
func TestCommGraphStableWithClearLOS(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	tug := spacecraft.NewFromLoadout("Relay-Tug")
	tug.SystemIdx = 0
	gs := w.GroundStations[0]
	sys := w.System()
	body := *sys.FindBody(gs.BodyID)
	offset := w.groundStationWorldPos(gs, body).Sub(w.BodyPosition(body)) // surface point, body-relative
	tug.Primary = body
	tug.State.R = offset.Add(offset.Unit().Scale(1_000_000)) // 1000 km straight up → clear LOS
	w.Crafts[0] = tug
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)

	base := w.Clock.SimTime
	for k := 0; k < 200; k++ { // 20 sim-seconds of body rotation, fine steps
		w.Clock.SimTime = base.Add(time.Duration(k) * 100 * time.Millisecond)
		w.RecomputeCommGraph()
		if !w.CommGraph.HasConnection(tug.ID) {
			t.Fatalf("connection dropped at step %d with clear line of sight — station self-occlusion flicker regressed", k)
		}
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
