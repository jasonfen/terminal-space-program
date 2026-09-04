package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// TestPlanTransferRefusesLoudlyWithNoTarget — #428 mechanical fix: `H`
// with World.Target at its zero value (TargetNone) used to be a
// deliberate silent no-op ("that gap is deliberate" per the old
// comment); it must now refuse out loud like every sibling guard.
func TestPlanTransferRefusesLoudlyWithNoTarget(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Target = sim.Target{} // TargetNone, the zero value
	if !a.world.CraftVisibleHere() {
		t.Fatal("fresh world: active craft should be visible in its own system")
	}

	pressKey(a, 'H')

	want := "transfer: no target — press t to aim at a body"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
}

// TestManeuverPlannerLowercaseCFlashesCtrlKHint — ADR 0047 §3 / #428:
// `c` inside the planner no longer clears every node (nor does it
// silently fall through to ReArmDock, the map's binding for the same
// key) — it flashes a hint pointing at ctrl+k, and the plan survives.
func TestManeuverPlannerLowercaseCFlashesCtrlKHint(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	c.Nodes = append(c.Nodes, sim.ManeuverNode{DV: 100, TriggerTime: a.world.Clock.SimTime.Add(time.Hour)})
	a.active = screenManeuver

	pressKey(a, 'c')

	if a.active != screenManeuver {
		t.Errorf("active screen = %v after 'c', want screenManeuver (form must stay open)", a.active)
	}
	if len(a.world.ActiveCraft().Nodes) != 1 {
		t.Errorf("'c' cleared the plan — got %d nodes, want 1", len(a.world.ActiveCraft().Nodes))
	}
	if !strings.Contains(a.statusMsg, "ctrl+k") {
		t.Errorf("statusMsg = %q, want it to mention ctrl+k", a.statusMsg)
	}
}

// TestManeuverPlannerCtrlKStillClearsAll — the control for the above:
// ctrl+k is still the real clear-all binding from inside the planner.
func TestManeuverPlannerCtrlKStillClearsAll(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	c.Nodes = append(c.Nodes, sim.ManeuverNode{DV: 100, TriggerTime: a.world.Clock.SimTime.Add(time.Hour)})
	a.active = screenManeuver

	// HandleKey's ctrl+k path returns a tea.Cmd (NodeClearAllMsg) rather
	// than mutating state directly — the same two-step a real bubbletea
	// run loop performs: execute the Cmd, feed its Msg back through Update.
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if cmd == nil {
		t.Fatal("ctrl+k produced no Cmd")
	}
	a.Update(cmd())

	if len(a.world.ActiveCraft().Nodes) != 0 {
		t.Errorf("ctrl+k did not clear the plan — got %d nodes", len(a.world.ActiveCraft().Nodes))
	}
	if a.active != screenOrbit {
		t.Errorf("active screen = %v after ctrl+k, want screenOrbit", a.active)
	}
}

// TestManeuverPlannerQuickPlanHDispatchesAndReturnsToMap — ADR 0047 §4
// / #428: pressing H inside the planner does exactly what it does on
// the map (plants a transfer to World.Target) and leaves the planner.
func TestManeuverPlannerQuickPlanHDispatchesAndReturnsToMap(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: 1}
	a.active = screenManeuver
	before := len(a.world.ActiveCraft().Nodes)

	pressKey(a, 'H')

	if a.active != screenOrbit {
		t.Errorf("active screen = %v after H in the planner, want screenOrbit", a.active)
	}
	if got := len(a.world.ActiveCraft().Nodes); got <= before {
		t.Errorf("H inside the planner planted nothing: node count %d → %d", before, got)
	}
}

// TestManeuverPlannerQuickPlanGatedOffWhileTextFieldFocused — ADR 0047
// §4 / #428: H/I/C/K/P/R must not fire while the Δv or throttle field
// has focus — otherwise typing a value could get hijacked into
// planting a burn.
func TestManeuverPlannerQuickPlanGatedOffWhileTextFieldFocused(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: 1}
	a.active = screenManeuver
	// Tab twice from the default focus (mode=0) to reach the Δv field (2).
	a.Update(tea.KeyMsg{Type: tea.KeyTab})
	a.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !a.maneuver.TextFieldFocused() {
		t.Fatal("setup: Δv field should have focus after two tabs")
	}
	before := len(a.world.ActiveCraft().Nodes)

	pressKey(a, 'H')

	if a.active != screenManeuver {
		t.Errorf("active screen = %v after H with a text field focused, want screenManeuver (H must not fire)", a.active)
	}
	if got := len(a.world.ActiveCraft().Nodes); got != before {
		t.Errorf("H fired while a text field had focus: node count %d → %d", before, got)
	}
}

// TestBurnExecutedOverBudgetFlashesAndStillPlants — ADR 0047 §2 / #428:
// warn and allow. A node whose Δv exceeds the vessel's remaining
// budget plants exactly as commanded, and the status flash names the
// shortfall.
func TestBurnExecutedOverBudgetFlashesAndStillPlants(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	budget := c.RemainingDeltaV()
	over := budget + 1521

	// Event-relative (not the bare TriggerAbsolute-and-zero-time
	// "fire now" quick-plant path) so this goes through PlanNode and
	// lands a real, inspectable Nodes[0] entry rather than starting an
	// immediate ActiveBurn.
	a.Update(screens.BurnExecutedMsg{
		Mode:       spacecraft.BurnPrograde,
		DV:         over,
		Event:      sim.TriggerNextPeri,
		EditingIdx: -1,
	})

	if len(a.world.ActiveCraft().Nodes) != 1 {
		t.Fatalf("over-budget plant did not land a node: %d nodes", len(a.world.ActiveCraft().Nodes))
	}
	if a.world.ActiveCraft().Nodes[0].DV != over {
		t.Errorf("planted node DV = %.0f, want %.0f (plant should never be refused/clamped)", a.world.ActiveCraft().Nodes[0].DV, over)
	}
	want := "plan exceeds Δv budget by 1521 m/s"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
}
