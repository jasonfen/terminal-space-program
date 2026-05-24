// Package sim — v0.11.0+ ViewLaunch state machine.
//
// ViewLaunch is the chase-cam launch scene routed into automatically
// when an active vessel transitions Landed false→true (Launchpad
// spawn today; future Touchdown semantics will extend the trigger).
// Auto-release fires when the orbit's apoapsis crosses
// LaunchMissionFloorM — the same predicate that gates v0.10.7's
// ORBIT READY callout, sharing single-source-of-truth with the
// LaunchAnchor on ViewTilted.
//
// This file owns the per-tick handler called from World.Tick and the
// session-open/close helpers. Render-side (the chase-cam scene
// itself) lives in internal/tui/screens/launch.go.
//
// Plan reference: docs/v0.11-plan.md → Slice v0.11.0.
// Architectural rationale: docs/adr/0002-launch-view-as-distinct-viewmode.md.
package sim

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// tickLaunchView runs the per-tick route/release state machine for
// ViewLaunch. Called from World.Tick after integration so the
// authoritative post-tick Landed state is visible.
//
// v0.11.0 Slice 1 tracer-bullet shape: detects the Landed-false→true
// transition on the active craft and routes into ViewLaunch by
// opening a session; releases the session when the orbit's apoapsis
// crosses LaunchMissionFloorM (parity with v0.10.7's
// LaunchAnchorPhi predicate). Subsequent slice-time tests will
// extend this with the active-switch handler, manual-cycle clearing,
// and pointer-keyed shadow disambiguation.
func (w *World) tickLaunchView() {
	active := w.ActiveCraft()
	if active != nil && active.Landed && !w.LaunchSessionActive {
		w.routeToLaunchView()
	}
	if w.LaunchSessionActive && active != nil && apoAltAboveFloor(active) {
		w.releaseLaunchSession()
	}
}

// apoAltAboveFloor reports whether the craft's current orbit has an
// apoapsis altitude (AGL relative to the primary's mean radius) above
// LaunchMissionFloorM. Hyperbolic / degenerate orbits (a ≤ 0,
// e ≥ 1) return false — apoapsis is undefined there, and the plan
// explicitly leaves the session running until the orbit becomes
// bound. Mirrors the predicate in v0.10.7's LaunchAnchorPhi.
func apoAltAboveFloor(c *spacecraft.Spacecraft) bool {
	mu := c.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if el.A <= 0 || el.E >= 1 {
		return false
	}
	apoAlt := el.Apoapsis() - c.Primary.RadiusMeters()
	return apoAlt > LaunchMissionFloorM
}

// releaseLaunchSession ends the current session: restores ViewMode to
// PrevViewMode, clears the sentinel, and zeroes all session-scoped
// state so the next route handler entry sees a clean slate. Called
// from tickLaunchView when the auto-release predicate fires; will
// also be invoked from the active-switch handler's "end" branch in
// a subsequent test cycle.
func (w *World) releaseLaunchSession() {
	w.ViewMode = w.PrevViewMode
	w.LaunchSessionActive = false
	w.LaunchT0 = time.Time{}
	w.LaunchMaxQ = 0
	w.LaunchTrail = w.LaunchTrail[:0]
	w.LaunchZoom = 0
}

// routeToLaunchView opens a fresh ViewLaunch session. Stashes the
// current ViewMode in PrevViewMode so auto-release can later restore
// it, sets the sentinel, switches ViewMode, and seeds the rest of
// the session-scoped state: stamps LaunchT0 to current sim-time
// (HUD T+ anchor), zeroes LaunchMaxQ, clears the breadcrumb trail,
// resets LaunchZoom to auto. Idempotent guard lives in the caller
// (tickLaunchView checks !LaunchSessionActive).
func (w *World) routeToLaunchView() {
	w.PrevViewMode = w.ViewMode
	w.LaunchSessionActive = true
	w.ViewMode = ViewLaunch
	w.LaunchT0 = w.Clock.SimTime
	w.LaunchMaxQ = 0
	w.LaunchTrail = w.LaunchTrail[:0]
	w.LaunchZoom = 0
}
