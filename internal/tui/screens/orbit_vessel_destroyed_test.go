package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestVesselDestroyedChipAppearsOnCrashAndSurvivesDeclutter is #427's
// Standing Alert test: VESSEL DESTROYED renders the moment the active
// craft is Crashed, names both exits, and — like the live-burn NODES
// chip (TestOrbitMetricsAlwaysOnAndLiveBurnForceShows) — survives BOTH
// every Chip toggled off AND F2 Declutter, because it bypasses
// chipEnabled entirely rather than merely defaulting on.
func TestVesselDestroyedChipAppearsOnCrashAndSurvivesDeclutter(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}

	// A live (non-crashed) vessel must not show the alert.
	out := v.Render(w, 0, 120, 40)
	if strings.Contains(out, "VESSEL DESTROYED") {
		t.Fatalf("a live vessel must not show VESSEL DESTROYED:\n%s", out)
	}

	// Disable every toggleable Chip AND declutter — the same setup the
	// NODES force-show test uses — to prove this doesn't merely default
	// on, it actually bypasses both gates.
	s := settings.Default()
	for _, chipID := range settings.AllChips {
		s.SetChip(chipID, false)
	}
	v.SetSettings(s)
	v.SetDeclutter(true)

	c.Crashed = true
	out = v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "VESSEL DESTROYED") {
		t.Errorf("a crashed vessel must show VESSEL DESTROYED even with every chip disabled and declutter on:\n%s", out)
	}
	if !strings.Contains(out, "[E] end flight") || !strings.Contains(out, "[F9] quickload") {
		t.Errorf("VESSEL DESTROYED must name both exits ([E] end flight, [F9] quickload):\n%s", out)
	}

	// End Flight clears Crashed → the alert must clear with it (it names
	// a currently-true state, not a one-shot notice).
	c.Crashed = false
	out = v.Render(w, 0, 120, 40)
	if strings.Contains(out, "VESSEL DESTROYED") {
		t.Errorf("VESSEL DESTROYED lingered after Crashed cleared:\n%s", out)
	}
}

// TestBuildVesselDestroyedChipNilWhenNotVisibleHere: the builder itself
// (unit-level, no render) must return nil rather than a stale alert when
// the active craft isn't in the camera's current system — mirrors
// buildVesselChip's own CraftVisibleHere gate.
func TestBuildVesselDestroyedChipNilWhenNotVisibleHere(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.Crashed = true
	c.SystemIdx = w.SystemIdx + 1 // parked in a different system than the camera

	if lines := v.buildVesselDestroyedChip(w); lines != nil {
		t.Errorf("buildVesselDestroyedChip returned %v for a craft not visible here, want nil", lines)
	}
}
