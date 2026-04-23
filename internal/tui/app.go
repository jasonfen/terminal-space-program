package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// screenID enumerates the active screen for the state machine in docs/plan.md
// §TUI Design. Only OrbitView is wired in C6; BodyInfo/Maneuver/Help are
// stubs until C9 / C20.
type screenID int

const (
	screenOrbit screenID = iota
	screenBodyInfo
	screenManeuver
	screenHelp
)

// App is the root tea.Model. It owns the world, theme, keymap, and which
// screen is active. Screens read from the shared world; they don't
// mutate it (writes go through root-level messages like burnExecutedMsg).
type App struct {
	world  *sim.World
	theme  Theme
	keys   Keymap
	active screenID

	// selectedBody is the cursor index into the current system's body list
	// (used by OrbitView arrow cycling and by BodyInfo).
	selectedBody int

	width, height int
}

// New builds a root App. Returns an error if systems can't load.
func New() (*App, error) {
	w, err := sim.NewWorld()
	if err != nil {
		return nil, err
	}
	return &App{
		world:  w,
		theme:  DefaultTheme(),
		keys:   DefaultKeymap(),
		active: screenOrbit,
	}, nil
}

// Init kicks off the tick loop.
func (a *App) Init() tea.Cmd {
	return sim.TickCmd(a.world.Clock.BaseStep)
}

// Update routes every tea.Msg. Globals (quit, warp, system-cycle, tick)
// are handled here; screen-scoped keys delegate to the active screen
// (stubbed for C6).
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case sim.TickMsg:
		a.world.Tick()
		return a, sim.TickCmd(a.world.Clock.BaseStep)

	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		return a, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(m, a.keys.Quit):
			return a, tea.Quit
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
		}
	}
	return a, nil
}

// View renders the active screen. C6 is a placeholder text view — the
// real canvas + HUD composition arrives in C7/C8.
func (a *App) View() string {
	sys := a.world.System()
	var selectedName string
	if a.selectedBody < len(sys.Bodies) {
		selectedName = sys.Bodies[a.selectedBody].EnglishName
	}
	pausedTag := ""
	if a.world.Clock.Paused {
		pausedTag = " [PAUSED]"
	}
	header := a.theme.Title.Render(fmt.Sprintf("terminal-space-program — %s", sys.Name))
	status := fmt.Sprintf(
		"system: %s (%d/%d)  sim-time: %s  warp: %.0fx%s  selected: %s",
		sys.Name,
		a.world.SystemIdx+1, len(a.world.Systems),
		a.world.Clock.SimTime.Format("2006-01-02 15:04"),
		a.world.Clock.Warp(),
		pausedTag,
		selectedName,
	)
	footer := a.theme.Footer.Render("[q] quit  [s] next system  [←/→] body  [.,] warp  [0/space] pause")
	return header + "\n\n" + status + "\n\n" + footer + "\n"
}

