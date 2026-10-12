package save_test

import (
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #594: the standing "done" row survives save/load with NO new persisted
// field, and a load never replays the completion flash.
func TestRungDoneSurvivesSaveLoad(t *testing.T) {
	if save.SchemaVersion != 14 {
		t.Fatalf("SchemaVersion = %d, #594 must not change the save shape (want 14)", save.SchemaVersion)
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	w.RecordAction(missions.ActionCycleView)
	w.Tick()
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.Target = spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: i}
		}
	}
	w.RecordAction(missions.ActionCycleTarget)
	w.Tick()
	if !w.RungDoneFlash() {
		t.Fatal("precondition: flash should be armed right after Orientation")
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	if name, ok := got.LastFinishedRung(); !ok || name != "Flight School: Orientation" {
		t.Errorf("after load LastFinishedRung = %q, %v; want Orientation", name, ok)
	}
	for i := 0; i < 20; i++ {
		got.Tick()
	}
	if got.RungDoneFlash() {
		t.Error("a load (or ticks after it) replayed the completion flash")
	}
}
