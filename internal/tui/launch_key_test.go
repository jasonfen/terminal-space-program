package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

func pressV(a *App) { a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}}) }

// TestLaunchKeyTogglesAndAnnounces drives the real keyboard dispatch:
// `V` enters the launch/surface view and `V` again returns to the map,
// and BOTH halves say so — same discipline TestProximityKeyTogglesAndAnnounces
// pins for the sibling toggle key `o`.
func TestLaunchKeyTogglesAndAnnounces(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.ViewMode = sim.ViewTop

	pressV(a)
	if a.world.ViewMode != sim.ViewLaunch {
		t.Fatalf("after [V]: ViewMode = %s, want launch", a.world.ViewMode)
	}
	if !strings.Contains(a.statusMsg, "launch view") {
		t.Errorf("entering said %q, want a launch-view toast", a.statusMsg)
	}

	pressV(a)
	if a.world.ViewMode != sim.ViewTop {
		t.Errorf("after the second [V]: ViewMode = %s, want top (the view we jumped from)", a.world.ViewMode)
	}
}

// TestLaunchKeyRefusalIsVisible: pressed with no active vessel, the key
// must explain itself and leave the view alone. Silent no-ops read as
// broken keys (the same lesson ProximityView's refusal already applies).
func TestLaunchKeyRefusalIsVisible(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Crafts = nil
	a.world.ActiveCraftIdx = 0
	a.world.ViewMode = sim.ViewTilted

	pressV(a)
	if a.world.ViewMode != sim.ViewTilted {
		t.Errorf("a refused jump moved the camera: ViewMode = %s", a.world.ViewMode)
	}
	if a.statusMsg == "" {
		t.Fatal("refusal produced no toast")
	}
	if !strings.Contains(a.statusMsg, "no vessel") {
		t.Errorf("refusal toast %q doesn't name the missing thing", a.statusMsg)
	}
}

// TestLaunchKeyIsOrbitScreenOnly: `V` is a map-screen binding; on
// another screen it must not reach through and change the ViewMode
// underneath — mirrors TestProximityKeyIsOrbitScreenOnly.
func TestLaunchKeyIsOrbitScreenOnly(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.ViewMode = sim.ViewTilted
	a.active = screenMissions

	pressV(a)
	if a.world.ViewMode != sim.ViewTilted {
		t.Errorf("[V] on the missions screen changed the ViewMode to %s", a.world.ViewMode)
	}
}

// TestLaunchKeyReturnsFromAutoRoute drives the real dispatch for the
// "auto-route-on-liftoff still works, and the toggle key returns from
// it" requirement: a launchpad spawn routes into ViewLaunch on the next
// Tick with no key press at all, and `V` still works as the way back out
// even though it didn't open that session itself.
func TestLaunchKeyReturnsFromAutoRoute(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	priorView := a.world.ViewMode

	if _, err := a.world.SpawnCraft(sim.SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}
	a.world.Tick()
	if a.world.ViewMode != sim.ViewLaunch {
		t.Fatalf("precondition: auto-route did not fire, ViewMode = %s", a.world.ViewMode)
	}

	pressV(a)
	if a.world.ViewMode != priorView {
		t.Errorf("ViewMode = %s, want %s (the view the auto-route entered from)", a.world.ViewMode, priorView)
	}
}

// TestLaunchAndProximityToggleRenderAtMinTerminalSize: both toggling
// jump keys must render cleanly at the floor size the game actually
// supports (104×24, screens.MinTerminalWidth/Height) — the round trip
// touches OrbitView's, LaunchView's and Proximity's Framing-Event/canvas
// machinery, and a panic or a garbled frame there would only show up
// below the sizes developers usually run at.
func TestLaunchAndProximityToggleRenderAtMinTerminalSize(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var m tea.Model = a
	m, _ = m.Update(tea.WindowSizeMsg{Width: screens.MinTerminalWidth, Height: screens.MinTerminalHeight})

	if _, err := a.world.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)

	pressV(a)
	_ = m.View()
	pressV(a)
	_ = m.View()

	pressO(a)
	_ = m.View()
	pressO(a)
	_ = m.View()

	if a.world.ViewMode == sim.ViewLaunch || a.world.ViewMode == sim.ViewProximity {
		t.Errorf("ended the round trip still inside a jump view: %s", a.world.ViewMode)
	}
}
