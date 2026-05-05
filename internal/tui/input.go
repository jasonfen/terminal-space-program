package tui

import "github.com/charmbracelet/bubbles/key"

// Keymap centralises every binding the root model cares about. Screens
// that need additional keys define them locally.
type Keymap struct {
	Quit       key.Binding
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
	SpawnCraft    key.Binding // v0.8.1+: spawn a new craft. Replaced the v0.7.x PlanNode quick-plant default-node debug aid (m/Enter is the proper plant path).
	// ClearNodes used to live on `N` (the case-collision partner of
	// `n` SpawnCraft, which made remembering which one wiped vs
	// spawned a constant trip-up). v0.8.6 dropped the global
	// keybinding; clear-all now lives in the maneuver-form footer
	// alongside per-node delete (`m` then ctrl+k / ctrl+d). Field
	// kept with no key so callers compile during transition; remove
	// in a later cleanup.
	ClearNodes    key.Binding
	PlanTransfer  key.Binding
	PlanIncl      key.Binding // v0.7.4+: plane rotation toward selected body's inclination (or equatorial when none).
	Porkchop      key.Binding
	// v0.8.6: Save / Load moved S / L → F5 / F9 to match the KSP
	// quicksave-quickload muscle memory and to clear the case-
	// collision with `s` (AttitudeRetrograde) and `l` (NextBody).
	Save          key.Binding
	Load          key.Binding
	RefinePlan    key.Binding
	CycleView     key.Binding

	// v0.7.3+ manual flight controls.
	ThrottleFull       key.Binding // throttle to 100 %
	ThrottleCut        key.Binding // throttle 0 % (also stops a manual burn)
	ThrottleUp         key.Binding // +10 % step
	ThrottleDown       key.Binding // -10 % step
	AttitudePrograde   key.Binding
	AttitudeRetrograde key.Binding
	AttitudeNormalPlus  key.Binding
	AttitudeNormalMinus key.Binding
	AttitudeRadialOut  key.Binding
	AttitudeRadialIn   key.Binding
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

	// Undock (v0.8.3+): split the active composite craft back
	// into its docked components.
	Undock key.Binding

	// CycleTarget / ClearTarget (v0.9.0+): unified `World.Target` slot
	// that planted-Hohmann (`H`) and plane-match (`I`) consume in
	// place of the pre-v0.9 implicit body cursor. Cycle order: bodies
	// in current system → non-active sibling craft → none.
	CycleTarget key.Binding
	ClearTarget key.Binding
}

func DefaultKeymap() Keymap {
	return Keymap{
		Quit:       key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit (immediate)")),
		// v0.7.3.1: QuitAsk no longer has a dedicated key — Esc on
		// the home view opens the confirm prompt instead. Binding
		// kept with no keys so the struct field stays stable; remove
		// in v0.8 cleanup.
		QuitAsk:    key.NewBinding(),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		BodyInfo:   key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "body info")),
		Maneuver:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "maneuver")),
		NextBody:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next body")),
		PrevBody:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev body")),
		// v0.7.3: NextSystem moved s → tab to free `s` for AttitudeRetrograde.
		NextSystem: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next system")),
		WarpUp:     key.NewBinding(key.WithKeys("."), key.WithHelp(".", "warp up")),
		WarpDown:   key.NewBinding(key.WithKeys(","), key.WithHelp(",", "warp down")),
		Pause:      key.NewBinding(key.WithKeys("0", " "), key.WithHelp("0/space", "pause")),
		ZoomIn:     key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "zoom in")),
		ZoomOut:    key.NewBinding(key.WithKeys("-", "_"), key.WithHelp("-", "zoom out")),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		FocusNext:  key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "next focus")),
		FocusPrev:  key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "prev focus")),
		FocusReset: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "focus: system")),
		SpawnCraft:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "spawn craft (sister copy of active)")),
		ClearNodes:   key.NewBinding(), // v0.8.6: dropped — see ClearNodes field comment.
		PlanTransfer: key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "plant Hohmann transfer to selected body")),
		PlanIncl:     key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "plant inclination match (selected body / equatorial)")),
		Porkchop:     key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "porkchop plot for selected body")),
		Save:         key.NewBinding(key.WithKeys("f5"), key.WithHelp("F5", "quicksave")),
		Load:         key.NewBinding(key.WithKeys("f9"), key.WithHelp("F9", "quickload")),
		RefinePlan:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refine plan (re-Lambert arrival)")),
		CycleView:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "cycle view (top / right / bottom / left / orbit-flat)")),

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
		Undock:              key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "undock active composite")),
		CycleTarget:         key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "cycle target (body / craft)")),
		ClearTarget:         key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "clear target")),
	}
}
