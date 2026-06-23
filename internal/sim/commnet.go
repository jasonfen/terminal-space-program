package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// CommNet connectivity graph (v0.23 / ADR 0027, cycle 2 C2-4). Each tick
// the world builds a graph over {comms-capable crafts, ground stations} in
// the active craft's system and computes, for every unmanned probe,
// whether an unoccluded + in-range relay chain reaches any ground station.
// The result gates command of unmanned vessels (CanCommandCraft) and feeds
// coverage objectives + the comms HUD (later slices). Transient — never
// persisted; rebuilt each tick.

// CommRangePerWatt is the placeholder link-range scale: the max link
// distance (m) is this constant times the WEAKER endpoint's antenna power
// (W). The two-endpoint range-combination formula is deferred tuning
// (ADR 0027 §2 floats ∝ √(Pₐ·P_b)); this conservative min-power form ships
// until playtest decides the real curve.
const CommRangePerWatt = 5000.0

// commLinkRangeM is the max distance at which two antennas can link, from
// the weaker of the two powers (placeholder — see CommRangePerWatt).
func commLinkRangeM(pa, pb float64) float64 {
	return math.Min(pa, pb) * CommRangePerWatt
}

// CommGraph is the cached per-tick connectivity result: the set of
// unmanned craft (by stable ID) that currently have a connection to a
// ground station, plus — for each connected probe — the world-frame relay
// chain it reaches the network through (probe, relays…, station), which the
// comms HUD draws and counts hops from (C2-7).
type CommGraph struct {
	Connected map[uint64]bool
	// Paths maps a connected probe's ID to its shortest relay chain as
	// ordered world-frame points: the probe first, then each relay hop, then
	// the terminal ground station. Absent for disconnected probes. The
	// positions are the same absolute world frame the orbit canvas projects
	// (body position + state), so the HUD draws segments without rebasing.
	Paths map[uint64][]orbital.Vec3
}

// HasConnection reports whether the craft with the given ID has a network
// connection this tick. nil-safe (a not-yet-computed graph → false).
func (g *CommGraph) HasConnection(id uint64) bool {
	return g != nil && g.Connected[id]
}

// Path returns the world-frame relay chain (probe→relays…→station) for the
// craft with the given ID, or nil if it has no recorded connection this
// tick. nil-safe.
func (g *CommGraph) Path(id uint64) []orbital.Vec3 {
	if g == nil {
		return nil
	}
	return g.Paths[id]
}

// commNode is one node in the connectivity graph — a craft antenna or a
// ground station, with the world-frame position the LOS + range tests use.
type commNode struct {
	pos      orbital.Vec3
	powerW   float64
	forwards bool   // can relay traffic onward: a relay antenna + Controllable, or a ground station
	station  bool   // a ground station (a connection sink)
	probe    bool   // an unmanned controllable craft — a BFS source that needs a connection
	craftID  uint64 // 0 for stations
}

// connectivityResult is the full output of the connectivity solve: which
// probes are connected, and the shortest relay path (as node indices,
// probe→…→station) for each connected probe. RecomputeCommGraph maps the
// node-index paths to world-frame positions; the bool map alone is enough
// for the command gate (CanCommandCraft).
type connectivityResult struct {
	connected map[uint64]bool
	paths     map[uint64][]int // probe id → node-index chain probe…station
}

// connectivity builds the adjacency graph over nodes (a link needs both
// LOS — unoccluded by any body — and range) and returns, for each probe
// node, whether a relay chain reaches a station. Forwarding is allowed
// only through forwarder nodes (relays / stations); a direct-only craft is
// a dead end you cannot relay through. Thin bool-map wrapper over
// connectivityFull for the command-gate callers and their tests.
func connectivity(nodes []commNode, occ []physics.OccluderBody) map[uint64]bool {
	return connectivityFull(nodes, occ).connected
}

