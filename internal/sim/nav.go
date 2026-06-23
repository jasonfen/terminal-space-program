package sim

import "github.com/jasonfen/terminal-space-program/internal/spacecraft"

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
type NavMode int

const (
	NavOrbit NavMode = iota
	NavSurface
	NavTarget
)

// String returns a short HUD label.
func (n NavMode) String() string {
	switch n {
	case NavSurface:
		return "SURFACE"
	case NavTarget:
		return "TARGET"
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
	if nav == NavTarget && w.Target.Kind != TargetCraft {
		nav = NavOrbit
	}
	switch nav {
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

// CycleNavMode advances World.NavMode through Orbit → Surface → Target
// → Orbit. NavTarget is skipped when no craft target is bound so the
// player never lands on a mode that silently degrades back to orbit.
// Returns the new mode for the caller's HUD flash.
func (w *World) CycleNavMode() NavMode {
	// Not comms-gated (ADR 0027): NavMode is the navball reference frame —
	// a display / SAS-reference toggle (like the camera), not a new command
	// to the vessel. The gated attitude command is SetAttitudeMode.
	hasCraftTarget := w.Target.Kind == TargetCraft
	for i := 0; i < 3; i++ {
		w.NavMode = (w.NavMode + 1) % 3
		if w.NavMode != NavTarget || hasCraftTarget {
			return w.NavMode
		}
	}
	return w.NavMode
}

// reconcileNavMode forces NavTarget back to NavOrbit when the player
// drops or swaps off a craft target. Called from every Target mutator
// so NavMode stays self-consistent with what the HUD displays. v0.9.3+.
func (w *World) reconcileNavMode() {
	if w.NavMode == NavTarget && w.Target.Kind != TargetCraft {
		w.NavMode = NavOrbit
	}
}
