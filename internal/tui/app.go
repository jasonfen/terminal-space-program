package tui

import (
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"io/fs"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
	"github.com/jasonfen/terminal-space-program/internal/version"
)

type screenID int

// askContext is what a transpose / deploy ask was raised about.
type askContext struct {
	active screenID
	world  *sim.World
	craft  *spacecraft.Spacecraft
}

func (a *App) currentAskContext() askContext {
	return askContext{active: a.active, world: a.world, craft: a.world.ActiveCraft()}
}

// dropStaleAsks clears a transpose / deploy card whose context
// (screen, world, active vessel) changed since it was raised. Runs at the
// top of every Update, so a tick, click or key after the change drops it
// before anything can answer it.
func (a *App) dropStaleAsks() {
	if a.altBox.CapturingText() && a.altStamp != a.currentAskContext() {
		a.altBox.Close()
	}
	if a.card == nil || !a.card.stamped {
		return
	}
	if a.card.stamp != a.currentAskContext() {
		a.card = nil
	}
}

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
	screenSaves    // v0.26 / ADR 0033 §F: unified Saves browser, reached from the menu's Save/Load items.
	screenBoss     // boss key: full-screen fake developer shell (backtick from any screen).
	screenSession  // v0.27 S6 / ADR 0034: multiplayer roster + invites, on `O`.
)

// UndockGuestMsg and TransferControlMsg are cross-player-docking intents the
// App emits up to the serve reporting wrapper, which owns the dock ledger
// (v0.28 S5). Same pattern as SessionHostMsg / SessionAdminMsg: the App can't
// reach the ledger (sim sits below serve), so the flight key produces a
// command the wrapper intercepts in its Update. Both are inert in solo play —
// the App only emits them for a cross-player stack, which never exists without
// a server.
type UndockGuestMsg struct{}

// TransferControlMsg requests handing the active cross-player stack to the guest.
type TransferControlMsg struct{}

// ReleaseGuestMsg is the OWNER seat's [U] on a cross-player stack (ADR 0040
// §3): release the guest's component through the ledger rather than splitting
// locally, which would clone their craft into this World. It travels the same
// route as the other two — the App can't reach the ledger — and works whether
// or not the guest is connected: an absent guest's component becomes a Parcel.
type ReleaseGuestMsg struct{}

// HandOverCargoMsg is the rider's `u` (ADR 0057 conflict 2a): the Dragon's
// owner is riding in someone else's stack, so the composite lives in the stack
// owner's World and the hand-over has to be asked of that seat through the dock
// ledger. Like the other cross-player intents it is inert in solo play.
type HandOverCargoMsg struct{}

// ChatSendMsg is a chat line on its way up to the serve wrapper, which
// owns the chat ring (ADR 0035). To/ToHandle address a DM at one player
// (resolved against the online roster before sending); both empty means
// broadcast. Inert in solo play — chat is unavailable without a session,
// like the rest of SessionInfo.
type ChatSendMsg struct {
	Text     string
	To       string // target fingerprint, "" = broadcast
	ToHandle string
}

// App is the root tea.Model. It owns the world, theme, keymap, and which
// screen is active. Screens read from the shared world; they don't
// mutate it.
type App struct {
	world  *sim.World
	theme  Theme
	keys   Keymap
	active screenID

	resumed bool // the World came from durable storage, not tui.New's default

	selectedBody int

	width, height int
	// backStart/backEnd: the [Back] button's display-cell span on the
	// Title Row of the last form screen rendered (B11 / G9 Q1).
	backStart, backEnd int
	// fromMenu: the active screen was opened from the pause menu, so
	// back returns there (backToOpener).
	fromMenu bool
	// helpFrom is the screen F1 was pressed on (GK Q3, #589); closing help
	// puts the player back on it, state untouched. helpFromSet is false when
	// help was opened from the pause menu's Help row, which keeps the
	// backToOpener rule.
	helpFrom    screenID
	helpFromSet bool
	// menuHeld: the pause menu owns the clock's pause (openMenu), and
	// menuPrevPaused is the state to give back on closeMenu.
	menuHeld, menuPrevPaused bool
	// menuOriginX/Y: the pause card's top-left, in the screen's body
	// coordinates (below the Title Row), for click translation.
	menuOriginX, menuOriginY int

	orbitView  *screens.OrbitView
	launchView *screens.LaunchView
	bodyInfo   *screens.BodyInfo
	help       *screens.Help
	maneuver   *screens.Maneuver
	porkchop   *screens.Porkchop
	menu       *screens.Menu
	// card is the one question on screen (ADR 0054, #633): the quit prompt
	// or a flight ask (end flight, transpose, deploy, quickload), drawn
	// over the dimmed screen. nil = no question is up.
	card     *appCard
	// altBox is the L key's typed-altitude box (ADR 0059 decision 4); altStamp
	// is what it was opened about, lastRaiseKm the altitude last typed.
	altBox      *screens.AltitudeBox
	altStamp    askContext
	lastRaiseKm float64
	missions *screens.Missions
	spawn    *screens.SpawnCraft

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

	// saves is the unified Saves browser (v0.26 / ADR 0033 §F), reached
	// from both pause-menu items ([Save Game] opens it in save-mode,
	// [Load Game] in load-mode). The screen is filesystem-free: the App
	// passes the save.List() listing at open/refresh and dispatches the
	// returned SavesCommands against the save package.
	saves *screens.SavesScreen

	// boss is the backtick "boss key" fake shell. bossReturnScreen records
	// the screen that was active when it opened, and bossPrevPaused records
	// the sim pause state at that moment; exit restores both so the player
	// lands back exactly where they left, with the clock as it was.
	boss             *screens.BossShell
	bossReturnScreen screenID
	bossPrevPaused   bool

	// session is the multiplayer roster screen (v0.27 S6, ADR 0034).
	// Renders from world.Session (the serve-layer slate); its admin
	// commands (mint/revoke/remove) are executed by the serve wrapper
	// via SessionAdminMsg — the App only dispatches.
	session *screens.SessionScreen

	// savesPrevPaused records the sim pause state when the Saves screen
	// opened (finding 4). The Saves browser freezes the clock while it is
	// up so a save can't capture a world that drifted (or crashed) while
	// the player typed a name; exit restores the prior state — mirroring
	// the boss-key save/restore. A load exits by replacing the world
	// outright, so it never restores this.
	savesPrevPaused bool

	// statusMsg flashes a one-line notice in the HUD footer for ~3
	// seconds after save / load. Cleared by clearStatusAfter via a
	// scheduled tea.Cmd.
	statusMsg     string
	statusExpires time.Time

	// autosaveFailing: the last solo autosave failed and the player was told.
	autosaveFailing bool
	// lastAutosave is the wall-clock time of the last periodic-autosave
	// check anchor (v0.26 S4 / ADR 0033 §E): armed by the first physics
	// tick, reset on every interval fire. Deliberately wall-clock — the
	// timer must not accelerate under warp — and fed from the tea.Tick
	// timestamp carried in sim.TickMsg (see maybeAutosave), never from
	// an inline time.Now(), so tests can drive it with a synthetic
	// clock. Zero means "not armed yet".
	lastAutosave time.Time

	// guestSave, when non-nil, marks this App as a multiplayer guest
	// session (v0.27 S3, ADR 0034): every autosave surface writes
	// through this per-player sink instead of the host's local saves,
	// and the local Saves surface (F5/F9, menu Save/Load) is disabled.
	guestSave func(*sim.World) error
	// recDir is where the Flight Recording files go (ADR 0062): the local
	// saves dir's recordings/ by default, the session dir's per-player folder
	// for a guest (SetRecordingsDir). recWorld is the World recording is
	// attached to; the first tick (or a load) attaches a World it differs from.
	recDir   string
	recWorld *sim.World
	// recFolders: recDir is the local saves dir, where each game gets its own
	// folder (v0.63.3). SetRecordingsDir (a guest) turns it off.
	recFolders bool
	// onPersisted is called with the World after persistNow wrote it
	// successfully (ADR 0040 amendment 3: the hosting App's autosaves
	// acknowledge to the dock ledger).
	onPersisted func(*sim.World)

	// Chat input overlay (ADR 0035 S3). App-level rather than a screen
	// so the capturingText obligation is one unconditional check — not
	// another arm of the per-screen switch a future screen could forget.
	// While chatOpen the sim keeps running and every key is text or an
	// editing action (accepted cost: flight keys are captured). The tab
	// state drives @handle completion cycling: chatTabbing marks a cycle
	// in progress (any edit ends it), chatTabBase is the typed prefix
	// the candidate list is built from (may legitimately be empty — a
	// bare @ matches everyone), chatTabIdx the candidate last offered.
	chatOpen    bool
	chatInput   []rune
	chatTabbing bool
	chatTabBase string
	chatTabIdx  int
}

// chatInputRuneCap mirrors the serve-side message cap (the ring
// truncates authoritatively; capping here keeps typing honest).
const chatInputRuneCap = 120

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
	// Gate the seeded mission programs by the persisted toggles: Flight
	// School is on unless explicitly switched off (#425), the Challenge
	// ladder stays opt-in. v0.21 Slice 7.
	w.SetEnabledMissionPrograms(enabledProgramsFromSettings(prefs))
	recDir := ""
	if d, err := save.SavesDir(); err == nil {
		recDir = filepath.Join(d, "recordings")
	}
	return &App{
		recDir:     recDir,
		recFolders: recDir != "",
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
		altBox:     screens.NewAltitudeBox(sth),
		porkchop:   screens.NewPorkchop(sth),
		menu:       screens.NewMenu(sth),
		missions:   screens.NewMissions(sth),
		spawn:      screens.NewSpawnCraft(sth),

		settingsScreen: screens.NewSettingsScreen(sth),
		controls:       screens.NewControlsScreen(sth),
		vab:            screens.NewVAB(sth),
		saves:          screens.NewSavesScreen(sth),
		boss:           screens.NewBossShell(sth),
		session:        screens.NewSessionScreen(sth),
	}, nil
}

// Init kicks off the tick loop.
func (a *App) Init() tea.Cmd {
	return sim.TickCmd(a.world.Clock.BaseStep)
}

// World exposes the live sim state for the multiplayer serve layer —
// the final persist on session disconnect runs after the program has
// stopped, so the read is race-free. Screens keep going through sim
// methods; this is not a mutation door.
func (a *App) World() *sim.World { return a.world }

// SetGuestPersistence flips this App into guest-session mode: sink
// replaces every local autosave write and the local Saves surface is
// refused with a toast. v0.27 S3 (ADR 0034).
func (a *App) SetGuestPersistence(sink func(*sim.World) error) { a.guestSave = sink }

// SetRecordingsDir points the Flight Recording files at dir (the serve layer,
// per player). Call before the first tick: the file opens then.
func (a *App) SetRecordingsDir(dir string) { a.recDir, a.recFolders = dir, false }

// attachRecording closes the recording of the World being left and opens the
// Active Vessel's file for w, with the configured dir and window.
func (a *App) attachRecording(w *sim.World) {
	left := ""
	if a.recWorld != nil && a.recWorld != w {
		a.recWorld.CloseRecording()
		left = a.recWorld.GameFolder
	}
	a.recWorld = w
	w.RecordingsDir = a.recDir
	if a.recFolders {
		// A World without a (usable) folder is a new game; a loaded save's
		// folder is continued.
		f := w.GameFolder
		if f == "" || f != filepath.Base(f) || f == "." || f == ".." {
			var err error
			if f, err = sim.NewGameFolder(a.recDir, time.Now()); err != nil {
				w.NoteRecordingError(err)
				return
			}
			w.GameFolder = f
		}
		if left != f {
			sim.RemoveAbandonedGameFolder(a.recDir, left)
		}
		w.RecordingsDir = filepath.Join(a.recDir, f)
	}
	w.RecordingWindowDays = a.orbitView.Settings().RecordingWindow()
	_ = w.OpenActiveRecording()
}

// SetOnPersisted installs the hook persistNow calls after a successful write.
func (a *App) SetOnPersisted(fn func(*sim.World)) { a.onPersisted = fn }

// ResumeNewestAutosave swaps in the newest readable autosave-ring slot as the
// live World and says so in the footer. It is the inverse of the ring's
// rotation, for a `--serve` start (ADR 0040 amendment 3). With no readable
// slot it returns an error and leaves the fresh World alone.
func (a *App) ResumeNewestAutosave() (string, error) {
	infos, err := save.List() // newest first
	if err != nil {
		return "", err
	}
	for _, in := range infos {
		if in.Lane != save.LaneAutosave || in.Unreadable {
			continue
		}
		if err := a.loadWorldByID(in.ID); err != nil {
			continue // a slot that will not load (catalog changed) is skipped, not fatal
		}
		a.flash("resumed autosave " + in.ID)
		return in.ID, nil
	}
	return "", errors.New("no readable autosave to resume")
}

// ResumedFromSave reports whether the World came from durable storage. A New
// App's default World did not, so the ledger cannot match its Vessel IDs.
func (a *App) ResumedFromSave() bool { return a.resumed }

// NewWithWorld builds an App around an existing world — the
// multiplayer reconnect path, where the player's persisted world is
// restored instead of a fresh default start.
func NewWithWorld(w *sim.World) (*App, error) {
	a, err := New(nil)
	if err != nil {
		return nil, err
	}
	a.world, a.resumed = w, true
	a.world.SetEnabledMissionPrograms(enabledProgramsFromSettings(a.orbitView.Settings()))
	return a, nil
}

// sizeGated reports whether the terminal is below the playable floor
// (v0.27 S2). Width 0 means no WindowSizeMsg yet — not gated, View's
// "initializing…" placeholder covers that instant.
func (a *App) sizeGated() bool {
	return a.width > 0 &&
		(a.width < screens.MinTerminalWidth || a.height < screens.MinTerminalHeight)
}

