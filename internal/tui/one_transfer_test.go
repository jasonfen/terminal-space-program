package tui

// S7 (UX cycle 3 Phase 4 wave 2): one transfer at a time (#587), a clear
// that stops a firing burn (#588 lead 1), the ENGINE node: row saying why
// an apsis node waits (wave 1 known issue 5). Every guard drives the
// production seam: keys through App.Update, ticks through World.Tick
// (via TickMsg), the view through App.View.

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func s7Moon(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, a, "Moon")}
	return a
}

func s7RoundTrip(t *testing.T, a *App) {
	t.Helper()
	path := t.TempDir() + "/save.json"
	if err := save.Save(a.world, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	w, err := save.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a.world = w
}

// The caption's time is the time ENGINE's node: row shows for the same
// burn (the burn's start), read off the rendered frame.
func TestSecondTransferCaptionMatchesEngineNodeRow(t *testing.T) {
	a := s7Moon(t)
	pressKey(a, 'H')
	row := regexp.MustCompile(`node:\s+▸ in (\S+)\s+Prograde\s+3062 m/s`).FindStringSubmatch(a.View())
	if row == nil {
		t.Fatalf("setup: no queued Prograde node: row in the frame:\n%s", a.View())
	}
	pressKey(a, 'H')
	want := "transfer: a burn is already planted (Prograde 3062 m/s in " + row[1] + "); clear it in [m] first"
	if a.statusMsg != want {
		t.Errorf("flash = %q\nwant      %q", a.statusMsg, want)
	}
}

// Refusal holds across a save/load (the plan is persisted state, the
// guard reads it, not a latch) and after the flash has expired.
func TestSecondTransferRefusesAcrossSaveLoadAndFlashExpiry(t *testing.T) {
	a := s7Moon(t)
	pressKey(a, 'H')
	n1, dv1 := triagePlanTotals(a)
	s7RoundTrip(t, a)
	a.statusMsg = ""
	pressKey(a, 'H')
	n2, dv2 := triagePlanTotals(a)
	if n2 != n1 || dv2 > dv1*1.01 {
		t.Errorf("after save/load a second H stacked: %d/%.0f -> %d/%.0f", n1, dv1, n2, dv2)
	}
	if !strings.Contains(a.statusMsg, "already planted") {
		t.Errorf("after save/load the refusal said %q", a.statusMsg)
	}
}

// Mid-departure under warp (Kepler coast, then the RK4 burn): the press
// refuses and the half-flown burn is untouched.
func TestSecondTransferRefusesMidDepartureUnderWarp(t *testing.T) {
	a := s7Moon(t)
	pressKey(a, 'H')
	a.world.Clock.WarpIdx = 2 // 100x: Kepler coast to the node, RK4 once it ignites
	for i := 0; i < 400 && a.world.ActiveCraft().ActiveBurn == nil; i++ {
		triageTick(a, 1)
	}
	ab := a.world.ActiveCraft().ActiveBurn
	if ab == nil {
		t.Fatal("setup: the departure never ignited")
	}
	n1, dv1 := triagePlanTotals(a)
	pressKey(a, 'H')
	n2, dv2 := triagePlanTotals(a)
	if n2 != n1 || dv2 != dv1 {
		t.Errorf("mid-burn H changed the plan: %d/%.0f -> %d/%.0f", n1, dv1, n2, dv2)
	}
	if a.world.ActiveCraft().ActiveBurn != ab {
		t.Error("mid-burn H replaced the running burn")
	}
	if !strings.Contains(a.statusMsg, "already planted") {
		t.Errorf("mid-burn refusal said %q", a.statusMsg)
	}
}

// The planter still works from the planner's QUICK PLANS (H inside [m]
// routes to the same method) and the refusal applies there too.
func TestSecondTransferRefusesFromPlannerQuickPlans(t *testing.T) {
	a := s7Moon(t)
	pressKey(a, 'H')
	n1, _ := triagePlanTotals(a)
	pressKey(a, 'm')
	pressKey(a, 'H')
	if n2, _ := triagePlanTotals(a); n2 != n1 {
		t.Errorf("planner H stacked: %d -> %d nodes", n1, n2)
	}
	if !strings.Contains(a.statusMsg, "already planted") {
		t.Errorf("planner refusal said %q", a.statusMsg)
	}
}

func s7MercuryBurning(t *testing.T, warpIdx int) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, a, "Mercury")}
	pressKey(a, 'H')
	a.world.Clock.WarpIdx = warpIdx
	triageTick(a, 20)
	if a.world.ActiveCraft().ActiveBurn == nil {
		t.Fatal("setup: the Mercury departure did not ignite")
	}
	a.world.Clock.WarpIdx = 0
	return a
}

func s7NoBurn(t *testing.T, a *App, what string) {
	t.Helper()
	c := a.world.ActiveCraft()
	if c.ActiveBurn != nil {
		t.Errorf("%s: a %v burn is still running", what, c.ActiveBurn.Mode)
	}
	m0 := c.TotalMass()
	triageTick(a, 200)
	if d := m0 - c.TotalMass(); d > 0 {
		t.Errorf("%s: the engine burned %.0f kg over the next 10 s", what, d)
	}
}

