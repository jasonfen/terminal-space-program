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

// Advisory key tags (#293). PlanRendezvousNudge (K) and
// PlanCircularizeAtApoapsis (C) stamp their planted node's
// spacecraft.ManeuverNode.AdvisoryKey with one of these so a repeat
// press replaces its own previous unfired node instead of stacking a
// stale duplicate behind it — see replaceAdvisoryNode.
const (
	AdvisoryKeyRendezvousNudge = "rendezvous-nudge"
	AdvisoryKeyCircularize     = "circularize"
	// AdvisoryKeyMeetingBurn (ADR 0045 S5, #398) tags the single node a
	// Meeting Planner Lap Ladder row plants (PlanMeetingBurn) — same
	// "replace, don't stack" treatment as K's own advisory node.
	AdvisoryKeyMeetingBurn = "meeting-burn"
)

// replaceAdvisoryNode removes every node on c whose AdvisoryKey matches
// key before a fresh single-keystroke advisory plant (#293's
// "replace, don't stack" ruling). No-op when key is empty (guards
// against accidentally stripping ordinary, non-advisory nodes, which
// carry the zero-value "" tag) or when no matching node exists (the
// key's first press).
//
// Every node in c.Nodes is by definition unfired — executeDueNodesFor
// pops fired nodes out of the slice the same tick they fire — so "the
// craft's own previous UNFIRED node" reduces to exactly this: any
// currently-queued node carrying the same key. Once a node has fired
// (moved into c.ActiveBurn or applied instantly) it is already gone
// from c.Nodes, so a fresh advisory plant after that point starts
// clean without needing to distinguish fired from unfired itself.
//
// Scoped to c's own Nodes slice — replacing an advisory node never
// touches another craft's queue, and a different AdvisoryKey (e.g. C's
// node when K is pressed) is left untouched: only the same key's own
// node is replaced, per the ruling.
func (w *World) replaceAdvisoryNode(c *spacecraft.Spacecraft, key string) {
	if key == "" || c == nil {
		return
	}
	kept := c.Nodes[:0]
	for _, n := range c.Nodes {
		if n.AdvisoryKey == key {
			continue
		}
		kept = append(kept, n)
	}
	c.Nodes = kept
}

// plantedAdvisoryNode returns c's currently-queued node carrying the
// given AdvisoryKey AND whose own target binding (TargetCraftID +
// TargetGhostOwner) matches targetCraftID/targetGhostOwner, and
// ok=false when none is planted (key is empty, c is nil, or no queued
// node matches both the key and the binding). Companion to
// replaceAdvisoryNode — that one strips a stale advisory node before a
// fresh plant; this one lets a caller (RendezvousCommit, PR #392
// review Finding 1) find and honor one that's already sitting there.
// Same "every node in c.Nodes is by definition unfired" invariant
// applies (see replaceAdvisoryNode): once the node fires it's popped
// out of c.Nodes, so a hit here always means "planted but not yet
// flown".
//
// The binding check (PR #392 review, finding — engage-toward-stale-
// nudge) matters because PlanRendezvousNudge stamps TargetCraftID/
// TargetGhostOwner at plant time so a later target switch doesn't
// retarget the burn (rendezvous.go ~606-611): a node planted against
// peer A must not be picked up here when the caller is now engaging
// peer B, or the commit path would feed B's target state into a node
// whose Δv/direction was computed for A — a phantom co-warp toward a
// course that will never be flown. A binding mismatch is treated
// exactly like "nothing planted": the caller falls through to its
// current-course path (and its refusal, if the current course doesn't
// converge either).
func plantedAdvisoryNode(c *spacecraft.Spacecraft, key string, targetCraftID uint64, targetGhostOwner string) (spacecraft.ManeuverNode, bool) {
	if key == "" || c == nil {
		return spacecraft.ManeuverNode{}, false
	}
	for _, n := range c.Nodes {
		if n.AdvisoryKey == key && n.TargetCraftID == targetCraftID && n.TargetGhostOwner == targetGhostOwner {
			return n, true
		}
	}
	return spacecraft.ManeuverNode{}, false
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
