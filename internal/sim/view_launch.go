// Package sim — v0.11.0+ ViewLaunch state machine.
//
// ViewLaunch is the chase-cam launch scene routed into automatically
// when an active vessel transitions Landed false→true (Launchpad
// spawn today; future Touchdown semantics will extend the trigger).
// Auto-release fires when the ascent is essentially orbital: the orbit's
// apoapsis has climbed clear of the primary's atmosphere AND the
// impulsive circularisation Δv at that apoapsis is within
// LaunchCircularizeDvCapMS — so the player stays in the chase-cam until
// the upper stage has built most of its orbital velocity, then hands off
// to the orbit view. (The ORBIT READY callout and the ViewTilted
// LaunchAnchor keep their own LaunchMissionFloorM gate; this release is
// deliberately stricter.)
//
// This file owns the per-tick handler called from World.Tick and the
// session-open/close helpers. Render-side (the chase-cam scene
// itself) lives in internal/tui/screens/launch.go.
//
// Plan reference: designdocs/terminal-space-program/v0.11-plan.md → Slice v0.11.0.
// Architectural rationale: designdocs/terminal-space-program/adr/0002-launch-view-as-distinct-viewmode.md.
package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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

	// 1. Switch detection. Pointer diff catches real active-vessel
	// changes; the lastActiveCraft != nil guard suppresses the
	// first-tick-after-init / first-tick-after-save-load case where
	// the shadow is unseeded — we don't want a phantom "switch" to
	// fire the handler on the bootstrap tick.
	if w.lastActiveCraft != nil && active != w.lastActiveCraft {
		w.handleActiveCraftSwitch(active)
		// Update wasActiveLanded eagerly so the post-switch Landed-
		// transition check (step 2) reads the new active's state, not
		// the prior active's.
		w.wasActiveLanded = active != nil && active.Landed
	}

	// 2. Landed-transition route. Uses the wasActiveLanded shadow so a
	// player who manually cycled out of ViewLaunch (cleared the
	// session) and remains on a Landed vessel does NOT trigger a
	// spurious re-route — wasActiveLanded stays true across the
	// cycle.
	//
	// v0.11.4+ (ADR 0004): gates on OnPad so a post-flight soft-
	// landing (which sets Landed=true but leaves OnPad=false since
	// OnPad was cleared on the original liftoff) does NOT auto-route
	// the player into ViewLaunch mid-touchdown. The Launchpad-spawn
	// path still triggers the route as before (spawn sets both
	// Landed and OnPad).
	if active != nil && active.Landed && active.OnPad && !w.wasActiveLanded {
		w.routeToLaunchView()
	}

	// 3. Auto-release once the ascent is essentially orbital (apoapsis
	// clear of the atmosphere AND within the circularisation Δv cap).
	// Gated on LaunchSessionActive so a manually-entered ViewLaunch (no
	// session) stays put even when the predicate is satisfied.
	if w.LaunchSessionActive && active != nil && launchAscentNearlyOrbital(active) {
		w.releaseLaunchSession()
	}

	// 4. Trail sampling — gated on LaunchSessionActive (no breadcrumbs
	// outside a real session). Runs after release so a freshly-
	// released session doesn't sample one last point on the way out.
	if w.LaunchSessionActive {
		w.maybeSampleLaunchTrail()
		w.updateLaunchMaxQ()
	}

	// 5. Shadow update for next tick.
	w.wasActiveLanded = active != nil && active.Landed
	w.lastActiveCraft = active
}