// ctrl+k at 100x warp, then across a save/load, then the next ticks.
func TestPlannerClearAllStopsBurnUnderWarpAndAcrossSaveLoad(t *testing.T) {
	a := s7MercuryBurning(t, 2)
	pressKey(a, 'm')
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	a.Update(cmd())
	s7RoundTrip(t, a)
	s7NoBurn(t, a, "after ctrl+k and save/load")
	// And still nothing after the Moon plan is planted on top.
	a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, a, "Moon")}
	pressKey(a, 'H')
	if a.world.ActiveCraft().ActiveBurn != nil {
		t.Error("planting the next transfer revived a burn")
	}
}

// Deleting every row with ctrl+d stops the firing burn once the plan is
// empty. Mid-departure the departure has left the list, so the one row
// left is the arrival burn.
func TestPlannerDeletingEveryRowStopsFiringBurn(t *testing.T) {
	a := s7MercuryBurning(t, 0)
	for guard := 0; len(a.world.ActiveCraft().Nodes) > 0 && guard < 10; guard++ {
		if a.world.ActiveCraft().ActiveBurn == nil {
			t.Fatal("the burn stopped before the last row was deleted")
		}
		pressKey(a, 'm')
		a.Update(tea.KeyMsg{Type: tea.KeyUp}) // Plan Cursor off the new-node row onto the last planted row
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
		if cmd == nil {
			t.Fatal("ctrl+d produced no Cmd")
		}
		a.Update(cmd())
	}
	if n := len(a.world.ActiveCraft().Nodes); n != 0 {
		t.Fatalf("setup: %d rows left", n)
	}
	if !strings.Contains(a.statusMsg, "burn stopped") {
		t.Errorf("deleting the last row of a firing plan said %q", a.statusMsg)
	}
	s7NoBurn(t, a, "after deleting every row")
}

// Clearing with nothing firing says nothing new (positive control that the
// flash is tied to a stopped burn, not to every clear).
func TestPlannerClearAllWithNothingFiringDoesNotClaimAStop(t *testing.T) {
	a := s7Moon(t)
	pressKey(a, 'H')
	pressKey(a, 'm')
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	a.Update(cmd())
	if strings.Contains(a.statusMsg, "burn stopped") {
		t.Errorf("flash %q claims a burn stopped when none was firing", a.statusMsg)
	}
	if n := len(a.world.ActiveCraft().Nodes); n != 0 {
		t.Errorf("%d nodes left after clear-all", n)
	}
}

// Wave 1 known issue 5: ENGINE's node: row for an apsis node that cannot
// resolve says why, like the planner row does.
func TestEngineNodeRowSaysWhyAnApsisNodeWaits(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	c := a.world.ActiveCraft()
	// Exactly circular: speed = sqrt(mu/r), perpendicular to r.
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	perp := c.State.R.Unit().Cross(c.State.V).Cross(c.State.R.Unit()).Unit()
	c.State.V = perp.Scale(sqrtF(mu / r))
	a.world.PlanNode(sim.ManeuverNode{Event: sim.TriggerNextApo, Mode: spacecraft.BurnPrograde, DV: 300, PrimaryID: c.Primary.ID})
	triageTick(a, 2)
	if c.Nodes[0].IsResolved() {
		t.Fatal("setup: the node resolved on a circular orbit")
	}
	row := rowWith(a.View(), "node:")
	if !strings.Contains(row, "next apo (undefined)") || !strings.Contains(row, "Prograde") || !strings.Contains(row, "300 m/s") {
		t.Errorf("ENGINE node: row = %q, want `▸ next apo (undefined)  Prograde  300 m/s`", row)
	}
}

// Positive control for the row above: on an eccentric orbit the same node
// resolves and reads a countdown, never "(undefined)".
func TestEngineNodeRowResolvedApsisNodeHasNoUndefined(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	c := a.world.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	perp := c.State.R.Unit().Cross(c.State.V).Cross(c.State.R.Unit()).Unit()
	c.State.V = perp.Scale(1.05 * sqrtF(mu/r))
	a.world.PlanNode(sim.ManeuverNode{Event: sim.TriggerNextApo, Mode: spacecraft.BurnPrograde, DV: 300, PrimaryID: c.Primary.ID})
	triageTick(a, 2)
	row := rowWith(a.View(), "node:")
	if strings.Contains(row, "undefined") || !strings.Contains(row, " in ") {
		t.Errorf("ENGINE node: row = %q, want a countdown", row)
	}
}

func sqrtF(x float64) float64 {
	z := x
	for i := 0; i < 60; i++ {
		z = 0.5 * (z + x/z)
	}
	return z
}

func rowWith(frame, needle string) string {
	for _, l := range strings.Split(frame, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	return ""
}
