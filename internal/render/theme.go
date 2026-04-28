package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
)

// Theme is the on-disk shape of the user's optional theme overlay,
// loaded from $XDG_CONFIG_HOME/terminal-space-program/theme.json
// (or ~/.config/... if XDG is unset). Both blocks are optional;
// missing keys fall through to v0.7.1's per-body Color JSON field
// and the legacy bodyPalette table.
//
// Keys in UI match the lower-cased name of the corresponding
// package-level Color* var (without the "Color" prefix). Keys in
// Bodies match each body's id from systems/*.json.
type Theme struct {
	UI     map[string]string `json:"ui,omitempty"`
	Bodies map[string]string `json:"bodies,omitempty"`
}

// bodyOverrides holds the per-body color overrides applied at
// startup by LoadTheme. Read by ColorFor before the per-body Color
// field and the bodyPalette table fallback.
var bodyOverrides map[string]string

// uiDefaults preserves the package-default UI-tier colors so a
// later LoadTheme call (e.g. in tests) can reset to defaults
// without restarting the process.
var uiDefaults = map[string]lipgloss.Color{
	"alert":         ColorAlert,
	"warning":       ColorWarning,
	"plannednode":   ColorPlannedNode,
	"trajectory":    ColorTrajectory,
	"currentorbit":  ColorCurrentOrbit,
	"craftmarker":   ColorCraftMarker,
	"foreignsoi":    ColorForeignSOI,
	"dim":           ColorDim,
}

// LoadTheme reads the user theme overlay (if present) and applies
// it to package-level state. UI overrides mutate the Color* vars in
// place; body overrides land in bodyOverrides for ColorFor to
// consult. Returns the parsed Theme (nil if no file) plus any
// LoadWarnings (parse / I/O failures).
//
// Idempotent — calling twice with the same file produces the same
// final state. Pre-existing UI defaults are restored before
// applying overrides so a second call with a smaller theme doesn't
// keep stale overrides from the first.
func LoadTheme() (*Theme, []LoadWarning, error) {
	resetUIDefaults()
	bodyOverrides = nil

	path := userThemePath()
	if path == "" {
		return nil, nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, []LoadWarning{{Path: path, Err: err}}, nil
	}
	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, []LoadWarning{{Path: path, Err: err}}, nil
	}
	applyUIOverrides(t.UI)
	if len(t.Bodies) > 0 {
		bodyOverrides = make(map[string]string, len(t.Bodies))
		for k, v := range t.Bodies {
			bodyOverrides[k] = v
		}
	}
	return &t, nil, nil
}

// LoadWarning is the render-package counterpart to bodies.LoadWarning
// for theme-overlay parse failures. Defined locally so this package
// doesn't import bodies just to reuse the type.
type LoadWarning struct {
	Path string
	Err  error
}

func (w LoadWarning) Error() string {
	return fmt.Sprintf("%s: %v", w.Path, w.Err)
}

func resetUIDefaults() {
	ColorAlert = uiDefaults["alert"]
	ColorWarning = uiDefaults["warning"]
	ColorPlannedNode = uiDefaults["plannednode"]
	ColorTrajectory = uiDefaults["trajectory"]
	ColorCurrentOrbit = uiDefaults["currentorbit"]
	ColorCraftMarker = uiDefaults["craftmarker"]
	ColorForeignSOI = uiDefaults["foreignsoi"]
	ColorDim = uiDefaults["dim"]
}

func applyUIOverrides(ui map[string]string) {
	for k, v := range ui {
		c := lipgloss.Color(v)
		switch k {
		case "alert":
			ColorAlert = c
		case "warning":
			ColorWarning = c
		case "plannednode":
			ColorPlannedNode = c
		case "trajectory":
			ColorTrajectory = c
		case "currentorbit":
			ColorCurrentOrbit = c
		case "craftmarker":
			ColorCraftMarker = c
		case "foreignsoi":
			ColorForeignSOI = c
		case "dim":
			ColorDim = c
		}
	}
}

func userThemePath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "theme.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "terminal-space-program", "theme.json")
}