// connectivityFull is the connectivity solve: it builds the adjacency graph
// then, for each probe, runs a hop-shortest BFS to a station, recording both
// the connected flag and the node-index path. Pure (no world state) so it is
// unit-tested with synthetic nodes.
func connectivityFull(nodes []commNode, occ []physics.OccluderBody) connectivityResult {
	n := len(nodes)
	adj := make([][]int, n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if commLinked(nodes[i], nodes[j], occ) {
				adj[i] = append(adj[i], j)
				adj[j] = append(adj[j], i)
			}
		}
	}
	res := connectivityResult{connected: map[uint64]bool{}, paths: map[uint64][]int{}}
	for i := 0; i < n; i++ {
		if !nodes[i].probe {
			continue
		}
		if path := bfsPathToStation(i, nodes, adj); path != nil {
			res.connected[nodes[i].craftID] = true
			res.paths[nodes[i].craftID] = path
		}
	}
	return res
}

func commLinked(a, b commNode, occ []physics.OccluderBody) bool {
	if a.powerW <= 0 || b.powerW <= 0 {
		return false
	}
	if a.pos.Sub(b.pos).Norm() > commLinkRangeM(a.powerW, b.powerW) {
		return false
	}
	return !physics.SegmentOccludedByBody(a.pos, b.pos, occ)
}

// bfsPathToStation returns the hop-shortest relay chain from the start node
// to any ground station, as node indices [start, …, station], or nil if no
// chain reaches a station. The start expands unconditionally (it is
// transmitting); an intermediate node expands only if it can forward. BFS
// order makes the first station reached the fewest-hops one; the predecessor
// map reconstructs the chain.
func bfsPathToStation(start int, nodes []commNode, adj [][]int) []int {
	visited := make([]bool, len(nodes))
	pred := make([]int, len(nodes))
	for i := range pred {
		pred[i] = -1
	}
	visited[start] = true
	queue := []int{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur != start && !nodes[cur].forwards {
			continue // a non-forwarder (direct-only craft) cannot relay through
		}
		for _, nb := range adj[cur] {
			if visited[nb] {
				continue
			}
			visited[nb] = true
			pred[nb] = cur
			if nodes[nb].station {
				return reconstructPath(pred, start, nb)
			}
			queue = append(queue, nb)
		}
	}
	return nil
}

// reconstructPath walks the predecessor map from end back to start and
// returns the forward chain [start, …, end].
func reconstructPath(pred []int, start, end int) []int {
	var rev []int
	for at := end; at != -1; at = pred[at] {
		rev = append(rev, at)
		if at == start {
			break
		}
	}
	// rev is end→start; flip to start→end.
	path := make([]int, len(rev))
	for i, v := range rev {
		path[len(rev)-1-i] = v
	}
	return path
}

// RecomputeCommGraph rebuilds w.CommGraph for the active craft's system:
// gathers ground-station + craft nodes with their current world positions
// and the body occluders, then runs connectivity. Called each Tick after
// physics; also lazily by CanCommandCraft if the cache is nil.
func (w *World) RecomputeCommGraph() {
	sys := w.System()
	sysIdx := w.SystemIdx

	occ := make([]physics.OccluderBody, 0, len(sys.Bodies))
	for i := range sys.Bodies {
		b := sys.Bodies[i]
		occ = append(occ, physics.OccluderBody{Center: w.BodyPosition(b), Radius: b.RadiusMeters()})
	}

	var nodes []commNode
	for _, st := range w.GroundStations {
		body := sys.FindBody(st.BodyID)
		if body == nil {
			continue // station's body is not in this system
		}
		nodes = append(nodes, commNode{
			pos:      w.groundStationWorldPos(st, *body),
			powerW:   st.AntennaPowerW,
			forwards: true,
			station:  true,
		})
	}
	for _, c := range w.Crafts {
		if c == nil || c.SystemIdx != sysIdx || c.AntennaKind == spacecraft.AntennaNone {
			continue
		}
		nodes = append(nodes, commNode{
			pos:      w.BodyPosition(c.Primary).Add(c.State.R),
			powerW:   c.AntennaPowerW,
			forwards: c.AntennaKind == spacecraft.AntennaRelay && c.Controllable,
			probe:    c.Controllable && !c.Crewed,
			craftID:  c.ID,
		})
	}

	res := connectivityFull(nodes, occ)
	paths := make(map[uint64][]orbital.Vec3, len(res.paths))
	for id, idxPath := range res.paths {
		pts := make([]orbital.Vec3, len(idxPath))
		for i, ni := range idxPath {
			pts[i] = nodes[ni].pos
		}
		paths[id] = pts
	}
	w.CommGraph = &CommGraph{Connected: res.connected, Paths: paths}
}

