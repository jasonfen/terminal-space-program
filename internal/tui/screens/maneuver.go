package screens

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// Maneuver is the burn-planning screen. Opening it pauses the sim (app.go
// handles the pause); closing with Esc cancels, with Enter emits a
// BurnExecutedMsg that the app applies to the spacecraft.
//
// Per plan §C20: live preview = shadow trajectory on a miniature canvas.
// v0.2.1: three fields — mode / Δv / duration. v0.6.0: four fields —
// mode / fire-at / Δv / duration. v0.6.5: three fields again — mode /
// fire-at / Δv. Duration is no longer an independent input; the planner
// derives it from Δv via the rocket equation at commit time, since at
// fixed thrust + mass the two are the same dial — letting the player
// set both was over-determined and the only effect of mismatch was a
// truncated burn (planned Δv undelivered if duration was too short).
// KSP-style: specify Δv, the engine takes as long as it takes.
type Maneuver struct {
	theme         Theme
	canvas        *widgets.Canvas
	dvInput       textinput.Model
	throttleInput textinput.Model // v0.7.6+: per-node throttle (0-100 %)

	modeIdx          int
	fireAtIdx        int
	focus            int  // 0=mode, 1=fireAt, 2=dv, 3=throttle (v0.7.6+), 4=iterate (v0.8.6 (b))
	iterateForTarget bool // v0.8.6 (b): when true, refine commanded Δv via planner.IterateForTarget at plant time so the post-burn apsides match the projected-orbit preview (compensates finite-burn loss). Off by default — preserves impulsive-target semantics for short / low-loss burns where the refinement is below resolution.

	// editingIdx and loadedTriggerTime carry the v0.6.4 click-to-edit
	// state. Default editingIdx = -1 (creating a new node). LoadNode
	// sets them so the next BurnExecutedMsg can replace the original
	// node in place AND preserve its scheduled trigger time —
	// otherwise re-planting an Absolute-event node would lose its
	// future TriggerTime and fall back to the legacy "fire now"
	// quick-plant path.
	editingIdx        int
	loadedTriggerTime time.Time

	// hasTargetCraft + targetCraftID + targetGhostOwner carry the bound
	// target for the form's four target-relative burn modes and the
	// TriggerNextClosestApproach event to resolve their direction /
	// trigger against — local craft (targetGhostOwner=="") or a remote
	// player's ghost (targetGhostOwner!="", v0.28 S4 / ADR 0034).
	//
	// Two different callers bind this, with two different intents
	// (#294 review round 3 finding I):
	//
	//   - LoadNode (click-to-edit an existing planted node) reads the
	//     node's OWN stored binding — preserving it is the point of
	//     opening the form on an already-target-bound node, so a
	//     reloaded ghost-bound node doesn't lose its lock just because
	//     it's being edited.
	//   - bindManeuverTarget (app.go, called for a brand-NEW node —
	//     `m` on the orbit screen, or the click-an-empty-canvas-point
	//     staging flow) reads the CURRENT World.Target instead, since a
	//     new node has no binding of its own yet to preserve.
	//
	// Neither is re-read per keypress: a target switch while the form
	// is open doesn't silently retarget a planted burn, and — for the
	// edit case — the only way to point an already-bound node at a
	// DIFFERENT target is to close the form, retarget, then reopen for
	// a fresh (now new-node-shaped) bind. v0.9.3+; bound by stable
	// craft ID since v0.14.x (ADR 0012); ghost-owner-aware since #294
	// review round 3.
	hasTargetCraft   bool
	targetCraftID    uint64
	targetGhostOwner string

	// advisoryKey (#294 second-round review finding 6) carries a loaded
	// node's spacecraft.ManeuverNode.AdvisoryKey through the edit cycle,
	// mirroring hasTargetCraft/targetCraftID/targetGhostOwner above.
	// Without this, editing a planted K-nudge (or C-circularize) and
	// committing re-planted it with NO AdvisoryKey at all — a later
	// press of the same key could no longer find and replace it (#293's
	// "replace, don't stack" ruling broke), and the edited node lost its
	// advisory identity outright. Set unconditionally in LoadNode (so it
	// correctly clears to "" for a non-advisory node too, same leak this
	// struct's target-binding fields guard against — finding 2) and
	// cleared in ResetEditing so a later NEW-node open never inherits a
	// stale key from whatever was edited last.
	advisoryKey string

	// cursorIdx is the Plan Cursor (ADR 0047 / #428): the row ↑/↓ move
	// through PLANNED NODES, independent of the Tab-cycled form-field
	// focus above (↑/↓ were unused inside the form before this). A
	// value in [0, len(Nodes)-1] names a planted node; len(Nodes) itself
	// names the blank new-node row at the list's end. Negative is the
	// "unset" sentinel ResetEditing/LoadStaged write for a brand-new-
	// node open — cursorRow resolves it (and any now-out-of-range value)
	// to the new-node row given the live node count. LoadNode sets this
	// to the loaded index directly, so the mouse click-to-edit path and
	// keyboard Enter-to-load path always agree on where the cursor is.
	cursorIdx int
}

// cursorRow resolves the Plan Cursor to a concrete row index in [0, n]
// given the current planted-node count n (n itself names the new-node
// row). Handles both the negative "unset" sentinel and an out-of-range
// value (the node count shrank under the cursor via some other path)
// by falling back to the new-node row.
func (m *Maneuver) cursorRow(n int) int {
	if m.cursorIdx < 0 || m.cursorIdx > n {
		return n
	}
	return m.cursorIdx
}

// TextFieldFocused reports whether the Δv or throttle text input
// currently owns keystrokes. The app uses this to gate the QUICK PLANS
// letter bindings (H/I/C/K/P/R) inside the planner (ADR 0047 §4): they
// fire only when no text-entry field has focus, so typing a Δv value
// never gets hijacked into planting a burn.
func (m *Maneuver) TextFieldFocused() bool {
	return m.focus == 2 || m.focus == 3
}

// SetTargetCraft binds (or unbinds) the target — local craft (owner=="")
// or remote ghost (owner!="") — the form's planted burn will be aimed
// at. Called by the app when opening the form for a NEW node so the
// four target-relative burn modes and the TriggerNextClosestApproach
// event can resolve at plant + fire time. Pass ok=false to clear (no
// target set / target is a body). v0.9.3+; binds by ID since v0.14.x
// (ADR 0012); ghost-owner-aware since #294 review round 3 (finding I).
func (m *Maneuver) SetTargetCraft(ok bool, owner string, id uint64) {
	m.hasTargetCraft = ok
	if ok {
		m.targetCraftID = id
		m.targetGhostOwner = owner
	} else {
		m.targetCraftID = 0
		m.targetGhostOwner = ""
	}
	// If the currently-selected mode or trigger requires a target
	// and we no longer have one, snap to safe defaults so the form
	// renders something fireable.
	if !m.hasTargetCraft {
		mode := spacecraft.AllBurnModes[m.modeIdx]
		if spacecraft.IsTargetRelativeMode(mode) {
			m.modeIdx = 0
		}
		if sim.AllTriggerEvents[m.fireAtIdx] == sim.TriggerNextClosestApproach {
			m.fireAtIdx = 0
		}
	}
}

