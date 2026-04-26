package screens

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
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
	theme      Theme
	canvas     *widgets.Canvas
	dvInput    textinput.Model
	durInput   textinput.Model
	modeIdx    int
	fireAtIdx  int
	focus      int // 0=mode, 1=fireAt, 2=dv, 3=duration
}

// BurnExecutedMsg is emitted when the user hits Enter. App consumes it.
// Duration zero = impulsive (legacy path); >0 = finite burn. Event
// (v0.6.0+) selects the trigger model — TriggerAbsolute uses the
// app-side default delay; event-relative modes leave TriggerTime zero
// and let the World's lazy-freeze resolver compute it from the live
// orbit on the first Tick after plant.
type BurnExecutedMsg struct {
	Mode     spacecraft.BurnMode
	DV       float64
	Duration time.Duration
	Event    sim.TriggerEvent
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
	dur.SetValue("0")

	m := &Maneuver{
		theme:    th,
		canvas:   widgets.NewCanvas(60, 20),
		dvInput:  dv,
		durInput: dur,
	}
	m.applyFocus()
	return m
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
	canvasCols := cols * 6 / 10
	if canvasCols < 20 {
		canvasCols = 20
	}
	if canvasCols > 80 {
		canvasCols = 80
	}
	m.canvas.Resize(canvasCols, rows-6)
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
		return func() tea.Msg {
			return BurnExecutedMsg{
				Mode:     spacecraft.AllBurnModes[m.modeIdx],
				DV:       dv,
				Duration: dur,
				Event:    event,
			}
		}, true
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
	m.canvas.Center(orbital.Vec3{})

	// Draw current orbit from state elements (white/primary).
	c := w.Craft
	mu := c.Primary.GravitationalParameter()
	currentEl := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	m.canvas.FitTo(math.Max(currentEl.Apoapsis(), c.State.R.Norm()) * 1.1)
	m.canvas.DrawEllipseDotted(currentEl, 360, 4)

	// Draw shadow trajectory after applying the current (mode, dv) pair.
	dv := m.parsedDV()
	mode := spacecraft.AllBurnModes[m.modeIdx]
	dir := spacecraft.DirectionUnit(mode, c.State.R, c.State.V)
	shadowState := physics.StateVector{
		R: c.State.R,
		V: c.State.V.Add(dir.Scale(dv)),
		M: c.State.M,
	}
	// Propagate for the new orbital period (or 1 hour if hyperbolic).
	shadowPeriod := orbitalPeriodOrFallback(shadowState, mu)
	pts := planner.Predict(shadowState, mu, shadowPeriod, 256)
	for _, p := range pts {
		m.canvas.Plot(p)
	}

	// Plot planet (primary) at origin.
	m.canvas.Plot(orbital.Vec3{})
	// Plot craft (current position) with a cluster.
	for i := -4; i <= 4; i++ {
		step := 1.0 / m.canvas.Scale()
		m.canvas.Plot(c.State.R.Add(orbital.Vec3{X: float64(i) * step}))
		m.canvas.Plot(c.State.R.Add(orbital.Vec3{Y: float64(i) * step}))
	}

	canvasPanel := m.theme.HUDBox.Render(m.canvas.String())

	form := m.renderForm(w, dv, shadowState, mu)
	body := strings.Join([]string{canvasPanel, form}, "\n")

	footer := m.theme.Footer.Render(
		"[tab] cycle field  [←/→] cycle mode  [enter] commit  [esc] cancel  [digits] edit",
	)
	return m.theme.Title.Render("maneuver planner") + "\n" + body + "\n" + footer
}

func (m *Maneuver) renderForm(w *sim.World, dv float64, shadow physics.StateVector, mu float64) string {
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

	// Fire-at line — highlight if focused, otherwise dim.
	fireAt := sim.AllTriggerEvents[m.fireAtIdx]
	fireAtLabel := fireAt.String()
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

	lines := []string{
		m.theme.Primary.Render("BURN PLAN"),
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
		primaryR := c.Primary.RadiusMeters()
		lines = append(lines, "", m.theme.Primary.Render("PROJECTED ORBIT"))
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
