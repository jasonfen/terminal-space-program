package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/settings"
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
	screenSpawn    // v0.8.2+: craft-type pick form on `n`.
	screenSettings // v0.13 slice 3: per-Chip visibility toggles, reached from the menu.
	screenControls // ADR 0022: keyboard-layout selector, reached from the menu.
	screenVAB      // v0.24 / ADR 0029: Vehicle Assembly (VAB) builder, reached from the menu.
	screenBoss     // boss key: full-screen fake developer shell (backtick from any screen).
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

	orbitView  *screens.OrbitView
	launchView *screens.LaunchView
	bodyInfo   *screens.BodyInfo
	help       *screens.Help
	maneuver   *screens.Maneuver
	porkchop   *screens.Porkchop
	menu       *screens.Menu
	missions   *screens.Missions
	spawn      *screens.SpawnCraft

	// settingsScreen is the v0.13 per-Chip visibility toggle screen. Its
	// edits write through to orbitView's settings.Settings and persist to
	// settings.json immediately (see toggleChip).
	settingsScreen *screens.SettingsScreen

	// controls is the keyboard-layout selector screen (ADR 0022); cycling
	// it updates `layout` and persists via cycleLayout. layout is the active
	// keyboard layout, applied to every keypress by normalizeKey before
	// binding-matching and inverted for the F1 help labels.
	controls *screens.ControlsScreen
	layout   keylayout.Layout

	// vab is the Vehicle Assembly builder (v0.24 / ADR 0029), reached from
	// the pause menu. It owns the designs-store interaction (save / load /
	// delete) directly — designs are app-managed catalog data, not world
	// state — so opening it from the menu is the only wiring the App needs.
	vab *screens.VAB

	// boss is the backtick "boss key" fake shell. bossReturnScreen records
	// the screen that was active when it opened, and bossPrevPaused records
	// the sim pause state at that moment; exit restores both so the player
	// lands back exactly where they left, with the clock as it was.
	boss             *screens.BossShell
	bossReturnScreen screenID
	bossPrevPaused   bool

	// statusMsg flashes a one-line notice in the HUD footer for ~3
	// seconds after save / load. Cleared by clearStatusAfter via a
	// scheduled tea.Cmd.
	statusMsg     string
	statusExpires time.Time

	// endFlightConfirm (v0.11.4+, ADR 0004) gates the [E] end-flight
	// removal behind a y/n confirm. When true, the orbit screen
	// renders a footer prompt and the next y/Y commits the removal;
	// n/N/Esc cancels. Session-only state — not persisted (the
	// confirmation has no meaning across a save / load boundary).
	endFlightConfirm bool
}

