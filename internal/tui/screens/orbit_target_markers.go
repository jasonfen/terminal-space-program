package screens

import (
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// closestApproachHorizonSec is the forward-prediction window
// NextClosestApproach(Positions) searches for the encounter, matching
// closestApproachRows' (orbit_chip_builders.go) TARGET-chip CA/TCA rows
// exactly — the map's ✕ marker and the chip's numbers must never
// disagree about which encounter they're describing.
const closestApproachHorizonSec = 4 * 3600.0

// drawClosestApproachMarker plots the map's ✕ Closest-Approach marker
// (ADR 0020 / #346) at both ends of the predicted encounter: the active
// craft's own point on its orbit, and its twin at the bound target's
// position on ITS orbit, at the instant NextClosestApproachPositions
// reports for the current geometry. The two positions come from the same
// re-propagation that produces the TARGET chip's CA distance, so the
// marker separation always agrees with the chip's number — there is no
// separate, potentially-drifting position computation.
//
// No-op without a craft/ghost target sharing the active craft's primary
// (see World.TargetSharesActivePrimary) — cross-SOI rendezvous is
// already out of scope for the rendezvous tooling (CONTEXT.md's
// Rendezvous entry), same restriction NextClosestApproach itself
// documents.
//
// Not occlusion-culled against the primary disk, matching drawNodes' and
// drawSOIPass's marker convention (a maneuver marker or Perilune marker
// behind the body still shows) — overlap between markers resolves by
// deterministic draw order (ADR 0020 D), not a per-marker occlusion
// test.
func (v *OrbitView) drawClosestApproachMarker(w *sim.World) {
	c := w.ActiveCraft()
	if c == nil || !w.TargetSharesActivePrimary() {
		return
	}
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return
	}
	mu := c.Primary.GravitationalParameter()
	active := orbital.Vec3State{R: c.State.R, V: c.State.V}
	target := orbital.Vec3State{R: rT, V: vT}
	_, _, posA, posB, err := planner.NextClosestApproachPositions(active, target, mu, closestApproachHorizonSec)
	if err != nil {
		return
	}
	primaryPos := w.BodyPosition(c.Primary)
	drawMarker(v.canvas, primaryPos.Add(posA), render.MarkerClosestApproach, render.MarkerNominal, "", widgets.CellTag{})
	drawMarker(v.canvas, primaryPos.Add(posB), render.MarkerClosestApproach, render.MarkerNominal, "", widgets.CellTag{})
}

// drawTargetPlaneNodes plots the map's ◇ Ascending / ◆ Descending Node
// markers (ADR 0020 / #346) where the active craft's orbit crosses its
// bound target's orbital plane — reusing World.TargetPlaneNodePositions,
// which itself reuses the navball / PlanPlaneMatch node-crossing
// primitives (orbital.FrameFromNormal + orbital.TimeToNodeCrossing)
// against the target craft/ghost's plane instead of the primary's usual
// reference plane. No-op when neither crossing resolves (no target,
// cross-primary target, or the two orbits are already coplanar — see
// TargetPlaneNodePositions for the full list of refusal cases).
func (v *OrbitView) drawTargetPlaneNodes(w *sim.World) {
	anPos, dnPos, hasAN, hasDN := w.TargetPlaneNodePositions()
	if hasAN {
		drawMarker(v.canvas, anPos, render.MarkerAscendingNode, render.MarkerNominal, "", widgets.CellTag{})
	}
	if hasDN {
		drawMarker(v.canvas, dnPos, render.MarkerDescendingNode, render.MarkerNominal, "", widgets.CellTag{})
	}
}