// Update routes every tea.Msg. Globals handled here; screen-scoped
// keys delegate to the active screen.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A terminal in modifyOtherKeys / kitty mode sends shifted keys as CSI
	// sequences Bubble Tea v1 drops; turn them back into the key pressed
	// (modified_keys.go) so `|`, `Z`, `X` and the rest still work.
	if k, ok := decodeModifiedKey(msg); ok {
		msg = k
	}
	a.dropStaleAsks()
	switch m := msg.(type) {
	case sim.TickMsg:
		// ADR 0058 decision 5: note which vessels burn lit solids going in, so
		// a tank that empties inside this tick (or a warp batch) flashes once.
		boostersLit := make(map[*spacecraft.Spacecraft]bool)
		for _, c := range a.world.Crafts {
			if sim.BoosterSolidsLit(c) {
				boostersLit[c] = true
			}
		}
		if a.recWorld != a.world {
			// A fresh start can rewind the clock to the same scenario: name it.
			a.world.SetRecordingMarker("session start: " + version.Version)
			a.attachRecording(a.world)
		}
		a.world.Tick()
		if a.orbitView.RendezvousPickerOpen() {
			a.orbitView.SetRendezvousPickerNow(a.world.Clock.SimTime)
		}
		// v0.26 S4: periodic autosave, driven by the wall-clock stamp
		// tea.Tick put in the TickMsg — real minutes, never game minutes,
		// so warp can't accelerate it.
		a.maybeAutosave(time.Time(m))
		// v0.8.3+: surface docking events as a status flash. Cleared
		// here so a single fusion only flashes once. #421 / ADR 0048 §2:
		// same voice Undock already uses ("undocked into N components") —
		// verb, what, the number that matters — rather than the old bare
		// "composite: <name>", so an auto-fuse (the "alongside active"
		// spawn case chief among them) never welds two vessels together
		// in total silence.
		if e := a.world.LastDockEvent; e != nil {
			a.flash(fmt.Sprintf("docked with %s, now 1 vessel, %d components", e.PartnerName, e.ComponentCount))
			a.world.LastDockEvent = nil
		}
		// ADR 0053 decision 8 (slice D): two hulls touched outside the
		// docking gate and bounced. One-shot, same surface as the dock flash.
		a.flashBump()
		// v0.11.0+: ViewLaunch release toast, names only the view returned to (#468; ADR 0021 D
		// retired the apoapsis-floor auto-release). Same flash
		// surface as docking; cleared after one fire.
		if e := a.world.LastLaunchReleaseEvent; e != nil {
			a.flash(fmt.Sprintf("launch view off, back on the %s view", e.PrevView))
			a.world.LastLaunchReleaseEvent = nil
		}
		// #294 review finding 5: a due target-relative node (or in-flight
		// burn) refused to fire because its bound target hasn't resolved —
		// say so instead of leaving the stall silent. Same flash surface,
		// cleared after one fire. #294 review finding 2: a never-resolvable
		// ref (local target craft gone for good, or a ghost ref the
		// reconnect watchdog already gave up on) gets a distinct message —
		// the node/burn is cancelled outright, not merely held pending a
		// re-latch that will never come.
		if e := a.world.LastNodeTargetRefusal; e != nil {
			if e.NoEngine {
				a.flash(fmt.Sprintf("%s: node cancelled, no engine", e.CraftName))
			} else if e.Cancelled {
				a.flash(fmt.Sprintf("%s: node cancelled, target gone", e.CraftName))
			} else {
				a.flash(fmt.Sprintf("%s: burn held off — target lock not resolved", e.CraftName))
			}
			a.world.LastNodeTargetRefusal = nil
		}
		if f := a.world.RecordingFault; f != "" {
			a.flash("flight recording stopped: " + f)
			a.world.RecordingFault = ""
		}
		// ADR 0048 / decision 4: burn fired / finished Event Flashes — the
		// gap the ADR named ("a burn fires and finishes with no notice").
		// Per-craft (executeDueNodesFor / integrateOneCraft run once per
		// craft in the slate), so these fire regardless of which vessel is
		// active. Slices, not single pointers (review finding 1): two
		// crafts can ignite (or exhaust) in the same tick, and both must
		// reach the player, not just whichever stashed last. Drained (read
		// then cleared) in full every TickMsg, fired-events before
		// finished-events, joined into ONE a.flash() call (review finding
		// 6): statusMsg is a single-slot display with one shared TTL, so
		// separate flash() calls in the same tick left only the last one
		// visible — a real coin-flip, not hypothetical: a single craft
		// with two nodes queued back-to-back collides on ~every chained
		// burn, since Tick() runs integrateOneCraft (finish) before
		// executeDueNodes (fire) each tick. Joining costs nothing (two
		// messages run ~90 chars, well inside the 140-col design floor)
		// and means no event is ever silently unseen.
		var burnMsgs []string
		for _, e := range a.world.PendingBurnFiredEvents {
			burnMsgs = append(burnMsgs, fmt.Sprintf("%s: node %d firing — %.0f m/s", e.CraftName, e.NodeIndex+1, e.DV))
		}
		a.world.PendingBurnFiredEvents = nil
		for _, e := range a.world.PendingBurnFinishedEvents {
			if e.DV == 0 {
				// #447 review finding 8: a pre-PR save loaded mid-burn
				// decodes ActiveBurn.PlannedDV as its zero value (the
				// field didn't exist yet), so DV reads 0 here — not
				// "burned 0 m/s" (a false statement about a burn that
				// just delivered real Δv) but simply an unknown figure.
				// Drop the clause rather than print a fabricated number.
				burnMsgs = append(burnMsgs, fmt.Sprintf("%s: node %d burned — %d remaining", e.CraftName, e.NodeIndex+1, e.NodesRemaining))
			} else {
				burnMsgs = append(burnMsgs, fmt.Sprintf("%s: node %d burned — %.0f m/s, %d remaining", e.CraftName, e.NodeIndex+1, e.DV, e.NodesRemaining))
			}
		}
		a.world.PendingBurnFinishedEvents = nil
		// ADR 0059 decision 4: a commanded raise ends with its own flash
		// (complete, argon exhausted, stopped), joined with the burn lines.
		for _, e := range a.world.PendingRaiseEvents {
			burnMsgs = append(burnMsgs, e.Text)
		}
		a.world.PendingRaiseEvents = nil
		for c := range boostersLit {
			if !sim.BoosterSolidsLit(c) && len(c.Stages) > 0 && c.Stages[0].LitWithAbove && c.Stages[0].FuelMass <= 0 {
				burnMsgs = append(burnMsgs, "boosters burned out")
				break
			}
		}
		if len(burnMsgs) > 0 {
			a.flash(strings.Join(burnMsgs, " · "))
		}
		return a, sim.TickCmd(a.world.Clock.BaseStep)

	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.orbitView.Resize(m.Width, m.Height)
		a.launchView.Resize(m.Width, m.Height)
		a.maneuver.Resize(m.Width, m.Height-1)
		return a, nil

	case screens.BurnExecutedMsg:
		overBudget, overBy := false, 0.0
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
			// Over-budget Node (ADR 0047 §2 / #428): warn and allow —
			// planning a burn you can't yet afford is legitimate (you
			// may refuel, stage, or dock a tug before it fires), so this
			// never blocks the plant. Computed against the CURRENT
			// remaining budget, before anything below mutates fuel —
			// a scheduled PlanNode doesn't touch mass at all, and an
			// impulsive/ActiveBurn plant hasn't started consuming it
			// yet either, so this read is stable regardless of which
			// branch fires next.
			if budget := a.world.ActiveCraft().RemainingDeltaV(); m.DV > budget {
				overBudget, overBy = true, m.DV-budget
			}
			// v0.6.5: derive burn duration from Δv using the rocket
			// equation against the live craft state, so the planner UX
			// only has to specify Δv. Zero-thrust craft fall back to the
			// legacy impulsive path (Duration = 0) — the API still
			// supports that branch, just no longer through the form.
			dur := a.world.ActiveCraft().BurnTimeForDVAt(m.DV, m.Throttle)
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
					ID:               editedID,
					TriggerTime:      m.TriggerTime,
					Mode:             m.Mode,
					DV:               m.DV,
					Duration:         dur,
					Event:            m.Event,
					Throttle:         m.Throttle,
					TargetCraftID:    m.TargetCraftID,
					TargetGhostOwner: m.TargetGhostOwner,
					AdvisoryKey:      m.AdvisoryKey, // #294 second-round review finding 6
				})
			case m.Event != sim.TriggerAbsolute:
				// v0.6.0: event-relative nodes go through PlanNode so
				// the resolver can freeze TriggerTime against the live
				// orbit on the next Tick.
				a.world.PlanNode(sim.ManeuverNode{
					ID:               editedID,
					Mode:             m.Mode,
					DV:               m.DV,
					Duration:         dur,
					Event:            m.Event,
					Throttle:         m.Throttle,
					TargetCraftID:    m.TargetCraftID,
					TargetGhostOwner: m.TargetGhostOwner,
					AdvisoryKey:      m.AdvisoryKey, // #294 second-round review finding 6
				})
			case dur == 0:
				// v0.9.3+: target-relative impulsive needs the bound
				// target snapshot for direction resolution. Resolve a
				// local ref by stable ID (ADR 0012) or a remote ghost
				// ref against the ghost slate (#294 review round 3
				// finding I) via the same resolver a planted node's
				// fire-time dispatch uses.
				if c := a.world.ActiveCraft(); c.Thrust <= 0 || c.Isp <= 0 {
					// Audit 18: no engine, no Δv. This replaces the over-budget flash below.
					overBudget = false
					a.flash(fmt.Sprintf("%s: no engine", c.Name))
					break
				}
				if m.TargetCraftID != 0 {
					if rT, vT, ok := a.world.TargetRelState(m.TargetGhostOwner, m.TargetCraftID, a.world.ActiveCraft().Primary); ok {
						a.world.ActiveCraft().ApplyImpulsiveWithTarget(m.Mode, m.DV, rT, vT)
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
					Mode:             m.Mode,
					DVRemaining:      m.DV,
					EndTime:          a.world.Clock.SimTime.Add(dur),
					Throttle:         effThrottle,
					TargetCraftID:    m.TargetCraftID,
					TargetGhostOwner: m.TargetGhostOwner,
				}
			}
			if overBudget {
				// Same flash mechanism as every other planner outcome
				// (ADR 0048 §1 — the one announcement helper).
				a.flash(fmt.Sprintf("plan exceeds Δv budget by %.0f m/s", overBy))
			}
		}
		a.closeManeuverToOrbit()
		return a, nil

	case screens.NodeDeleteMsg:
		// v0.8.6+: per-node delete from the maneuver form. Form
		// dispatched ctrl+d for the node under the Plan Cursor (ADR 0047).
		stopped := a.world.DeleteNode(m.EditingIdx)
		a.closeManeuverToOrbit()
		a.flashStoppedBurn(stopped)
		return a, nil

	case screens.NodeClearAllMsg:
		// v0.8.6+: clear-all from the maneuver form. Replaces the
		// retired N global keybinding.
		stopped := a.world.ClearNodes()
		a.world.RecordAction(missions.ActionClearNodes) // ADR 0025 §7
		a.closeManeuverToOrbit()
		a.flashStoppedBurn(stopped)
		return a, nil

	case tea.MouseMsg:
		// v0.6.4: click-only selection. Left-press only; motion /
		// release / wheel ignored. Per-screen routing: orbit's hit
		// dispatch is most-specific-first (vessel → node → body →
		// HUD); porkchop click sets the cell selection.
		if m.Action != tea.MouseActionPress || m.Button != tea.MouseButtonLeft {
			return a, nil
		}
		// v0.27 S2: clicks against a gate screen would hit-test stale
		// layouts — drop them like gated keys.
		if a.sizeGated() {
			return a, nil
		}
		// A card swallows every click that is not on one of its rows (ADR
		// 0054): not the Title Row's buttons, not the screen under it, not the
		// pause menu's rows a quit card covers. A row picks as its letter would.
		if a.card != nil {
			x, y := a.cardOrigin(screens.CardW, a.card.Height())
			if vis, ok := a.card.HitRow(m.X-x, m.Y-y); ok {
				return a.cardPicked(a.card.Pick(vis))
			}
			return a, nil
		}
		// A screen that asks owns its card (ActiveCard); the same rule holds:
		// a click on a row answers it as enter on that row would, anything
		// else is swallowed.
		if c := a.screenCard(); c != nil {
			x, y := a.cardOrigin(screens.CardW, c.Height())
			if vis, ok := c.HitRow(m.X-x, m.Y-y); ok {
				c.SetCursor(vis)
				return a.Update(tea.KeyMsg{Type: tea.KeyEnter})
			}
			return a, nil
		}
		// B11 / G9 Q1: every non-flight screen has the shared Title Row on
		// row 0 with a [Back] button (the click twin of esc); below it the
		// screen's own hit-tests see rows counted from the body's top.
		if a.active != screenOrbit && a.active != screenBoss {
			if m.Y == 0 {
				if m.X >= a.backStart && m.X < a.backEnd {
					return a.Update(tea.KeyMsg{Type: tea.KeyEsc})
				}
				return a, nil
			}
			m.Y--
		}
		switch a.active {
		case screenOrbit:
			// #456 fix: while ViewLaunch is showing, the Menu/Missions/
			// Burn title-bar buttons and the navball controls below all
			// read OrbitView state that LaunchView.Render never touches
			// — the map's own last-actual-render layout, frozen and
			// unrelated to what's on screen right now. A click on THIS
			// screen's [»Burn] button used to fall through to those
			// stale map coordinates and could land inside e.g. the
			// map's [Missions] range instead. Route to LaunchView's own
			// hit-test (added alongside this fix) first.
			//
			// Chips are the one exception (#457 review finding 1):
			// LaunchView.Render composites its chips through the SAME
			// shared OrbitView (v.hudSource.composeChips), which
			// rewrites v.chipRects in absolute screen coordinates every
			// frame regardless of which screen called it — so
			// a.orbitView.HitChip is live and correct here, not stale,
			// and the NODES chip's click-to-maneuver-screen route
			// already worked on Launch View before this fix. Falling
			// through to it (but no further — HitNavballControl / HitAt
			// / the vessel-node-body switch below ARE stale) preserves
			// that instead of silently regressing it.
			if a.world.ViewMode == sim.ViewLaunch {
				if a.launchView.HitBurnButton(m.X, m.Y) {
					a.toggleAutoWarpBurn()
					return a, nil
				}
				// B11 / G9 Q1: the launch Title Row carries [Menu] and
				// [Missions] like the map's.
				if a.launchView.HitMenuButton(m.X, m.Y) {
					a.openMenu()
					return a, nil
				}
				if a.launchView.HitMissionsButton(m.X, m.Y) {
					a.active = screenMissions
					return a, nil
				}
				if id, ok := a.orbitView.HitChip(m.X, m.Y); ok {
					if id == settings.ChipNodes {
						a.world.Clock.Paused = true
						a.active = screenManeuver
					}
					return a, nil
				}
				return a, nil
			}
			// v0.7.4+: title-bar [Menu] / [Missions] buttons take
			// priority over canvas / HUD hits, since they sit at
			// row 0 above the body region.
			if a.orbitView.HitMenuButton(m.X, m.Y) {
				a.openMenu()
				return a, nil
			}
			if a.orbitView.HitMissionsButton(m.X, m.Y) {
				a.active = screenMissions
				return a, nil
			}
			// v0.16 / ADR 0016: the [»Burn] button toggles Auto-Warp
			// (click-equivalent of `G`). A no-op when no burn is eligible.
			if a.orbitView.HitBurnButton(m.X, m.Y) {
				a.toggleAutoWarpBurn()
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
			// Inspect (ADR 0041 §3): a click on ANY tagged pixel drives
			// the same highlight the [j] cycle does — glyphs and disks
			// as before, and now orbit-line pixels too, since the class
			// drawers carry an owner tag. Applied before the action
			// switch and unconditionally, so a click both DOES its
			// existing thing and answers "what is this?".
			ownerRef, ownerHit := screens.InspectRef{}, false
			if hit.Owner != "" {
				ownerRef, ownerHit = a.orbitView.InspectByOwner(hit.Owner)
			}
			// A hit on the ACTIVE vessel's own orbit line keeps its
			// v0.6.4 meaning — click a point on your own trajectory to
			// stage a burn there — because that is a place, not an
			// identity question: you already know whose line it is. Any
			// other line stops the click at identity.
			//
			// hit.OwnerTied vetoes it: where your line and someone
			// else's ink the cell equally, the click is genuinely
			// ambiguous, and the two readings do not cost the same. The
			// identity reading paints a name chip you can dismiss; the
			// own-line reading pauses the clock and swaps to the
			// maneuver screen. On a coin toss, take the reversible one —
			// the ADR 0041 §3 contract is identity ON DEMAND, and a
			// question that sometimes answers with a mode change reads
			// as a misfire. Zoom in and the tie resolves itself.
			ownOrbitLine := ownerHit && !hit.OwnerTied &&
				ownerRef.Kind == screens.InspectVessel &&
				a.world.ActiveCraft() != nil && ownerRef.CraftID == a.world.ActiveCraft().ID
			switch {
			case hit.IsVessel:
				if a.world.CraftVisibleHere() {
					a.world.Focus = sim.Focus{Kind: sim.FocusCraft}
				}
			case hit.NodeIdx > 0:
				idx := hit.NodeIdx - 1 // tags are 1-indexed; slice is 0-indexed
				if idx >= 0 && idx < len(a.world.ActiveCraft().Nodes) {
					// #294 review round 3 finding I: LoadNode already reads
					// the node's OWN target binding (local or ghost) — do
					// NOT follow it with bindManeuverTarget here, which
					// reflects the CURRENT live World.Target and would
					// clobber a ghost-bound node's lock the instant the
					// live target isn't that exact same ghost (a common
					// case right after a reconnect, before it re-latches).
					// bindManeuverTarget is for the NEW-node flows only —
					// see its own doc comment.
					a.maneuver.LoadNode(idx, a.world.ActiveCraft().Nodes[idx])
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
			case ownerHit && !ownOrbitLine:
				// Orbit-line (or other owner-tagged) pixel with no
				// vessel / node / body action of its own: the flare and
				// the name chip ARE the response. Swallowed here so it
				// doesn't fall through to the stage-a-burn path below —
				// clicking someone else's track must not plant anything
				// on yours.
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
			action := a.menu.HandleClick(m.X-a.menuOriginX, m.Y-a.menuOriginY)
			if action != screens.MenuActionNone {
				return a.applyMenuAction(action)
			}
		case screenMissions:
			// Missions has no clickable body rows; [Back] is the Title Row's.
		case screenSettings:
			action, chip := a.settingsScreen.HandleClick(m.X, m.Y)
			switch action {
			case screens.SettingsActionToggle:
				a.toggleChip(chip)
			case screens.SettingsActionToggleTutorial:
				a.toggleMissionProgram(true)
			case screens.SettingsActionToggleChallenges:
				a.toggleMissionProgram(false)
			case screens.SettingsActionCycleAutosave:
				a.cycleAutosaveInterval()
			case screens.SettingsActionCycleEmptyReadings:
				a.cycleEmptyReadings()
			case screens.SettingsActionCycleRecordingWindow:
				a.cycleRecordingWindow()
			case screens.SettingsActionCancel:
				a.backToOpener()
			}
		case screenControls:
			switch a.controls.HandleClick(m.X, m.Y) {
			case screens.ControlsActionCycleLayout:
				a.cycleLayout()
			case screens.ControlsActionCancel:
				a.backToOpener()
			}
		case screenSaves:
			return a.applySavesCommand(a.saves.HandleClick(m.X, m.Y))
		}
		return a, nil

	case tea.KeyMsg:
		// A key on the map clears any stale menu-opener (backToOpener).
		if a.active == screenOrbit {
			a.fromMenu = false
		}
		// Keyboard-layout normalization (ADR 0022): translate the keypress
		// from the player's layout back to its QWERTY position before any
		// matching, so the Keymap and every raw-string handler stay QWERTY.
		// Skipped while a screen is capturing literal text (the boss shell,
		// a Saves name input) — a QWERTZ player typing there wants their
		// real keycaps, not a transposed rune (finding 3).
		if !a.capturingText() {
			m = normalizeKey(a.layout, m)
		}
		// ctrl+c bypasses everything else (standard interrupt
		// convention) and is honored from any screen — but as of #474
		// it raises the quit prompt rather than quitting on the spot.
		// This is the deliberate trade Jason chose: ctrl+c stops being
		// an instant panic key so there's always a chance to say no.
		if key.Matches(m, a.keys.Quit) {
			a.armQuit()
			return a, nil
		}
		// While a card is up (the quit prompt or a flight ask), every key
		// funnels through its handling — honored from any screen for the
		// same reason the raise above is, so a stray flight key can't slip
		// through mid-question (ADR 0054, the ADR 0052 modal rule).
		if a.card != nil {
			return a.handleCardKey(m)
		}
		// v0.27 S2: while the size gate is up, gameplay keys are
		// swallowed — the player can't see what a keypress would do.
		// Quit above stays honored from the gated state.
		if a.sizeGated() {
			return a, nil
		}
		// The altitude box (ADR 0059): while it is up every key is text or an
		// editing action, so no flight key slips through a half-typed number.
		if a.altBox.CapturingText() {
			return a.handleAltBoxKey(m)
		}
		// Boss key: a single global keypress (backtick) swaps the whole
		// screen to a fake developer shell, and swaps back on
		// exit/logout/ctrl+d. This block sits ABOVE every per-screen block
		// and above the flight keys so it fires from ANY screen and so the
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
		// The boss key opens from any screen — EXCEPT while a screen is
		// capturing literal text (a Saves name input), where a backtick is
		// a character the player is typing, not the boss hotkey (finding 9).
		// A screen-owned card (roster, saves, VAB) swallows it too: every card
		// takes every key but its own (ADR 0054), and the screen's cardKey
		// only sees keys that get past the globals below.
		if key.Matches(m, a.keys.BossKey) && !a.capturingText() && a.screenCard() == nil {
			a.bossReturnScreen = a.active
			a.bossPrevPaused = a.world.Clock.Paused
			a.world.Clock.Paused = true // freeze the sim while "away"
			a.boss.Reset()              // fresh lived-in session each open
			a.active = screenBoss
			return a, nil
		}
		// Chat capture (ADR 0035 S3): while the input is up, every key
		// is text or an editing action — the unconditional return keeps
		// flight controls, screens, and the boss key out of reach.
		// While chatting E is text, so the end-flight confirm can never
		// arm underneath an open input.
		if a.chatOpen {
			return a.handleChatKey(m)
		}
		// ~ opens chat from the flight view (ADR 0035 §4; U+007E, a
		// different rune from the U+0060 boss key, and absent from every
		// layout map so normalization passed it through untouched). Not
		// while a card is up: the card above swallows the ~, so chat can
		// never open over an armed question.
		if a.active == screenOrbit && m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == '~' {
			if a.world.Session == nil {
				// Solo: say so — a dead key reads as broken (v0.30 lesson).
				a.toast("chat is multiplayer — host or join a session [O]")
				return a, nil
			}
			a.chatOpen = true
			a.chatInput = a.chatInput[:0]
			a.chatTabbing = false
			return a, nil
		}
		// F1 / ? opens help from every screen, on that screen's own page,
		// and closing it puts the player back exactly where they were
		// (GK Q3, #589). Sits above every per-screen block so no screen
		// can swallow it. Inside a free-text box only F1 opens it: `?` is
		// a character there. Screens keep their state because the key is
		// never forwarded to them.
		if key.Matches(m, a.keys.Help) && a.active != screenHelp && a.screenCard() == nil && (m.String() != "?" || !a.capturingText()) {
			a.openHelpHere()
			return a, nil
		}
		// Rendezvous Planner picker (ADR 0045 S6, #399): a walkable chip on
		// the orbit map, not a screen — a.active never changes while it's
		// open, so this can't be handled by any of the a.active==screenXxx
		// blocks below. Sits above the flight-key switch (which owns
		// PanLeft/Right/Up/Down on the very same physical keys) for the
		// same reason the card intercept does: a new interactive surface
		// that forgets to claim its keys here bleeds into flight controls
		// — exactly the trap ADR 0044's review caught (#399's own second
		// named trap).
		if a.orbitView.RendezvousPickerOpen() {
			return a.handleRendezvousPickerKey(m)
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
					a.flash("custom stack is empty — add a part with [a]")
					return a, nil
				}
				spec := sim.SpawnSpec{
					LoadoutID:       a.spawn.SelectedLoadoutID(),
					DesignID:        a.spawn.SelectedDesignID(),
					CustomStages:    a.spawn.SelectedCustomStages(),
					NosePayloadPlan: a.spawn.SelectedNosePayloadPlan(),
					ParentBodyID:    a.spawn.SelectedParentID(),
					AltitudeM:       a.spawn.SelectedAltitudeM(),
					Inclination:     a.spawn.SelectedInclinationDeg(),
					Retrograde:      a.spawn.SelectedRetrograde(),
					Alongside:       a.spawn.SelectedAlongside(),
					Launchpad:       a.spawn.SelectedLaunchpad(),
					Latitude:        a.spawn.SelectedLatitudeDeg(),
					LongitudeOffset: a.spawn.SelectedLongitudeEastDeg(),
				}
				if c, err := a.world.SpawnCraft(spec); err == nil {
					a.flash(fmt.Sprintf("spawned vessel %d (%s)", a.world.ActiveCraftIdx+1, c.Name))
				} else {
					// Surface the failure instead of silently returning to
					// orbit — a swallowed error reads as "nothing happens".
					a.flash(fmt.Sprintf("spawn failed: %v", err))
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
			case screens.SettingsActionCycleAutosave:
				a.cycleAutosaveInterval()
			case screens.SettingsActionCycleEmptyReadings:
				a.cycleEmptyReadings()
			case screens.SettingsActionCycleRecordingWindow:
				a.cycleRecordingWindow()
			case screens.SettingsActionCancel:
				a.backToOpener()
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
				a.backToOpener()
			}
			return a, nil
		}
		// VAB (v0.24 / ADR 0029): the Vehicle Assembly builder owns its own
		// keymap (palette / stack / save / load); esc backs out to the pause
		// menu it was opened from (#501), a second esc reaches orbit.
		// Handled here so its keys don't fall through to the orbit flight
		// controls.
		if a.active == screenVAB {
			// A terminal delivers fast typing (or a paste) as one multi-rune
			// message; while a text box is open it is a run of characters,
			// not a named key, so feed them one at a time.
			if m.Type == tea.KeyRunes && len(m.Runes) > 1 && a.vab.CapturingText() {
				for _, r := range m.Runes {
					a.vab.HandleKey(string(r))
				}
				return a, nil
			}
			if a.vab.HandleKey(m.String()) == screens.VABActionCancel {
				a.backToOpener()
			}
			return a, nil
		}
		// Saves browser (v0.26 / ADR 0033 §F): list navigation, confirm
		// gates, and the name input all live in the screen; finalised
		// intents come back as SavesCommands and are dispatched against
		// the save package here. Handled before the global switch so its
		// keys (and typed save names) never fall through to flight
		// controls.
		if a.active == screenSaves {
			return a.applySavesCommand(a.saves.HandleKey(m))
		}
		// Session roster (v0.27 S6 / ADR 0034): navigation, the mint
		// input, and confirm gates live in the screen; finalised
		// intents come back as SessionCommands. Handled before the
		// global switch so typed invite handles never reach flight
		// controls.
		if a.active == screenSession {
			return a.applySessionCommand(a.session.HandleKey(a.world, m))
		}
		// Maneuver screen has its own text input that eats most keys;
		// esc-to-cancel goes through the screen's handler so it can
		// clean up.
		if a.active == screenManeuver {
			if key.Matches(m, a.keys.Back) {
				a.closeManeuverToOrbit()
				return a, nil
			}
			// ADR 0047 §3 / #428: clear-all moved off plain letters
			// entirely. `c` is a flight key on the map (the throttle target,
			// ADR 0058), so inside the planner it just points at the real
			// binding instead of clearing the plan.
			if !a.maneuver.TextFieldFocused() && m.String() == "c" {
				a.refuse("clear all", "is ctrl+k")
				return a, nil
			}
			// QUICK PLANS (ADR 0047 §4 / #428): H/I/C/K/P/R do exactly
			// what they do on the map, then leave the planner — gated
			// off whenever the Δv/throttle text field has focus so
			// typing a number is never hijacked into planting a burn.
			if !a.maneuver.TextFieldFocused() {
				switch {
				case key.Matches(m, a.keys.PlanTransfer):
					a.doPlanTransfer()
					a.closeManeuverToOrbit()
					return a, nil
				case key.Matches(m, a.keys.PlanIncl):
					a.doPlanPlaneMatch()
					a.closeManeuverToOrbit()
					return a, nil
				case key.Matches(m, a.keys.PlanCircularize):
					a.doPlanCircularize()
					a.closeManeuverToOrbit()
					return a, nil
				case key.Matches(m, a.keys.PlanRendezvous):
					// handlePlanRendezvousKey may itself route to the
					// Rendezvous Planner picker (a.active = screenOrbit
					// with the picker armed) — closeManeuverToOrbit
					// after it is still correct: same target screen,
					// and it clears the planner's own edit/cursor state.
					a.doPlanRendezvous()
					a.closeManeuverToOrbit()
					return a, nil
				case key.Matches(m, a.keys.Porkchop):
					if a.doOpenPorkchop() {
						a.maneuver.ResetEditing()
						a.world.Clock.Paused = false
					}
					return a, nil
				case key.Matches(m, a.keys.RefinePlan):
					a.doRefinePlan()
					a.closeManeuverToOrbit()
					return a, nil
				}
			}
			cmd, done := a.maneuver.HandleKey(m, a.activeCraftNodes())
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
			// esc steps back from a page to the index first (#494);
			// esc on the index and F1 anywhere close the overlay.
			if key.Matches(m, a.keys.Back) && a.help.Back() {
				return a, nil
			}
			if key.Matches(m, a.keys.Help) || key.Matches(m, a.keys.Back) {
				a.closeHelp()
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
		// Missions ladder: `1`/`2` turn on Flight School / the Challenge
		// ladder when the empty-state or per-program offer row is showing
		// (#426 item F, decision 5) — wired through the same
		// toggleMissionProgram the Settings screen uses, not a new path.
		// Sits before the generic switch (like the Porkchop/Help blocks
		// above) so these raw digits don't also fall through to the
		// switch's CraftSlot case, which — per the comment on the K case
		// below — reaches screenMissions same as screenOrbit.
		// A true toggle: the ladder shows "[N] turn on" while a program is
		// off and "[N] turn off" while it is on, so the key always does
		// what the row beside it says. The off direction is the fast opt-out
		// from Flight School (M, 1, esc) for a player who wants to fly
		// unguided; it persists like the Settings toggle.
		if a.active == screenMissions {
			switch m.String() {
			case "1":
				a.toggleMissionProgram(true)
				return a, nil
			case "2":
				a.toggleMissionProgram(false)
				return a, nil
			}
		}
		switch {
		case key.Matches(m, a.keys.Help):
			// Unreachable for Help itself (the block above and the
			// early F1 intercept own it); kept as the one fallback.
			a.openHelpHere()
			return a, nil
		case key.Matches(m, a.keys.Back):
			// Inspect (ADR 0041 §3) claims Esc first: while a highlight
			// is up, Esc means "put the map back", not "open the menu".
			// A player who just asked what something was and got their
			// answer reaches for Esc to dismiss it — landing in the
			// save/load modal instead would be a jarring non-sequitur.
			if a.active == screenOrbit && a.orbitView.Inspecting() {
				a.orbitView.InspectClear()
				return a, nil
			}
			// v0.7.3.3: Esc on the home (orbit) view opens the
			// splash menu (save / load / quit). Replaces the
			// v0.7.3.1 inline "Quit and save? [y/N]" footer prompt
			// with a centered modal. From any other screen Esc
			// returns to orbit first, so a second Esc opens the
			// menu.
			if a.active == screenOrbit {
				a.openMenu()
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
			if a.active == screenOrbit {
				// #282 sweep: same CraftVisibleHere state-guard as the
				// planning keys below — say why instead of doing
				// nothing.
				if !a.world.CraftVisibleHere() {
					a.refuse("maneuver", "vessel not in this system")
					return a, nil
				}
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
		case key.Matches(m, a.keys.Session):
			// `O` opens the multiplayer session roster (v0.27 S6 / ADR
			// 0034). Works in single-player too — the screen explains
			// how to host.
			if a.active == screenOrbit {
				a.session.Reset()
				a.active = screenSession
			}
			return a, nil
		case m.String() == "y" && a.active == screenOrbit && a.world.RendezvousInvite != nil:
			// Rendezvous Warp respond (v0.29 S2): join the pending invite
			// from the main screen — no trip to the Session screen. The
			// responder adopts the initiator's committed τ+CA verbatim off
			// the wire; the coast starts when the drive sees the mutual
			// arm. `y` is contextual: without a pending invite it stays a
			// free key. Forward-only refusal covers a τ that passed
			// between frames (the invite slate drops it next tick anyway).
			inv := a.world.RendezvousInvite
			if inv.Blocked {
				// #250: a subspace-gapped invite renders join-suppressed;
				// the key must refuse too — Engage would succeed (τ is
				// still future) but the coast could never start. Sync is
				// forward-only, so a viewer who is ahead can't come back:
				// the advice flips to the initiator Syncing forward.
				advice := "Sync to their time first"
				if inv.AheadBy > 0 {
					advice = "they must Sync to your time"
				}
				a.toast(fmt.Sprintf("can't join %s — subspace gap, %s", inv.Handle, advice))
				return a, nil
			}
			// Joining takes the COPILOT seat (ADR 0037 §2): the initiator
			// flies the pair's clock through the terminal phase and you may
			// brake it down or cancel out, never push it faster. Say so at
			// the moment the seat is taken — it is the only point where the
			// asymmetry is a choice rather than a surprise.
			if a.world.EngageRendezvousWarpAs(inv.Owner, inv.Handle, inv.Tau, inv.CA, false) {
				// ADR 0045 S7 (#400): adopt the initiator's Rendezvous Orbit
				// verbatim, same as Tau/CA above — the accepter has no code
				// path that can change it (see RendezvousArm's doc comment).
				a.world.SetRendezvousOrbit(inv.RendezvousOrbitLabel, inv.RendezvousLaps)
				if inv.Tau.IsZero() {
					a.toast(fmt.Sprintf("rendezvous agreed with %s — no plan yet; you fly copilot ([,] brakes the pair, [/] cancels)", inv.Handle))
				} else {
					a.toast(fmt.Sprintf("rendezvous with %s — coasting together; you fly copilot ([,] brakes the pair, [/] cancels)%s",
						inv.Handle, rendezvousGapNoteSuffix(a.world, inv.CA)))
				}
			} else {
				a.toast(fmt.Sprintf("can't join %s — the encounter time has passed", inv.Handle))
			}
			return a, nil
		case key.Matches(m, a.keys.WarpUp):
			// Manual warp cancels Auto-Warp, then applies (ADR 0016) —
			// Disengage leaves Selected Warp untouched so the step lands
			// from the player's own rate.
			//
			// Except during an engaged Rendezvous Warp coast (#249):
			// there the key's intent is "hurry this along", not "abandon
			// the rendezvous" — the intent asymmetry vs [/], which IS the
			// deliberate cancel. DisengageAutoWarp must clear the arm on
			// every cancel path (or DriveRendezvousWarp restarts the
			// coast next tick), and the retraction travels the wire, so
			// routing warp keys through it silently tore down the
			// rendezvous for BOTH players. The coast owns its rate
			// (#248), leaving nothing for the key to adjust — so refuse,
			// with a toast pointing at [/]. Plain node-chase / Sync
			// Auto-Warp keeps cancel-on-warp-key below.
			if a.world.RendezvousWarpEngaged() {
				a.toast("rendezvous coast owns the rate — [/] to cancel")
				return a, nil
			}
			// Copilot seat (ADR 0037 §2): warp up RELEASES the brake back
			// toward following the initiator; it can never push the pair
			// past them. Refusing here, at the input layer where the intent
			// is known, is what makes "never faster" a property rather than
			// a convention.
			if a.releaseRendezvousBrake() {
				return a, nil
			}
			a.world.DisengageAutoWarp()
			a.world.Clock.WarpUp()
			return a, nil
		case key.Matches(m, a.keys.WarpDown):
			// Same refusal as WarpUp during a rendezvous coast — see the
			// rationale above.
			if a.world.RendezvousWarpEngaged() {
				a.toast("rendezvous coast owns the rate — [/] to cancel")
				return a, nil
			}
			// Copilot seat (ADR 0037 §2): warp down BRAKES the pair — the
			// one direction the copilot's selection carries.
			if a.brakeRendezvousPair() {
				return a, nil
			}
			a.world.DisengageAutoWarp()
			a.world.Clock.WarpDown()
			return a, nil
		case key.Matches(m, a.keys.AutoWarp):
			// Toggle Auto-Warp to the globally-soonest burn.
			//
			// item-3 UX batch: pressing G with nothing eligible used to
			// no-op silently — AutoWarpEligible() already drives the
			// Launch View button's dimmed state (#445), but the raw key
			// had no equivalent. Check it up front and say why instead
			// of falling into toggleAutoWarpBurn, whose true/false
			// return is ambiguous between "just disengaged" and "engage
			// failed" (see ToggleAutoWarp's doc).
			//
			// On the pad with the launch window inside its 30 s lead
			// there is nothing to warp to: re-trim the heading to the
			// window's and say so (review #582 L3), rather than refusing.
			if a.retrimInLaunchWindowLead() {
				return a, nil
			}
			if !a.world.AutoWarpEngaged() && !a.world.AutoWarpEligible() {
				a.refuse("auto-warp", "no burn planned to warp to")
				return a, nil
			}
			a.toggleAutoWarpBurn()
			return a, nil
		case key.Matches(m, a.keys.CancelWarp):
			// Drop straight to 1× from any warp state: cancel Auto-Warp
			// and reset Selected Warp to the 1× floor (WarpIdx 0). Pause
			// state is left as-is — this stops accelerating time, it
			// doesn't resume a paused clock.
			//
			// Rendezvous Warp (v0.29 S2): `/` is also the cancel
			// affordance for an armed/coasting rendezvous. Disengaging
			// only the Auto-Warp would leave the arm set and the drive
			// would restart the coast next tick — the arm must clear too
			// (either side's cancel releases both, via the wire).
			if a.world.RendezvousArm != nil || a.world.RendezvousWarpEngaged() {
				a.world.DisengageRendezvousWarp()
				a.toast("rendezvous warp cancelled")
			}
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
		case key.Matches(m, a.keys.PanLeft):
			a.orbitView.PanLeft()
			return a, nil
		case key.Matches(m, a.keys.PanRight):
			a.orbitView.PanRight()
			return a, nil
		case key.Matches(m, a.keys.PanUp):
			a.orbitView.PanUp()
			return a, nil
		case key.Matches(m, a.keys.PanDown):
			a.orbitView.PanDown()
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
		case key.Matches(m, a.keys.PanReset):
			a.orbitView.ResetPan()
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
			a.spawn.Reset(sys.Bodies, defaultParentID, designs, sys.Scale(), sys.Name, a.world.CommBandCoverage)
			a.active = screenSpawn
			a.world.RecordAction(missions.ActionSpawnCraft) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.PlanTransfer):
			// v0.9.0+: H consumes World.Target instead of the implicit
			// body cursor. TargetCraft is the v0.9.3 rendezvous-tooling
			// surface and routes through `R` once that lands; here it
			// flashes a redirect rather than silently no-opping. #428
			// mechanical fix: TargetNone now refuses out loud too
			// (doPlanTransfer) instead of the old deliberate silence.
			a.doPlanTransfer()
			return a, nil
		case key.Matches(m, a.keys.PlanIncl):
			// v0.9.0+: I consumes World.Target — see doPlanPlaneMatch
			// for the full TargetBody / TargetCraft / None breakdown.
			//
			// Pre-v0.9 this block read App.selectedBody, the implicit
			// body cursor driven by ←/→. selectedBody now drives only
			// body-info / porkchop / SELECTED HUD pane.
			a.doPlanPlaneMatch()
			return a, nil
		case key.Matches(m, a.keys.PlanCircularize):
			// v0.9.4+: `C` plants a prograde burn at next apoapsis sized
			// to circularise. Pairs with the LAUNCH HUD's ORBIT READY
			// callout — when the player sees ORBIT READY (apoapsis is
			// in space) they press `C`, coast to apoapsis, the planted
			// node fires, periapsis rises to match apoapsis, mission
			// passes. Mirrors v0.7.4's `I` planter shape.
			a.doPlanCircularize()
			return a, nil
		case key.Matches(m, a.keys.PlanRendezvous):
			// v0.10.2+: `K` plants the recommended single-burn nudge
			// toward the current craft target. Reads from
			// world.RecommendedRendezvousBurn (Lambert intercept →
			// project onto velocity-frame axes → verify via
			// NextClosestApproach). Mirrors PlanCircularize shape; the
			// HUD's TARGET block already shows the advisory's
			// achievable-CA / Δv readouts when the gate passes.
			//
			// #282: this used to silently no-op when the active vessel
			// wasn't visible in the current system (browsing another
			// system via CycleSystem) — a session host read the dead
			// key as broken. That guard now says why.
			//
			// K is NOT screenOrbit-gated: on main the dispatcher above
			// this switch early-returns for every screen except
			// screenOrbit/screenBodyInfo/screenMissions, so those are
			// the only three active values that ever reach this case —
			// and K has always planted from all three (no
			// `a.active == screenOrbit` guard, unlike its sibling
			// planning keys). Adding one here would silently narrow a
			// working keybinding rather than fix a silent no-op; see
			// #282 review discussion.
			//
			// ADR 0045 S6 (#399): K became modal — a close, near-matched
			// pair still plants directly (unchanged from v0.10.2); a
			// phase-mismatched pair opens the Rendezvous Planner picker
			// instead of refusing. See doPlanRendezvous / handlePlanRendezvousKey.
			a.doPlanRendezvous()
			return a, nil
		case key.Matches(m, a.keys.Porkchop):
			// #282 sweep: on the orbit screen this key was silently
			// ignored without a visible target or a vessel to plan
			// from — same state-guard shape as the K/E fix, so it gets
			// the same one-phrase treatment.
			// #495 (B3): also reachable from body info, where [t] then
			// [P] plots the shown body. Other screens never get here.
			if a.active == screenOrbit || a.active == screenBodyInfo {
				a.doOpenPorkchop()
			}
			return a, nil
		// v0.8.6: ClearNodes global binding retired — clear-all now
		// lives in the maneuver-form footer (`m` then ctrl+k).
		case key.Matches(m, a.keys.Save):
			a.flashStatus("save", a.doSave())
			return a, nil
		case key.Matches(m, a.keys.Load):
			// item-3 UX batch (reverses ADR 0033 §H): arm the same y/n
			// confirm the Saves browser's Load already uses instead of
			// loading instantly — but only when there's actually a
			// quicksave to discard-into. A guest is checked first
			// (review finding 2): a.quicksaveExists() only reads the
			// LOCAL saves dir, which can be stale from a prior solo
			// session on this same machine even though a guest's F9
			// can never succeed (doLoad's errGuestSaves); the menu's
			// own Load path already orders the guest check first for
			// the same reason. An empty local lane (solo, no F5 yet)
			// surfaces straight to doLoad's own "no quicksave" /
			// "resume autosave" message — confirming a load that can
			// only fail teaches nothing.
			if a.guestSave != nil || !a.quicksaveExists() {
				a.flashStatus("load", a.doLoad())
				return a, nil
			}
			a.askQuickload()
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
		case key.Matches(m, a.keys.Inspect):
			// Inspect (ADR 0041 §3 / #346): step the identity highlight
			// through what the map drew this frame. Orbit screen only —
			// the highlight is a property of that map, and the set it
			// steps through is rebuilt by that map's own render.
			if a.active == screenOrbit {
				a.orbitView.InspectNext()
			}
			return a, nil
		case key.Matches(m, a.keys.InspectCommit) && a.active == screenOrbit && a.orbitView.Inspecting():
			// Enter commits the inspected entity as Target — the body
			// Cursor's hover→commit contract, generalised. Gated on
			// Inspecting so Enter is otherwise still a free key here.
			a.commitInspect()
			return a, nil
		case key.Matches(m, a.keys.JumpToLaunchView):
			// Launch/surface toggling jump key (issue #348 §4, mirrors
			// `o`'s ProximityView below): press to enter a fresh
			// chase-cam session framed for the moment, press again to
			// return to the map exactly as it was left. Orbit screen
			// only, same reasoning as ProximityView — it's a ViewMode of
			// that screen's canvas, not a screen of its own. Leaving
			// reuses releaseLaunchSession, whose LastLaunchReleaseEvent
			// already surfaces the "launch view off, back on the X view" toast
			// on the next Tick (the same plumbing the switch-triggered
			// release uses), so only entry and refusal need a toast here.
			if a.active == screenOrbit {
				entered, refusal := a.world.ToggleLaunchView()
				if refusal != "" {
					a.toast("launch view — " + refusal)
				} else if entered {
					a.toast("launch view — [V] returns to the map")
				}
			}
			return a, nil
		case key.Matches(m, a.keys.ProximityView):
			// Proximity View (ADR 0043 / #348): toggle into and out of the
			// target-centred close-range picture. Orbit screen only — it
			// is a ViewMode of that screen's canvas, not a screen of its
			// own. Every branch says something: a refused jump that
			// silently did nothing would read as a broken key, which is
			// exactly how the last one of those was reported.
			if a.active == screenOrbit {
				entered, refusal := a.world.ToggleProximityView()
				switch {
				case refusal != "":
					a.toast("proximity view — " + refusal)
				case entered:
					a.toast(fmt.Sprintf("proximity view — %s centred; [o] returns to the map", a.world.TargetName()))
				default:
					a.toast("proximity view off — back on the map")
				}
			}
			return a, nil
		case key.Matches(m, a.keys.EndFlight):
			// v0.11.4+ (ADR 0004): open the end-flight confirm prompt
			// when the active vessel is Crashed. The y/n intercept at
			// the top of the KeyMsg branch handles commit / cancel
			// once the prompt is open; this case is the open trigger.
			// #282: a live vessel used to swallow this key with no
			// feedback at all — a tester read that as the binding
			// being broken or host-gated. Every branch now says
			// something.
			switch c := a.world.ActiveCraft(); {
			case c == nil:
				a.refuse("end flight", "no vessel")
			case c.Crashed:
				a.askEndFlight(c.Name)
			default:
				a.refuse("end flight", "vessel is not wreckage")
			}
			return a, nil
		case key.Matches(m, a.keys.RefinePlan):
			a.doRefinePlan()
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
		case key.Matches(m, a.keys.ThrottleTarget):
			if t, ok := a.world.CycleThrottleTarget(); ok {
				a.flash("throttle: " + t.String())
			} else {
				a.flash("one throttle")
			}
			return a, nil
		case key.Matches(m, a.keys.ThrottleUp):
			a.world.AdjustThrottle(0.1)
			a.world.RecordAction(missions.ActionThrottleUp) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.ThrottleDown):
			a.world.AdjustThrottle(-0.1)
			a.world.RecordAction(missions.ActionThrottleDown) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.ThrottleUpFine):
			a.world.AdjustThrottle(0.01)
			a.world.RecordAction(missions.ActionThrottleUp) // ADR 0025 §7
			return a, nil
		case key.Matches(m, a.keys.ThrottleDownFine):
			a.world.AdjustThrottle(-0.01)
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
			wasBurning := a.world.ActiveCraft() != nil && a.world.ActiveCraft().ManualBurn != nil
			a.world.ToggleManualBurn()
			a.world.RecordAction(missions.ActionToggleBurn) // ADR 0025 §7
			// Ignite only when the engine actually lit (not on a cut, and
			// not when throttle/fuel/command refused it): Flight School's
			// lift-off rung waits on it (#519).
			if c := a.world.ActiveCraft(); c != nil && !wasBurning && c.ManualBurn != nil {
				a.world.RecordAction(missions.ActionIgnite)
			}
			return a, nil
		case key.Matches(m, a.keys.CycleEngine):
			a.world.CycleEngineMode()
			return a, nil
		case key.Matches(m, a.keys.CycleRCSScale):
			a.world.CycleRCSPulseScale()
			if c := a.world.ActiveCraft(); c != nil {
				if c.EngineMode == spacecraft.EngineRCS {
					a.flash(fmt.Sprintf("rcs pulse %g m/s", c.RCSPulseDV()))
				} else {
					a.flash(fmt.Sprintf("rcs pulse %g m/s (press r for rcs)", c.RCSPulseDV()))
				}
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
			// first byte of the key string is the digit.
			//
			// item-3 UX batch: an empty-slot digit used to swallow the
			// key with no feedback — say why. Checked directly against
			// len(Crafts) rather than SwitchToCraftIdx's bool return,
			// which is false for the *already on that slot* case too
			// (a harmless redundant press, not a "no vessel there" —
			// refusing that would be a wrong message, not just a
			// missing one).
			digit := int(m.String()[0] - '0')
			idx := digit - 1
			if idx >= len(a.world.Crafts) {
				a.refuse("vessel", fmt.Sprintf("no vessel in slot %d", digit))
				return a, nil
			}
			a.world.SwitchToCraftIdx(idx)
			return a, nil
		case key.Matches(m, a.keys.Undock):
			// v0.28 S5: while docked-as-guest (one of my craft rides in
			// another player's stack), U signals that stack's owner to split
			// MY component and hand it back — undock-anytime, never gated
			// behind the host. Emitted up to the serve reporting wrapper
			// (which owns the dock ledger); a no-op in solo play.
			if a.world.DockGuest != nil {
				return a, func() tea.Msg { return UndockGuestMsg{} }
			}
			// ADR 0040 §3: from the OWNER seat of a cross-player stack, [U]
			// releases the guest's component through the ledger. Before this
			// the key simply refused here, so a guest who disconnected while
			// docked left the docker holding a composite with no way out from
			// either seat (#312). The structural exception stands (§5): after
			// a control transfer the other party's components sit at the
			// bottom, where a peel would swap the two players' vehicles —
			// GuestReleaseRefusal says so and names the two-step.
			if active := a.world.ActiveCraft(); sim.StackHasGuest(active) {
				if reason := a.world.GuestReleaseRefusal(a.world.ActiveCraftIdx); reason != "" {
					a.flash(reason)
					return a, nil
				}
				a.flash("releasing your partner's vessel…")
				return a, func() tea.Msg { return ReleaseGuestMsg{} }
			}
			// #308: say why when it refuses. The key is bound and the stack is
			// real, so a bare no-op reads as a broken game — and the commonest
			// refusal (a cross-player stack) has an answer the player can act
			// on. UndockRefusal is exhaustive over Undock's false paths, so
			// there is no third branch that falls through in silence.
			if reason := a.world.UndockRefusal(a.world.ActiveCraftIdx); reason != "" {
				a.flash(reason)
			} else if idx := a.world.ActiveCraftIdx; a.world.IsBatch(idx) {
				// ADR 0059 decision 3: two or more payloads above the lead is a
				// batch. Refuse first (stack along the velocity), then ask.
				if reason := sim.BatchFanRefusal(a.world.Crafts[idx]); reason != "" {
					a.flash(reason)
				} else if name, n, note, ok := a.world.BatchReleaseCard(idx); ok {
					a.askReleaseBatch(name, n, note)
				}
			} else if before := len(a.world.Crafts); a.world.Undock(a.world.ActiveCraftIdx) {
				// The stack becomes its pieces: the slate grows by pieces-1 (#709).
				a.flash(fmt.Sprintf("undocked into %d components", len(a.world.Crafts)-before+1))
				a.world.RecordAction(missions.ActionUndock) // ADR 0025 §7
			}
			return a, nil
		case key.Matches(m, a.keys.HandOverCargo):
			// ADR 0057 decision 5.4: u hands every cargo load aboard into the
			// stack's hold. Riding in another player's stack (conflict 2a) the
			// composite is not in this World: the ask goes up to the serve
			// wrapper, and the answer comes back as a session chip. Otherwise
			// the sim states the outcome in three facts or the mass moved.
			if a.world.DockGuest != nil {
				return a, func() tea.Msg { return HandOverCargoMsg{} }
			}
			if kg, refusal := a.world.HandOverCargo(a.world.ActiveCraftIdx); refusal != "" {
				a.flash(refusal)
			} else {
				a.flash(sim.CargoHandedOverText(kg))
			}
			return a, nil
		case key.Matches(m, a.keys.ReArmDock):
			// #372: the explicit "yes, I meant it" for a same-World docking
			// pair a localReArm latch is holding apart — the way out that
			// isn't a 200 m round trip or a 10-minute wait. Changes nothing
			// else: local docking stays automatic, so a pair already inside
			// both proximity gates fuses on the very next tick once the
			// latch clears, exactly as an un-latched approach would.
			if partners, ok := a.world.ReArmDocking(); ok {
				a.flash("re-armed docking with " + strings.Join(partners, ", "))
			} else {
				// A press with no latch held must say so rather than no-op
				// silently — a dead keypress reads as a broken key (v0.30
				// lesson).
				a.flash("docking: no re-arm latch held")
			}
			return a, nil
		case key.Matches(m, a.keys.TransferControl):
			// v0.28 S5, ADR 0034 §6: hand a cross-player stack I own to the
			// guest — instant, roles swap, refused mid-burn (the ledger
			// enforces the refusal). Only meaningful while flying a stack that
			// carries a guest and I'm not myself the guest.
			// ADR 0040 §4: from the guest seat the same key asks for the
			// stick back, and against an owner whose session is not live the
			// ledger grants it outright. The wrapper decides which of the two
			// meanings applies — it is the side that knows who is connected.
			if a.world.DockGuest != nil {
				return a, func() tea.Msg { return TransferControlMsg{} }
			}
			if active := a.world.ActiveCraft(); active != nil && sim.StackHasGuest(active) {
				return a, func() tea.Msg { return TransferControlMsg{} }
			}
			a.flash("transfer: not flying a cross-player stack")
			return a, nil
		case key.Matches(m, a.keys.SetOrbitAltitude):
			a.startOrbitAltitude()
			return a, nil
		case key.Matches(m, a.keys.Deploy):
			// v0.23 / ADR 0028: release the top carried payload, keep flying the
			// carrier (drop-and-continue). ADR 0052 decision 7: ask first, but
			// only when Deploy would go ahead (refusal first, then the ask).
			// World.Deploy self-records the deploy action (so any caller emits
			// it) — no second RecordAction here.
			name, ok := a.world.DeployPayloadName(a.world.ActiveCraftIdx)
			if !ok {
				a.flash("deploy: no payload to release (carrier carries no docked payload)")
				return a, nil
			}
			a.askDeploy(name)
			return a, nil
		case key.Matches(m, a.keys.Transpose):
			// v0.12 / ADR 0009: one-shot Apollo transposition. Reorders
			// the [Descent, Ascent, SM, CM] stack so the SM fires (LOI/
			// TEI) with the LM as a docked nose payload released via U.
			// ADR 0052 decision 7: ask first, but refuse before asking.
			switch err := a.world.TransposeRefusal(a.world.ActiveCraftIdx); {
			case err == nil:
				a.askTranspose()
			case errors.Is(err, sim.ErrTransposeNotReady):
				a.flash("transpose: drop the launch vehicle first (stack must be Descent/Ascent/SM/CM)")
			default:
				a.flash(fmt.Sprintf("transpose failed: %v", err))
			}
			return a, nil
		case key.Matches(m, a.keys.CycleTarget):
			if a.active == screenBodyInfo {
				// #495 (B3): on body info `t` aims at the body SHOWN, not
				// the nearest-first cycle step. A deliberate per-screen
				// meaning; the map's cycle is unchanged.
				a.doTargetShownBody()
				return a, nil
			}
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
			a.flash(fmt.Sprintf("nav: %s", nav))
			return a, nil
		case key.Matches(m, a.keys.ToggleInstantSAS):
			a.world.ToggleInstantSAS()
			a.flash(fmt.Sprintf("SAS: %s", sasModeLabel(a.world.InstantSAS)))
			return a, nil
		case key.Matches(m, a.keys.AttitudeSurfacePrograde):
			a.handleAttitudeKey(spacecraft.BurnSurfacePrograde)
			return a, nil
		case key.Matches(m, a.keys.AttitudeSurfaceRetrograde):
			a.handleAttitudeKey(spacecraft.BurnSurfaceRetrograde)
			return a, nil
		case key.Matches(m, a.keys.AttitudeInertial):
			// A set-hold even with RCS armed: an inertial "pulse" has no direction.
			if a.world.ActiveCraft() == nil {
				a.refuse("attitude", "no vessel")
				return a, nil
			}
			a.world.SetAttitudeMode(spacecraft.BurnInertial)
			return a, nil
		case key.Matches(m, a.keys.PitchTrimEast):
			// Arrows trim (ADR 0052): only on the flight screen. Body info
			// and the mission ladder share this switch, and an arrow there
			// must not steer a vessel the player cannot see.
			if a.active != screenOrbit {
				return a, nil
			}
			if a.world.NudgeInertialHold(-spacecraft.HeadingTrimStepRad, 0) { // ADR 0063: under Inertial → turns the held nose right
				return a, nil
			}
			if a.portTrim(0, +spacecraft.PortTrimStepRad) { // ADR 0055: PORT turns the nose across the port face
				return a, nil
			}
			if c := a.world.ActiveCraft(); c != nil {
				c.PitchTrim += spacecraft.PitchTrimStepRad
			}
			return a, nil
		case key.Matches(m, a.keys.PitchTrimWest):
			// Arrows trim (ADR 0052): only on the flight screen. Body info
			// and the mission ladder share this switch, and an arrow there
			// must not steer a vessel the player cannot see.
			if a.active != screenOrbit {
				return a, nil
			}
			if a.world.NudgeInertialHold(+spacecraft.HeadingTrimStepRad, 0) {
				return a, nil
			}
			if a.portTrim(0, -spacecraft.PortTrimStepRad) {
				return a, nil
			}
			if c := a.world.ActiveCraft(); c != nil {
				c.PitchTrim -= spacecraft.PitchTrimStepRad
			}
			return a, nil
		case key.Matches(m, a.keys.HeadingTrimNorth):
			if a.active != screenOrbit { // see PitchTrimEast above
				return a, nil
			}
			// ADR 0049 decision 9 (key moved to ↑ by ADR 0052): offsetRad IS
			// the desired bearing shift away from due east (see ApplyHeadingTrim's doc comment):
			// increasing bearing sweeps east(090°) -> south(180°) ->
			// west(270°) -> north(360°=0°), so nudging TOWARD north from
			// due east means DECREASING the offset. Swapping this sign
			// with HeadingTrimSouth's is exactly the risk
			// TestHeadingArrowsReadTheCorrectCompassDirection
			// sabotage-proves in app_same_keys_test.go.
			if a.world.NudgeInertialHold(0, +spacecraft.PitchTrimStepRad) {
				return a, nil
			}
			if a.portTrim(+spacecraft.PortTrimStepRad, 0) { // ↑ tips the nose up across the port face
				return a, nil
			}
			if c := a.world.ActiveCraft(); c != nil {
				c.HeadingTrim -= spacecraft.HeadingTrimStepRad
			}
			return a, nil
		case key.Matches(m, a.keys.HeadingTrimSouth):
			// Arrows trim (ADR 0052): only on the flight screen. Body info
			// and the mission ladder share this switch, and an arrow there
			// must not steer a vessel the player cannot see.
			if a.active != screenOrbit {
				return a, nil
			}
			if a.world.NudgeInertialHold(0, -spacecraft.PitchTrimStepRad) {
				return a, nil
			}
			if a.portTrim(-spacecraft.PortTrimStepRad, 0) {
				return a, nil
			}
			if c := a.world.ActiveCraft(); c != nil {
				c.HeadingTrim += spacecraft.HeadingTrimStepRad
			}
			return a, nil
		case key.Matches(m, a.keys.PitchTrimEastFine, a.keys.PitchTrimWestFine,
			a.keys.HeadingTrimNorthFine, a.keys.HeadingTrimSouthFine):
			// #460 (G6 Q6b, ADR 0052 amendment): alt makes any trim fine,
			// 1° per press. Same flight-screen-only gate and same signs as
			// the plain arrows above.
			if a.active != screenOrbit {
				return a, nil
			}
			// ADR 0055: in PORT alt is 0.25° across the port face. Only the
			// four arrow chords reach here (alt+f / alt+b are the
			// emacs-style aliases of alt+→ / alt+←, same keys, same effect).
			hf, pf := spacecraft.HeadingTrimFineStepRad, spacecraft.PitchTrimFineStepRad
			var yaw, pitch float64
			switch {
			case key.Matches(m, a.keys.PitchTrimEastFine):
				yaw = -hf
			case key.Matches(m, a.keys.PitchTrimWestFine):
				yaw = hf
			case key.Matches(m, a.keys.HeadingTrimNorthFine):
				pitch = pf
			default:
				pitch = -pf
			}
			if a.world.NudgeInertialHold(yaw, pitch) { // ADR 0063: alt = fine, under Inertial too
				return a, nil
			}
			step := spacecraft.PortTrimFineStepRad
			switch {
			case key.Matches(m, a.keys.PitchTrimEastFine):
				if a.portTrim(0, +step) {
					return a, nil
				}
			case key.Matches(m, a.keys.PitchTrimWestFine):
				if a.portTrim(0, -step) {
					return a, nil
				}
			case key.Matches(m, a.keys.HeadingTrimNorthFine):
				if a.portTrim(+step, 0) {
					return a, nil
				}
			case key.Matches(m, a.keys.HeadingTrimSouthFine):
				if a.portTrim(-step, 0) {
					return a, nil
				}
			}
			if c := a.world.ActiveCraft(); c != nil {
				switch {
				case key.Matches(m, a.keys.PitchTrimEastFine):
					c.PitchTrim += spacecraft.PitchTrimFineStepRad
				case key.Matches(m, a.keys.PitchTrimWestFine):
					c.PitchTrim -= spacecraft.PitchTrimFineStepRad
				case key.Matches(m, a.keys.HeadingTrimNorthFine):
					c.HeadingTrim -= spacecraft.HeadingTrimFineStepRad
				case key.Matches(m, a.keys.HeadingTrimSouthFine):
					c.HeadingTrim += spacecraft.HeadingTrimFineStepRad
				}
			}
			return a, nil
		case key.Matches(m, a.keys.PitchTrimReset):
			// ADR 0049 decision 9 widened this to reset both trims.
			if c := a.world.ActiveCraft(); c != nil {
				c.PitchTrim = 0
				c.HeadingTrim = 0
				c.PortPitchTrim = 0 // ADR 0055: | clears whichever pair is live
				c.PortYawTrim = 0
			}
			return a, nil
		case key.Matches(m, a.keys.Stage):
			// v0.9.1+: drop the bottom stage of the active craft.
			// Inside the maneuver form, space is the iterate-toggle
			// (v0.8.6.3) — that path doesn't reach here because the
			// maneuver form intercepts keys before app.update.
			//
			// #427 / ADR 0048: name the DROPPED STAGE in the Event Flash,
			// not the jettisoned wreckage craft's disambiguated slate name
			// (nextCraftName always appends "-N", so "S-IC" would read
			// "S-IC-1", "S-IC-2", ... across a playthrough — the loadout's
			// bare stage name is stable and matches the issue's own
			// example, "dropped S-IC, 2 stages left"). Captured before
			// the call since StageActive reslices Stages out from under
			// the pre-drop bottom entry.
			droppedName := ""
			if c := a.world.ActiveCraft(); c != nil && len(c.Stages) > 0 {
				droppedName = c.Stages[0].Name
			}
			_, _, err := a.world.StageActive(a.world.ActiveCraftIdx)
			switch {
			case err == nil:
				a.world.RecordAction(missions.ActionStage) // ADR 0025 §7
				// The number that matters is how many stages remain to
				// fly, not the jettisoned craft's rename (the review's
				// "renames the vessel with no message" finding).
				remaining := 0
				if c := a.world.ActiveCraft(); c != nil {
					remaining = len(c.Stages)
				}
				stageWord := "stages"
				if remaining == 1 {
					stageWord = "stage"
				}
				a.flash(fmt.Sprintf("dropped %s, %d %s left", droppedName, remaining, stageWord))
				// v0.12 / ADR 0009: surface the transposition hint the
				// moment a stage drop leaves the Apollo stack in the
				// pre-transposition shape [Descent, Ascent, SM, CM] — the
				// player needs to know `D` flips the SM to the firing core
				// (or stage once more to drop the LM for a manual flip).
				// Without this the wrong-engine state is silent.
				if sim.TransposeReady(a.world.ActiveCraft()) {
					a.flash("TRANSPOSE READY: press D to flip (SM → firing core; LM becomes nose payload)")
				}
			case errors.Is(err, sim.ErrStageOnlyOne):
				// v0.12 Slice 3 (ADR 0008): once the vessel is reduced to
				// its bare chute-bearing stage, the staging-no-op press
				// arms the parachute instead — "just another staging
				// action," no new keybinding. Falls through to the normal
				// no-op flash when there's no stowed chute to arm.
				if c := a.world.ActiveCraft(); c != nil && c.ArmParachute() {
					a.flash("parachute ARMED — auto-deploys in atmosphere")
				} else {
					a.flash("stage: cannot drop the only remaining stage")
				}
			case errors.Is(err, sim.ErrStageSolidsLit):
				a.flash(err.Error()) // ADR 0058: the bare refusal, "boosters still burning"
			default:
				a.flash(fmt.Sprintf("stage failed: %v", err))
			}
			return a, nil
		case key.Matches(m, a.keys.TiltUp), key.Matches(m, a.keys.TiltDown):
			// v0.10.6+: nudge ViewTilted's polar tilt θ ±5°.
			//
			// item-3 UX batch: outside ViewTilted this used to no-op
			// silently, on the theory that a stray shift+arrow in
			// ViewTop shouldn't blast a misleading flash — but the
			// review found the opposite: plain ↑ pans in every view, so
			// the silence on shift+↑ reads as "the modifier is broken",
			// not "not now". Refuse out loud like every sibling guard.
			if a.world.ViewMode != sim.ViewTilted {
				a.refuse("tilt", "only in the tilted view, [v] cycles")
				return a, nil
			}
			delta := sim.ViewTiltThetaStep
			if key.Matches(m, a.keys.TiltDown) {
				delta = -sim.ViewTiltThetaStep
			}
			theta := a.world.NudgeViewTiltTheta(delta)
			a.flash(fmt.Sprintf("view: Tilted %g°", theta))
			return a, nil
		case key.Matches(m, a.keys.YawLeft), key.Matches(m, a.keys.YawRight):
			// ADR 0021 G: nudge ViewTilted's yaw φ ±5°, wrapping at
			// 360° (no clamp — yaw is a full turn around the orbit).
			//
			// item-3 UX batch: same fix as the tilt keys above — refuse
			// out loud outside ViewTilted instead of the old silent
			// no-op. (Keys moved to { } by ADR 0052.)
			if a.world.ViewMode != sim.ViewTilted {
				a.refuse("yaw", "only in the tilted view, [v] cycles")
				return a, nil
			}
			delta := sim.ViewTiltPhiStep
			if key.Matches(m, a.keys.YawLeft) {
				delta = -sim.ViewTiltPhiStep
			}
			phi := a.world.NudgeViewTiltPhi(delta)
			a.flash(fmt.Sprintf("view: yaw %g°", phi))
			return a, nil
		}
	}
	return a, nil
}

// doSave writes the current world to the quicksave lane — F5 always
// targets quicksave.json (v0.26 / ADR 0033 §D), so it can never touch
// a named save.
func (a *App) doSave() error {
	if a.guestSave != nil {
		return errGuestSaves
	}
	return save.WriteQuicksave(a.world)
}

// errGuestSaves refuses the local Saves surface in a guest session —
// the per-player payload autosaves server-side instead (v0.27 S3).
var errGuestSaves = errors.New("no local saves in a session — your program autosaves server-side")

// errGuestSettings refuses the Settings/Controls screens in a guest
// session — they write the host machine's settings file (v0.27 review
// follow-up; per-guest settings are a later cycle).
var errGuestSettings = errors.New("settings belong to the host in a session")

// doLoad replaces the live world with the quicksave lane. F9 used to
// stay instant with no confirm (ADR 0033 §H); the item-3 UX batch
// reversed that (see the quickload card and the ADR's
// Amendments section) — this func is now only reached after that
// confirm, or when the lane is empty and there's nothing to discard.
// An empty lane (no F5 yet) is surfaced as a clear "no quicksave"
// message rather than a raw file-not-found; failures leave the
// existing world untouched.
//
// When there is no quicksave but an autosave IS on disk (the common
// quit → relaunch → F9 case, whose quit state landed in the autosave
// ring, not the quicksave lane), the message points the player at the
// Saves browser instead of dead-ending — the signpost fix for
// batch-review finding 5. F9 deliberately does NOT auto-load the
// autosave: a stray mid-session F9 must never silently discard live
// progress, so resuming an autosave stays an explicit menu action.
func (a *App) doLoad() error {
	if a.guestSave != nil {
		return errGuestSaves
	}
	err := a.loadWorldByID(save.QuicksaveID)
	if errors.Is(err, fs.ErrNotExist) {
		if a.hasResumableAutosave() {
			return errors.New("no quicksave — press Esc → Load to resume your autosave")
		}
		return errors.New("no quicksave yet — press F5 first")
	}
	return err
}

// hasResumableAutosave reports whether the saves directory holds at
// least one readable autosave-ring entry — the state F9 points the
// player at when the quicksave lane is empty (finding 5).
func (a *App) hasResumableAutosave() bool {
	infos, err := save.List()
	if err != nil {
		return false
	}
	for _, in := range infos {
		if in.Lane == save.LaneAutosave && !in.Unreadable {
			return true
		}
	}
	return false
}

// quicksaveExists reports whether the quicksave lane holds a readable
// entry — gates whether F9 raises the quickload card (item-3 UX batch) or
// falls straight through to doLoad's own empty-lane message.
func (a *App) quicksaveExists() bool {
	infos, err := save.List()
	if err != nil {
		return false
	}
	for _, in := range infos {
		if in.Lane == save.LaneQuicksave && !in.Unreadable {
			return true
		}
	}
	return false
}

// loadWorldByID hydrates the save id and swaps it in as the live
// world — the single world-swap path shared by F9 quickload and the
// Saves-screen browser load (post-confirm). Failures leave the
// existing world untouched.
func (a *App) loadWorldByID(id string) error {
	w, err := save.LoadID(id)
	if err != nil {
		return err
	}
	a.world, a.resumed = w, true
	// The loaded world starts with the nil ("all enabled") default; re-apply
	// the player's program toggles so a load respects them. v0.21 Slice 7.
	a.world.SetEnabledMissionPrograms(enabledProgramsFromSettings(a.orbitView.Settings()))
	a.world.SetRecordingMarker("loaded save " + id)
	a.attachRecording(w)
	a.fromMenu = false // a load lands on the map, never back on the menu (Q3)
	a.menuHeld = false // the new world's own pause state applies
	a.active = screenOrbit
	return nil
}

// appCard is the one card the App has up (ADR 0054, #633): the shared
// screens.Card plus what only the App knows. answer is called once, with the
// card already closed, with the ID of the row that was finally picked.
type appCard struct {
	*screens.Card
	kind   cardKind
	answer func(a *App, id string) (tea.Model, tea.Cmd)
	// stamp is what the card was raised about (screen, world, active
	// vessel) for the asks that reshape the vessel; dropStaleAsks closes
	// the card when it changes, so a later y never commits against a view
	// the player was not looking at (review LOW 122/123).
	stamp   askContext
	stamped bool
}

type cardKind int

const (
	cardQuit cardKind = iota + 1
	cardEndFlight
	cardTranspose
	cardDeploy
	cardReleaseBatch
	cardRaise
	cardStopRaise
	cardSpread
	cardStopSpread
	cardQuickload
)

// card row IDs for the flight asks.
const (
	rowYes = "yes"
	rowOut = "out"
)

// outRow is the way-out row every flight card ends on: n, N and esc.
func outRow(label string) screens.CardRow {
	return screens.CardRow{ID: rowOut, Label: label, Key: "n  esc", Letters: []string{"n", "N", "esc"}, Out: true}
}

// yesRow is the first, action row: y or Y.
func yesRow(label string) screens.CardRow {
	return screens.CardRow{ID: rowYes, Label: label, Key: "y", Letters: []string{"y", "Y"}}
}

// raiseCard puts a card up on the App, replacing any card already up.
func (a *App) raiseCard(kind cardKind, c *screens.Card, answer func(a *App, id string) (tea.Model, tea.Cmd)) *appCard {
	a.card = &appCard{Card: c, kind: kind, answer: answer}
	return a.card
}

// handleCardKey answers the card that is up. ↑/↓ (and j/k) move the ▸ and
// enter picks the highlighted row; each row's own letters pick it directly;
// every other key is swallowed, so no flight key slips through mid-question.
// An action row asks once more before it acts (call 1); the way-out row acts
// at once.
func (a *App) handleCardKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A flight ask raised before the window shrank below the design size is
	// not answerable blind; the quit card still is (ctrl+c is honoured from
	// the size gate, and so is its way out).
	if a.card.kind != cardQuit && a.sizeGated() {
		return a, nil
	}
	switch s := m.String(); s {
	case "up", "k":
		a.card.Up()
	case "down", "j":
		a.card.Down()
	case "enter":
		return a.cardPicked(a.card.Enter())
	default:
		if row, done, ok := a.card.Letter(s); ok {
			return a.cardPicked(row, done)
		}
	}
	return a, nil
}

// cardPicked runs a pick's result: nothing yet when the card only moved to its
// second step; otherwise the card closes and its answer runs.
func (a *App) cardPicked(row int, done bool) (tea.Model, tea.Cmd) {
	if !done {
		return a, nil
	}
	c := a.card
	id := c.RowID(row)
	a.card = nil
	return c.answer(a, id)
}

// cardOrigin is where a card of the given size sits: centred under the Title
// Row, one cell short of the dimmed screen all round (cardOver draws it there,
// the mouse route hit-tests against it).
func (a *App) cardOrigin(cardW, cardH int) (x, y int) {
	x = (a.width - cardW) / 2
	if x < 0 {
		x = 0
	}
	y = 1 + (a.height-1-cardH)/2
	if y < 1 {
		y = 1
	}
	return x, y
}

// armQuit raises the quit card (#474 prompt, drawn as a card since
// 2026-10-05) on its first row, worded for a host or a guest. Every quit path
// (ctrl+c, the menu's Quit row) lands here. A guest can't decline
// (persistMiddleware writes their flight on session unwind no matter how the
// App exits), so their card offers only quit and stay, never a "no".
func (a *App) armQuit() {
	a.raiseCard(cardQuit, screens.NewQuitCard(a.screensTheme(), a.guestSave != nil), (*App).answerQuit)
}

// answerQuit answers both quit cards (#699). Save card: Yes autosaves into the
// ring and quits; No raises Are you sure?; Back returns the player to exactly
// where they were, touching nothing. Are you sure?: Yes quits writing
// nothing; No returns to the Save card; Back as above. The guest's card
// shares the Save and Stay ids.
func (a *App) answerQuit(id string) (tea.Model, tea.Cmd) {
	switch id {
	case screens.QuitRowSave:
		// The Saves browser freezes the clock while it's up (finding 4);
		// that freeze is transient UI state, not a gameplay pause, so
		// don't let a quit-from-browser persist Paused=true into the
		// autosave — restore the pre-open state first so the reloaded
		// session resumes exactly as it was running.
		if a.active == screenSaves {
			a.world.Clock.Paused = a.savesPrevPaused
		}
		// Likewise the pause menu's own hold (B11 / G9 Q6): quitting from
		// the menu must save the flight as it was running, not as a frozen
		// world that autosave's paused-world guard would skip.
		if a.menuHeld {
			a.world.Clock.Paused = a.menuPrevPaused
		}
		a.autosave()
		a.world.CloseRecording()
		return a, tea.Quit
	case screens.QuitRowNoSave:
		// Save before quitting? No: ask once more (#699).
		a.raiseCard(cardQuit, screens.NewQuitSureCard(a.screensTheme()), (*App).answerQuit)
	case screens.QuitRowSure:
		a.world.CloseRecording()
		return a, tea.Quit // quit writing nothing
	case screens.QuitRowUnsure:
		a.armQuit()
	}
	// QuitRowStay (Back on either card): the card is already closed, which
	// is the return to where the quit was opened.
	return a, nil
}

// askEndFlight raises the end-flight card (E on a crashed vessel).
func (a *App) askEndFlight(name string) {
	a.raiseCard(cardEndFlight, screens.NewCard(a.screensTheme(), "End flight for "+name+"?", "",
		[]screens.CardRow{yesRow("remove the wreckage"), outRow("keep it")}, 0),
		func(a *App, id string) (tea.Model, tea.Cmd) {
			if id == rowYes && a.world.EndFlightActive() {
				a.flash("● END FLIGHT — wreckage removed")
			}
			return a, nil
		})
}

// askTranspose raises the transpose card. The verb is re-checked at commit
// time through the same sim method, so a state change between ask and answer
// (a tick that moved the stack) still refuses honestly. The way out is under
// the ▸: the stack has no undo.
func (a *App) askTranspose() {
	c := a.raiseCard(cardTranspose, screens.NewCard(a.screensTheme(), "Transpose?",
		"SM becomes the firing core, LM rides as a nose payload",
		[]screens.CardRow{yesRow("transpose"), outRow("cancel")}, 1),
		func(a *App, id string) (tea.Model, tea.Cmd) {
			if id != rowYes {
				return a, nil
			}
			switch err := a.world.Transpose(a.world.ActiveCraftIdx); {
			case err == nil:
				a.world.RecordAction(missions.ActionTranspose) // ADR 0025 §7
				a.flash("transposed: SM is firing core, press U to release the LM")
			case errors.Is(err, sim.ErrTransposeNotReady):
				a.flash("transpose: drop the launch vehicle first (stack must be Descent/Ascent/SM/CM)")
			default:
				a.flash(fmt.Sprintf("transpose failed: %v", err))
			}
			return a, nil
		})
	c.stamp, c.stamped = a.currentAskContext(), true
}

// askDeploy raises the deploy card for the named payload (Y). Like transpose,
// it has no undo, so the ▸ opens on the way out.
func (a *App) askDeploy(name string) {
	c := a.raiseCard(cardDeploy, screens.NewCard(a.screensTheme(), "Deploy "+name+"?",
		"it flies on as its own vessel",
		[]screens.CardRow{yesRow("deploy"), outRow("cancel")}, 1),
		func(a *App, id string) (tea.Model, tea.Cmd) {
			if id != rowYes {
				return a, nil
			}
			if a.world.Deploy(a.world.ActiveCraftIdx) {
				a.flash(fmt.Sprintf("deployed payload, %d vessels on the slate", len(a.world.Crafts)))
			} else {
				a.flash("deploy: no payload to release (carrier carries no docked payload)")
			}
			return a, nil
		})
	c.stamp, c.stamped = a.currentAskContext(), true
}

// askReleaseBatch raises the U card for a batch (ADR 0059 decision 3). It has
// no undo, so the ▸ opens on the way out.
func (a *App) askReleaseBatch(name string, n int, note string) {
	c := a.raiseCard(cardReleaseBatch, screens.NewCard(a.screensTheme(), fmt.Sprintf("Release all %d %s?", n, name), note,
		[]screens.CardRow{yesRow("release"), outRow("cancel")}, 1),
		func(a *App, id string) (tea.Model, tea.Cmd) {
			if id != rowYes {
				return a, nil
			}
			if a.world.ReleaseBatch(a.world.ActiveCraftIdx) {
				a.flash(fmt.Sprintf("released %d, %d vessels on the slate", n, len(a.world.Crafts)))
			}
			return a, nil
		})
	c.stamp, c.stamped = a.currentAskContext(), true
}

// startOrbitAltitude is the L key (ADR 0059 decision 4): a raise in progress
// asks to stop; otherwise a vessel that could climb gets the altitude box,
// and one that could not says why and opens nothing.
func (a *App) startOrbitAltitude() {
	idx := a.world.ActiveCraftIdx
	c := a.world.ActiveCraft()
	if c == nil {
		a.flash("raise: no vessel")
		return
	}
	// A member still waiting on its scheduled order is offered "raise this one"
	// instead; one that climbs (by hand or scheduled) asks to stop.
	if o := c.Raise; o != nil && (o.Started || o.BatchID == 0) {
		a.askStopRaise(c.Name)
		return
	}
	if c.BatchLead {
		if _, running := a.world.SpreadStatus(c.BatchID); running {
			a.askStopSpread(c.BatchID)
			return
		}
	}
	now := (orbitalSemiMajor(c) - c.Primary.RadiusMeters()) / 1000
	// The carrier is not a satellite: it only hosts the spread, so it is not
	// asked whether it could climb itself.
	if err := a.world.RaiseRefusal(idx, now*1000); err != nil && !c.BatchLead {
		a.flash(err.Error())
		return
	}
	def := a.lastRaiseKm
	if def <= 0 {
		def = math.Round(now)
	}
	a.altStamp = a.currentAskContext()
	a.altBox.Open("raise to:", def)
}

// handleAltBoxKey routes a key to the altitude box; enter on a number checks
// the order and raises the card, which is the second place a refusal can land.
func (a *App) handleAltBoxKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	done, ok, altM := a.altBox.HandleKey(m)
	if !done || !ok {
		return a, nil
	}
	idx, c := a.world.ActiveCraftIdx, a.world.ActiveCraft()
	a.lastRaiseKm = altM / 1000
	if c.BatchLead {
		a.askSpread(c, altM)
		return a, nil
	}
	if err := a.world.RaiseRefusal(idx, altM); err != nil {
		a.flash(err.Error())
		return a, nil
	}
	if c.BatchID != 0 && len(a.world.SpreadMembers(c.BatchID)) > 1 {
		a.askSpread(c, altM)
		return a, nil
	}
	a.askRaise(c, altM)
	return a, nil
}

