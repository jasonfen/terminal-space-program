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
	a, err := New(nil)
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
	a, err := New(nil)
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
	a, err := New(nil)
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
	a, err := New(nil)
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
	a, err := New(nil)
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

	a, err := New(nil)
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

// TestVABOpensFromMenuAndCloses — the pause menu `b` key opens the Vehicle
// Assembly screen (v0.24 / ADR 0029), its keys are consumed by the screen,
// and Esc returns to orbit.
func TestVABOpensFromMenuAndCloses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.menu.Reset()
	a.active = screenMenu
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if a.active != screenVAB {
		t.Fatalf("after menu `b`, active = %v, want screenVAB", a.active)
	}
	// A build key is consumed by the screen (does not fall through / crash).
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}) // new stage
	if a.active != screenVAB {
		t.Errorf("VAB build key leaked screen change: active = %v", a.active)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.active != screenOrbit {
		t.Errorf("after esc, active = %v, want screenOrbit", a.active)
	}
}

// Clicking the target ± buttons holds BurnTarget / BurnAntiTarget.
func TestDispatchNavballControlTarget(t *testing.T) {
	a, err := New(nil)
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
	a, err := New(nil)
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
	a, err := New(nil)
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

// TestCancelWarpKeyDropsToOneX — `/` cancels Auto-Warp and resets
// Selected Warp to 1× from any warp state.
func TestCancelWarpKeyDropsToOneX(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Manual warp up, no auto-warp → `/` drops to 1×.
	a.world.Clock.WarpIdx = 4
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if a.world.Clock.WarpIdx != 0 {
		t.Errorf("`/` left WarpIdx at %d, want 0 (1×)", a.world.Clock.WarpIdx)
	}

	// Auto-warp engaged → `/` cancels it and drops to 1×.
	a.world.PlanNode(sim.ManeuverNode{
		TriggerTime: a.world.Clock.SimTime.Add(2 * time.Hour),
		DV:          10,
		Mode:        spacecraft.BurnPrograde,
	})
	a.world.Clock.WarpIdx = 5
	if !a.world.EngageAutoWarp() {
		t.Fatal("engage failed")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if a.world.AutoWarpEngaged() {
		t.Error("`/` did not cancel Auto-Warp")
	}
	if a.world.Clock.WarpIdx != 0 {
		t.Errorf("`/` left WarpIdx at %d after cancelling auto-warp, want 0", a.world.Clock.WarpIdx)
	}
}

// TestYawKeysNudgePhi (ADR 0021 G): shift+← / shift+→ nudge ViewTilt.Phi
// ±5° with 360° wrap while in ViewTilted, flashing the resulting yaw —
// and stay silent (no mutation, no toast) in any other ViewMode,
// matching the Theta tilt keys' (shift+↑/↓) gating.
func TestYawKeysNudgePhi(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.world.ViewMode != sim.ViewTilted {
		t.Fatalf("expected the default ViewTilted, got %v", a.world.ViewMode)
	}

	// shift+→ yaws +5° and toasts the new value.
	a.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
	if a.world.ViewTilt.Phi != 5 {
		t.Errorf("after shift+→: Phi = %v, want 5", a.world.ViewTilt.Phi)
	}
	if a.statusMsg != "view: yaw 5°" {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, "view: yaw 5°")
	}

	// shift+← twice crosses zero and wraps to 355° — no clamp.
	a.Update(tea.KeyMsg{Type: tea.KeyShiftLeft})
	a.Update(tea.KeyMsg{Type: tea.KeyShiftLeft})
	if a.world.ViewTilt.Phi != 355 {
		t.Errorf("after shift+← shift+←: Phi = %v, want 355 (wrap below zero)", a.world.ViewTilt.Phi)
	}
	if a.statusMsg != "view: yaw 355°" {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, "view: yaw 355°")
	}

	// Outside ViewTilted the keys are a silent no-op.
	a.world.ViewMode = sim.ViewTop
	a.statusMsg = ""
	a.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
	if a.world.ViewTilt.Phi != 355 {
		t.Errorf("shift+→ in ViewTop mutated Phi to %v, want 355 (no-op)", a.world.ViewTilt.Phi)
	}
	if a.statusMsg != "" {
		t.Errorf("shift+→ in ViewTop flashed %q, want silence", a.statusMsg)
	}
}

