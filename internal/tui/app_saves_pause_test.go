package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// TestSavesScreenFreezesSim — finding 4. The pause menu keeps the sim
// running, so opening the Saves browser must freeze the clock: a Save-As
// captured a world that drifted (or crashed) while the player typed a
// name otherwise. Leaving via cancel / Save-As / overwrite restores the
// pause state the sim had before it opened (mirroring the boss key).
func TestSavesScreenFreezesSim(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.world.Clock.Paused {
		t.Fatal("world started paused — test premise broken")
	}

	a.openSaves(screens.SavesModeSave)
	if !a.world.Clock.Paused {
		t.Error("opening the Saves browser did not freeze the sim (finding 4)")
	}

	// Cancel restores the prior (running) state and returns to orbit.
	a.applySavesCommand(screens.SavesCommand{Kind: screens.SavesActionCancel})
	if a.world.Clock.Paused {
		t.Error("leaving the Saves browser did not restore the running clock")
	}
	if a.active != screenOrbit {
		t.Errorf("active = %v, want screenOrbit after cancel", a.active)
	}
}

// TestSavesScreenRestoresPriorPause — if the sim was ALREADY paused when
// the browser opened (e.g. opened from a maneuver-planning pause), exit
// must leave it paused, not force it running.
func TestSavesScreenRestoresPriorPause(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Clock.Paused = true

	a.openSaves(screens.SavesModeLoad)
	if !a.world.Clock.Paused {
		t.Fatal("browser unfroze an already-paused sim")
	}
	a.applySavesCommand(screens.SavesCommand{Kind: screens.SavesActionCancel})
	if !a.world.Clock.Paused {
		t.Error("exit forced the clock to run despite a prior paused state")
	}
}
