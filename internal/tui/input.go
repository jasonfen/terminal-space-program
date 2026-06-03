package tui

import "github.com/charmbracelet/bubbles/key"

// Keymap centralises every binding the root model cares about. Screens
// that need additional keys define them locally.
type Keymap struct {
	Quit key.Binding
	// QuitAsk used to live on `q` (then `Q` after v0.7.3 freed `q`
	// for AttitudeRadialOut). v0.7.3.1 dropped the binding entirely:
	// quit-confirm now lives on Esc when the home (orbit) view is
	// active, since Esc is otherwise unused there. The field stays
	// in the struct (and keeps a no-key binding) so callers that
	// reference a.keys.QuitAsk continue to compile during the
	// transition; remove in v0.8 cleanup.
	QuitAsk    key.Binding
	Help       key.Binding
	BodyInfo   key.Binding
	Maneuver   key.Binding
	NextBody   key.Binding
	PrevBody   key.Binding
	NextSystem key.Binding
	WarpUp     key.Binding
	WarpDown   key.Binding
	Pause      key.Binding
	ZoomIn     key.Binding
	ZoomOut    key.Binding
	Back       key.Binding
	FocusNext  key.Binding
	FocusPrev  key.Binding
	FocusReset key.Binding
	SpawnCraft key.Binding // v0.8.1+: spawn a new craft. Replaced the v0.7.x PlanNode quick-plant default-node debug aid (m/Enter is the proper plant path).
	// ClearNodes used to live on `N` (the case-collision partner of
	// `n` SpawnCraft, which made remembering which one wiped vs
	// spawned a constant trip-up). v0.8.6 dropped the global
	// keybinding; clear-all now lives in the maneuver-form footer
	// alongside per-node delete (`m` then ctrl+k / ctrl+d). Field
	// kept with no key so callers compile during transition; remove
	// in a later cleanup.
	ClearNodes   key.Binding
	PlanTransfer key.Binding
	PlanIncl     key.Binding // v0.7.4+: plane rotation toward selected body's inclination (or equatorial when none).
	// PlanCircularize (v0.9.4+) plants a prograde burn at the active
	// craft's next apoapsis sized to circularise the orbit there. The
	// "single keystroke for the natural last step of an ascent"
	// shortcut, mirroring the v0.7.4 `I` plane-match planter.
	PlanCircularize key.Binding
	// PlanRendezvous (v0.10.2+) plants the recommended single-burn
	// nudge that improves closest approach to the current craft
	// target. Mirrors the H/I/C capital-letter plant-burn family;
	// reads from World.RecommendedRendezvousBurn (Lambert-and-project
	// over the 8 velocity-frame axes). No-op without a craft target
	// or when the advisory reports no useful nudge.
	PlanRendezvous key.Binding
	Porkchop       key.Binding
	// v0.8.6: Save / Load moved S / L → F5 / F9 to match the KSP
	// quicksave-quickload muscle memory and to clear the case-
	// collision with `s` (AttitudeRetrograde) and `l` (NextBody).
	Save       key.Binding
	Load       key.Binding
	RefinePlan key.Binding
	CycleView  key.Binding

	// v0.7.3+ manual flight controls.
	ThrottleFull        key.Binding // throttle to 100 %
	ThrottleCut         key.Binding // throttle 0 % (also stops a manual burn)
	ThrottleUp          key.Binding // +10 % step
	ThrottleDown        key.Binding // -10 % step
	AttitudePrograde    key.Binding
	AttitudeRetrograde  key.Binding
	AttitudeNormalPlus  key.Binding
	AttitudeNormalMinus key.Binding
	AttitudeRadialOut   key.Binding
	AttitudeRadialIn    key.Binding
	// ToggleBurn (v0.7.3.2+): explicit engage / disengage gate for
	// manual flight. Pre-v0.7.3.2 the attitude keys auto-started the
	// engine, which made accidental burns easy. Now attitude keys
	// only orient; firing requires an explicit `b` press.
	ToggleBurn key.Binding

	// CycleEngine (v0.8.0+): flip between main engine and RCS /
	// monoprop precision thrusters. Bound to `r` for "RCS." In RCS
	// mode the attitude keys (w/s/a/d/q/e) also fire one pulse per
	// keypress in addition to setting the held attitude.
	CycleEngine key.Binding

	// NextCraft / PrevCraft (v0.8.1+): cycle the active craft in the
	// multi-craft slate. No-op when only one craft is loaded.
	NextCraft key.Binding
	PrevCraft key.Binding

	// CraftSlot (v0.12.0+): number-row 1..9 jumps straight to craft
	// index 0..8 (the matched digit is read from the key string).
	// No-op when no craft occupies that slot. `0` stays Pause.
	// Complements the [/] relative cycle once fleets grow past 4.
	CraftSlot key.Binding

	// Undock (v0.8.3+): split the active composite craft back
	// into its docked components.
	Undock key.Binding

	// Transpose (v0.12 / ADR 0009): one-shot Apollo transposition —
	// reorder the pre-transposition [Descent, Ascent, SM, CM] stack so
	// the SM is the firing core with the LM as a docked nose payload.
	Transpose key.Binding

	// CycleTarget / ClearTarget (v0.9.0+): unified `World.Target` slot
	// that planted-Hohmann (`H`) and plane-match (`I`) consume in
	// place of the pre-v0.9 implicit body cursor. Cycle order:
	// non-active sibling craft → bodies in current system → none.
	CycleTarget key.Binding
	ClearTarget key.Binding

	// CycleNavMode (v0.9.3+): rotate the SAS reference frame through
	// Orbit → Surface → Target → Orbit (KSP nav-ball mode cycle).
	// Skips Target when no craft target is bound. The same w/s/a/d/q/e
	// axis keys reinterpret accordingly: in NavTarget, w/s become
	// target-relative prograde/retrograde and q/e become Target/Anti-
	// Target (toward / away). Bound to `;`. Existing `W` / `S` direct-
	// surface shortcuts stay as nav-mode-bypass.
	CycleNavMode key.Binding

	// Stage (v0.9.1+): KSP-style player-managed sequential decouple.
	// Drops the active craft's bottom stage (Stages[0]) — spawning
	// it as a passive Spacecraft in the slate at the same inertial
	// state — and the upper-stage chain becomes the active craft's
	// new propulsion. Bound to `space` per docs/v0.9-plan.md
	// resolved scoping #3 (matches KSP muscle memory). v0.9.1
	// retired the `space` binding from Pause; pause now lives on
	// `0` alone. The maneuver form's iterate-toggle (v0.8.6.3)
	// already used `space` inside `m`; the decouple binding is
	// confined to the no-form context so the two don't collide.
	Stage key.Binding

	// AttitudeSurfacePrograde / AttitudeSurfaceRetrograde (v0.9.2+):
	// align thrust to ±(v - ω×r), the velocity relative to the
	// rotating atmosphere. KSP's "surface prograde" SAS mode for
	// ascent gravity-turn flight. Bound to shift+W / shift+S so the
	// existing w/s prograde-orbit attitudes keep their muscle memory.
	AttitudeSurfacePrograde   key.Binding
	AttitudeSurfaceRetrograde key.Binding

	// PitchTrimEast / PitchTrimWest (v0.9.2+): nudge thrust direction
	// ±5° east of the active mode's natural direction. Used by
	// ascent gravity-turn flight to initiate the pitch-over from
	// vertical. Held → continuous trim ramp at the terminal's
	// key-repeat rate. Reset via PitchTrimReset.
	PitchTrimEast  key.Binding
	PitchTrimWest  key.Binding
	PitchTrimReset key.Binding

	// ToggleInstantSAS (v0.10.0+): flip the manual-flight attitude
	// model between rate-limited slew (MANUAL, the v0.10 default) and
	// the legacy instantaneous "magic SAS" snap (AUTO). This is the
	// locked-decision surfacing for World.InstantSAS — a deliberate,
	// non-silent behaviour switch, mirrored by the navball [SAS]
	// MANUAL/AUTO tag. Bound to `k` (a free key adjacent to the
	// w/s/a/d/q/e attitude cluster); the toggle is a session UI
	// preference and is not persisted.
	ToggleInstantSAS key.Binding

	// TiltUp / TiltDown (v0.10.6+): nudge World.ViewTilt.Theta ±5°
	// while ViewMode == ViewTilted. Per-press step + clamp lives in
	// sim.World.NudgeViewTiltTheta. Bound to shift+↑ / shift+↓ —
	// arrow keys don't have an uppercase form, so the explicit
	// modifier syntax is required (W/S used capitals for letter-key
	// shifts). Yaw φ controls are deliberately deferred to a post-
	// ship playtest signal; the vessel is a single icon, so yaw
	// isn't visually load-bearing the way tilt is.
	TiltUp   key.Binding
	TiltDown key.Binding

	// EndFlight (v0.11.4+, ADR 0004): removes a Crashed active
	// vessel from the slate after a y/n confirm prompt. No-op when
	// the active vessel is not Crashed (the prompt only opens when
	// the predicate holds). Auto-switches active to the next vessel
	// in the slate; falls back to Active=nil when the wreckage was
	// the only vessel. Bound to capital `E` — lowercase `e` is
	// AttitudeRadialIn.
	EndFlight key.Binding

	// JumpToLaunchView (v0.11.4+, ADR 0004): manual jump to
	// ViewLaunch focused on the active vessel. Skips the lowercase
	// `v` cycle (which rotates through orbit / top / sides /
	// orbit-flat without touching ViewLaunch by design — auto-route
	// owns the entry path). Player workflow: switch active to the
	// vessel of interest first, then `V` to scope its chase-cam.
	JumpToLaunchView key.Binding

	// Declutter (v0.13+, ADR 0010): toggle the momentary "hide all
	// overlays" view — suppresses every Chip and the navball to expose a
	// clean orbit map. F2 matches the KSP "toggle UI" convention.
	// Transient and unsaved; never hides the slim HUD column.
	Declutter key.Binding
}

