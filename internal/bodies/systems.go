package bodies

import (
	"embed"
	"encoding/json"
	"fmt"
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

// LoadAll reads every embedded system JSON and returns them sorted by name,
// with Sol always first (spacecraft is restricted to Sol for v0.1).
func LoadAll() ([]System, error) {
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
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == "Sol" {
			return true
		}
		if out[j].Name == "Sol" {
			return false
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
