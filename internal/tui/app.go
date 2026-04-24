package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

type screenID int

const (
	screenOrbit screenID = iota
	screenBodyInfo
	screenManeuver
	screenHelp
	screenPorkchop
)

// App is the root tea.Model. It owns the world, theme, keymap, and which
// screen is active. Screens read from the shared world; they don't
// mutate it.
type App struct {
	world  *sim.World
	theme  Theme
	keys   Keymap
	active screenID

	selectedBody int

	width, height int

	orbitView *screens.OrbitView
	bodyInfo  *screens.BodyInfo
	help      *screens.Help
	maneuver  *screens.Maneuver
	porkchop  *screens.Porkchop
}

// New builds a root App. Returns an error if systems can't load.
func New() (*App, error) {
	w, err := sim.NewWorld()
	if err != nil {
		return nil, err
	}
	th := DefaultTheme()
	sth := screens.Theme{
		Primary: th.Primary,
		Warning: th.Warning,
		Alert:   th.Alert,
		Dim:     th.Dim,
		HUDBox:  th.HUDBox,
		Footer:  th.Footer,
		Title:   th.Title,
	}
	return &App{
		world:     w,
		theme:     th,
		keys:      DefaultKeymap(),
		active:    screenOrbit,
		orbitView: screens.NewOrbitView(sth),
		bodyInfo:  screens.NewBodyInfo(sth),
		help:      screens.NewHelp(sth),
		maneuver:  screens.NewManeuver(sth),
		porkchop:  screens.NewPorkchop(sth),
	}, nil
}

// Init kicks off the tick loop.
func (a *App) Init() tea.Cmd {
	return sim.TickCmd(a.world.Clock.BaseStep)
}