// askRaise raises the raise card: the order, then what it costs. It has no
// undo to speak of (L again stops it), but the ▸ opens on the action as the
// box already was the deliberate step.
func (a *App) askRaise(c *spacecraft.Spacecraft, altM float64) {
	dv, prop, dur := sim.RaisePlan(c, altM)
	note := fmt.Sprintf("%s · %.1f kg argon\n%s at %.2f N", readout.DeltaV(dv), prop, readout.Duration(dur), c.Thrust)
	if have := c.ActiveStageFuel(); prop > have {
		note = fmt.Sprintf("%s · %.1f kg argon, tank holds %.1f kg", readout.DeltaV(dv), prop, have)
	}
	verb := "Raise"
	if altM < orbitalSemiMajor(c)-c.Primary.RadiusMeters() {
		verb = "Lower"
	}
	idx := a.world.ActiveCraftIdx
	k := a.raiseCard(cardRaise, screens.NewCard(a.screensTheme(), fmt.Sprintf("%s %s to %s?", verb, c.Name, screens.AltitudeKm(altM)), note,
		[]screens.CardRow{yesRow(strings.ToLower(verb)), outRow("cancel")}, 0),
		func(a *App, id string) (tea.Model, tea.Cmd) {
			if id != rowYes {
				return a, nil
			}
			if err := a.world.CommandRaise(idx, altM); err != nil {
				a.flash(err.Error())
			}
			return a, nil
		})
	k.stamp, k.stamped = a.currentAskContext(), true
}