// BurnExecutedMsg is emitted when the user hits Enter. App consumes it.
// Event (v0.6.0+) selects the trigger model — TriggerAbsolute uses the
// app-side default delay; event-relative modes leave TriggerTime zero
// and let the World's lazy-freeze resolver compute it from the live
// orbit on the first Tick after plant.
//
// v0.6.4+: TriggerTime non-zero forces the app to plant a real
// ManeuverNode at exactly that time (skipping the legacy "fire now"
// path used by quick-plant). Set by LoadNode so a click-to-edit
// flow preserves the original schedule. EditingIdx ≥ 0 tells the
// app to remove the original Nodes[idx] before planting, so the
// edit reads as "replace in place" rather than "duplicate."
//
// v0.6.5: Duration dropped from this message — the App computes it
// on receipt via spacecraft.BurnTimeForDV(DV) using the live craft's
// thrust + Isp + mass. Letting the player set both Δv AND duration
// was over-determined: at fixed thrust + mass the two are the same
// dial, and the only effect of mismatch was a truncated burn
// (planned Δv undelivered if the duration was too short). Zero-thrust
// craft return Duration = 0 from BurnTimeForDV, preserving the
// impulsive code path even though the form no longer exposes it.
type BurnExecutedMsg struct {
	Mode        spacecraft.BurnMode
	DV          float64
	Event       sim.TriggerEvent
	TriggerTime time.Time
	EditingIdx  int // -1 = creating a new node; ≥ 0 = replacing world.Nodes[idx]
	// Throttle (v0.7.6+) is the per-node throttle [0, 1]. Zero is
	// remapped to 1.0 by ManeuverNode.EffectiveThrottle, so callers
	// that don't set it (legacy quick-plant paths) get the prior
	// full-open behaviour for free.
	Throttle float64
	// IterateForTarget (v0.8.6 (b)) requests that the app refine the
	// commanded Δv via World.IterateBurnDV before planting, so the
	// post-burn apsides match what an impulsive Δv at the same
	// commanded value would have delivered (compensating finite-burn
	// loss). Ignored for impulsive (zero-thrust) and Normal± burns.
	IterateForTarget bool
	// TargetCraftID (v0.14.x / ADR 0012; was the one-based index
	// TargetCraftIdx) is the stable Spacecraft.ID the form was bound to
	// at plant. Zero = no target. Mirrors ManeuverNode.TargetCraftID;
	// the app passes it straight through. Only populated for target-
	// relative modes / TriggerNextClosestApproach event.
	TargetCraftID uint64
	// TargetGhostOwner (#294 review round 3 finding I) mirrors
	// ManeuverNode.TargetGhostOwner: non-empty when TargetCraftID names a
	// REMOTE player's craft rather than a local one. Without this the
	// form had no way to tell the app a bound target was a ghost, so
	// committing an edit on a ghost-bound node silently re-planted it as
	// an (unresolvable) local ref — the binding survived LoadNode only
	// to be dropped again at commit.
	TargetGhostOwner string
	// AdvisoryKey (#294 second-round review finding 6) mirrors
	// ManeuverNode.AdvisoryKey through the edit cycle. Before this field
	// existed, editing a planted single-keystroke advisory node (K's
	// PlanRendezvousNudge / C's PlanCircularizeAtApoapsis) and committing
	// re-planted it with no AdvisoryKey at all — the edited node lost its
	// advisory identity, so a later press of the SAME key could no
	// longer find and replace it (World.replaceAdvisoryNode matches on
	// this field) and instead stacked a stale duplicate behind it,
	// breaking #293's "replace, don't stack" ruling. Empty for every
	// ordinary (non-advisory) node, same zero-value-omitempty
	// convention as the field it mirrors.
	AdvisoryKey string
}

// NodeDeleteMsg is emitted when the player presses ctrl+d in the
// maneuver form while editing a planted node. The app handles it
// by calling World.DeleteNode(EditingIdx) and closing the screen.
// Replaces the v0.8.5-and-earlier `N` global "clear all nodes"
// keybinding for the per-node case. v0.8.6+.
type NodeDeleteMsg struct {
	EditingIdx int
}

// NodeClearAllMsg is emitted when the player presses c / C (or the
// ctrl+k back-compat alias) in the maneuver form. The app handles
// it by calling World.ClearNodes() and closing the screen. Replaces
// the v0.8.5-and-earlier `N` global keybinding for the wipe-all
// case. v0.8.6+; primary binding simplified to `c` in v0.10.1.
type NodeClearAllMsg struct{}

func NewManeuver(th Theme) *Maneuver {
	dv := textinput.New()
	dv.Placeholder = "0"
	dv.CharLimit = 8
	dv.Width = 10
	dv.SetValue("100")

	throttle := textinput.New()
	throttle.Placeholder = "100"
	throttle.CharLimit = 3
	throttle.Width = 5
	throttle.SetValue("100")

	m := &Maneuver{
		theme:         th,
		canvas:        widgets.NewCanvas(60, 20),
		dvInput:       dv,
		throttleInput: throttle,
		editingIdx:    -1,
	}
	m.applyFocus()
	return m
}

// ResetEditing clears the click-to-edit state so the next commit
// plants a fresh node rather than replacing one. Called on `m`-key
// open (new-node intent) and after every BurnExecutedMsg / Esc so
// the editingIdx doesn't leak across opens.
func (m *Maneuver) ResetEditing() {
	m.editingIdx = -1
	m.loadedTriggerTime = time.Time{}
	// #294 second-round review finding 6: clear the advisory-key carry
	// too — every path that opens a NEW node's form (LoadStaged, or
	// bindManeuverTarget after the `m` quick-plant key) calls this
	// first, and without clearing it here a stale key from whatever was
	// last edited would leak onto the new, non-advisory node.
	m.advisoryKey = ""
	// Plan Cursor (ADR 0047 / #428): a NEW-node open starts on the
	// blank new-node row. -1 is the "unset" sentinel cursorRow resolves
	// against the live node count at Render/HandleKey time, since this
	// method doesn't know the count itself.
	m.cursorIdx = -1
}

