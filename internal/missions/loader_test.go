package missions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeOverlay points XDG_CONFIG_HOME at a temp dir and writes name→json
// files into its terminal-space-program/missions overlay dir, returning
// the overlay dir path. Mirrors the bodies overlay convention.
func writeOverlay(t *testing.T, files map[string]string) string {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	dir := filepath.Join(cfg, "terminal-space-program", "missions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func missionByID(cat Catalog, id string) (Mission, bool) {
	for _, m := range cat.Missions {
		if m.ID == id {
			return m, true
		}
	}
	return Mission{}, false
}

// TestLoadAllNoOverlay — with no user overlay dir, LoadAll returns just
// the embedded starter catalog and no warnings.
func TestLoadAllNoOverlay(t *testing.T) {
	// Point XDG at an empty temp dir so the overlay dir is absent.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cat, warnings, err := LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("LoadAllWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if len(cat.Missions) != 4 {
		t.Errorf("expected 4 embedded missions, got %d", len(cat.Missions))
	}
}

// TestLoadAllMergesUserOverlay — a valid user mission file is appended to
// the embedded catalog.
func TestLoadAllMergesUserOverlay(t *testing.T) {
	writeOverlay(t, map[string]string{
		"custom.json": `{"missions":[{"id":"user-venus-flyby","name":"Venus flyby",
			"objectives":[{"kind":"soi_flyby","params":{"primary_id":"venus"}}]}]}`,
	})
	cat, warnings, err := LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("LoadAllWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	m, ok := missionByID(cat, "user-venus-flyby")
	if !ok {
		t.Fatalf("user mission not merged; got %d missions", len(cat.Missions))
	}
	if len(m.Objectives) != 1 || m.Objectives[0].Kind != KindSOIFlyby {
		t.Errorf("user mission objectives not parsed: %+v", m.Objectives)
	}
	// Embedded missions survive alongside the overlay.
	if _, ok := missionByID(cat, "mars-soi-flyby"); !ok {
		t.Error("embedded mission dropped after overlay merge")
	}
}

// TestLoadAllUserOverrideByID — a user mission whose ID matches an
// embedded one replaces it (user files win on ID match), and the total
// count doesn't grow.
func TestLoadAllUserOverrideByID(t *testing.T) {
	writeOverlay(t, map[string]string{
		"override.json": `{"missions":[{"id":"mars-soi-flyby","name":"Mars flyby (custom)",
			"objectives":[{"kind":"soi_flyby","params":{"primary_id":"mars"}}]}]}`,
	})
	cat, _, err := LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("LoadAllWithWarnings: %v", err)
	}
	if len(cat.Missions) != 4 {
		t.Errorf("override grew the catalog: got %d missions, want 4", len(cat.Missions))
	}
	m, ok := missionByID(cat, "mars-soi-flyby")
	if !ok {
		t.Fatal("overridden mission missing")
	}
	if m.Name != "Mars flyby (custom)" {
		t.Errorf("user override didn't win: name = %q", m.Name)
	}
}

// TestLoadAllBadFileBecomesFailedMission — a malformed user file never
// fails the whole catalog: it surfaces as one Failed-status mission whose
// description carries the parse error, plus a warning, while the embedded
// missions all load. (ADR 0025 — fixes the "stderr line you didn't see"
// gap.)
func TestLoadAllBadFileBecomesFailedMission(t *testing.T) {
	writeOverlay(t, map[string]string{
		"broken.json": `{"missions":[{"id":"oops",`, // truncated JSON
	})
	cat, warnings, err := LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("LoadAllWithWarnings should not hard-fail on a bad overlay: %v", err)
	}
	if len(warnings) == 0 {
		t.Error("expected a warning for the malformed overlay file")
	}
	// All four embedded missions still present.
	for _, id := range []string{"leo-circularize-1000", "luna-orbit-insertion", "mars-soi-flyby", "saturn-v-pad-to-leo"} {
		if _, ok := missionByID(cat, id); !ok {
			t.Errorf("embedded mission %q dropped by a bad overlay file", id)
		}
	}
	// The bad file is represented by exactly one Failed-status mission.
	var failed []Mission
	for _, m := range cat.Missions {
		if m.Status == Failed {
			failed = append(failed, m)
		}
	}
	if len(failed) != 1 {
		t.Fatalf("expected 1 Failed placeholder mission, got %d", len(failed))
	}
	if !strings.Contains(failed[0].Name, "broken.json") {
		t.Errorf("Failed mission name should name the file: %q", failed[0].Name)
	}
	if failed[0].Description == "" {
		t.Error("Failed mission should carry the parse error in its description")
	}
}

// TestLoadAllDropsWarnings — the convenience LoadAll wraps
// LoadAllWithWarnings and swallows warnings but still returns the merged
// catalog (including any Failed placeholder).
func TestLoadAllDropsWarnings(t *testing.T) {
	writeOverlay(t, map[string]string{
		"broken.json": `not json at all`,
	})
	cat, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(cat.Missions) != 5 { // 4 embedded + 1 Failed placeholder
		t.Errorf("got %d missions, want 5 (4 embedded + 1 failed placeholder)", len(cat.Missions))
	}
}
