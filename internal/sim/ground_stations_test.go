package sim

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestGroundStationsDSNRing (C2-3, ADR 0027; v0.24): the embedded catalog
// holds a 3-station DSN ring on each home body — Earth (Sol) and Kern
// (Lumen) — each spread around the globe (~120° apart) and high-power, so a
// vessel launched in either system can reach the network.
func TestGroundStationsDSNRing(t *testing.T) {
	stations, warnings := LoadGroundStations()
	if len(warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(warnings))
	}
	// Group the embedded ring by home body.
	byBody := map[string][]GroundStationPreset{}
	for _, s := range stations {
		if s.Source != "embedded" {
			t.Errorf("station %q Source = %q, want embedded", s.Key, s.Source)
		}
		if s.AntennaRangeM <= 0 {
			t.Errorf("station %q has no antenna range", s.Key)
		}
		byBody[s.BodyID] = append(byBody[s.BodyID], s)
	}
	// Every home body with launch infrastructure carries a DSN ring.
	for _, home := range []string{"earth", "kern"} {
		ring := byBody[home]
		if len(ring) != 3 {
			t.Errorf("%s DSN ring = %d stations, want 3", home, len(ring))
			continue
		}
		assertRingSpread(t, home, ring)
	}
}

// assertRingSpread checks the three stations of a DSN ring sit ~120° apart
// in longitude (each consecutive gap well past 60°), the spread that gives
// near-continuous home coverage as the body rotates.
func assertRingSpread(t *testing.T, body string, ring []GroundStationPreset) {
	t.Helper()
	lons := make([]float64, 0, len(ring))
	for _, s := range ring {
		lons = append(lons, math.Mod(s.LonEastDeg+360, 360)) // normalize to [0,360)
	}
	sort.Float64s(lons)
	for i := range lons {
		gap := lons[(i+1)%len(lons)] - lons[i]
		if i == len(lons)-1 {
			gap = lons[0] + 360 - lons[len(lons)-1]
		}
		if gap < 60 {
			t.Errorf("%s longitude gap %d = %.1f°, want > 60° (stations clustered, not a ring)", body, i, gap)
		}
	}
}

// TestNewWorldLoadsGroundStations (C2-3): NewWorld populates
// World.GroundStations from the catalog — both home-body DSN rings.
func TestNewWorldLoadsGroundStations(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if len(w.GroundStations) != 6 {
		t.Errorf("World.GroundStations = %d, want 6 (Earth + Kern DSN rings)", len(w.GroundStations))
	}
}

// TestGroundStationUserOverlay (C2-3): a user overlay adds/replaces by
// Key; a malformed file is skipped with a warning.
func TestGroundStationUserOverlay(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "terminal-space-program", "ground_stations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", root)

	// Add a new station + override Goldstone's range.
	good := `{"stations":[
		{"key":"luna-farside","name":"Luna Farside","body_id":"moon","lat_deg":0,"lon_east_deg":180,"antenna_range_m":50000},
		{"key":"goldstone","name":"Goldstone (modded)","body_id":"earth","lat_deg":35.43,"lon_east_deg":-116.89,"antenna_range_m":250000}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "mine.json"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	stations, warnings := LoadGroundStations()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1 (the malformed file)", len(warnings))
	}
	byKey := map[string]GroundStationPreset{}
	for _, s := range stations {
		byKey[s.Key] = s
	}
	if g, ok := byKey["luna-farside"]; !ok || g.BodyID != "moon" || g.Source != "user" {
		t.Errorf("user station luna-farside not merged correctly: %+v", g)
	}
	if g := byKey["goldstone"]; g.AntennaRangeM != 250000 || g.Source != "user" {
		t.Errorf("user override of goldstone failed: %+v", g)
	}
}

// TestGroundStationLegacyKeyFallsBackToDefaultRange (#182): a user overlay
// authored before the power_w→range_m rename uses the old key, which now
// unmarshals to range 0. The loader must rescue it to the DSN-tier default so
// the station stays functional rather than silently dead (commLinked rejects
// range<=0).
func TestGroundStationLegacyKeyFallsBackToDefaultRange(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "terminal-space-program", "ground_stations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", root)

	// Pre-amendment overlay: the legacy antenna_power_w key, no antenna_range_m.
	legacy := `{"stations":[
		{"key":"legacy","name":"Legacy Dish","body_id":"earth","lat_deg":0,"lon_east_deg":0,"antenna_power_w":250000}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "legacy.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	stations, _ := LoadGroundStations()
	for _, s := range stations {
		if s.Key == "legacy" {
			if s.AntennaRangeM != DefaultGroundStationRangeM {
				t.Errorf("legacy-key station range = %g, want default %g (rescued, not dead)", s.AntennaRangeM, DefaultGroundStationRangeM)
			}
			return
		}
	}
	t.Error("legacy-key station was not loaded")
}
