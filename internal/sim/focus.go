package sim

import (
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
			// Use SOI relative to the system primary — gives a close-up
			// that still shows any moons / nearby spacecraft.
			primary := sys.Bodies[0]
			if soi := physics.SOIRadius(b, primary); soi > 0 {
				return soi
			}
			return b.RadiusMeters() * 50
		}
	case FocusCraft:
		if w.Craft != nil {
			// Fit to an altitude circle comfortably larger than the craft's
			// current altitude — so the primary planet + craft orbit are
			// both visible.
			alt := w.Craft.State.R.Norm()
			if alt <= 0 {
				alt = w.Craft.Primary.RadiusMeters() * 2
			}
			return alt * 3
		}
	}
	return w.systemOutermostRadius()
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
		if w.Craft != nil {
			return w.Craft.Name
		}
	}
	return "System-wide"
}
