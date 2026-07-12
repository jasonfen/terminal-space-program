package save_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
)

// TestImportLegacyIfNeeded — ADR 0033 §G. A legacy single-slot
// save.json with no saves/ directory imports exactly once into the
// named lane, with the default name derived from the IN-GAME date
// (Payload.SimTimeNano — not ClockT0, which is wall-clock save time),
// and the legacy file is left byte-identical as the downgrade safety
// net. A second startup probe is a no-op.
func TestImportLegacyIfNeeded(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	legacyPath, err := save.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if err := save.Save(w, legacyPath); err != nil {
		t.Fatalf("Save legacy fixture: %v", err)
	}
	legacyBefore, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("read legacy fixture: %v", err)
	}

	info, imported, err := save.ImportLegacyIfNeeded()
	if err != nil {
		t.Fatalf("ImportLegacyIfNeeded: %v", err)
	}
	if !imported {
		t.Fatal("expected the legacy save to import")
	}
	if info.Lane != save.LaneNamed {
		t.Errorf("Lane = %q, want %q", info.Lane, save.LaneNamed)
	}

	// The default name carries the in-game date — the world's SimTime,
	// not the wall-clock write time.
	wantDate := w.Clock.SimTime.UTC().Format("2006-01-02")
	if !strings.Contains(info.Meta.Name, wantDate) {
		t.Errorf("Name = %q, want it to contain the in-game date %q", info.Meta.Name, wantDate)
	}
	if got := info.Meta.InGameEpoch.UnixNano(); got != w.Clock.SimTime.UnixNano() {
		t.Errorf("InGameEpoch = %d, want SimTimeNano %d", got, w.Clock.SimTime.UnixNano())
	}
	if info.Meta.SavedAt.IsZero() {
		t.Error("SavedAt not stamped (should derive from the legacy ClockT0)")
	}
	if want := w.ActiveCraft().Name; info.Meta.ActiveVesselName != want {
		t.Errorf("ActiveVesselName = %q, want %q", info.Meta.ActiveVesselName, want)
	}

	// Exactly one named save landed in saves/, and it lists.
	if files := jsonFiles(t, dir); len(files) != 1 {
		t.Fatalf("saves dir = %v, want exactly the one imported file", files)
	}
	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != info.ID {
		t.Fatalf("List = %+v, want the single imported entry %q", infos, info.ID)
	}

	// The imported save fully loads back (payload carried through raw).
	loaded, err := save.LoadID(info.ID)
	if err != nil {
		t.Fatalf("LoadID(imported): %v", err)
	}
	if !loaded.Clock.SimTime.Equal(w.Clock.SimTime) {
		t.Errorf("loaded SimTime = %v, want %v", loaded.Clock.SimTime, w.Clock.SimTime)
	}

	// The legacy file is untouched — byte-identical (§G: never rewritten
	// or deleted; a downgraded binary still reads it).
	legacyAfter, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("re-read legacy: %v", err)
	}
	if !bytes.Equal(legacyBefore, legacyAfter) {
		t.Error("legacy save.json bytes changed — import must leave it untouched")
	}

	// Second startup: saves/ now exists, so the probe is a no-op even
	// though the legacy file is still present.
	if _, again, err := save.ImportLegacyIfNeeded(); err != nil {
		t.Fatalf("second ImportLegacyIfNeeded: %v", err)
	} else if again {
		t.Error("second probe re-imported — must be idempotent")
	}
	if files := jsonFiles(t, dir); len(files) != 1 {
		t.Errorf("saves dir after second probe = %v, want still one file", files)
	}
}

// TestImportLegacyIfNeededFreshInstall — no legacy save.json and no
// saves/: the probe is a silent no-op and does NOT create the saves
// directory (the first real write does that).
func TestImportLegacyIfNeededFreshInstall(t *testing.T) {
	dir := testSavesDir(t)

	if _, imported, err := save.ImportLegacyIfNeeded(); err != nil {
		t.Fatalf("ImportLegacyIfNeeded: %v", err)
	} else if imported {
		t.Error("imported on a fresh install with no legacy file")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("saves dir stat = %v, want not-exist (probe must not create it)", err)
	}
}

// TestImportRetriesWhenSavesDirExistsButNotSettled — regression for the
// finding-1 retry gate. A saves/ directory that exists for some OTHER
// reason (a quicksave/autosave wrote it, or a prior import failed after
// MkdirAll but before the imported file landed) must NOT block the
// migration: with the settled-marker absent, an existing saves/ dir
// still imports the legacy save. The old bare-Stat(saves/) gate hid the
// legacy save forever here.
func TestImportRetriesWhenSavesDirExistsButNotSettled(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	legacyPath, err := save.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if err := save.Save(w, legacyPath); err != nil {
		t.Fatalf("Save legacy fixture: %v", err)
	}
	// saves/ already present (a quicksave got there first) but the
	// migration never settled — no marker.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll saves: %v", err)
	}

	_, imported, err := save.ImportLegacyIfNeeded()
	if err != nil {
		t.Fatalf("ImportLegacyIfNeeded: %v", err)
	}
	if !imported {
		t.Fatal("did not import despite an existing-but-unsettled saves/ dir — the legacy save would be lost")
	}
	if files := jsonFiles(t, dir); len(files) != 1 {
		t.Errorf("saves dir = %v, want the one imported file", files)
	}
}

// TestImportMarkerBlocksReimport — once the migration has settled (the
// marker is written), the legacy save never re-imports, even if the
// player later deletes every file in saves/. The import is one-time;
// deleting the imported game must not resurrect it every launch.
func TestImportMarkerBlocksReimport(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	legacyPath, err := save.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if err := save.Save(w, legacyPath); err != nil {
		t.Fatalf("Save legacy fixture: %v", err)
	}

	info, imported, err := save.ImportLegacyIfNeeded()
	if err != nil || !imported {
		t.Fatalf("first import: imported=%v err=%v", imported, err)
	}
	// Player deletes the imported save.
	if err := save.Delete(info.ID); err != nil {
		t.Fatalf("Delete imported: %v", err)
	}

	if _, again, err := save.ImportLegacyIfNeeded(); err != nil {
		t.Fatalf("second ImportLegacyIfNeeded: %v", err)
	} else if again {
		t.Error("re-imported after the marker settled — the migration must be one-time")
	}
	if files := jsonFiles(t, dir); len(files) != 0 {
		t.Errorf("saves dir = %v, want empty (deleted, not resurrected)", files)
	}
}

// TestImportPreservesSavedAtOrdering — the imported entry's SavedAt
// derives from the legacy ClockT0 (wall-clock write time), so it
// sorts truthfully against newer saves rather than jumping to the top
// of the browser as if written now.
func TestImportPreservesSavedAtOrdering(t *testing.T) {
	testSavesDir(t)
	w := newTestWorld(t)

	legacyPath, err := save.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if err := save.Save(w, legacyPath); err != nil {
		t.Fatalf("Save legacy fixture: %v", err)
	}

	before := time.Now().Add(-time.Minute)
	info, imported, err := save.ImportLegacyIfNeeded()
	if err != nil || !imported {
		t.Fatalf("ImportLegacyIfNeeded: imported=%v err=%v", imported, err)
	}
	// The fixture was written moments ago, so its ClockT0-derived
	// SavedAt must land within the test's own wall-clock window.
	if info.Meta.SavedAt.Before(before) || info.Meta.SavedAt.After(time.Now().Add(time.Minute)) {
		t.Errorf("SavedAt = %v, want within the fixture's write window", info.Meta.SavedAt)
	}
}
