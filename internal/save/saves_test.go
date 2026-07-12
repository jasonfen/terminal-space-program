package save_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// testSavesDir points SavesDir at a per-test temp root via
// XDG_STATE_HOME and returns the resolved saves/ directory. The body
// catalog resolves through XDG_CONFIG_HOME, so redirecting state does
// not perturb the catalog hash the writers stamp.
func testSavesDir(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, err := save.SavesDir()
	if err != nil {
		t.Fatalf("SavesDir: %v", err)
	}
	return dir
}

func newTestWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}

// jsonFiles returns the sorted .json basenames present in dir
// (ignoring tmpfiles and anything else).
func jsonFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		names = append(names, e.Name())
	}
	return names
}

// compactPayload extracts and compacts the raw payload bytes of the
// envelope at path, for byte-level does-not-touch-the-World assertions.
func compactPayload(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var f struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, f.Payload); err != nil {
		t.Fatalf("compact payload: %v", err)
	}
	return buf.Bytes()
}

// TestWriteNamedListLoadRoundtrip — ADR 0033 §B/§C. WriteNamed mints an
// opaque file, List surfaces it with the stamped Meta, and LoadID
// rehydrates an equivalent World.
func TestWriteNamedListLoadRoundtrip(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)
	w.Clock.SimTime = w.Clock.SimTime.Add(42 * time.Hour)

	info, err := save.WriteNamed(w, "Apollo — pre-TLI")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	if info.ID == "" || info.ID != filepath.Base(info.ID) {
		t.Fatalf("WriteNamed ID = %q, want a bare filename", info.ID)
	}
	if info.Lane != save.LaneNamed {
		t.Errorf("Lane = %q, want %q", info.Lane, save.LaneNamed)
	}
	if got := jsonFiles(t, dir); len(got) != 1 || got[0] != info.ID {
		t.Fatalf("saves dir = %v, want exactly [%s]", got, info.ID)
	}

	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("List = %d entries, want 1", len(infos))
	}
	m := infos[0].Meta
	if m.Name != "Apollo — pre-TLI" {
		t.Errorf("Meta.Name = %q", m.Name)
	}
	if m.SavedAt.IsZero() {
		t.Error("Meta.SavedAt is zero, want stamped wall-clock time")
	}
	if !m.InGameEpoch.Equal(w.Clock.SimTime) {
		t.Errorf("Meta.InGameEpoch = %v, want %v", m.InGameEpoch, w.Clock.SimTime)
	}
	if m.ActiveVesselName != w.ActiveCraft().Name {
		t.Errorf("Meta.ActiveVesselName = %q, want %q", m.ActiveVesselName, w.ActiveCraft().Name)
	}
	if m.SystemName == "" {
		t.Error("Meta.SystemName empty, want the active vessel's system")
	}

	got, err := save.LoadID(info.ID)
	if err != nil {
		t.Fatalf("LoadID: %v", err)
	}
	if !got.Clock.SimTime.Equal(w.Clock.SimTime) {
		t.Errorf("SimTime: got %v want %v", got.Clock.SimTime, w.Clock.SimTime)
	}
	if got.ActiveCraft().Name != w.ActiveCraft().Name {
		t.Errorf("craft name: got %q want %q", got.ActiveCraft().Name, w.ActiveCraft().Name)
	}
	if !vecEq(got.ActiveCraft().State.R, w.ActiveCraft().State.R) {
		t.Errorf("craft R: got %v want %v", got.ActiveCraft().State.R, w.ActiveCraft().State.R)
	}
}