// LoadStaged opens the form for a NEW node staged at a specific
// trigger time — used by the v0.6.4 empty-canvas mouse path to
// "click a point on the orbit, plant a burn there." Distinct from
// LoadNode in that there's no original to replace (editingIdx
// stays at -1); the form simply previews and commits with the
// staged TriggerTime so the new node fires at the click's
// projected orbit position. Mode / fire-at fall back to defaults
// (prograde / Absolute); Δv defaults to "100" so the form is
// immediately usable, focus jumps to the Δv field so the player
// can type a value without tabbing.
func (m *Maneuver) LoadStaged(triggerTime time.Time) {
	m.editingIdx = -1
	m.loadedTriggerTime = triggerTime
	m.modeIdx = 0   // prograde — the most common new-burn intent
	m.fireAtIdx = 0 // TriggerAbsolute — the staged TriggerTime IS the absolute schedule
	m.dvInput.SetValue("100")
	m.throttleInput.SetValue("100")
	m.focus = 2 // Δv input — player typically wants to set magnitude first
	// #294 second-round review finding 6: this is a NEW-node entry point
	// (like ResetEditing, which this doesn't route through) — clear any
	// advisory key carried over from whatever was last edited, or a
	// staged plain-click plant would inherit a stale K/C identity.
	m.advisoryKey = ""
	// Plan Cursor (ADR 0047 / #428): same "new-node row" default as
	// ResetEditing — see cursorIdx's doc comment.
	m.cursorIdx = -1
	m.applyFocus()
}

// LoadNode pre-populates the form fields from an existing planted
// node and records the click-to-edit state — used by the v0.6.4
// orbit-canvas mouse path. Maps the node's BurnMode + TriggerEvent
// back to their cycle indices, writes Δv / duration into the text
// inputs, and stores idx + TriggerTime so the next Enter commit
// emits a BurnExecutedMsg with EditingIdx = idx + TriggerTime
// = original schedule. The app then removes Nodes[idx] before
// planting so the edit replaces in place AND preserves the
// node's future trigger time.
func (m *Maneuver) LoadNode(idx int, n sim.ManeuverNode) {
	m.modeIdx = 0
	for i, mode := range spacecraft.AllBurnModes {
		if mode == n.Mode {
			m.modeIdx = i
			break
		}
	}
	m.fireAtIdx = 0
	for i, ev := range sim.AllTriggerEvents {
		if ev == n.Event {
			m.fireAtIdx = i
			break
		}
	}
	m.dvInput.SetValue(fmt.Sprintf("%.0f", n.DV))
	m.throttleInput.SetValue(fmt.Sprintf("%.0f", n.EffectiveThrottle()*100))
	m.focus = 0
	m.editingIdx = idx
	m.loadedTriggerTime = n.TriggerTime
	// v0.9.3+: preserve the node's stored target binding through the
	// edit cycle so re-planting doesn't drop it — this is authoritative
	// for the edit flow; unlike the new-node flow, the app does NOT
	// follow this call with bindManeuverTarget (#294 review round 3
	// finding I: bindManeuverTarget only knew TargetCraft, so it used to
	// unconditionally clobber whatever this just loaded the instant the
	// live World.Target was a ghost, or None, or anything else — most
	// visibly stripping a reloaded ghost-bound node's lock the moment
	// the player clicked it to edit, even though nothing about the node
	// itself had changed). Ghost owner rides along with the ID so a
	// remote ref survives the edit cycle too, not just a local one.
	if id, ok := n.TargetCraftIDValue(); ok {
		m.hasTargetCraft = true
		m.targetCraftID = id
		m.targetGhostOwner = n.TargetGhostOwner
	} else {
		// #294 second-round review finding 2: the Maneuver screen is one
		// long-lived value (app.go) reused across every plant/edit, so
		// without this else-branch clear a PRIOR node's ghost/craft
		// binding leaks onto a later untargeted node: open a ghost-bound
		// node, Esc, then click-to-edit an untargeted one — the form
		// would still offer target-relative modes and commitCmd would
		// stamp the stale refs onto a node that was never bound to
		// anything.
		m.hasTargetCraft = false
		m.targetCraftID = 0
		m.targetGhostOwner = ""
	}
	// #294 second-round review finding 6: carry the node's own
	// AdvisoryKey through the edit cycle too, unconditionally (not
	// gated on the target binding above — a K-nudge's mode is often a
	// plain velocity-frame axis, not one of the four target-relative
	// modes, yet it still carries an AdvisoryKey). Assigning n.AdvisoryKey
	// directly — rather than an if/else — both captures it for an
	// advisory node and correctly clears it to "" for an ordinary one,
	// the same leak class finding 2 fixed for the target-binding fields.
	m.advisoryKey = n.AdvisoryKey
	// Plan Cursor (ADR 0047 / #428): keep the keyboard cursor and the
	// mouse click-to-edit path in agreement about which row is loaded —
	// a click on a node's map glyph moves the cursor to it exactly as
	// pressing Enter on that row would.
	m.cursorIdx = idx
	m.applyFocus()
}

// applyFocus pushes focus state down to the bubbletea text inputs.
// Focus 0 = mode (cycle), 1 = fire-at (cycle), 2 = Δv, 3 = throttle.
// v0.6.5 dropped the duration field. v0.7.6+ added throttle as a
// fourth stop so per-node throttle is editable in the form.
func (m *Maneuver) applyFocus() {
	m.dvInput.Blur()
	m.throttleInput.Blur()
	switch m.focus {
	case 2:
		m.dvInput.Focus()
	case 3:
		m.throttleInput.Focus()
	}
}

// Resize handles terminal-size changes. Keep the maneuver canvas ≤ 60 cols
// wide so the form panel sits cleanly alongside it.
func (m *Maneuver) Resize(cols, rows int) {
	// Horizontal layout (v0.6.4 fix): canvas on the left, form panel
	// on the right. Sized so canvas + form sit side-by-side under
	// the title and footer rather than stacking vertically — pre-fix
	// the form's ~14 rows added on top of canvas rows-6 overflowed
	// any terminal under ~36 rows tall, scrolling the title off the
	// top in some renderers.
	canvasCols := cols * 6 / 10
	if canvasCols < 20 {
		canvasCols = 20
	}
	if canvasCols > 80 {
		canvasCols = 80
	}
	// Reserve 3 rows for title (1) + footer (1) + a 1-row gap between
	// title and the canvas-panel border.
	canvasRows := rows - 3
	if canvasRows < 6 {
		canvasRows = 6
	}
	m.canvas.Resize(canvasCols, canvasRows)
}

