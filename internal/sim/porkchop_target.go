package sim

// PorkchopTarget resolves what `P` plots: the body the player has
// TARGETed (Target wins; the map body cursor is not consulted, #502).
// ok=false carries a one-phrase reason the key can't plot right now,
// phrased to follow a "porkchop:" label. The same guard feeds the
// planner's QUICK PLANS row so the dimmed reason never drifts from what
// the key does.
func (w *World) PorkchopTarget() (idx int, reason string, ok bool) {
	switch w.Target.Kind {
	case TargetNone:
		return 0, "no target, press t to aim at a planet", false
	case TargetBody:
	default:
		return 0, "target is not a planet, press t", false
	}
	idx = w.Target.BodyIdx
	sys := w.System()
	if idx <= 0 || idx >= len(sys.Bodies) {
		return 0, "no target, press t to aim at a planet", false
	}
	if c := w.ActiveCraft(); c != nil {
		b := sys.Bodies[idx]
		// The Lambert solve is heliocentric: a body you orbit, a moon
		// of it, or the planet your own moon orbits shares your system
		// and is an H trip, not a porkchop.
		if b.ID == c.Primary.ID || b.ParentID == c.Primary.ID || b.ID == c.Primary.ParentID {
			return 0, "same system as your orbit, use H", false
		}
	}
	return idx, "", true
}