// TestQuestionMarkOpensHelpPitchTrimResetOnPipe locks the #425 keybinding
// move: `?` — the reflex key everyone tries first — now opens the same
// help overlay F1 does, and does NOT touch pitch trim. Pitch-trim reset
// moved to `|` ("vertical bar = straight up").
func TestQuestionMarkOpensHelpPitchTrimResetOnPipe(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// F1 still toggles the help overlay.
	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	if a.active != screenHelp {
		t.Errorf("F1 did not open help (active=%v)", a.active)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	if a.active == screenHelp {
		t.Error("second F1 did not close help")
	}

	// `?` opens help too, and must NOT touch pitch trim.
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.PitchTrim = 0.3
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if a.active != screenHelp {
		t.Error("`?` did not open help")
	}
	if c.PitchTrim != 0.3 {
		t.Errorf("`?` touched pitch trim (PitchTrim=%v), want untouched 0.3", c.PitchTrim)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if a.active == screenHelp {
		t.Error("second `?` did not close help")
	}

	// `|` resets pitch trim and does NOT open help.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'|'}})
	if a.active == screenHelp {
		t.Error("`|` opened help; it should reset pitch trim instead")
	}
	if c.PitchTrim != 0 {
		t.Errorf("`|` did not reset pitch trim (PitchTrim=%v)", c.PitchTrim)
	}
}

// TestClickToEditGhostBoundNodePreservesBinding — #294 review round 3
// finding I. Click-to-edit on a reloaded ghost-targeted node used to
// silently strip its binding: LoadNode correctly captured the node's
// own target, but the app always followed it with bindManeuverTarget,
// which only knew TargetCraft — for a TargetGhost (or, as here, a
// LIVE target that no longer matches what the node was planted
// against) it clobbered the freshly-loaded binding down to none. This
// drives the exact production path (LoadNode, HandleKey's Enter →
// commitCmd → BurnExecutedMsg, App.Update's re-plant) and asserts the
// ghost ref survives both the FORM state and the REPLANTED node.
func TestClickToEditGhostBoundNodePreservesBinding(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	c.Nodes = append(c.Nodes, sim.ManeuverNode{
		Mode:             spacecraft.BurnTargetPrograde,
		DV:               42,
		TriggerTime:      a.world.Clock.SimTime.Add(time.Hour),
		PrimaryID:        c.Primary.ID,
		TargetGhostOwner: "SHA256:peer",
		TargetCraftID:    987654,
	})

	// The world's CURRENT live target is unrelated to the node being
	// edited — e.g. the give-up countdown already cleared it, or the
	// player simply hasn't retargeted since the node was planted.
	// bindManeuverTarget must never be consulted for an edit.
	a.world.ClearTarget()

	// Click-to-edit: LoadNode captures the node's OWN binding.
	a.maneuver.LoadNode(0, c.Nodes[0])

	cmd, ok := a.maneuver.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}, c.Nodes)
	if !ok || cmd == nil {
		t.Fatal("Enter did not commit")
	}
	raw := cmd()
	msg, ok := raw.(screens.BurnExecutedMsg)
	if !ok {
		t.Fatalf("commit produced %T, not BurnExecutedMsg", raw)
	}
	if msg.TargetCraftID != 987654 || msg.TargetGhostOwner != "SHA256:peer" {
		t.Fatalf("edit dropped the ghost binding from the form: TargetCraftID=%d TargetGhostOwner=%q",
			msg.TargetCraftID, msg.TargetGhostOwner)
	}

	// Apply the commit through the app, as the real Update loop would,
	// and confirm the REPLANTED node still carries the ghost ref — not
	// just the form's transient state.
	a.Update(msg)
	nodes := a.world.ActiveCraft().Nodes
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node after commit, got %d: %+v", len(nodes), nodes)
	}
	if nodes[0].TargetGhostOwner != "SHA256:peer" || nodes[0].TargetCraftID != 987654 {
		t.Errorf("replanted node lost its ghost binding: %+v", nodes[0])
	}
}