// askSpread raises the spread card (ADR 0059 decision 5) for the active
// vessel's batch: the carrier's card offers the spread; a member's offers
// raise this one above spread all N. A refused solve flashes and raises no card.
func (a *App) askSpread(c *spacecraft.Spacecraft, altM float64) {
	idx, id := a.world.ActiveCraftIdx, c.BatchID
	sch, err := a.world.SpreadPlan(id, altM)
	if err != nil {
		a.flash(err.Error())
		return
	}
	first := "now"
	if f := sch.First(); f > 0 {
		first = "in " + readout.Duration(f)
	}
	// Whole m/s and no "argon": the note wraps at 34 cells and "each 27 m/s · 0.94 kg argon · 1d12h" is 35.
	note := fmt.Sprintf("first starts %s · last in %s\neach %.0f m/s · %.2f kg · %s",
		first, readout.Duration(sch.Last()), sch.DV, sch.PropKg, readout.Duration(sch.Slots[0].Dur))
	n := len(sch.Slots)
	spreadRow := yesRow("spread")
	rows := []screens.CardRow{spreadRow, outRow("cancel")}
	if !c.BatchLead {
		spreadRow = screens.CardRow{ID: "spread", Label: fmt.Sprintf("spread all %d", n), Key: "s", Letters: []string{"s", "S"}}
		rows = []screens.CardRow{yesRow("raise this one"), spreadRow, outRow("cancel")}
		rows[0].ID = "raise"
	}
	k := a.raiseCard(cardSpread, screens.NewCard(a.screensTheme(), fmt.Sprintf("Spread %d %s at %s?", n, sch.Name, screens.AltitudeKm(altM)), note, rows, 0),
		func(a *App, rid string) (tea.Model, tea.Cmd) {
			var err error
			switch rid {
			case "raise":
				err = a.world.CommandRaise(idx, altM) // replaces this member's scheduled order
			case "spread", rowYes:
				err = a.world.SpreadBatch(id, altM)
			}
			if err != nil {
				a.flash(err.Error())
			}
			return a, nil
		})
	k.stamp, k.stamped = a.currentAskContext(), true
}

