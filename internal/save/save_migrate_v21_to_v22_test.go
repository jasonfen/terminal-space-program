package save_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// One-line reason for a new file: one migration per test file, as v14 -> v15.

// Schema v22 (ADR 0067 decision 13): a booster mid-brake survives a reload,
// and a reload cannot re-roll the aim or navigation offsets.
func TestLandingAutopilotRoundTrips(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	want := spacecraft.LandingAutopilot{
		Engaged: true, Phase: spacecraft.PhaseApproach, ROD: true, RODRateMps: -2.5, ThrottleHeld: true,
		AimAlongM: 250, AimAcrossM: -25, NavAlongM: 310.5, NavAcrossM: -480, NavSeeded: true, NavFixed: true,
	}
	c := w.ActiveCraft()
	path := filepath.Join(t.TempDir(), "save.json")
	for _, nose := range []bool{true, false} { // fix E: absent in the file = the full autopilot
		want.Nose = nose
		cp := want
		c.Landing = &cp
		if err := save.Save(w, path); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(path)
		if has := strings.Contains(string(raw), `"nose"`); has != nose {
			t.Errorf("nose=%v but the file has the key: %v", nose, has)
		}
		got, err := save.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		l := got.ActiveCraft().Landing
		if l == nil || *l != want {
			t.Errorf("autopilot after reload = %+v, want %+v", l, want)
		}
	}
}

// A v21 envelope (no autopilot key) loads with the autopilot off, and one a
// hand-edit put in a v21 file is dropped: no v21 build wrote it.
func TestV21SaveLoadsWithNoAutopilot(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.ActiveCraft().Landing = &spacecraft.LandingAutopilot{Engaged: true, Phase: spacecraft.PhaseBrake}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var f save.File
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Payload.Crafts[f.Payload.ActiveCraftIdx].Landing == nil {
		t.Fatal("setup: the written v22 save carries no landing key")
	}
	f.Version = 21 // an old envelope that somehow holds one
	raw, _ = json.Marshal(f)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("v21 load: %v", err)
	}
	if l := got.ActiveCraft().Landing; l != nil {
		t.Errorf("v21 load kept an autopilot %+v, want none", *l)
	}
}
