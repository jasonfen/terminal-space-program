package save_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Audit L3-2: a loaded world lands on the same default tilt as a new game.
func TestLoadKeepsDefaultViewTilt(t *testing.T) {
	testSavesDir(t)
	info, err := save.WriteNamed(newTestWorld(t), "tilt")
	if err != nil {
		t.Fatal(err)
	}
	w, err := save.LoadID(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.ViewTilt != sim.DefaultViewTilt() {
		t.Errorf("ViewTilt after load = %+v, want %+v", w.ViewTilt, sim.DefaultViewTilt())
	}
}

// Audit L3-5: a save written against another catalog is flagged in the
// listing (header-only) and its load error names no raw hashes.
func TestListMarksCatalogMismatch(t *testing.T) {
	dir := testSavesDir(t)
	info, err := save.WriteNamed(newTestWorld(t), "old universe")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, info.ID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`"body_catalog_hash":\s*"[0-9a-f]+"`)
	if !re.Match(data) {
		t.Fatal("fixture has no body_catalog_hash to rewrite")
	}
	if err := os.WriteFile(path, re.ReplaceAll(data, []byte(`"body_catalog_hash":"deadbeef"`)), 0o644); err != nil {
		t.Fatal(err)
	}

	infos, err := save.List()
	if err != nil || len(infos) != 1 {
		t.Fatalf("List = %v, %v", infos, err)
	}
	if !infos[0].Unreadable || infos[0].Note != "other catalog" {
		t.Errorf("entry = unreadable %v note %q, want unreadable, \"other catalog\"", infos[0].Unreadable, infos[0].Note)
	}

	_, err = save.LoadID(info.ID)
	if err == nil || strings.Contains(err.Error(), "deadbeef") {
		t.Errorf("load error = %v, want one that omits the raw hashes", err)
	}
}
