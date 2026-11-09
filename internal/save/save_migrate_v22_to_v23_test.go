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

// One-line reason for a new file: one migration per test file, as v21 -> v22.

func twoCraftWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatal(err)
	}
	w.SetActiveCraftIdx(0)
	return w
}

// Schema v23 (ADR 0067 P22): two vessels on different frames round-trip.
func TestNavFramePerVesselRoundTrips(t *testing.T) {
	w := twoCraftWorld(t)
	w.NavMode = sim.NavSurface // A
	w.SetActiveCraftIdx(1)
	w.NavMode = sim.NavOrbit // B
	w.SetActiveCraftIdx(0)
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var f save.File
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Payload.NavMode != 0 {
		t.Errorf("v23 still writes the envelope nav_mode = %d", f.Payload.NavMode)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.NavMode != sim.NavSurface {
		t.Errorf("active A loads on %v, want SURFACE", got.NavMode)
	}
	got.SetActiveCraftIdx(1)
	if got.NavMode != sim.NavOrbit {
		t.Errorf("B loads on %v, want ORBIT", got.NavMode)
	}
	got.SetActiveCraftIdx(0)
	if got.NavMode != sim.NavSurface {
		t.Errorf("A after switching back: %v, want SURFACE", got.NavMode)
	}
}

// A v22 envelope's nav_mode moves onto the ACTIVE vessel; the others start on
// ORBIT. A v22 save with no nav_mode (ORBIT, omitted) stays ORBIT everywhere.
func TestV22NavModeMigratesToActiveVessel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     int // envelope nav_mode in the v22 file; 0 = key omitted
		active  int
		want    sim.NavMode
		engaged bool // the OTHER vessel carries an engaged landing autopilot
	}{
		{"surface on active 1", int(sim.NavSurface), 1, sim.NavSurface, false},
		{"omitted", 0, 1, sim.NavOrbit, false},
		{"surface on active 0 (boundary)", int(sim.NavSurface), 0, sim.NavSurface, false},
		{"other vessel's engaged landing starts on SITE", 0, 1, sim.NavOrbit, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := twoCraftWorld(t)
			w.SetActiveCraftIdx(tc.active)
			if tc.engaged { // the old switch-time snap, now applied once by the migration
				w.Crafts[1-tc.active].Landing = &spacecraft.LandingAutopilot{Engaged: true}
			}
			path := filepath.Join(t.TempDir(), "save.json")
			if err := save.Save(w, path); err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(path)
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			m["version"] = 22
			pl := m["payload"].(map[string]any)
			delete(pl, "nav_mode")
			if tc.env != 0 {
				pl["nav_mode"] = tc.env
			}
			for _, c := range pl["crafts"].([]any) {
				delete(c.(map[string]any), "nav_mode")
			}
			raw, _ = json.Marshal(m)
			if strings.Contains(string(raw), `"nav_mode":0`) {
				t.Fatal("setup")
			}
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := save.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got.ActiveCraftIdx != tc.active || got.NavMode != tc.want {
				t.Errorf("active %d frame %v, want %d / %v", got.ActiveCraftIdx, got.NavMode, tc.active, tc.want)
			}
			other := 1 - tc.active
			wantStored := int(sim.NavOrbit)
			if tc.engaged {
				wantStored = int(sim.NavSite)
			}
			if got.Crafts[other].NavFrame != wantStored {
				t.Errorf("other vessel stored frame %d, want %d", got.Crafts[other].NavFrame, wantStored)
			}
			got.SetActiveCraftIdx(other)
			if got.NavMode != sim.NavOrbit {
				t.Errorf("other vessel on %v, want ORBIT", got.NavMode)
			}
		})
	}
}
