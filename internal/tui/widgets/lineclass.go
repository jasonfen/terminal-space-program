package widgets

// Line-style vocabulary (ADR 0041 §2 / issue #346, CONTEXT.md "Line
// Class"): every trajectory the orbit map draws belongs to exactly one of
// three learnable classes, carried entirely by ink PATTERN. Color stays a
// separate axis — semantic (targeting, alarm state), never identity — and
// composes with the class pattern rather than replacing it: a targeted
// entity keeps its class's pattern and gains the TARGET green tint, and
// the pre-existing far-side depth cue (drawEllipseAdaptive's near/far
// spacing split) still thins the ink behind a body regardless of class.
//
// Before this, six orbit layers (own craft, other local craft, ghosts,
// targeted entities, body backdrop, predicted legs) differed only by
// color plus an ad hoc dot stride chosen per call site — no genuine dash
// pattern existed anywhere, so "solid-ish but sparser" and "actually
// dashed" were indistinguishable. This file is the ONE place that states
// what solid / dashed / dotted mean, in canvas pixels.
import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// LineClass is the three-way vocabulary a drawn trajectory's line STYLE
// carries. Never used to encode identity or targeting — see the package
// doc comment above.
type LineClass int

const (
	// ClassReal is a live orbit: solid, contiguous ink. Bright
	// (render.ColorCurrentOrbit) for the active vessel, dim
	// (render.ColorDim / a craft's own Color) for any other real
	// craft/player, including multiplayer Kepler ghosts. Promoted to
	// render.ColorTarget — same pattern, different color — when the
	// entity is targeted.
	ClassReal LineClass = iota
	// ClassPlanned is a burn's before-it-fires consequence: dashed.
	// Node legs, predicted trajectories (including the maneuver
	// planner's post-burn shadow), and SOI-pass encounter arcs.
	ClassPlanned
	// ClassScenery is backdrop: dotted, faint. Body orbits and the SOI
	// Ring — never the thing the player is flying or planning around.
	ClassScenery
)

// Ink-pattern pixel constants. Real's near-side spacing of 1 is genuinely
// contiguous ink ("solid") — before this fold, the own-craft orbit drew at
// a 2px stride and other craft/ghosts at 3-4px, dense enough to read as a
// line but not actually solid, which is exactly why two Real lines at
// different call sites looked like different KINDS instead of the same
// kind at different brightness (ADR 0041's "whose line is whose"
// diagnosis). Real's far side keeps the pre-existing depth-cue halving
// (see drawEllipseAdaptive's doc comment) so the "passes behind the body"
// read survives the fold into one class.
//
// Scenery's spacing is carried over verbatim from the body-orbit call
// site, which already matched "dotted, faint" before this PR — folded
// here so the SOI Ring (previously its own ringDotSpacingPx = 4) shares
// the identical cadence instead of an incidentally-close second number.
//
// Planned's dash/gap replaces the sparse single-pixel-per-N-px dotting
// every "dashed" call site actually used (ADR 0041's measured fact: "no
// dash patterns exist"). The on:off ratio keeps a similar overall ink
// density to the old default so overall clutter is unchanged, but the
// pattern is now a literal dash run rather than one lit pixel in N.
const (
	classRealNearSpacingPx = 1
	classRealFarSpacingPx  = 2

	classSceneryNearSpacingPx = 5
	classSceneryFarSpacingPx  = 10

	classPlannedDashPx = 3
	classPlannedGapPx  = 2
)

// DrawEllipseClass draws an offset ellipse at the LineClass's ink pattern,
// through the same adaptive flattening + near/far body-occlusion split
// DrawEllipseOffsetFarSideDashed established — the class only chooses the
// two spacings, never the flattening or occlusion behaviour.
//
// class must be ClassReal or ClassScenery: every ellipse call site in this
// codebase is a real orbit or a body/scenery orbit. Planned trajectories
// are legs and arcs, not single ellipses — see PlotPolylineClass. Passing
// ClassPlanned here draws with Real's spacing (documented fallback, not a
// panic, since a caller passing it is a mistake to fix, not a state worth
// crashing the render loop over).
func (c *Canvas) DrawEllipseClass(el orbital.Elements, offset orbital.Vec3, minSpans int, class LineClass, bodyPos orbital.Vec3, bodyPxR int, color lipgloss.Color) {
	c.DrawEllipseClassTagged(el, offset, minSpans, class, bodyPos, bodyPxR, CellTag{Color: color})
}

// DrawEllipseClassTagged is DrawEllipseClass with the full CellTag —
// colour PLUS the Inspect Owner key (ADR 0041 §3 / #346) — recorded on
// every pixel of the curve. This is the natural place to thread line
// ownership: the class drawers are already the one chokepoint every
// trajectory call site goes through, so tagging here means a click
// anywhere on ANY drawn orbit resolves to the entity that owns it, with
// no per-call-site hit-test code.
func (c *Canvas) DrawEllipseClassTagged(el orbital.Elements, offset orbital.Vec3, minSpans int, class LineClass, bodyPos orbital.Vec3, bodyPxR int, tag CellTag) {
	near, far := classRealNearSpacingPx, classRealFarSpacingPx
	if class == ClassScenery {
		near, far = classSceneryNearSpacingPx, classSceneryFarSpacingPx
	}
	c.drawEllipseAdaptiveTagged(el, offset, minSpans, near, far, bodyPos, bodyPxR, tag)
}

// PlotPolylineClass draws a run of world points as one connected curve at
// the LineClass's ink pattern — the polyline sibling of DrawEllipseClass,
// for trajectories that aren't a single ellipse: node legs, predicted
// legs, encounter arcs, and the maneuver planner's post-burn shadow. Dash
// phase carries across points exactly like PlotDensePolylineColored (and,
// for ClassPlanned, across separate calls sharing the same curve is NOT
// supported — each call restarts its own phase at 0, matching how every
// current Planned call site already draws one call per connected run).
func (c *Canvas) PlotPolylineClass(pts []orbital.Vec3, color lipgloss.Color, class LineClass) {
	c.PlotPolylineClassTagged(pts, CellTag{Color: color}, class)
}

// PlotPolylineClassTagged is PlotPolylineClass with the full CellTag —
// colour plus the Inspect Owner key (ADR 0041 §3) — on every lit pixel.
// The polyline sibling of DrawEllipseClassTagged; see that doc comment
// for why the class drawers are where line ownership is threaded.
func (c *Canvas) PlotPolylineClassTagged(pts []orbital.Vec3, tag CellTag, class LineClass) {
	switch class {
	case ClassPlanned:
		c.plotDensePolylineDashedTagged(pts, tag, classPlannedDashPx, classPlannedGapPx)
	case ClassScenery:
		c.plotDensePolylineTagged(pts, tag, classSceneryNearSpacingPx)
	default: // ClassReal
		c.plotDensePolylineTagged(pts, tag, classRealNearSpacingPx)
	}
}
