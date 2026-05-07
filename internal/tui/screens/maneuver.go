package screens

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

	// hasTargetCraft + targetCraftIdx carry the World.Target binding
	// at form-open time, so the four target-relative burn modes and
	// the TriggerNextClosestApproach event can resolve their
	// direction / trigger against the captured target. Bound at open
	// (not at every keypress) so a target switch while the form is
	// open doesn't silently retarget a planted burn — the player
	// closes + reopens the form to retarget. v0.9.3+.
	hasTargetCraft bool
	targetCraftIdx int
}

// SetTargetCraft binds (or unbinds) the target-craft slate index the
// form's planted burn will be aimed at. Called by the app when
// opening the form so the four target-relative burn modes and the
// TriggerNextClosestApproach event can resolve at plant + fire time.
// Pass ok=false to clear (no craft target set / target is a body).
// v0.9.3+.
func (m *Maneuver) SetTargetCraft(ok bool, idx int) {
	m.hasTargetCraft = ok
	if ok {
		m.targetCraftIdx = idx
	} else {
		m.targetCraftIdx = 0
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
	// TargetCraftIdx (v0.9.3+) is the one-based encoding of the
	// target slate idx the form was bound to at plant. Zero = no
	// target. Mirrors ManeuverNode.TargetCraftIdx; the app passes it
	// straight through. Only populated for target-relative modes /
	// TriggerNextClosestApproach event.
	TargetCraftIdx int
}

// NodeDeleteMsg is emitted when the player presses ctrl+d in the
// maneuver form while editing a planted node. The app handles it
// by calling World.DeleteNode(EditingIdx) and closing the screen.
// Replaces the v0.8.5-and-earlier `N` global "clear all nodes"
// keybinding for the per-node case. v0.8.6+.
type NodeDeleteMsg struct {
	EditingIdx int
}

// NodeClearAllMsg is emitted when the player presses ctrl+k in the
// maneuver form. The app handles it by calling World.ClearNodes()
// and closing the screen. Replaces the v0.8.5-and-earlier `N`
// global keybinding for the wipe-all case. v0.8.6+.
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
	// edit cycle so re-planting doesn't drop it. Caller (app) is
	// expected to follow up with SetTargetCraft to reflect the
	// CURRENT World.Target if the node's binding is stale, but the
	// default-load behaviour preserves the original target.
	if tIdx, ok := n.TargetCraftIdxValue(); ok {
		m.hasTargetCraft = true
		m.targetCraftIdx = tIdx
	}
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
// means the app should exit the maneuver screen (commit or cancel).
//
// Key bindings:
//   tab / shift+tab        — cycle focus across mode / fire-at / Δv fields
//   ←/→ (mode focused)     — cycle direction modes
//   ←/→ (fire-at focused)  — cycle trigger events (Absolute / NextPeri / NextApo / NextAN / NextDN)
//   enter                  — commit burn → emits BurnExecutedMsg with rocket-equation duration
//   esc                    — cancel → plain exit (app handles)
//   ctrl+d                 — delete the planted node being edited (no-op when creating new)
//   ctrl+k                 — clear ALL planted nodes for the active craft
//   digits/backspace       — forwarded to focused text input
func (m *Maneuver) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	const focusFields = 5 // mode / fireAt / dv / throttle / iterate
	switch msg.String() {
	case "ctrl+d":
		// v0.8.6+: per-node delete. Only meaningful while editing
		// an existing node — creating-new sessions have no node to
		// delete yet. App receives NodeDeleteMsg and routes to
		// World.DeleteNode(idx).
		if m.editingIdx < 0 {
			return nil, false
		}
		idx := m.editingIdx
		return func() tea.Msg { return NodeDeleteMsg{EditingIdx: idx} }, true
	case "ctrl+k":
		// v0.8.6+: clear all nodes for the active craft. Replaces
		// the v0.8.5-and-earlier `N` global keybinding. Closes the
		// form on dispatch.
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
	}
	// v0.9.3+: capture the bound target craft idx for target-relative
	// modes and the TriggerNextClosestApproach event. One-based to
	// match the ManeuverNode encoding (zero = no target, idx+1
	// otherwise — JSON omitempty drops it for non-target nodes).
	if m.hasTargetCraft && (spacecraft.IsTargetRelativeMode(mode) || event == sim.TriggerNextClosestApproach) {
		msg.TargetCraftIdx = m.targetCraftIdx + 1
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


// Render composes the preview canvas + form panel.
func (m *Maneuver) Render(w *sim.World, cols, rows int) string {
	if w.ActiveCraft() == nil {
		return "no spacecraft"
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

	// Current orbit. Empty colour → uses Plot for back-compat with
	// the existing white-on-default rendering of this canvas.
	m.canvas.DrawEllipseOffsetOccluded(currentEl, orbital.Vec3{}, 360, 4, orbital.Vec3{}, primaryPxR, "")

	// v0.9.3 polish: target craft's orbit + current position when it
	// shares the active craft's primary. The maneuver canvas centers
	// on the active craft's primary at origin {0,0,0}, so the target
	// state vector (already primary-relative when same-primary) plots
	// directly. Cross-primary targets are out of scope — the canvas
	// frame is the wrong one for them.
	if w.Target.Kind == sim.TargetCraft && w.Target.CraftIdx >= 0 && w.Target.CraftIdx < len(w.Crafts) {
		if tc := w.Crafts[w.Target.CraftIdx]; tc != nil && tc.Primary.ID == c.Primary.ID {
			tEl := orbital.ElementsFromState(tc.State.R, tc.State.V, mu)
			tOrbitVisible := tEl.A > 0 && !math.IsNaN(tEl.A) && !math.IsInf(tEl.A, 0)
			if tOrbitVisible {
				m.canvas.DrawEllipseOffsetOccluded(tEl, orbital.Vec3{}, 360, 3, orbital.Vec3{}, primaryPxR, render.ColorTarget)
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
	for _, p := range pts {
		pp := p.Add(primaryGap)
		if m.canvas.IsBehindBody(pp, orbital.Vec3{}, primaryPxR) {
			continue
		}
		m.canvas.Plot(pp)
	}

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
	m.canvas.SetCellLabel(labelCol, m.canvas.Rows()-1, viewLabel)

	canvasPanel := m.theme.HUDBox.Render(m.canvas.String())

	form := m.renderForm(w, dv, shadowState, shadowPrimary, shadowMu)
	body := lipgloss.JoinHorizontal(lipgloss.Top, canvasPanel, "  ", form)

	footer := m.theme.Footer.Render(
		"[tab] cycle field  [←/→] cycle mode  [enter] commit  [esc] cancel  [digits] edit",
	)
	title := "maneuver planner"
	if m.editingIdx >= 0 {
		// v0.6.4 click-to-edit: surface the editing target so the
		// player knows Enter will replace this node, not duplicate.
		// Node display index is 1-based to match user expectations
		// (auto-plant labels nodes "departure" / "arrival" — for
		// hand-edits we just show the slice position).
		title = fmt.Sprintf("maneuver planner — editing node %d", m.editingIdx+1)
	}
	return m.theme.Title.Render(title) + "\n" + body + "\n" + footer
}

func (m *Maneuver) renderForm(w *sim.World, dv float64, shadow physics.StateVector, shadowPrimary bodies.CelestialBody, mu float64) string {
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

	// v0.6.4 click-to-edit: surface the editing target inline in
	// the form so the player sees "Enter replaces this node" at the
	// field they're about to commit. Title-row variants ride above
	// this and may wrap or get cropped by some renderers; the
	// form-panel header is the unambiguous spot. Warning style
	// (orange/yellow) for visual distinction from a fresh-plan
	// Primary-style header.
	headerStyle := m.theme.Primary
	header := "BURN PLAN"
	if m.editingIdx >= 0 {
		headerStyle = m.theme.Warning
		header = fmt.Sprintf("BURN PLAN — editing node %d", m.editingIdx+1)
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

	lines := []string{
		headerStyle.Render(header),
		"  mode:     " + modeLabel,
		"  fire at:  " + fireAtLabel,
		"  Δv:       " + m.dvInput.View() + " m/s" + warn,
		"  throttle: " + m.throttleInput.View() + " %",
		"  iterate:  " + iterateLabel,
		"  → " + burnDescr,
		"",
		"  Δv budget remaining: " + fmt.Sprintf("%.0f m/s", budget),
		fmt.Sprintf("  thrust: %.0f N  Isp: %.0f s", c.Thrust, c.Isp),
	}

	// v0.6.1: PROJECTED ORBIT readout — apo / peri / AN / DN of the
	// orbit produced by the current (mode, dv) pair. Updates live as
	// the player tweaks the form, so they can see the headline orbit
	// shape change without leaving the planner. Only shown when dv > 0
	// — at zero Δv the projected orbit equals the live orbit, which
	// the VESSEL block on the orbit screen already displays.
	if dv > 0 {
		frame := orbital.ReferenceFrameForPrimary(shadowPrimary)
		ro := orbital.OrbitReadoutInFrame(shadow.R, shadow.V, mu, frame)
		primaryR := shadowPrimary.RadiusMeters()
		lines = append(lines, "", m.theme.Primary.Render("PROJECTED ORBIT"))
		if shadowPrimary.ID != c.Primary.ID {
			lines = append(lines, fmt.Sprintf("  primary:       %s", shadowPrimary.EnglishName))
		}
		if ro.Hyperbolic {
			lines = append(lines,
				"  "+m.theme.Warning.Render("hyperbolic — escape trajectory"),
				fmt.Sprintf("  new periapsis: %.1f km alt", (ro.PeriMeters-primaryR)/1000),
				fmt.Sprintf("  e:             %.3f", ro.Eccentricity),
			)
		} else {
			lines = append(lines,
				fmt.Sprintf("  new apoapsis:  %.1f km alt", (ro.ApoMeters-primaryR)/1000),
				fmt.Sprintf("  new periapsis: %.1f km alt", (ro.PeriMeters-primaryR)/1000),
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
	return strings.Join(lines, "\n")
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
	totalSecs := int64(d.Seconds())
	if totalSecs == 0 {
		return prefix + "0s"
	}
	days := totalSecs / 86400
	hours := (totalSecs % 86400) / 3600
	mins := (totalSecs % 3600) / 60
	secs := totalSecs % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%s%dd%dh", prefix, days, hours)
	case hours > 0:
		return fmt.Sprintf("%s%dh%dm", prefix, hours, mins)
	case mins > 0:
		return fmt.Sprintf("%s%dm%ds", prefix, mins, secs)
	default:
		return fmt.Sprintf("%s%ds", prefix, secs)
	}
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
