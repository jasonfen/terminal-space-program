package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// FocusKind enumerates what the OrbitView canvas is centered on.
type FocusKind int

const (
	// FocusSystem centers on the system primary (Sun) and auto-fits to the
	// outermost body's apoapsis. This is the v0.1.0 default.
	FocusSystem FocusKind = iota
	// FocusBody centers on a specific body by its index in System().Bodies.
	FocusBody
	// FocusCraft centers on the spacecraft's inertial position. Only
	// reachable when CraftVisibleHere is true.
	FocusCraft
)

// Focus describes the current OrbitView center. The zero value (FocusSystem,
// BodyIdx=0) is the v0.1.0 behavior.
type Focus struct {
	Kind    FocusKind
	BodyIdx int
}

// CycleFocus advances the focus to the next target (or previous if
// forward=false). Order: System → Body(0) → Body(1) → … → Body(n-1) →
// Craft (only if CraftVisibleHere) → System.
func (w *World) CycleFocus(forward bool) {
	targets := w.focusTargets()
	if len(targets) == 0 {
		return
	}
	idx := 0
	for i, f := range targets {
		if f == w.Focus {
			idx = i
			break
		}
	}
	if forward {
		idx = (idx + 1) % len(targets)
	} else {
		idx = (idx - 1 + len(targets)) % len(targets)
	}
	w.Focus = targets[idx]
}

// ResetFocus snaps back to the system-wide view.
func (w *World) ResetFocus() { w.Focus = Focus{Kind: FocusSystem} }

// focusTargets enumerates the valid focus cycle for the current system.
// Rebuilt each cycle rather than cached because CraftVisibleHere is
// system-dependent and may flip when the user hits [s] to switch systems.
func (w *World) focusTargets() []Focus {
	targets := []Focus{{Kind: FocusSystem}}
	for i := range w.System().Bodies {
		targets = append(targets, Focus{Kind: FocusBody, BodyIdx: i})
	}
	if w.CraftVisibleHere() {
		targets = append(targets, Focus{Kind: FocusCraft})
	}
	return targets
}

// FocusPosition returns the inertial (system primary-centric) position of
// the current focus target. Origin for FocusSystem.
func (w *World) FocusPosition() orbital.Vec3 {
	switch w.Focus.Kind {
	case FocusBody:
		sys := w.System()
		if w.Focus.BodyIdx >= 0 && w.Focus.BodyIdx < len(sys.Bodies) {
			return w.BodyPosition(sys.Bodies[w.Focus.BodyIdx])
		}
	case FocusCraft:
		if w.CraftVisibleHere() {
			return w.CraftInertial()
		}
	}
	return orbital.Vec3{}
}

// FocusZoomRadius suggests a world-space radius for auto-fit. Canvas.FitTo
// uses ~90% of the smaller pixel axis, so a returned radius R yields a
// frame that comfortably shows a circle of radius R around the focus.
func (w *World) FocusZoomRadius() float64 {
	switch w.Focus.Kind {
	case FocusBody:
		sys := w.System()
		if w.Focus.BodyIdx >= 0 && w.Focus.BodyIdx < len(sys.Bodies) {
			b := sys.Bodies[w.Focus.BodyIdx]
			if b.SemimajorAxis == 0 {
				// System primary: fall back to outermost-body fit.
				return w.systemOutermostRadius()
			}
			// v0.8.5.7+: terminal bodies (those without children
			// orbiting them — Luna, Phobos, Galileans, etc.) zoom
			// in tight to ~8× body radius so the surface texture
			// is clearly visible. Bodies with children (Earth, Mars,
			// Jupiter, Saturn) keep the SOI-radius view so their
			// moons + nearby spacecraft remain in the frame.
			hasChildren := false
			for _, other := range sys.Bodies {
				if other.ParentID == b.ID {
					hasChildren = true
					break
				}
			}
			if !hasChildren {
				return b.RadiusMeters() * 8
			}
			// Use SOI relative to the system primary — gives a close-up
			// that still shows any moons / nearby spacecraft.
			primary := sys.Bodies[0]
			if soi := physics.SOIRadius(b, primary); soi > 0 {
				return soi
			}
			return b.RadiusMeters() * 50
		}
	case FocusCraft:
		if w.ActiveCraft() != nil {
			// Fit to an altitude circle comfortably larger than the craft's
			// current altitude — so the primary planet + craft orbit are
			// both visible.
			alt := w.ActiveCraft().State.R.Norm()
			if alt <= 0 {
				alt = w.ActiveCraft().Primary.RadiusMeters() * 2
			}
			return alt * 3
		}
	}
	return w.systemOutermostRadius()
}