// New builds a root App. Returns an error if systems can't load. When
// scenario is non-nil, the fresh world's default LEO seed is replaced per
// the command-line start scenario (system / body / orbit / launch site);
// an unknown system / body / loadout there is surfaced as the error. Pass
// nil for the standard default start. v0.17.
func New(scenario *sim.StartScenario) (*App, error) {
	w, err := sim.NewWorld()
	if err != nil {
		return nil, err
	}
	if scenario != nil {
		// A custom start replaces the seed craft; the Earth-Moon lunar
		// transfer-window adjust below is irrelevant (and wrong for a
		// non-Earth / non-Sol start), so skip it.
		if err := w.ApplyStartScenario(*scenario); err != nil {
			return nil, err
		}
	} else {
		// Open a fresh game a few hours before the next ideal Moon-transfer
		// window instead of the ~10 days out the J2000 epoch yields. A false
		// return just keeps the J2000 start — never fatal.
		w.AdjustStartForLunarTransferWindow(sim.DefaultLunarTransferLead)
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
	orbitView := screens.NewOrbitView(sth)
	// Per-Chip visibility preferences (ADR 0010). A missing settings.json
	// yields all-on defaults, preserving pre-0010 behaviour; parse/IO
	// warnings were already surfaced by main before bubbletea took the
	// screen, so they're dropped here on the rehydrating load.
	prefs, _ := settings.Load()
	orbitView.SetSettings(prefs)
	// Gate the seeded mission programs by the persisted toggles (both default
	// off — a fresh sandbox shows no missions until opted in). v0.21 Slice 7.
	w.SetEnabledMissionPrograms(enabledProgramsFromSettings(prefs))
	return &App{
		world:      w,
		theme:      th,
		keys:       DefaultKeymap(),
		layout:     keylayout.Resolve(prefs.KeyboardLayout),
		active:     screenOrbit,
		orbitView:  orbitView,
		launchView: screens.NewLaunchView(sth, orbitView),
		bodyInfo:   screens.NewBodyInfo(sth),
		help:       screens.NewHelp(sth),
		maneuver:   screens.NewManeuver(sth),
		porkchop:   screens.NewPorkchop(sth),
		menu:       screens.NewMenu(sth),
		missions:   screens.NewMissions(sth),
		spawn:      screens.NewSpawnCraft(sth),

		settingsScreen: screens.NewSettingsScreen(sth),
		controls:       screens.NewControlsScreen(sth),
		vab:            screens.NewVAB(sth),
		boss:           screens.NewBossShell(sth),
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
		// v0.11.0+: ViewLaunch switch-end release toast (ADR 0021 D
		// retired the apoapsis-floor auto-release). Same flash
		// surface as docking; cleared after one fire.
		if e := a.world.LastLaunchReleaseEvent; e != nil {
			a.statusMsg = fmt.Sprintf("ORBIT READY — returning to %s", e.PrevView)
			a.statusExpires = time.Now().Add(4 * time.Second)
			a.world.LastLaunchReleaseEvent = nil
		}
		return a, sim.TickCmd(a.world.Clock.BaseStep)

	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.orbitView.Resize(m.Width, m.Height)
		a.launchView.Resize(m.Width, m.Height)
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
			//
			// v0.16 / ADR 0016: carry the edited node's stable ID across
			// the re-plant so an engaged Auto-Warp target (frozen by node
			// identity) keeps following the burn through an edit instead of
			// silently disengaging. editedID stays 0 for a brand-new node,
			// and stampNodeID then mints a fresh ID inside PlanNode.
			var editedID uint64
			if m.EditingIdx >= 0 && m.EditingIdx < len(a.world.ActiveCraft().Nodes) {
				editedID = a.world.ActiveCraft().Nodes[m.EditingIdx].ID
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
					ID:            editedID,
					TriggerTime:   m.TriggerTime,
					Mode:          m.Mode,
					DV:            m.DV,
					Duration:      dur,
					Event:         m.Event,
					Throttle:      m.Throttle,
					TargetCraftID: m.TargetCraftID,
				})
			case m.Event != sim.TriggerAbsolute:
				// v0.6.0: event-relative nodes go through PlanNode so
				// the resolver can freeze TriggerTime against the live
				// orbit on the next Tick.
				a.world.PlanNode(sim.ManeuverNode{
					ID:            editedID,
					Mode:          m.Mode,
					DV:            m.DV,
					Duration:      dur,
					Event:         m.Event,
					Throttle:      m.Throttle,
					TargetCraftID: m.TargetCraftID,
				})
			case dur == 0:
				// v0.9.3+: target-relative impulsive needs the bound
				// target snapshot for direction resolution. Resolve by
				// stable ID (ADR 0012).
				if m.TargetCraftID != 0 {
					if tc, _, ok := a.world.CraftByID(m.TargetCraftID); ok && tc.Primary.ID == a.world.ActiveCraft().Primary.ID {
						a.world.ActiveCraft().ApplyImpulsiveWithTarget(m.Mode, m.DV, tc.State.R, tc.State.V)
						break
					}
				}
				a.world.ActiveCraft().ApplyImpulsive(m.Mode, m.DV)
			default:
				effThrottle := m.Throttle
				if effThrottle <= 0 {
					effThrottle = 1.0
				}
				a.world.ActiveCraft().ActiveBurn = &sim.ActiveBurn{
					Mode:          m.Mode,
					DVRemaining:   m.DV,
					EndTime:       a.world.Clock.SimTime.Add(dur),
					Throttle:      effThrottle,
					TargetCraftID: m.TargetCraftID,
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
		a.world.RecordAction(missions.ActionClearNodes) // ADR 0025 §7
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
			// v0.16 / ADR 0016: the [»Burn] button toggles Auto-Warp
			// (click-equivalent of `G`). A no-op when no burn is eligible.
			if a.orbitView.HitBurnButton(m.X, m.Y) {
				a.world.ToggleAutoWarp()
				return a, nil
			}
			// Framed navball panel is opaque and drawn over the
			// canvas, so its control hits take priority over the
			// canvas / body hits underneath. v0.9.6-polish.
			if ctrl, ok := a.orbitView.HitNavballControl(m.X, m.Y); ok {
				a.dispatchNavballControl(ctrl)
				return a, nil
			}
			// Chips are opaque overlays drawn over the canvas corners
			// (ADR 0010), so a chip hit takes priority over the canvas /
			// body hits underneath. The Nodes chip opens the maneuver
			// screen — the canonical full, editable node list ([m]);
			// other chips are display-only and just swallow the click so
			// it doesn't fall through to a body behind them.
			if id, ok := a.orbitView.HitChip(m.X, m.Y); ok {
				if id == settings.ChipNodes {
					a.world.Clock.Paused = true
					a.active = screenManeuver
				}
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
		case screenSettings:
			action, chip := a.settingsScreen.HandleClick(m.X, m.Y)
			switch action {
			case screens.SettingsActionToggle:
				a.toggleChip(chip)
			case screens.SettingsActionToggleTutorial:
				a.toggleMissionProgram(true)
			case screens.SettingsActionToggleChallenges:
				a.toggleMissionProgram(false)
			case screens.SettingsActionCancel:
				a.active = screenOrbit
			}
		case screenControls:
			switch a.controls.HandleClick(m.X, m.Y) {
			case screens.ControlsActionCycleLayout:
				a.cycleLayout()
			case screens.ControlsActionCancel:
				a.active = screenOrbit
			}
		}
		return a, nil

	case tea.KeyMsg:
		// Keyboard-layout normalization (ADR 0022): translate the keypress
		// from the player's layout back to its QWERTY position before any
		// matching, so the Keymap and every raw-string handler stay QWERTY.
		// Skipped for the boss shell, which consumes literal typed text — a
		// QWERTZ player typing in the fake shell wants their real keycaps.
		if a.active != screenBoss {
			m = normalizeKey(a.layout, m)
		}
		// ctrl+c bypasses everything else (standard interrupt
		// convention). Honored from any screen.
		if key.Matches(m, a.keys.Quit) {
			a.autosave()
			return a, tea.Quit
		}
		// Boss key: a single global keypress (backtick) swaps the whole
		// screen to a fake developer shell, and swaps back on
		// exit/logout/ctrl+d. This block sits ABOVE every per-screen block
		// and above endFlightConfirm so it fires from ANY screen and so the
		// shell's own keystrokes never leak into game handlers — while the
		// shell is active we return early for EVERY key. Checking
		// screenBoss first also means a backtick typed inside the shell is
		// consumed as text by HandleKey rather than re-triggering the open.
		if a.active == screenBoss {
			if a.boss.HandleKey(m.String()) == screens.BossActionExit {
				a.active = a.bossReturnScreen
				a.world.Clock.Paused = a.bossPrevPaused
			}
			return a, nil
		}
		if key.Matches(m, a.keys.BossKey) {
			a.bossReturnScreen = a.active
			a.bossPrevPaused = a.world.Clock.Paused
			a.world.Clock.Paused = true // freeze the sim while "away"
			a.boss.Reset()              // fresh lived-in session each open
			a.active = screenBoss
			return a, nil
		}
		// v0.11.4+ (ADR 0004): end-flight y/n confirm intercept. When
		// the [E] prompt is open, y/Y commits the removal and n/N/Esc
		// cancels; every other key is swallowed so attitude / warp
		// inputs don't slip through mid-confirm. Honored from any
		// screen for the same reason ctrl+c is — accidental escape
		// from a confirm prompt would be the wrong answer to "are
		// you sure?"
		if a.endFlightConfirm {
			s := m.String()
			switch s {
			case "y", "Y":
				if a.world.EndFlightActive() {
					a.statusMsg = "● END FLIGHT — wreckage removed"
					a.statusExpires = time.Now().Add(3 * time.Second)
				}
				a.endFlightConfirm = false
			case "n", "N", "esc":
				a.endFlightConfirm = false
			}
			return a, nil
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
					DesignID:        a.spawn.SelectedDesignID(),
					CustomStages:    a.spawn.SelectedCustomStages(),
					NosePayloadPlan: a.spawn.SelectedNosePayloadPlan(),
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
				} else {
					// Surface the failure instead of silently returning to
					// orbit — a swallowed error reads as "nothing happens".
					a.statusMsg = fmt.Sprintf("spawn failed: %v", err)
					a.statusExpires = time.Now().Add(3 * time.Second)
				}
				a.active = screenOrbit
			case screens.SpawnActionCancel:
				a.active = screenOrbit
			}
			return a, nil
		}
		// v0.13 slice 3: Settings screen. Up/down move the cursor,
		// space/enter toggles the highlighted Chip (write-through +
		// persist via toggleChip), Esc backs out to orbit. Handled here
		// (like screenSpawn) so its navigation keys don't fall through to
		// the orbit keymap.
		if a.active == screenSettings {
			action, chip := a.settingsScreen.HandleKey(m.String())
			switch action {
			case screens.SettingsActionToggle:
				a.toggleChip(chip)
			case screens.SettingsActionToggleTutorial:
				a.toggleMissionProgram(true)
			case screens.SettingsActionToggleChallenges:
				a.toggleMissionProgram(false)
			case screens.SettingsActionCancel:
				a.active = screenOrbit
			}
			return a, nil
		}
		// Controls screen (ADR 0022): the layout selector. Space/enter/←/→
		// cycle the keyboard layout (persist via cycleLayout), esc backs out.
		// Handled here so its keys don't fall through to the orbit keymap.
		if a.active == screenControls {
			switch a.controls.HandleKey(m.String()) {
			case screens.ControlsActionCycleLayout:
				a.cycleLayout()
			case screens.ControlsActionCancel:
				a.active = screenOrbit
			}
			return a, nil
		}
		// VAB (v0.24 / ADR 0029): the Vehicle Assembly builder owns its own
		// keymap (palette / stack / save / load); esc backs out to orbit.
		// Handled here so its keys don't fall through to the orbit flight
		// controls.
		if a.active == screenVAB {
			if a.vab.HandleKey(m.String()) == screens.VABActionCancel {
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
		// Help: F1/esc close; every other key scrolls the overlay. Sits
		// before the global switch so scroll keys (↑/↓, PgUp/PgDn, etc.)
		// don't fall through to flight actions. Backtick/ctrl+c/end-flight
		// are handled above this block, so the boss key etc. still work.
		if a.active == screenHelp {
			if key.Matches(m, a.keys.Help) || key.Matches(m, a.keys.Back) {
				a.active = screenOrbit
				return a, nil
			}
			a.help.HandleKey(m)
			return a, nil
		}
		// Porkchop: ←/→/↑/↓ navigate cells, Esc returns.
		if a.active == screenPorkchop {
			_, done := a.porkchop.HandleKey(m)
			if done {
				if tgt, depD, tofD, opts, ok := a.porkchop.PendingPlant(); ok {
					_, _ = a.world.PlanTransferAt(tgt, depD, tofD, opts)
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
				a.help.ResetScroll() // always open at the top
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
				a.world.RecordAction(missions.ActionOpenManeuver) // ADR 0025 §7
			}
			return a, nil
		case key.Matches(m, a.keys.Missions):
			// `M` opens the mission ladder (ADR 0025 Slice 5) — the same
			// screen as the title-bar [Missions] button. No RecordAction: the
			// curated event vocabulary excludes pure-navigation bindings.
			if a.active == screenOrbit {
				a.active = screenMissions
			}
			return a, nil
		case key.Matches(m, a.keys.WarpUp):
			// Manual warp cancels Auto-Warp, then applies (ADR 0016) —
			// Disengage leaves Selected Warp untouched so the step lands
			// from the player's own rate.
			a.world.DisengageAutoWarp()
			a.world.Clock.WarpUp()
			return a, nil
		case key.Matches(m, a.keys.WarpDown):
			a.world.DisengageAutoWarp()
			a.world.Clock.WarpDown()
			return a, nil
		case key.Matches(m, a.keys.AutoWarp):
			// Toggle Auto-Warp to the globally-soonest burn. A no-op when
			// no burn is eligible (engage returns false silently).
			a.world.ToggleAutoWarp()
			a.world.RecordAction(missions.ActionAutoWarp) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.CancelWarp):
			// Drop straight to 1× from any warp state: cancel Auto-Warp
			// and reset Selected Warp to the 1× floor (WarpIdx 0). Pause
			// state is left as-is — this stops accelerating time, it
			// doesn't resume a paused clock.
			a.world.DisengageAutoWarp()
			a.world.Clock.WarpIdx = 0
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
			if a.world.ViewMode == sim.ViewLaunch {
				a.world.NudgeLaunchZoom(+1, a.launchView.CurrentScale(a.world))
			} else {
				a.orbitView.ZoomIn()
			}
			return a, nil
		case key.Matches(m, a.keys.ZoomOut):
			if a.world.ViewMode == sim.ViewLaunch {
				a.world.NudgeLaunchZoom(-1, a.launchView.CurrentScale(a.world))
			} else {
				a.orbitView.ZoomOut()
			}
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
			// v0.24 / ADR 0029: offer saved VAB designs alongside catalog
			// loadouts. ListDesigns is read here (not in the form) so the
			// form stays filesystem-free and testable.
			designs, _ := spacecraft.ListDesigns()
			// ADR 0031 / S10: pass the active System's Scale Class so the form
			// filters the catalog to system-matching craft (sys is a local so
			// the pointer-receiver Scale() is callable).
			sys := a.world.System()
			a.spawn.Reset(sys.Bodies, defaultParentID, designs, sys.Scale())
			a.active = screenSpawn
			a.world.RecordAction(missions.ActionSpawnCraft) // ADR 0025 §7
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
					a.world.RecordAction(missions.ActionPlanTransfer) // ADR 0025 §7
					// v0.12.x (ADR 0005): the intra-primary auto-plant is
					// now a plane-aware dual-strategy solver (combined
					// fused-Lambert vs split raise + apoapsis plane change)
					// that plants the cheaper — so flash both candidate Δv
					// totals and which was planted (supersedes the retired
					// "match plane [I], circularize, then [H]" advisory).
					// Non-intra-primary plants leave the comparison empty.
					if cmp := a.world.LastTransfer.Format(); cmp != "" {
						a.statusMsg = cmp
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
				// v0.9.0+: I consumes World.Target. TargetBody → full
				// plane match to the body's orbit (v0.10.4: matches
				// inclination AND the node line, so a following Hohmann
				// departs coplanar); None → drop to the equatorial plane
				// of the craft's primary (the equatorial inclination
				// match shipped with v0.7.4); TargetCraft is deferred.
				//
				// Pre-v0.9 this block read App.selectedBody, the implicit
				// body cursor driven by ←/→. selectedBody now drives only
				// body-info / porkchop / SELECTED HUD pane.
				var plan *planner.InclinationPlan
				var err error
				switch a.world.Target.Kind {
				case sim.TargetBody:
					plan, err = a.world.PlanPlaneMatch(a.world.Target.BodyIdx)
				case sim.TargetCraft:
					a.statusMsg = "I targets bodies — for craft, plan via [m]"
					a.statusExpires = time.Now().Add(3 * time.Second)
					return a, nil
				default:
					plan, err = a.world.PlanInclinationChange(0)
				}
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
				a.world.RecordAction(missions.ActionPlanIncl) // ADR 0025 §7
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
				a.world.RecordAction(missions.ActionPlanCircularize) // ADR 0025 §7
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
				a.world.RecordAction(missions.ActionPlanRendezvous) // ADR 0025 §7
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
			a.world.RecordAction(missions.ActionCycleView) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.Declutter):
			// v0.13+ (ADR 0010): toggle the momentary "hide all overlays"
			// view. Transient + unsaved — it flips the OrbitView's
			// declutter flag, which the chip render rule and navball
			// compositing honour; the slim HUD column is never hidden.
			// The launch screen shares this OrbitView, so it declutters
			// in step.
			a.orbitView.SetDeclutter(!a.orbitView.Declutter())
			return a, nil
		case key.Matches(m, a.keys.JumpToLaunchView):
			// v0.11.4+ (ADR 0004): manual jump to ViewLaunch focused
			// on the active vessel — skips the lowercase `v` cycle.
			// No-op without an active vessel; the launch view's own
			// "no active vessel" render path covers the empty-slate
			// case if the player jumps after end-flight clears the
			// slate (sub-scope 5).
			a.world.SetViewModeLaunch()
			return a, nil
		case key.Matches(m, a.keys.EndFlight):
			// v0.11.4+ (ADR 0004): open the end-flight confirm prompt
			// when the active vessel is Crashed. The y/n intercept at
			// the top of the KeyMsg branch handles commit / cancel
			// once the prompt is open; this case is the open trigger.
			// Silently ignored on a live vessel — the action is only
			// meaningful on wreckage.
			if c := a.world.ActiveCraft(); c != nil && c.Crashed {
				a.endFlightConfirm = true
			}
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
				a.world.RecordAction(missions.ActionRefinePlan) // ADR 0025 §7
			}
			return a, nil

		// v0.7.3+ manual flight controls. v0.7.3.2 split the engage
		// path off from the attitude keys: tapping w/s/a/d/q/e
		// orients only — actually firing the engine requires `b`.
		// Pre-fix the attitude keys auto-started the burn, which
		// was easy to trigger by accident.
		case key.Matches(m, a.keys.ThrottleFull):
			a.world.SetThrottle(1.0)
			a.world.RecordAction(missions.ActionThrottleFull) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.ThrottleCut):
			a.world.SetThrottle(0)
			a.world.RecordAction(missions.ActionThrottleCut) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.ThrottleUp):
			a.world.AdjustThrottle(0.1)
			a.world.RecordAction(missions.ActionThrottleUp) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.ThrottleDown):
			a.world.AdjustThrottle(-0.1)
			a.world.RecordAction(missions.ActionThrottleDown) // ADR 0025 §7
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
			a.world.RecordAction(missions.ActionToggleBurn) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.CycleEngine):
			a.world.CycleEngineMode()
			return a, nil
		case key.Matches(m, a.keys.CycleRCSScale):
			a.world.CycleRCSPulseScale()
			if c := a.world.ActiveCraft(); c != nil {
				if c.EngineMode == spacecraft.EngineRCS {
					a.statusMsg = fmt.Sprintf("rcs pulse %g m/s", c.RCSPulseDV())
				} else {
					a.statusMsg = fmt.Sprintf("rcs pulse %g m/s (press r for rcs)", c.RCSPulseDV())
				}
				a.statusExpires = time.Now().Add(3 * time.Second)
			}
			return a, nil
		case key.Matches(m, a.keys.NextCraft):
			a.world.CycleActiveCraft(1)
			return a, nil
		case key.Matches(m, a.keys.PrevCraft):
			a.world.CycleActiveCraft(-1)
			return a, nil
		case key.Matches(m, a.keys.CraftSlot):
			// v0.12.0+: number-row 1..9 jumps to craft index 0..8.
			// The binding only matches single digits '1'..'9', so the
			// first byte of the key string is the digit; no-op when no
			// craft occupies that slot (SwitchToCraftIdx bounds-checks).
			a.world.SwitchToCraftIdx(int(m.String()[0]-'0') - 1)
			return a, nil
		case key.Matches(m, a.keys.Undock):
			if a.world.Undock(a.world.ActiveCraftIdx) {
				a.statusMsg = fmt.Sprintf("undocked into %d components", len(a.world.Crafts))
				a.statusExpires = time.Now().Add(3 * time.Second)
				a.world.RecordAction(missions.ActionUndock) // ADR 0025 §7
			}
			return a, nil
		case key.Matches(m, a.keys.Deploy):
			// v0.23 / ADR 0028: release the top carried payload, keep flying the
			// carrier (drop-and-continue). World.Deploy self-records the deploy
			// action (so any caller emits it) — no second RecordAction here.
			if a.world.Deploy(a.world.ActiveCraftIdx) {
				a.statusMsg = fmt.Sprintf("deployed payload — %d craft on the slate", len(a.world.Crafts))
			} else {
				a.statusMsg = "deploy: no payload to release (carrier carries no docked payload)"
			}
			a.statusExpires = time.Now().Add(3 * time.Second)
			return a, nil
		case key.Matches(m, a.keys.Transpose):
			// v0.12 / ADR 0009: one-shot Apollo transposition. Reorders
			// the [Descent, Ascent, SM, CM] stack so the SM fires (LOI/
			// TEI) with the LM as a docked nose payload released via U.
			switch err := a.world.Transpose(a.world.ActiveCraftIdx); {
			case err == nil:
				a.world.RecordAction(missions.ActionTranspose) // ADR 0025 §7
				a.statusMsg = "transposed: SM is firing core — press U to release the LM"
			case errors.Is(err, sim.ErrTransposeNotReady):
				a.statusMsg = "transpose: drop the launch vehicle first (stack must be Descent/Ascent/SM/CM)"
			default:
				a.statusMsg = fmt.Sprintf("transpose failed: %v", err)
			}
			a.statusExpires = time.Now().Add(3 * time.Second)
			return a, nil
		case key.Matches(m, a.keys.CycleTarget):
			a.world.CycleTarget(true)
			a.world.RecordAction(missions.ActionCycleTarget) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.ClearTarget):
			a.world.ClearTarget()
			a.world.RecordAction(missions.ActionClearTarget) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.CycleNavMode):
			nav := a.world.CycleNavMode()
			a.world.RecordAction(missions.ActionCycleNavMode) // ADR 0025 §7
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
				a.world.RecordAction(missions.ActionStage) // ADR 0025 §7
				name := a.world.Crafts[jettIdx].Name
				a.statusMsg = fmt.Sprintf("staged: %s jettisoned", name)
				// v0.12 / ADR 0009: surface the transposition hint the
				// moment a stage drop leaves the Apollo stack in the
				// pre-transposition shape [Descent, Ascent, SM, CM] — the
				// player needs to know `D` flips the SM to the firing core
				// (or stage once more to drop the LM for a manual flip).
				// Without this the wrong-engine state is silent.
				if sim.TransposeReady(a.world.ActiveCraft()) {
					a.statusMsg = "TRANSPOSE READY — press D to flip (SM → firing core; LM becomes nose payload)"
				}
			case errors.Is(err, sim.ErrStageOnlyOne):
				// v0.12 Slice 3 (ADR 0008): once the vessel is reduced to
				// its bare chute-bearing stage, the staging-no-op press
				// arms the parachute instead — "just another staging
				// action," no new keybinding. Falls through to the normal
				// no-op flash when there's no stowed chute to arm.
				if c := a.world.ActiveCraft(); c != nil && c.ArmParachute() {
					a.statusMsg = "parachute ARMED — auto-deploys in atmosphere"
				} else {
					a.statusMsg = "stage: cannot drop the only remaining stage"
				}
			default:
				a.statusMsg = fmt.Sprintf("stage failed: %v", err)
			}
			a.statusExpires = time.Now().Add(3 * time.Second)
			return a, nil
		case key.Matches(m, a.keys.TiltUp), key.Matches(m, a.keys.TiltDown):
			// v0.10.6+: nudge ViewTilted's polar tilt θ ±5°. No-op when
			// the active projection isn't ViewTilted — keep the binding
			// silent on cardinals / orbit-flat so a stray shift+arrow
			// while in ViewTop doesn't blast a misleading flash.
			if a.world.ViewMode != sim.ViewTilted {
				return a, nil
			}
			delta := sim.ViewTiltThetaStep
			if key.Matches(m, a.keys.TiltDown) {
				delta = -sim.ViewTiltThetaStep
			}
			theta := a.world.NudgeViewTiltTheta(delta)
			a.statusMsg = fmt.Sprintf("view: tilted %g°", theta)
			a.statusExpires = time.Now().Add(1500 * time.Millisecond)
			return a, nil
		case key.Matches(m, a.keys.YawLeft), key.Matches(m, a.keys.YawRight):
			// ADR 0021 G: nudge ViewTilted's yaw φ ±5°, wrapping at
			// 360° (no clamp — yaw is a full turn around the orbit).
			// Same gating as the tilt keys: silent outside ViewTilted
			// so a stray brace in a cardinal view doesn't flash a
			// misleading toast.
			if a.world.ViewMode != sim.ViewTilted {
				return a, nil
			}
			delta := sim.ViewTiltPhiStep
			if key.Matches(m, a.keys.YawLeft) {
				delta = -sim.ViewTiltPhiStep
			}
			phi := a.world.NudgeViewTiltPhi(delta)
			a.statusMsg = fmt.Sprintf("view: yaw %g°", phi)
			a.statusExpires = time.Now().Add(1500 * time.Millisecond)
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
	// The loaded world starts with the nil ("all enabled") default; re-apply
	// the player's program toggles so a load respects them. v0.21 Slice 7.
	a.world.SetEnabledMissionPrograms(enabledProgramsFromSettings(a.orbitView.Settings()))
	a.active = screenOrbit
	return nil
}