// handleActiveCraftSwitch dispatches the four-case switch handler.
// Called from tickLaunchView when the active-craft pointer changes
// (the lastActiveCraft != nil guard is the caller's job — this
// handler assumes a real switch).
//
// Quadrants (Locked decisions, designdocs/terminal-space-program/v0.11-plan.md):
//   - in-session × new Landed   → hand-off (keep session, re-stamp T0)
//   - in-session × new !Landed  → end + restore PrevViewMode
//   - !in-session × new Landed  → fresh inline session
//   - !in-session × new !Landed → no-op
//
// Slice 1 progressively implements quadrants as their tests land.
func (w *World) handleActiveCraftSwitch(newActive *spacecraft.Spacecraft) {
	inSession := w.LaunchSessionActive
	newLanded := newActive != nil && newActive.Landed
	// v0.11.4+ (ADR 0004): "fresh inline session" only opens on a pad
	// spawn (OnPad && Landed). A switch to a soft-landed vessel
	// (Landed && !OnPad) is the "post-flight wreckage / parked"
	// case — don't rip the player into a launch chase-cam for a
	// vessel that already flew.
	newOnPad := newActive != nil && newActive.OnPad

	switch {
	case inSession && newLanded:
		// Hand-off: keep session + view, re-stamp T0 to the switch
		// moment so T+ ticks from the player's perspective of the
		// new vessel's launch.
		w.LaunchT0 = w.Clock.SimTime
		w.LaunchMaxQ = 0
		w.LaunchTrail = w.LaunchTrail[:0]
		w.LaunchZoom = 0
	case inSession && !newLanded:
		// End: player switched onto a flying vessel mid-session.
		// Restore PrevViewMode and clear all session state. Same
		// effect as auto-release without the apo-floor predicate.
		w.releaseLaunchSession()
	case !inSession && newLanded && newOnPad:
		// Fresh inline session: player switched onto a fresh
		// launchpad-spawned vessel while not in a session. The
		// route handler can't pick this up on the same tick because
		// step 1's eager shadow update (post-switch) sets
		// wasActiveLanded=true, suppressing the Landed-transition
		// predicate. So the switch handler opens the session
		// directly — semantically identical to a route. This is
		// the quadrant the pre-grill design missed.
		//
		// v0.11.4+: OnPad-gated — switching to a soft-landed vessel
		// doesn't open a launch session (the vessel already flew;
		// there's nothing to launch).
		w.routeToLaunchView()
	}
	// Quadrant 4 (!inSession && !newLanded) is a no-op — the player
	// is moving between flying vessels with no launch state on either.
}

// LaunchCircularizeDvCapMS is the impulsive circularisation-Δv budget
// (m/s) at or below which an ascent counts as "essentially orbital" —
// the second half of the ViewLaunch auto-release predicate (the first
// being an apoapsis clear of the atmosphere). At ~750 m/s the upper
// stage has built nearly all of its orbital velocity, so the chase-cam
// has served its purpose and the orbit view takes over. Chosen to be
// comfortably above a clean low-orbit circularisation burn yet well
// below the multi-km/s gap of an ascent still mid-gravity-turn.
const LaunchCircularizeDvCapMS = 750.0

// launchAscentNearlyOrbital reports whether the craft's current orbit is
// far enough along to release the ViewLaunch session: its apoapsis sits
// above the primary's atmosphere (above the surface for an airless body)
// AND the impulsive circularisation Δv at that apoapsis is within
// LaunchCircularizeDvCapMS. Hyperbolic / degenerate orbits (a ≤ 0,
// e ≥ 1) return false — apoapsis is undefined there, and the session
// stays open until the trajectory becomes bound.
func launchAscentNearlyOrbital(c *spacecraft.Spacecraft) bool {
	mu := c.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	// Apoapsis undefined on unbound / degenerate orbits.
	if el.A <= 0 || el.E >= 1 {
		return false
	}
	rApo := el.Apoapsis()
	apoAlt := rApo - c.Primary.RadiusMeters()
	// 1. Apoapsis must clear the atmosphere so drag can't decay it
	// (above the surface for an airless body, where atmTop = 0).
	atmTop := 0.0
	if c.Primary.Atmosphere != nil {
		atmTop = c.Primary.Atmosphere.CutoffAltitude
	}
	if apoAlt <= atmTop {
		return false
	}
	// 2. Impulsive circularisation Δv at apoapsis: vis-viva speed there
	// vs. the local circular speed (mirrors the LAUNCH HUD's Δv→circ
	// readout in tui/screens/orbit.go). A circular orbit reads 0.
	vApo := math.Sqrt(mu * (2/rApo - 1/el.A))
	vCirc := math.Sqrt(mu / rApo)
	return vCirc-vApo <= LaunchCircularizeDvCapMS
}

// LaunchReleaseEvent records a ViewLaunch session ending so the App's
// status flash can surface a `"ORBIT READY — returning to <prev view>"`
// toast. Same shape as LastDockEvent — App reads and clears.
type LaunchReleaseEvent struct {
	PrevView string
}

