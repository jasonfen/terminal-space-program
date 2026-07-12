package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/save"
)

// testStateDirs isolates both the saves directory (XDG_STATE_HOME) and
// settings.json (XDG_CONFIG_HOME) into per-test temp roots, so the lane
// tests can't read or clobber the developer's real state, and returns
// the resolved saves/ dir.
func testStateDirs(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := save.SavesDir()
	if err != nil {
		t.Fatalf("SavesDir: %v", err)
	}
	return dir
}

// savesDirFiles returns the sorted .json basenames in the saves dir
// (empty when the dir doesn't exist yet).
func savesDirFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	return names
}

// TestQuicksaveKeyWritesLane — F5 writes the fixed quicksave lane
// (quicksave.json, ADR 0033 §D), never a named save, and flashes the
// ok toast.
func TestQuicksaveKeyWritesLane(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyF5})
	files := savesDirFiles(t, dir)
	if len(files) != 1 || files[0] != save.QuicksaveID {
		t.Fatalf("saves dir = %v, want exactly [%s]", files, save.QuicksaveID)
	}
	if !strings.HasPrefix(a.statusMsg, "save ok") {
		t.Errorf("statusMsg = %q, want a `save ok` toast", a.statusMsg)
	}
}

// TestQuickloadKeySwapsWorld — F9 loads the quicksave lane instantly
// (no confirm, ADR 0033 §H), replacing the world and re-applying the
// player's mission-program toggles (the v0.21 Slice 7 behaviour the
// rewire must preserve).
func TestQuickloadKeySwapsWorld(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyF5}) // quicksave at WarpIdx 0
	old := a.world
	a.world.Clock.WarpIdx = 4 // diverge so the swap is observable

	a.Update(tea.KeyMsg{Type: tea.KeyF9})
	if a.world == old {
		t.Fatal("F9 did not replace the world")
	}
	if a.world.Clock.WarpIdx != 0 {
		t.Errorf("WarpIdx = %d, want 0 (the quicksaved state)", a.world.Clock.WarpIdx)
	}
	// Settings default both program toggles off — a fresh Load yields the
	// nil "all enabled" map, so a reapplied (non-nil, empty) set is the
	// proof the toggles were pushed onto the loaded world.
	if a.world.MissionProgramEnabled(missions.ProgramTutorial) {
		t.Error("mission-program toggles not re-applied after quickload")
	}
	if a.active != screenOrbit {
		t.Errorf("active = %v, want screenOrbit after quickload", a.active)
	}
	if !strings.HasPrefix(a.statusMsg, "load ok") {
		t.Errorf("statusMsg = %q, want a `load ok` toast", a.statusMsg)
	}
}

// TestQuickloadEmptyLane — F9 before any F5 flashes a clear "no
// quicksave" message via flashStatus (not a raw file-not-found error)
// and leaves the live world untouched.
func TestQuickloadEmptyLane(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	old := a.world

	a.Update(tea.KeyMsg{Type: tea.KeyF9})
	if a.world != old {
		t.Fatal("F9 on an empty quicksave lane swapped the world")
	}
	if !strings.Contains(a.statusMsg, "no quicksave") {
		t.Errorf("statusMsg = %q, want a clear `no quicksave` message", a.statusMsg)
	}
	if strings.Contains(a.statusMsg, "no such file") {
		t.Errorf("statusMsg = %q leaks a raw file-not-found error", a.statusMsg)
	}
}

// TestQuickloadEmptyLanePointsToAutosave — finding 5. When there is no
// quicksave but an autosave IS on disk (the quit → relaunch → F9 case),
// F9 stays a no-op signpost — it does NOT auto-load, so a stray
// mid-session F9 can't silently discard live progress — and the message
// points the player at the Saves browser (Load) instead of dead-ending.
func TestQuickloadEmptyLanePointsToAutosave(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.autosave() // a quit-style autosave lands in the ring; no quicksave
	old := a.world

	a.Update(tea.KeyMsg{Type: tea.KeyF9})
	if a.world != old {
		t.Fatal("F9 auto-loaded the autosave — it must stay a no-op signpost, not swap the world")
	}
	if !strings.Contains(a.statusMsg, "no quicksave") {
		t.Errorf("statusMsg = %q, want a no-quicksave message", a.statusMsg)
	}
	if !strings.Contains(a.statusMsg, "Load") {
		t.Errorf("statusMsg = %q, want it to point at the Saves browser (Esc → Load)", a.statusMsg)
	}
}

// TestQuitAutosavesToRing — ctrl+c quit writes the rotating autosave
// ring (ADR 0033 §E), not a named save and not the legacy single-slot
// save.json.
func TestQuitAutosavesToRing(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	files := savesDirFiles(t, dir)
	if len(files) != 1 || files[0] != "autosave-1.json" {
		t.Fatalf("saves dir = %v, want exactly [autosave-1.json]", files)
	}
	legacyPath, err := save.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("quit wrote the legacy save.json (stat err = %v); autosave must use the ring", err)
	}
}