// autosave persists on quit. Errors are swallowed — the user is leaving
// and there's no surface to flash a message on. Console-printable saves
// can be wired later if needed.
func (a *App) autosave() {
	_ = a.doSave()
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
	case screens.MenuActionSettings:
		// Navigating to a screen is harmless + reversible, so unlike
		// save/load/quit there is no confirm gate. Reset the cursor so the
		// screen always opens on the first Chip.
		a.settingsScreen.Reset()
		a.active = screenSettings
		return a, nil
	case screens.MenuActionControls:
		a.controls.Reset()
		a.active = screenControls
		return a, nil
	case screens.MenuActionVAB:
		// Open the Vehicle Assembly builder with the live component catalog
		// (embedded + user overlay). Reversible, so no confirm gate.
		a.vab.Reset(spacecraft.Components)
		a.active = screenVAB
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

// toggleChip flips Chip c's visibility in the shared settings.Settings
// and persists it to settings.json immediately (persist-on-toggle — no
// apply button, the v0.13 slice-3 open question decided in favour of the
// simpler write-on-change). The launch screen shares orbitView as its
// hudSource, so SetSettings updates both screens' chip visibility at
// once. A failed write flashes the footer but leaves the in-memory edit
// applied, so the toggle still takes visible effect this session.
func (a *App) toggleChip(c settings.Chip) {
	s := a.orbitView.Settings()
	s.SetChip(c, !s.ChipEnabled(c))
	a.orbitView.SetSettings(s)
	if err := settings.Save(s); err != nil {
		a.statusMsg = fmt.Sprintf("settings save failed: %v", err)
		a.statusExpires = time.Now().Add(3 * time.Second)
	}
}

// enabledProgramsFromSettings maps the persisted Tutorial/Challenges toggles
// to the set of active mission-program names the World evaluator gates on
// (ADR 0025 §2 / v0.21 Slice 7). Always returns a non-nil map so the World's
// nil-default ("all enabled") is overridden — missions stay off until the
// player opts in.
func enabledProgramsFromSettings(s settings.Settings) map[string]bool {
	enabled := map[string]bool{}
	if s.TutorialEnabled {
		enabled[missions.ProgramTutorial] = true
	}
	if s.ChallengesEnabled {
		enabled[missions.ProgramChallenge] = true
	}
	return enabled
}

// toggleMissionProgram flips one gameplay program toggle (tutorial when true,
// challenges when false), re-pushes the active-program set to the World, and
// persists settings.json — mirroring toggleChip's persist-on-change. v0.21
// Slice 7 (ADR 0025 §2).
func (a *App) toggleMissionProgram(tutorial bool) {
	s := a.orbitView.Settings()
	if tutorial {
		s.TutorialEnabled = !s.TutorialEnabled
	} else {
		s.ChallengesEnabled = !s.ChallengesEnabled
	}
	a.orbitView.SetSettings(s)
	a.world.SetEnabledMissionPrograms(enabledProgramsFromSettings(s))
	if err := settings.Save(s); err != nil {
		a.statusMsg = fmt.Sprintf("settings save failed: %v", err)
		a.statusExpires = time.Now().Add(3 * time.Second)
	}
}

// cycleLayout advances the keyboard layout to the next in cycle order and
// persists it to settings.json immediately (ADR 0022) — mirroring
// toggleChip's persist-on-change. The Settings value carried in orbitView is
// the single source of truth for both chips and layout, so the layout write
// rides on the same struct without clobbering the chip overrides. A failed
// write flashes the footer but leaves the in-memory layout applied, so the
// change still takes effect this session.
func (a *App) cycleLayout() {
	a.layout = keylayout.Next(a.layout)
	s := a.orbitView.Settings()
	s.KeyboardLayout = string(a.layout)
	a.orbitView.SetSettings(s)
	if err := settings.Save(s); err != nil {
		a.statusMsg = fmt.Sprintf("settings save failed: %v", err)
		a.statusExpires = time.Now().Add(3 * time.Second)
	}
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
		base = a.help.Render(a.width, a.height, a.layout)
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
	case screenSettings:
		base = a.settingsScreen.Render(a.orbitView.Settings(), a.width)
	case screenControls:
		base = a.controls.Render(a.layout, a.width)
	case screenVAB:
		base = a.vab.Render(a.width)
	case screenBoss:
		base = a.boss.Render(a.width, a.height)
	default:
		if a.world.ViewMode == sim.ViewLaunch {
			base = a.launchView.Render(a.world, a.width, a.height)
		} else {
			base = a.orbitView.Render(a.world, a.selectedBody, a.width, a.height)
		}
	}
	// v0.8.1+: overlay the status message on top of an existing row
	// rather than appending a new line. Appending grew the rendered
	// height by one row and pushed the terminal to scroll the view
	// every time the message expired / re-fired. v0.14+: the keybind
	// footer is gone, so these ride the canvas's bottom border instead
	// — short-lived flight-state lines embedded in the frame edge.
	border := lipgloss.NewStyle().Foreground(a.theme.Primary.GetForeground())
	if a.statusMsg != "" && time.Now().Before(a.statusExpires) {
		base = overlayBottomBorder(base, a.theme.Warning.Render(a.statusMsg), border)
	}
	// v0.11.4+ (ADR 0004): end-flight confirm prompt rides the same
	// bottom-border band as the status flash. Takes precedence when both
	// are active: an in-flight confirm is the actionable state; a stale
	// status message can wait.
	if a.endFlightConfirm {
		c := a.world.ActiveCraft()
		name := "vessel"
		if c != nil {
			name = c.Name
		}
		prompt := fmt.Sprintf("END FLIGHT — remove %s? [y/n]", name)
		base = overlayBottomBorder(base, a.theme.Alert.Render(prompt), border)
	}
	return base
}

