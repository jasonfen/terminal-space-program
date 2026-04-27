package sim

// ViewMode selects the canvas projection basis. v0.6.4+. World-level
// state so the orbit screen and the maneuver-planner mini-canvas
// share the same camera angle without per-screen coordination.
type ViewMode int

const (
	// ViewEquatorial projects (X, Y) in the system / primary's
	// equatorial frame, dropping Z. The pre-v0.6.4 behaviour and the
	// zero-value default — v2 saves load with this mode without
	// schema changes. Inclined orbits foreshorten in this view, but
	// equatorial alignment matches body-rendered surface features.
	ViewEquatorial ViewMode = iota
	// ViewOrbitPerpendicular projects onto the active craft's orbit
	// plane via the perifocal (P, Q) basis. The orbit renders as a
	// clean ellipse with no foreshortening regardless of inclination,
	// useful for inclined transfers and polar mission profiles. Falls
	// back to ViewEquatorial when the craft's orbit is degenerate
	// (no craft, hyperbolic, near-rectilinear) so the basis is
	// well-defined.
	ViewOrbitPerpendicular
)

// String returns a short human label for the view mode.
func (m ViewMode) String() string {
	switch m {
	case ViewEquatorial:
		return "equatorial"
	case ViewOrbitPerpendicular:
		return "orbit-perp"
	}
	return "?"
}

// AllViewModes enumerates the modes in canonical cycle order.
var AllViewModes = [...]ViewMode{
	ViewEquatorial,
	ViewOrbitPerpendicular,
}

// CycleViewMode advances ViewMode to the next mode in cycle order.
// Wraps around — modes are a small finite set.
func (w *World) CycleViewMode() {
	w.ViewMode = (w.ViewMode + 1) % ViewMode(len(AllViewModes))
}
