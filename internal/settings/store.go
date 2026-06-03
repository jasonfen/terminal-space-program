package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadWarning reports a non-fatal problem reading settings.json (a parse
// failure or an I/O error other than "file absent"). Defined locally so
// this package mirrors render.LoadWarning / bodies.LoadWarning without
// importing either. On any warning Load still returns a usable Settings
// (the all-on Default), so a corrupt file degrades to defaults rather
// than blocking startup.
type LoadWarning struct {
	Path string
	Err  error
}

func (w LoadWarning) Error() string {
	return fmt.Sprintf("%s: %v", w.Path, w.Err)
}

// Path returns the location of settings.json:
// $XDG_CONFIG_HOME/terminal-space-program/settings.json, falling back to
// ~/.config/... when XDG_CONFIG_HOME is unset — mirroring theme.json's
// userThemePath (internal/render/theme.go). Returns "" if the home
// directory cannot be resolved and XDG is unset.
func Path() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "terminal-space-program", "settings.json")
}

// Load reads settings.json and returns the player's preferences. A
// missing file is the common case and yields Default() (all Chips
// visible) with no warning. A present-but-partial file fills only the
// keys it names, leaving every other Chip at its visible default. A
// parse or I/O error yields Default() plus a LoadWarning, so the caller
// can surface it without the app losing a working configuration.
//
// Idempotent: Load reads a file and never mutates global state, so
// repeated calls with the same file return equivalent values.
func Load() (Settings, []LoadWarning) {
	path := Path()
	if path == "" {
		return Default(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Default(), []LoadWarning{{Path: path, Err: err}}
	}
	s := Default()
	if err := json.Unmarshal(data, &s); err != nil {
		return Default(), []LoadWarning{{Path: path, Err: err}}
	}
	return s, nil
}

// Save writes s to settings.json, creating parent directories as needed.
// Atomic on POSIX: writes a sibling tmpfile and renames it into place,
// mirroring save.Save (internal/save/save.go) so a crash mid-write can't
// leave a truncated config.
func Save(s Settings) error {
	path := Path()
	if path == "" {
		return fmt.Errorf("settings: cannot resolve config path")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmpfile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}