// encounterFrame returns the camera center and auto-fit radius that frame body
// b's predicted SOI-pass arc, when a pass reaches b. The arc draws
// Local-to-Body (ADR 0021 B): body-relative samples anchored at b's CURRENT
// position, so the frame centers on the drawn perilune next to b's disk and
// fits to the arc's SOI-scale extent. ok=false when no pass reaches b or it
// couldn't place its arc — callers then fall back to b's current position.
func (w *World) encounterFrame(b bodies.CelestialBody) (center orbital.Vec3, radius float64, ok bool) {
	p, hit := w.bestSOIPass()
	if !hit || p.Body.ID != b.ID || !p.HasPerilunePt {
		return orbital.Vec3{}, 0, false
	}
	center, radius = w.framePass(p)
	return center, radius, true
}

// framePass is the geometry behind encounterFrame: center on the pass's drawn
// Perilune (PerilunePosition — the Body's current position plus the relative
// offset) and fit to the rebased arc's extent — the farthest body-relative
// sample from the perilune offset, ×1.15 — so the whole drawn capture curve,
// Body disk included, fills the canvas (ADR 0021 B). Falls back to a
// body-radius multiple for a degenerate single-point arc. Takes an
// already-fetched pass so callers that have one (SOIPassViewFraming) don't
// re-run the forward prediction. Caller guarantees p.HasPerilunePt.
func (w *World) framePass(p SOIPass) (center orbital.Vec3, radius float64) {
	center = w.PerilunePosition(p)
	var maxd float64
	for _, s := range p.ArcSegments {
		for _, rel := range s.RelPoints {
			if d := rel.Sub(p.PeriluneRel).Norm(); d > maxd {
				maxd = d
			}
		}
	}
	radius = maxd * 1.15
	if radius <= 0 {
		radius = p.Body.RadiusMeters() * 50
	}
	return center, radius
}

// bodyApproachFraming frames body b for the approach views (ViewTarget /
// ViewSOIPass). When an SOI pass reaches b it centers on the drawn encounter
// arc — Local-to-Body, at b's current position (ADR 0021 B) — and fits to its
// extent, with no craft widening, since the craft is a whole transfer away
// and widening to it would shrink the arc back to a dot (issue #144). With no
// pass it falls back to the pre-encounter approach frame (current position +
// craft-distance widening).
func (w *World) bodyApproachFraming(b bodies.CelestialBody) (center orbital.Vec3, radius float64, ok bool) {
	if center, radius, hit := w.encounterFrame(b); hit {
		return center, radius, true
	}
	return w.bodyApproachFallback(b)
}

// bodyApproachFallback frames body b's craft→body approach before any encounter
// resolves: centered on b's *current* position, fit to its SOI and widened to
// the craft→body distance so the approach line stays in frame and tightens as
// the gap closes (ADR 0019 F). Always ok=true.
func (w *World) bodyApproachFallback(b bodies.CelestialBody) (center orbital.Vec3, radius float64, ok bool) {
	center = w.BodyPosition(b)
	radius = physics.SOIRadius(b, w.System().Bodies[0]) * 1.3
	if radius <= 0 {
		radius = b.RadiusMeters() * 50
	}
	if c := w.ActiveCraft(); c != nil && c.SystemIdx == w.SystemIdx {
		if dist := w.CraftInertial().Sub(center).Norm(); dist > radius {
			radius = dist
		}
	}
	return center, radius, true
}