// Update routes every tea.Msg. Globals handled here; screen-scoped
// keys delegate to the active screen.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case sim.TickMsg:
		a.world.Tick()
		return a, sim.TickCmd(a.world.Clock.BaseStep)

	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.orbitView.Resize(m.Width, m.Height)
		a.maneuver.Resize(m.Width, m.Height)
		return a, nil

	case screens.BurnExecutedMsg:
		if a.world.Craft != nil {
			if m.Duration == 0 {
				a.world.Craft.ApplyImpulsive(m.Mode, m.DV)
			} else {
				a.world.ActiveBurn = &sim.ActiveBurn{
					Mode:        m.Mode,
					DVRemaining: m.DV,
					EndTime:     a.world.Clock.SimTime.Add(m.Duration),
				}
			}
		}
		a.world.Clock.Paused = false
		a.active = screenOrbit
		return a, nil

	case tea.KeyMsg:
		// Maneuver screen has its own text input that eats most keys; we
		// still honor global quit, and esc-to-cancel goes through the
		// screen's handler so it can clean up.
		if a.active == screenManeuver {
			if key.Matches(m, a.keys.Quit) {
				return a, tea.Quit
			}
			if key.Matches(m, a.keys.Back) {
				a.world.Clock.Paused = false
				a.active = screenOrbit
				return a, nil
			}
			cmd, done := a.maneuver.HandleKey(m)
			if done {
				return a, cmd
			}
			return a, cmd
		}
		// Porkchop: ←/→/↑/↓ navigate cells, Esc returns.
		if a.active == screenPorkchop {
			if key.Matches(m, a.keys.Quit) {
				return a, tea.Quit
			}
			_, done := a.porkchop.HandleKey(m)
			if done {
				a.active = screenOrbit
			}
			return a, nil
		}
		switch {
		case key.Matches(m, a.keys.Quit):
			return a, tea.Quit
		case key.Matches(m, a.keys.Help):
			if a.active == screenHelp {
				a.active = screenOrbit
			} else {
				a.active = screenHelp
			}
			return a, nil
		case key.Matches(m, a.keys.Back):
			if a.active != screenOrbit {
				a.active = screenOrbit
			}
			return a, nil
		case key.Matches(m, a.keys.BodyInfo):
			if a.active == screenOrbit {
				a.active = screenBodyInfo
			}
			return a, nil
		case key.Matches(m, a.keys.Maneuver):
			if a.active == screenOrbit && a.world.CraftVisibleHere() {
				a.active = screenManeuver
				a.world.Clock.Paused = true
			}
			return a, nil
		case key.Matches(m, a.keys.WarpUp):
			a.world.Clock.WarpUp()
			return a, nil
		case key.Matches(m, a.keys.WarpDown):
			a.world.Clock.WarpDown()
			return a, nil
		case key.Matches(m, a.keys.Pause):
			a.world.Clock.TogglePause()
			return a, nil
		case key.Matches(m, a.keys.NextSystem):
			a.world.CycleSystem()
			a.selectedBody = 0
			return a, nil
		case key.Matches(m, a.keys.NextBody):
			n := len(a.world.System().Bodies)
			if n > 0 {
				a.selectedBody = (a.selectedBody + 1) % n
			}
			return a, nil
		case key.Matches(m, a.keys.PrevBody):
			n := len(a.world.System().Bodies)
			if n > 0 {
				a.selectedBody = (a.selectedBody - 1 + n) % n
			}
			return a, nil
		case key.Matches(m, a.keys.ZoomIn):
			a.orbitView.ZoomIn()
			return a, nil
		case key.Matches(m, a.keys.ZoomOut):
			a.orbitView.ZoomOut()
			return a, nil
		case key.Matches(m, a.keys.FocusNext):
			a.world.CycleFocus(true)
			return a, nil
		case key.Matches(m, a.keys.FocusPrev):
			a.world.CycleFocus(false)
			return a, nil
		case key.Matches(m, a.keys.FocusReset):
			a.world.ResetFocus()
			return a, nil
		case key.Matches(m, a.keys.PlanNode):
			if a.world.CraftVisibleHere() {
				const defaultDV = 50.0
				dur := finiteBurnDuration(defaultDV, a.world.Craft.TotalMass(), a.world.Craft.Thrust)
				a.world.PlanNode(sim.ManeuverNode{
					TriggerTime: a.world.Clock.SimTime.Add(5 * time.Minute),
					Mode:        spacecraft.BurnPrograde,
					DV:          defaultDV,
					Duration:    dur,
				})
			}
			return a, nil
		case key.Matches(m, a.keys.PlanTransfer):
			if a.world.CraftVisibleHere() && a.selectedBody > 0 {
				_, _ = a.world.PlanTransfer(a.selectedBody)
				// Errors silently ignored: the targeted body is the one
				// the user selected with ←/→, so the only failure modes
				// (system primary, equal radii) are handled by the input
				// guard above. A future polish item is showing the error
				// message in the HUD when planting fails.
			}
			return a, nil
		case key.Matches(m, a.keys.Porkchop):
			if a.active == screenOrbit && a.world.CraftVisibleHere() && a.selectedBody > 0 {
				a.porkchop.Load(a.world, a.selectedBody)
				a.active = screenPorkchop
			}
			return a, nil
		case key.Matches(m, a.keys.ClearNodes):
			a.world.ClearNodes()
			return a, nil
		}
	}
	return a, nil
}

// finiteBurnDuration returns the sim-time duration needed to deliver dv
// at the given mass and engine thrust: Δt = dv × m / F. Zero (impulsive
// fallback) when thrust is zero or the inputs are otherwise degenerate;
// callers set that on ManeuverNode.Duration to opt out of the finite-
// burn integrator branch. Uses mass at plant time — the integrator
// tracks real mass loss once the burn starts, so this is only a
// starting-point budget.
func finiteBurnDuration(dv, mass, thrust float64) time.Duration {
	if thrust <= 0 || mass <= 0 || dv <= 0 {
		return 0
	}
	secs := dv * mass / thrust
	return time.Duration(secs * float64(time.Second))
}

// View delegates to the active screen.
func (a *App) View() string {
	if a.width == 0 {
		return "initializing…"
	}
	switch a.active {
	case screenHelp:
		return a.help.Render()
	case screenBodyInfo:
		return a.bodyInfo.Render(a.world, a.selectedBody, a.width, a.height)
	case screenManeuver:
		return a.maneuver.Render(a.world, a.width, a.height)
	case screenPorkchop:
		return a.porkchop.Render(a.world, a.width, a.height)
	default:
		return a.orbitView.Render(a.world, a.selectedBody, a.width, a.height)
	}
}
