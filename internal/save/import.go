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

// ImportLegacyIfNeeded migrates the legacy single-slot save.json into
// the saves directory as a named save — once. The probe fires only
// while saves/ is absent (idempotent: any saves directory, even an
// empty one, means the import already settled or the player started
// fresh on v0.26+). The legacy file is read, never rewritten or
// deleted — it stays behind untouched as a downgrade safety net (§G:
// a pre-v0.26 binary still reads it).
//
// The default name derives from the save's IN-GAME date, which lives
// in Payload.SimTimeNano — hence the one full unmarshal of the one
// legacy file. ClockT0 is wall-clock save time, not the in-game date,
// and must not be substituted; it seeds Meta.SavedAt instead, so the
// imported entry sorts truthfully in the browser. The payload bytes
// are carried through raw, Rename-style, with the original schema
// Version riding along untouched — LoadID migrates it on load like
// any older save.
//
// Returns the imported entry and true when the migration ran; the
// zero SaveInfo and false when there was nothing to do.
func ImportLegacyIfNeeded() (SaveInfo, bool, error) {
	dir, err := SavesDir()
	if err != nil {
		return SaveInfo{}, false, err
	}
	if _, err := os.Stat(dir); err == nil {
		return SaveInfo{}, false, nil // saves/ exists — import already settled
	} else if !os.IsNotExist(err) {
		return SaveInfo{}, false, fmt.Errorf("probe saves dir: %w", err)
	}
	legacy, err := DefaultPath()
	if err != nil {
		return SaveInfo{}, false, err
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		if os.IsNotExist(err) {
			return SaveInfo{}, false, nil // fresh install — nothing to import
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
	meta := &Meta{Name: "Imported save", SavedAt: time.Now().UTC()}
	if p.SimTimeNano != 0 {
		meta.InGameEpoch = time.Unix(0, p.SimTimeNano).UTC()
		meta.Name = "Imported " + meta.InGameEpoch.Format("2006-01-02")
	}
	if rf.ClockT0 != 0 {
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
	id := mintID()
	if err := writeAtomic(filepath.Join(dir, id), out); err != nil {
		return SaveInfo{}, false, err
	}
	return SaveInfo{ID: id, Meta: *meta, Lane: LaneNamed}, true, nil
}
