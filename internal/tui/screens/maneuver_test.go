package screens

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// TestManeuverClearAllIsCtrlKOnly — ADR 0047 §3 / #428: only ctrl+k
// clears every planted node now. `c` and `C` are handled one level up
// in app.go (a flash pointing at ctrl+k for `c`; `C` routes to the
// PlanCircularize quick-plan) and never reach this screen's HandleKey
// at all, so from here they must be plain no-ops — no NodeClearAllMsg,
// form stays open.
func TestManeuverClearAllIsCtrlKOnly(t *testing.T) {
	m := NewManeuver(Theme{})
	cmd, done := m.HandleKey(keyMsg("ctrl+k"), nil)
	if !done {
		t.Fatal("ctrl+k: done = false, want true (form should close)")
	}
	if cmd == nil {
		t.Fatal("ctrl+k: nil cmd, want a NodeClearAllMsg command")
	}
	if _, ok := cmd().(NodeClearAllMsg); !ok {
		t.Errorf("ctrl+k: emitted %T, want NodeClearAllMsg", cmd())
	}

	for _, key := range []string{"c", "C"} {
		m := NewManeuver(Theme{})
		cmd, done := m.HandleKey(keyMsg(key), nil)
		if done {
			t.Errorf("%q: done = true, want false — c/C must not close the form from HandleKey", key)
		}
		if cmd != nil {
			if _, ok := cmd().(NodeClearAllMsg); ok {
				t.Errorf("%q: emitted NodeClearAllMsg — clear-all must be ctrl+k only", key)
			}
		}
	}
}

// TestManeuverRendersPlannedNodes — the form panel lists every
// planted node for the active craft, and shows the new-node row when
// there are none.
func TestManeuverRendersPlannedNodes(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	m := NewManeuver(Theme{})

	if out := m.Render(w, 120, 40, 0); !strings.Contains(out, "PLANNED NODES") || !strings.Contains(out, "+ new node") {
		t.Errorf("new-node row missing with no nodes planted:\n%s", out)
	}

	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{DV: 120, TriggerTime: w.Clock.SimTime.Add(time.Hour)},
		spacecraft.ManeuverNode{DV: 45, TriggerTime: w.Clock.SimTime.Add(2 * time.Hour)},
	)
	out := m.Render(w, 120, 40, 0)
	if !strings.Contains(out, "PLANNED NODES (2)") {
		t.Error("node-count header missing / wrong with 2 nodes planted")
	}
	if !strings.Contains(out, "120 m/s") || !strings.Contains(out, "45 m/s") {
		t.Errorf("planned-node Δv values not listed:\n%s", out)
	}
}