// TestClickToEditUntargetedNodeAfterGhostBoundLoadClearsBinding — #294
// second-round review finding 2. The Maneuver screen is one long-lived
// value reused across every plant/edit cycle (app.go): LoadNode used to
// set hasTargetCraft/targetCraftID/targetGhostOwner ONLY inside its
// `if id, ok := n.TargetCraftIDValue(); ok` branch, with no else. So
// loading a ghost-bound node, leaving it (Esc, in the real flow), then
// click-to-editing a later UNTARGETED node left the PRIOR node's
// binding stamped on the form: it still offered target-relative modes,
// and commitCmd would stamp the stale refs onto a node that was never
// targeted.
func TestClickToEditUntargetedNodeAfterGhostBoundLoadClearsBinding(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	c.Nodes = append(c.Nodes,
		sim.ManeuverNode{
			Mode:             spacecraft.BurnTargetPrograde,
			DV:               42,
			TriggerTime:      a.world.Clock.SimTime.Add(time.Hour),
			PrimaryID:        c.Primary.ID,
			TargetGhostOwner: "SHA256:peer",
			TargetCraftID:    987654,
		},
		sim.ManeuverNode{
			Mode:        spacecraft.BurnPrograde,
			DV:          10,
			TriggerTime: a.world.Clock.SimTime.Add(2 * time.Hour),
			PrimaryID:   c.Primary.ID,
			// no target binding
		},
	)

	// Open the ghost-bound node (as if clicked), leave it (Esc, in the
	// real flow), then click-to-edit the SECOND, untargeted node.
	a.maneuver.LoadNode(0, c.Nodes[0])
	a.maneuver.LoadNode(1, c.Nodes[1])

	// Cycle the mode field through every non-target-relative entry
	// (AllBurnModes lists the body-frame six first, the four target-
	// relative ones appended after). A clean binding must skip straight
	// past the target-relative block and wrap back to the start; a
	// binding leaked from node 0's earlier load lets it stop inside
	// that block instead (advanceMode's own skip-when-untargeted gate).
	nonTR := 0
	for _, md := range spacecraft.AllBurnModes {
		if !spacecraft.IsTargetRelativeMode(md) {
			nonTR++
		}
	}
	for i := 0; i < nonTR; i++ {
		if _, done := a.maneuver.HandleKey(tea.KeyMsg{Type: tea.KeyRight}, c.Nodes); done {
			t.Fatalf("right press %d unexpectedly ended the form", i)
		}
	}

	cmd, ok := a.maneuver.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}, c.Nodes)
	if !ok || cmd == nil {
		t.Fatal("Enter did not commit")
	}
	raw := cmd()
	msg, ok := raw.(screens.BurnExecutedMsg)
	if !ok {
		t.Fatalf("commit produced %T, not BurnExecutedMsg", raw)
	}
	if spacecraft.IsTargetRelativeMode(msg.Mode) {
		t.Errorf("mode cycling stopped on a target-relative mode (%v) for a node with no bound target — hasTargetCraft leaked from the earlier ghost-bound LoadNode", msg.Mode)
	}
	if msg.TargetCraftID != 0 || msg.TargetGhostOwner != "" {
		t.Errorf("stale target binding leaked onto an untargeted node's commit: TargetCraftID=%d TargetGhostOwner=%q",
			msg.TargetCraftID, msg.TargetGhostOwner)
	}
}

