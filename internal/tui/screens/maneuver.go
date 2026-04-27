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
// mode / fire-at / Δv / duration. Duration zero = impulsive (legacy
// v0.1 path); duration > 0 = finite burn. Fire-at = TriggerAbsolute
// fires at T+5min (legacy quick-plant default); event-relative modes
// resolve to the next periapsis/apoapsis/AN/DN at plant time.
type Maneuver struct {
	theme    Theme
	canvas   *widgets.Canvas
	dvInput  textinput.Model
	durInput textinput.Model
	modeIdx   int
	fireAtIdx int
	focus     int // 0=mode, 1=fireAt, 2=dv, 3=duration

	// editingIdx and loadedTriggerTime carry the v0.6.4 click-to-edit
	// state. Default editingIdx = -1 (creating a new node). LoadNode
	// sets them so the next BurnExecutedMsg can replace the original
	// node in place AND preserve its scheduled trigger time —
	// otherwise re-planting an Absolute-event node would lose its
	// future TriggerTime and fall back to the legacy "fire now"
	// quick-plant path.
	editingIdx        int
	loadedTriggerTime time.Time
}

// BurnExecutedMsg is emitted when the user hits Enter. App consumes it.
// Duration zero = impulsive (legacy path); >0 = finite burn. Event
// (v0.6.0+) selects the trigger model — TriggerAbsolute uses the
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
type BurnExecutedMsg struct {
	Mode        spacecraft.BurnMode
	DV          float64
	Duration    time.Duration
	Event       sim.TriggerEvent
	TriggerTime time.Time
	EditingIdx  int // -1 = creating a new node; ≥ 0 = replacing world.Nodes[idx]
}

