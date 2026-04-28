package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// withThemeFile sets up an isolated XDG_CONFIG_HOME with a theme.json
// containing the given body. Returns the dir for caller use.
func withThemeFile(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "terminal-space-program")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(content), 0o644); err != nil {
			t.Fatalf("write theme: %v", err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", root)
	return dir
}

func TestLoadThemeNoFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	defer LoadTheme() // restore defaults after subsequent tests

	originalAlert := ColorAlert
	theme, warnings, err := LoadTheme()
	if err != nil {
		t.Fatalf("LoadTheme: %v", err)
	}
	if theme != nil {
		t.Errorf("theme = %+v, want nil for missing file", theme)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(warnings))
	}
	if ColorAlert != originalAlert {
		t.Errorf("ColorAlert mutated to %q without a theme file", ColorAlert)
	}
}

func TestLoadThemeUIOverride(t *testing.T) {
	withThemeFile(t, `{"ui": {"alert": "#abcdef", "warning": "#012345"}}`)
	defer LoadTheme()

	if _, _, err := LoadTheme(); err != nil {
		t.Fatalf("LoadTheme: %v", err)
	}
	if string(ColorAlert) != "#abcdef" {
		t.Errorf("ColorAlert = %q, want #abcdef", string(ColorAlert))
	}
	if string(ColorWarning) != "#012345" {
		t.Errorf("ColorWarning = %q, want #012345", string(ColorWarning))
	}
}

func TestLoadThemeBodyOverrideWinsOverColorField(t *testing.T) {
	withThemeFile(t, `{"bodies": {"earth": "#aabbcc"}}`)
	defer LoadTheme()

	if _, _, err := LoadTheme(); err != nil {
		t.Fatalf("LoadTheme: %v", err)
	}
	b := bodies.CelestialBody{
		ID:       "earth",
		BodyType: "Planet",
		Color:    "#5BB3FF", // would normally win post-v0.7.1
	}
	got := ColorFor(b)
	if string(got) != "#aabbcc" {
		t.Errorf("ColorFor with body override = %q, want #aabbcc", string(got))
	}
}

func TestLoadThemeBodyOverrideMissingBody(t *testing.T) {
	withThemeFile(t, `{"bodies": {"mars": "#123456"}}`)
	defer LoadTheme()

	if _, _, err := LoadTheme(); err != nil {
		t.Fatalf("LoadTheme: %v", err)
	}
	b := bodies.CelestialBody{
		ID:       "earth",
		BodyType: "Planet",
		Color:    "#5BB3FF",
	}
	got := ColorFor(b)
	if string(got) != "#5BB3FF" {
		t.Errorf("non-overridden body = %q, want #5BB3FF", string(got))
	}
}

func TestLoadThemeMalformedWarns(t *testing.T) {
	withThemeFile(t, `{not valid json`)
	defer LoadTheme()

	originalAlert := ColorAlert
	theme, warnings, err := LoadTheme()
	if err != nil {
		t.Fatalf("LoadTheme: %v", err)
	}
	if theme != nil {
		t.Errorf("theme = %+v, want nil on parse error", theme)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(warnings))
	}
	if ColorAlert != originalAlert {
		t.Errorf("ColorAlert mutated despite parse failure")
	}
}

func TestLoadThemeIdempotentResetsToDefaults(t *testing.T) {
	dir := withThemeFile(t, `{"ui": {"alert": "#aaaaaa"}}`)
	defer LoadTheme()

	// First call applies overrides.
	if _, _, err := LoadTheme(); err != nil {
		t.Fatalf("first LoadTheme: %v", err)
	}
	if string(ColorAlert) != "#aaaaaa" {
		t.Fatalf("first call: ColorAlert = %q, want #aaaaaa", string(ColorAlert))
	}

	// Replace the file with one that doesn't override alert.
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(`{"ui": {"warning": "#bbbbbb"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadTheme(); err != nil {
		t.Fatalf("second LoadTheme: %v", err)
	}
	if ColorAlert != uiDefaults["alert"] {
		t.Errorf("second call: ColorAlert = %q, want default %q", string(ColorAlert), string(uiDefaults["alert"]))
	}
	if string(ColorWarning) != "#bbbbbb" {
		t.Errorf("second call: ColorWarning = %q, want #bbbbbb", string(ColorWarning))
	}
}
