package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// A guest App writes every autosave surface through the per-player
// sink and never touches the host's local saves (v0.27 S3, ADR 0034).
func TestGuestPersistenceRedirectsAutosave(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var sunk int
	a.SetGuestPersistence(func(w *sim.World) error { sunk++; return nil })

	a.autosave() // the quit-path write
	if sunk != 1 {
		t.Errorf("quit autosave: sink calls = %d, want 1", sunk)
	}
	saves := filepath.Join(stateDir, "terminal-space-program", "saves")
	if entries, err := os.ReadDir(saves); err == nil && len(entries) > 0 {
		t.Errorf("guest autosave leaked into the host saves dir: %v", entries)
	}
}

// F5/F9 are refused for guests with a pointer at the server-side
// autosave — no quicksave file appears.
func TestGuestQuicksaveRefused(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.SetGuestPersistence(func(w *sim.World) error { return nil })

	if err := a.doSave(); err == nil {
		t.Error("doSave succeeded for a guest; want refusal")
	} else if !strings.Contains(err.Error(), "autosaves server-side") {
		t.Errorf("doSave refusal message = %q", err)
	}
	if err := a.doLoad(); err == nil {
		t.Error("doLoad succeeded for a guest; want refusal")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "terminal-space-program", "saves")); err == nil {
		t.Error("a saves dir appeared for a guest session")
	}
}

// NewWithWorld builds the App around the given (restored) world.
func TestNewWithWorld(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	a, err := NewWithWorld(w)
	if err != nil {
		t.Fatalf("NewWithWorld: %v", err)
	}
	if a.World() != w {
		t.Error("App not built around the provided world")
	}
}
