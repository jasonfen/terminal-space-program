package save_test

import (
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #601: the navball frame survives save/load, and loading above the ascent
// line with a manual SURF does not replay the SURF to ORBIT switch (the edge
// memory is transient and re-seeds silently).
func TestNavFrameSurvivesSaveLoadWithoutReplayingASwitch(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "earth",
		Launchpad: true, Latitude: 28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.Tick() // seeds below the line, NavMode SURF
	c := w.ActiveCraft()
	up := w.NavAutoAscentAltitudeFor(c.Primary)
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + up*1.5)
	c.Landed, c.OnPad = false, false
	// Player is above the line but chose SURF by hand; no Tick yet, so the
	// pre-save world has not auto-switched.
	w.NavMode = sim.NavSurface

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	w2, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if w2.NavMode != sim.NavSurface {
		t.Fatalf("loaded NavMode = %v, want SURF kept", w2.NavMode)
	}
	w2.Tick()
	w2.Tick()
	if w2.NavMode != sim.NavSurface {
		t.Errorf("load replayed a switch: NavMode = %v, want SURF", w2.NavMode)
	}
}