// TestPlanCursorNavigatesAndLoadsOnEnter — ADR 0047 §1 / #428: ↑/↓ move
// the Plan Cursor through PLANNED NODES (bounded to [0, len(Nodes)],
// the last row being the blank new-node row); Enter on an unloaded
// planted node loads it into the form (mirrors the mouse LoadNode
// path) instead of committing.
func TestPlanCursorNavigatesAndLoadsOnEnter(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{Mode: spacecraft.BurnPrograde, DV: 120, TriggerTime: w.Clock.SimTime.Add(time.Hour)},
		spacecraft.ManeuverNode{Mode: spacecraft.BurnRetrograde, DV: 45, TriggerTime: w.Clock.SimTime.Add(2 * time.Hour)},
	)
	m := NewManeuver(Theme{})
	m.ResetEditing() // cursor defaults onto the new-node row (index 2)

	if got := m.cursorRow(len(c.Nodes)); got != 2 {
		t.Fatalf("fresh-open cursor = %d, want 2 (new-node row)", got)
	}

	// ↑ walks up into the planted nodes: 2 → 1 → 0, then stays at 0.
	if cmd, done := m.HandleKey(keyMsg("up"), c.Nodes); cmd != nil || done {
		t.Fatalf("up: unexpected cmd/done")
	}
	if got := m.cursorRow(len(c.Nodes)); got != 1 {
		t.Fatalf("cursor after one up = %d, want 1", got)
	}
	m.HandleKey(keyMsg("up"), c.Nodes)
	if got := m.cursorRow(len(c.Nodes)); got != 0 {
		t.Fatalf("cursor after two ups = %d, want 0", got)
	}
	m.HandleKey(keyMsg("up"), c.Nodes) // one more — must clamp, not go negative
	if got := m.cursorRow(len(c.Nodes)); got != 0 {
		t.Fatalf("cursor clamped past 0 = %d, want 0", got)
	}

	// Enter on the unloaded node 0 must LOAD it, not commit — the form
	// stays open (done=false) and takes on node 0's values.
	cmd, done := m.HandleKey(keyMsg("enter"), c.Nodes)
	if done || cmd != nil {
		t.Fatalf("enter on unloaded cursor node: got done=%v cmd=%v, want a load (done=false, no cmd)", done, cmd)
	}
	if m.editingIdx != 0 {
		t.Fatalf("enter did not load node 0 for editing: editingIdx=%d", m.editingIdx)
	}
	if got := m.dvInput.Value(); got != "120" {
		t.Errorf("loaded form Δv = %q, want \"120\" (node 0's own value)", got)
	}

	// Enter AGAIN, now that node 0 is loaded (cur == editingIdx),
	// commits instead of reloading.
	cmd2, done2 := m.HandleKey(keyMsg("enter"), c.Nodes)
	if !done2 || cmd2 == nil {
		t.Fatalf("second enter (already loaded) should commit: done=%v cmd=%v", done2, cmd2)
	}
	if _, ok := cmd2().(BurnExecutedMsg); !ok {
		t.Errorf("second enter emitted %T, want BurnExecutedMsg", cmd2())
	}
}

// TestPlanCursorCtrlDDeletesCursorNode — ADR 0047 §1 / #428: ctrl+d
// deletes whatever the Plan Cursor is pointing at, not only a
// mouse-loaded node (the pre-#428 bug: ctrl+d silently did nothing
// unless editingIdx was already set by a click).
func TestPlanCursorCtrlDDeletesCursorNode(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{DV: 120, TriggerTime: w.Clock.SimTime.Add(time.Hour)},
	)
	m := NewManeuver(Theme{})
	m.ResetEditing() // editingIdx == -1, nothing mouse-loaded

	m.HandleKey(keyMsg("up"), c.Nodes) // cursor: new-row(1) → node 0
	if got := m.cursorRow(len(c.Nodes)); got != 0 {
		t.Fatalf("cursor after up = %d, want 0", got)
	}

	cmd, done := m.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlD}, c.Nodes)
	if !done || cmd == nil {
		t.Fatalf("ctrl+d on cursor node with nothing mouse-loaded: got done=%v cmd=%v, want a NodeDeleteMsg", done, cmd)
	}
	del, ok := cmd().(NodeDeleteMsg)
	if !ok {
		t.Fatalf("ctrl+d emitted %T, want NodeDeleteMsg", cmd())
	}
	if del.EditingIdx != 0 {
		t.Errorf("NodeDeleteMsg.EditingIdx = %d, want 0 (the cursor row)", del.EditingIdx)
	}
}

// TestManeuverOverBudgetNodeMarked — ADR 0047 §2 / #428: a planted
// node whose Δv exceeds the vessel's remaining budget plants (this
// test only checks the RENDER side) and its row carries the alert
// marker naming the shortfall.
func TestManeuverOverBudgetNodeMarked(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	budget := c.RemainingDeltaV()
	over := budget + 1521
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		Mode: spacecraft.BurnPrograde, DV: over, TriggerTime: w.Clock.SimTime.Add(time.Hour),
	})
	m := NewManeuver(Theme{})
	// Wide enough (formPanelWidth) that the row's own text isn't itself
	// ellipsized — that behavior is TestManeuverEllipsizesNarrowRows's job.
	out := m.Render(w, 260, 40, 0)
	if !strings.Contains(out, "exceeds budget by 1521 m/s") {
		t.Errorf("over-budget marker missing / wrong for a %.0f m/s node on a %.0f m/s budget:\n%s", over, budget, out)
	}
}