// HandleKey routes planner-local keys. Returns (cmd, done) where done=true
// means the app should exit the maneuver screen (commit or cancel). nodes
// is the active craft's planted-node slice (empty/nil when there's no
// active craft) — needed to bound the Plan Cursor and to load the node it
// points at on Enter (ADR 0047 / #428).
//
// Key bindings:
//
//	tab / shift+tab        — cycle focus across mode / fire-at / Δv fields
//	←/→ (mode focused)     — cycle direction modes
//	←/→ (fire-at focused)  — cycle trigger events (Absolute / NextPeri / NextApo / NextAN / NextDN)
//	↑/↓                    — move the Plan Cursor through PLANNED NODES
//	enter                  — Plan Cursor on an unloaded planted node: load it for editing.
//	                          Otherwise: commit the form → BurnExecutedMsg with rocket-equation duration
//	esc                    — cancel → plain exit (app handles)
//	ctrl+d                 — delete the node under the Plan Cursor (no-op on the new-node row)
//	ctrl+k                 — clear ALL planted nodes for the active craft (`c`/`C` no longer do this — see app.go)
//	digits/backspace       — forwarded to focused text input
func (m *Maneuver) HandleKey(msg tea.KeyMsg, nodes []sim.ManeuverNode) (tea.Cmd, bool) {
	const focusFields = 5 // mode / fireAt / dv / throttle / iterate
	n := len(nodes)
	switch msg.String() {
	case "up":
		if cur := m.cursorRow(n); cur > 0 {
			m.cursorIdx = cur - 1
		} else {
			m.cursorIdx = 0
		}
		return nil, false
	case "down":
		if cur := m.cursorRow(n); cur < n {
			m.cursorIdx = cur + 1
		}
		return nil, false
	case "ctrl+d":
		// Plan Cursor delete (ADR 0047 / #428): deletes the node under
		// the cursor, not whatever happens to be loaded for editing —
		// the pre-#428 editingIdx-only gate is why this key used to sit
		// silent on the footer whenever nothing had been mouse-loaded.
		// No-op on the blank new-node row — nothing planted there yet.
		cur := m.cursorRow(n)
		if cur >= n {
			return nil, false
		}
		return func() tea.Msg { return NodeDeleteMsg{EditingIdx: cur} }, true
	case "ctrl+k":
		// Clear ALL nodes for the active craft, then close the form.
		// ADR 0047 / #428: this is now the ONLY clear-all binding — `c`
		// (mapped to ReArmDock on the map) and `C` (PlanCircularize)
		// are handled one level up in app.go before HandleKey is even
		// called, so this switch never sees either letter.
		return func() tea.Msg { return NodeClearAllMsg{} }, true
	case "tab":
		m.focus = (m.focus + 1) % focusFields
		m.applyFocus()
		return nil, false
	case "shift+tab":
		m.focus = (m.focus + focusFields - 1) % focusFields
		m.applyFocus()
		return nil, false
	case "left":
		switch m.focus {
		case 0:
			m.advanceMode(-1)
			return nil, false
		case 1:
			m.advanceFireAt(-1)
			return nil, false
		case 4:
			m.iterateForTarget = !m.iterateForTarget
			return nil, false
		}
	case "right":
		switch m.focus {
		case 0:
			m.advanceMode(1)
			return nil, false
		case 1:
			m.advanceFireAt(1)
			return nil, false
		case 4:
			m.iterateForTarget = !m.iterateForTarget
			return nil, false
		}
	case " ":
		// Space toggles the iterate field — no other field uses it
		// (the dv / throttle inputs filter to digits), so the
		// dispatch is unambiguous.
		if m.focus == 4 {
			m.iterateForTarget = !m.iterateForTarget
			return nil, false
		}
	case "enter":
		// Plan Cursor (ADR 0047 / #428): Enter on a planted node the
		// cursor hasn't loaded yet LOADS it for editing instead of
		// committing — mirrors the mouse click-to-edit path exactly
		// (LoadNode), so ↑/↓ + Enter is now a full keyboard equivalent
		// of clicking a node's map glyph. Once loaded (cur == editingIdx)
		// — or on the blank new-node row — Enter commits as before.
		if cur := m.cursorRow(n); cur < n && cur != m.editingIdx {
			m.LoadNode(cur, nodes[cur])
			return nil, false
		}
		// dv drives both the BurnExecutedMsg's Δv field AND its derived
		// Duration via the rocket equation. Zero-thrust craft return
		// Duration = 0 from BurnTimeForDV, falling back to the legacy
		// impulsive path — preserving the impulsive capability through
		// the API even though the form no longer exposes it directly.
		cmd := m.commitCmd()
		if cmd == nil {
			return nil, false // zero Δv — ignore, user needs to type a number
		}
		return cmd, true
	}
	var cmd tea.Cmd
	switch m.focus {
	case 2:
		m.dvInput, cmd = m.dvInput.Update(msg)
	case 3:
		m.throttleInput, cmd = m.throttleInput.Update(msg)
	}
	return cmd, false
}

// commitCmd builds a BurnExecutedMsg from the current form values.
// Caller (HandleKey on Enter) returns nil cmd to ignore commits with
// zero Δv. Split out so the burn-time derivation lives in one place
// and the form panel can preview the same number.
func (m *Maneuver) commitCmd() tea.Cmd {
	dv := m.parsedDV()
	if dv == 0 {
		return nil
	}
	mode := spacecraft.AllBurnModes[m.modeIdx]
	event := sim.AllTriggerEvents[m.fireAtIdx]
	msg := BurnExecutedMsg{
		Mode:             mode,
		DV:               dv,
		Event:            event,
		TriggerTime:      m.loadedTriggerTime,
		EditingIdx:       m.editingIdx,
		Throttle:         m.parsedThrottle(),
		IterateForTarget: m.iterateForTarget,
		// #294 second-round review finding 6: carry the advisory-key
		// identity straight through — see AdvisoryKey's doc on
		// BurnExecutedMsg for why an edited K/C node needs this to
		// survive the re-plant.
		AdvisoryKey: m.advisoryKey,
	}
	// v0.9.3+: capture the bound target craft. Bound by stable craft ID
	// since v0.14.x (ADR 0012); zero = no target.
	//
	// #294 second-round review finding 6: attached for EVERY mode, not
	// just the target-relative four (+ TriggerNextClosestApproach) —
	// PlanRendezvousNudge's K plant binds TargetCraftID/TargetGhostOwner
	// onto velocity-frame axis nodes too (BurnPrograde/Retrograde/
	// RadialOut/RadialIn — axisLabelToBurnMode's cycle), recording which
	// target the nudge was actually computed against even though those
	// modes never read rT/vT for direction. Gating this on mode meant
	// LoadNode would faithfully load the binding (LoadNode reads it
	// unconditionally) only for commitCmd to silently drop it again on
	// re-plant — editing one of those nudges quietly stripped state the
	// ORIGINAL plant always carried, for no functional reason.
	if m.hasTargetCraft {
		msg.TargetCraftID = m.targetCraftID
		msg.TargetGhostOwner = m.targetGhostOwner
	}
	return func() tea.Msg { return msg }
}

// advanceMode steps modeIdx by delta, skipping target-relative modes
// when no craft target is bound. Stops after one full cycle to avoid
// looping forever in the impossible "all modes invalid" case (would
// only happen if AllBurnModes were entirely target-relative). v0.9.3+.
func (m *Maneuver) advanceMode(delta int) {
	n := len(spacecraft.AllBurnModes)
	if n == 0 {
		return
	}
	for step := 0; step < n; step++ {
		m.modeIdx = (m.modeIdx + delta + n) % n
		mode := spacecraft.AllBurnModes[m.modeIdx]
		if !spacecraft.IsTargetRelativeMode(mode) || m.hasTargetCraft {
			return
		}
	}
}