// askStopSpread raises the stop card for a running spread: waiting orders are
// dropped, climbing ones finish.
func (a *App) askStopSpread(id uint64) {
	st, _ := a.world.SpreadStatus(id)
	k := a.raiseCard(cardStopSpread, screens.NewCard(a.screensTheme(), "Stop the spread?",
		fmt.Sprintf("%d waiting dropped\n%d climbing finish", st.Waiting, st.Climbing),
		[]screens.CardRow{yesRow("stop"), outRow("keep spreading")}, 0),
		func(a *App, rid string) (tea.Model, tea.Cmd) {
			if rid == rowYes {
				a.flash(fmt.Sprintf("spread stopped, %d orders dropped", a.world.StopSpread(id)))
			}
			return a, nil
		})
	k.stamp, k.stamped = a.currentAskContext(), true
}

// askStopRaise raises the stop card for the active vessel's raise.
func (a *App) askStopRaise(name string) {
	idx := a.world.ActiveCraftIdx
	k := a.raiseCard(cardStopRaise, screens.NewCard(a.screensTheme(), "Stop the raise?", name+" coasts where it is",
		[]screens.CardRow{yesRow("stop"), outRow("keep climbing")}, 0),
		func(a *App, id string) (tea.Model, tea.Cmd) {
			if id == rowYes {
				a.world.StopRaise(idx)
			}
			return a, nil
		})
	k.stamp, k.stamped = a.currentAskContext(), true
}

// askQuickload raises the F9 card: a load discards everything since the last
// quicksave, so the ▸ opens on the way out.
func (a *App) askQuickload() {
	a.raiseCard(cardQuickload, screens.NewCard(a.screensTheme(), "Load the quicksave?",
		"discards everything since your last quicksave",
		[]screens.CardRow{yesRow("load"), outRow("cancel")}, 1),
		func(a *App, id string) (tea.Model, tea.Cmd) {
			if id == rowYes {
				a.flashStatus("load", a.doLoad())
			}
			return a, nil
		})
}

// autosave persists on quit into the rotating autosave ring (v0.26 /
// ADR 0033 §E) — never the named lane, never the legacy single slot.
// Errors are swallowed — the user is leaving and there's no surface to
// flash a message on. Console-printable saves can be wired later if
// needed.
//
// Skips a paused world (#474 follow-up): maybeAutosave's periodic lane
// already refuses to fire on a frozen sim (nothing has changed since
// the last snapshot), but the on-quit write never got the same guard —
// repeated quits from a frozen sim could fill all three ring slots
// with near-identical snapshots and evict a state worth keeping.
func (a *App) autosave() {
	if a.world.Clock.Paused {
		return
	}
	a.persistNow()
}

// PersistNow writes the world through whichever persistence surface this
// App is wired to — the per-player sink in a guest session, the local
// autosave ring otherwise. It exists for the serve layer's admin
// drain-and-restart (v0.30 S4), which leaves the process via os.Exit and
// so never reaches the quit path's autosave. Deliberately bypasses
// autosave's paused-world guard above: os.Exit gives this exactly one
// chance to write, whatever the sim's pause state, and it must always
// take it — a restart mid-pause must not silently stop persisting.
// Call it on the Bubble Tea update goroutine: it reads the live world.
func (a *App) PersistNow() error { return a.persistNow() }

// persistNow is the unconditional write shared by autosave (once past
// its paused-world guard) and PersistNow (which has none). Never call
// this directly from a quit path — go through autosave so the guard
// applies.
func (a *App) persistNow() error {
	// The pause menu's hold is UI state, not a gameplay pause: persist the
	// clock as the player left it, so a restart mid-menu does not come back
	// frozen (B11 / G9 Q6).
	if a.menuHeld {
		held := a.world.Clock.Paused
		a.world.Clock.Paused = a.menuPrevPaused
		defer func() { a.world.Clock.Paused = held }()
	}
	write := save.WriteAutosave
	if a.guestSave != nil {
		write = a.guestSave
	}
	if err := write(a.world); err != nil {
		return err
	}
	if a.onPersisted != nil {
		a.onPersisted(a.world)
	}
	return nil
}

// fixedAutosaveInterval is the guests' cadence and the hosting floor.
const fixedAutosaveInterval = 5 * time.Minute

// maybeAutosave fires the periodic autosave (v0.26 S4 / ADR 0033 §E)
// when the player's wall-clock interval has elapsed since the last
// fire. now is the tea.Tick timestamp from the current sim.TickMsg —
// the injected clock — so the check never reads time.Now() itself and
// stays testable. An interval of 0 ("off" in Settings) disables the
// periodic lane entirely; the on-quit autosave is independent. The
// first tick only arms the anchor (no save at startup — the world is
// seconds old). Write errors are swallowed like the quit path's:
// autosave is a background safety net and must never interrupt flight.
func (a *App) maybeAutosave(now time.Time) {
	interval := a.orbitView.Settings().AutosaveInterval()
	// Guests persist server-side on a fixed cadence (v0.27 review
	// follow-up): AutosaveInterval is the HOST's setting — a host with
	// autosave off must not silently disable every guest's periodic
	// persistence, leaving only the disconnect-time write.
	if a.guestSave != nil {
		interval = fixedAutosaveInterval
	}
	// A hosting App (onPersisted is the ledger ack) never goes quieter than
	// the guests: its acks are what retire dock marks and copies (F6).
	if a.onPersisted != nil && (interval <= 0 || interval > fixedAutosaveInterval) {
		interval = fixedAutosaveInterval
	}
	if interval <= 0 {
		return
	}
	if a.lastAutosave.IsZero() {
		a.lastAutosave = now
		return
	}
	if now.Sub(a.lastAutosave) < interval {
		return
	}
	// Don't autosave a frozen world. Wall-clock ticks keep flowing while
	// the sim is paused (pause menu, boss key, maneuver planning, a long
	// idle), but the state never changes — firing here would evict all
	// three distinct recovery slots with identical snapshots. The anchor
	// is intentionally NOT re-armed on this skip, so the first tick after
	// unpause captures one honest live snapshot instead of none. (Fix:
	// batch-review finding 2.)
	if a.world.Clock.Paused {
		return
	}
	a.lastAutosave = now // re-arm even on error so a failing disk isn't hammered every tick
	if a.guestSave != nil {
		_ = a.guestSave(a.world)
		return
	}
	// Tell the player once per failing streak, not every interval (#758).
	err := a.persistNow()
	if err != nil && !a.autosaveFailing {
		reason := err.Error()
		var pe *fs.PathError
		if errors.As(err, &pe) {
			reason = pe.Err.Error() // "no space left on device", not the path chain
		}
		a.flash("autosave failed: " + reason)
	}
	a.autosaveFailing = err != nil
}

// portTrim is the arrow keys' PORT branch (ADR 0055 decision 4): with
// NavMode PORT the arrows tip the nose across the port face instead of
// trimming off the local horizon. dUp / dRight are radians toward the face's
// up / right (negative: down / left). Reports whether the key was consumed,
// so the caller falls through to the surface trim everywhere else.
func (a *App) portTrim(dUp, dRight float64) bool {
	if a.world.NavMode != sim.NavPort {
		return false
	}
	if c := a.world.ActiveCraft(); c != nil {
		c.PortPitchTrim += dUp
		c.PortYawTrim += dRight
	}
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
	c := a.world.ActiveCraft()
	if c == nil {
		a.refuse("attitude", "no vessel")
		return
	}
	if c.EngineMode == spacecraft.EngineRCS {
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
		a.flash(fmt.Sprintf("nav: %s", nav))
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
		a.flash(fmt.Sprintf("RCS: %s", state))
	case screens.NavballControlSAS:
		a.world.ToggleInstantSAS()
		a.flash(fmt.Sprintf("SAS: %s", sasModeLabel(a.world.InstantSAS)))
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
	case screens.MenuActionSave, screens.MenuActionLoad, screens.MenuActionSettings,
		screens.MenuActionControls, screens.MenuActionHelp, screens.MenuActionVAB:
		// Whatever opens next remembers the menu as its opener (Q3). The
		// refusal paths below fall back to the map and clear it again.
		a.fromMenu = true
	case screens.MenuActionCancel, screens.MenuActionQuit:
		a.fromMenu = false
	}
	switch action {
	// v0.26 S3 (ADR 0033 §F): both menu items open the unified Saves
	// screen; the entry point selects save-mode vs load-mode. Opening a
	// screen is reversible, so like Settings/VAB there is no confirm
	// gate here — the destructive confirms (§H) live in the screen.
	case screens.MenuActionSave:
		// Guests have no local Saves surface (v0.27 S3) — same refusal
		// as F5/F9, surfaced as a toast over the orbit screen.
		if a.guestSave != nil {
			a.flashStatus("save", errGuestSaves)
			a.fromMenu = false
			a.closeMenu()
			return a, nil
		}
		a.openSaves(screens.SavesModeSave)
		return a, nil
	case screens.MenuActionLoad:
		if a.guestSave != nil {
			a.flashStatus("load", errGuestSaves)
			a.fromMenu = false
			a.closeMenu()
			return a, nil
		}
		a.openSaves(screens.SavesModeLoad)
		return a, nil
	case screens.MenuActionSettings:
		// Guests share the host's process — Settings/Controls write the
		// HOST machine's settings.json, so both are refused in a guest
		// session (v0.27 review follow-up).
		if a.guestSave != nil {
			a.flashStatus("settings", errGuestSettings)
			a.fromMenu = false
			a.closeMenu()
			return a, nil
		}
		// Navigating to a screen is harmless + reversible, so unlike
		// save/load/quit there is no confirm gate. Reset the cursor so the
		// screen always opens on the first Chip.
		a.settingsScreen.Reset()
		a.active = screenSettings
		return a, nil
	case screens.MenuActionControls:
		if a.guestSave != nil {
			a.flashStatus("controls", errGuestSettings)
			a.fromMenu = false
			a.closeMenu()
			return a, nil
		}
		a.controls.Reset()
		a.active = screenControls
		return a, nil
	case screens.MenuActionHelp:
		// #425: the menu's own real pointer to the keybinding list.
		// Mirrors exactly what F1/? do — reset scroll so it always
		// opens at the top, no guest restriction (help is read-only,
		// unlike Settings/Controls which write the host's settings.json).
		a.help.ResetScroll()
		a.helpFromSet = false
		a.active = screenHelp
		return a, nil
	case screens.MenuActionVAB:
		// Open the Vehicle Assembly builder with the live component catalog
		// (embedded + user overlay). Reversible, so no confirm gate.
		a.vab.Reset(spacecraft.Components)
		a.active = screenVAB
		return a, nil
	case screens.MenuActionQuit:
		// #474: the menu's Quit row arms the same app-level quit prompt
		// ctrl+c does (handleQuitConfirmKey), rather than autosaving and
		// quitting on the spot — one question, one wording, wherever the
		// player leaves from.
		a.armQuit()
		return a, nil
	case screens.MenuActionCancel:
		a.closeMenu()
		return a, nil
	}
	return a, nil
}