// overlayBottomBorder embeds overlay (already styled) into the final row
// of base — the canvas's rounded bottom border — keeping the frame intact:
// `╰─ overlay ─────╯`. Preserves the rendered height. When the last row
// isn't a bottom border (a non-canvas screen) or overlay is too wide to
// inset, it falls back to replacing the row outright. border styles the
// corners/dashes to match the canvas frame.
func overlayBottomBorder(base, overlay string, border lipgloss.Style) string {
	idx := strings.LastIndex(base, "\n")
	if idx < 0 {
		return overlay
	}
	last := base[idx+1:]
	label := " " + overlay + " "
	inner := lipgloss.Width(last) - 2 // dashes available between the corners
	dashes := inner - lipgloss.Width(label)
	if !strings.Contains(last, "╰") || !strings.Contains(last, "╯") || dashes < 0 {
		return base[:idx+1] + overlay // fallback: plain row replacement
	}
	lead := 1
	if dashes < lead {
		lead = 0
	}
	trail := dashes - lead
	return base[:idx+1] +
		border.Render("╰"+strings.Repeat("─", lead)) +
		label +
		border.Render(strings.Repeat("─", trail)+"╯")
}

// bindManeuverTarget hands the current World.Target binding to the
// maneuver form so the four target-relative burn modes and the
// TriggerNextClosestApproach event are pickable + correctly captured
// at plant. Bound at form-open time (not per-keypress), so a target
// switch while the form is open doesn't silently retarget a planted
// burn — the player closes + reopens to retarget. v0.9.3+.
func (a *App) bindManeuverTarget() {
	if a.world.Target.Kind == sim.TargetCraft {
		a.maneuver.SetTargetCraft(true, a.world.Target.CraftID)
		return
	}
	a.maneuver.SetTargetCraft(false, 0)
}
