// Package sim — v0.11.0+ ViewLaunch state machine.
//
// ViewLaunch is the chase-cam launch scene routed into automatically
// when an active vessel transitions Landed false→true (Launchpad
// spawn today; future Touchdown semantics will extend the trigger).
// Leaving the chase cam is a manual `v` cycle — ADR 0021 D retired the
// apoapsis-floor auto-release, the one camera change driven by ambient
// sim state. (The ORBIT READY callout and the ViewTilted LaunchAnchor
// keep their LaunchMissionFloorM gate; only the view restore went.)
// A session still ends without a `v` press when the player switches
// active onto a flying vessel — that restore answers the player's
// switch, not ambient sim state.
//
// This file owns the per-tick handler called from World.Tick and the
// session-open/close helpers. Render-side (the chase-cam scene
// itself) lives in internal/tui/screens/launch.go.
//
// Plan reference: designdocs/terminal-space-program/v0.11-plan.md → Slice v0.11.0.
// Architectural rationale: designdocs/terminal-space-program/adr/0002-launch-view-as-distinct-viewmode.md
// (auto-release clause superseded by adr/0021-player-owned-camera-local-to-body-arcs.md, decision D).
package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// tickLaunchView runs the per-tick route/release state machine for
// ViewLaunch. Called from World.Tick after integration so the
// authoritative post-tick Landed state is visible.
//
// Detects the Landed-false→true transition on the active craft and
// routes into ViewLaunch by opening a session; dispatches the
// active-switch handler on pointer changes; samples the breadcrumb
// trail while a session is open. There is no ambient release — ADR
// 0021 D retired the apoapsis-floor auto-release, so the session ends
// only on a manual `v` cycle (CycleViewMode) or a switch onto a
// flying vessel (handleActiveCraftSwitch's end branch).
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

	// 3. (Retired) Auto-release. Pre-ADR-0021 a session ended here when
	// the ascent went nearly orbital (apoapsis clear of the atmosphere
	// AND within a circularisation-Δv cap). ADR 0021 D retired it: the
	// chase cam stays until the player cycles `v` — no ambient sim
	// state moves the camera.

	// 4. Trail sampling — gated on LaunchSessionActive (no breadcrumbs
	// outside a real session).
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
		// Restore PrevViewMode and clear all session state. This is
		// the only sim-side session end left after ADR 0021 D — it
		// answers the player's switch, not ambient sim state.
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

// LaunchReleaseEvent records a ViewLaunch session ending so the App's
// status flash can surface a `"ORBIT READY — returning to <prev view>"`
// toast. Same shape as LastDockEvent — App reads and clears.
type LaunchReleaseEvent struct {
	PrevView string
}

// releaseLaunchSession ends the current session: stamps the toast
// event with the restored ViewMode's label, restores ViewMode to
// PrevViewMode, clears the sentinel, and zeroes all session-scoped
// state so the next route handler entry sees a clean slate. Invoked
// from the active-switch handler's "end" branch (ADR 0021 D retired
// the per-tick apoapsis-floor auto-release that used to call this).
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
// current ViewMode in PrevViewMode so the switch-end release can
// later restore it, sets the sentinel, switches ViewMode, and seeds
// the rest of the session-scoped state: stamps LaunchT0 to current
// sim-time (HUD T+ anchor), zeroes LaunchMaxQ, clears the breadcrumb
// trail, resets LaunchZoom to auto. Idempotent guard lives in the
// caller (tickLaunchView checks !LaunchSessionActive).
//
// The `ViewMode != ViewLaunch` guard on the PrevViewMode capture
// matters for save-load: persisted saves carry ViewMode but not
// LaunchSessionActive, so a save taken mid-session reloads with
// ViewMode=ViewLaunch and LaunchSessionActive=false. The first
// post-load tick then re-fires this handler on the already-Landed
// vessel; without the guard, PrevViewMode would capture ViewLaunch
// and the switch-end release would later "restore" to ViewLaunch
// (a no-op). Leaving PrevViewMode at its zero value (ViewTilted) on
// this path is the correct fallback.
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
