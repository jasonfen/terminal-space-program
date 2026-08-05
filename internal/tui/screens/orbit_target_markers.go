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
	ownPos, targetPos := primaryPos.Add(posA), primaryPos.Add(posB)
	// Inspect (ADR 0041 §3): the ✕ PAIR is one identity, not two — its
	// meaning is the relationship "closest approach with <target>", which
	// is why both ends share a ref and one chip. The pair anchors on the
	// craft's own end, so the chip lands where the player is looking from.
	caRef := InspectRef{Kind: InspectApproach}
	caTag := widgets.CellTag{Owner: caRef.OwnerKey()}
	if _, _, onCanvas := v.canvas.Project(ownPos); onCanvas {
		// Not targetable: the ✕ exists BECAUSE something is already
		// targeted, so committing it would be a no-op on the slot it
		// describes.
		v.addInspectable(caRef, inspectApproachName(w), ownPos, false)
	} else if _, _, onCanvas := v.canvas.Project(targetPos); onCanvas {
		// Only the target's end of the pair made the canvas. Not a cycle
		// stop (the pair anchors on the craft's end, which isn't visible),
		// but the ✕ that IS drawn carries the owner tag, so a click on it
		// has to resolve — anchored on the end the player can actually see.
		v.registerDrawnOwner(caRef, inspectApproachName(w), targetPos, false)
	}
	drawMarker(v.canvas, ownPos, render.MarkerClosestApproach, render.MarkerNominal, "", caTag)
	drawMarker(v.canvas, targetPos, render.MarkerClosestApproach, render.MarkerNominal, "", caTag)
	if v.isInspected(caRef) {
		// Flare BOTH ends: the pair is the entity, and lighting only one
		// ✕ would hide the very thing the marker exists to show — which
		// two points on which two orbits the encounter joins.
		glyph := render.MarkerGlyph(render.MarkerClosestApproach)
		v.canvas.SetCellOverlayColored(ownPos, glyph, render.ColorInspect)
		v.canvas.SetCellOverlayColored(targetPos, glyph, render.ColorInspect)
	}
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