// TargetViewFraming returns the camera center and auto-fit radius for
// ViewTarget: framing the craft→target approach. Before an encounter is
// predicted it centers on the body Target's current position and widens the fit
// to the craft→target distance so the approach line stays in frame and zooms in
// as the gap closes; once an SOI pass to the target resolves it centers on the
// encounter arc (drawn Local-to-Body at the body's current position, ADR 0021
// B) and fits to the arc's extent so the capture curve fills the canvas.
// ok=false when there's no body target in the active system. v0.17.3+.
func (w *World) TargetViewFraming() (center orbital.Vec3, radius float64, ok bool) {
	if w.Target.Kind != TargetBody {
		return orbital.Vec3{}, 0, false
	}
	sys := w.System()
	if w.Target.BodyIdx <= 0 || w.Target.BodyIdx >= len(sys.Bodies) {
		return orbital.Vec3{}, 0, false
	}
	return w.bodyApproachFraming(sys.Bodies[w.Target.BodyIdx])
}

// SOIPassViewFraming returns the camera center and auto-fit radius for
// ViewSOIPass (ADR 0019 F): framing the active SOI Pass's Body and its
// encounter arc + Perilune marker. It mirrors TargetViewFraming's geometry but
// sources the Pass Body from the pass itself, *not* the Target slot, so framing
// an encounter never requires (or touches) the Target. The pass is the planted
// (node-modified) one when nodes exist — so the view is available, and centered
// on the encounter, while flying a planted transfer (issue #144) — else the
// live pass. ok=false when there's no upcoming SOI Pass — the orbit view then
// falls through to the ordinary focus center, mirroring how ViewTarget degrades
// when the target clears. v0.18.0+.
func (w *World) SOIPassViewFraming() (center orbital.Vec3, radius float64, ok bool) {
	pass, ok := w.bestSOIPass()
	if !ok {
		return orbital.Vec3{}, 0, false
	}
	// Frame the drawn arc directly from the pass we already fetched (avoids a
	// second forward prediction); fall back to the approach frame only when the
	// pass couldn't place its arc.
	if pass.HasPerilunePt {
		center, radius = w.framePass(pass)
		return center, radius, true
	}
	return w.bodyApproachFallback(pass.Body)
}

// FocusEncounterFraming frames the predicted encounter for the currently
// focused body in the ordinary (non-Target / non-SOIPass) views: when an SOI
// pass reaches the focus body it centers on the encounter arc and fits to its
// extent, so a plain "focus the body" view shows the same capture curve
// ViewTarget / ViewSOIPass would (issue #144 — the playtest "focus on Cursor"
// path). ok=false unless the focus is a body with an active encounter, so the
// orbit screen falls through to the ordinary focus center + zoom.
func (w *World) FocusEncounterFraming() (center orbital.Vec3, radius float64, ok bool) {
	if w.Focus.Kind != FocusBody {
		return orbital.Vec3{}, 0, false
	}
	sys := w.System()
	if w.Focus.BodyIdx < 0 || w.Focus.BodyIdx >= len(sys.Bodies) {
		return orbital.Vec3{}, 0, false
	}
	return w.encounterFrame(sys.Bodies[w.Focus.BodyIdx])
}

func (w *World) systemOutermostRadius() float64 {
	var maxR float64
	for _, b := range w.System().Bodies {
		r := b.SemimajorAxisMeters() * (1 + b.Eccentricity)
		if r > maxR {
			maxR = r
		}
	}
	if maxR == 0 {
		return 1e11
	}
	return maxR
}

// FocusName returns a short human label for the current focus.
func (w *World) FocusName() string {
	switch w.Focus.Kind {
	case FocusBody:
		sys := w.System()
		if w.Focus.BodyIdx >= 0 && w.Focus.BodyIdx < len(sys.Bodies) {
			return sys.Bodies[w.Focus.BodyIdx].EnglishName
		}
	case FocusCraft:
		if w.ActiveCraft() != nil {
			return w.ActiveCraft().Name
		}
	}
	return "System-wide"
}