// advanceFireAt steps fireAtIdx by delta, skipping
// TriggerNextClosestApproach when no craft target is bound. v0.9.3+.
func (m *Maneuver) advanceFireAt(delta int) {
	n := len(sim.AllTriggerEvents)
	if n == 0 {
		return
	}
	for step := 0; step < n; step++ {
		m.fireAtIdx = (m.fireAtIdx + delta + n) % n
		ev := sim.AllTriggerEvents[m.fireAtIdx]
		if ev != sim.TriggerNextClosestApproach || m.hasTargetCraft {
			return
		}
	}
}

// parsedThrottle returns the form's throttle setting as a fraction
// in [0, 1]. Empty / unparseable input falls back to 1.0 (full
// open) so a player who skips the field gets the prior universal
// behaviour. Out-of-range values clamp into the unit interval.
func (m *Maneuver) parsedThrottle() float64 {
	raw := m.throttleInput.Value()
	if raw == "" {
		return 1.0
	}
	var pct float64
	if _, err := fmt.Sscanf(raw, "%f", &pct); err != nil {
		return 1.0
	}
	t := pct / 100
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

func (m *Maneuver) parsedDV() float64 {
	var dv float64
	if _, err := fmt.Sscanf(m.dvInput.Value(), "%f", &dv); err != nil {
		return 0
	}
	if dv < 0 {
		dv = -dv
	}
	return dv
}

// Render composes the preview canvas + form panel. selectedBody is the
// App's body cursor (from the orbit/porkchop selection, not World state)
// — threaded through so the QUICK PLANS block can dim [P] with "no body
// selected" the same way app.go's own porkchop guard does (ADR 0047 /
// #428).
func (m *Maneuver) Render(w *sim.World, cols, rows, selectedBody int) string {
	if w.ActiveCraft() == nil {
		return "no vessel"
	}

	m.canvas.Clear()
	m.canvas.SetBasis(viewBasis(w))
	m.canvas.Center(orbital.Vec3{})

	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	currentEl := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	m.canvas.FitTo(math.Max(currentEl.Apoapsis(), c.State.R.Norm()) * 1.1)

	// v0.6.3 disk-render + v0.6.4 side-view occlusion: draw the
	// primary FIRST so the orbit + shadow + craft cluster can skip
	// any back-half sample whose screen position falls inside the
	// disk, leaving a clean gap where the body occludes them.
	// True-scale radius × scale; 3-pixel floor (so Luna-class moons
	// always read as a disk) and 64-pixel ceiling (extreme-zoom
	// guard).
	primaryColor := render.ColorFor(c.Primary)
	primaryPxR := int(math.Round(c.Primary.RadiusMeters() * m.canvas.Scale()))
	if primaryPxR < 3 {
		primaryPxR = 3
	} else if primaryPxR > 64 {
		primaryPxR = 64
	}
	m.canvas.FillColoredDisk(orbital.Vec3{}, primaryPxR, primaryColor)

	// Current orbit — Real class, solid (ADR 0041 §2). Empty colour →
	// uses Plot for back-compat with the existing white-on-default
	// rendering of this canvas.
	m.canvas.DrawEllipseClass(currentEl, orbital.Vec3{}, 360, widgets.ClassReal, orbital.Vec3{}, primaryPxR, "")

	// v0.9.3 polish: target craft's orbit + current position when it
	// shares the active craft's primary. The maneuver canvas centers
	// on the active craft's primary at origin {0,0,0}, so the target
	// state vector (already primary-relative when same-primary) plots
	// directly. Cross-primary targets are out of scope — the canvas
	// frame is the wrong one for them.
	if tc, _, ok := w.ResolveTargetCraft(); ok {
		if tc.Primary.ID == c.Primary.ID {
			tEl := orbital.ElementsFromState(tc.State.R, tc.State.V, mu)
			tOrbitVisible := tEl.A > 0 && !math.IsNaN(tEl.A) && !math.IsInf(tEl.A, 0)
			if tOrbitVisible {
				// Still Real class — the TARGET green is a colour swap,
				// not a different line style (ADR 0041 §2).
				m.canvas.DrawEllipseClass(tEl, orbital.Vec3{}, 360, widgets.ClassReal, orbital.Vec3{}, primaryPxR, render.ColorTarget)
			}
			if !m.canvas.IsBehindBody(tc.State.R, orbital.Vec3{}, primaryPxR) {
				m.canvas.PlotColored(tc.State.R, render.ColorTarget)
				if tc.Glyph != "" {
					if g := []rune(tc.Glyph); len(g) > 0 {
						m.canvas.SetCellOverlay(tc.State.R, g[0])
					}
				}
			}
		}
	}

	// Draw shadow trajectory after applying the current (mode, dv,
	// fire-at) triple. v0.6.1: when fire-at is event-relative, the
	// world's PreviewBurnState propagates the craft to the event
	// point before applying Δv — so a prograde burn at next apoapsis
	// raises the *opposite* point (perigee), not the apoapsis the
	// craft is nowhere near. Falls back to current-state preview if
	// the event is unreachable (hyperbolic / equatorial AN/DN).
	dv := m.parsedDV()
	dur := c.BurnTimeForDV(dv)
	mode := spacecraft.AllBurnModes[m.modeIdx]
	event := sim.AllTriggerEvents[m.fireAtIdx]
	shadowState, shadowPrimary, ok := w.PreviewBurnState(mode, dv, dur, event)
	if !ok {
		dir := spacecraft.DirectionUnit(mode, c.State.R, c.State.V)
		shadowState = physics.StateVector{
			R: c.State.R,
			V: c.State.V.Add(dir.Scale(dv)),
			M: c.State.M,
		}
		shadowPrimary = c.Primary
	}
	shadowMu := shadowPrimary.GravitationalParameter()
	shadowPeriod := orbitalPeriodOrFallback(shadowState, shadowMu)
	pts := planner.Predict(shadowState, shadowMu, shadowPeriod, 256)
	primaryGap := w.BodyPosition(shadowPrimary).Sub(w.BodyPosition(c.Primary))
	// Planned class, dashed (ADR 0041 §2): this is a predicted trajectory
	// — the orbit the burn WOULD produce, not a real one. Previously every
	// sample plotted its own untagged dot regardless of its neighbours,
	// which at 256 fixed samples read as solid rather than the "plans are
	// dashed" vocabulary calls for. Runs are broken at each occluded
	// (behind-primary) point exactly as before, so a run never bridges a
	// gap the body actually occludes; each surviving run gets a fresh
	// phase-continuous dash, same as node legs.
	var visibleRun []orbital.Vec3
	flushShadowRun := func() {
		if len(visibleRun) > 0 {
			m.canvas.PlotPolylineClass(visibleRun, "", widgets.ClassPlanned)
			visibleRun = visibleRun[:0]
		}
	}
	for _, p := range pts {
		pp := p.Add(primaryGap)
		if m.canvas.IsBehindBody(pp, orbital.Vec3{}, primaryPxR) {
			flushShadowRun()
			continue
		}
		visibleRun = append(visibleRun, pp)
	}
	flushShadowRun()

	// Craft cluster — skip if behind primary in the active view.
	if !m.canvas.IsBehindBody(c.State.R, orbital.Vec3{}, primaryPxR) {
		step := 1.0 / m.canvas.Scale()
		for i := -4; i <= 4; i++ {
			m.canvas.Plot(c.State.R.Add(orbital.Vec3{X: float64(i) * step}))
			m.canvas.Plot(c.State.R.Add(orbital.Vec3{Y: float64(i) * step}))
		}
	}

	// Mirror the orbit screen's bottom-right view-mode label so the
	// player can tell which projection the preview is in without
	// flipping back to the orbit screen. v0.7.4+.
	viewLabel := "view: " + w.ViewMode.String()
	labelCol := m.canvas.Cols() - len([]rune(viewLabel)) - 1
	if labelCol < 0 {
		labelCol = 0
	}
	m.canvas.SetCellLabelColored(labelCol, m.canvas.Rows()-1, viewLabel, m.theme.Primary.GetForeground())

	canvasPanel := m.theme.HUDBox.Render(m.canvas.String())

	panelWidth := formPanelWidth(cols)
	form := m.renderForm(w, dv, shadowState, shadowPrimary, shadowMu, selectedBody, panelWidth)
	body := lipgloss.JoinHorizontal(lipgloss.Top, canvasPanel, "  ", form)

	// #428 mechanical fix: the footer is one full-width row (not part
	// of the two-column canvas/form split renderForm ellipsizes), and
	// it's long enough on its own to overflow a narrow terminal — the
	// exact hard-clip symptom this issue exists to remove, just on a
	// different line. Ellipsize it against `cols` directly.
	footer := m.theme.Footer.Render(ansi.Truncate(
		"[tab] field  [←/→] cycle  [↑/↓] cursor  [enter] commit/load  [esc] cancel  [ctrl+d] del node  [ctrl+k] clear all",
		cols, "…",
	))
	// Plan Cursor (ADR 0047 / #428): the title bar names the node under
	// the cursor, same as the form's own "BURN PLAN" header — see
	// renderForm's identical switch for why cur/editingIdx can diverge
	// (browsing a different node than the one loaded in the form).
	nNodes := len(c.Nodes)
	cur := m.cursorRow(nNodes)
	title := "maneuver planner"
	switch {
	case cur < nNodes && cur == m.editingIdx:
		title = fmt.Sprintf("maneuver planner — editing node %d of %d", cur+1, nNodes)
	case cur < nNodes:
		title = fmt.Sprintf("maneuver planner — node %d of %d", cur+1, nNodes)
	}
	title = ansi.Truncate(title, cols, "…")
	return m.theme.Title.Render(title) + "\n" + body + "\n" + footer
}

func (m *Maneuver) renderForm(w *sim.World, dv float64, shadow physics.StateVector, shadowPrimary bodies.CelestialBody, mu float64, selectedBody, panelWidth int) string {
	c := w.ActiveCraft()
	mode := spacecraft.AllBurnModes[m.modeIdx]
	budget := c.RemainingDeltaV()
	// v0.6.5: duration is derived from Δv at render time (and again at
	// commit), so the form preview matches what the App will plant.
	dur := c.BurnTimeForDV(dv)

	warn := ""
	if dv > budget {
		warn = m.theme.Alert.Render(fmt.Sprintf(" [EXCEEDS BUDGET by %.0f m/s]", dv-budget))
	}

	// Mode line — highlight if focused, otherwise dim.
	modeLabel := mode.String()
	if m.focus == 0 {
		modeLabel = m.theme.Warning.Render(modeLabel) + "  (←/→ to cycle)"
	} else {
		modeLabel = m.theme.Dim.Render(modeLabel)
	}

	// Fire-at line — highlight if focused, otherwise dim. v0.6.4
	// click-to-edit appends the loaded TriggerTime as a relative
	// countdown so "T+" alone doesn't read as "fire now" — the user
	// has the schedule context they need to confirm the edit.
	// Absolute mode replaces the bare "T+" with the countdown
	// (which already carries the T+ prefix); event-relative modes
	// keep the event name and parenthesize the countdown.
	fireAt := sim.AllTriggerEvents[m.fireAtIdx]
	fireAtLabel := fireAt.String()
	if !m.loadedTriggerTime.IsZero() {
		countdown := formatCountdown(m.loadedTriggerTime.Sub(w.Clock.SimTime))
		if fireAt == sim.TriggerAbsolute {
			fireAtLabel = countdown
		} else {
			fireAtLabel = fmt.Sprintf("%s (%s)", fireAtLabel, countdown)
		}
	}
	if m.focus == 1 {
		fireAtLabel = m.theme.Warning.Render(fireAtLabel) + "  (←/→ to cycle)"
	} else {
		fireAtLabel = m.theme.Dim.Render(fireAtLabel)
	}

	// v0.6.5: burn description shows the rocket-equation-derived
	// duration. Zero-thrust craft fall back to "impulsive" since
	// BurnTimeForDV returns 0 in that case; otherwise we surface
	// the engine-on time the App will plant.
	burnDescr := "impulsive"
	if dur > 0 {
		burnDescr = fmt.Sprintf("finite burn — %.1fs at %.0f kN, Isp %.0f s",
			dur.Seconds(), c.Thrust/1000, c.Isp)
	}

	// Plan Cursor (ADR 0047 / #428): the header names the node under the
	// cursor, e.g. "BURN PLAN — node 2 of 3" — and, when that node is
	// ALSO the one loaded in the form (cur == editingIdx), calls out
	// that Enter replaces it in place rather than duplicating it. cur
	// and editingIdx can diverge: ↑/↓ browsing a different node than
	// whatever's still loaded in the form fields shows the plain
	// "node N of M" form (no "editing"), same as never having loaded
	// anything. Warning style is reserved for the actively-loaded case
	// — visual distinction from a fresh-plan / just-browsing header.
	nNodes := len(c.Nodes)
	cur := m.cursorRow(nNodes)
	headerStyle := m.theme.Primary
	header := "BURN PLAN"
	switch {
	case cur < nNodes && cur == m.editingIdx:
		headerStyle = m.theme.Warning
		header = fmt.Sprintf("BURN PLAN — editing node %d of %d", cur+1, nNodes)
	case cur < nNodes:
		header = fmt.Sprintf("BURN PLAN — node %d of %d", cur+1, nNodes)
	}
	// Iterate-for-target line. Highlights when focused; toggle via
	// space or ←/→. v0.8.6 (b).
	iterateLabel := "off"
	if m.iterateForTarget {
		iterateLabel = "on"
	}
	if m.focus == 4 {
		iterateLabel = m.theme.Warning.Render(iterateLabel) + "  (space toggles)"
	} else {
		iterateLabel = m.theme.Dim.Render(iterateLabel)
	}

	// Budget line (#428 mechanical fix): show what the plan LEAVES, not
	// just what the vessel has — "6129 m/s (2217 after plan)" — so the
	// player doesn't have to add the PLANNED NODES column by hand to
	// see if the plan on the board is affordable.
	var planTotal float64
	for _, n := range c.Nodes {
		planTotal += n.DV
	}
	budgetLine := fmt.Sprintf("  Δv budget: %.0f m/s", budget)
	if len(c.Nodes) > 0 {
		budgetLine += fmt.Sprintf(" (%.0f after plan)", budget-planTotal)
	}

	lines := []string{
		headerStyle.Render(header),
		"  mode:     " + modeLabel,
		"  fire at:  " + fireAtLabel,
		"  Δv:       " + m.dvInput.View() + " m/s" + warn,
		"  throttle: " + m.throttleInput.View() + " %",
		"  iterate:  " + iterateLabel,
		"  → " + burnDescr,
		"",
		budgetLine,
		fmt.Sprintf("  thrust: %.0f N  Isp: %.0f s", c.Thrust, c.Isp),
	}

	// PLANNED NODES (v0.10.1+; Plan Cursor since ADR 0047 / #428): list
	// every node currently planted on the active craft so the planner
	// shows the full schedule, not just the one being created/edited.
	// Resolved nodes show a T± countdown; event-relative nodes that
	// haven't frozen a trigger yet show the event name instead. Rows
	// render at normal (unstyled) foreground — Theme.Dim used to mark
	// every row as if the whole list were disabled, which is why the
	// actual subject of the screen read as inert chrome (#428 finding).
	// The list always ends in a blank new-node row so the Plan Cursor
	// has somewhere to land for "start a fresh plant."
	lines = append(lines, "")
	nodesHeader := "PLANNED NODES"
	if nNodes > 0 {
		nodesHeader = fmt.Sprintf("PLANNED NODES (%d)", nNodes)
	}
	lines = append(lines, m.theme.Primary.Render(nodesHeader))
	const maxList = 8
	shown := nNodes
	if shown > maxList {
		shown = maxList
	}
	for i := 0; i < shown; i++ {
		n := c.Nodes[i]
		when := n.Event.String()
		if !n.TriggerTime.IsZero() {
			when = formatCountdown(n.TriggerTime.Sub(w.Clock.SimTime))
		}
		row := fmt.Sprintf("%d. %-10s %6.0f m/s  %s", i+1, n.Mode.String(), n.DV, when)
		// Over-budget Node (ADR 0047 §2 / #428): a planted node whose Δv
		// exceeds the vessel's current remaining budget plants anyway —
		// warn and allow, never refuse — but every list carrying it
		// shows the shortfall so the player isn't surprised later. Same
		// wording as the on-map NODES chip (orbit_chips.go).
		if over := n.DV - budget; over > 0 {
			row += "  " + m.theme.Alert.Render(fmt.Sprintf("⚠ exceeds budget by %.0f m/s", over))
		}
		switch {
		case i == m.editingIdx:
			row = m.theme.Warning.Render("▸ " + row + "  ← editing")
		case i == cur:
			row = m.theme.Primary.Render("▸ " + row)
		default:
			row = "  " + row
		}
		lines = append(lines, row)
	}
	if nNodes > maxList {
		lines = append(lines, m.theme.Dim.Render(fmt.Sprintf("  … +%d more", nNodes-maxList)))
	}
	newRow := "+ new node"
	if cur == nNodes {
		newRow = m.theme.Primary.Render("▸ " + newRow)
	} else {
		newRow = m.theme.Dim.Render("  " + newRow)
	}
	lines = append(lines, newRow)

	// QUICK PLANS (ADR 0047 §4 / #428): the one-key planners, legal
	// right now or dimmed with the reason when a precondition isn't
	// met. Pressing the key inside the planner does exactly what it
	// does on the map (app.go intercepts H/I/C/K/P/R before this
	// screen's own HandleKey sees them) — this block only renders the
	// same guards those handlers already apply.
	lines = append(lines, "", m.theme.Primary.Render("QUICK PLANS"))
	for _, q := range quickPlanRows(w, selectedBody) {
		row := fmt.Sprintf("  [%s] %s", q.key, q.label)
		if q.ok {
			lines = append(lines, row)
		} else {
			lines = append(lines, m.theme.Dim.Render(row+" — "+q.reason))
		}
	}

	// PROJECTED ORBIT (Plan Cursor, ADR 0047 §1 / #428): always the
	// orbit AFTER the node under the cursor, never a leftover draft
	// masquerading as the plan — the #428 finding was a form's
	// abandoned 100 m/s draft projecting next to an unrelated real
	// three-burn plan and reading as its result.
	//
	// Two sources, chosen by where the cursor sits:
	//   - cursor on the blank new-node row, OR on a planted node that
	//     IS loaded into the form (cur == editingIdx): project the
	//     form's Draft (the shadow/mu the caller already computed from
	//     live field values) — explicitly labelled "(this draft)" only
	//     for the genuinely-new-node case, since a Draft must never be
	//     shown as if it were a planted node.
	//   - cursor on a planted node NOT loaded into the form: project
	//     that node's own REAL committed state via PredictedLegs, which
	//     chains every prior node's burn instead of pretending the
	//     current form values apply to it.
	browsingUnloaded := cur < nNodes && cur != m.editingIdx
	poLabel := "PROJECTED ORBIT"
	poState, poPrimary := shadow, shadowPrimary
	poMu := mu
	poResolved := dv > 0
	if browsingUnloaded {
		poResolved = false
		for _, leg := range w.PredictedLegs() {
			if leg.NodeIndex == cur {
				poState, poPrimary = leg.State, leg.Primary
				poMu = poPrimary.GravitationalParameter()
				poResolved = true
				break
			}
		}
	} else if m.editingIdx < 0 {
		poLabel = "PROJECTED ORBIT (this draft)"
	}
	switch {
	case browsingUnloaded && !poResolved:
		lines = append(lines, "", m.theme.Primary.Render(poLabel),
			m.theme.Dim.Render("  pending — event not yet resolved"))
	case poResolved:
		// v0.6.1: apo / peri / AN / DN of the projected orbit. Updates
		// live as the player tweaks the form (draft case), or reflects
		// the real planted node (browsing case).
		frame := orbital.ReferenceFrameForPrimary(poPrimary)
		ro := orbital.OrbitReadoutInFrame(poState.R, poState.V, poMu, frame)
		primaryR := poPrimary.RadiusMeters()
		lines = append(lines, "", m.theme.Primary.Render(poLabel))
		if poPrimary.ID != c.Primary.ID {
			lines = append(lines, fmt.Sprintf("  primary:       %s", poPrimary.EnglishName))
		}
		if ro.Hyperbolic {
			lines = append(lines,
				"  "+m.theme.Warning.Render("hyperbolic — escape trajectory"),
				fmt.Sprintf("  new Pe:        %.1f km alt", (ro.PeriMeters-primaryR)/1000),
				fmt.Sprintf("  e:             %.3f", ro.Eccentricity),
			)
		} else {
			lines = append(lines,
				fmt.Sprintf("  new Ap:        %.1f km alt", (ro.ApoMeters-primaryR)/1000),
				fmt.Sprintf("  new Pe:        %.1f km alt", (ro.PeriMeters-primaryR)/1000),
				fmt.Sprintf("  new inclin.:   %.2f°", ro.Inclination*180/math.Pi),
			)
			const equatorialTol = 1e-3
			if ro.Inclination < equatorialTol || math.Abs(ro.Inclination-math.Pi) < equatorialTol {
				lines = append(lines, m.theme.Dim.Render("  AN/DN:         equatorial (undefined)"))
			} else {
				lines = append(lines,
					fmt.Sprintf("  new AN angle:  %.1f°", normalizeManeuverDeg(ro.AscNode*180/math.Pi)),
					fmt.Sprintf("  new DN angle:  %.1f°", normalizeManeuverDeg(ro.DescNode*180/math.Pi)),
				)
			}
		}
	}

	// #428 mechanical fix: ellipsize rather than let the terminal
	// hard-clip a line mid-word at narrow widths. ANSI-aware so styled
	// rows (over-budget markers, the cursor highlight, …) keep their
	// colour codes intact instead of getting cut mid-escape.
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, panelWidth, "…")
	}
	return strings.Join(lines, "\n")
}