func DefaultKeymap() Keymap {
	return Keymap{
		Quit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit (immediate)")),
		// v0.7.3.1: QuitAsk no longer has a dedicated key — Esc on
		// the home view opens the confirm prompt instead. Binding
		// kept with no keys so the struct field stays stable; remove
		// in v0.8 cleanup.
		QuitAsk:  key.NewBinding(),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		BodyInfo: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "body info")),
		Maneuver: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "maneuver")),
		NextBody: key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next body")),
		PrevBody: key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev body")),
		// v0.7.3: NextSystem moved s → tab to free `s` for AttitudeRetrograde.
		NextSystem: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next system")),
		WarpUp:     key.NewBinding(key.WithKeys("."), key.WithHelp(".", "warp up")),
		WarpDown:   key.NewBinding(key.WithKeys(","), key.WithHelp(",", "warp down")),
		// v0.9.1: dropped `space` from Pause; space is now Stage. `0`
		// alone retains the pause binding (it never collided).
		Pause:           key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "pause")),
		ZoomIn:          key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "zoom in")),
		ZoomOut:         key.NewBinding(key.WithKeys("-", "_"), key.WithHelp("-", "zoom out")),
		Back:            key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		FocusNext:       key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "next focus")),
		FocusPrev:       key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "prev focus")),
		FocusReset:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "focus: system")),
		SpawnCraft:      key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "spawn craft (sister copy of active)")),
		ClearNodes:      key.NewBinding(), // v0.8.6: dropped — see ClearNodes field comment.
		PlanTransfer:    key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "plant transfer to selected body (plane-aware)")),
		PlanIncl:        key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "plant inclination match (selected body / equatorial)")),
		PlanCircularize: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "plant circularize burn at next apoapsis")),
		PlanRendezvous:  key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "plant rendezvous nudge to target craft")),
		Porkchop:        key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "porkchop plot for selected body")),
		Save:            key.NewBinding(key.WithKeys("f5"), key.WithHelp("F5", "quicksave")),
		Load:            key.NewBinding(key.WithKeys("f9"), key.WithHelp("F9", "quickload")),
		RefinePlan:      key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refine plan (re-Lambert arrival)")),
		CycleView:       key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "cycle view (top / right / bottom / left / orbit-flat)")),

		ThrottleFull:        key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "throttle 100%")),
		ThrottleCut:         key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "throttle 0% / cut burn")),
		ThrottleUp:          key.NewBinding(key.WithKeys("Z"), key.WithHelp("Z", "throttle +10%")),
		ThrottleDown:        key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "throttle -10%")),
		AttitudePrograde:    key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "attitude: prograde")),
		AttitudeRetrograde:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "attitude: retrograde")),
		AttitudeNormalPlus:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "attitude: normal+")),
		AttitudeNormalMinus: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "attitude: normal-")),
		AttitudeRadialOut:   key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "attitude: radial+")),
		AttitudeRadialIn:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "attitude: radial-")),
		ToggleBurn:          key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "engage / cut manual burn")),
		CycleEngine:         key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "engine: main / rcs")),
		NextCraft:           key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next craft")),
		PrevCraft:           key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev craft")),
		CraftSlot:           key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "jump to craft N")),
		Undock:              key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "undock active composite")),
		Transpose:           key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "transpose (SM → firing core, LM → nose payload)")),
		CycleTarget:         key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "cycle target (body / craft)")),
		ClearTarget:         key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "clear target")),
		CycleNavMode:        key.NewBinding(key.WithKeys(";"), key.WithHelp(";", "nav: orbit / surface / target")),
		Stage:               key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "decouple bottom stage")),

		AttitudeSurfacePrograde:   key.NewBinding(key.WithKeys("W"), key.WithHelp("W", "attitude: surface prograde")),
		AttitudeSurfaceRetrograde: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "attitude: surface retrograde")),
		PitchTrimEast:             key.NewBinding(key.WithKeys(">"), key.WithHelp(">", "pitch trim +10° east")),
		PitchTrimWest:             key.NewBinding(key.WithKeys("<"), key.WithHelp("<", "pitch trim -10° west")),
		PitchTrimReset:            key.NewBinding(key.WithKeys("\\"), key.WithHelp("\\", "reset pitch trim")),
		ToggleInstantSAS:          key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "SAS model: slew / instant (MANUAL/AUTO)")),
		TiltUp:                    key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("shift+↑", "tilt +5° (ViewTilted)")),
		TiltDown:                  key.NewBinding(key.WithKeys("shift+down"), key.WithHelp("shift+↓", "tilt -5° (ViewTilted)")),
		EndFlight:                 key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "end flight (Crashed vessel)")),
		JumpToLaunchView:          key.NewBinding(key.WithKeys("V"), key.WithHelp("V", "jump to launch view (active vessel)")),
		Declutter:                 key.NewBinding(key.WithKeys("f2"), key.WithHelp("F2", "declutter (hide overlays)")),
	}
}