// openSaves lists the saves directory and opens the unified Saves
// screen (v0.26 / ADR 0033 §F) in the given entry-point mode. A
// listing failure still opens the (empty) screen with a toast — the
// save-mode "＋ New save…" row must stay reachable even when the scan
// hiccups.
func (a *App) openSaves(mode screens.SavesMode) {
	entries, err := save.List()
	if err != nil {
		a.toast(fmt.Sprintf("saves listing failed: %v", err))
	}
	a.saves.Open(mode, entries, defaultSaveName(a.world))
	// Freeze the clock while the browser is up (finding 4) — a Save-As
	// captured a drifting world otherwise, since the pause menu itself
	// keeps the sim running. Restored on exit (see closeSavesToOrbit).
	a.savesPrevPaused = a.world.Clock.Paused
	a.world.Clock.Paused = true
	a.active = screenSaves
}

// closeSavesToOrbit leaves the Saves browser back to the flight view,
// restoring the pause state the sim had before it opened (finding 4).
// Used by every non-load exit — cancel, Save-As, overwrite. A load does
// NOT route through here: it swaps in a fresh world whose own pause
// state applies.
func (a *App) closeSavesToOrbit() {
	a.world.Clock.Paused = a.savesPrevPaused
	a.backToOpener()
}

// backToOpener is the one back rule (B11 / G9 Q3): a screen opened from
// the pause menu (Saves, Settings, Keyboard layout, Help, VAB) goes back to
// the menu; a screen opened by a key (Missions, Spawn, Maneuver, Body info,
// Session, ...) goes back to the map. A second esc from the menu flies.
// openMenu opens the pause menu and really pauses (B11 / G9 Q6): the clock
// stops, remembering whether it was already stopped, and closeMenu hands
// that state back. Per client: in a --serve session each seat owns its own
// Clock.Paused (the relay reports it to partners, who see the seat hold,
// the same as while the maneuver planner is open).
func (a *App) openMenu() {
	a.menu.Reset()
	a.menuPrevPaused = a.world.Clock.Paused
	a.menuHeld = true
	a.world.SeatHold, a.world.SeatHoldUnpause = true, false
	a.world.Clock.Paused = true
	a.active = screenMenu
}

// releaseMenuHold gives the clock back after the pause menu. A partner's
// Rendezvous Warp arm or Sync that arrived while the card was open was
// deferred (World.SeatHoldUnpause, B11 review L-mp): it starts now, so the
// player's pause won while the card was up and the partner's warp is not lost.
func (a *App) releaseMenuHold() {
	a.world.Clock.Paused = a.menuPrevPaused
	if a.world.SeatHoldUnpause {
		a.world.Clock.Paused = false
	}
	a.world.SeatHold, a.world.SeatHoldUnpause = false, false
	a.menuHeld = false
}

// closeMenu flies again: the map returns and the clock goes back to the
// state it had when the menu opened.
func (a *App) closeMenu() {
	if a.menuHeld {
		a.releaseMenuHold()
	}
	a.active = screenOrbit
}

// helpPageFor names the help page that belongs to screen s ("" = the index).
func helpPageFor(s screenID) string {
	switch s {
	case screenSpawn:
		return "SPAWN FORM"
	case screenVAB:
		return "VEHICLE ASSEMBLY"
	case screenSession:
		return "MULTIPLAYER"
	case screenManeuver, screenPorkchop:
		return "PLAN BURNS"
	case screenSaves:
		return "SAVES"
	case screenMissions:
		return "MISSIONS"
	case screenMenu, screenSettings, screenControls:
		return "PAUSE MENU"
	}
	return ""
}

// openHelpHere opens help from the active screen: that screen's own page if
// it has one, else the index, remembering the screen so closeHelp returns to
// it with its focus and any half-typed text untouched.
func (a *App) openHelpHere() {
	a.helpFrom, a.helpFromSet = a.active, true
	if title := helpPageFor(a.active); title != "" {
		a.help.OpenFor(title)
	} else {
		a.help.ResetScroll() // the index, always at the top
	}
	a.active = screenHelp
}

// closeHelp leaves help for the screen F1 was pressed on, or by the back
// rule when help came from the pause menu's Help row.
func (a *App) closeHelp() {
	if a.helpFromSet {
		a.helpFromSet = false
		a.active = a.helpFrom
		return
	}
	a.backToOpener()
}

func (a *App) backToOpener() {
	if a.fromMenu {
		a.fromMenu = false
		a.menu.Reset()
		a.active = screenMenu
		return
	}
	a.active = screenOrbit
}

// capturingText reports whether the active screen is currently taking
// literal typed text into a free-text field. While it is, the App
// bypasses keyboard-layout normalization (finding 3) and the global
// boss-key intercept (finding 9) so the raw keycaps reach the field: a
// QWERTZ player's letters aren't transposed and a literal backtick types
// a backtick instead of opening the boss shell. The boss shell is always
// capturing; the Saves browser only during name entry (Save-As/rename);
// the Spawn screen only while its ALTITUDE typed-edit box is open (ADR
// 0044 / S4 — review finding #5: this comment's own "extend here" note
// was not followed when that box landed, so “ ` “ mid-digit swapped the
// whole screen to the boss shell instead of typing a backtick nobody
// wanted). Extend here as future free-text surfaces (VAB, search) land.
func (a *App) capturingText() bool {
	// The chat overlay is App-level, not a screen — checked before the
	// per-screen switch so it can never be forgotten there (ADR 0035).
	if a.chatOpen {
		return true
	}
	// The Rendezvous Planner picker (ADR 0045 S6, #399) is a chip on the
	// orbit map, not a screen — a.active stays screenOrbit (or
	// screenBodyInfo/screenMissions, K's other two reachable screens)
	// while it's open, so it can't be reached via the per-screen switch
	// below either. Checked here, same as chatOpen, per this func's own
	// "extend here" note — ADR 0044's review caught exactly this kind of
	// omission (a new interactive surface never added here let the boss
	// key fire mid-edit).
	if a.orbitView.RendezvousPickerOpen() {
		return true
	}
	if a.altBox.CapturingText() {
		return true
	}
	switch a.active {
	case screenBoss:
		return true
	case screenSaves:
		return a.saves.CapturingText()
	case screenSession:
		return a.session.CapturingText()
	case screenSpawn:
		return a.spawn.CapturingText()
	case screenVAB:
		return a.vab.CapturingText()
	}
	return false
}

// handleChatKey routes every keypress while the chat input is up
// (ADR 0035 S3). Switch on msg.Type, not String, and return
// unconditionally — the mint-invite discipline — so nothing leaks to
// flight controls.
func (a *App) handleChatKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.Type {
	case tea.KeyEscape:
		a.chatOpen, a.chatInput, a.chatTabbing = false, a.chatInput[:0], false
	case tea.KeyEnter:
		return a.sendChat()
	case tea.KeyBackspace:
		if len(a.chatInput) > 0 {
			a.chatInput = a.chatInput[:len(a.chatInput)-1]
		}
		a.chatTabbing = false
	case tea.KeyTab:
		a.chatTabComplete()
	case tea.KeySpace:
		a.chatAppend(' ')
	case tea.KeyRunes:
		for _, r := range m.Runes {
			a.chatAppend(r)
		}
	}
	return a, nil
}

func (a *App) chatAppend(r rune) {
	if len(a.chatInput) < chatInputRuneCap {
		a.chatInput = append(a.chatInput, r)
	}
	a.chatTabbing = false
}

// sendChat parses the drafted line and emits the ChatSendMsg the serve
// wrapper acts on. A leading @ addresses a DM: the target is resolved
// by the LONGEST online-roster handle matching the head of the text
// (handles may legally contain spaces — "mission control" must be
// DM-able, and tab-complete produces exactly that draft; v0.32 review
// finding), case-insensitively, self excluded. A miss refuses to send
// and leaves the text intact — never a silent fall-back to broadcast
// (ADR 0035 §7) — and so does an AMBIGUOUS handle (two online players
// whose handles fold to the same string): a private line must never
// route by enrollment order (v0.32 review finding).
func (a *App) sendChat() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(string(a.chatInput))
	if text == "" {
		a.chatOpen, a.chatInput = false, a.chatInput[:0]
		return a, nil
	}
	msg := ChatSendMsg{Text: text}
	if strings.HasPrefix(text, "@") {
		p, rest, matches := a.chatResolveDM(text[1:])
		if matches == 0 {
			first, _, _ := strings.Cut(text[1:], " ")
			a.toast("no online player \"" + first + "\" — not sent")
			return a, nil
		}
		if matches > 1 {
			a.toast("\"" + p.Handle + "\" is ambiguous — two players share it; not sent")
			return a, nil
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			a.toast("message is empty — not sent")
			return a, nil
		}
		msg = ChatSendMsg{Text: rest, To: p.Fingerprint, ToHandle: p.Handle}
	}
	a.chatOpen, a.chatInput, a.chatTabbing = false, a.chatInput[:0], false
	return a, func() tea.Msg { return msg }
}

// chatResolveDM matches the head of body (the text after the leading @)
// against the online roster, longest handle first: a handle matches when
// body starts with it case-insensitively and the next byte is a space or
// the end. Returns the matched player, the remaining message, and how
// many ONLINE players carry the winning handle (0 = no match, >1 =
// ambiguous — the caller refuses).
func (a *App) chatResolveDM(body string) (sim.SessionPlayer, string, int) {
	info := a.world.Session
	if info == nil {
		return sim.SessionPlayer{}, "", 0
	}
	var best sim.SessionPlayer
	bestLen, matches := 0, 0
	for _, p := range info.Players {
		if !p.Online || p.Fingerprint == info.Self || p.Handle == "" {
			continue
		}
		h := p.Handle
		if len(body) < len(h) || !strings.EqualFold(body[:len(h)], h) {
			continue
		}
		if len(body) > len(h) && body[len(h)] != ' ' {
			continue
		}
		switch {
		case len(h) > bestLen:
			best, bestLen, matches = p, len(h), 1
		case len(h) == bestLen:
			matches++
		}
	}
	if matches == 0 {
		return sim.SessionPlayer{}, "", 0
	}
	return best, body[bestLen:], matches
}

// RestoreChatDraft reopens the chat input with the given draft — the
// serve wrapper's door for handing a refused DM back to the player
// instead of destroying up to 120 typed runes (v0.32 review finding:
// the target can go offline between the roster tick and Enter).
func (a *App) RestoreChatDraft(draft string) {
	a.chatOpen = true
	a.chatInput = []rune(draft)
	a.chatTabbing = false
}

// chatTabComplete completes a leading @prefix against the online
// roster; repeated tab cycles the matches (ADR 0035 §7). Only the
// @word itself completes — once a space is typed the handle is
// committed and tab is inert.
func (a *App) chatTabComplete() {
	// The no-spaces guard applies only when STARTING a cycle — while a
	// cycle is live the input is exactly "@<candidate>", which may
	// itself contain spaces ("mission control"), and tab must keep
	// cycling past it to the other candidates (v0.32 review finding).
	if !a.chatTabbing {
		cur := string(a.chatInput)
		if !strings.HasPrefix(cur, "@") || strings.ContainsRune(cur, ' ') {
			return
		}
		a.chatTabbing, a.chatTabBase, a.chatTabIdx = true, cur[1:], -1
	}
	info := a.world.Session
	if info == nil {
		return
	}
	var cands []string
	for _, p := range info.Players {
		if !p.Online || p.Fingerprint == info.Self {
			continue
		}
		if strings.HasPrefix(strings.ToLower(p.Handle), strings.ToLower(a.chatTabBase)) {
			cands = append(cands, p.Handle)
		}
	}
	if len(cands) == 0 {
		return
	}
	a.chatTabIdx = (a.chatTabIdx + 1) % len(cands)
	a.chatInput = []rune("@" + cands[a.chatTabIdx])
}

// refreshSaves re-lists the directory into the open Saves screen after
// an in-screen mutation (delete / rename).
func (a *App) refreshSaves() {
	entries, err := save.List()
	if err != nil {
		a.toast(fmt.Sprintf("saves listing failed: %v", err))
		return
	}
	a.saves.SetEntries(entries)
}

// defaultSaveName is the Save-As prefill (ADR 0033 §B): the active
// vessel plus the in-game day, the latter in the same layout the orbit
// HUD's clock chip renders.
func defaultSaveName(w *sim.World) string {
	day := w.Clock.SimTime.Format("2006-01-02")
	if c := w.ActiveCraft(); c != nil {
		return c.Name + " " + day
	}
	return day
}

// applySessionCommand dispatches a finalised SessionCommand (v0.27 S6
// / ADR 0034). Target + toast are world/App-local; the admin trio
// (mint / revoke / remove) is re-emitted as a SessionAdminMsg for the
// serve-layer wrapper, which owns the session store — the App knows
// nothing about sessiondir.
func (a *App) applySessionCommand(cmd screens.SessionCommand) (tea.Model, tea.Cmd) {
	switch cmd.Kind {
	case screens.SessionCmdClose:
		a.active = screenOrbit
	case screens.SessionCmdTargetGhost:
		a.world.SetTargetGhost(cmd.Owner, cmd.CraftID)
		a.flash(fmt.Sprintf("target: %s's vessel", cmd.Handle))
		a.active = screenOrbit
	case screens.SessionCmdSpectate:
		// Spectate (v0.28 S6 / ADR 0034): camera-follow a remote player's
		// ghost. Read-only — no guestSave gate, reachable by host and guest
		// alike. Fits once to the ghost's drawn orbit extent (Framing Event)
		// then tracks it; [f]/[alt+g] refocus away from it.
		a.world.SpectateGhost(cmd.Owner, cmd.CraftID)
		a.flash(fmt.Sprintf("spectating %s — [f] to return", cmd.Handle))
		a.active = screenOrbit
	case screens.SessionCmdSync:
		// Sync-to (v0.27 S7 / ADR 0034): Auto-Warp to the player's
		// subspace time. The screen already refused backward targets;
		// this guard covers a slate that moved between frames.
		if a.world.EngageSyncWarp(cmd.Time, cmd.Owner, cmd.Handle) {
			a.flash(fmt.Sprintf("syncing to %s — auto-warp engaged", cmd.Handle))
		} else {
			a.flash(fmt.Sprintf("can't sync — %s is no longer ahead", cmd.Handle))
		}
		a.active = screenOrbit
	case screens.SessionCmdRendezvous:
		// Rendezvous Warp initiate (v0.29 S2 / ADR 0034 v0.29 addendum;
		// ADR 0045 S7, #400: Engage IS the agreement now). Target the
		// partner's ghost (the advisory + TARGET chip context), commit
		// whatever encounter is available — a planted Rendezvous Burn node's
		// own arrival, a planted trim-rung nudge's post-burn course, else
		// the current-course closest approach (#276: never the K-nudge
		// ADVISORY's unfired preview) — and arm toward them regardless of
		// whether anything was found. The coast starts only when they
		// respond (mutual arm), and only once there IS a plan; the screen
		// already gated same-subspace and ghost presence.
		//
		// PR #392 review finding 2: SetTargetGhost runs before the commit,
		// so a refusal below must not leave the switch behind — capture
		// the prior target/NavMode and restore them on !ok. Without this a
		// refused Engage silently stole whatever the player was targeting
		// before (mid-transfer TargetBody Moon, say), flipped NavMode
		// along with it, and left the stale ghost ref to persist into the
		// next save. ADR 0045 S7 narrows when !ok can even fire: only the
		// genuinely structural gates (no target resolvable, cross-primary)
		// refuse now — "no encounter found" no longer does.
		prevTarget, prevNav := a.world.Target, a.world.NavMode
		a.world.SetTargetGhost(cmd.Owner, cmd.CraftID)
		plan, ok := a.world.RendezvousCommitWithPlan()
		switch {
		case !ok:
			a.world.RestoreTarget(prevTarget, prevNav)
			a.flash(fmt.Sprintf("can't rendezvous with %s — no reported position", cmd.Handle))
		// The seat is fixed here (ADR 0037 §2): proposing the rendezvous
		// makes you pilot-in-command of the pair's time once the terminal
		// phase begins.
		case a.world.EngageRendezvousWarpAs(cmd.Owner, cmd.Handle, plan.Tau, plan.CommittedCA, true):
			// Rendezvous Orbit (ADR 0045 S7, #400): stamped after Engage
			// succeeds, so a re-Engage that plants nothing new (plan has no
			// Place) clears whatever the PREVIOUS arm carried — the arm was
			// just replaced wholesale above, so this keeps the two in sync
			// rather than accidentally carrying stale Place text forward.
			a.world.SetRendezvousOrbit(plan.RendezvousOrbitLabel, plan.RendezvousLaps)
			// Name the acting craft (#295): arming acts on whatever slot is
			// active, and a player who cycled it earlier has no other way to
			// catch a wrong-vessel arm before the invitation goes out.
			craftTag := screens.CraftTag(a.world.RendezvousArm.CraftName)
			if plan.Tau.IsZero() {
				a.flash(fmt.Sprintf("rendezvous agreed with %s%s — no plan yet, waiting for them to join",
					cmd.Handle, craftTag))
			} else {
				a.flash(fmt.Sprintf("rendezvous armed toward %s%s — waiting for them to join%s",
					cmd.Handle, craftTag, rendezvousGapNoteSuffix(a.world, plan.CommittedCA)))
			}
		default:
			a.flash(fmt.Sprintf("can't arm rendezvous with %s — encounter is in the past", cmd.Handle))
		}
		a.active = screenOrbit
	case screens.SessionCmdToast:
		a.flash(cmd.Message)
	case screens.SessionCmdStartHost:
		// In-UI hosting (v0.28 S3 / ADR 0034). Host-only by
		// construction: a guest session carries a guestSave sink, so it
		// can never start a listener of its own — the same guard that
		// refuses guests local saves + settings.
		if a.guestSave != nil {
			a.toast("guests can't host — you're already in someone's session")
			return a, nil
		}
		msg := screens.SessionHostMsg{Start: true}
		return a, func() tea.Msg { return msg }
	case screens.SessionCmdStopHost:
		msg := screens.SessionHostMsg{Start: false}
		return a, func() tea.Msg { return msg }
	case screens.SessionCmdRestart:
		return a, func() tea.Msg { return screens.SessionRestartMsg{} }
	case screens.SessionCmdMint, screens.SessionCmdRevoke, screens.SessionCmdRemove,
		screens.SessionCmdKick, screens.SessionCmdDisconnect,
		screens.SessionCmdPromote, screens.SessionCmdDemote:
		// ADR 0054: kick and disconnect go to the serve wrapper like the other
		// admin intents, which drops the live session (serve/drop.go).
		msg := screens.SessionAdminMsg{Cmd: cmd}
		return a, func() tea.Msg { return msg }
	}
	return a, nil
}

// applySavesCommand dispatches a finalised SavesCommand from the Saves
// screen against the save package. The screen has already run every
// confirm gate (§H), so each destructive kind executes directly here.
// Load / Save-As / Overwrite exit to orbit; delete / rename stay on
// the screen with a refreshed listing.
func (a *App) applySavesCommand(cmd screens.SavesCommand) (tea.Model, tea.Cmd) {
	switch cmd.Kind {
	case screens.SavesActionCancel:
		a.closeSavesToOrbit()
	case screens.SavesActionLoad:
		if err := a.loadWorldByID(cmd.ID); err != nil {
			a.toast(fmt.Sprintf("load failed: %v", err))
		} else {
			a.toast(fmt.Sprintf("loaded %s", cmd.Name))
		}
	case screens.SavesActionDelete:
		if err := save.Delete(cmd.ID); err != nil {
			a.toast(fmt.Sprintf("delete failed: %v", err))
		} else {
			a.toast(fmt.Sprintf("deleted %s", cmd.Name))
		}
		a.refreshSaves()
	case screens.SavesActionRename:
		if err := save.Rename(cmd.ID, cmd.Name); err != nil {
			a.toast(fmt.Sprintf("rename failed: %v", err))
		} else {
			a.toast(fmt.Sprintf("renamed to '%s'", cmd.Name))
		}
		a.refreshSaves()
	case screens.SavesActionSaveNew:
		var err error
		a.withFlightClock(func() { _, err = save.WriteNamed(a.world, cmd.Name) })
		if err != nil {
			a.toast(fmt.Sprintf("save failed: %v", err))
		} else {
			a.toast(fmt.Sprintf("saved '%s'", cmd.Name))
			a.closeSavesToOrbit()
		}
	case screens.SavesActionOverwrite:
		var err error
		a.withFlightClock(func() { err = save.Overwrite(cmd.ID, a.world) })
		if err != nil {
			a.toast(fmt.Sprintf("overwrite failed: %v", err))
		} else {
			a.toast(fmt.Sprintf("overwrote '%s'", cmd.Name))
			a.closeSavesToOrbit()
		}
	}
	return a, nil
}