// TestManeuverBudgetLineShowsAfterPlan — #428 mechanical fix: the
// budget line reads "Δv budget: X m/s (Y after plan)" once anything
// is planted, not just the pre-plan total.
func TestManeuverBudgetLineShowsAfterPlan(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	budget := c.RemainingDeltaV()
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		Mode: spacecraft.BurnPrograde, DV: 100, TriggerTime: w.Clock.SimTime.Add(time.Hour),
	})
	m := NewManeuver(Theme{})
	out := m.Render(w, 120, 40, 0)
	want := "after plan"
	if !strings.Contains(out, want) {
		t.Errorf("budget line missing %q:\n%s", want, out)
	}
	if strings.Contains(out, "Δv budget remaining:") {
		t.Error("old \"Δv budget remaining:\" wording still present")
	}
	_ = budget
}

// TestManeuverQuickPlansBlockListsAllSix — ADR 0047 §4 / #428: the
// QUICK PLANS block lists all six one-key planners regardless of
// legality (illegal ones are dimmed with a reason, not hidden).
func TestManeuverQuickPlansBlockListsAllSix(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	m := NewManeuver(Theme{})
	// Wide enough that the reason text isn't itself ellipsized.
	out := m.Render(w, 260, 40, 0)
	if !strings.Contains(out, "QUICK PLANS") {
		t.Fatalf("QUICK PLANS header missing:\n%s", out)
	}
	for _, key := range []string{"[H]", "[I]", "[C]", "[K]", "[P]", "[R]"} {
		if !strings.Contains(out, key) {
			t.Errorf("QUICK PLANS block missing %s:\n%s", key, out)
		}
	}
	// A fresh world has no target and no body selected — H and P must
	// show their reasons.
	if !strings.Contains(out, "no target — press t to aim at a body") {
		t.Errorf("H's no-target reason missing:\n%s", out)
	}
	if !strings.Contains(out, "no body selected") {
		t.Errorf("P's no-body-selected reason missing:\n%s", out)
	}
}

