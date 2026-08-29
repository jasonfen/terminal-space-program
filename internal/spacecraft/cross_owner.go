package spacecraft

// StripCrossOwnerTargetRefs returns a shallow copy of c with every
// target-relative reference — Target (when Kind is TargetCraft or
// TargetGhost), any planted node's TargetCraftID/TargetGhostOwner, and the
// active burn's TargetCraftID/TargetGhostOwner — cleared. c itself is left
// untouched; only the copy's Target/Nodes/ActiveBurn are replaced.
//
// This is the ONE place that decides which refs are "target-relative" for
// a craft crossing into a DIFFERENT player's World. save.CraftToWireForTransfer
// (the dock ledger's persistence path, #294 review finding 3 / finding G)
// calls this before wire-converting a parked payload; the live (no-restart)
// cross-player dock-ledger delivery (relay.DockLedger, the finding that
// followed) calls it directly on the in-memory craft right before handing it
// to a different owner's World. Both a ghost ref (owner fingerprint + remote
// craft ID) and a bare local ref are unsafe to carry across: a ghost ref can
// never resolve in the recipient's World and can even alias the recipient's
// own fingerprint, and craft IDs are dense per-World small ints, so a local
// ref can silently resolve against an unrelated craft that happens to hold
// the same numeric ID on the other side.
//
// A craft delivered back into the SAME owner's World (a live undock/release
// handback, or an abort that returns a guest's own craft) must NOT go
// through this — every ref on it stayed valid the whole time, and stripping
// it there would silently break a standing lock the player never lost.
func StripCrossOwnerTargetRefs(c *Spacecraft) *Spacecraft {
	if c == nil {
		return nil
	}
	cp := *c
	if cp.Target.Kind == TargetCraft || cp.Target.Kind == TargetGhost {
		cp.Target = Target{}
	}
	if len(cp.Nodes) > 0 {
		nodes := make([]ManeuverNode, len(cp.Nodes))
		copy(nodes, cp.Nodes)
		for i := range nodes {
			if nodes[i].TargetCraftID != 0 {
				nodes[i].TargetGhostOwner = ""
				nodes[i].TargetCraftID = 0
			}
		}
		cp.Nodes = nodes
	}
	if cp.ActiveBurn != nil && cp.ActiveBurn.TargetCraftID != 0 {
		ab := *cp.ActiveBurn
		ab.TargetGhostOwner = ""
		ab.TargetCraftID = 0
		cp.ActiveBurn = &ab
	}
	return &cp
}