// withFlightClock runs fn with Clock.Paused set to what the player was
// flying with BEFORE the pause menu and the Saves screen took their holds
// (menuPrevPaused, then savesPrevPaused), and puts the holds back after. A
// save written from the Saves screen thus loads as it was flying; one made
// while the player had really paused still loads paused. Same rule as the
// quit-autosave path.
func (a *App) withFlightClock(fn func()) {
	held := a.world.Clock.Paused
	flying := held
	if a.active == screenSaves {
		flying = a.savesPrevPaused
	}
	if a.menuHeld {
		flying = a.menuPrevPaused
	}
	a.world.Clock.Paused = flying
	defer func() { a.world.Clock.Paused = held }()
	fn()
}

// brakeRendezvousPair / releaseRendezvousBrake give the warp keys their
// copilot meaning inside a rendezvous agreement's terminal phase (ADR
// 0037 §2). Each reports whether it consumed the press, so the normal
// warp path runs untouched in every other seat and phase.
//
// Both toast, always: a key that silently does nothing is the exact
// failure #302 recorded from the seat, and the v0.30 lesson that silent
// no-ops read as broken applies to warp above everything else.
func (a *App) brakeRendezvousPair() bool {
	if a.world.RendezvousRate.Seat != sim.RendezvousSeatCopilot {
		return false
	}
	brake, ok := a.world.StepRendezvousBrake(false)
	if !ok {
		return false
	}
	a.toast(fmt.Sprintf("braking the pair to %s — [.] releases back to following %s",
		screens.WarpLabel(brake), a.world.RendezvousRate.Handle))
	return true
}

func (a *App) releaseRendezvousBrake() bool {
	if a.world.RendezvousRate.Seat != sim.RendezvousSeatCopilot {
		return false
	}
	brake, ok := a.world.StepRendezvousBrake(true)
	switch {
	case !ok:
		a.toast(fmt.Sprintf("you're copilot — %s flies the pair's clock; [,] brakes, [/] cancels",
			a.world.RendezvousRate.Handle))
	case brake == 0:
		a.toast(fmt.Sprintf("following %s — the pair runs at their warp", a.world.RendezvousRate.Handle))
	default:
		a.toast(fmt.Sprintf("brake eased to %s", screens.WarpLabel(brake)))
	}
	return true
}

// toggleAutoWarpBurn carries the shared intent behind the `G` key and
// the mouse [»Burn] button: toggle Auto-Warp to the globally-soonest
// burn. Reports whether the toggle ran, and when it did records the
// auto_warp action (ADR 0025 §7) for either input: the button used to
// record nothing (Jason 2026-10-09).
//
// Except during an engaged Rendezvous Warp coast (#259, sibling of
// #249): ToggleAutoWarp routes through DisengageAutoWarp, which must
// clear the arm on every cancel path (or DriveRendezvousWarp restarts
// the coast next tick), and the retraction travels the wire — so a
// stray G / click silently tore down the rendezvous for BOTH players.
// As with the warp keys (#256), the guard lives here at the input
// layer, where the intent is known, and [/] stays the single
// deliberate cancel gesture — the toggle paths refuse with a toast
// instead. Plain node-chase / Sync Auto-Warp keeps today's toggle.
func (a *App) toggleAutoWarpBurn() bool {
	if a.world.RendezvousWarpEngaged() {
		a.toast("rendezvous coast running — [/] to cancel")
		return false
	}
	if a.retrimInLaunchWindowLead() {
		return true
	}
	a.world.ToggleAutoWarp()
	a.world.RecordAction(missions.ActionAutoWarp)
	return true
}

// retrimInLaunchWindowLead is G's and [»Burn]'s answer on the pad once the
// launch window is inside its 30 s lead (review #582 L3): nothing to warp
// to, so it turns the heading to the window's and says so. Reports whether
// it did; false leaves the caller to warp or refuse as before.
func (a *App) retrimInLaunchWindowLead() bool {
	if a.world.AutoWarpEngaged() {
		return false
	}
	deg, ok := a.world.TrimToLaunchWindowInLead()
	if !ok {
		return false
	}
	a.toast(fmt.Sprintf("heading %03d° for the launch window", deg))
	return true
}

// toast flashes a transient one-line notice in the HUD footer for ~3s
// (the generic sibling of flashStatus, which is F5/F9-specific).
// rendezvousGapNoteSuffix appends the ADR 0039 S3 gap note to an Engage
// success message when the committed CA is far above the couple/lock
// gate — "" otherwise, so callers can splice it in unconditionally.
// Named at the display layer (the message text) over sim.World's
// RendezvousNeedsBurnToClose (the domain judgment), mirroring how
// refusal reasons are composed elsewhere in this file.
func rendezvousGapNoteSuffix(w *sim.World, ca float64) string {
	if !w.RendezvousNeedsBurnToClose(ca) {
		return ""
	}
	return " — a burn will be needed to close this"
}

// commitInspect binds the currently-inspected entity into the Target
// slot (ADR 0041 §3 / #346) — the same hover→commit contract the body
// Cursor already had, generalised past bodies to vessels, ghosts and
// back again.
//
// It lives on the App, not the screen: OrbitView reports WHAT is
// inspected and the App decides what that means, so the screen keeps
// its no-world-mutation rule and the commit goes through the existing
// sim setters (SetTargetBody / SetTargetCraft / SetTargetGhost) rather
// than a second targeting path.
//
// Entities the Target slot can't hold — your own vessel, the system
// primary, a planted node, the closest-approach pair — are a no-op that
// SAYS so. A silent refusal reads as a broken key, and the whole point
// of Inspect is that the map answers questions.
func (a *App) commitInspect() {
	ref, targetable, ok := a.orbitView.InspectedRef()
	if !ok {
		return
	}
	name := a.orbitView.InspectedName()
	if !targetable {
		a.toast(fmt.Sprintf("%s can't be a target", name))
		return
	}
	switch ref.Kind {
	case screens.InspectBody:
		idx := -1
		for i, b := range a.world.System().Bodies {
			if b.ID == ref.BodyID {
				idx = i
				break
			}
		}
		if idx <= 0 {
			a.toast(fmt.Sprintf("%s can't be a target", name))
			return
		}
		a.world.SetTargetBody(idx)
		// Keep the body Cursor in step: committing a body from the map
		// is the same act as browsing to it with h/l and is consumed by
		// the same planners (H / I / P read selectedBody), so leaving
		// the Cursor pointing elsewhere would split one selection into
		// two disagreeing ones.
		a.selectedBody = idx
	case screens.InspectVessel:
		idx := -1
		for i, c := range a.world.Crafts {
			if c != nil && c.ID == ref.CraftID {
				idx = i
				break
			}
		}
		if idx < 0 || idx == a.world.ActiveCraftIdx {
			a.toast(fmt.Sprintf("%s can't be a target", name))
			return
		}
		a.world.SetTargetCraft(idx)
	case screens.InspectGhost:
		a.world.SetTargetGhost(ref.Owner, ref.CraftID)
	default:
		a.toast(fmt.Sprintf("%s can't be a target", name))
		return
	}
	// Committing ends the question: the highlight comes down and the
	// TARGET readouts take over as the standing display of the choice.
	a.orbitView.InspectClear()
	a.toast(fmt.Sprintf("target: %s", name))
}

// toast is the pre-#427 name for flash, kept as a thin alias — it has
// ~30 call sites in this file plus the exported Toast below, which
// internal/serve calls across the App boundary to surface hosting/
// dock/reporting outcomes. Both now share flash's one voice and one
// lifetime (ADR 0048) rather than carrying a second copy of the same
// two-line body.
func (a *App) toast(msg string) {
	a.flash(msg)
}

// Toast flashes msg in the HUD footer — the exported door for the
// serve-layer wrapper to surface lazy-hosting outcomes (listener
// started, port conflict) on the host's own screen instead of a
// pre-TUI stderr line (v0.28 S3, ADR 0034).
func (a *App) Toast(msg string) { a.toast(msg) }

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
		a.flash(fmt.Sprintf("settings save failed: %v", err))
	}
}

// enabledProgramsFromSettings maps the persisted Tutorial/Challenges toggles
// to the set of active mission-program names the World evaluator gates on
// (ADR 0025 §2 / v0.21 Slice 7). Always returns a non-nil map so the World's
// nil-default ("all enabled") is overridden. Flight School is on unless
// explicitly switched off (#425); the Challenge ladder stays opt-in.
func enabledProgramsFromSettings(s settings.Settings) map[string]bool {
	enabled := map[string]bool{}
	if s.TutorialOn() {
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
// Slice 7 (ADR 0025 §2). Flipping tutorial always records an explicit choice
// (SetTutorialEnabled), which is the only thing that can silence it (#425).
func (a *App) toggleMissionProgram(tutorial bool) {
	s := a.orbitView.Settings()
	if tutorial {
		s.SetTutorialEnabled(!s.TutorialOn())
	} else {
		s.ChallengesEnabled = !s.ChallengesEnabled
	}
	a.orbitView.SetSettings(s)
	a.world.SetEnabledMissionPrograms(enabledProgramsFromSettings(s))
	if err := settings.Save(s); err != nil {
		a.flash(fmt.Sprintf("settings save failed: %v", err))
	}
}

// cycleAutosaveInterval advances the periodic-autosave interval to the
// next step in settings.AutosaveIntervalSteps (v0.26 S4 / ADR 0033 §E)
// and persists it — mirroring toggleChip's persist-on-change. Cycling
// deliberately does not reset lastAutosave: the anchor is "time of the
// last save/arm", and the next tick compares it against the new
// interval, so shortening the interval can fire promptly and
// lengthening it just waits longer.
func (a *App) cycleAutosaveInterval() {
	s := a.orbitView.Settings()
	s.SetAutosaveIntervalMin(settings.NextAutosaveIntervalMin(s.AutosaveIntervalMinutes()))
	a.orbitView.SetSettings(s)
	if err := settings.Save(s); err != nil {
		a.flash(fmt.Sprintf("settings save failed: %v", err))
	}
}

// cycleRecordingWindow advances the Flight Recording window and persists
// it (ADR 0062 decision 9), mirroring cycleAutosaveInterval. The value also
// goes to the live recorder (re-arming its prune check) and to the World, so
// a file opened later by a vessel switch uses it too.
func (a *App) cycleRecordingWindow() {
	s := a.orbitView.Settings()
	s.SetRecordingWindowDays(settings.NextRecordingWindowDays(s.RecordingWindow()))
	a.orbitView.SetSettings(s)
	a.world.RecordingWindowDays = s.RecordingWindow()
	if a.world.Recording != nil {
		a.world.Recording.SetWindowDays(s.RecordingWindow())
	}
	if err := settings.Save(s); err != nil {
		a.flash(fmt.Sprintf("settings save failed: %v", err))
	}
}

// cycleEmptyReadings advances Empty readings (Full, Tidy, Compact) and
// persists it to settings.json (ADR 0051 W6, #482), mirroring
// cycleAutosaveInterval. The orbit view reads it live from its Settings.
func (a *App) cycleEmptyReadings() {
	s := a.orbitView.Settings()
	s.SetEmptyReadings(settings.NextEmptyReadings(s.EmptyReadingsMode()))
	a.orbitView.SetSettings(s)
	if err := settings.Save(s); err != nil {
		a.flash(fmt.Sprintf("settings save failed: %v", err))
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
		a.flash(fmt.Sprintf("settings save failed: %v", err))
	}
}

// flashLifetime is the one lifetime every Event Flash lives for (ADR
// 0048 decision 1). Pre-#427 call sites disagreed — 1.5s / 2s / 3s / 4s
// / 6s depending on which corner of app.go set statusExpires — with no
// single owner. flash below is now that owner.
const flashLifetime = 3 * time.Second

// flash writes one Event Flash to the HUD footer: the transient border
// line for something that is already over the moment it happens (a burn
// fired, a stage dropped, a refusal) as opposed to a Standing Alert,
// which persists while its state still holds (see standingAlert /
// VESSEL DESTROYED). Every call site in this file goes through flash so
// the voice and the lifetime can't drift call site to call site (ADR
// 0048): one verb, what happened, and the number that matters —
// "node 1 burned — 3054 m/s, 2 remaining", "dropped S-IC — 2 stages
// left". text is the finished line; callers format their own message
// (fmt.Sprintf or a literal) and hand it here.
func (a *App) flash(text string) {
	a.statusMsg = text
	a.statusExpires = time.Now().Add(flashLifetime)
}

// flashBump is the bottom-edge consumer of World.LastBumpEvent (ADR 0053
// decision 8, ADR 0048 Event Flash): what bumped, how hard, and what the
// docking gate asks for, e.g. "bumped S-IVB-1 at 0.40 m/s; re-approach under
// 0.10 m/s", or, for a touch the re-arm latch alone refused, "...; held off:
// back 5 m or alt+u". Reads and clears the event so a contact flashes once.
func (a *App) flashBump() {
	e := a.world.LastBumpEvent
	if e == nil {
		return
	}
	if e.Latched {
		// The approach met the gate; the re-arm latch refused it (#794).
		a.flash(fmt.Sprintf("bumped %s at %s; held off: back %.0f m or %s",
			e.PartnerName, readout.Speed(e.SpeedMS), sim.ReArmDistM, a.keys.ReArmDock.Help().Key))
	} else {
		a.flash(fmt.Sprintf("bumped %s at %s; re-approach under %s",
			e.PartnerName, readout.Speed(e.SpeedMS), readout.Speed(sim.DockingVMS)))
	}
	a.world.LastBumpEvent = nil
}

// refuse writes a one-phrase "<label>: <reason>" refusal to the HUD
// footer — the standing fix for #282: a guarded key that can't act right
// now must say why instead of doing nothing, the way the guest-settings
// guard (errGuestSettings via flashStatus) and the existing rendezvous
// ([K]) error path already do (see TestRendezvousRefusalLabelsOnce,
// which pins that the label appears exactly once). Generalised into one
// helper so every guarded key speaks with the same voice. label should
// name the action ("end flight", "rendezvous"), not repeat itself in
// reason. Refusals are Event Flashes too (ADR 0048), so this is a thin
// wrapper over flash.
func (a *App) refuse(label, reason string) {
	a.flash(fmt.Sprintf("%s: %s", label, reason))
}

// flashStatus writes a transient message to the HUD footer. The
// success path names the quicksave lane — F5/F9 are its only callers
// (v0.26 / ADR 0033 §D).
func (a *App) flashStatus(op string, err error) {
	if err != nil {
		a.flash(fmt.Sprintf("%s failed: %v", op, err))
		return
	}
	a.flash(fmt.Sprintf("%s ok: %s", op, save.QuicksaveID))
}

// handlePlanRendezvousKey is K's modal body (ADR 0045 S6, #399), called
// once the CraftVisibleHere gate has already passed. Delegates the actual
// decision to World.PlanRendezvousOrOpenPicker: a close, near-matched
// pair plants directly (unchanged v0.10.2 behavior); a phase-mismatched
// pair opens the Rendezvous Planner picker instead of refusing; a plane
// mismatch or a structural gate (no target, different primaries, already
// docked) refuses outright, naming the remedy.
func (a *App) handlePlanRendezvousKey() {
	out, err := a.world.PlanRendezvousOrOpenPicker()
	if err != nil {
		// item-3 UX batch (features finding 17): the refusal used to
		// name the missing precondition but not the action that
		// supplies it. Review finding 3 moved the "[t] to target it"
		// clause onto sim.ErrRendezvousNoTarget itself (rendezvous.go)
		// instead of special-casing it here, so [I] plane-match gets
		// the same fix for free rather than the two sibling keys
		// disagreeing on whether the refusal names it.
		a.flash(fmt.Sprintf("rendezvous: %v", err))
		return
	}
	switch {
	case out.Planted != nil:
		adv := out.Planted
		// ADR 0039 S1: arrival speed rides along as a plain info row — no
		// gate, no judgment, it just sizes the hand-flown part of the job
		// (#290 found this invisible at plan time: a "successful" plant
		// that in fact arrived 4.6 m/s under the lock gate, with nothing
		// on screen warning the player before they committed).
		// F11 (gate review, item4-A-review.md): TArrival is time-to-CA from
		// now after the burn, a future event; the old raw-seconds format
		// hardcoded a "T+" prefix on it, the exact inversion decision 2
		// exists to remove (T- means until, T+ means since), surviving
		// only because this flash lives one directory up from the guard.
		// readout.Countdown supplies its own signed prefix, so the
		// literal "T+" is gone from the format string entirely.
		a.flash(fmt.Sprintf("rendezvous nudge: %s %s → CA %s @ %s, arriving ~%s",
			readout.DeltaV(adv.DV), adv.Axis, readout.Distance(adv.AchievableCA),
			readout.Countdown(time.Duration(adv.TArrival*float64(time.Second))), readout.Speed(adv.ArrivalSpeed)))
		a.world.RecordAction(missions.ActionPlanRendezvous) // ADR 0025 §7
	case out.OpenPicker:
		// The picker is a chip on the orbit MAP (ADR 0045 §2) — switch
		// there if K was pressed from body-info/missions (both of which
		// also reach this case, #282) so the chip the player just
		// summoned is actually visible.
		a.active = screenOrbit
		a.orbitView.OpenRendezvousPicker(out.Place, out.Ladder, out.LadderErr)
		// G4 Q1b (#418): reading rows at coasting warp lets the burn epoch
		// run away, so the picker drops to 1x. Rows burn ~5 min out; [G]
		// after Enter auto-warps to the burn.
		a.world.DropWarpForPlanning()
		a.flash(rendezvousPickerOpenFlash)
	}
}

// rendezvousPickerOpenFlash is the footer line K's picker opens with. Kept
// under ~105 cells so it fits the 140-cell Design Size floor with room
// (Wave B review LOW 147: the first wording was 118).
const rendezvousPickerOpenFlash = "rendezvous plan: warp 1x, burns ~5 min out. [←→↑↓] walk, Enter plant, Esc cancel, then [G] warp to the burn"

// handleRendezvousPickerKey routes every keypress while the Rendezvous Planner
// picker (ADR 0045 S6, #399) is open. Mirrors the chat/end-flight-confirm
// discipline in the caller: switch and return unconditionally so nothing
// leaks to camera pan or flight controls while the picker holds input.
func (a *App) handleRendezvousPickerKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(m, a.keys.PickerLeft):
		a.orbitView.RendezvousPickerLeft()
		a.refreshRendezvousPickerLadder()
	case key.Matches(m, a.keys.PickerRight):
		a.orbitView.RendezvousPickerRight()
		a.refreshRendezvousPickerLadder()
	case key.Matches(m, a.keys.PickerUp):
		a.orbitView.RendezvousPickerUp()
	case key.Matches(m, a.keys.PickerDown):
		a.orbitView.RendezvousPickerDown()
	case key.Matches(m, a.keys.PlanRendezvous):
		// The expired-row refusal says "press [K]": K inside the open
		// picker re-solves the ladder from now (same Place), so the
		// pilot never has to Esc out and back in. Warp drops to 1x again
		// for the same reason it does on open (G4 Q1b).
		a.world.DropWarpForPlanning()
		a.refreshRendezvousPickerLadder()
		a.flash("rendezvous plan: rows re-solved from now, burns ~5 min out")
	case m.Type == tea.KeyEnter:
		a.planRendezvousPickerSelection()
	case key.Matches(m, a.keys.Back):
		a.orbitView.CloseRendezvousPicker()
	}
	return a, nil
}

// refreshRendezvousPickerLadder recomputes the ladder for the picker's
// (just-changed) Place against the live World and pushes it back in.
// RendezvousPickerLeft/Right themselves only walk the Place cycle — they
// can't call World (the picker's navigation state is intentionally
// World-free, see orbit_rendezvous_picker.go) — so the App does the
// recompute here, the same "App drives World, screen renders" split
// every other form on this screen follows.
func (a *App) refreshRendezvousPickerLadder() {
	place := a.orbitView.RendezvousPickerOrbit()
	ladder, err := a.world.RecommendRendezvousLadder(place)
	a.orbitView.SetRendezvousPickerLadder(place, ladder, err)
}

// planRendezvousPickerSelection is Enter's action: plant the highlighted Lap
// Ladder row. An unaffordable / unsafe / structurally-refused row keeps
// the picker OPEN with the refusal flashed — a bad row must not eject the
// player back to flight controls, since the whole point of showing
// unaffordable rows (ADR 0045 §2) is to let them pick a different one
// instead. Esc is still the only "give up" exit.
func (a *App) planRendezvousPickerSelection() {
	laps, ok := a.orbitView.RendezvousPickerSelectedLaps()
	if !ok {
		return // structurally-refused Place (#407): no row to plant.
	}
	place := a.orbitView.RendezvousPickerOrbit()
	// Plant the ladder the pilot READ (rows carry their own burn epoch,
	// #418), not a fresh solve against wherever the clock has got to.
	plan, err := a.world.PlanRendezvousFromLadder(place, a.orbitView.RendezvousPickerLadder(), laps)
	if err != nil {
		if place == planner.RendezvousYourOrbit && errors.Is(err, sim.ErrRendezvousUnsafePeriapsis) {
			// "your orbit": the PARTNER burns, so the generic "circularize
			// [C] first" names the wrong vessel. Lowering rows are often
			// unsafe; say where the safe rows are (Wave B review LOW 164).
			a.flash("rendezvous: their burn would drop their periapsis unsafely: try another lap row, or [←] their orbit so you burn instead")
			return
		}
		a.flash(fmt.Sprintf("rendezvous: %v", err))
		return
	}
	if plan.ForActive {
		a.flash(fmt.Sprintf("rendezvous burn planted: %.1f m/s → CA %.0f m, arriving ~%.0f m/s",
			plan.DV, plan.AchievableCA, plan.ArrivalSpeed))
		a.world.RecordAction(missions.ActionPlanRendezvous) // ADR 0025 §7
	} else {
		// RendezvousYourOrbit: the PARTNER is the mover. #399 out of scope —
		// carrying this plan to them over the wire is a later slice.
		a.flash(fmt.Sprintf("rendezvous plan (their burn): %.1f m/s → CA %.0f m, arriving ~%.0f m/s",
			plan.DV, plan.AchievableCA, plan.ArrivalSpeed))
	}
	a.orbitView.CloseRendezvousPicker()
}

