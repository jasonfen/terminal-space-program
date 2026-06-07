package sim

import "github.com/jasonfen/terminal-space-program/internal/spacecraft"

// Node identity (v0.16 / ADR 0016). Planted maneuver nodes carry a
// stable ManeuverNode.ID so a feature that must follow one specific
// node — Auto-Warp's frozen target — can resolve it across the
// sortNodes reorder that runs on every plant. Neither a slice index nor
// a pointer survives that re-sort; the ID does. This mirrors the
// craft-identity pattern (ADR 0012, craft_identity.go) one level down,
// scoped to a craft's Nodes slice.

// stampNodeID assigns n the next stable ID when it is planted without
// one. No-op for a node that already has an ID (e.g. an arrival node
// rebuilt in place that carried its identity forward) or a nil node.
func (w *World) stampNodeID(n *spacecraft.ManeuverNode) {
	if n == nil || n.ID != 0 {
		return
	}
	if w.NextNodeID == 0 {
		w.NextNodeID = 1
	}
	n.ID = w.NextNodeID
	w.NextNodeID++
}

// EnsureNodeIDs advances NextNodeID past any node ID already in the
// slate and stamps every planted node still missing one. Idempotent;
// called from NewWorld and the save loader so every node carries a
// unique stable ID and the counter never hands out a colliding value
// (the node-level analogue of EnsureCraftIDs).
func (w *World) EnsureNodeIDs() {
	var maxID uint64
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		for i := range c.Nodes {
			if c.Nodes[i].ID > maxID {
				maxID = c.Nodes[i].ID
			}
		}
	}
	if w.NextNodeID <= maxID {
		w.NextNodeID = maxID + 1
	}
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		for i := range c.Nodes {
			w.stampNodeID(&c.Nodes[i])
		}
	}
}

// nodeByID returns the planted node with stable ID nodeID on the craft
// with stable ID craftID, and ok=false when either no longer resolves —
// the craft was removed, the node was deleted or re-planted, or an id is
// zero. This is the single resolution chokepoint for Auto-Warp's frozen
// target (ADR 0016): a target that stops resolving here is the cue to
// disengage. The returned pointer is into the craft's live Nodes slice,
// so it is invalidated by the next slice mutation — resolve, read, drop.
func (w *World) nodeByID(craftID, nodeID uint64) (*spacecraft.ManeuverNode, bool) {
	if craftID == 0 || nodeID == 0 {
		return nil, false
	}
	c, _, ok := w.craftByID(craftID)
	if !ok {
		return nil, false
	}
	for i := range c.Nodes {
		if c.Nodes[i].ID == nodeID {
			return &c.Nodes[i], true
		}
	}
	return nil, false
}
