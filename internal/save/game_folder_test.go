package save_test

// New file: save_test.go is over the ~600-line rule (v0.63.3 game folders).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Guard 2 (load side): the game folder survives a save and a load.
func TestGameFolderRoundTrips(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.GameFolder = "2026-10-27-1843-2"
	if got := saveLoad(t, w).GameFolder; got != w.GameFolder {
		t.Errorf("GameFolder after save/load = %q, want %q", got, w.GameFolder)
	}
}

// Guard 3: a v18 save has no game_folder key and loads with no folder, so the
// App starts a new one.
func TestV18SaveLoadsWithNoGameFolder(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.GameFolder = "2026-10-27-1843"
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.Contains(s, `"game_folder"`) { // the instrument can see the key
		t.Fatal("setup: written save has no game_folder key")
	}
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if !strings.Contains(ln, `"game_folder"`) {
			out = append(out, ln)
		}
	}
	s = strings.Replace(strings.Join(out, "\n"), `"version": 19`, `"version": 18`, 1)
	if !strings.Contains(s, `"version": 18`) {
		t.Fatal("setup: version line not rewritten")
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := save.Load(path)
	if err != nil {
		t.Fatalf("v18 load: %v", err)
	}
	if loaded.GameFolder != "" {
		t.Errorf("GameFolder = %q, want empty", loaded.GameFolder)
	}
}
