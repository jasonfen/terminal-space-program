package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// The size gate replaces rendering below the playable floor and
// releases it the moment the terminal grows back (v0.27 S2).
func TestSizeGateEnterExit(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var m tea.Model = a

	// Exactly at the floor: the game renders, no gate.
	m, _ = m.Update(tea.WindowSizeMsg{Width: screens.MinTerminalWidth, Height: screens.MinTerminalHeight})
	if out := m.View(); strings.Contains(out, "TERMINAL TOO SMALL") {
		t.Errorf("gate shown at the floor size %d×%d", screens.MinTerminalWidth, screens.MinTerminalHeight)
	}

	// One column under: gated, and the prompt names the floor.
	m, _ = m.Update(tea.WindowSizeMsg{Width: screens.MinTerminalWidth - 1, Height: 40})
	out := m.View()
	if !strings.Contains(out, "TERMINAL TOO SMALL") {
		t.Fatalf("expected gate below min width, got:\n%s", out)
	}
	if !strings.Contains(out, "104×24") {
		t.Errorf("gate prompt should name the required floor, got:\n%s", out)
	}

	// One row under: gated too.
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: screens.MinTerminalHeight - 1})
	if !strings.Contains(m.View(), "TERMINAL TOO SMALL") {
		t.Error("expected gate below min height")
	}

	// Grow back: the game returns.
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if strings.Contains(m.View(), "TERMINAL TOO SMALL") {
		t.Error("gate did not release after resize above the floor")
	}
}

// Gameplay keys are swallowed while gated — a blind keypress must not
// mutate the world — but the sim keeps ticking underneath.
func TestSizeGateSwallowsGameplayKeys(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var m tea.Model = a
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// WarpUp while gated: dropped.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	if got := a.world.Clock.Warp(); got != 1 {
		t.Errorf("warp key acted while gated: warp = %v, want 1", got)
	}
	// Sim still advances while gated.
	before := a.world.Clock.SimTime
	m, _ = m.Update(sim.TickMsg(time.Now()))
	if !a.world.Clock.SimTime.After(before) {
		t.Error("sim clock frozen while gated; the gate should hold rendering, not time")
	}

	// After resizing up the same key works again.
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	if got := a.world.Clock.Warp(); got != 10 {
		t.Errorf("warp key dead after gate released: warp = %v, want 10", got)
	}
	_ = m
}

// The gate screen itself renders safely at hostile sizes.
func TestRenderSizeGateTinyTerminal(t *testing.T) {
	for _, sz := range [][2]int{{10, 3}, {1, 1}, {40, 8}} {
		out := screens.RenderSizeGate(sz[0], sz[1])
		lines := strings.Split(out, "\n")
		if len(lines) > sz[1] {
			t.Errorf("size %v: %d lines overflow height", sz, len(lines))
		}
		for _, l := range lines {
			if len([]rune(l)) > sz[0] {
				t.Errorf("size %v: line wider than terminal: %q", sz, l)
			}
		}
	}
}