// quickPlanRow describes one row of the planner's QUICK PLANS block
// (ADR 0047 §4 / #428).
type quickPlanRow struct {
	key    string
	label  string
	ok     bool
	reason string
}

// quickPlanRows computes the legality (and, when illegal, the reason)
// of each one-key planner using the SAME guards their app.go handlers
// apply — CraftVisibleHere, World.Target, ResolveTargetCraft,
// HasRefinablePlan — so a row's dimmed reason never drifts from what
// actually happens when the key is pressed. selectedBody is the App's
// body cursor (not World state), needed for [P]'s "no body selected"
// guard.
func quickPlanRows(w *sim.World, selectedBody int) []quickPlanRow {
	visible := w.CraftVisibleHere()
	rows := make([]quickPlanRow, 0, 6)

	h := quickPlanRow{key: "H", label: "transfer to target body"}
	switch {
	case !visible:
		h.reason = "vessel not in this system"
	case w.Target.Kind == sim.TargetNone:
		h.reason = "no target — press t to aim at a body"
	case w.Target.Kind != sim.TargetBody:
		h.reason = "targets bodies only — try [m] for a vessel"
	default:
		h.ok = true
	}
	rows = append(rows, h)

	pm := quickPlanRow{key: "I", label: "plane match"}
	if visible {
		pm.ok = true
	} else {
		pm.reason = "vessel not in this system"
	}
	rows = append(rows, pm)

	circ := quickPlanRow{key: "C", label: "circularize at apoapsis"}
	if visible {
		circ.ok = true
	} else {
		circ.reason = "vessel not in this system"
	}
	rows = append(rows, circ)

	k := quickPlanRow{key: "K", label: "close on target vessel"}
	_, _, hasCraftTarget := w.ResolveTargetCraft()
	switch {
	case !visible:
		k.reason = "vessel not in this system"
	case !hasCraftTarget:
		k.reason = "needs a vessel target"
	default:
		k.ok = true
	}
	rows = append(rows, k)

	p := quickPlanRow{key: "P", label: "porkchop plot"}
	switch {
	case !visible:
		p.reason = "vessel not in this system"
	case selectedBody <= 0:
		p.reason = "no body selected"
	default:
		p.ok = true
	}
	rows = append(rows, p)

	r := quickPlanRow{key: "R", label: "refine plan"}
	switch {
	case !visible:
		r.reason = "vessel not in this system"
	case !w.HasRefinablePlan():
		r.reason = "no planted transfer"
	default:
		r.ok = true
	}
	rows = append(rows, r)

	return rows
}

