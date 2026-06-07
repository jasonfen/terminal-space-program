package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// Clicking the navball panel's [MODE] button cycles NavMode and
// surfaces the same status toast the CycleNavMode key does.
func TestDispatchNavballControlMode(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.NavMode = sim.NavOrbit
	a.dispatchNavballControl(screens.NavballControlMode)
	if a.world.NavMode == sim.NavOrbit {
		t.Errorf("NavMode did not cycle off NavOrbit")
	}
	if a.statusMsg == "" {
		t.Errorf("expected a nav-mode status toast")
	}
}

// Clicking an axis button holds that SAS intent — same path as the
// keyboard, with NavMode rebinding applied via ResolveAttitudeIntent.
func TestDispatchNavballControlAxis(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.NavMode = sim.NavOrbit
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft on the spawn-state world")
	}
	// Force the main-engine path so the click sets the held attitude
	// rather than firing a one-off RCS pulse.
	c.EngineMode = spacecraft.EngineMain

	a.dispatchNavballControl(screens.NavballControlRadialOut)
	want := a.world.ResolveAttitudeIntent(sim.IntentRadialOut)
	if c.AttitudeMode != want {
		t.Errorf("AttitudeMode = %v, want %v (resolved radial-out)", c.AttitudeMode, want)
	}
}

// Clicking the RCS toggle flips EngineMode both ways and toasts.
func TestDispatchNavballControlRCS(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.EngineMode = spacecraft.EngineMain

	a.dispatchNavballControl(screens.NavballControlRCS)
	if c.EngineMode != spacecraft.EngineRCS {
		t.Errorf("EngineMode = %v, want EngineRCS after first toggle", c.EngineMode)
	}
	if a.statusMsg == "" {
		t.Errorf("expected an RCS status toast")
	}
	a.dispatchNavballControl(screens.NavballControlRCS)
	if c.EngineMode != spacecraft.EngineMain {
		t.Errorf("EngineMode = %v, want EngineMain after second toggle", c.EngineMode)
	}
}

// Clicking the [SAS] tag flips World.InstantSAS both ways and toasts
// the new model name — the locked-decision non-silent surfacing.
func TestDispatchNavballControlSAS(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.world.InstantSAS {
		t.Fatalf("InstantSAS should default false (slew is the v0.10 default)")
	}
	a.dispatchNavballControl(screens.NavballControlSAS)
	if !a.world.InstantSAS {
		t.Errorf("InstantSAS = false, want true after first toggle")
	}
	if a.statusMsg == "" {
		t.Errorf("expected a SAS-model status toast")
	}
	a.dispatchNavballControl(screens.NavballControlSAS)
	if a.world.InstantSAS {
		t.Errorf("InstantSAS = true, want false after second toggle")
	}
}

// Pressing a number-row key (1-9) on the orbit screen jumps to that
// craft slot; an empty slot is a no-op. Guards the digit-parse +
// binding wiring behind World.SwitchToCraftIdx.
func TestCraftSlotKeyJumps(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Stand up a 3-craft slate (New() starts with one).
	for i := 0; i < 2; i++ {
		if _, err := a.world.SpawnSisterCraft(); err != nil {
			t.Fatalf("SpawnSisterCraft: %v", err)
		}
	}
	if len(a.world.Crafts) != 3 {
		t.Fatalf("expected 3 craft, got %d", len(a.world.Crafts))
	}

	// '3' → craft index 2.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if a.world.ActiveCraftIdx != 2 {
		t.Errorf("after '3' = %d, want 2", a.world.ActiveCraftIdx)
	}
	// '9' → empty slot, no-op (stays on 2).
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	if a.world.ActiveCraftIdx != 2 {
		t.Errorf("after empty-slot '9' = %d, want 2 (no-op)", a.world.ActiveCraftIdx)
	}
	// '1' → back to craft index 0.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if a.world.ActiveCraftIdx != 0 {
		t.Errorf("after '1' = %d, want 0", a.world.ActiveCraftIdx)
	}
}

