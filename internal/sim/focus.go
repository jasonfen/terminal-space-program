package sim

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
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
	// FocusGhost centers on a remote player's ghost — the Spectate mode
	// (v0.28 S6, ADR 0034). Entered only from the Session screen's [v]
	// row action, never in the CycleFocus rotation. The camera tracks
	// the ghost's world position each frame; the single Framing-Event
	// fit frames its drawn orbit extent. Reports moving the ghost never
	// re-fit (ADR 0021). The ghost is addressed by (owner, craft ID) so
	// a stale slate degrades gracefully rather than dangling a pointer.
	FocusGhost
)

// Focus describes the current OrbitView center. The zero value (FocusSystem,
// BodyIdx=0) is the v0.1.0 behavior.
type Focus struct {
	Kind    FocusKind
	BodyIdx int
	// GhostOwner + GhostCraftID address the spectated ghost when
	// Kind == FocusGhost (v0.28 S6). Kept as a value ref, not a pointer,
	// so Focus stays comparable — CycleFocus and the orbit screen's
	// framing-event guard both rely on struct equality.
	GhostOwner   string
	GhostCraftID uint64
}

// CycleFocus advances the focus to the next target (or previous if
// forward=false). Order: System → Body(0) → Body(1) → … → Body(n-1) →
// Craft (only if CraftVisibleHere) → System.
func (w *World) CycleFocus(forward bool) {
	// Spectate exit (v0.28 S6): a ghost focus is outside the cycle, so any
	// focus key returns you home rather than advancing off a non-member.
	// This is the "return-to-own-craft focus key" the Spectate spec names —
	// one more Framing Event restoring own-craft framing (or the system
	// view when no craft is visible here).
	if w.Focus.Kind == FocusGhost {
		w.Focus = w.ownCraftFocus()
		return
	}
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

// ResetFocus snaps back to the system-wide view. Also the coarse exit
// from Spectate (v0.28 S6) — [g] leaves a ghost focus like any other.
func (w *World) ResetFocus() { w.Focus = Focus{Kind: FocusSystem} }

// ownCraftFocus is the framing to return to when leaving a transient
// focus (Spectate): the active craft when it's visible here, else the
// system view. v0.28 S6.
func (w *World) ownCraftFocus() Focus {
	if w.CraftVisibleHere() {
		return Focus{Kind: FocusCraft}
	}
	return Focus{Kind: FocusSystem}
}

// SpectateGhost enters Spectate mode on a remote player's ghost (v0.28
// S6, ADR 0034): the camera fits once to the ghost's drawn orbit extent
// (a Framing Event) then tracks the ghost as focus, pan/zoom free and no
// re-fit on report corrections. Read-only — it adds no write surface, so
// it's reachable by host and guest alike. The Session screen is the
// selection surface; the ref is validated lazily at render (a vanished
// ghost degrades to the system view via FocusPosition/FocusZoomRadius).
func (w *World) SpectateGhost(owner string, craftID uint64) {
	w.Focus = Focus{Kind: FocusGhost, GhostOwner: owner, GhostCraftID: craftID}
}

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
	case FocusGhost:
		// Track the spectated ghost's world position each frame (v0.28 S6).
		// A vanished ghost (owner disconnected, craft gone, left system)
		// degrades to the system origin rather than dangling — Spectate
		// stays live but the view falls back gracefully.
		if g, ok := w.ghostByRef(w.Focus.GhostOwner, w.Focus.GhostCraftID); ok {
			return g.Pos
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
				if soi := w.BodySOIRadius(b); soi > 0 {
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
			// Use the parent-relative SOI — gives a close-up that still
			// shows any moons / nearby spacecraft. Parent, not the system
			// root (#143): identical for a planet, correct for a moon.
			if soi := w.BodySOIRadius(b); soi > 0 {
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
	case FocusGhost:
		// Frame the ghost's whole drawn ellipse (v0.28 S6, ADR 0034). The
		// same S2 ghost-orbit pipeline: ElementsFromState off the ghost's
		// retained primary-relative state and its primary's μ. Centered on
		// the ghost — a point on its own orbit — the far side of the ellipse
		// is at most one major axis (2a) away, so 2a frames the whole track
		// from any phase. Evaluated once, at the Framing Event; report
		// corrections that move the ghost never re-run this (ADR 0021).
		if g, ok := w.ghostByRef(w.Focus.GhostOwner, w.Focus.GhostCraftID); ok {
			if primary, ok := w.bodyInSystemByID(g.PrimaryID); ok {
				el := orbital.ElementsFromState(g.RelPos, g.Vel, primary.GravitationalParameter())
				if el.A > 0 && !math.IsNaN(el.A) && !math.IsInf(el.A, 0) {
					return 2 * el.A
				}
			}
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

// FocusedBody returns the body the camera is focused on, when Focus is
// a FocusBody with a valid index. ok is false for system/craft focus or
// an out-of-range index.
func (w *World) FocusedBody() (bodies.CelestialBody, bool) {
	if w.Focus.Kind != FocusBody {
		return bodies.CelestialBody{}, false
	}
	sys := w.System()
	if w.Focus.BodyIdx < 0 || w.Focus.BodyIdx >= len(sys.Bodies) {
		return bodies.CelestialBody{}, false
	}
	return sys.Bodies[w.Focus.BodyIdx], true
}

// FocusIsEncounterFramed reports whether the current body focus is being
// framed for an active SOI pass (ADR 0021 F) — i.e. FocusZoomRadius
// returned the ~1.3× parent-SOI encounter fit rather than the default
// surface-viewing fit. The orbit screen uses this to leave the wide
// encounter framing alone instead of zooming in to show the body's
// surface texture.
func (w *World) FocusIsEncounterFramed() bool {
	b, ok := w.FocusedBody()
	if !ok {
		return false
	}
	p, ok := w.bestSOIPass()
	return ok && p.Body.ID == b.ID
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
	case FocusGhost:
		// Spectate label (v0.28 S6): "spectating <handle>" so the title-bar
		// focus readout says whose ghost the camera is following.
		if g, ok := w.ghostByRef(w.Focus.GhostOwner, w.Focus.GhostCraftID); ok {
			who := g.Handle
			if who == "" {
				who = g.Name
			}
			return "spectating " + who
		}
		return "spectating"
	}
	return "System-wide"
}
