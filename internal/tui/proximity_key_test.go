package tui

import (
	"regexp"
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

func proximityApp(t *testing.T) *App {
	t.Helper()
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
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	pressO(a)
	if a.world.ViewMode != sim.ViewProximity {
		t.Fatalf("setup: not in the proximity view (%s)", a.world.ViewMode)
	}
	return a
}

// Review MEDIUM 3 (ADR 0052 decision 2 stands: plain arrows trim in every
// flight view, Proximity included). The view must make a trim press visible
// and name the keys: an arrow press changes the trim reading on the screen,
// and the footer row advertises [←→↑↓] trim. Goes through App.Update.
func TestProximityViewShowsTheTrimItsArrowsMove(t *testing.T) {
	a := proximityApp(t)
	before := a.View()
	if !strings.Contains(before, "[←→↑↓] trim") {
		t.Errorf("proximity footer does not name the arrow trims:\n%s", before)
	}
	if !regexp.MustCompile(`trim:\s+\+0°`).MatchString(before) {
		t.Fatalf("proximity view shows no trim reading at rest:\n%s", before)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := a.world.ActiveCraft().PitchTrim; got == 0 {
		t.Fatal("setup: the arrow did not trim at all")
	}
	after := a.View()
	if !regexp.MustCompile(`trim:\s+\+5°`).MatchString(after) {
		t.Errorf("a → press changed no visible trim reading in the proximity view:\n%s", after)
	}
	if before == after {
		t.Error("the proximity frame is identical before and after the arrow press")
	}
	// The heading half too.
	hdgBefore := regexp.MustCompile(`hdg:\s+\S+`).FindString(after)
	a.Update(tea.KeyMsg{Type: tea.KeyUp})
	hdgAfter := regexp.MustCompile(`hdg:\s+\S+`).FindString(a.View())
	if hdgBefore == "" || hdgAfter == "" || hdgBefore == hdgAfter {
		t.Errorf("an ↑ press changed no visible heading reading: %q -> %q", hdgBefore, hdgAfter)
	}
}