// TestReadHeaderSkipsCatalogAndPayload — ADR 0033 §C. A header read
// must not hit the body catalog (no ErrCatalogMismatch path) and must
// not hydrate the payload: a file whose hash is bogus and whose
// payload Load would reject still header-reads cleanly.
func TestReadHeaderSkipsCatalogAndPayload(t *testing.T) {
	dir := testSavesDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "handmade.json")
	raw := `{
  "version": 9,
  "generator": "test",
  "clock_t0": 1234567890,
  "body_catalog_hash": "bogus-hash",
  "meta": {
    "name": "Handmade",
    "saved_at": "2026-07-01T12:00:00Z",
    "in_game_epoch": "1971-01-01T00:00:00Z",
    "active_vessel_name": "Apollo",
    "system_name": "Sol"
  },
  "payload": {"system_idx": 9999}
}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := save.ReadHeader(path)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if m.Name != "Handmade" || m.ActiveVesselName != "Apollo" || m.SystemName != "Sol" {
		t.Errorf("Meta = %+v", m)
	}
	// SavedAt derives from the envelope clock_t0 (the single source of
	// truth), NOT from any persisted saved_at key — the "saved_at":
	// "2026-07-01…" above is an ignored legacy key and must NOT win.
	if want := time.Unix(0, 1234567890).UTC(); !m.SavedAt.Equal(want) {
		t.Errorf("SavedAt = %v, want ClockT0-derived %v (saved_at key must be ignored)", m.SavedAt, want)
	}

	// Sanity: the same file is rejected by the full Load path on the
	// bogus hash — proving ReadHeader really skipped that check.
	if _, err := save.Load(path); !errors.Is(err, save.ErrCatalogMismatch) {
		t.Errorf("Load err = %v, want ErrCatalogMismatch", err)
	}
}

// TestMetaLessV9Lists — ADR 0033 §J backward compat. A v9 envelope
// written before Meta existed lists with SavedAt derived from ClockT0
// (wall-clock save time — NOT zero, so it must not sort to the
// bottom), an empty name, an empty in-game date (unknowable from the
// header), and still LoadIDs.
func TestMetaLessV9Lists(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	// An older named save first, then the Meta-less file: the
	// Meta-less one is newer by ClockT0 and must list first.
	if _, err := save.WriteNamed(w, "older"); err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	// save.Save is the legacy single-slot writer — it stamps no Meta,
	// which is exactly the pre-v0.26 v9 envelope shape.
	if err := save.Save(w, filepath.Join(dir, "legacy.json")); err != nil {
		t.Fatalf("Save (legacy shape): %v", err)
	}

	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("List = %d entries, want 2", len(infos))
	}
	first := infos[0]
	if first.ID != "legacy.json" {
		t.Fatalf("newest entry = %q, want legacy.json first (ClockT0-derived SavedAt must not sort to the bottom)", first.ID)
	}
	if first.Meta.SavedAt.IsZero() {
		t.Error("Meta-less SavedAt is zero, want ClockT0-derived")
	}
	if first.Meta.Name != "" {
		t.Errorf("Meta-less Name = %q, want empty", first.Meta.Name)
	}
	if !first.Meta.InGameEpoch.IsZero() {
		t.Errorf("Meta-less InGameEpoch = %v, want zero (unknowable from the header)", first.Meta.InGameEpoch)
	}
	if first.Lane != save.LaneNamed {
		t.Errorf("Lane = %q, want %q", first.Lane, save.LaneNamed)
	}

	if _, err := save.LoadID("legacy.json"); err != nil {
		t.Errorf("LoadID(legacy.json): %v", err)
	}
}

// TestAutosaveRingRotation — ADR 0033 §E. Four successive autosaves
// leave exactly the 3 ring files, with the first (oldest) write gone.
func TestAutosaveRingRotation(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	var stamps []time.Time
	for i := 0; i < 4; i++ {
		if err := save.WriteAutosave(w); err != nil {
			t.Fatalf("WriteAutosave #%d: %v", i+1, err)
		}
		// Record the newest SavedAt in the ring after each write.
		newest := time.Time{}
		for _, id := range jsonFiles(t, dir) {
			m, err := save.ReadHeader(filepath.Join(dir, id))
			if err != nil {
				t.Fatalf("ReadHeader(%s): %v", id, err)
			}
			if m.SavedAt.After(newest) {
				newest = m.SavedAt
			}
		}
		stamps = append(stamps, newest)
		time.Sleep(5 * time.Millisecond)
	}

	files := jsonFiles(t, dir)
	if len(files) != 3 {
		t.Fatalf("ring = %v, want exactly 3 files", files)
	}
	for _, id := range files {
		info, err := save.ReadHeader(filepath.Join(dir, id))
		if err != nil {
			t.Fatalf("ReadHeader(%s): %v", id, err)
		}
		if info.SavedAt.Equal(stamps[0]) {
			t.Errorf("%s still carries the first write's SavedAt %v — oldest should have been rotated out", id, stamps[0])
		}
		if info.Name != "" {
			t.Errorf("%s Meta.Name = %q, want empty (reserved lanes carry no player name)", id, info.Name)
		}
		if info.ActiveVesselName == "" || info.SystemName == "" || info.InGameEpoch.IsZero() {
			t.Errorf("%s Meta = %+v, want full metadata stamped on the reserved lane", id, info)
		}
	}
}

// TestAutosaveRingFillsEmptySlot — with only 2 ring files present, the
// next write fills the missing slot rather than rotating over a
// survivor.
func TestAutosaveRingFillsEmptySlot(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	for i := 0; i < 3; i++ {
		if err := save.WriteAutosave(w); err != nil {
			t.Fatalf("WriteAutosave #%d: %v", i+1, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	victim := filepath.Join(dir, "autosave-2.json")
	if err := os.Remove(victim); err != nil {
		t.Fatalf("remove ring slot: %v", err)
	}
	survivors := map[string]time.Time{}
	for _, id := range jsonFiles(t, dir) {
		m, err := save.ReadHeader(filepath.Join(dir, id))
		if err != nil {
			t.Fatal(err)
		}
		survivors[id] = m.SavedAt
	}

	if err := save.WriteAutosave(w); err != nil {
		t.Fatalf("WriteAutosave (fill): %v", err)
	}
	files := jsonFiles(t, dir)
	if len(files) != 3 {
		t.Fatalf("ring = %v, want 3 files after fill", files)
	}
	refilled, err := save.ReadHeader(victim)
	if err != nil {
		t.Fatalf("ReadHeader(refilled slot): %v", err)
	}
	for id, at := range survivors {
		m, err := save.ReadHeader(filepath.Join(dir, id))
		if err != nil {
			t.Fatal(err)
		}
		if !m.SavedAt.Equal(at) {
			t.Errorf("%s SavedAt changed %v → %v — fill must not rotate a survivor", id, at, m.SavedAt)
		}
		if !refilled.SavedAt.After(m.SavedAt) {
			t.Errorf("refilled slot SavedAt %v not newer than survivor %s (%v)", refilled.SavedAt, id, m.SavedAt)
		}
	}
}

// TestAutosaveRingUnreadableCountsOldest — a corrupt or Meta-less ring
// file is the first rotation victim.
func TestAutosaveRingUnreadableCountsOldest(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	for i := 0; i < 3; i++ {
		if err := save.WriteAutosave(w); err != nil {
			t.Fatalf("WriteAutosave #%d: %v", i+1, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	corrupt := filepath.Join(dir, "autosave-2.json")
	if err := os.WriteFile(corrupt, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := map[string]time.Time{}
	for _, id := range []string{"autosave-1.json", "autosave-3.json"} {
		m, err := save.ReadHeader(filepath.Join(dir, id))
		if err != nil {
			t.Fatal(err)
		}
		before[id] = m.SavedAt
	}

	if err := save.WriteAutosave(w); err != nil {
		t.Fatalf("WriteAutosave (over corrupt): %v", err)
	}
	if _, err := save.ReadHeader(corrupt); err != nil {
		t.Errorf("corrupt slot not overwritten with a valid save: %v", err)
	}
	for id, at := range before {
		m, err := save.ReadHeader(filepath.Join(dir, id))
		if err != nil {
			t.Fatal(err)
		}
		if !m.SavedAt.Equal(at) {
			t.Errorf("%s was rotated (%v → %v) — the unreadable slot should have been the victim", id, at, m.SavedAt)
		}
	}
}

// TestQuicksaveLane — ADR 0033 §D. F5's target is the fixed
// quicksave.json, stamped with full Meta minus a player name.
func TestQuicksaveLane(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	if err := save.WriteQuicksave(w); err != nil {
		t.Fatalf("WriteQuicksave: %v", err)
	}
	if got := jsonFiles(t, dir); len(got) != 1 || got[0] != save.QuicksaveID {
		t.Fatalf("saves dir = %v, want exactly [%s]", got, save.QuicksaveID)
	}
	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 || infos[0].Lane != save.LaneQuicksave {
		t.Fatalf("List = %+v, want one quicksave-lane entry", infos)
	}
	m := infos[0].Meta
	if m.Name != "" {
		t.Errorf("quicksave Meta.Name = %q, want empty", m.Name)
	}
	if m.SavedAt.IsZero() || m.InGameEpoch.IsZero() || m.ActiveVesselName == "" || m.SystemName == "" {
		t.Errorf("quicksave Meta = %+v, want full metadata", m)
	}
	// A second quicksave overwrites in place — still one file.
	if err := save.WriteQuicksave(w); err != nil {
		t.Fatalf("WriteQuicksave #2: %v", err)
	}
	if got := jsonFiles(t, dir); len(got) != 1 {
		t.Errorf("saves dir = %v, want quicksave overwritten in place", got)
	}
}

// TestRenameMetaOnly — ADR 0033 §B. Rename is a pure Meta rewrite:
// same filename, payload bytes semantically untouched, SavedAt
// preserved (a rename must not reorder the list).
func TestRenameMetaOnly(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	info, err := save.WriteNamed(w, "before")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	path := filepath.Join(dir, info.ID)
	payloadBefore := compactPayload(t, path)
	metaBefore, err := save.ReadHeader(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := save.Rename(info.ID, "after"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if got := jsonFiles(t, dir); len(got) != 1 || got[0] != info.ID {
		t.Fatalf("saves dir = %v, want the same single filename %s (no file move)", got, info.ID)
	}
	m, err := save.ReadHeader(path)
	if err != nil {
		t.Fatalf("ReadHeader after rename: %v", err)
	}
	if m.Name != "after" {
		t.Errorf("Name = %q, want %q", m.Name, "after")
	}
	if !m.SavedAt.Equal(metaBefore.SavedAt) {
		t.Errorf("SavedAt changed %v → %v — rename must not reorder", metaBefore.SavedAt, m.SavedAt)
	}
	if !bytes.Equal(compactPayload(t, path), payloadBefore) {
		t.Error("payload bytes changed across Rename — must be Meta-only")
	}
}

// TestReservedLaneInvariant — ADR 0033 §D/§F. Rename and Overwrite
// refuse the reserved quicksave/autosave filenames.
func TestReservedLaneInvariant(t *testing.T) {
	testSavesDir(t)
	w := newTestWorld(t)
	if err := save.WriteQuicksave(w); err != nil {
		t.Fatalf("WriteQuicksave: %v", err)
	}
	if err := save.WriteAutosave(w); err != nil {
		t.Fatalf("WriteAutosave: %v", err)
	}

	reserved := []string{save.QuicksaveID, "autosave-1.json", "autosave-2.json", "autosave-3.json"}
	for _, id := range reserved {
		if err := save.Rename(id, "hijack"); !errors.Is(err, save.ErrReservedLane) {
			t.Errorf("Rename(%s) err = %v, want ErrReservedLane", id, err)
		}
		if err := save.Overwrite(id, w); !errors.Is(err, save.ErrReservedLane) {
			t.Errorf("Overwrite(%s) err = %v, want ErrReservedLane", id, err)
		}
	}
}

// TestOverwriteRefreshesMeta — Overwrite rewrites the targeted file in
// place, preserving Meta.Name while refreshing the volatile fields.
func TestOverwriteRefreshesMeta(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	info, err := save.WriteNamed(w, "keep-this-name")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	before, err := save.ReadHeader(filepath.Join(dir, info.ID))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)
	w.Clock.SimTime = w.Clock.SimTime.Add(99 * time.Hour)
	if err := save.Overwrite(info.ID, w); err != nil {
		t.Fatalf("Overwrite: %v", err)
	}

	if got := jsonFiles(t, dir); len(got) != 1 || got[0] != info.ID {
		t.Fatalf("saves dir = %v, want the single overwritten file", got)
	}
	m, err := save.ReadHeader(filepath.Join(dir, info.ID))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "keep-this-name" {
		t.Errorf("Name = %q, want preserved %q", m.Name, "keep-this-name")
	}
	if !m.SavedAt.After(before.SavedAt) {
		t.Errorf("SavedAt = %v, want refreshed past %v", m.SavedAt, before.SavedAt)
	}
	if !m.InGameEpoch.Equal(w.Clock.SimTime) {
		t.Errorf("InGameEpoch = %v, want refreshed to %v", m.InGameEpoch, w.Clock.SimTime)
	}

	// A missing target is an error, not a silent create.
	if err := save.Overwrite("save-0-0.json", w); err == nil {
		t.Error("Overwrite of a nonexistent id succeeded, want error")
	}
}

// TestDuplicateNamesTwoFiles — ADR 0033 §B. Save-As is always-new:
// two WriteNamed with the same display name mint two distinct files,
// told apart by SavedAt.
func TestDuplicateNamesTwoFiles(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	a, err := save.WriteNamed(w, "same name")
	if err != nil {
		t.Fatalf("WriteNamed #1: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	b, err := save.WriteNamed(w, "same name")
	if err != nil {
		t.Fatalf("WriteNamed #2: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("both writes minted %q — duplicate names must not collapse to one file", a.ID)
	}
	if got := jsonFiles(t, dir); len(got) != 2 {
		t.Fatalf("saves dir = %v, want 2 files", got)
	}
	if !b.Meta.SavedAt.After(a.Meta.SavedAt) {
		t.Errorf("SavedAt not ordered: a=%v b=%v", a.Meta.SavedAt, b.Meta.SavedAt)
	}
	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 || infos[0].ID != b.ID || infos[1].ID != a.ID {
		t.Errorf("List order = %+v, want newest (%s) first", infos, b.ID)
	}
}

// TestDeleteRemovesTargetedFile — Delete removes exactly the targeted
// save and nothing else.
func TestDeleteRemovesTargetedFile(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	a, err := save.WriteNamed(w, "goner")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	b, err := save.WriteNamed(w, "survivor")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}

	if err := save.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := jsonFiles(t, dir); len(got) != 1 || got[0] != b.ID {
		t.Fatalf("saves dir = %v, want exactly [%s]", got, b.ID)
	}
}

// TestSavedAtNotPersisted — the wall-clock write time lives ONLY in the
// envelope's clock_t0; Meta must not also persist a saved_at key (the
// pre-release duplication that finding 10 removed). List still surfaces
// a non-zero SavedAt because it derives from clock_t0 on read.
func TestSavedAtNotPersisted(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	info, err := save.WriteNamed(w, "no dup")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	// The returned entry carries the derived SavedAt (from the ClockT0
	// just written), so callers still see a real time.
	if info.Meta.SavedAt.IsZero() {
		t.Error("returned SavedAt is zero, want the ClockT0-derived write time")
	}

	raw, err := os.ReadFile(filepath.Join(dir, info.ID))
	if err != nil {
		t.Fatalf("read save: %v", err)
	}
	if bytes.Contains(raw, []byte("saved_at")) {
		t.Errorf("save JSON persists a saved_at key — must be derived from clock_t0 only:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("clock_t0")) {
		t.Errorf("save JSON missing clock_t0 (the wall-clock source of truth):\n%s", raw)
	}

	// And List recomputes the same SavedAt from clock_t0 on read.
	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 || infos[0].Meta.SavedAt.IsZero() {
		t.Errorf("List SavedAt not derived from clock_t0: %+v", infos)
	}
}

// TestListSurfacesUnreadable — finding 6. A corrupt .json and a
// newer-build (schema v+1) save are LISTED as flagged, non-loadable
// entries rather than silently dropped: a directory that plainly holds
// files must never render as "(no saves yet)", and a newer-version save
// must stay visible on a downgrade. Valid saves list normally alongside.
func TestListSurfacesUnreadable(t *testing.T) {
	dir := testSavesDir(t)
	w := newTestWorld(t)

	good, err := save.WriteNamed(w, "readable")
	if err != nil {
		t.Fatalf("WriteNamed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "save-corrupt.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	newer := `{"version": 999, "generator": "future", "clock_t0": 1, "payload": {}}`
	if err := os.WriteFile(filepath.Join(dir, "save-newer.json"), []byte(newer), 0o644); err != nil {
		t.Fatal(err)
	}

	infos, err := save.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 3 {
		t.Fatalf("List = %d entries, want 3 (1 valid + 2 flagged)", len(infos))
	}

	byID := map[string]save.SaveInfo{}
	for _, in := range infos {
		byID[in.ID] = in
	}
	if g := byID[good.ID]; g.Unreadable {
		t.Errorf("valid save flagged unreadable: %+v", g)
	}
	corrupt, ok := byID["save-corrupt.json"]
	if !ok || !corrupt.Unreadable || corrupt.Note != "corrupt" {
		t.Errorf("corrupt entry = %+v, want Unreadable with Note=corrupt", corrupt)
	}
	future, ok := byID["save-newer.json"]
	if !ok || !future.Unreadable || future.Note != "newer version" {
		t.Errorf("newer entry = %+v, want Unreadable with Note=\"newer version\"", future)
	}
	// The valid save sorts above the (zero-SavedAt) flagged ones.
	if infos[0].ID != good.ID {
		t.Errorf("newest entry = %q, want the valid save %q first", infos[0].ID, good.ID)
	}
}
