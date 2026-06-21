package save_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestLoadV8SaveReseedsMissions — ADR 0025 soft save break. A pre-v0.21
// (v8) save carries old single-predicate mission progress; loading it
// under v9 drops that progress and reseeds the new ladder from the
// catalog, so a previously "passed" mission comes back fresh in the new
// nested shape.
func TestLoadV8SaveReseedsMissions(t *testing.T) {
	// Isolate from any real user overlay so the reseed is exactly the
	// embedded starter catalog.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Rewrite the freshly-saved (v9) file to look like a pre-v0.21 v8
	// save: downgrade the version and replace the mission array with the
	// old single-predicate shape (type/params/status, no objectives), with
	// the mission already Passed.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	doc["version"] = 8
	payload := doc["payload"].(map[string]any)
	payload["missions"] = []any{
		map[string]any{
			"id":     "leo-circularize-1000",
			"name":   "Old circularize",
			"type":   "circularize",
			"status": 1, // Passed, in the old shape
			"params": map[string]any{"primary_id": "earth", "altitude_m": 1000000},
		},
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load v8 save: %v", err)
	}

	// Reseeded to the new nested catalog: every starter mission present,
	// each with objectives, none carrying the dropped Passed progress.
	base, _ := missions.DefaultCatalog()
	if len(got.Missions) != len(base.Missions) {
		t.Fatalf("reseed: got %d missions, want %d embedded starter missions", len(got.Missions), len(base.Missions))
	}
	for _, m := range got.Missions {
		if len(m.Objectives) == 0 {
			t.Errorf("reseeded mission %q has no objectives (old husk leaked through)", m.ID)
		}
		if m.Status != missions.InProgress {
			t.Errorf("reseeded mission %q status = %v, want InProgress (old progress not dropped)", m.ID, m.Status)
		}
	}
}

// TestRoundtripV9MissionShape — a v9 save preserves the full nested
// mission shape: program tag, requires edges, ordered objectives, and
// per-objective status all survive Save→Load.
func TestRoundtripV9MissionShape(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{{
		ID:          "rt-multi",
		Name:        "Round trip",
		Description: "two ordered steps",
		Program:     "test-prog",
		Requires:    []string{"leo-circularize-1000"},
		Objectives: []missions.Objective{
			{Kind: missions.KindSOIFlyby, Params: missions.Params{PrimaryID: "moon"}, Status: missions.Passed},
			{Kind: missions.KindSOIFlyby, Params: missions.Params{PrimaryID: "mars"}, Status: missions.InProgress},
		},
		Status: missions.InProgress,
	}}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Missions) != 1 {
		t.Fatalf("got %d missions, want 1", len(got.Missions))
	}
	m := got.Missions[0]
	if m.ID != "rt-multi" || m.Program != "test-prog" {
		t.Errorf("metadata lost: id=%q program=%q", m.ID, m.Program)
	}
	if len(m.Requires) != 1 || m.Requires[0] != "leo-circularize-1000" {
		t.Errorf("requires lost: %v", m.Requires)
	}
	if len(m.Objectives) != 2 {
		t.Fatalf("got %d objectives, want 2", len(m.Objectives))
	}
	if m.Objectives[0].Kind != missions.KindSOIFlyby || m.Objectives[0].Params.PrimaryID != "moon" {
		t.Errorf("objective 0 lost: %+v", m.Objectives[0])
	}
	if m.Objectives[0].Status != missions.Passed {
		t.Errorf("objective 0 status: got %v, want Passed", m.Objectives[0].Status)
	}
	if m.Objectives[1].Status != missions.InProgress {
		t.Errorf("objective 1 status: got %v, want InProgress", m.Objectives[1].Status)
	}
}
