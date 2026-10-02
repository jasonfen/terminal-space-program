package save_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Schema v14 (Wave B review MEDIUM 2): a vessel's standing Rendezvous Plan
// (the last Rendezvous Burn planted from the picker, kept after the node
// fires) round-trips, so a reloaded save's TARGET chip still reads the plan.
func TestRoundtripRendezvousPlan(t *testing.T) {
	if save.SchemaVersion != 14 {
		t.Fatalf("SchemaVersion = %d, want 14", save.SchemaVersion)
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	base := w.Clock.SimTime
	want := &spacecraft.RendezvousPlan{
		NodeID:           7,
		TriggerTime:      base.Add(5 * time.Minute),
		ArrivalTime:      base.Add(3*time.Hour + 18*time.Minute),
		SeparationM:      42.5,
		TargetCraftID:    3,
		TargetGhostOwner: "",
	}
	w.ActiveCraft().RendezvousPlan = want
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := got.ActiveCraft().RendezvousPlan
	if p == nil {
		t.Fatal("RendezvousPlan lost in round-trip")
	}
	if p.NodeID != 7 || p.TargetCraftID != 3 || p.SeparationM != 42.5 ||
		!p.TriggerTime.Equal(want.TriggerTime) || !p.ArrivalTime.Equal(want.ArrivalTime) {
		t.Errorf("RendezvousPlan = %+v, want %+v", *p, *want)
	}
}

// A v13 envelope (no rendezvous_plan key) loads with no plan.
func TestMigrateV13ToV14_NoPlan(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(string(raw), `"version": 14`, `"version": 13`, 1)
	if s == string(raw) {
		t.Fatal("could not build the v13 fixture")
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("v13 save refused: %v", err)
	}
	if got.ActiveCraft().RendezvousPlan != nil {
		t.Error("v13 save loaded with a plan")
	}
}
