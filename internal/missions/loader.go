package missions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadWarning reports a user-overlay file that couldn't be loaded. The
// embedded starter catalog must always load — a parse failure there is a
// hard error, not a warning. Mirrors bodies.LoadWarning.
type LoadWarning struct {
	Path string
	Err  error
}

func (w LoadWarning) Error() string {
	return fmt.Sprintf("%s: %v", w.Path, w.Err)
}

// LoadAll returns the embedded starter catalog merged with any user
// overlay files from $XDG_CONFIG_HOME/terminal-space-program/missions/*.json
// (or ~/.config/... if XDG is unset). Warnings from malformed user files
// are dropped — call LoadAllWithWarnings to inspect them. A malformed
// overlay file still leaves a Failed-status placeholder mission in the
// catalog so the player sees it in-game. Mirrors bodies.LoadAll.
func LoadAll() (Catalog, error) {
	cat, _, err := LoadAllWithWarnings()
	return cat, err
}

// LoadAllWithWarnings is the warning-aware variant of LoadAll. The
// returned warnings slice contains a LoadWarning for any user overlay
// file that failed to read or parse; a malformed embedded catalog surfaces
// as a hard error (the err value) since the embedded set must always load.
//
// A bad user file never fails the whole catalog (ADR 0025): it is appended
// as a single Failed-status mission whose description carries the parse
// error, fixing the archived "stderr line you didn't see" gap. User
// missions whose ID matches an embedded mission win on the match;
// otherwise they're appended.
func LoadAllWithWarnings() (Catalog, []LoadWarning, error) {
	cat, err := DefaultCatalog()
	if err != nil {
		return Catalog{}, nil, err
	}
	merged, warnings := mergeUserOverlay(cat.Missions, userMissionsDir())
	cat.Missions = merged
	return cat, warnings, nil
}

func mergeUserOverlay(base []Mission, dir string) ([]Mission, []LoadWarning) {
	if dir == "" {
		return base, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Missing dir is fine — the overlay is optional. Other I/O errors
		// (permissions, etc.) surface as a warning on the dir path; we
		// can't enumerate files, so there's no per-file placeholder.
		if os.IsNotExist(err) {
			return base, nil
		}
		return base, []LoadWarning{{Path: dir, Err: err}}
	}
	var warnings []LoadWarning
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, LoadWarning{Path: path, Err: err})
			base = append(base, failedMissionFromFile(path, err))
			continue
		}
		uc, err := LoadCatalog(data)
		if err != nil {
			warnings = append(warnings, LoadWarning{Path: path, Err: err})
			base = append(base, failedMissionFromFile(path, err))
			continue
		}
		for _, m := range uc.Missions {
			base = upsertMission(base, m)
		}
	}
	return base, warnings
}

// failedMissionFromFile turns an unloadable overlay file into a single
// Failed-status placeholder mission so the problem is visible in-game
// rather than dropped to stderr.
func failedMissionFromFile(path string, err error) Mission {
	base := filepath.Base(path)
	return Mission{
		ID:          "invalid:" + base,
		Name:        "Invalid mission file: " + base,
		Description: err.Error(),
		Status:      Failed,
	}
}

// upsertMission appends m, or replaces an existing mission with the same
// ID (user files win on ID match).
func upsertMission(base []Mission, m Mission) []Mission {
	for i := range base {
		if base[i].ID == m.ID {
			base[i] = m
			return base
		}
	}
	return append(base, m)
}

func userMissionsDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "missions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "terminal-space-program", "missions")
}
