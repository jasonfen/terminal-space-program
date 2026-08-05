package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Inspect — point-and-ask identity on the orbit map (ADR 0041 §3 / issue
// #346, CONTEXT.md "Inspect").
//
// The map answers "what is this?" for exactly one entity at a time, only
// when asked. `[j]` steps a highlight through everything DRAWN this frame;
// a click on any tagged pixel — glyph, disk, node marker, or an orbit
// line, now that the class drawers carry an Owner tag — jumps the same
// highlight straight to what was clicked. The inspected entity flares in
// render.ColorInspect and gets a one-line name chip beside it. Enter
// commits it as Target, the same hover→commit contract the body Cursor
// has, generalised past bodies. Esc leaves.
//
// This is the answer ADR 0041 chose INSTEAD of standing ink: identity is
// never painted on the map by default (no proximity auto-labels, no
// per-player hue), so colour stays free to mean targeting and state while
// "whose line is whose" is still answerable in one keystroke.
//
// All of it is transient screen state: rebuilt from the frame, cleared at
// any Framing Event, never saved, never mutating the world. The one world
// mutation Inspect can cause — Enter → Target — is dispatched by the App
// through the existing sim target setters, not from here.

// InspectKind enumerates the identity-bearing things the orbit map draws.
// Ordering is not significant; cycle order comes from draw order (see
// addInspectable).
type InspectKind int

const (
	// InspectNone is the "nothing inspected" state — both the initial
	// state and the one the cycle wraps through, so a full lap around
	// the map always returns the player to a clean view.
	InspectNone InspectKind = iota
	InspectBody
	InspectVessel // a local craft in the slate, active or not
	InspectGhost  // another player's craft (ADR 0034 Kepler ghost)
	InspectNode   // a planted maneuver node
	InspectApproach
)

// InspectRef is the stable identity of one inspectable entity — stable
// across frames, so the highlight survives the map being redrawn while
// the entity moves. Comparable, so the render path can ask "is this the
// inspected one?" with ==.
//
// Each kind populates only its own fields; the zero value (Kind ==
// InspectNone) means nothing is inspected.
type InspectRef struct {
	Kind InspectKind
	// BodyID identifies an InspectBody.
	BodyID string
	// CraftID identifies an InspectVessel (slate craft ID, ADR 0012) or,
	// with Owner, an InspectGhost. Slate INDEXES are deliberately not
	// used: they shift under staging and undocking, and the highlight
	// would silently jump to a different vessel.
	CraftID uint64
	// Owner is the remote player handle for an InspectGhost.
	Owner string
	// NodeIdx identifies an InspectNode, 1-based — the same convention
	// CellTag.NodeIdx already uses.
	NodeIdx int
}

// OwnerKey renders a ref as the opaque string the canvas records on each
// pixel it draws for this entity (widgets.CellTag.Owner). Never parsed
// back: the mouse path looks the key up in the frame's inspectable list,
// so the encoding only has to be collision-free, not decodable. Exported
// because the key crosses a package boundary — it is the value that lands
// in the widgets-package tag and comes back out of HitAt.
func (r InspectRef) OwnerKey() string {
	switch r.Kind {
	case InspectBody:
		return "b:" + r.BodyID
	case InspectVessel:
		return fmt.Sprintf("v:%d", r.CraftID)
	case InspectGhost:
		return fmt.Sprintf("g:%s/%d", r.Owner, r.CraftID)
	case InspectNode:
		return fmt.Sprintf("n:%d", r.NodeIdx)
	case InspectApproach:
		return "ca"
	}
	return ""
}

// inspectable is one entry in the frame's inspectable set: the identity,
// the short name its chip shows, where on the map it lives, and whether
// Enter has anything to commit.
type inspectable struct {
	ref InspectRef
	// name is the chip text — short by contract: a vessel name, a
	// player's handle, a body name, "Node 2", "CA w/ Aurora".
	name string
	// pos is the world point the flare ring and the name chip anchor to.
	pos orbital.Vec3
	// targetable is false for things the Target slot can't hold: your own
	// vessel, the system primary, a planted node, a closest-approach pair.
	// Enter on one of those is a no-op that says so rather than silently
	// doing nothing.
	targetable bool
}

// inspectFlareRingPx is the radius of the bright ring stamped at the
// inspected entity's position. Large enough to read as a halo around a
// vessel glyph or a small body disk at typical zoom, small enough not to
// swamp a crowded rendezvous view.
const inspectFlareRingPx = 3

// resetInspectables clears the frame's inspectable set. Called once at
// the top of Render, before any drawing, so the set only ever contains
// what THIS frame actually inked — the ADR's "if it isn't on screen, it
// isn't inspectable" rule falls out of building the list at the draw
// sites rather than from world state.
func (v *OrbitView) resetInspectables() {
	v.inspectables = v.inspectables[:0]
}

// addInspectable records an entity as inspectable at the point it is
// drawn. Call order IS cycle order (see InspectNext): the map's own draw
// order — scenery, then ghosts, then your vessel, then other local
// vessels, then plan overlays — is already the codebase's deterministic
// overlap-priority rule (ADR 0020 D), so Inspect steps in the same order
// the renderer commits to rather than inventing a second one.
//
// Off-canvas entities are the caller's filter: every call site gates on
// the anchor projecting onto the canvas.
func (v *OrbitView) addInspectable(ref InspectRef, name string, pos orbital.Vec3, targetable bool) {
	v.inspectables = append(v.inspectables, inspectable{ref: ref, name: name, pos: pos, targetable: targetable})
}

// isInspected reports whether ref is the entity currently flaring. The
// per-entity draw sites call it to choose a colour, which is what makes
// the flare a genuine redraw of the entity rather than a decoration
// stamped over it.
func (v *OrbitView) isInspected(ref InspectRef) bool {
	return v.inspectRef.Kind != InspectNone && v.inspectRef == ref
}

// inspectLineColor returns the colour an entity's orbit line should draw
// in this frame: the flare white while it is inspected, otherwise the
// colour the caller already resolved (live-orbit slate, dim, TARGET
// green). Promotion is a colour change only — the Line Class pattern
// (ADR 0041 §2) is untouched, so an inspected ghost's track is still a
// solid Real-class line, just a bright one.
func (v *OrbitView) inspectLineColor(ref InspectRef, base lipgloss.Color) lipgloss.Color {
	if v.isInspected(ref) {
		return render.ColorInspect
	}
	return base
}

// inspected resolves the current highlight against the frame's
// inspectable set. ok=false both when nothing is inspected and when the
// inspected entity is no longer drawn — a ghost that went offline, a
// vessel that panned off-canvas, a node that fired. The caller treats
// those identically: no flare, no chip.
func (v *OrbitView) inspected() (inspectable, bool) {
	if v.inspectRef.Kind == InspectNone {
		return inspectable{}, false
	}
	for _, it := range v.inspectables {
		if it.ref == v.inspectRef {
			return it, true
		}
	}
	return inspectable{}, false
}

// InspectNext steps the highlight one place along the frame's inspectable
// set, in draw order, and wraps through the "nothing inspected" state at
// the end of the lap. That empty slot is deliberate: it makes the cycle a
// way OUT of Inspect as well as through it, so a player who over-steps
// keeps pressing rather than hunting for Esc.
//
// An entity that vanished between frames restarts the lap from the top
// rather than guessing where it would have been.
func (v *OrbitView) InspectNext() {
	if len(v.inspectables) == 0 {
		v.inspectRef = InspectRef{}
		return
	}
	at := -1
	for i, it := range v.inspectables {
		if it.ref == v.inspectRef {
			at = i
			break
		}
	}
	if at+1 >= len(v.inspectables) {
		v.inspectRef = InspectRef{} // wrap through "nothing inspected"
		return
	}
	v.inspectRef = v.inspectables[at+1].ref
}

// InspectClear leaves Inspect. Esc, a commit, and any Framing Event all
// go through here.
func (v *OrbitView) InspectClear() {
	v.inspectRef = InspectRef{}
}

// Inspecting reports whether something is currently inspected — the gate
// the App uses so Esc and Enter only mean "leave Inspect" / "commit this"
// while Inspect is actually up, and keep their existing meanings
// otherwise.
func (v *OrbitView) Inspecting() bool {
	return v.inspectRef.Kind != InspectNone
}

// InspectedRef returns the inspected entity's identity plus whether the
// Target slot can hold it. ok=false when nothing is inspected or the
// entity is no longer drawn.
func (v *OrbitView) InspectedRef() (ref InspectRef, targetable bool, ok bool) {
	it, ok := v.inspected()
	if !ok {
		return InspectRef{}, false, false
	}
	return it.ref, it.targetable, true
}

// InspectedName returns the inspected entity's chip name, or "" when
// nothing is inspected. The App uses it to word the commit / refusal
// message in the same words the chip on the map is showing.
func (v *OrbitView) InspectedName() string {
	it, ok := v.inspected()
	if !ok {
		return ""
	}
	return it.name
}

// InspectByOwner drives Inspect from a canvas pixel's Owner tag — the
// mouse half of ADR 0041 §3. It sets exactly the state InspectNext sets,
// so a click and a key press are indistinguishable downstream. ok=false
// when the key belongs to nothing drawn this frame (a stale tag from a
// previous frame's pixels).
func (v *OrbitView) InspectByOwner(key string) (InspectRef, bool) {
	if key == "" {
		return InspectRef{}, false
	}
	for _, it := range v.inspectables {
		if it.ref.OwnerKey() == key {
			v.inspectRef = it.ref
			return it.ref, true
		}
	}
	return InspectRef{}, false
}

// InspectableCount reports how many entities this frame offered to
// Inspect — a test hook for the "exactly what's drawn" contract.
func (v *OrbitView) InspectableCount() int {
	return len(v.inspectables)
}

// drawInspectFlare stamps the bright ring that marks the inspected entity
// on the canvas, after every other layer so nothing paints over it. The
// entity's own ink is already promoted at its draw site
// (inspectLineColor); the ring is what carries the flare for the kinds
// that have no line of their own — a planted node, a closest-approach
// pair, a vessel too small to show its orbit.
func (v *OrbitView) drawInspectFlare() {
	it, ok := v.inspected()
	if !ok {
		return
	}
	v.canvas.RingColoredOutline(it.pos, inspectFlareRingPx, render.ColorInspect)
}

// composeInspectChip paints the inspected entity's name chip onto the
// rendered canvas, beside the entity rather than in a corner — the chip
// is an ANSWER to "what is this?", so it has to be next to the thing it
// names. One chip, only while inspecting; this is the whole of Inspect's
// standing ink budget.
//
// Placement: one cell to the right of and one row above the entity's
// cell, clamped onto the canvas so an entity near an edge still gets a
// fully-visible chip. Painted last (after the navball and the corner
// Chips) so it is never the thing that gets covered.
func (v *OrbitView) composeInspectChip(canvasStr string, cCols, cRows int) string {
	it, ok := v.inspected()
	if !ok {
		return canvasStr
	}
	px, py, onCanvas := v.canvas.Project(it.pos)
	if !onCanvas {
		return canvasStr
	}
	label := it.name
	if label == "" {
		return canvasStr
	}
	// Keep the chip narrow enough to sit on a small terminal without
	// spanning the map; the names Inspect uses are short by contract, so
	// truncation is a guard, not the normal case.
	maxInner := cCols - 4
	if maxInner < 3 {
		return canvasStr
	}
	if lipgloss.Width(label) > maxInner {
		label = string([]rune(label)[:maxInner-1]) + "…"
	}
	inner := lipgloss.Width(label)
	block := wrapBorder(label, inner, v.theme.Primary.GetForeground())
	bw, bh := inner+2, 3

	col, row := px/2+1, py/4-1
	if col+bw > cCols {
		col = px/2 - bw // flip to the entity's left when it would overrun
	}
	if col < 0 {
		col = 0
	}
	if row < 0 {
		row = 0
	}
	if row+bh > cRows {
		row = cRows - bh
	}
	if row < 0 {
		return canvasStr
	}
	return strings.Join(overlayStyledBlock(strings.Split(canvasStr, "\n"), block, row, col, cCols), "\n")
}

// inspectBodyName is the chip name for a body: its English name, which is
// what the body Cursor, the TARGET chip and the body-info screen all
// already call it.
func inspectBodyName(englishName string) string {
	return englishName
}

// inspectGhostName is the chip name for another player's craft. The
// handle leads — in a session "whose is this" is answered by WHO, and the
// vessel name is the qualifier. Matches World.TargetName's ghost wording
// so the chip and the TARGET readout agree.
func inspectGhostName(handle, craftName string) string {
	switch {
	case handle != "" && craftName != "":
		return handle + "'s " + craftName
	case handle != "":
		return handle
	}
	return craftName
}

// inspectNodeName is the chip name for a planted maneuver node. 1-based,
// matching the tag convention and the Nodes chip's own numbering.
func inspectNodeName(nodeIdx int) string {
	return fmt.Sprintf("Node %d", nodeIdx)
}

// inspectApproachName is the chip name for the closest-approach ✕ pair.
// A CA marker's identity is not a place, it is a RELATIONSHIP — "closest
// approach with X" — so the name says so and carries the target's name
// rather than a bare glyph label.
func inspectApproachName(w *sim.World) string {
	if n := w.TargetName(); n != "" {
		return "CA w/ " + n
	}
	return "closest approach"
}