// View delegates to the active screen, then overlays a transient
// status line at the bottom for ~3s after a save / load.
func (a *App) View() string {
	if a.width == 0 {
		return "initializing…"
	}
	// v0.27 S2 (ADR 0034): below the playable floor the gate replaces
	// every screen — local and ssh alike. The sim keeps ticking; only
	// rendering and gameplay input are held.
	if a.sizeGated() {
		return screens.RenderSizeGate(a.width, a.height)
	}
	var base string
	// B11 / G9 Q1: every non-flight screen wears the shared Title Row; its
	// body is rendered one row shorter to make room for it.
	// #599: every frame form also leaves the last row blank for the flash
	// (flashH is the height a form renders at); the maneuver planner keeps
	// its own reserve (it draws its legend and that blank row itself).
	bodyH := a.height - 1
	flashH := bodyH - 1
	switch a.active {
	case screenHelp:
		base = a.formScreen("Help", "", a.help.Render(a.width, flashH, a.layout))
	case screenBodyInfo:
		// The legend that advertises t/H/P rides the frame's bottom edge;
		// a "target: Mars" flash lands on the Flash Row formScreen leaves
		// under it (#599), so the legend stays.
		base = a.formScreen("Body info", a.bodyInfoName(), a.bodyInfo.Render(a.world, a.selectedBody, a.width, flashH))
	case screenManeuver:
		base = a.formTitled("Maneuver planner", a.maneuver.TitleContext(a.world), a.maneuver.Render(a.world, a.width, bodyH, a.selectedBody))
	case screenPorkchop:
		base = a.formScreen("Porkchop plot", a.porkchop.TitleContext(), a.porkchop.Render(a.world, a.width, flashH))
	case screenMenu:
		base = a.menuOverMap()
	case screenSpawn:
		base = a.formScreen("Spawn vessel", "", a.spawn.Render(a.width, flashH))
	case screenMissions:
		base = a.formScreen("Missions", "", a.missions.Render(a.world, a.width, flashH))
	case screenSettings:
		base = a.formScreen("Settings", "", a.settingsScreen.Render(a.orbitView.Settings(), a.width, flashH))
	case screenControls:
		base = a.formScreen("Keyboard layout", "", a.controls.Render(a.layout, a.width, flashH))
	case screenVAB:
		base = a.formScreen(a.vab.TitleScreen(), "", a.vab.Render(a.width, flashH))
	case screenSaves:
		base = a.formScreen("Saves", a.saves.TitleContext(), a.saves.Render(a.width, flashH))
	case screenBoss:
		base = a.boss.Render(a.width, a.height)
	case screenSession:
		base = a.formScreen("Session", "", a.session.Render(a.world, a.width, flashH))
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
	// Chat input (ADR 0035 S3) rides the same band and wins it while
	// open — it is the live actionable state. A concurrent toast (a DM
	// refusal) rides along after the draft so refusals stay visible
	// while the text waits to be fixed.
	if a.chatOpen {
		line := "~ " + string(a.chatInput) + "▏"
		if a.statusMsg != "" && time.Now().Before(a.statusExpires) {
			line += "  " + a.theme.Warning.Render(a.statusMsg)
		}
		base = overlayBottomBorder(base, a.theme.Primary.Render(line), border)
	}
	// A card (ADR 0054) is composited last, over everything above: the
	// point of asking is defeated if a stale toast or an open chat draft
	// could hide the question. The map is dimmed under it and the clock is
	// not held (Jason 2026-10-12, conflict 1).
	if a.card != nil {
		// Raised from the pause menu, the quit card replaces the pause card
		// rather than stacking on it (its edges would show above and below).
		if a.active == screenMenu {
			base = a.flightUnderCard()
		}
		base = a.cardOver(base, a.card.Render(), screens.CardW)
	} else if a.altBox.CapturingText() {
		base = a.cardOver(base, a.altBox.Render(), screens.CardW)
	} else if c := a.screenCard(); c != nil {
		// A screen's own ask (the roster's, ADR 0054) is composited the same
		// way, over the screen's own rendering.
		base = a.cardOver(base, c.Render(), screens.CardW)
	}
	return base
}

// cardScreen is a screen that asks questions as cards. It keeps the ask state
// and the keys; the App draws the card over the dimmed screen and routes a
// click on a row to it (ADR 0054, #633).
type cardScreen interface{ ActiveCard() *screens.Card }

// screenCard is the card the active screen has up, or nil.
func (a *App) screenCard() *screens.Card {
	var s any
	switch a.active {
	case screenSession:
		s = a.session
	case screenSaves:
		s = a.saves
	case screenVAB:
		s = a.vab
	}
	if cs, ok := s.(cardScreen); ok {
		return cs.ActiveCard()
	}
	return nil
}

// screensTheme is the theme the screens package takes, built from the
// App's own.
func (a *App) screensTheme() screens.Theme {
	th := a.theme
	return screens.Theme{
		Primary: th.Primary, Warning: th.Warning, Alert: th.Alert, Dim: th.Dim,
		HUDBox: th.HUDBox, Footer: th.Footer, Title: th.Title,
	}
}

// formScreen puts the shared Title Row (B11 / G9 Q1) above a non-flight
// screen's body and records where its [Back] button sits so a click on it
// can be routed (esc is the keyboard twin).
func (a *App) formScreen(screen, context, body string) string {
	// The blank last row is the flash row (#599): a status flash overlays
	// it (overlayBottomBorder's plain-row fallback), so the legend on the
	// frame's bottom edge is never covered.
	return a.formTitled(screen, context, body) + "\n"
}

// formTitled is formScreen without the reserved flash row, for a screen that
// already ends in one (the maneuver planner).
func (a *App) formTitled(screen, context, body string) string {
	row, bs, be := screens.RenderFormTitleRow(a.screensTheme(), a.world, screen, context, a.width)
	a.backStart, a.backEnd = bs, be
	return row + "\n" + body
}

// menuOverMap draws the pause card centred over the dimmed map (B11 / G9
// Q6). The flight Title Row stays as it is, reading PAUSED; everything under
// it goes grey, and the card is spliced in by cell.
func (a *App) menuOverMap() string {
	a.backStart, a.backEnd = 0, 0 // no [Back] on the flight bar
	return a.cardOver(a.flightUnderCard(), a.menu.Render(), screens.MenuCardW)
}

// flightUnderCard is the flight view a card sits on: the launch view or the
// map, whichever the player was flying in.
func (a *App) flightUnderCard() string {
	if a.world.ViewMode == sim.ViewLaunch {
		return a.launchView.Render(a.world, a.width, a.height)
	}
	return a.orbitView.Render(a.world, a.selectedBody, a.width, a.height)
}

// cardOver splices a card centred over a rendered screen (B11 / G9 Q6 pause
// card; the quit card reuses it). Row 0, the Title Row, stays as it is;
// everything under it goes grey, and the card is spliced in by cell. It
// records the card's origin for click routing (menuOriginX/Y).
func (a *App) cardOver(under, cardStr string, cardW int) string {
	lines := strings.Split(under, "\n")
	card := strings.Split(cardStr, "\n")
	// Centre the card in the rows below the Title Row.
	x, y := a.cardOrigin(cardW, len(card))
	a.menuOriginX, a.menuOriginY = x, y-1
	dim := a.theme.Dim
	// One clear cell all round the card (B11 review L11): without it a map
	// box border meets the card corner (`╭───╭`). The margin is real spaces,
	// so braille dots and box lines stop a cell short of the card.
	ml, mr := 1, 1
	if x < 1 {
		ml = 0
	}
	if x+cardW+1 > a.width {
		mr = 0
	}
	for i := 1; i < len(lines); i++ {
		plain := ansi.Strip(lines[i])
		if r := i - y; r >= 0 && r < len(card) {
			lines[i] = dim.Render(ansi.Truncate(plain, x-ml, "")+strings.Repeat(" ", ml)) + card[r] +
				dim.Render(strings.Repeat(" ", mr)+ansi.TruncateLeft(plain, x+cardW+mr, ""))
			continue
		}
		if r := i - y; r == -1 || r == len(card) {
			lines[i] = dim.Render(ansi.Truncate(plain, x-ml, "") +
				strings.Repeat(" ", cardW+ml+mr) +
				ansi.TruncateLeft(plain, x+cardW+mr, ""))
			continue
		}
		lines[i] = dim.Render(plain)
	}
	return strings.Join(lines, "\n")
}

// bodyInfoName is the selected body's name for the Body info Title Row.
func (a *App) bodyInfoName() string {
	sys := a.world.System()
	if a.selectedBody >= 0 && a.selectedBody < len(sys.Bodies) {
		return sys.Bodies[a.selectedBody].EnglishName
	}
	return ""
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
// maneuver form for a BRAND-NEW node (`m` on the orbit screen, or the
// click-an-empty-canvas-point staging flow) so the four target-relative
// burn modes and the TriggerNextClosestApproach event are pickable +
// correctly captured at plant. Bound at form-open time (not per-
// keypress), so a target switch while the form is open doesn't silently
// retarget a planted burn — the player closes + reopens to retarget.
//
// NOT called after LoadNode (click-to-edit an existing node): that path
// preserves the node's OWN stored binding instead — see LoadNode's and
// the Maneuver struct's doc comments (#294 review round 3 finding I).
//
// v0.9.3+; ghost-aware since #294 review round 3 (previously only
// TargetCraft was handled here, so opening a NEW node's form while
// aimed at a ghost silently left it untargeted).
func (a *App) bindManeuverTarget() {
	switch a.world.Target.Kind {
	case sim.TargetCraft:
		a.maneuver.SetTargetCraft(true, "", a.world.Target.CraftID)
	case sim.TargetGhost:
		a.maneuver.SetTargetCraft(true, a.world.Target.GhostOwner, a.world.Target.CraftID)
	default:
		a.maneuver.SetTargetCraft(false, "", 0)
	}
}

// closeManeuverToOrbit resets the maneuver form's click-to-edit / Plan
// Cursor state, resumes the clock, and returns to the map — the common
// "leave the planner" tail shared by the form's own commit/delete/
// clear-all/cancel paths and the QUICK PLANS dispatch (ADR 0047 §4 /
// #428).
func (a *App) closeManeuverToOrbit() {
	a.maneuver.ResetEditing()
	a.world.Clock.Paused = false
	a.active = screenOrbit
}

// activeCraftNodes returns the active craft's planted-node slice, or an
// empty slice when there's no active craft — the Plan Cursor (ADR 0047 /
// #428) needs the live node count/contents on every keypress, not just
// at Render time.
func (a *App) activeCraftNodes() []sim.ManeuverNode {
	if c := a.world.ActiveCraft(); c != nil {
		return c.Nodes
	}
	return nil
}

// plantedBurnSummary names the burn that makes H refuse, in the words and
// with the time ENGINE's node: row uses for it: a firing burn reads like
// the row's live-burn branch, a queued node by its burn start (or its
// trigger event while it waits to resolve). Empty when nothing is planted
// or firing.
func (a *App) plantedBurnSummary() string {
	c := a.world.ActiveCraft()
	if c == nil {
		return ""
	}
	if ab := c.ActiveBurn; ab != nil {
		return fmt.Sprintf("%s %s left, firing", ab.Mode, readout.DeltaV(ab.DVRemaining))
	}
	if len(c.Nodes) == 0 {
		return ""
	}
	n := c.Nodes[0]
	if !n.IsResolved() {
		return fmt.Sprintf("%s %s, %s", n.Mode, readout.DeltaV(n.DV), n.Event)
	}
	dt := n.BurnStart().Sub(a.world.Clock.SimTime)
	if dt < 0 {
		dt = 0
	}
	return fmt.Sprintf("%s %s in %s", n.Mode, readout.DeltaV(n.DV), readout.Duration(dt))
}

// flashStoppedBurn announces a planted burn that clearing the plan just
// stopped (#588 lead 1). Silent when nothing was firing.
func (a *App) flashStoppedBurn(ab *sim.ActiveBurn) {
	if ab == nil {
		return
	}
	a.flash(fmt.Sprintf("burn stopped: %s, %s unburned", ab.Mode, readout.DeltaV(ab.DVRemaining)))
}

// doPlanTransfer executes `H`: plant a transfer to World.Target. Shared
// by the map binding and the planner's QUICK PLANS block (ADR 0047 §4 /
// #428), which routes H to this exact method so pressing it inside the
// planner "does exactly what it does on the map."
func (a *App) doPlanTransfer() {
	if !a.world.CraftVisibleHere() {
		// #282 sweep: same state-guard shape as K/E — say why.
		a.refuse("transfer", "vessel not in this system")
		return
	}
	// #587 (GS Q6): one transfer at a time. Any planted burn, or a planted
	// burn already firing, blocks H: nothing is replaced (half a departure
	// cannot be un-flown) and nothing is stacked. The game does not
	// remember which burns H planted, so a hand-made burn blocks it too.
	if planted := a.plantedBurnSummary(); planted != "" {
		a.refuse("transfer", fmt.Sprintf("a burn is already planted (%s); clear it in [m] first", planted))
		return
	}
	switch a.world.Target.Kind {
	case sim.TargetBody:
		if _, err := a.world.PlanTransfer(a.world.Target.BodyIdx); err != nil {
			var other *sim.OtherPlanetsMoonError
			if errors.As(err, &other) { // #610: beyond the planning assists
				a.refuse("transfer", err.Error())
				return
			}
		}
		a.world.RecordAction(missions.ActionPlanTransfer) // ADR 0025 §7
		// v0.12.x (ADR 0005): the intra-primary auto-plant is
		// now a plane-aware dual-strategy solver (combined
		// fused-Lambert vs split raise + apoapsis plane change)
		// that plants the cheaper — so flash both candidate Δv
		// totals and which was planted (supersedes the retired
		// "match plane [I], circularize, then [H]" advisory).
		// Non-intra-primary plants leave the comparison empty.
		if cmp := a.world.LastTransfer.Format(); cmp != "" {
			a.flash(cmp)
		}
	case sim.TargetCraft:
		a.flash("H targets bodies — for vessels, plan via [m]")
	default:
		// #428 mechanical fix: this used to be a deliberate silent
		// no-op (there was "genuinely nothing to name" for TargetNone)
		// — the UX review found that indistinguishable from a broken
		// keybinding. Refuse out loud like every sibling guard does.
		a.refuse("transfer", "no target — press t to aim at a body")
	}
}

// doPlanPlaneMatch executes `I`: plant a plane-match burn. See
// doPlanTransfer's doc for why this exists as a standalone method.
func (a *App) doPlanPlaneMatch() {
	if !a.world.CraftVisibleHere() {
		// #282 sweep: same state-guard shape as K/E — say why.
		a.refuse("inclination", "vessel not in this system")
		return
	}
	// #548 review: a vessel on the pad has no orbit to tilt (its
	// co-rotation pseudo-orbit read as "already at target inclination"
	// while TARGET showed a 47 degree gap). The pad's plane change is the
	// launch heading, which NAVIGATION's plan: row names.
	if c := a.world.ActiveCraft(); c != nil && c.Landed {
		a.refuse("inclination", "on the pad, no orbit to tilt: fly the plan: row's heading")
		return
	}
	// v0.9.0+: I consumes World.Target. TargetBody → full
	// plane match to the body's orbit (v0.10.4: matches
	// inclination AND the node line, so a following Hohmann
	// departs coplanar); None → drop to the equatorial plane
	// of the craft's primary (the equatorial inclination
	// match shipped with v0.7.4); TargetCraft/TargetGhost
	// (ADR 0045 S4, #397) → match the OTHER VESSEL's plane
	// (relative angular momentum rT × vT gives the node line
	// and rotation angle), not a scalar tilt number — two
	// vessels can share an inclination magnitude and still sit
	// in different planes (different RAAN), which the old
	// scalar match could never fix.
	var plan *planner.InclinationPlan
	var err error
	switch a.world.Target.Kind {
	case sim.TargetBody:
		plan, err = a.world.PlanPlaneMatch(a.world.Target.BodyIdx)
	case sim.TargetCraft, sim.TargetGhost:
		plan, err = a.world.PlanVesselPlaneMatch()
	default:
		plan, err = a.world.PlanInclinationChange(0)
	}
	if err != nil {
		// The planner's errors carry their package prefix; the key's own
		// label is enough.
		a.flash(fmt.Sprintf("inclination: %s", strings.TrimPrefix(err.Error(), "planinclination: ")))
	} else {
		nodeLabel := "DN"
		if plan.AtAN {
			nodeLabel = "AN"
		}
		a.flash(fmt.Sprintf("inclination plan — %.1f m/s at next %s",
			plan.DV, nodeLabel))
	}
	a.world.RecordAction(missions.ActionPlanIncl) // ADR 0025 §7
}

// doPlanCircularize executes `C`: plant a circularizing burn at next
// apoapsis. See doPlanTransfer's doc for why this exists as a
// standalone method.
func (a *App) doPlanCircularize() {
	if !a.world.CraftVisibleHere() {
		// #282 sweep: same state-guard shape as K/E — say why.
		a.refuse("circularize", "vessel not in this system")
		return
	}
	plan, err := a.world.PlanCircularizeAtApoapsis()
	if err != nil {
		a.flash(fmt.Sprintf("circularize: %v", err))
	} else {
		a.flash(fmt.Sprintf("circularize @ apoapsis (%.0f km) → +%.0f m/s prograde",
			plan.ApoAltM/1000, plan.DV))
	}
	a.world.RecordAction(missions.ActionPlanCircularize) // ADR 0025 §7
}

// doPlanRendezvous executes `K`: plant the recommended nudge toward the
// target vessel, or open the Rendezvous Planner picker. See
// doPlanTransfer's doc for why this exists as a standalone method.
func (a *App) doPlanRendezvous() {
	switch {
	case !a.world.CraftVisibleHere():
		a.refuse("rendezvous", "vessel not in this system")
	default:
		a.handlePlanRendezvousKey()
	}
}

// doOpenPorkchop executes `P`: open the porkchop plot for the TARGET
// body (#502). Returns true when it actually opened (so callers know whether
// to also tear down whatever screen they were leaving). See
// doPlanTransfer's doc for why this exists as a standalone method.
func (a *App) doOpenPorkchop() bool {
	switch {
	case !a.world.CraftVisibleHere():
		// #282 sweep: on the orbit screen this key was silently
		// ignored without a visible target or a vessel to plan
		// from — same state-guard shape as the K/E fix, so it gets
		// the same one-phrase treatment.
		a.refuse("porkchop", "vessel not in this system")
		return false
	default:
		// #502: Target wins. The body cursor (h/l) feeds i, not P.
		idx, reason, ok := a.world.PorkchopTarget()
		if !ok {
			a.refuse("porkchop", reason)
			return false
		}
		a.porkchop.Load(a.world, idx)
		a.active = screenPorkchop
		return true
	}
}

// doTargetShownBody executes `t` on the body info screen (#495): set the
// Target to the body the screen shows. Refuses in one phrase where
// SetTargetBody would clear (the star) or where the cycle never offers the
// body (the one you orbit), and says so when it is already the Target.
func (a *App) doTargetShownBody() {
	sys := a.world.System()
	idx := a.selectedBody
	if idx < 0 || idx >= len(sys.Bodies) {
		a.refuse("target", "no body selected")
		return
	}
	b := sys.Bodies[idx]
	switch {
	case idx == 0:
		a.refuse("target", "a star can't be targeted")
		return
	case a.world.Target.Kind == sim.TargetBody && a.world.Target.BodyIdx == idx:
		a.refuse("target", b.EnglishName+" is already targeted")
		return
	}
	if c := a.world.ActiveCraft(); c != nil && a.world.CraftVisibleHere() && c.Primary.ID == b.ID {
		a.refuse("target", "you are orbiting "+b.EnglishName)
		return
	}
	a.world.SetTargetBody(idx)
	a.world.RecordAction(missions.ActionCycleTarget) // ADR 0025 §7
	a.flash("target: " + b.EnglishName)
}

// doRefinePlan executes `R`: re-Lambert the pending arrival and plant a
// mid-course correction. See doPlanTransfer's doc for why this exists
// as a standalone method.
func (a *App) doRefinePlan() {
	if !a.world.CraftVisibleHere() {
		// #282 sweep: same state-guard shape as K/E — say why.
		a.refuse("refine", "vessel not in this system")
		return
	}
	corr, arr, err := a.world.RefinePlan()
	if err != nil {
		a.flash(fmt.Sprintf("refine failed: %v", err))
	} else {
		a.flash(fmt.Sprintf("refined — correction %.1f m/s, arrival %.1f m/s", corr, arr))
	}
	a.world.RecordAction(missions.ActionRefinePlan) // ADR 0025 §7
}

// orbitalSemiMajor is craft c's semi-major axis about its primary, metres.
func orbitalSemiMajor(c *spacecraft.Spacecraft) float64 {
	return orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter()).A
}