// formPanelWidth mirrors Resize's canvasCols split so the form panel's
// own ellipsize width (#428 mechanical fix) matches what's actually
// left over once the canvas box and the gap between them are laid out.
// The canvas box itself is wider on screen than canvasCols alone: the
// HUDBox style (theme.go) wraps it in a rounded border (1 col each
// side) AND Padding(0, 1) (1 more col each side) — 4 columns of
// overhead easy to undercount, which is exactly what the first version
// of this function did (subtracting only a bare 2), silently
// overflowing the terminal width it was supposed to fit inside and
// reproducing the review's own hard-clip bug one level up (the pty
// wrapping the combined row instead of the panel getting an ellipsis
// at all). Render() then joins canvasPanel, a literal "  " 2-col gap,
// and the form with lipgloss.JoinHorizontal — hence the extra -2 below.
func formPanelWidth(cols int) int {
	canvasCols := cols * 6 / 10
	if canvasCols < 20 {
		canvasCols = 20
	}
	if canvasCols > 80 {
		canvasCols = 80
	}
	const canvasBoxOverhead = 4 // HUDBox: 2 border cols + 2 padding cols
	const joinGap = 2           // Render's literal "  " between canvas and form
	w := cols - canvasCols - canvasBoxOverhead - joinGap
	if w < 10 {
		w = 10 // floor so a row always has room to show something plus "…"
	}
	return w
}

