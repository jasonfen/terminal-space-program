package save_test

// New file: save_test.go is over the ~600-line rule.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0062 decision 2: the liftoff instant survives a save to the second.
func TestLiftoffSimTimeRoundTrips(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2031, 3, 4, 5, 6, 7, 0, time.UTC)
	w.ActiveCraft().LiftoffSimTime = want
	got := saveLoad(t, w).ActiveCraft().LiftoffSimTime
	if !got.Equal(want) {
		t.Errorf("LiftoffSimTime after save/load = %v, want %v", got, want)
	}
}

// A v17 save has no liftoff key and loads with the zero instant.
func TestV17SaveLoadsWithZeroLiftoff(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.ActiveCraft().LiftoffSimTime = time.Date(2031, 3, 4, 5, 6, 7, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.Contains(s, `"liftoff_sim_time"`) { // the instrument can see the key
		t.Fatal("setup: written save has no liftoff_sim_time key")
	}
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if !strings.Contains(ln, `"liftoff_sim_time"`) {
			out = append(out, ln)
		}
	}
	s = strings.Join(out, "\n")
	s = strings.Replace(s, `"version": 18`, `"version": 17`, 1)
	if !strings.Contains(s, `"version": 17`) {
		t.Fatal("setup: version line not rewritten")
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := save.Load(path)
	if err != nil {
		t.Fatalf("v17 load: %v", err)
	}
	if !loaded.ActiveCraft().LiftoffSimTime.IsZero() {
		t.Errorf("LiftoffSimTime = %v, want zero", loaded.ActiveCraft().LiftoffSimTime)
	}
}
