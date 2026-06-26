package sim

import (
	"errors"
	"strings"
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
// Positions are in meters along the X axis; rated ranges chosen so the
// combinability link range (√(rₐ·r_b)) makes "near" links close and "far"
// links break.
func station(id uint64, x, r float64) commNode {
	return commNode{pos: orbital.Vec3{X: x}, rangeM: r, forwards: true, station: true}
}
func probeNode(id uint64, x, r float64, relay bool) commNode {
	return commNode{pos: orbital.Vec3{X: x}, rangeM: r, probe: true, craftID: id, forwards: relay}
}
func relayNode(x, r float64) commNode {
	return commNode{pos: orbital.Vec3{X: x}, rangeM: r, forwards: true}
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
	// rng = √(100000·3000); place the probe just past it.
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
	relay := commNode{pos: orbital.Vec3{Y: 2000}, rangeM: 100000, forwards: true}
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
		{pos: orbital.Vec3{Y: 2000}, rangeM: 100000, forwards: false, craftID: 9},
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
		{pos: orbital.Vec3{Y: 2000}, rangeM: 100000, forwards: true}, // index 2 relay
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

// radialAboveStation0 places craft c at distM straight up from DSN station 0
// (clear radial line of sight, home system), so connectivity is gated by range
// alone. craft-to-station distance == distM.
func radialAboveStation0(t *testing.T, w *World, c *spacecraft.Spacecraft, distM float64) {
	t.Helper()
	gs := w.GroundStations[0]
	sys := w.System()
	body := *sys.FindBody(gs.BodyID)
	up := w.groundStationWorldPos(gs, body).Sub(w.BodyPosition(body)).Unit()
	c.SystemIdx = 0
	c.Primary = body
	c.State.R = up.Scale(body.RadiusMeters() + distM)
}

// connectedAt spawns the given loadout distM straight up from DSN station 0
// (clear LOS) and reports whether it reaches the network — the integration
// seam for the combinability/tier reach (#182, ADR 0027 §2 amendment).
func connectedAt(t *testing.T, w *World, loadout string, distM float64) bool {
	t.Helper()
	c := spacecraft.NewFromLoadout(loadout)
	radialAboveStation0(t, w, c, distM)
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.RecomputeCommGraph()
	return w.CommGraph.HasConnection(c.ID)
}

// TestCommReachTiers (#182): the combinability model + antenna tiers give each
// tier its intended reach against the home DSN. The Relay-Tug fulfils
// Earth/Moon; a basic antenna stops at geostationary; deep-space reaches
// Mars-class; and the range limit is still real.
func TestCommReachTiers(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	const (
		geo     = 35_786e3   // geostationary
		moon    = 384_400e3  // Earth–Moon
		marsish = 40e9       // 40M km — within deep-space reach, beyond the relay tug
		beyond  = 100e9      // 100M km — past even deep-space reach
	)
	// Relay-Tug: reaches geostationary AND the Moon (the headline fix).
	if !connectedAt(t, w, "Relay-Tug", geo) {
		t.Error("Relay-Tug must reach geostationary")
	}
	if !connectedAt(t, w, "Relay-Tug", moon) {
		t.Error("Relay-Tug must reach the Moon (Earth/Moon workhorse)")
	}
	// Basic telemetry (Station-Keeper / direct-basic): geostationary yes, Moon no.
	if !connectedAt(t, w, "Station-Keeper", geo) {
		t.Error("a basic direct antenna must reach geostationary")
	}
	if connectedAt(t, w, "Station-Keeper", moon) {
		t.Error("a basic direct antenna must NOT reach the Moon — that's the relay tug's job")
	}
	// Deep-space: reaches Mars-class where the relay tug cannot.
	if !connectedAt(t, w, "Deep-Space-Relay", marsish) {
		t.Error("the deep-space antenna must reach Mars-class distance")
	}
	if connectedAt(t, w, "Relay-Tug", marsish) {
		t.Error("a relay tug must NOT reach Mars-class distance (needs a deep-space antenna)")
	}
	// The limit is still real — even deep-space drops out eventually.
	if connectedAt(t, w, "Deep-Space-Relay", beyond) {
		t.Error("even the deep-space antenna must read NO SIGNAL past its reach")
	}
}

// TestCommRelayChainBridgesCislunar (#182): two Relay-Tugs link to each other
// across cislunar distances, so a tug beyond direct DSN reach still reaches the
// network through a forwarding tug — and is NO SIGNAL without it.
func TestCommRelayChainBridgesCislunar(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	a := spacecraft.NewFromLoadout("Relay-Tug")
	radialAboveStation0(t, w, a, 2_200_000_000) // 2.2M km — within DSN reach (~2.24M km)
	b := spacecraft.NewFromLoadout("Relay-Tug")
	radialAboveStation0(t, w, b, 3_000_000_000) // 3.0M km — beyond direct DSN reach
	w.Crafts = []*spacecraft.Spacecraft{a, b}
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(1)
	w.RecomputeCommGraph()
	if !w.CommGraph.HasConnection(b.ID) {
		t.Error("tug B should reach the network through tug A (cislunar relay chain)")
	}
	// Drop the forwarding tug → B is beyond direct DSN reach → NO SIGNAL.
	w.Crafts = []*spacecraft.Spacecraft{b}
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.RecomputeCommGraph()
	if w.CommGraph.HasConnection(b.ID) {
		t.Error("without the relay, tug B (beyond direct DSN reach) must read NO SIGNAL")
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

// TestKernDSNConnectsProbe (v0.24): a basic-antenna probe in low Kern orbit
// (the Lumen home world) reaches the Kern DSN ring. Before the Kern ring
// existed, ground stations were Earth-only and the graph filtered
// out-of-system stations away, so a probe launched in Lumen could never
// connect regardless of its antenna. This proves the "earth, kern" goal.
func TestKernDSNConnectsProbe(t *testing.T) {
	w := mustWorld(t)
	// Browse to the Lumen system so Kern and its DSN ring are active.
	lumen := -1
	for i := range w.Systems {
		if strings.EqualFold(w.Systems[i].Name, "Lumen") {
			lumen = i
			break
		}
	}
	if lumen < 0 {
		t.Fatal("Lumen system not loaded")
	}
	w.SystemIdx = lumen
	w.Calculator = orbital.ForSystem(w.System())

	// Find a Kern ground station; place a basic-antenna probe just above it
	// (clear radial LOS, so connectivity is gated by range alone).
	var gs GroundStationPreset
	found := false
	for _, s := range w.GroundStations {
		if s.BodyID == "kern" {
			gs, found = s, true
			break
		}
	}
	if !found {
		t.Fatal("no Kern ground station in the catalog")
	}
	sys := w.System()
	kern := sys.FindBody("kern")
	if kern == nil {
		t.Fatal("Kern not in the Lumen system")
	}
	up := w.groundStationWorldPos(gs, *kern).Sub(w.BodyPosition(*kern)).Unit()

	probe := spacecraft.NewFromLoadout("Science-Probe") // direct-basic antenna, probe core
	probe.SystemIdx = lumen
	probe.Primary = *kern
	probe.State.R = up.Scale(kern.RadiusMeters() + 200_000) // 200 km up
	w.Crafts = []*spacecraft.Spacecraft{probe}
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.RecomputeCommGraph()

	if !w.CommGraph.HasConnection(probe.ID) {
		t.Error("a basic-antenna probe in low Kern orbit should reach the Kern DSN ring")
	}
}

// TestNearHomeBypassesOcclusion (v0.24.1): the home-telemetry blanket links a
// near-home probe to a ground station on its own primary even when the body
// occludes the line of sight — but only to stations on that same body.
func TestNearHomeBypassesOcclusion(t *testing.T) {
	// A body at the origin sits squarely between probe and station, so an
	// ordinary link is occluded (but in range).
	occ := []physics.OccluderBody{{Center: orbital.Vec3{}, Radius: 6_371_000}}
	probe := commNode{pos: orbital.Vec3{X: 7_000_000}, rangeM: 1e7, probe: true, bodyID: "earth"}
	station := commNode{pos: orbital.Vec3{X: -6_371_000}, rangeM: 5e9, station: true, forwards: true, bodyID: "earth"}

	if commLinked(probe, station, occ) {
		t.Fatal("an occluded probe/station pair must not link without the home blanket")
	}
	probe.nearHome = true
	if !commLinked(probe, station, occ) {
		t.Error("a near-home probe must link to a station on its primary despite occlusion")
	}
	station.bodyID = "mars"
	if commLinked(probe, station, occ) {
		t.Error("the home blanket must not link to a station on a different body")
	}
}

// TestNearHomeConnectsLowEarthOrbit (v0.24.1): regression for the playtest
// report "spawn at 500 km around Earth → NO SIGNAL". The 3 DSN stations sit at
// ~35-40° latitude and a 500 km orbit's horizon cone is only ~22°, so Earth
// occludes every station from an equatorial spawn — the home blanket must
// cover it instead.
func TestNearHomeConnectsLowEarthOrbit(t *testing.T) {
	w := mustWorld(t)
	w.Crafts = nil
	w.ActiveCraftIdx = -1
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    "Science-Probe",
		ParentBodyID: "earth",
		AltitudeM:    500_000, // deep in the DSN coverage gap
		Inclination:  0,       // equatorial: never reaches the mid-latitude ring
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	w.RecomputeCommGraph()
	if !w.CommGraph.HasConnection(c.ID) {
		t.Error("a probe in a 500 km equatorial Earth orbit should be connected via the home-telemetry blanket")
	}
}

// TestNearHomeOnlyAroundStationBodies (v0.24.1): the blanket is gated to bodies
// that actually host stations. A low lunar orbit (no stations on the Moon,
// Earth's stations far out of range) stays NO SIGNAL.
func TestNearHomeOnlyAroundStationBodies(t *testing.T) {
	w := mustWorld(t)
	w.Crafts = nil
	w.ActiveCraftIdx = -1
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    "Science-Probe",
		ParentBodyID: "moon",
		AltitudeM:    100_000,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	w.RecomputeCommGraph()
	if w.CommGraph.HasConnection(c.ID) {
		t.Error("a probe in low lunar orbit (no Moon stations, Earth out of range) must read NO SIGNAL")
	}
}
