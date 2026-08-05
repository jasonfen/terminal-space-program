package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func pressO(a *App) { a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}}) }

// TestProximityKeyTogglesAndAnnounces drives the real keyboard dispatch:
// `o` enters the view and `o` again returns to the map, and BOTH halves
// say so. A view change the player asked for that produced no words at
// all is the failure mode this repo has already shipped once.
func TestProximityKeyTogglesAndAnnounces(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)
	a.world.ViewMode = sim.ViewTop

	pressO(a)
	if a.world.ViewMode != sim.ViewProximity {
		t.Fatalf("after [o]: ViewMode = %s, want proximity", a.world.ViewMode)
	}
	if !strings.Contains(a.statusMsg, "proximity view") {
		t.Errorf("entering said %q, want a proximity-view toast", a.statusMsg)
	}

	pressO(a)
	if a.world.ViewMode != sim.ViewTop {
		t.Errorf("after the second [o]: ViewMode = %s, want top (the view we jumped from)", a.world.ViewMode)
	}
	if !strings.Contains(a.statusMsg, "proximity view") {
		t.Errorf("leaving said %q, want a proximity-view toast", a.statusMsg)
	}
}

// TestProximityKeyRefusalIsVisible: pressed with a body target, the key
// must explain itself and leave the view alone. Silent no-ops read as
// broken keys.
func TestProximityKeyRefusalIsVisible(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.SetTargetBody(1)
	a.world.ViewMode = sim.ViewTilted

	pressO(a)
	if a.world.ViewMode != sim.ViewTilted {
		t.Errorf("a refused jump moved the camera: ViewMode = %s", a.world.ViewMode)
	}
	if a.statusMsg == "" {
		t.Fatal("refusal produced no toast")
	}
	if !strings.Contains(a.statusMsg, "VESSEL target") {
		t.Errorf("refusal toast %q doesn't name the missing thing", a.statusMsg)
	}
}

// TestProximityKeyIsOrbitScreenOnly: `o` is a map-screen binding; on
// another screen it must not reach through and change the ViewMode
// underneath (`o` is the porkchop screen's own options key).
func TestProximityKeyIsOrbitScreenOnly(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)
	a.world.ViewMode = sim.ViewTilted
	a.active = screenMissions

	pressO(a)
	if a.world.ViewMode != sim.ViewTilted {
		t.Errorf("[o] on the missions screen changed the ViewMode to %s", a.world.ViewMode)
	}
}
