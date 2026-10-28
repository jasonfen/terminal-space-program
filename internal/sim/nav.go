package sim

import (
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// NavMode selects the reference frame the SAS axis hotkeys
// (prograde / retrograde / normal±, radial±) interpret against.
// KSP-style: the player cycles a single mode and the same six axis
// keys reinterpret accordingly.
//
//	NavOrbit   — prograde ≡ +v̂ in the active craft's primary-relative
//	             frame (the v0.7.3+ default).
//	NavSurface — prograde ≡ +(v − ω×r), velocity relative to the
//	             rotating atmosphere; useful for ascent. Only prograde
//	             / retrograde rebind in this mode (KSP shows orbital
//	             normal/radial on the surface navball too).
//	NavTarget  — prograde ≡ unit(v_active − v_target), retrograde its
//	             flip; radial± rebinds to BurnTarget / BurnAntiTarget
//	             (toward / away from target). Only valid when
//	             World.Target.Kind == TargetCraft. v0.9.3+.
//	NavPort    — the TARGET's docking-port frame (ADR 0053 decision 7):
//	             q/e push in/out along the port's axis, w/s up/down and
//	             a/d left/right across its face. Offered by `;` only when
//	             the target has a port; the self-switch never enters it.
//	             Appended last: the numbers are persisted (`nav_mode`).
type NavMode int

const (
	NavOrbit NavMode = iota
	NavSurface
	NavTarget
	NavPort
)

// String returns a short HUD label. #478 A2 (one spelling of a frame
// everywhere): this used to spell out SURFACE/TARGET in full while
// GUIDANCE's hold: row tagged the same frames SURF/TGT, so a single
// glance could read the same frame two different ways on one row. Now
// both read the abbreviated form navModeLabel already used.
func (n NavMode) String() string { return n.Tag() }

// Tag is the one frame word the GUIDANCE box, the navball button and the
// Flight Recording's hold column all print.
func (n NavMode) Tag() string {
	switch n {
	case NavSurface:
		return "SURF"
	case NavTarget:
		return "TGT"
	case NavPort:
		return "PORT"
	}
	return "ORBIT"
}

// AttitudeIntent is the input intent — which SAS axis the player
// pressed — independent of NavMode. The TUI maps w/s/a/d/q/e to the
// six intents; ResolveAttitudeIntent translates intent + NavMode to
// the concrete BurnMode the integrator + SAS hold consume. v0.9.3+.
type AttitudeIntent int

const (
	IntentPrograde AttitudeIntent = iota
	IntentRetrograde
	IntentNormalPlus
	IntentNormalMinus
	IntentRadialOut
	IntentRadialIn
)

// ResolveAttitudeIntent maps (intent, NavMode) → BurnMode. NavTarget
// silently falls back to NavOrbit when no craft target is bound — a
// stale NavTarget should never produce a zero-direction SAS hold.
// NavSurface only redefines prograde / retrograde (KSP behavior); the
// other axes stay orbit-relative.
func (w *World) ResolveAttitudeIntent(intent AttitudeIntent) spacecraft.BurnMode {
	nav := w.NavMode
	if nav == NavTarget && !w.HasRelativeTarget() {
		nav = NavOrbit
	}
	if nav == NavPort && !w.TargetHasPort() {
		nav = NavOrbit
	}
	switch nav {
	case NavPort:
		// ADR 0053 decision 7: the six keys become the six port
		// directions. q pushes IN toward their port (your nose on this
		// is the docking attitude), e out, w/s up/down, a/d left/right.
		switch intent {
		case IntentRadialOut:
			return spacecraft.BurnPortIn
		case IntentRadialIn:
			return spacecraft.BurnPortOut
		case IntentPrograde:
			return spacecraft.BurnPortUp
		case IntentRetrograde:
			return spacecraft.BurnPortDown
		case IntentNormalPlus:
			return spacecraft.BurnPortLeft
		case IntentNormalMinus:
			return spacecraft.BurnPortRight
		}
	case NavTarget:
		switch intent {
		case IntentPrograde:
			return spacecraft.BurnTargetPrograde
		case IntentRetrograde:
			return spacecraft.BurnTargetRetrograde
		case IntentRadialOut:
			return spacecraft.BurnTarget
		case IntentRadialIn:
			return spacecraft.BurnAntiTarget
		}
	case NavSurface:
		switch intent {
		case IntentPrograde:
			return spacecraft.BurnSurfacePrograde
		case IntentRetrograde:
			return spacecraft.BurnSurfaceRetrograde
		}
	}
	switch intent {
	case IntentRetrograde:
		return spacecraft.BurnRetrograde
	case IntentNormalPlus:
		return spacecraft.BurnNormalPlus
	case IntentNormalMinus:
		return spacecraft.BurnNormalMinus
	case IntentRadialOut:
		return spacecraft.BurnRadialOut
	case IntentRadialIn:
		return spacecraft.BurnRadialIn
	}
	return spacecraft.BurnPrograde
}

// CycleNavMode advances World.NavMode through Surface → Orbit → Target
// → Port → Surface (Jason, 2026-10-08 for the first three; ADR 0053
// decision 7 appends Port). NavTarget is skipped when no craft target is
// bound, and NavPort unless the target has a docking port, so the player
// never lands on a mode that silently degrades back to orbit. The order
// lives in navModeNext rather than in the enum, because the enum's
// numbers are persisted (save `nav_mode`). Returns the new mode for the
// caller's HUD flash.
func (w *World) CycleNavMode() NavMode {
	// Not comms-gated (ADR 0027): NavMode is the navball reference frame —
	// a display / SAS-reference toggle (like the camera), not a new command
	// to the vessel. The gated attitude command is SetAttitudeMode.
	hasCraftTarget := w.HasRelativeTarget()
	hasPort := w.TargetHasPort()
	// Walk the candidates without writing NavMode: a skipped mode must
	// have no side effects (setNavMode zeroes trims on PORT entry/exit), so
	// only the mode the cycle lands on goes through setNavMode (#689).
	next := w.NavMode
	for i := 0; i < 4; i++ {
		next = navModeNext(next)
		if next == NavTarget && !hasCraftTarget {
			continue
		}
		if next == NavPort && !hasPort {
			continue
		}
		break
	}
	w.setNavMode(next)
	return w.NavMode
}

// navModeNext is the `;` cycle order: Surface → Orbit → Target → Port →
// Surface.
func navModeNext(m NavMode) NavMode {
	switch m {
	case NavSurface:
		return NavOrbit
	case NavOrbit:
		return NavTarget
	case NavTarget:
		return NavPort
	default:
		return NavSurface
	}
}

// reconcileNavMode forces NavTarget back to NavOrbit when the player
// drops or swaps off a craft target, and NavPort back to NavOrbit when
// the target changes to one without a port or loses its port. Called from
// every Target mutator (and once a tick, for a target that sheds its
// port) so NavMode stays self-consistent with what the HUD displays.
// v0.9.3+; Port ADR 0053.
func (w *World) reconcileNavMode() {
	if w.NavMode == NavTarget && !w.HasRelativeTarget() {
		w.NavMode = NavOrbit
	}
	if w.NavMode == NavPort && !w.TargetHasPort() {
		w.setNavMode(NavOrbit)
		w.resetDanglingPortHold()
	}
}

// setNavMode is the one writer of NavMode that moves between PORT and the
// other frames, and keeps one trim pair live (ADR 0055 decision 4): stepping
// onto PORT zeroes the active vessel's surface pair (a surface lean would go
// on tilting the hold invisibly under a (port) row), and leaving PORT zeroes
// the port pair. The port pair is session-only, so nothing else holds it.
func (w *World) setNavMode(next NavMode) {
	prev := w.NavMode
	w.NavMode = next
	if prev == next {
		return
	}
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	if next == NavPort {
		c.PitchTrim, c.HeadingTrim = 0, 0
		c.PortPitchTrim, c.PortYawTrim = 0, 0
	}
	if prev == NavPort {
		c.PortPitchTrim, c.PortYawTrim = 0, 0
	}
}

// resetDanglingPortHold sets a BurnPort* hold to Inertial when PORT drops
// (ADR 0063 §1, superseding ADR 0055's Prograde): a port hold resolves to the zero vector once
// there is no port, so it would go on being named by hold: while pointing
// nowhere. Any other hold is left alone.
func (w *World) resetDanglingPortHold() {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	switch c.AttitudeMode {
	case spacecraft.BurnPortIn, spacecraft.BurnPortOut, spacecraft.BurnPortUp,
		spacecraft.BurnPortDown, spacecraft.BurnPortLeft, spacecraft.BurnPortRight:
		c.AttitudeMode = spacecraft.BurnInertial
	}
}

// AttitudeHoldParts is the held mode's display name and the frame it is
// held in (the frame of the MODE, never the live NavMode; see the tui
// attitudeHoldLabel comment for the rules).
func (w *World) AttitudeHoldParts(mode spacecraft.BurnMode) (string, NavMode) {
	if w.NavMode == NavTarget && w.HasRelativeTarget() {
		switch mode {
		case spacecraft.BurnPrograde:
			mode = spacecraft.BurnTargetPrograde
		case spacecraft.BurnRetrograde:
			mode = spacecraft.BurnTargetRetrograde
		case spacecraft.BurnRadialOut:
			mode = spacecraft.BurnTarget
		case spacecraft.BurnRadialIn:
			mode = spacecraft.BurnAntiTarget
		}
	}
	frame := NavOrbit
	switch mode {
	case spacecraft.BurnSurfacePrograde, spacecraft.BurnSurfaceRetrograde:
		frame = NavSurface
	case spacecraft.BurnTarget, spacecraft.BurnAntiTarget, spacecraft.BurnTargetPrograde, spacecraft.BurnTargetRetrograde:
		frame = NavTarget
	case spacecraft.BurnPortIn, spacecraft.BurnPortOut, spacecraft.BurnPortUp,
		spacecraft.BurnPortDown, spacecraft.BurnPortLeft, spacecraft.BurnPortRight:
		// ADR 0053 decision 7: the six PORT modes are frame-locked to the
		// target's port, so they tag their OWN frame, never the live
		// NavMode (a hold of Port+ stays "Port+ (PORT)" after ; moves on).
		frame = NavPort
	}
	// #478 A2: the bracket tag already names the frame, so a surface-
	// framed mode drops its own redundant "Surface " word ("Surface
	// Retrograde (SURF)" -> "Retrograde (SURF)"). The frame was being
	// named three times on the row otherwise: here, the tag, and (before
	// NavMode.String()'s own A2 fix) the nav: cell beside it.
	return strings.TrimPrefix(mode.String(), "Surface "), frame
}