// ActiveCommPath returns the active craft's relay chain as ordered
// world-frame points (probe, relays…, station), the hop count (number of
// links = len(points)-1), and whether it is connected. Recomputes the graph
// lazily if the cache is nil. connected is false for a crewed/non-probe
// active craft (which has no BFS path) or a disconnected probe — the comms
// HUD uses that to choose between DIRECT/CONNECTED and NO SIGNAL, and to
// decide whether to draw the chain. (C2-7)
func (w *World) ActiveCommPath() (points []orbital.Vec3, hops int, connected bool) {
	c := w.ActiveCraft()
	if c == nil {
		return nil, 0, false
	}
	if w.CommGraph == nil {
		w.RecomputeCommGraph()
	}
	pts := w.CommGraph.Path(c.ID)
	if len(pts) < 2 {
		return nil, 0, false
	}
	return pts, len(pts) - 1, true
}

// groundStationWorldPos is a station's current world-frame position: its
// body's position plus the body-fixed surface point (co-rotating with the
// body) at sim time.
func (w *World) groundStationWorldPos(st GroundStationPreset, body bodies.CelestialBody) orbital.Vec3 {
	r := body.RadiusMeters()
	dir := render.BodyFixedToWorld(body, st.LatDeg, st.LonEastDeg, w.Clock.SimTime)
	return w.BodyPosition(body).Add(orbital.Vec3{X: r * dir.X, Y: r * dir.Y, Z: r * dir.Z})
}

// CanCommandCraft reports whether the player may issue NEW commands to a
// craft (ADR 0027 §4 — command, not flight). A crewed vessel is never
// gated; an unmanned probe needs a network connection; passive debris (no
// command source) is never commandable. The onboard flight plan still
// executes regardless — only committing new commands is gated by the
// caller.
func (w *World) CanCommandCraft(c *spacecraft.Spacecraft) bool {
	if c == nil || !c.Controllable {
		return false
	}
	if c.Crewed {
		return true
	}
	if w.CommGraph == nil {
		w.RecomputeCommGraph()
	}
	return w.CommGraph.HasConnection(c.ID)
}

// noSignalFlashWindow is how long the "NO SIGNAL" transient stays up after
// a command is blocked (wall-clock, so it expires even while paused).
const noSignalFlashWindow = 2 * time.Second

// canCommand gates a player command on craft c (which must be non-nil).
// Returns true if the command may proceed; otherwise flags the NO SIGNAL
// transient and returns false. Used by the command methods (throttle,
// attitude, node plant/delete, staging, nav mode) to block new commands to
// an out-of-contact unmanned probe while letting its onboard plan run.
func (w *World) canCommand(c *spacecraft.Spacecraft) bool {
	if w.CanCommandCraft(c) {
		return true
	}
	w.commBlockedUntil = time.Now().Add(noSignalFlashWindow)
	return false
}

// CommBlockedFlash returns ("NO SIGNAL", true) while a just-blocked command
// is within its flash window, else ("", false). The HUD reads this each
// frame (the comms chip / status flash, C2-7).
func (w *World) CommBlockedFlash() (string, bool) {
	if time.Now().Before(w.commBlockedUntil) {
		return "NO SIGNAL", true
	}
	return "", false
}