// TestEditedAdvisoryNudgeKeepsIdentityAndAutoWarpHonorsIt — #294
// second-round review finding 6. A K-planted rendezvous nudge
// (PlanRendezvousNudge) stamps AdvisoryKey and a target binding even on
// a plain velocity-frame mode (BurnPrograde here) — the binding records
// what the nudge was computed against, not a direction dependency.
// Before this fix, LoadNode faithfully loaded both, but commitCmd's
// target-binding stamp was gated on IsTargetRelativeMode (which
// BurnPrograde never is) and BurnExecutedMsg had no AdvisoryKey field
// at all — so editing the nudge (here: changing its Δv, which shifts
// the derived Duration and so BurnStart) silently re-planted it with
// NEITHER. This drives the exact production path (LoadNode → Enter →
// commitCmd → BurnExecutedMsg → App.Update's re-plant) and asserts the
// edited node keeps its AdvisoryKey and target binding, and that
// Auto-Warp Engage (which resolves by stable node ID, unaffected by
// AdvisoryKey, but proves the re-plant produced a coherent, still-
// eligible node) still finds and targets it.
func TestEditedAdvisoryNudgeKeepsIdentityAndAutoWarpHonorsIt(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	origTrigger := a.world.Clock.SimTime.Add(time.Hour)
	c.Nodes = append(c.Nodes, sim.ManeuverNode{
		Mode:             spacecraft.BurnPrograde, // velocity-frame axis, NOT target-relative
		DV:               20,
		TriggerTime:      origTrigger,
		PrimaryID:        c.Primary.ID,
		TargetCraftID:    c.ID, // stands in for the nudge's computed-against target
		TargetGhostOwner: "",
		AdvisoryKey:      sim.AdvisoryKeyRendezvousNudge,
	})
	origID := c.Nodes[0].ID
	if origID == 0 {
		// EnsureNodeIDs runs at world construction, not on manual
		// slice appends — stamp one here so AutoWarp has an ID to match.
		a.world.EnsureNodeIDs()
		origID = c.Nodes[0].ID
	}

	// Click-to-edit, then change the Δv (the form's actual editable
	// numeric field) — a real edit, not a no-op re-commit.
	a.maneuver.LoadNode(0, c.Nodes[0])
	// LoadNode focuses the mode field (0); tab twice to reach the Δv
	// input (2), clear the loaded "20", and type a genuinely different
	// value.
	a.maneuver.HandleKey(tea.KeyMsg{Type: tea.KeyTab}, c.Nodes)
	a.maneuver.HandleKey(tea.KeyMsg{Type: tea.KeyTab}, c.Nodes)
	for i := 0; i < 4; i++ {
		a.maneuver.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace}, c.Nodes)
	}
	for _, r := range "999" {
		a.maneuver.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}, c.Nodes)
	}

	cmd2, ok2 := a.maneuver.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}, c.Nodes)
	if !ok2 || cmd2 == nil {
		t.Fatal("Enter did not commit")
	}
	raw := cmd2()
	msg, ok := raw.(screens.BurnExecutedMsg)
	if !ok {
		t.Fatalf("commit produced %T, not BurnExecutedMsg", raw)
	}
	if msg.AdvisoryKey != sim.AdvisoryKeyRendezvousNudge {
		t.Errorf("AdvisoryKey dropped by the edit: got %q, want %q", msg.AdvisoryKey, sim.AdvisoryKeyRendezvousNudge)
	}
	if msg.TargetCraftID != c.ID {
		t.Errorf("target binding dropped by the edit on a non-target-relative mode: TargetCraftID=%d, want %d", msg.TargetCraftID, c.ID)
	}

	a.Update(msg)
	nodes := a.world.ActiveCraft().Nodes
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node after commit, got %d: %+v", len(nodes), nodes)
	}
	edited := nodes[0]
	if edited.AdvisoryKey != sim.AdvisoryKeyRendezvousNudge {
		t.Errorf("replanted node lost its AdvisoryKey: %+v", edited)
	}
	if edited.TargetCraftID != c.ID {
		t.Errorf("replanted node lost its target binding: %+v", edited)
	}
	if edited.ID != origID {
		t.Fatalf("replanted node did not keep its stable ID: got %d, want %d", edited.ID, origID)
	}
	if edited.DV != 999 {
		t.Fatalf("edit did not take — DV = %v, want 999 (setup sanity check)", edited.DV)
	}

	// Auto-Warp Engage must still find and target this exact node —
	// the edit must not have produced anything soonestEligibleBurn
	// can't resolve.
	if !a.world.EngageAutoWarp() {
		t.Fatal("EngageAutoWarp found no eligible burn after the edit")
	}
	if a.world.AutoWarp == nil || a.world.AutoWarp.CraftID != c.ID || a.world.AutoWarp.NodeID != origID {
		t.Errorf("AutoWarp did not target the edited nudge: %+v (want CraftID=%d NodeID=%d)", a.world.AutoWarp, c.ID, origID)
	}
}

