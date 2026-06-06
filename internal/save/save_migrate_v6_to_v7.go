// v0.14.x: schema v6 → v7 migration (ADR 0012, GH #87). Pre-v7 saves
// bind a target to a craft by its position in the slate: Target.CraftIdx
// is a 0-based index, ManeuverNode/ActiveBurn.TargetCraftIdx is a
// one-based index (zero = "no target"). v7 vessels carry a stable
// Spacecraft.ID and targets reference that ID instead, so a slate
// mutation can no longer re-point a stored target at the wrong vessel.
//
// The conversion is purely positional: assign each craft the ID
// (slate index + 1), then rewrite every stored index to the matching
// ID. With IDs handed out as index+1:
//
//   - a 0-based slate index i  → ID i+1   (Target.CraftIdx)
//   - a one-based slate index j → ID j     (TargetCraftIdx; slot j-1 → ID (j-1)+1 = j)
//
// so a one-based TargetCraftIdx copies straight across to TargetCraftID.

package save

// migrateV6PayloadToV7 rewrites a pre-v7 wire payload in place: stamps
// sequential craft IDs and converts every index-based target binding
// (per-craft Target + Nodes + ActiveBurn, and the pre-polish/pre-v5
// payload-level Target + Nodes + ActiveBurn) to ID-based.
func migrateV6PayloadToV7(p *Payload) {
	n := len(p.Crafts)

	// Stamp sequential IDs by slate position and prime the counter.
	for i := range p.Crafts {
		p.Crafts[i].ID = uint64(i + 1)
	}
	p.NextCraftID = uint64(n + 1)

	// Per-craft bindings.
	for i := range p.Crafts {
		c := &p.Crafts[i]
		if c.Target != nil {
			c.Target.CraftID = craftIDForIndex0(c.Target.CraftIdx, n)
		}
		for j := range c.Nodes {
			c.Nodes[j].TargetCraftID = craftIDForIndex1(c.Nodes[j].TargetCraftIdx, n)
		}
		if c.ActiveBurn != nil {
			c.ActiveBurn.TargetCraftID = craftIDForIndex1(c.ActiveBurn.TargetCraftIdx, n)
		}
	}

	// Pre-polish (single payload-level Target) + pre-v5 (payload-level
	// Nodes / ActiveBurn) bindings. These coexist with a single craft in
	// practice, but convert defensively the same way.
	if p.Target != nil {
		p.Target.CraftID = craftIDForIndex0(p.Target.CraftIdx, n)
	}
	for j := range p.Nodes {
		p.Nodes[j].TargetCraftID = craftIDForIndex1(p.Nodes[j].TargetCraftIdx, n)
	}
	if p.ActiveBurn != nil {
		p.ActiveBurn.TargetCraftID = craftIDForIndex1(p.ActiveBurn.TargetCraftIdx, n)
	}
}

// craftIDForIndex0 maps a 0-based slate index to its v7 craft ID
// (index+1), returning 0 ("no target") for an out-of-range index — the
// same silent-drop the pre-v7 bounds checks produced.
func craftIDForIndex0(idx0, n int) uint64 {
	if idx0 < 0 || idx0 >= n {
		return 0
	}
	return uint64(idx0 + 1)
}

// craftIDForIndex1 maps a one-based slate index (the TargetCraftIdx
// encoding, zero = no target) to its v7 craft ID. Slate slot j-1 was
// stamped ID (j-1)+1 = j, so a valid one-based index copies straight to
// the ID; zero and out-of-range map to 0 (no target).
func craftIDForIndex1(idx1, n int) uint64 {
	if idx1 <= 0 || idx1 > n {
		return 0
	}
	return uint64(idx1)
}
