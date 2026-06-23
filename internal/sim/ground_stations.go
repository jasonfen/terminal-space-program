package sim

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CommNet ground-station catalog (v0.23 / ADR 0027). The home network is
// a static DSN ring on the home body; relays extend coverage to the far
// side of other bodies + deep space. Mirrors the LaunchSitePreset shape
// but is DATA-driven (embedded JSON + user overlay) so a hard-mode single
// station, extra stations, or stations on other bodies are just data
// (ADR 0027 §5). Each station co-rotates with its body — its world
// position is computed on demand by the connectivity graph (a later
// cycle-2 slice), from BodyFixedToWorld at sim time.

//go:embed data/ground_stations.json
var groundStationsFS embed.FS

// GroundStationPreset is one fixed-surface comms anchor. Distinct from a
// LaunchSitePreset (a launchpad): a ground station is a network node. Key
// is the short token, BodyID the body it sits on (its surface co-rotates),
// LatDeg / LonEastDeg the body-fixed position (east-positive, pseudo-
// Greenwich at simTime=0 — same convention as launch sites), AntennaPowerW
// the relay power (ground stations are high-power; the two-endpoint range
// formula is deferred tuning, ADR 0027 §2).
type GroundStationPreset struct {
	Key           string  `json:"key"`
	Name          string  `json:"name"`
	BodyID        string  `json:"body_id"`
	LatDeg        float64 `json:"lat_deg"`
	LonEastDeg    float64 `json:"lon_east_deg"`
	AntennaPowerW float64 `json:"antenna_power_w"`

	// Source is a runtime annotation ("embedded" / "user"); excluded from
	// JSON so it never affects round-trips.
	Source string `json:"-"`
}

// groundStationCatalog is the on-disk envelope (embedded + user overlay).
type groundStationCatalog struct {
	Stations []GroundStationPreset `json:"stations"`
}

// GroundStationWarning records a user overlay file that failed to load —
// the bodies-pattern skip-bad-with-warning (ADR 0027 §5 inherits ADR 0026 §3).
type GroundStationWarning struct {
	Path string
	Err  error
}

func (w GroundStationWarning) Error() string { return fmt.Sprintf("%s: %v", w.Path, w.Err) }

// LoadGroundStations reads the embedded DSN ring merged with any user
// overlay, dropping warnings. Call LoadGroundStationsWithWarnings to
// inspect them.
func LoadGroundStations() ([]GroundStationPreset, []GroundStationWarning) {
	return LoadGroundStationsWithWarnings()
}

// LoadGroundStationsWithWarnings loads the embedded catalog merged with the
// user overlay ($XDG_CONFIG_HOME/terminal-space-program/ground_stations/*.json),
// user winning on Key. A malformed embedded file panics (build error); a
// malformed user file is skipped with a warning.
func LoadGroundStationsWithWarnings() ([]GroundStationPreset, []GroundStationWarning) {
	stations := loadEmbeddedGroundStations()
	return mergeUserGroundStations(stations, userGroundStationsDir())
}

func loadEmbeddedGroundStations() []GroundStationPreset {
	data, err := groundStationsFS.ReadFile("data/ground_stations.json")
	if err != nil {
		panic("sim: embedded ground_stations.json missing: " + err.Error())
	}
	var cat groundStationCatalog
	if err := json.Unmarshal(data, &cat); err != nil {
		panic("sim: embedded ground_stations.json parse: " + err.Error())
	}
	for i := range cat.Stations {
		cat.Stations[i].Source = "embedded"
	}
	return cat.Stations
}

func mergeUserGroundStations(stations []GroundStationPreset, dir string) ([]GroundStationPreset, []GroundStationWarning) {
	if dir == "" {
		return stations, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return stations, nil
		}
		return stations, []GroundStationWarning{{Path: dir, Err: err}}
	}
	var warnings []GroundStationWarning
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, GroundStationWarning{Path: path, Err: err})
			continue
		}
		var cat groundStationCatalog
		if err := json.Unmarshal(raw, &cat); err != nil {
			warnings = append(warnings, GroundStationWarning{Path: path, Err: err})
			continue
		}
		for _, s := range cat.Stations {
			s.Source = "user"
			replaced := false
			for i := range stations {
				if stations[i].Key == s.Key {
					stations[i] = s
					replaced = true
					break
				}
			}
			if !replaced {
				stations = append(stations, s)
			}
		}
	}
	return stations, warnings
}

func userGroundStationsDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "ground_stations")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "terminal-space-program", "ground_stations")
}
