package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// forceTrueColour pins the lipgloss profile for the test and restores the
// ambient one (go test is not a TTY, so every style renders colourless
// otherwise and a colour assertion is vacuous).
func forceTrueColour(t *testing.T) {
	t.Helper()
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })
}

// warnMarkerTheme gives Warning a colour no other style in the theme
// uses, so its escape code in a row means "this row rendered Warning".
func warnMarkerTheme() Theme {
	th := dimMarkerTheme()
	th.Warning = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	return th
}

const warnEsc = "\x1b[38;5;214m"

func rowContaining(out, needle string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(stripANSI(l), needle) {
			return l
		}
	}
	return ""
}

func focusRow(t *testing.T, m *Maneuver, n int) {
	t.Helper()
	for i := 0; i < 5 && m.focus != n; i++ {
		m.HandleKey(keyMsg("tab"), nil)
	}
	if m.focus != n {
		t.Fatalf("could not focus row %d", n)
	}
}

// TestPlannerDvAndThrottleRowsShowFocus (#585): mode, fire-at and
// iterate render in Warning when focused; the two number rows used to
// give no cue at all.
func TestPlannerDvAndThrottleRowsShowFocus(t *testing.T) {
	forceTrueColour(t)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	m := NewManeuver(warnMarkerTheme())
	m.ResetEditing()

	// Instrument check: a focused mode row does carry the Warning code.
	focusRow(t, m, 0)
	if r := rowContaining(m.Render(w, 140, 40, 0), "mode:"); !strings.Contains(r, warnEsc) {
		t.Fatalf("instrument cannot see Warning on the focused mode row: %q", r)
	}

	for _, tc := range []struct {
		focus int
		label string
		other string
	}{{2, "Δv:", "throttle:"}, {3, "throttle:", "Δv:"}} {
		focusRow(t, m, tc.focus)
		out := m.Render(w, 140, 40, 0)
		if r := rowContaining(out, tc.label); !strings.Contains(r, warnEsc) {
			t.Errorf("focused %s row shows no Warning cue: %q", tc.label, r)
		}
		if r := rowContaining(out, tc.other); strings.Contains(r, warnEsc) {
			t.Errorf("unfocused %s row carries the Warning cue: %q", tc.other, r)
		}
	}
}

// TestPlannerFooterNamesOnlyKeysThatAct (#585): ←/→ cycle on mode and
// fire-at, toggle on iterate, and move the text cursor on the number
// rows, so the footer must change with the focused row.
func TestPlannerFooterNamesOnlyKeysThatAct(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	m := NewManeuver(Theme{})
	m.ResetEditing()
	footer := func(f int) string {
		focusRow(t, m, f)
		lines := strings.Split(strings.TrimRight(m.Render(w, 140, 40, 0), "\n"), "\n")
		return lines[len(lines)-1]
	}
	for _, f := range []int{0, 1} {
		if got := footer(f); !strings.Contains(got, "[←/→] cycle") {
			t.Errorf("focus %d footer should name [←/→] cycle: %q", f, got)
		}
	}
	for _, f := range []int{2, 3} {
		got := footer(f)
		if strings.Contains(got, "[←/→]") || strings.Contains(got, "cycle") {
			t.Errorf("focus %d (number row) footer names ←/→ cycle, which does nothing there: %q", f, got)
		}
		if !strings.Contains(got, "[enter]") || !strings.Contains(got, "[tab]") {
			t.Errorf("focus %d footer lost the keys that always act: %q", f, got)
		}
	}
	got := footer(4)
	if strings.Contains(got, "cycle") || !strings.Contains(got, "[space]") {
		t.Errorf("iterate footer should name [space]/[←/→] toggle, not cycle: %q", got)
	}
}

// circularise sets the active vessel onto an orbit with the given
// speed factor over circular speed (1 = circular).
func setOrbit(w *sim.World, factor float64) {
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	rhat := c.State.R.Unit()
	// a horizontal direction: any unit vector perpendicular to r.
	perp := orbital.Vec3{Z: 1}.Cross(rhat)
	if perp.Norm() < 1e-6 {
		perp = orbital.Vec3{X: 1}.Cross(rhat)
	}
	v := perp.Unit().Scale(factor * sqrtF(mu/r))
	c.State = physics.StateVector{R: c.State.R, V: v, M: c.State.M}
}

