// Package sim — v0.11.4+ end-flight action (ADR 0004).
//
// End-flight is the cleanup step for a Crashed vessel: removes the
// wreckage from World.Crafts entirely. Triggered by `[E]` on the
// orbit screen after a y/n confirm prompt — the prompt is opened
// by the screen layer when the active vessel is Crashed; this
// helper just commits the removal.
//
// Auto-switch: if the active vessel was removed, ActiveCraftIdx
// snaps to the next vessel in the slate (the entry that took the
// removed slot's index, or the previous slot when removing the
// tail). When the slate becomes empty, ActiveCraftIdx is set to
// -1 — the v0.11.4 Slice 1 ViewLaunch render path already handles
// the "no active vessel" case (sub-scope 5).
//
// Scope: Crashed-only. Removing a live (non-Crashed) vessel is a
// separate sandbox / cleanup feature (deferred to v0.12+ — see
// ADR 0004 "End-flight scope" alternatives section).

package sim

// EndFlightActive removes the active vessel from the slate IFF it
// is Crashed. Returns true when the removal happened. A no-op
// (returns false) when there is no active vessel or the active
// vessel is not Crashed — the screen-side confirm prompt should
// gate on `c.Crashed`, but defence-in-depth here avoids accidental
// removal via direct API calls.
//
// Active reassignment: the slate is left in its natural order
// (no resort); ActiveCraftIdx snaps to the same index when there's
// a successor at that slot, or to (len-1) when the removed entry
// was the tail. Empty slate → ActiveCraftIdx = -1.
func (w *World) EndFlightActive() bool {
	idx := w.ActiveCraftIdx
	if idx < 0 || idx >= len(w.Crafts) {
		return false
	}
	c := w.Crafts[idx]
	if c == nil || !c.Crashed {
		return false
	}
	// Drop the entry and snap active to the next slot. Slice
	// append-trick: keep the prefix, skip idx, then append the
	// suffix.
	w.Crafts = append(w.Crafts[:idx], w.Crafts[idx+1:]...)
	// The active (outgoing) craft was just removed, so there is no craft
	// to checkpoint w.Target onto. Clear ActiveCraftIdx before
	// reassigning: otherwise SetActiveCraftIdx's outgoing-checkpoint
	// (still pointing at the old slot) would write the removed craft's
	// live target onto whatever successor now occupies that slot,
	// clobbering its own stored target binding (GH #87, defect 2).
	w.ActiveCraftIdx = -1
	switch {
	case len(w.Crafts) == 0:
		// Slate empty — no active vessel. Set sentinel so
		// ActiveCraft() returns nil cleanly.
		w.ActiveCraftIdx = -1
		w.Target = newEmptyTargetForCraft(nil)
		w.reconcileNavMode()
	case idx >= len(w.Crafts):
		// Removed the tail — snap to the new last entry.
		w.SetActiveCraftIdx(len(w.Crafts) - 1)
	default:
		// Successor took the slot — keep ActiveCraftIdx pointing
		// at the same index (now a different vessel).
		w.SetActiveCraftIdx(idx)
	}
	// End-flight cancels any in-flight burn that may have been
	// attributed to the removed vessel via the global engine state.
	w.StopManualBurn()
	return true
}

// newEmptyTargetForCraft returns a cleared Target. Helper kept
// local so the empty-slate branch above stays readable; the
// returned zero value reads identically to `spacecraft.Target{}`
// at the call site.
func newEmptyTargetForCraft(_ interface{}) Target { return Target{} }
