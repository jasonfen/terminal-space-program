package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/save"
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
	screenMenu
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
	menu      *screens.Menu

	// statusMsg flashes a one-line notice in the HUD footer for ~3
	// seconds after save / load. Cleared by clearStatusAfter via a
	// scheduled tea.Cmd.
	statusMsg     string
	statusExpires time.Time
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
		menu:      screens.NewMenu(sth),
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
			// v0.6.5: derive burn duration from Δv using the rocket
			// equation against the live craft state, so the planner UX
			// only has to specify Δv. Zero-thrust craft fall back to the
			// legacy impulsive path (Duration = 0) — the API still
			// supports that branch, just no longer through the form.
			dur := a.world.Craft.BurnTimeForDV(m.DV)
			// v0.6.4 click-to-edit: replace the original node before
			// planting so click → edit → Enter reads as "modify in
			// place" rather than "duplicate." Removal must come first
			// so PlanNode's sort handles the new node's position
			// against the rest of the (post-removal) slice.
			if m.EditingIdx >= 0 && m.EditingIdx < len(a.world.Nodes) {
				a.world.Nodes = append(a.world.Nodes[:m.EditingIdx], a.world.Nodes[m.EditingIdx+1:]...)
			}
			switch {
			case !m.TriggerTime.IsZero():
				// LoadNode preserved a scheduled trigger — plant a real
				// ManeuverNode at exactly that time, skipping the
				// legacy "fire now" Absolute path that quick-plant
				// uses. Event is forwarded so resolved-then-edited
				// event-relative nodes keep their semantic label.
				a.world.PlanNode(sim.ManeuverNode{
					TriggerTime: m.TriggerTime,
					Mode:        m.Mode,
					DV:          m.DV,
					Duration:    dur,
					Event:       m.Event,
				})
			case m.Event != sim.TriggerAbsolute:
				// v0.6.0: event-relative nodes go through PlanNode so
				// the resolver can freeze TriggerTime against the live
				// orbit on the next Tick.
				a.world.PlanNode(sim.ManeuverNode{
					Mode:     m.Mode,
					DV:       m.DV,
					Duration: dur,
					Event:    m.Event,
				})
			case dur == 0:
				a.world.Craft.ApplyImpulsive(m.Mode, m.DV)
			default:
				a.world.ActiveBurn = &sim.ActiveBurn{
					Mode:        m.Mode,
					DVRemaining: m.DV,
					EndTime:     a.world.Clock.SimTime.Add(dur),
				}
			}
		}
		a.maneuver.ResetEditing()
		a.world.Clock.Paused = false
		a.active = screenOrbit
		return a, nil

	case tea.MouseMsg:
		// v0.6.4: click-only selection. Left-press only; motion /
		// release / wheel ignored. Per-screen routing: orbit's hit
		// dispatch is most-specific-first (vessel → node → body →
		// HUD); porkchop click sets the cell selection.
		if m.Action != tea.MouseActionPress || m.Button != tea.MouseButtonLeft {
			return a, nil
		}
		switch a.active {
		case screenOrbit:
			hit := a.orbitView.HitAt(m.X, m.Y)
			switch {
			case hit.IsVessel:
				if a.world.CraftVisibleHere() {
					a.world.Focus = sim.Focus{Kind: sim.FocusCraft}
				}
			case hit.NodeIdx > 0:
				idx := hit.NodeIdx - 1 // tags are 1-indexed; slice is 0-indexed
				if idx >= 0 && idx < len(a.world.Nodes) {
					a.maneuver.LoadNode(idx, a.world.Nodes[idx])
					a.world.Clock.Paused = true
					a.active = screenManeuver
				}
			case hit.BodyID != "":
				for i, b := range a.world.System().Bodies {
					if b.ID == hit.BodyID {
						a.selectedBody = i
						break
					}
				}
			case a.orbitView.IsCanvasClick(m.X, m.Y):
				// Empty-canvas click → stage a new burn at the
				// orbit point nearest the click. v0.6.4: the user
				// can place a maneuver at a point along their
				// trajectory without manually computing a T+
				// offset. ProjectToOrbit returns time-of-flight
				// from now to that point's true-anomaly; we open
				// the form pre-staged with TriggerAbsolute and
				// that schedule.
				if dt, ok := a.orbitView.ProjectToOrbit(a.world, m.X, m.Y); ok && a.world.CraftVisibleHere() {
					a.maneuver.LoadStaged(a.world.Clock.SimTime.Add(dt))
					a.world.Clock.Paused = true
					a.active = screenManeuver
				}
			case a.orbitView.IsHudClick(m.X):
				// HUD click → open body info for the currently
				// selected body. Coarse: doesn't try to identify
				// which HUD section was clicked, just routes any
				// HUD click to the info screen so the user has a
				// pointer to the same view as `i`.
				a.active = screenBodyInfo
			}
		case screenPorkchop:
			if depIdx, tofIdx, ok := a.porkchop.HitCell(m.X, m.Y); ok {
				a.porkchop.SetSelection(depIdx, tofIdx)
			}
		}
		return a, nil

	case tea.KeyMsg:
		// ctrl+c bypasses everything else (standard interrupt
		// convention). Honored from any screen.
		if key.Matches(m, a.keys.Quit) {
			a.autosave()
			return a, tea.Quit
		}
		// v0.7.3.3+: Esc on the orbit (home) view opens the splash
		// menu. The menu owns the save / load / quit dispatch from
		// then on; every other key is dropped so accidental presses
		// can't fall through to the orbit screen.
		if a.active == screenMenu {
			switch a.menu.HandleKey(m.String()) {
			case screens.MenuActionSave:
				if err := a.doSave(); err != nil {
					a.statusMsg = fmt.Sprintf("save failed: %v", err)
				} else {
					a.statusMsg = "saved"
				}
				a.statusExpires = time.Now().Add(3 * time.Second)
				a.active = screenOrbit
				return a, nil
			case screens.MenuActionLoad:
				if err := a.doLoad(); err != nil {
					a.statusMsg = fmt.Sprintf("load failed: %v", err)
				} else {
					a.statusMsg = "loaded"
				}
				a.statusExpires = time.Now().Add(3 * time.Second)
				a.active = screenOrbit
				return a, nil
			case screens.MenuActionQuit:
				a.autosave()
				return a, tea.Quit
			case screens.MenuActionCancel:
				a.active = screenOrbit
				return a, nil
			}
			return a, nil
		}
		// Maneuver screen has its own text input that eats most keys;
		// esc-to-cancel goes through the screen's handler so it can
		// clean up.
		if a.active == screenManeuver {
			if key.Matches(m, a.keys.Back) {
				a.maneuver.ResetEditing()
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
			_, done := a.porkchop.HandleKey(m)
			if done {
				if tgt, depD, tofD, ok := a.porkchop.PendingPlant(); ok {
					_, _ = a.world.PlanTransferAt(tgt, depD, tofD)
				}
				a.active = screenOrbit
			}
			return a, nil
		}
		switch {
		case key.Matches(m, a.keys.Help):
			if a.active == screenHelp {
				a.active = screenOrbit
			} else {
				a.active = screenHelp
			}
			return a, nil
		case key.Matches(m, a.keys.Back):
			// v0.7.3.3: Esc on the home (orbit) view opens the
			// splash menu (save / load / quit). Replaces the
			// v0.7.3.1 inline "Quit and save? [y/N]" footer prompt
			// with a centered modal. From any other screen Esc
			// returns to orbit first, so a second Esc opens the
			// menu.
			if a.active == screenOrbit {
				a.active = screenMenu
				return a, nil
			}
			a.active = screenOrbit
			return a, nil
		case key.Matches(m, a.keys.BodyInfo):
			if a.active == screenOrbit {
				a.active = screenBodyInfo
			}
			return a, nil
		case key.Matches(m, a.keys.Maneuver):
			if a.active == screenOrbit && a.world.CraftVisibleHere() {
				// Pressing `m` opens for a NEW node — drop any
				// click-to-edit state that may be lingering from a
				// previous open.
				a.maneuver.ResetEditing()
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
				const defaultDV = 200.0
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
		case key.Matches(m, a.keys.Save):
			a.flashStatus("save", a.doSave())
			return a, nil
		case key.Matches(m, a.keys.Load):
			a.flashStatus("load", a.doLoad())
			return a, nil
		case key.Matches(m, a.keys.CycleView):
			a.world.CycleViewMode()
			return a, nil
		case key.Matches(m, a.keys.RefinePlan):
			if a.world.CraftVisibleHere() {
				corr, arr, err := a.world.RefinePlan()
				if err != nil {
					a.statusMsg = fmt.Sprintf("refine failed: %v", err)
				} else {
					a.statusMsg = fmt.Sprintf("refined — correction %.1f m/s, arrival %.1f m/s", corr, arr)
				}
				a.statusExpires = time.Now().Add(3 * time.Second)
			}
			return a, nil

		// v0.7.3+ manual flight controls. v0.7.3.2 split the engage
		// path off from the attitude keys: tapping w/s/a/d/q/e
		// orients only — actually firing the engine requires `b`.
		// Pre-fix the attitude keys auto-started the burn, which
		// was easy to trigger by accident.
		case key.Matches(m, a.keys.ThrottleFull):
			a.world.SetThrottle(1.0)
			return a, nil
		case key.Matches(m, a.keys.ThrottleCut):
			a.world.SetThrottle(0)
			return a, nil
		case key.Matches(m, a.keys.ThrottleUp):
			a.world.AdjustThrottle(0.1)
			return a, nil
		case key.Matches(m, a.keys.ThrottleDown):
			a.world.AdjustThrottle(-0.1)
			return a, nil
		case key.Matches(m, a.keys.AttitudePrograde):
			a.world.SetAttitudeMode(spacecraft.BurnPrograde)
			return a, nil
		case key.Matches(m, a.keys.AttitudeRetrograde):
			a.world.SetAttitudeMode(spacecraft.BurnRetrograde)
			return a, nil
		case key.Matches(m, a.keys.AttitudeNormalPlus):
			a.world.SetAttitudeMode(spacecraft.BurnNormalPlus)
			return a, nil
		case key.Matches(m, a.keys.AttitudeNormalMinus):
			a.world.SetAttitudeMode(spacecraft.BurnNormalMinus)
			return a, nil
		case key.Matches(m, a.keys.AttitudeRadialOut):
			a.world.SetAttitudeMode(spacecraft.BurnRadialOut)
			return a, nil
		case key.Matches(m, a.keys.AttitudeRadialIn):
			a.world.SetAttitudeMode(spacecraft.BurnRadialIn)
			return a, nil
		case key.Matches(m, a.keys.ToggleBurn):
			a.world.ToggleManualBurn()
			return a, nil
		}
	}
	return a, nil
}

