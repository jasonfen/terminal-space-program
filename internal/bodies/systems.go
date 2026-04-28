package bodies

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed systems/*.json
var systemsFS embed.FS

// System is a named collection of celestial bodies orbiting a common primary.
// The first body (index 0) is treated as the primary (star or barycenter).
type System struct {
	Name        string          `json:"systemName"`
	Description string          `json:"description"`
	Distance    string          `json:"distance"`
	Galaxy      string          `json:"galaxy"`
	Bodies      []CelestialBody `json:"bodies"`

	// Source is a runtime annotation: "embedded" for the built-in
	// catalog, "user" for files loaded from
	// $XDG_CONFIG_HOME/terminal-space-program/systems/*.json (v0.7.0+).
	// Excluded from JSON marshaling so CatalogHash is stable across
	// identical-data overlays.
	Source string `json:"-"`
}

// LoadWarning is returned by LoadAllWithWarnings for user-supplied
// overlay files that failed to parse. Embedded systems must always
// load — a parse failure there is a hard error, not a warning.
type LoadWarning struct {
	Path string
	Err  error
}

func (w LoadWarning) Error() string {
	return fmt.Sprintf("%s: %v", w.Path, w.Err)
}

// Primary returns the body treated as the gravitational primary (index 0).
func (s *System) Primary() *CelestialBody {
	if len(s.Bodies) == 0 {
		return nil
	}
	return &s.Bodies[0]
}

// ParentOf returns the gravitational parent of body `b` in this system.
// For top-level bodies (ParentID empty) the system primary (index 0) is
// returned. Returns nil if b's ParentID is set but unresolvable, which
// signals a malformed system.
func (s *System) ParentOf(b CelestialBody) *CelestialBody {
	if b.ParentID == "" {
		return s.Primary()
	}
	for i := range s.Bodies {
		if s.Bodies[i].ID == b.ParentID {
			return &s.Bodies[i]
		}
	}
	return nil
}

// FindBody returns a pointer to the body with matching id or englishName.
// Case-insensitive on englishName; exact match on id.
func (s *System) FindBody(query string) *CelestialBody {
	for i := range s.Bodies {
		if s.Bodies[i].ID == query {
			return &s.Bodies[i]
		}
	}
	ql := strings.ToLower(query)
	for i := range s.Bodies {
		if strings.ToLower(s.Bodies[i].EnglishName) == ql {
			return &s.Bodies[i]
		}
	}
	return nil
}

// LoadAll reads every embedded system JSON, merges any user overlay
// files from $XDG_CONFIG_HOME/terminal-space-program/systems/*.json
// (or ~/.config/... if XDG is unset), and returns the merged set
// sorted by name with Sol always first. Warnings from malformed user
// files are dropped — call LoadAllWithWarnings to inspect them.
func LoadAll() ([]System, error) {
	systems, _, err := LoadAllWithWarnings()
	return systems, err
}

// LoadAllWithWarnings is the warning-aware variant of LoadAll. The
// returned warnings slice contains LoadWarning entries for any user
// overlay files that failed to parse; embedded-catalog parse failures
// surface as a hard error (returned as the err value) since the
// embedded set must always load.
func LoadAllWithWarnings() ([]System, []LoadWarning, error) {
	systems, err := loadEmbedded()
	if err != nil {
		return nil, nil, err
	}
	systems, warnings := mergeUserOverlay(systems, userSystemsDir())
	sort.Slice(systems, func(i, j int) bool {
		if systems[i].Name == "Sol" {
			return true
		}
		if systems[j].Name == "Sol" {
			return false
		}
		return systems[i].Name < systems[j].Name
	})
	return systems, warnings, nil
}

func loadEmbedded() ([]System, error) {
	entries, err := systemsFS.ReadDir("systems")
	if err != nil {
		return nil, fmt.Errorf("read embedded systems dir: %w", err)
	}
	out := make([]System, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := systemsFS.ReadFile("systems/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var s System
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		s.Source = "embedded"
		out = append(out, s)
	}
	return out, nil
}

func mergeUserOverlay(systems []System, dir string) ([]System, []LoadWarning) {
	if dir == "" {
		return systems, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Missing dir is fine — overlay is optional. Permission /
		// other I/O errors get surfaced as warnings on a synthetic
		// "<dir>" path so the user knows their overlay dir is
		// unreadable.
		if os.IsNotExist(err) {
			return systems, nil
		}
		return systems, []LoadWarning{{Path: dir, Err: err}}
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
			continue
		}
		var s System
		if err := json.Unmarshal(data, &s); err != nil {
			warnings = append(warnings, LoadWarning{Path: path, Err: err})
			continue
		}
		s.Source = "user"
		// Conflict policy: user files win on system.Name match,
		// otherwise append.
		replaced := false
		for i := range systems {
			if systems[i].Name == s.Name {
				systems[i] = s
				replaced = true
				break
			}
		}
		if !replaced {
			systems = append(systems, s)
		}
	}
	return systems, warnings
}

func userSystemsDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "systems")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "terminal-space-program", "systems")
}