func NewManeuver(th Theme) *Maneuver {
	dv := textinput.New()
	dv.Placeholder = "0"
	dv.CharLimit = 8
	dv.Width = 10
	dv.SetValue("100")

	dur := textinput.New()
	dur.Placeholder = "0"
	dur.CharLimit = 6
	dur.Width = 10
	// v0.6.1: default to a 10 s finite burn rather than impulsive.
	// Impulsive Δv is unphysical for a chemical engine and the
	// PROJECTED ORBIT readout reflects a more realistic plan when
	// the player picks a duration up front. They can still set 0 to
	// fall back to the legacy impulsive path.
	dur.SetValue("10")

	m := &Maneuver{
		theme:      th,
		canvas:     widgets.NewCanvas(60, 20),
		dvInput:    dv,
		durInput:   dur,
		editingIdx: -1,
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
	m.durInput.SetValue(fmt.Sprintf("%.1f", n.Duration.Seconds()))
	m.focus = 0
	m.editingIdx = idx
	m.loadedTriggerTime = n.TriggerTime
	m.applyFocus()
}

// applyFocus pushes focus state down to the bubbletea text inputs.
// Focus 0 = mode (cycle), 1 = fire-at (cycle), 2 = Δv, 3 = duration.
func (m *Maneuver) applyFocus() {
	m.dvInput.Blur()
	m.durInput.Blur()
	switch m.focus {
	case 2:
		m.dvInput.Focus()
	case 3:
		m.durInput.Focus()
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
//   tab / shift+tab        — cycle focus across mode / fire-at / Δv / duration fields
//   ←/→ (mode focused)     — cycle direction modes
//   ←/→ (fire-at focused)  — cycle trigger events (Absolute / NextPeri / NextApo / NextAN / NextDN)
//   enter                  — commit burn → emits BurnExecutedMsg
//   esc                    — cancel → plain exit (app handles)
//   digits/backspace       — forwarded to focused text input
func (m *Maneuver) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	const focusFields = 4
	switch msg.String() {
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
			m.modeIdx = (m.modeIdx - 1 + len(spacecraft.AllBurnModes)) % len(spacecraft.AllBurnModes)
			return nil, false
		case 1:
			m.fireAtIdx = (m.fireAtIdx - 1 + len(sim.AllTriggerEvents)) % len(sim.AllTriggerEvents)
			return nil, false
		}
	case "right":
		switch m.focus {
		case 0:
			m.modeIdx = (m.modeIdx + 1) % len(spacecraft.AllBurnModes)
			return nil, false
		case 1:
			m.fireAtIdx = (m.fireAtIdx + 1) % len(sim.AllTriggerEvents)
			return nil, false
		}
	case "enter":
		dv := m.parsedDV()
		if dv == 0 {
			return nil, false // ignore — user needs to type a number
		}
		dur := m.parsedDuration()
		event := sim.AllTriggerEvents[m.fireAtIdx]
		msg := BurnExecutedMsg{
			Mode:        spacecraft.AllBurnModes[m.modeIdx],
			DV:          dv,
			Duration:    dur,
			Event:       event,
			TriggerTime: m.loadedTriggerTime,
			EditingIdx:  m.editingIdx,
		}
		return func() tea.Msg { return msg }, true
	}
	var cmd tea.Cmd
	switch m.focus {
	case 2:
		m.dvInput, cmd = m.dvInput.Update(msg)
	case 3:
		m.durInput, cmd = m.durInput.Update(msg)
	}
	return cmd, false
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

// parsedDuration returns the duration field as a time.Duration. The field
// is in seconds; zero means impulsive.
func (m *Maneuver) parsedDuration() time.Duration {
	var secs float64
	if _, err := fmt.Sscanf(m.durInput.Value(), "%f", &secs); err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// Render composes the preview canvas + form panel.
func (m *Maneuver) Render(w *sim.World, cols, rows int) string {
	if w.Craft == nil {
		return "no spacecraft"
	}

	m.canvas.Clear()
	m.canvas.SetBasis(viewBasis(w))
	m.canvas.Center(orbital.Vec3{})

	c := w.Craft
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

	// Draw shadow trajectory after applying the current (mode, dv,
	// fire-at) triple. v0.6.1: when fire-at is event-relative, the
	// world's PreviewBurnState propagates the craft to the event
	// point before applying Δv — so a prograde burn at next apoapsis
	// raises the *opposite* point (perigee), not the apoapsis the
	// craft is nowhere near. Falls back to current-state preview if
	// the event is unreachable (hyperbolic / equatorial AN/DN).
	dv := m.parsedDV()
	dur := m.parsedDuration()
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
	c := w.Craft
	mode := spacecraft.AllBurnModes[m.modeIdx]
	budget := c.RemainingDeltaV()
	dur := m.parsedDuration()

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
	fireAt := sim.AllTriggerEvents[m.fireAtIdx]
	fireAtLabel := fireAt.String()
	if !m.loadedTriggerTime.IsZero() {
		delta := m.loadedTriggerTime.Sub(w.Clock.SimTime)
		fireAtLabel = fmt.Sprintf("%s %s", fireAtLabel, formatCountdown(delta))
	}
	if m.focus == 1 {
		fireAtLabel = m.theme.Warning.Render(fireAtLabel) + "  (←/→ to cycle)"
	} else {
		fireAtLabel = m.theme.Dim.Render(fireAtLabel)
	}

	burnDescr := "impulsive"
	if dur > 0 {
		// At constant thrust the analytical estimate is dv = thrust/mass × dur.
		// Show what the engine actually *can* deliver in the requested duration.
		mass := c.TotalMass()
		var estDv float64
		if mass > 0 {
			estDv = c.Thrust / mass * dur.Seconds()
		}
		actual := math.Min(dv, estDv)
		burnDescr = fmt.Sprintf("finite burn (%.0f N × %.1fs → est %.1f m/s, target %.0f m/s)",
			c.Thrust, dur.Seconds(), actual, dv)
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
	lines := []string{
		headerStyle.Render(header),
		"  mode:     " + modeLabel,
		"  fire at:  " + fireAtLabel,
		"  Δv:       " + m.dvInput.View() + " m/s" + warn,
		"  duration: " + m.durInput.View() + " s",
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
		ro := orbital.OrbitReadout(shadow.R, shadow.V, mu)
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
