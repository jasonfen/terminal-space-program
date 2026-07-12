package save

// Legacy single-slot import — v0.26 / ADR 0033 §G. On the first run
// of a saves-directory binary, the pre-v0.26 save.json migrates into
// saves/ as a named save so no player loses their game or wonders
// where it went on upgrade.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// legacyImportMarker is the path of the bookkeeping file that records
// the one-time legacy save.json → saves/ migration has settled. It sits
// beside the legacy save.json (the state dir), NOT inside saves/ — so a
// quicksave/autosave that creates saves/ before the import, or a
// once-failed import that left an empty saves/ behind, can never be
// mistaken for a completed migration. The earlier bare os.Stat(saves/)
// gate had exactly that false positive: any saves/ directory looked
// "settled", permanently hiding an un-imported legacy save.
func legacyImportMarker() (string, error) {
	legacy, err := DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(legacy), ".legacy-imported"), nil
}

// ImportLegacyIfNeeded migrates the legacy single-slot save.json into
// the saves directory as a named save — once. It runs whenever the
// import has not yet settled (the marker is absent) AND a legacy
// save.json is present; a successful import drops the marker so it
// never repeats, and a FAILED import leaves the marker absent so the
// next startup retries (the marker, not the mere existence of saves/,
// is what gates the retry). The legacy file is read, never rewritten or
// deleted — it stays behind untouched as a downgrade safety net (§G: a
// pre-v0.26 binary still reads it).
//
// The default name derives from the save's IN-GAME date, which lives
// in Payload.SimTimeNano — hence the one full unmarshal of the one
// legacy file. ClockT0 is wall-clock save time, not the in-game date,
// and must not be substituted; SavedAt derives from it (in memory only
// — Meta.SavedAt is never persisted) so the imported entry sorts
// truthfully in the browser. The payload bytes are carried through
// raw, Rename-style, with the original schema Version riding along
// untouched — LoadID migrates it on load like any older save.
//
// Returns the imported entry and true when the migration ran; the
// zero SaveInfo and false when there was nothing to do.
func ImportLegacyIfNeeded() (SaveInfo, bool, error) {
	marker, err := legacyImportMarker()
	if err != nil {
		return SaveInfo{}, false, err
	}
	if _, err := os.Stat(marker); err == nil {
		return SaveInfo{}, false, nil // marker present — import already settled
	} else if !os.IsNotExist(err) {
		return SaveInfo{}, false, fmt.Errorf("probe import marker: %w", err)
	}
	legacy, err := DefaultPath()
	if err != nil {
		return SaveInfo{}, false, err
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		if os.IsNotExist(err) {
			// Fresh install — nothing to import. Deliberately no marker:
			// if a legacy save is ever dropped in later, it still imports.
			return SaveInfo{}, false, nil
		}
		return SaveInfo{}, false, fmt.Errorf("read legacy save: %w", err)
	}
	var rf rawFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return SaveInfo{}, false, fmt.Errorf("parse legacy save: %w", err)
	}
	if rf.Version < 1 || rf.Version > SchemaVersion {
		return SaveInfo{}, false, fmt.Errorf("%w: got %d, want 1..%d", ErrSchemaMismatch, rf.Version, SchemaVersion)
	}
	// The full-payload decode §G asks for: the in-game date for the
	// default name, plus the active vessel for the browser columns.
	var p Payload
	if err := json.Unmarshal(rf.Payload, &p); err != nil {
		return SaveInfo{}, false, fmt.Errorf("parse legacy payload: %w", err)
	}
	meta := &Meta{Name: "Imported save"}
	if p.SimTimeNano != 0 {
		meta.InGameEpoch = time.Unix(0, p.SimTimeNano).UTC()
		meta.Name = "Imported " + meta.InGameEpoch.Format("2006-01-02")
	}
	if rf.ClockT0 != 0 {
		// Derived, non-persisted (json:"-") — for the returned SaveInfo
		// and the browser's newest-first sort. The legacy ClockT0 rides
		// through in rf, so List recomputes the same value on read.
		meta.SavedAt = time.Unix(0, rf.ClockT0).UTC()
	}
	if i := p.ActiveCraftIdx; i >= 0 && i < len(p.Crafts) {
		meta.ActiveVesselName = p.Crafts[i].Name
	} else if p.Craft != nil {
		meta.ActiveVesselName = p.Craft.Name // pre-v5 singular form
	}
	// SystemName stays empty — resolving Payload.SystemIdx to a name
	// needs the body catalog, which the import deliberately avoids; the
	// browser renders a blank system column, same as any Meta-less
	// legacy entry.
	rf.Meta = meta
	out, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return SaveInfo{}, false, fmt.Errorf("marshal imported save: %w", err)
	}
	dir, err := SavesDir()
	if err != nil {
		return SaveInfo{}, false, err
	}
	id := mintID()
	if err := writeAtomic(filepath.Join(dir, id), out); err != nil {
		return SaveInfo{}, false, err
	}
	// Settle the migration only after the imported file is safely on disk.
	// The write is best-effort: the import itself already succeeded, so a
	// failed marker must NOT be reported as an error — that would mislog
	// the run as "skipped" even though the save landed. Its only cost is a
	// harmless duplicate re-import next launch (the save is never lost),
	// and it can only fail if the same directory we just wrote to became
	// unwritable, which is vanishingly unlikely.
	_ = markLegacyImported(marker)
	return SaveInfo{ID: id, Meta: *meta, Lane: LaneNamed}, true, nil
}

// markLegacyImported drops the settled-migration marker (finding: the
// retry gate). The content is human-facing only — presence is what
// matters.
func markLegacyImported(marker string) error {
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	return os.WriteFile(marker, []byte("v0.26 legacy save.json imported (ADR 0033 §G)\n"), 0o644)
}