// Menu → Settings → toggle a chip → back round-trips through Update and
// persists the toggle to settings.json immediately (the slice-3
// persist-on-toggle decision). Drives the real keyboard dispatch so the
// menu-entry, screen-dispatch, write-through, and persist wiring are all
// exercised end to end.
func TestSettingsScreenRoundTripPersists(t *testing.T) {
	// Redirect settings.json into a temp dir so the test can't read or
	// clobber the developer's real config.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Open the menu, then press `t` to reach the Settings screen.
	a.menu.Reset()
	a.active = screenMenu
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if a.active != screenSettings {
		t.Fatalf("after menu `t`, active = %v, want screenSettings", a.active)
	}

	// The cursor opens on the first chip; it's enabled by default.
	first := settings.AllChips[0]
	if !a.orbitView.Settings().ChipEnabled(first) {
		t.Fatalf("%q should default enabled", first)
	}

	// Space toggles the highlighted chip off — in memory and on disk.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if a.orbitView.Settings().ChipEnabled(first) {
		t.Errorf("%q still enabled after toggle (in-memory)", first)
	}
	if reloaded, _ := settings.Load(); reloaded.ChipEnabled(first) {
		t.Errorf("%q still enabled after toggle (persisted)", first)
	}

	// Esc returns to orbit without losing the edit.
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.active != screenOrbit {
		t.Errorf("after esc, active = %v, want screenOrbit", a.active)
	}
	if a.orbitView.Settings().ChipEnabled(first) {
		t.Errorf("%q re-enabled after leaving the screen", first)
	}
}

// Clicking the target ± buttons holds BurnTarget / BurnAntiTarget.
func TestDispatchNavballControlTarget(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.EngineMode = spacecraft.EngineMain

	a.dispatchNavballControl(screens.NavballControlTargetPlus)
	if c.AttitudeMode != spacecraft.BurnTarget {
		t.Errorf("AttitudeMode = %v, want BurnTarget", c.AttitudeMode)
	}
	a.dispatchNavballControl(screens.NavballControlTargetMinus)
	if c.AttitudeMode != spacecraft.BurnAntiTarget {
		t.Errorf("AttitudeMode = %v, want BurnAntiTarget", c.AttitudeMode)
	}
}

// TestAutoWarpKeyToggleAndManualCancel — `G` engages Auto-Warp at the
// next burn and toggles it off; a manual warp keypress (`.`) also cancels
// an engaged driver, leaving Selected Warp to apply from the player's
// own rate (ADR 0016).
func TestAutoWarpKeyToggleAndManualCancel(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.PlanNode(sim.ManeuverNode{
		TriggerTime: a.world.Clock.SimTime.Add(2 * time.Hour),
		DV:          10,
		Mode:        spacecraft.BurnPrograde,
	})

	// `G` engages.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if !a.world.AutoWarpEngaged() {
		t.Fatal("G did not engage Auto-Warp")
	}
	// `G` again disengages.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if a.world.AutoWarpEngaged() {
		t.Fatal("second G did not disengage Auto-Warp")
	}

	// Re-engage, then a manual `.` cancels it.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if !a.world.AutoWarpEngaged() {
		t.Fatal("re-engage failed")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	if a.world.AutoWarpEngaged() {
		t.Error("manual warp `.` did not cancel Auto-Warp")
	}
}

// TestEditedNodeKeepsIDForAutoWarp — regression for ADR 0016: editing a
// planted node through the maneuver form (click → edit → Enter, a
// remove-then-replant) must carry the node's stable ID across the
// re-plant, so an engaged Auto-Warp target keeps resolving instead of
// silently disengaging. Pre-fix the re-plant minted a fresh ID.
func TestEditedNodeKeepsIDForAutoWarp(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.PlanNode(sim.ManeuverNode{
		TriggerTime: a.world.Clock.SimTime.Add(3 * time.Hour),
		DV:          50,
		Mode:        spacecraft.BurnPrograde,
	})
	origID := a.world.ActiveCraft().Nodes[0].ID
	if origID == 0 {
		t.Fatal("planted node has no ID")
	}
	if !a.world.EngageAutoWarp() {
		t.Fatal("engage failed")
	}

	// Simulate the form committing an edit of node 0 with a new trigger.
	a.Update(screens.BurnExecutedMsg{
		EditingIdx:  0,
		Mode:        spacecraft.BurnPrograde,
		DV:          80, // changed Δv
		TriggerTime: a.world.Clock.SimTime.Add(4 * time.Hour),
	})

	// The edited node must still carry origID, and Auto-Warp must still
	// be engaged and able to resolve it.
	found := false
	for _, n := range a.world.ActiveCraft().Nodes {
		if n.ID == origID {
			found = true
		}
	}
	if !found {
		t.Error("edited node lost its stable ID across the re-plant")
	}
	if !a.world.AutoWarpEngaged() {
		t.Error("Auto-Warp disengaged after a node edit")
	}
	a.world.Tick() // resolveAutoWarp must keep tracking the edited node
	if !a.world.AutoWarpEngaged() {
		t.Error("Auto-Warp disengaged on the tick after a node edit")
	}
}
