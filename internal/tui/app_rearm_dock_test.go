package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// buildRearmLatchApp docks two vessels and immediately undocks them,
// arming a same-World re-arm latch (#343) between the two restored craft
// — DockCrafts fuses regardless of proximity, and Undock's own separation
// push is what #372 says still leaves the pair inside the re-arm distance.
// Returns the app with the partner vessel targeted.
func buildRearmLatchApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(a.world.Crafts) != 2 {
		t.Fatalf("setup: expected 2 vessels after spawn, got %d", len(a.world.Crafts))
	}
	a.world.DockCrafts(0, 1)
	if len(a.world.Crafts) != 1 {
		t.Fatalf("setup: DockCrafts did not fuse the pair (slate has %d craft)", len(a.world.Crafts))
	}
	if !a.world.Undock(0) {
		t.Fatalf("setup: Undock refused on the freshly-fused composite")
	}
	if len(a.world.Crafts) != 2 {
		t.Fatalf("setup: expected 2 vessels after undock, got %d", len(a.world.Crafts))
	}
	a.world.SetTargetCraft(1)
	return a
}

// TestReArmDockKeyClearsLatch: pressing `c` with a latched, targeted
// partner clears the latch and chips the partner's name (#372 acceptance).
func TestReArmDockKeyClearsLatch(t *testing.T) {
	a := buildRearmLatchApp(t)
	partnerName := a.world.Crafts[1].Name

	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	if !strings.Contains(a.statusMsg, "re-armed") || !strings.Contains(a.statusMsg, partnerName) {
		t.Errorf("statusMsg = %q, want it to mention re-arming %q", a.statusMsg, partnerName)
	}
}

// TestReArmDockKeyNoLatchSaysSo: a press with nothing latched must report
// so rather than no-op silently — a dead keypress reads as a broken key.
func TestReArmDockKeyNoLatchSaysSo(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !strings.Contains(a.statusMsg, "no re-arm latch") {
		t.Errorf("statusMsg = %q, want it to say no latch is held", a.statusMsg)
	}
}

// TestReArmDockKeyInertWhileBossShellOpen is the #372 capturingText gate
// regression: a `c` typed while a text-capturing surface (the boss shell)
// is open must be consumed as text there, never leak through to
// ReArmDocking. Proven indirectly — since there is no exported latch
// inspector from this package — by pressing `c` while the boss shell is
// open, closing the shell, then pressing `c` for real: if the earlier
// press had already leaked through and cleared the latch, this second,
// genuine press would find nothing left to clear and report "no re-arm
// latch held" instead of the success chip.
func TestReArmDockKeyInertWhileBossShellOpen(t *testing.T) {
	a := buildRearmLatchApp(t)

	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'`'}})
	if a.active != screenBoss {
		t.Fatalf("setup: backtick did not open the boss shell (active=%v)", a.active)
	}

	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if a.active != screenBoss {
		t.Fatalf("'c' exited the boss shell (active=%v) — it should have been consumed as shell text", a.active)
	}

	pressMsg(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if a.active == screenBoss {
		t.Fatalf("setup: ctrl+d did not close the boss shell")
	}

	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !strings.Contains(a.statusMsg, "re-armed") {
		t.Errorf("statusMsg = %q after the real re-arm press — the latch looks like it was already cleared while the boss shell was capturing text", a.statusMsg)
	}
}
