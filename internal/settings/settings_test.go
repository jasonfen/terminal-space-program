package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withConfigRoot points XDG_CONFIG_HOME at a fresh temp dir and returns
// the settings.json path inside it. Optionally seeds the file with the
// given JSON (skipped when content == "").
func withConfigRoot(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	dir := filepath.Join(root, "terminal-space-program")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "settings.json")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("seed settings: %v", err)
		}
	}
	return path
}

func TestDefaultAllChipsVisible(t *testing.T) {
	s := Default()
	for _, c := range AllChips {
		if !s.ChipEnabled(c) {
			t.Errorf("Default(): chip %q disabled, want visible", c)
		}
	}
}

func TestLoadMissingFileYieldsDefault(t *testing.T) {
	withConfigRoot(t, "") // no file written

	s, warnings := Load()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none for a missing file", warnings)
	}
	for _, c := range AllChips {
		if !s.ChipEnabled(c) {
			t.Errorf("missing file: chip %q disabled, want visible", c)
		}
	}
}

func TestLoadPartialFileFillsAbsentWithDefault(t *testing.T) {
	// Only "stages" is named (and turned off); every other Chip must
	// stay at its visible default.
	withConfigRoot(t, `{"chips":{"stages":false}}`)

	s, warnings := Load()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none for a valid partial file", warnings)
	}
	if s.ChipEnabled(ChipStages) {
		t.Errorf("ChipStages enabled, want disabled (explicitly set false)")
	}
	for _, c := range AllChips {
		if c == ChipStages {
			continue
		}
		if !s.ChipEnabled(c) {
			t.Errorf("absent chip %q disabled, want visible default", c)
		}
	}
}

func TestLoadToleratesUnknownKeys(t *testing.T) {
	// A newer build's extra keys (an unknown chip + an unknown top-level
	// pref) must not fail the load; known state still applies.
	withConfigRoot(t, `{"chips":{"target":false,"warpFactor":true},"units":"metric"}`)

	s, warnings := Load()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none — unknown keys are tolerated", warnings)
	}
	if s.ChipEnabled(ChipTarget) {
		t.Errorf("ChipTarget enabled, want disabled")
	}
}

func TestLoadMalformedWarnsAndDefaults(t *testing.T) {
	withConfigRoot(t, `{not valid json`)

	s, warnings := Load()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1 for malformed JSON", len(warnings))
	}
	for _, c := range AllChips {
		if !s.ChipEnabled(c) {
			t.Errorf("malformed file: chip %q disabled, want visible default", c)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withConfigRoot(t, "")

	s := Default()
	s.SetChip(ChipNodes, false)
	s.SetChip(ChipCapture, false)
	if err := Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, warnings := Load()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if got.ChipEnabled(ChipNodes) {
		t.Errorf("ChipNodes enabled after round-trip, want disabled")
	}
	if got.ChipEnabled(ChipCapture) {
		t.Errorf("ChipCapture enabled after round-trip, want disabled")
	}
	if !got.ChipEnabled(ChipTarget) {
		t.Errorf("ChipTarget disabled after round-trip, want visible default")
	}
}

func TestKeyboardLayoutRoundTrips(t *testing.T) {
	withConfigRoot(t, "")

	// Default omits the field — an absent file is QWERTY (empty string).
	if got := Default().KeyboardLayout; got != "" {
		t.Errorf("Default KeyboardLayout = %q, want empty", got)
	}

	s := Default()
	s.KeyboardLayout = "qwertz"
	s.SetChip(ChipNodes, false) // co-persisted alongside the layout
	if err := Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, warnings := Load()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if got.KeyboardLayout != "qwertz" {
		t.Errorf("KeyboardLayout = %q after round-trip, want qwertz", got.KeyboardLayout)
	}
	if got.ChipEnabled(ChipNodes) {
		t.Errorf("chip override lost when layout co-persisted")
	}
}

func TestMissionProgramsDefaultOff(t *testing.T) {
	// A fresh sandbox shows no missions until the player opts in (v0.21 Slice 7).
	s := Default()
	if s.TutorialEnabled || s.ChallengesEnabled {
		t.Errorf("mission programs should default OFF, got tutorial=%v challenges=%v",
			s.TutorialEnabled, s.ChallengesEnabled)
	}
}

func TestMissionProgramTogglesRoundTrip(t *testing.T) {
	withConfigRoot(t, "")

	s := Default()
	s.TutorialEnabled = true // enable one, leave the other off
	s.SetChip(ChipNodes, false)
	if err := Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, warnings := Load()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if !got.TutorialEnabled {
		t.Error("TutorialEnabled lost across round-trip")
	}
	if got.ChallengesEnabled {
		t.Error("ChallengesEnabled flipped on across round-trip")
	}
	if got.ChipEnabled(ChipNodes) {
		t.Error("chip override lost when mission toggle co-persisted")
	}
}

func TestSaveIsIdempotent(t *testing.T) {
	withConfigRoot(t, "")

	s := Default()
	s.SetChip(ChipLaunch, false)
	if err := Save(s); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	first, _ := Load()
	if err := Save(first); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	second, _ := Load()

	for _, c := range AllChips {
		if first.ChipEnabled(c) != second.ChipEnabled(c) {
			t.Errorf("chip %q differs across re-save: %v then %v",
				c, first.ChipEnabled(c), second.ChipEnabled(c))
		}
	}
}

func TestSaveOmitsDefaultsWhenNoOverrides(t *testing.T) {
	// Saving the all-defaults Settings must not write a "chips" object —
	// the absent/empty map represents "all on" at zero cost.
	path := withConfigRoot(t, "")

	if err := Save(Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["chips"]; ok {
		t.Errorf("default Save wrote a %q key; want it omitted, got %s", "chips", data)
	}
}

func TestSaveCreatesConfigDir(t *testing.T) {
	// Point XDG at a temp root but do NOT pre-create the
	// terminal-space-program dir; Save must mkdir it.
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	s := Default()
	s.SetChip(ChipStages, false)
	if err := Save(s); err != nil {
		t.Fatalf("Save into nonexistent dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "terminal-space-program", "settings.json")); err != nil {
		t.Errorf("settings.json not created: %v", err)
	}
}

func TestPathHonoursXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	want := filepath.Join("/custom/xdg", "terminal-space-program", "settings.json")
	if got := Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathFallsBackToHomeConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	want := filepath.Join(home, ".config", "terminal-space-program", "settings.json")
	if got := Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestChipLabelsCoverAllChips(t *testing.T) {
	// Every Chip in the UI-facing list must have an explicit label so the
	// Settings screen never renders a raw key.
	for _, c := range AllChips {
		if _, ok := chipLabels[c]; !ok {
			t.Errorf("chip %q has no label", c)
		}
	}
}

// The Orbit-metrics readout is intentionally NOT a toggleable Chip — a
// player must never be able to permanently hide their current orbit from
// the Settings screen (it stays F2-Declutter-hideable in the tui). Guards
// against it being re-added to the toggle set.
func TestOrbitMetricsNotToggleable(t *testing.T) {
	for _, c := range AllChips {
		if c == Chip("orbitMetrics") {
			t.Errorf("orbitMetrics must not be a toggleable Chip (it is always-on)")
		}
	}
}