func sqrtF(x float64) float64 {
	z := x
	for i := 0; i < 60; i++ {
		z = 0.5 * (z + x/z)
	}
	return z
}

func selectFireAt(t *testing.T, m *Maneuver, ev sim.TriggerEvent) {
	t.Helper()
	focusRow(t, m, 1)
	for i := 0; i < len(sim.AllTriggerEvents) && sim.AllTriggerEvents[m.fireAtIdx] != ev; i++ {
		m.HandleKey(keyMsg("right"), nil)
	}
	if sim.AllTriggerEvents[m.fireAtIdx] != ev {
		t.Fatalf("could not select %v", ev)
	}
}

// TestApsisTriggerUndefinedOnCircularOrbit (#585): next peri / next apo
// read (undefined) when e is below orbital.ApsisDegenerateEcc (1e-6),
// the display convention the orbit screen's navigation box uses.
func TestApsisTriggerUndefinedOnCircularOrbit(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []sim.TriggerEvent{sim.TriggerNextPeri, sim.TriggerNextApo} {
		m := NewManeuver(Theme{})
		m.ResetEditing()
		selectFireAt(t, m, ev)

		// Positive control: a clearly eccentric orbit is defined.
		setOrbit(w, 1.05)
		if r := rowContaining(m.Render(w, 140, 40, 0), "fire at:"); strings.Contains(r, "undefined") {
			t.Errorf("%v on an eccentric orbit reads undefined: %q", ev, r)
		}
		// Circular: undefined.
		setOrbit(w, 1.0)
		if r := rowContaining(m.Render(w, 140, 40, 0), "fire at:"); !strings.Contains(r, "(undefined)") {
			t.Errorf("%v on a circular orbit should read (undefined): %q", ev, r)
		}
		// Just above the threshold (e ~ 2e-5) is defined again.
		setOrbit(w, 1.00001)
		if r := rowContaining(m.Render(w, 140, 40, 0), "fire at:"); strings.Contains(r, "undefined") {
			t.Errorf("%v just above the threshold reads undefined: %q", ev, r)
		}
	}
	// AN/DN is not touched by this rule.
	m := NewManeuver(Theme{})
	m.ResetEditing()
	selectFireAt(t, m, sim.TriggerNextAN)
	setOrbit(w, 1.0)
	if r := rowContaining(m.Render(w, 140, 40, 0), "fire at:"); strings.Contains(r, "undefined") {
		t.Errorf("AN row picked up the apsis rule: %q", r)
	}
}

// TestPlannerPreviewFollowsPlanCursor (#585): with the cursor on a
// planted node that is not loaded into the form, the PROJECTED ORBIT
// canvas draws that plan, not the form's draft.
func TestPlannerPreviewFollowsPlanCursor(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{Mode: spacecraft.BurnPrograde, DV: 300, TriggerTime: w.Clock.SimTime.Add(10 * time.Minute)},
		spacecraft.ManeuverNode{Mode: spacecraft.BurnRetrograde, DV: 120, TriggerTime: w.Clock.SimTime.Add(40 * time.Minute)},
	)
	canvasFor := func(cursorUps int, draft string) string {
		m := NewManeuver(Theme{})
		m.Resize(140, 36)
		m.ResetEditing()
		for i := 0; i < cursorUps; i++ {
			m.HandleKey(keyMsg("up"), c.Nodes)
		}
		m.dvInput.SetValue(draft)
		m.Render(w, 140, 40, 0)
		return m.canvas.String()
	}
	// cursor on node 0 (two ups from the new-node row), two different drafts.
	a := canvasFor(2, "100")
	b := canvasFor(2, "2500")
	// Instrument check: the canvas does depend on the draft when the draft is what is shown.
	onDraft := canvasFor(0, "100")
	onDraft2 := canvasFor(0, "2500")
	if onDraft == onDraft2 {
		t.Fatal("instrument cannot see a draft change on the canvas")
	}
	otherPlan := canvasFor(1, "100")
	if a == otherPlan {
		t.Error("canvas does not distinguish the plan under the cursor: node 0 and node 1 draw the same")
	}
	if a != b {
		t.Error("canvas changed with the draft while the cursor sits on a planted plan: it draws the draft, not the plan")
	}
	if a == onDraft {
		t.Error("canvas on a planted plan equals the draft canvas")
	}
}
