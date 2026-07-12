package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// keyStr sends a printable key through Update the way the terminal
// would deliver it.
func press(a *App, s string) {
	switch s {
	case "enter":
		a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	case "esc":
		a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	case "down":
		a.Update(tea.KeyMsg{Type: tea.KeyDown})
	default:
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	}
}

// openSavesVia opens the pause menu and presses the given item key
// ("s" save-mode, "l" load-mode).
func openSavesVia(t *testing.T, a *App, item string) {
	t.Helper()
	press(a, "esc") // orbit → menu
	if a.active != screenMenu {
		t.Fatalf("esc did not open the menu (active=%v)", a.active)
	}
	press(a, item)
	if a.active != screenSaves {
		t.Fatalf("menu %q did not open the Saves screen (active=%v)", item, a.active)
	}
}

// TestMenuOpensSavesScreenBothModes — both pause-menu items open the
// unified Saves screen (ADR 0033 §F), with the entry point selecting
// save-mode vs load-mode. Replaces the S2 interim one-shot dispatch.
func TestMenuOpensSavesScreenBothModes(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	openSavesVia(t, a, "s")
	if a.saves.Mode() != screens.SavesModeSave {
		t.Errorf("[Save Game] opened mode %v, want save-mode", a.saves.Mode())
	}
	press(a, "esc") // back to orbit

	openSavesVia(t, a, "l")
	if a.saves.Mode() != screens.SavesModeLoad {
		t.Errorf("[Load Game] opened mode %v, want load-mode", a.saves.Mode())
	}
}

// TestSavesScreenSaveAsWritesNamed — the plan's "Save-As with the
// default name writes a named save; the returned action path hits
// WriteNamed": accepting the prefilled default mints exactly one
// named save whose Meta.Name is the default (active vessel +
// in-game day).
func TestSavesScreenSaveAsWritesNamed(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	openSavesVia(t, a, "s")
	press(a, "enter") // New save row → naming (prefilled default)
	press(a, "enter") // accept the default
	if a.active != screenOrbit {
		t.Fatalf("active = %v after Save-As, want screenOrbit", a.active)
	}

	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 || infos[0].Lane != save.LaneNamed {
		t.Fatalf("saves = %+v, want exactly one named save", infos)
	}
	want := defaultSaveName(a.world)
	if infos[0].Meta.Name != want {
		t.Errorf("Meta.Name = %q, want the default %q", infos[0].Meta.Name, want)
	}
	if !strings.Contains(a.statusMsg, "saved") {
		t.Errorf("statusMsg = %q, want a saved toast", a.statusMsg)
	}
}

// TestSavesScreenOverwritePreservesOthers — the plan's "Overwrite
// targets the selected file (not a name match) and preserves other
// saves": with two saves sharing a display name, overwriting the
// older one leaves the newer file untouched and mints nothing new.
func TestSavesScreenOverwritePreservesOthers(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	older, err := save.WriteNamed(a.world, "Twin")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	newer, err := save.WriteNamed(a.world, "Twin")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}

	openSavesVia(t, a, "s")
	// Rows: [＋ New save…, newer, older] — target the OLDER twin.
	press(a, "down")
	press(a, "down")
	press(a, "enter") // overwrite confirm
	press(a, "y")
	if a.active != screenOrbit {
		t.Fatalf("active = %v after overwrite, want screenOrbit", a.active)
	}

	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("saves after overwrite = %d files, want 2 (no new mint)", len(infos))
	}
	byID := map[string]save.Meta{}
	for _, in := range infos {
		byID[in.ID] = in.Meta
	}
	got, ok := byID[older.ID]
	if !ok {
		t.Fatalf("overwritten file %s vanished", older.ID)
	}
	if !got.SavedAt.After(older.Meta.SavedAt) {
		t.Errorf("overwrite did not refresh SavedAt (%v → %v)", older.Meta.SavedAt, got.SavedAt)
	}
	if got.Name != "Twin" {
		t.Errorf("overwrite lost the display name: %q", got.Name)
	}
	other, ok := byID[newer.ID]
	if !ok {
		t.Fatalf("sibling save %s vanished", newer.ID)
	}
	if !other.SavedAt.Equal(newer.Meta.SavedAt) {
		t.Errorf("sibling save was touched (SavedAt %v → %v)", newer.Meta.SavedAt, other.SavedAt)
	}
}

// TestSavesScreenRenameEditsMeta — the plan's "rename edits Meta on
// named rows": r + a new name rewrites the display name in place
// (same ID), and the browser refreshes to show it.
func TestSavesScreenRenameEditsMeta(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	info, err := save.WriteNamed(a.world, "Before")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}

	openSavesVia(t, a, "l")
	press(a, "r")
	// The input is prefilled with "Before"; type an addition and commit.
	press(a, "!")
	press(a, "enter")
	if a.active != screenSaves {
		t.Fatalf("active = %v after rename, want to stay on the Saves screen", a.active)
	}

	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != info.ID {
		t.Fatalf("saves after rename = %+v, want the same single file", infos)
	}
	if infos[0].Meta.Name != "Before!" {
		t.Errorf("Meta.Name = %q, want %q", infos[0].Meta.Name, "Before!")
	}
}

// TestSavesScreenDeleteRemovesRow — the plan's "delete confirm removes
// the row": d + y deletes the file and the refreshed list drops it.
func TestSavesScreenDeleteRemovesRow(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := save.WriteNamed(a.world, "Doomed"); err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}

	openSavesVia(t, a, "l")
	press(a, "d")
	press(a, "y")
	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("saves after delete = %+v, want none", infos)
	}
	if n := a.saves.EntryCount(); n != 0 {
		t.Errorf("saves screen shows %d rows after delete, want 0 (refresh)", n)
	}
}

// TestSavesScreenLoadConfirmGatesWorldSwap — the plan's "load path
// emits the confirm gate before the world swap": Enter alone must NOT
// swap the world; y commits the swap and re-applies the
// mission-program toggles via the shared doLoad path.
func TestSavesScreenLoadConfirmGatesWorldSwap(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := save.WriteNamed(a.world, "Checkpoint"); err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	old := a.world
	a.world.Clock.WarpIdx = 4 // diverge so the swap is observable

	openSavesVia(t, a, "l")
	press(a, "enter") // confirm gate — no swap yet
	if a.world != old {
		t.Fatal("Enter swapped the world before the confirm")
	}
	press(a, "y")
	if a.world == old {
		t.Fatal("confirmed load did not swap the world")
	}
	if a.world.Clock.WarpIdx != 0 {
		t.Errorf("WarpIdx = %d, want 0 (the saved state)", a.world.Clock.WarpIdx)
	}
	if a.active != screenOrbit {
		t.Errorf("active = %v after load, want screenOrbit", a.active)
	}
}
