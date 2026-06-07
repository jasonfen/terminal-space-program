package sim

import "time"

// Auto-Warp (v0.16 / ADR 0016). One control warps time to a fixed lead
// before the next burn, then hands off to 1× so the player can watch it
// arm and fire. The driver never mutates Selected Warp (Clock.WarpIdx):
// while engaged it max-seeds clampedWarp's "selected" baseline and adds
// one approach term anchored at T (= BurnStart − lead), so every existing
// warp clamp still picks the actual rate — Auto-Warp only automates
// *picking and releasing* warp, never inventing a step that aliases past
// a burn. The target is frozen by node identity (ADR 0016 Slice 1) so it
// follows the burn the player engaged for across edits.

// autoWarpLeadTime is the sim-time gap between where Auto-Warp drops to
// 1× and the target burn's BurnStart. Fixed in v1 (not configurable):
// 30 s of 1× coast is ample warning to watch the burn arm.
const autoWarpLeadTime = 30 * time.Second

// AutoWarpTarget is the engaged driver's frozen aim. CraftID+NodeID is
// the stable identity of the burn being chased (ADR 0016 Slice 1); T is
// the sim-time the driver seeks before releasing to 1×. Transient — not
// persisted, so a save/load mid-warp lands disengaged.
type AutoWarpTarget struct {
	CraftID uint64
	NodeID  uint64
	T       time.Time
}

// autoWarpEngaged reports whether the driver is active.
func (w *World) autoWarpEngaged() bool { return w.AutoWarp != nil }

// AutoWarpEngaged is the exported form for the tui (HUD chip + button state).
func (w *World) AutoWarpEngaged() bool { return w.autoWarpEngaged() }

// AutoWarpEligible reports whether engaging right now would find a burn
// to chase — drives the dimmed/active state of the title-bar button.
func (w *World) AutoWarpEligible() bool {
	_, _, _, ok := w.soonestEligibleBurn()
	return ok
}

// AutoWarpSecondsToTarget returns the sim-seconds until the engaged
// driver's release point T, and ok=false when not engaged — feeds the
// `AUTO → Nx ⏱ Ms` HUD chip.
func (w *World) AutoWarpSecondsToTarget() (float64, bool) {
	if !w.autoWarpEngaged() {
		return 0, false
	}
	dt := w.AutoWarp.T.Sub(w.Clock.SimTime).Seconds()
	if dt < 0 {
		dt = 0
	}
	return dt, true
}

// EngageAutoWarp aims the driver at the globally-soonest eligible burn
// across all vessels and returns true on success. Eligible ⇔ BurnStart
// is more than autoWarpLeadTime in the future; otherwise the press is a
// no-op returning false (the button is dimmed). Engaging while paused
// auto-unpauses so time actually advances.
func (w *World) EngageAutoWarp() bool {
	craftID, nodeID, burnStart, ok := w.soonestEligibleBurn()
	if !ok {
		return false
	}
	w.AutoWarp = &AutoWarpTarget{
		CraftID: craftID,
		NodeID:  nodeID,
		T:       burnStart.Add(-autoWarpLeadTime),
	}
	w.Clock.Paused = false // engaging while paused auto-unpauses
	return true
}

// ToggleAutoWarp engages the driver, or disengages it if already on
// (a manual cancel — Selected Warp is left untouched). Returns the
// engaged state after the toggle.
func (w *World) ToggleAutoWarp() bool {
	if w.autoWarpEngaged() {
		w.DisengageAutoWarp()
		return false
	}
	return w.EngageAutoWarp()
}

// DisengageAutoWarp releases the driver without touching Selected Warp,
// so the player falls back to exactly the warp they had. This is the
// manual-cancel / node-invalidated path; the reached-target path in
// resolveAutoWarp additionally forces WarpIdx to 1×.
func (w *World) DisengageAutoWarp() { w.AutoWarp = nil }

// soonestEligibleBurn finds the earliest BurnStart across all vessels'
// resolved, identified nodes that is more than autoWarpLeadTime out, and
// returns its craft+node identity and BurnStart. ok=false when none
// qualifies. Walks every craft so Auto-Warp stops before whatever burn
// comes first, anyone's — the non-surprising behaviour (ADR 0016).
func (w *World) soonestEligibleBurn() (craftID, nodeID uint64, burnStart time.Time, ok bool) {
	threshold := w.Clock.SimTime.Add(autoWarpLeadTime)
	for _, c := range w.Crafts {
		if c == nil || c.ID == 0 {
			continue
		}
		for i := range c.Nodes {
			n := &c.Nodes[i]
			if !n.IsResolved() || n.ID == 0 {
				continue
			}
			bs := n.BurnStart()
			if !bs.After(threshold) {
				continue
			}
			if !ok || bs.Before(burnStart) {
				craftID, nodeID, burnStart, ok = c.ID, n.ID, bs, true
			}
		}
	}
	return
}

// resolveAutoWarp advances or releases the engaged target each tick,
// called before clampedWarp so the rate this tick reflects the result:
//   - target node gone or unresolved → disengage (Selected Warp kept);
//   - its BurnStart shifted → re-freeze T to track the edit;
//   - SimTime reached T → force Selected Warp to 1× and disengage.
//
// No-op when not engaged.
func (w *World) resolveAutoWarp() {
	if !w.autoWarpEngaged() {
		return
	}
	n, ok := w.nodeByID(w.AutoWarp.CraftID, w.AutoWarp.NodeID)
	if !ok || !n.IsResolved() {
		w.DisengageAutoWarp()
		return
	}
	if want := n.BurnStart().Add(-autoWarpLeadTime); !want.Equal(w.AutoWarp.T) {
		w.AutoWarp.T = want // re-freeze on a node edit
	}
	if !w.Clock.SimTime.Before(w.AutoWarp.T) {
		w.Clock.WarpIdx = 0 // hand off to 1× to watch the burn arm
		w.DisengageAutoWarp()
	}
}