// formatCountdown renders a relative duration as "T+1d3h", "T+14m32s",
// or "T-5s" (past, in case the node is overdue). v0.6.4 click-to-
// edit uses this to qualify the fire-at label so the player sees
// when the loaded burn is scheduled. Two-component precision keeps
// the line short — "1d3h" not "1d3h45m12s".
func formatCountdown(d time.Duration) string {
	prefix := "T+"
	if d < 0 {
		d = -d
		prefix = "T-"
	}
	// compactDuration (orbit.go) owns the two-unit decomposition; this
	// just signs it. v0.16 dedup — was a verbatim copy of that switch.
	return prefix + compactDuration(d)
}

// normalizeManeuverDeg wraps an angle in degrees into [0, 360). Local
// to this package because the orbit screen's own helper isn't exported
// — this avoids cross-screen coupling for a 4-line helper.
func normalizeManeuverDeg(d float64) float64 {
	d = math.Mod(d, 360)
	if d < 0 {
		d += 360
	}
	return d
}

func orbitalPeriodOrFallback(s physics.StateVector, mu float64) float64 {
	a := physics.SemimajorAxis(s, mu)
	if a <= 0 || math.IsNaN(a) || math.IsInf(a, 0) {
		return 3600 // 1 hour for hyperbolic — enough to see the trajectory shape
	}
	return 2 * math.Pi * math.Sqrt(a*a*a/mu)
}
