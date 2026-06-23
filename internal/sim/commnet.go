package sim

import (
	"math"

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
// ground station.
type CommGraph struct {
	Connected map[uint64]bool
}

// HasConnection reports whether the craft with the given ID has a network
// connection this tick. nil-safe (a not-yet-computed graph → false).
func (g *CommGraph) HasConnection(id uint64) bool {
	return g != nil && g.Connected[id]
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

// connectivity builds the adjacency graph over nodes (a link needs both
// LOS — unoccluded by any body — and range) and returns, for each probe
// node, whether a relay chain reaches a station. Forwarding is allowed
// only through forwarder nodes (relays / stations); a direct-only craft is
// a dead end you cannot relay through. Pure (no world state) so it is
// unit-tested with synthetic nodes.
func connectivity(nodes []commNode, occ []physics.OccluderBody) map[uint64]bool {
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
	out := map[uint64]bool{}
	for i := 0; i < n; i++ {
		if nodes[i].probe && bfsReachesStation(i, nodes, adj) {
			out[nodes[i].craftID] = true
		}
	}
	return out
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

// bfsReachesStation returns whether a relay chain from the start node
// reaches any ground station. The start expands unconditionally (it is
// transmitting); an intermediate node expands only if it can forward.
func bfsReachesStation(start int, nodes []commNode, adj [][]int) bool {
	visited := make([]bool, len(nodes))
	visited[start] = true
	queue := []int{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur != start && !nodes[cur].forwards {
			continue // a non-forwarder (direct-only craft) cannot relay through
		}
		for _, nb := range adj[cur] {
			if nodes[nb].station {
				return true
			}
			if !visited[nb] {
				visited[nb] = true
				queue = append(queue, nb)
			}
		}
	}
	return false
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

	w.CommGraph = &CommGraph{Connected: connectivity(nodes, occ)}
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