// TestManeuverEllipsizesNarrowRows — #428 mechanical fix: at a narrow
// terminal width (104×24, the review's own repro) a row too long for
// the form panel ellipsizes with "…" instead of the caller (or the
// outer terminal) hard-clipping it mid-word. formPanelWidth's own
// arithmetic is asserted directly — sane and independent of exactly
// how wide the braille canvas box renders in characters — and then a
// known-long QUICK PLANS reason is checked against it: present in full
// at a width that fits it, cut with "…" at one that doesn't.
func TestManeuverEllipsizesNarrowRows(t *testing.T) {
	if got := formPanelWidth(104); got <= 0 || got > 104 {
		t.Fatalf("formPanelWidth(104) = %d, want a small positive width", got)
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// The full row ("  [H] transfer to target body — " + this reason)
	// is ~70 characters — comfortably wider than formPanelWidth(104)
	// (40) — so ansi.Truncate must cut it, and since it truncates from
	// the right, the reason text at the row's tail is exactly what
	// goes missing.
	longReason := "no target — press t to aim at a body"
	m := NewManeuver(Theme{})

	narrow := m.Render(w, 104, 24, 0)
	if strings.Contains(narrow, longReason) {
		t.Errorf("H's full no-target reason survived un-ellipsized at 104 cols (panel width %d):\n%s", formPanelWidth(104), narrow)
	}
	if !strings.Contains(narrow, "…") {
		t.Errorf("expected an ellipsis on at least one row at 104 cols:\n%s", narrow)
	}

	wide := m.Render(w, 260, 40, 0)
	if !strings.Contains(wide, longReason) {
		t.Errorf("H's no-target reason missing at a wide (260-col) render, where it should fit unellipsized:\n%s", wide)
	}
}

// TestManeuverRenderFitsWithinCols — regression for the bug the first
// version of formPanelWidth introduced: it ellipsized the form's OWN
// text correctly but undercounted the canvas box's real on-screen
// width (HUDBox's rounded border AND its Padding(0, 1) — 4 columns,
// not the 2 the arithmetic assumed), so the COMBINED canvas+gap+form
// row still overflowed `cols` — just one level up. An overflowing row
// doesn't show as a missing "…" in the string itself; it shows as the
// outer terminal soft-wrapping it, which is the exact hard-clip
// symptom (mid-word cuts) this whole fix exists to remove. Render
// every joined line's actual rune width against `cols` directly so a
// future overhead miscount fails a unit test instead of only showing
// up live in a tmux capture.
func TestManeuverRenderFitsWithinCols(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{Mode: spacecraft.BurnPrograde, DV: 3054, TriggerTime: w.Clock.SimTime.Add(4*time.Hour + 17*time.Minute)},
		spacecraft.ManeuverNode{Mode: spacecraft.BurnNormalPlus, DV: 69, TriggerTime: w.Clock.SimTime.Add(4*24*time.Hour + 10*time.Hour)},
		spacecraft.ManeuverNode{Mode: spacecraft.BurnRetrograde, DV: 789, TriggerTime: w.Clock.SimTime.Add(5*24*time.Hour + 4*time.Hour)},
	)
	for _, cols := range []int{80, 104, 120, 140, 200} {
		m := NewManeuver(Theme{})
		// Production always resizes the canvas from tea.WindowSizeMsg
		// before the next Render (app.go); a fresh Maneuver's canvas
		// otherwise stays at NewManeuver's NewCanvas(60, 20) default
		// regardless of cols, which would make this test check a
		// combination the running game never actually renders.
		m.Resize(cols, 40)
		out := m.Render(w, cols, 40, 0)
		for i, line := range strings.Split(out, "\n") {
			if n := len([]rune(stripANSI(line))); n > cols {
				t.Errorf("cols=%d: rendered row %d is %d runes wide (overflows the terminal, will soft-wrap): %q", cols, i, n, line)
			}
		}
	}
}

// dimMarkerTheme is a Theme whose Dim style is the only one that emits a
// distinguishable ANSI code — everything else is a no-op — so a test can
// assert a row is (or isn't) Dim-wrapped by looking for that one code.
func dimMarkerTheme() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		HUDBox:  lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

// TestManeuverPlannedNodeRowsNotDimmed — #428 mechanical fix: PLANNED
// NODES rows render at normal foreground (no Theme.Dim wrapper) so the
// actual subject of the screen doesn't read as disabled chrome. Only
// the cursor row's own highlight (Primary/Warning) and the new-node
// row's hint are allowed to differ.
func TestManeuverPlannedNodeRowsNotDimmed(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{Mode: spacecraft.BurnPrograde, DV: 77, TriggerTime: w.Clock.SimTime.Add(time.Hour)},
		spacecraft.ManeuverNode{Mode: spacecraft.BurnRetrograde, DV: 55, TriggerTime: w.Clock.SimTime.Add(2 * time.Hour)},
	)
	m := NewManeuver(dimMarkerTheme())
	m.ResetEditing()
	// Move the cursor to node 0, then node 1's row is neither the
	// cursor row nor loaded — the plain case this test targets.
	m.HandleKey(keyMsg("up"), c.Nodes)
	out := m.Render(w, 120, 40, 0)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(stripANSI(line), "55 m/s") && strings.Contains(line, "\x1b[38;5;240m") {
			t.Errorf("PLANNED NODES row still Dim-styled: %q", line)
		}
	}
}