// doSave writes the current world to the default save path.
func (a *App) doSave() error {
	path, err := save.DefaultPath()
	if err != nil {
		return err
	}
	return save.Save(a.world, path)
}

// doLoad replaces the live world with the one persisted at the default
// save path. Failures leave the existing world untouched.
func (a *App) doLoad() error {
	path, err := save.DefaultPath()
	if err != nil {
		return err
	}
	w, err := save.Load(path)
	if err != nil {
		return err
	}
	a.world = w
	a.active = screenOrbit
	return nil
}

// autosave persists on quit. Errors are swallowed — the user is leaving
// and there's no surface to flash a message on. Console-printable saves
// can be wired later if needed.
func (a *App) autosave() {
	_ = a.doSave()
}

// flashStatus writes a transient message to the HUD footer.
func (a *App) flashStatus(op string, err error) {
	if err != nil {
		a.statusMsg = fmt.Sprintf("%s failed: %v", op, err)
	} else {
		path, _ := save.DefaultPath()
		a.statusMsg = fmt.Sprintf("%s ok — %s", op, path)
	}
	a.statusExpires = time.Now().Add(3 * time.Second)
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

// View delegates to the active screen, then overlays a transient
// status line at the bottom for ~3s after a save / load.
func (a *App) View() string {
	if a.width == 0 {
		return "initializing…"
	}
	var base string
	switch a.active {
	case screenHelp:
		base = a.help.Render()
	case screenBodyInfo:
		base = a.bodyInfo.Render(a.world, a.selectedBody, a.width, a.height)
	case screenManeuver:
		base = a.maneuver.Render(a.world, a.width, a.height)
	case screenPorkchop:
		base = a.porkchop.Render(a.world, a.width, a.height)
	case screenMenu:
		base = a.menu.Render()
	default:
		base = a.orbitView.Render(a.world, a.selectedBody, a.width, a.height)
	}
	if a.statusMsg != "" && time.Now().Before(a.statusExpires) {
		base += "\n" + a.theme.Footer.Render(a.statusMsg)
	}
	return base
}
