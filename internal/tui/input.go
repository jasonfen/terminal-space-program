package tui

import "github.com/charmbracelet/bubbles/key"

// Keymap centralises every binding the root model cares about. Screens
// that need additional keys define them locally.
type Keymap struct {
	Quit       key.Binding
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
}

func DefaultKeymap() Keymap {
	return Keymap{
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		BodyInfo:   key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "body info")),
		Maneuver:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "maneuver")),
		NextBody:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next body")),
		PrevBody:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev body")),
		NextSystem: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "next system")),
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
	}
}