// releaseLaunchSession ends the current session: stamps the toast
// event with the restored ViewMode's label, restores ViewMode to
// PrevViewMode, clears the sentinel, and zeroes all session-scoped
// state so the next route handler entry sees a clean slate. Called
// from tickLaunchView when the auto-release predicate fires; also
// invoked from the active-switch handler's "end" branch.
func (w *World) releaseLaunchSession() {
	w.LastLaunchReleaseEvent = &LaunchReleaseEvent{PrevView: w.PrevViewMode.String()}
	w.ViewMode = w.PrevViewMode
	w.LaunchSessionActive = false
	w.LaunchT0 = time.Time{}
	w.LaunchMaxQ = 0
	w.LaunchTrail = w.LaunchTrail[:0]
	w.LaunchZoom = 0
}

// updateLaunchMaxQ ratchets World.LaunchMaxQ with the active craft's
// instantaneous dynamic pressure each session-active tick. Q is
// 0.5·ρ·|v_rel|² using the same v_rel = v − ω × r the drag integrator
// uses (so a launchpad-co-rotating craft reads Q = 0, not a phantom
// inertial-speed reading). Returns 0 outside the atmosphere or when
// the body has no atmosphere.
func (w *World) updateLaunchMaxQ() {
	c := w.ActiveCraft()
	if c == nil || c.Primary.Atmosphere == nil {
		return
	}
	alt := c.Altitude()
	atm := c.Primary.Atmosphere
	if alt < 0 || alt > atm.CutoffAltitude {
		return
	}
	rho := atm.SurfaceDensity * math.Exp(-alt/atm.ScaleHeight)
	vRel := c.State.V.Sub(physics.AtmosphereOmega(c.Primary).Cross(c.State.R))
	vMag := vRel.Norm()
	q := 0.5 * rho * vMag * vMag
	if q > w.LaunchMaxQ {
		w.LaunchMaxQ = q
	}
}

// NudgeLaunchZoom adjusts the player-pinned chase-cam scale in
// response to a `+/-` press. dir > 0 zooms in (×0.8), dir < 0 zooms
// out (×1.25), dir == 0 is a no-op. The first press from auto
// (LaunchZoom == 0) pins LaunchZoom to currentAutoScale BEFORE
// applying the multiplicative step — caller supplies the auto-scale
// because canvas-row knowledge lives in the screen layer, not the
// sim. Floor: 1.0 m/cell. No-op when there's no active craft.
// v0.11.0+ Slice 1.
func (w *World) NudgeLaunchZoom(dir int, currentAutoScale float64) {
	if w.ActiveCraft() == nil || dir == 0 {
		return
	}
	if w.LaunchZoom <= 0 {
		if currentAutoScale > 0 {
			w.LaunchZoom = currentAutoScale
		} else {
			w.LaunchZoom = 1.0
		}
	}
	if dir > 0 {
		w.LaunchZoom *= 0.8
	} else {
		w.LaunchZoom *= 1.25
	}
	if w.LaunchZoom < 1.0 {
		w.LaunchZoom = 1.0
	}
}

// routeToLaunchView opens a fresh ViewLaunch session. Stashes the
// current ViewMode in PrevViewMode so auto-release can later restore
// it, sets the sentinel, switches ViewMode, and seeds the rest of
// the session-scoped state: stamps LaunchT0 to current sim-time
// (HUD T+ anchor), zeroes LaunchMaxQ, clears the breadcrumb trail,
// resets LaunchZoom to auto. Idempotent guard lives in the caller
// (tickLaunchView checks !LaunchSessionActive).
//
// The `ViewMode != ViewLaunch` guard on the PrevViewMode capture
// matters for save-load: persisted saves carry ViewMode but not
// LaunchSessionActive, so a save taken mid-session reloads with
// ViewMode=ViewLaunch and LaunchSessionActive=false. The first
// post-load tick then re-fires this handler on the already-Landed
// vessel; without the guard, PrevViewMode would capture ViewLaunch
// and auto-release would later "restore" to ViewLaunch (a no-op).
// Leaving PrevViewMode at its zero value (ViewTilted) on this path
// is the correct fallback.
func (w *World) routeToLaunchView() {
	if w.ViewMode != ViewLaunch {
		w.PrevViewMode = w.ViewMode
	}
	w.LaunchSessionActive = true
	w.ViewMode = ViewLaunch
	w.LaunchT0 = w.Clock.SimTime
	w.LaunchMaxQ = 0
	w.LaunchTrail = w.LaunchTrail[:0]
	w.LaunchZoom = 0
}
