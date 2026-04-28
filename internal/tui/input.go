package tui

import "github.com/charmbracelet/bubbles/key"

// Keymap centralises every binding the root model cares about. Screens
// that need additional keys define them locally.
type Keymap struct {
	Quit       key.Binding
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
	PlanNode      key.Binding
	ClearNodes    key.Binding
	PlanTransfer  key.Binding
	Porkchop      key.Binding
	Save          key.Binding
	Load          key.Binding
	RefinePlan    key.Binding
	CycleView     key.Binding

	// v0.7.3+ manual flight controls.
	ThrottleFull       key.Binding // engine to 100 %
	ThrottleCut        key.Binding // engine off (also stops a manual burn)
	ThrottleUp         key.Binding // +10 % step
	ThrottleDown       key.Binding // -10 % step
	AttitudePrograde   key.Binding
	AttitudeRetrograde key.Binding
	AttitudeNormalPlus  key.Binding
	AttitudeNormalMinus key.Binding
	AttitudeRadialOut  key.Binding
	AttitudeRadialIn   key.Binding
}

func DefaultKeymap() Keymap {
	return Keymap{
		Quit:       key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit (immediate)")),
		// v0.7.3: QuitAsk moved q → Q to free `q` for AttitudeRadialOut.
		QuitAsk:    key.NewBinding(key.WithKeys("Q"), key.WithHelp("Q", "quit (confirm)")),
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
		PlanNode:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "plan node (T+5m prograde 200m/s)")),
		ClearNodes:   key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "clear nodes")),
		PlanTransfer: key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "plant Hohmann transfer to selected body")),
		Porkchop:     key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "porkchop plot for selected body")),
		Save:         key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "save game")),
		Load:         key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "load game")),
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
	}
}
