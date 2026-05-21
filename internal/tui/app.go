package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
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
	screenMissions
	screenSpawn // v0.8.2+: craft-type pick form on `n`.
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
	missions  *screens.Missions
	spawn     *screens.SpawnCraft

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
		missions:  screens.NewMissions(sth),
		spawn:     screens.NewSpawnCraft(sth),
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
		// v0.8.3+: surface docking events as a status flash. Cleared
		// here so a single fusion only flashes once.
		if e := a.world.LastDockEvent; e != nil {
			a.statusMsg = fmt.Sprintf("● DOCKED — composite: %s", e.CompositeName)
			a.statusExpires = time.Now().Add(4 * time.Second)
			a.world.LastDockEvent = nil
		}
		return a, sim.TickCmd(a.world.Clock.BaseStep)

	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.orbitView.Resize(m.Width, m.Height)
		a.maneuver.Resize(m.Width, m.Height)
		return a, nil

	case screens.BurnExecutedMsg:
		if a.world.ActiveCraft() != nil {
			// v0.8.6 (b): if the form's iterate-for-target toggle was
			// on, refine the commanded Δv via World.IterateBurnDV so
			// the post-burn apsides match what an impulsive Δv at the
			// same value would have delivered. Falls back to the
			// commanded Δv on iteration failure (e.g. Newton diverged)
			// so the burn always plants — over-/under-deliver is a
			// graceful fallback.
			if m.IterateForTarget {
				if refined, err := a.world.IterateBurnDV(m.Mode, m.DV); err == nil {
					m.DV = refined
				}
			}
			// v0.6.5: derive burn duration from Δv using the rocket
			// equation against the live craft state, so the planner UX
			// only has to specify Δv. Zero-thrust craft fall back to the
			// legacy impulsive path (Duration = 0) — the API still
			// supports that branch, just no longer through the form.
			dur := a.world.ActiveCraft().BurnTimeForDV(m.DV)
			// v0.6.4 click-to-edit: replace the original node before
			// planting so click → edit → Enter reads as "modify in
			// place" rather than "duplicate." Removal must come first
			// so PlanNode's sort handles the new node's position
			// against the rest of the (post-removal) slice.
			if m.EditingIdx >= 0 && m.EditingIdx < len(a.world.ActiveCraft().Nodes) {
				a.world.ActiveCraft().Nodes = append(a.world.ActiveCraft().Nodes[:m.EditingIdx], a.world.ActiveCraft().Nodes[m.EditingIdx+1:]...)
			}
			switch {
			case !m.TriggerTime.IsZero():
				// LoadNode preserved a scheduled trigger — plant a real
				// ManeuverNode at exactly that time, skipping the
				// legacy "fire now" Absolute path that quick-plant
				// uses. Event is forwarded so resolved-then-edited
				// event-relative nodes keep their semantic label.
				a.world.PlanNode(sim.ManeuverNode{
					TriggerTime:    m.TriggerTime,
					Mode:           m.Mode,
					DV:             m.DV,
					Duration:       dur,
					Event:          m.Event,
					Throttle:       m.Throttle,
					TargetCraftIdx: m.TargetCraftIdx,
				})
			case m.Event != sim.TriggerAbsolute:
				// v0.6.0: event-relative nodes go through PlanNode so
				// the resolver can freeze TriggerTime against the live
				// orbit on the next Tick.
				a.world.PlanNode(sim.ManeuverNode{
					Mode:           m.Mode,
					DV:             m.DV,
					Duration:       dur,
					Event:          m.Event,
					Throttle:       m.Throttle,
					TargetCraftIdx: m.TargetCraftIdx,
				})
			case dur == 0:
				// v0.9.3+: target-relative impulsive needs the bound
				// target snapshot for direction resolution.
				if m.TargetCraftIdx != 0 {
					if tIdx := m.TargetCraftIdx - 1; tIdx >= 0 && tIdx < len(a.world.Crafts) {
						if tc := a.world.Crafts[tIdx]; tc != nil && tc.Primary.ID == a.world.ActiveCraft().Primary.ID {
							a.world.ActiveCraft().ApplyImpulsiveWithTarget(m.Mode, m.DV, tc.State.R, tc.State.V)
							break
						}
					}
				}
				a.world.ActiveCraft().ApplyImpulsive(m.Mode, m.DV)
			default:
				effThrottle := m.Throttle
				if effThrottle <= 0 {
					effThrottle = 1.0
				}
				a.world.ActiveCraft().ActiveBurn = &sim.ActiveBurn{
					Mode:           m.Mode,
					DVRemaining:    m.DV,
					EndTime:        a.world.Clock.SimTime.Add(dur),
					Throttle:       effThrottle,
					TargetCraftIdx: m.TargetCraftIdx,
				}
			}
		}
		a.maneuver.ResetEditing()
		a.world.Clock.Paused = false
		a.active = screenOrbit
		return a, nil

	case screens.NodeDeleteMsg:
		// v0.8.6+: per-node delete from the maneuver form. Form
		// dispatched ctrl+d while editing a planted node.
		a.world.DeleteNode(m.EditingIdx)
		a.maneuver.ResetEditing()
		a.world.Clock.Paused = false
		a.active = screenOrbit
		return a, nil

	case screens.NodeClearAllMsg:
		// v0.8.6+: clear-all from the maneuver form. Replaces the
		// retired N global keybinding.
		a.world.ClearNodes()
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
			// v0.7.4+: title-bar [Menu] / [Missions] buttons take
			// priority over canvas / HUD hits, since they sit at
			// row 0 above the body region.
			if a.orbitView.HitMenuButton(m.X, m.Y) {
				a.menu.Reset()
				a.active = screenMenu
				return a, nil
			}
			if a.orbitView.HitMissionsButton(m.X, m.Y) {
				a.active = screenMissions
				return a, nil
			}
			// Framed navball panel is opaque and drawn over the
			// canvas, so its control hits take priority over the
			// canvas / body hits underneath. v0.9.6-polish.
			if ctrl, ok := a.orbitView.HitNavballControl(m.X, m.Y); ok {
				a.dispatchNavballControl(ctrl)
				return a, nil
			}
			hit := a.orbitView.HitAt(m.X, m.Y)
			switch {
			case hit.IsVessel:
				if a.world.CraftVisibleHere() {
					a.world.Focus = sim.Focus{Kind: sim.FocusCraft}
				}
			case hit.NodeIdx > 0:
				idx := hit.NodeIdx - 1 // tags are 1-indexed; slice is 0-indexed
				if idx >= 0 && idx < len(a.world.ActiveCraft().Nodes) {
					a.maneuver.LoadNode(idx, a.world.ActiveCraft().Nodes[idx])
					a.bindManeuverTarget()
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
					a.bindManeuverTarget()
					a.world.Clock.Paused = true
					a.active = screenManeuver
				}
			case a.hudNodeHit(m.X, m.Y):
				// Handled inside hudNodeHit — opens the maneuver
				// planner pre-loaded for the clicked node and (in
				// multi-craft) switches active craft to its owner.
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
		case screenMenu:
			action := a.menu.HandleClick(m.X, m.Y)
			if action != screens.MenuActionNone {
				return a.applyMenuAction(action)
			}
		case screenMissions:
			if a.missions.HitBackButton(m.X, m.Y) {
				a.active = screenOrbit
				return a, nil
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
			action := a.menu.HandleKey(m.String())
			if action != screens.MenuActionNone {
				return a.applyMenuAction(action)
			}
			return a, nil
		}
		// v0.8.2+: spawn-craft form. Enter spawns the selected
		// loadout; Esc cancels back to orbit.
		if a.active == screenSpawn {
			action := a.spawn.HandleKey(m.String())
			switch action {
			case screens.SpawnActionConfirm:
				// v0.10.1+: Custom selected but no stages assembled is
				// not a spawnable craft — flash and keep the form open
				// so the player can add parts instead of silently
				// getting a round-robin default.
				if a.spawn.CustomStackEmpty() {
					a.statusMsg = "custom stack is empty — add a part with [a]"
					a.statusExpires = time.Now().Add(3 * time.Second)
					return a, nil
				}
				spec := sim.SpawnSpec{
					LoadoutID:       a.spawn.SelectedLoadoutID(),
					CustomStages:    a.spawn.SelectedCustomStages(),
					ParentBodyID:    a.spawn.SelectedParentID(),
					AltitudeM:       a.spawn.SelectedAltitudeM(),
					Retrograde:      a.spawn.SelectedRetrograde(),
					Alongside:       a.spawn.SelectedAlongside(),
					Launchpad:       a.spawn.SelectedLaunchpad(),
					Latitude:        a.spawn.SelectedLatitudeDeg(),
					LongitudeOffset: a.spawn.SelectedLongitudeEastDeg(),
				}
				if c, err := a.world.SpawnCraft(spec); err == nil {
					a.statusMsg = fmt.Sprintf("spawned craft %d (%s)", a.world.ActiveCraftIdx+1, c.Name)
					a.statusExpires = time.Now().Add(3 * time.Second)
				}
				a.active = screenOrbit
			case screens.SpawnActionCancel:
				a.active = screenOrbit
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
				a.menu.Reset()
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
				a.bindManeuverTarget()
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
		case key.Matches(m, a.keys.SpawnCraft):
			// v0.8.2+: open the spawn form. Player picks craft
			// type, parent body, altitude, and direction; Enter
			// spawns; Esc cancels. Default parent is whatever the
			// active craft currently orbits.
			defaultParentID := ""
			if c := a.world.ActiveCraft(); c != nil {
				defaultParentID = c.Primary.ID
			}
			a.spawn.Reset(a.world.System().Bodies, defaultParentID)
			a.active = screenSpawn
			return a, nil
		case key.Matches(m, a.keys.PlanTransfer):
			// v0.9.0+: H consumes World.Target instead of the implicit
			// body cursor. TargetCraft is the v0.9.3 rendezvous-tooling
			// surface and routes through `R` once that lands; here it
			// flashes a redirect rather than silently no-opping. None →
			// silent no-op (nothing aimed at).
			if a.world.CraftVisibleHere() {
				switch a.world.Target.Kind {
				case sim.TargetBody:
					_, _ = a.world.PlanTransfer(a.world.Target.BodyIdx)
					// v0.10.1: the auto-plant is a coplanar circular
					// solver; if the departure orbit is eccentric or
					// out of the target's plane it plants a silently-
					// off transfer. Surface a non-blocking advisory so
					// the result isn't a mystery (the node is still
					// planted — advisory, not a refusal).
					if warn := a.world.HohmannDepartureWarning(a.world.Target.BodyIdx); warn != "" {
						a.statusMsg = warn
						a.statusExpires = time.Now().Add(6 * time.Second)
					}
				case sim.TargetCraft:
					a.statusMsg = "H targets bodies — for craft, plan via [m]"
					a.statusExpires = time.Now().Add(3 * time.Second)
				}
			}
			return a, nil
		case key.Matches(m, a.keys.PlanIncl):
			if a.world.CraftVisibleHere() {
				// v0.9.0+: I consumes World.Target. TargetBody → plane-
				// match the body's orbit (existing v0.8.6 logic); None →
				// drop to equatorial of craft's primary (the equatorial
				// inclination match shipped with v0.7.4); TargetCraft is
				// deferred to v0.9.3 with the rendezvous-tooling slice.
				//
				// Pre-v0.9 this block read App.selectedBody, the implicit
				// body cursor driven by ←/→. selectedBody now drives only
				// body-info / porkchop / SELECTED HUD pane.
				target := 0.0 // default: drop to equatorial of craft's primary
				sys := a.world.System()
				switch a.world.Target.Kind {
				case sim.TargetBody:
					if a.world.Target.BodyIdx > 0 && a.world.Target.BodyIdx < len(sys.Bodies) {
						b := sys.Bodies[a.world.Target.BodyIdx]
						primary := a.world.ActiveCraft().Primary
						frame := orbital.ReferenceFrameForPrimary(primary)
						target = orbital.PlaneMatchInclination(b, frame)
					}
				case sim.TargetCraft:
					a.statusMsg = "I targets bodies — for craft, plan via [m]"
					a.statusExpires = time.Now().Add(3 * time.Second)
					return a, nil
				}
				plan, err := a.world.PlanInclinationChange(target)
				if err != nil {
					a.statusMsg = fmt.Sprintf("inclination: %v", err)
				} else {
					nodeLabel := "DN"
					if plan.AtAN {
						nodeLabel = "AN"
					}
					a.statusMsg = fmt.Sprintf("inclination plan — %.1f m/s at next %s",
						plan.DV, nodeLabel)
				}
				a.statusExpires = time.Now().Add(3 * time.Second)
			}
			return a, nil
		case key.Matches(m, a.keys.PlanCircularize):
			// v0.9.4+: `C` plants a prograde burn at next apoapsis sized
			// to circularise. Pairs with the LAUNCH HUD's ORBIT READY
			// callout — when the player sees ORBIT READY (apoapsis is
			// in space) they press `C`, coast to apoapsis, the planted
			// node fires, periapsis rises to match apoapsis, mission
			// passes. Mirrors v0.7.4's `I` planter shape.
			if a.world.CraftVisibleHere() {
				plan, err := a.world.PlanCircularizeAtApoapsis()
				if err != nil {
					a.statusMsg = fmt.Sprintf("circularize: %v", err)
				} else {
					a.statusMsg = fmt.Sprintf("circularize @ apoapsis (%.0f km) → +%.0f m/s prograde",
						plan.ApoAltM/1000, plan.DV)
				}
				a.statusExpires = time.Now().Add(3 * time.Second)
			}
			return a, nil
		case key.Matches(m, a.keys.PlanRendezvous):
			// v0.10.2+: `K` plants the recommended single-burn nudge
			// toward the current craft target. Reads from
			// world.RecommendedRendezvousBurn (Lambert intercept →
			// project onto velocity-frame axes → verify via
			// NextClosestApproach). Mirrors PlanCircularize shape; the
			// HUD's TARGET block already shows the advisory's
			// achievable-CA / Δv readouts when the gate passes.
			if a.world.CraftVisibleHere() {
				adv, err := a.world.PlanRendezvousNudge()
				if err != nil {
					a.statusMsg = fmt.Sprintf("rendezvous: %v", err)
				} else {
					a.statusMsg = fmt.Sprintf("rendezvous nudge: %.1f m/s %s → CA %.0f m @ T+%.0fs",
						adv.DV, adv.Axis, adv.AchievableCA, adv.TArrival)
				}
				a.statusExpires = time.Now().Add(3 * time.Second)
			}
			return a, nil
		case key.Matches(m, a.keys.Porkchop):
			if a.active == screenOrbit && a.world.CraftVisibleHere() && a.selectedBody > 0 {
				a.porkchop.Load(a.world, a.selectedBody)
				a.active = screenPorkchop
			}
			return a, nil
		// v0.8.6: ClearNodes global binding retired — clear-all now
		// lives in the maneuver-form footer (`m` then ctrl+k).
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
			a.handleAttitudeIntent(sim.IntentPrograde)
			return a, nil
		case key.Matches(m, a.keys.AttitudeRetrograde):
			a.handleAttitudeIntent(sim.IntentRetrograde)
			return a, nil
		case key.Matches(m, a.keys.AttitudeNormalPlus):
			a.handleAttitudeIntent(sim.IntentNormalPlus)
			return a, nil
		case key.Matches(m, a.keys.AttitudeNormalMinus):
			a.handleAttitudeIntent(sim.IntentNormalMinus)
			return a, nil
		case key.Matches(m, a.keys.AttitudeRadialOut):
			a.handleAttitudeIntent(sim.IntentRadialOut)
			return a, nil
		case key.Matches(m, a.keys.AttitudeRadialIn):
			a.handleAttitudeIntent(sim.IntentRadialIn)
			return a, nil
		case key.Matches(m, a.keys.ToggleBurn):
			a.world.ToggleManualBurn()
			return a, nil
		case key.Matches(m, a.keys.CycleEngine):
			a.world.CycleEngineMode()
			return a, nil
		case key.Matches(m, a.keys.NextCraft):
			a.world.CycleActiveCraft(1)
			return a, nil
		case key.Matches(m, a.keys.PrevCraft):
			a.world.CycleActiveCraft(-1)
			return a, nil
		case key.Matches(m, a.keys.Undock):
			if a.world.Undock(a.world.ActiveCraftIdx) {
				a.statusMsg = fmt.Sprintf("undocked into %d components", len(a.world.Crafts))
				a.statusExpires = time.Now().Add(3 * time.Second)
			}
			return a, nil
		case key.Matches(m, a.keys.CycleTarget):
			a.world.CycleTarget(true)
			return a, nil
		case key.Matches(m, a.keys.ClearTarget):
			a.world.ClearTarget()
			return a, nil
		case key.Matches(m, a.keys.CycleNavMode):
			nav := a.world.CycleNavMode()
			a.statusMsg = fmt.Sprintf("nav: %s", nav)
			a.statusExpires = time.Now().Add(2 * time.Second)
			return a, nil
		case key.Matches(m, a.keys.ToggleInstantSAS):
			a.world.ToggleInstantSAS()
			a.statusMsg = fmt.Sprintf("SAS: %s", sasModeLabel(a.world.InstantSAS))
			a.statusExpires = time.Now().Add(2 * time.Second)
			return a, nil
		case key.Matches(m, a.keys.AttitudeSurfacePrograde):
			a.handleAttitudeKey(spacecraft.BurnSurfacePrograde)
			return a, nil
		case key.Matches(m, a.keys.AttitudeSurfaceRetrograde):
			a.handleAttitudeKey(spacecraft.BurnSurfaceRetrograde)
			return a, nil
		case key.Matches(m, a.keys.PitchTrimEast):
			if c := a.world.ActiveCraft(); c != nil {
				c.PitchTrim += spacecraft.PitchTrimStepRad
			}
			return a, nil
		case key.Matches(m, a.keys.PitchTrimWest):
			if c := a.world.ActiveCraft(); c != nil {
				c.PitchTrim -= spacecraft.PitchTrimStepRad
			}
			return a, nil
		case key.Matches(m, a.keys.PitchTrimReset):
			if c := a.world.ActiveCraft(); c != nil {
				c.PitchTrim = 0
			}
			return a, nil
		case key.Matches(m, a.keys.Stage):
			// v0.9.1+: drop the bottom stage of the active craft.
			// Inside the maneuver form, space is the iterate-toggle
			// (v0.8.6.3) — that path doesn't reach here because the
			// maneuver form intercepts keys before app.update.
			_, jettIdx, err := a.world.StageActive(a.world.ActiveCraftIdx)
			switch {
			case err == nil:
				name := a.world.Crafts[jettIdx].Name
				a.statusMsg = fmt.Sprintf("staged: %s jettisoned", name)
			case errors.Is(err, sim.ErrStageOnlyOne):
				a.statusMsg = "stage: cannot drop the only remaining stage"
			default:
				a.statusMsg = fmt.Sprintf("stage failed: %v", err)
			}
			a.statusExpires = time.Now().Add(3 * time.Second)
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

// hudNodeHit checks whether a HUD click landed on a NODES-block
// entry; if so, switches active craft (when the clicked node lives
// on a different craft) and opens the maneuver planner pre-loaded
// for that node — same edit-replace UX as the canvas node-glyph
// click. Returns true when handled. v0.8.2.x.
func (a *App) hudNodeHit(x, y int) bool {
	craftIdx, nodeIdx, ok := a.orbitView.HitHudNode(x, y)
	if !ok {
		return false
	}
	if craftIdx < 0 || craftIdx >= len(a.world.Crafts) {
		return false
	}
	c := a.world.Crafts[craftIdx]
	if c == nil || nodeIdx < 0 || nodeIdx >= len(c.Nodes) {
		return false
	}
	// Switch active to the owning craft so the planner edits are
	// targeted correctly and the post-edit projected orbit reflects
	// the right craft.
	if craftIdx != a.world.ActiveCraftIdx {
		a.world.SetActiveCraftIdx(craftIdx)
		a.world.StopManualBurn()
	}
	a.maneuver.LoadNode(nodeIdx, c.Nodes[nodeIdx])
	a.world.Clock.Paused = true
	a.active = screenManeuver
	return true
}

// handleAttitudeKey dispatches a w/s/a/d/q/e tap. In EngineMain mode
// it sets the held attitude (the v0.7.3.2 explicit-engage UX stays —
// `b` actually fires the engine). In EngineRCS mode the same keypress
// fires one RCS pulse in the requested orbital-frame direction without
// touching the SAS hold — RCS is a 6-axis translation tool, so the
// nose stays put while the pulse nudges Δv. A held key produces a
// sustained pulse train at the terminal's key-repeat rate.
// v0.8.0+.
func (a *App) handleAttitudeKey(mode spacecraft.BurnMode) {
	if a.world.ActiveCraft().EngineMode == spacecraft.EngineRCS {
		a.world.FireRCSPulse(mode)
		return
	}
	a.world.SetAttitudeMode(mode)
}

// handleAttitudeIntent translates the player's SAS-axis input through
// the active NavMode (KSP-style nav-ball mode cycle) before dispatching.
// In NavOrbit (default) the intent maps 1:1 to the v0.7.3+ orbit-frame
// burn modes; NavSurface rebinds prograde / retrograde to the rotating-
// atmosphere frame; NavTarget rebinds prograde / retrograde to relative-
// velocity, and radial± to BurnTarget / BurnAntiTarget (toward / away
// from the bound craft target). v0.9.3+.
func (a *App) handleAttitudeIntent(intent sim.AttitudeIntent) {
	a.handleAttitudeKey(a.world.ResolveAttitudeIntent(intent))
}

// sasModeLabel maps the World.InstantSAS opt-out to the player-facing
// manual-flight attitude-model name. MANUAL = rate-limited slew (the
// v0.10.0 default, instantSAS=false); AUTO = legacy instantaneous
// "magic SAS" snap (instantSAS=true). The same vocabulary is used by
// the navball [SAS] tag so the toast and the indicator never drift.
// v0.10.0+.
func sasModeLabel(instantSAS bool) string {
	if instantSAS {
		return "AUTO (instant)"
	}
	return "MANUAL (slew)"
}

// dispatchNavballControl routes a click on the framed navball
// panel's controls to the same world actions as the keyboard: the
// [MODE] button cycles NavMode (mirroring the CycleNavMode key,
// status toast included) and each axis button drives the matching
// SAS-hold intent through handleAttitudeIntent — so a click holds
// prograde / normal± / radial± exactly as w/s/a/d/q/e would, with
// NavMode rebinding applied. v0.9.6-polish.
func (a *App) dispatchNavballControl(ctrl screens.NavballControlID) {
	switch ctrl {
	case screens.NavballControlMode:
		nav := a.world.CycleNavMode()
		a.statusMsg = fmt.Sprintf("nav: %s", nav)
		a.statusExpires = time.Now().Add(2 * time.Second)
	case screens.NavballControlPrograde:
		a.handleAttitudeIntent(sim.IntentPrograde)
	case screens.NavballControlRetrograde:
		a.handleAttitudeIntent(sim.IntentRetrograde)
	case screens.NavballControlNormalPlus:
		a.handleAttitudeIntent(sim.IntentNormalPlus)
	case screens.NavballControlNormalMinus:
		a.handleAttitudeIntent(sim.IntentNormalMinus)
	case screens.NavballControlRadialOut:
		a.handleAttitudeIntent(sim.IntentRadialOut)
	case screens.NavballControlRadialIn:
		a.handleAttitudeIntent(sim.IntentRadialIn)
	case screens.NavballControlRCS:
		a.world.CycleEngineMode()
		state := "off"
		if a.world.RCSActive() {
			state = "on"
		}
		a.statusMsg = fmt.Sprintf("RCS: %s", state)
		a.statusExpires = time.Now().Add(2 * time.Second)
	case screens.NavballControlSAS:
		a.world.ToggleInstantSAS()
		a.statusMsg = fmt.Sprintf("SAS: %s", sasModeLabel(a.world.InstantSAS))
		a.statusExpires = time.Now().Add(2 * time.Second)
	case screens.NavballControlTargetPlus:
		a.handleAttitudeKey(spacecraft.BurnTarget)
	case screens.NavballControlTargetMinus:
		a.handleAttitudeKey(spacecraft.BurnAntiTarget)
	}
}

// applyMenuAction dispatches a finalised MenuAction (Save / Load /
// Quit / Cancel) regardless of whether the player drove it through
// the keyboard or the click flow. Pulled out of the legacy inline
// switch in v0.7.4 so HandleClick and HandleKey share the same
// post-confirm side-effects (status flash, screen exit, autosave +
// quit).
func (a *App) applyMenuAction(action screens.MenuAction) (tea.Model, tea.Cmd) {
	switch action {
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
		base = a.menu.Render(a.width)
	case screenSpawn:
		base = a.spawn.Render(a.width)
	case screenMissions:
		base = a.missions.Render(a.world, a.width)
	default:
		base = a.orbitView.Render(a.world, a.selectedBody, a.width, a.height)
	}
	// v0.8.1+: overlay the status message on top of an existing row
	// rather than appending a new line. Appending grew the rendered
	// height by one row and pushed the terminal to scroll the view
	// every time the message expired / re-fired. The orbit screen's
	// footer (last row) is the natural target — short-lived status
	// lines are flight-state messages and live on the same band as
	// the keybind hints.
	if a.statusMsg != "" && time.Now().Before(a.statusExpires) {
		base = overlayLastLine(base, a.theme.Warning.Render(a.statusMsg))
	}
	return base
}

// overlayLastLine replaces the final \n-delimited row of base with
// overlay, preserving the rendered height. v0.8.1+: used by the
// status-message flash to avoid growing the screen.
func overlayLastLine(base, overlay string) string {
	idx := strings.LastIndex(base, "\n")
	if idx < 0 {
		return overlay
	}
	return base[:idx+1] + overlay
}

// bindManeuverTarget hands the current World.Target binding to the
// maneuver form so the four target-relative burn modes and the
// TriggerNextClosestApproach event are pickable + correctly captured
// at plant. Bound at form-open time (not per-keypress), so a target
// switch while the form is open doesn't silently retarget a planted
// burn — the player closes + reopens to retarget. v0.9.3+.
func (a *App) bindManeuverTarget() {
	if a.world.Target.Kind == sim.TargetCraft {
		a.maneuver.SetTargetCraft(true, a.world.Target.CraftIdx)
		return
	}
	a.maneuver.SetTargetCraft(false, 0)
}
