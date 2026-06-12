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
//
// This is the single Framing-Event fit resolution (ADR 0021 A): the orbit
// screen calls it exactly once per Framing Event (Focus change, ViewMode
// change, System switch), never per frame. The fit *value* may read sim
// state (the encounter-aware branch below) — the fit *timing* never does.
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
			// Encounter-aware fit (ADR 0021 F): focusing a Body with an
			// active SOI Pass fits to ~1.3× its parent-relative SOI so the
			// SOI Ring, the Local-to-Body arc, and its markers all land in
			// frame — instead of the terminal-body 8×-radius close-up that
			// would crop the capture curve. Evaluated only here, at a
			// Framing Event, so the forward prediction behind bestSOIPass
			// stays off the per-frame hot path.
			if p, ok := w.bestSOIPass(); ok && p.Body.ID == b.ID {
				if soi := physics.SOIRadius(b, w.parentBodyOf(b)); soi > 0 {
					return soi * 1.3
				}
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

// parentBodyOf returns the body b orbits (matched by ParentID), falling back
// to the system root when no parent is found in the catalog. The SOI used by
// the encounter-aware fit must be parent-relative (#143 — a moon's SOI against
// the system *root* is wildly oversized).
func (w *World) parentBodyOf(b bodies.CelestialBody) bodies.CelestialBody {
	sys := w.System()
	for i := range sys.Bodies {
		if sys.Bodies[i].ID == b.ParentID {
			return sys.Bodies[i]
		}
	}
	return sys.Bodies[0]
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
