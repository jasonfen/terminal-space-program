package bodies

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllSystems(t *testing.T) {
	systems, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(systems) < 4 {
		t.Fatalf("expected >=4 systems, got %d", len(systems))
	}
	if systems[0].Name != "Sol" {
		t.Errorf("expected Sol first, got %q", systems[0].Name)
	}
	names := map[string]bool{}
	for _, s := range systems {
		names[s.Name] = true
	}
	for _, want := range []string{"Sol", "Alpha Centauri", "TRAPPIST-1", "Kepler-452"} {
		if !names[want] {
			t.Errorf("missing system %q", want)
		}
	}
}

func TestSolEarthValues(t *testing.T) {
	systems, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var sol *System
	for i := range systems {
		if systems[i].Name == "Sol" {
			sol = &systems[i]
			break
		}
	}
	if sol == nil {
		t.Fatal("Sol not found")
	}
	earth := sol.FindBody("Earth")
	if earth == nil {
		t.Fatal("Earth not found in Sol")
	}
	// Earth semimajor axis ≈ 1 AU ± 0.1%.
	earthAMeters := earth.SemimajorAxisMeters()
	if d := math.Abs(earthAMeters-AU) / AU; d > 0.001 {
		t.Errorf("Earth semimajor axis %.3e m deviates from 1 AU by %.4f", earthAMeters, d)
	}
	// Earth mass ≈ 5.972e24 kg ± 0.1%.
	if d := math.Abs(earth.MassKg()-5.972e24) / 5.972e24; d > 0.001 {
		t.Errorf("Earth mass %.3e kg deviates from 5.972e24 by %.4f", earth.MassKg(), d)
	}
}

func TestGravitationalParameter(t *testing.T) {
	systems, _ := LoadAll()
	earth := systems[0].FindBody("Earth")
	if earth == nil {
		t.Fatal("Earth not found")
	}
	// Standard gravitational parameter of Earth: 3.986e14 m^3/s^2 ± 0.1%.
	mu := earth.GravitationalParameter()
	want := 3.986004418e14
	if d := math.Abs(mu-want) / want; d > 0.001 {
		t.Errorf("Earth GM %.3e deviates from %.3e by %.4f", mu, want, d)
	}
}

// withUserOverlay creates an empty XDG overlay dir for a test, then
// optionally writes one or more system files into it. Returns the dir
// for later writes by the caller.
func withUserOverlay(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "terminal-space-program", "systems")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir overlay dir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", root)
	return dir
}

func TestLoadAllEmptyOverlay(t *testing.T) {
	withUserOverlay(t)
	systems, warnings, err := LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("LoadAllWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(warnings))
	}
	if len(systems) < 4 {
		t.Errorf("systems = %d, want >= 4", len(systems))
	}
	for _, s := range systems {
		if s.Source != "embedded" {
			t.Errorf("system %q source = %q, want embedded", s.Name, s.Source)
		}
	}
}

func TestLoadAllUserOverlayAppends(t *testing.T) {
	dir := withUserOverlay(t)
	custom := `{
		"systemName": "Test System",
		"description": "test overlay",
		"distance": "0",
		"galaxy": "test",
		"bodies": [{"id":"x","englishName":"X","bodyType":"Star",
			"mass":{"massValue":1,"massExponent":30},"meanRadius":1000}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "test.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	systems, warnings, err := LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("LoadAllWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(warnings))
	}
	var found *System
	for i := range systems {
		if systems[i].Name == "Test System" {
			found = &systems[i]
		}
	}
	if found == nil {
		t.Fatal("Test System not in loaded set")
	}
	if found.Source != "user" {
		t.Errorf("source = %q, want user", found.Source)
	}
}

func TestLoadAllUserOverlayReplacesEmbedded(t *testing.T) {
	dir := withUserOverlay(t)
	stub := `{
		"systemName": "Sol",
		"description": "user override",
		"distance": "0",
		"galaxy": "Milky Way",
		"bodies": [{"id":"sun","englishName":"Sun","bodyType":"Star",
			"mass":{"massValue":1.98892,"massExponent":30},"meanRadius":695700}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "sol.json"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}
	systems, _, err := LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("LoadAllWithWarnings: %v", err)
	}
	if systems[0].Name != "Sol" {
		t.Fatal("Sol not first after sort")
	}
	sol := systems[0]
	if sol.Source != "user" {
		t.Errorf("Sol source = %q, want user", sol.Source)
	}
	if sol.Description != "user override" {
		t.Errorf("description = %q, want user override", sol.Description)
	}
	if len(sol.Bodies) != 1 {
		t.Errorf("bodies = %d, want 1 (stub)", len(sol.Bodies))
	}
}

func TestLoadAllMalformedUserFileWarns(t *testing.T) {
	dir := withUserOverlay(t)
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	systems, warnings, err := LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("malformed user file should not fail load: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(warnings))
	}
	if len(systems) < 4 {
		t.Errorf("embedded systems still loaded: got %d, want >= 4", len(systems))
	}
}
