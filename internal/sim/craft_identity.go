package sim

import "github.com/jasonfen/terminal-space-program/internal/spacecraft"

// Craft identity (v0.14.x / ADR 0012). Vessels carry a stable
// Spacecraft.ID; every target — the world cursor, per-craft bindings,
// planted-node and in-flight-burn target slots — references a craft by
// ID, not by its position in World.Crafts. A slate mutation that shifts
// indices (end-flight, dock, undock, stage) therefore can no longer
// re-point a stored target at the wrong vessel (GH #87); a removed
// craft's ID simply stops resolving and the target degrades to "none".

// stampCraftID assigns c the next stable ID when it enters the slate
// without one. No-op for a craft that already has an ID (e.g. a docking
// composite that inherits the lead's identity) or a nil entry.
func (w *World) stampCraftID(c *spacecraft.Spacecraft) {
	if c == nil || c.ID != 0 {
		return
	}
	if w.NextCraftID == 0 {
		w.NextCraftID = 1
	}
	c.ID = w.NextCraftID
	w.NextCraftID++
}

// EnsureCraftIDs advances NextCraftID past any ID already in the slate
// and stamps every craft still missing one. Idempotent; called from
// NewWorld and the save loader so every live craft carries a unique
// stable ID and the counter never hands out a colliding value.
func (w *World) EnsureCraftIDs() {
	var maxID uint64
	for _, c := range w.Crafts {
		if c != nil && c.ID > maxID {
			maxID = c.ID
		}
	}
	if w.NextCraftID <= maxID {
		w.NextCraftID = maxID + 1
	}
	for _, c := range w.Crafts {
		w.stampCraftID(c)
	}
}

// craftByID returns the live craft with the given stable ID, its slate
// index, and ok=false when no craft matches (id==0, or the craft was
// removed from the slate). This is the single resolution chokepoint for
// every identity-based target read (ADR 0012).
func (w *World) craftByID(id uint64) (*spacecraft.Spacecraft, int, bool) {
	if id == 0 {
		return nil, -1, false
	}
	for i, c := range w.Crafts {
		if c != nil && c.ID == id {
			return c, i, true
		}
	}
	return nil, -1, false
}

// targetCraft resolves the world cursor's bound craft (Kind==TargetCraft)
// to a live craft and its slate index. ok=false for a non-craft target
// or a stale/removed ID.
func (w *World) targetCraft() (*spacecraft.Spacecraft, int, bool) {
	if w.Target.Kind != spacecraft.TargetCraft {
		return nil, -1, false
	}
	return w.craftByID(w.Target.CraftID)
}

// CraftByID is the exported resolver for readers outside the sim package
// (the tui screens) that hold a stable craft ID — e.g. a node's
// TargetCraftID — and need the live craft + its current slate index.
// ok=false for id==0 or a craft no longer in the slate (ADR 0012).
func (w *World) CraftByID(id uint64) (*spacecraft.Spacecraft, int, bool) {
	return w.craftByID(id)
}

// ResolveTargetCraft is the exported form of targetCraft for the tui —
// resolves the world target cursor to its live craft + slate index, or
// ok=false for a non-craft / stale target (ADR 0012).
func (w *World) ResolveTargetCraft() (*spacecraft.Spacecraft, int, bool) {
	return w.targetCraft()
}
