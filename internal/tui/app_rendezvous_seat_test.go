package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// copilotApp puts an App in the terminal phase holding the copilot seat,
// with the initiator's clock published at 1000×.
func copilotApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world
	w.EngageRendezvousWarpAs("SHA256:guest", "gern", w.Clock.SimTime.Add(time.Hour), 6000, false)
	w.RendezvousArm.Approach = true
	w.RendezvousRate = sim.RendezvousRateState{
		Seat: sim.RendezvousSeatCopilot, Handle: "gern", PartnerRate: 1000,
	}
	return a
}

// In the copilot seat the warp keys brake and release rather than moving
// the player's own Selected Warp (ADR 0037 §2). Both press and refusal
// say what happened: #302's whole complaint was a key that appeared to do
// nothing for thirty minutes.
func TestCopilotWarpKeysBrakeAndRelease(t *testing.T) {
	a := copilotApp(t)
	w := a.world
	idx := w.Clock.WarpIdx

	// Up from the following seat is the one thing the copilot may not do.
	a.statusMsg = ""
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	if w.RendezvousArm.BrakeIdx != -1 {
		t.Errorf("[.] from the following seat set a brake: BrakeIdx = %d", w.RendezvousArm.BrakeIdx)
	}
	if w.Clock.WarpIdx != idx {
		t.Errorf("[.] moved the copilot's own Selected Warp: %d, want %d", w.Clock.WarpIdx, idx)
	}
	if !strings.Contains(a.statusMsg, "copilot") {
		t.Errorf("refusal %q does not name the seat", a.statusMsg)
	}

	// Down brakes the pair.
	a.statusMsg = ""
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	if w.RendezvousArm.BrakeIdx < 0 {
		t.Fatal("[,] did not brake the pair")
	}
	if w.Clock.WarpIdx != idx {
		t.Errorf("[,] moved the copilot's own Selected Warp: %d, want %d", w.Clock.WarpIdx, idx)
	}
	if !strings.Contains(a.statusMsg, "brak") {
		t.Errorf("brake toast %q does not say the pair was braked", a.statusMsg)
	}
	if w.RendezvousArm == nil {
		t.Fatal("[,] tore down the agreement")
	}
}

// The agreement must survive both keys — the release path routes through
// the same StepRendezvousBrake, never DisengageAutoWarp, so a copilot
// tapping warp can't cancel the rendezvous for both players (#249's
// lesson, applied to the new seat).
func TestCopilotWarpKeysNeverCancelTheAgreement(t *testing.T) {
	a := copilotApp(t)
	w := a.world
	w.StepRendezvousBrake(false)
	for i := 0; i < 12; i++ {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	}
	if w.RendezvousArm == nil || !w.RendezvousArm.Approach {
		t.Fatalf("warp keys ended the terminal-phase agreement: %+v", w.RendezvousArm)
	}
}

// The PILOT's warp keys are ordinary warp keys: they fly the clock.
func TestPilotWarpKeysMoveSelectedWarp(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world
	w.EngageRendezvousWarpAs("SHA256:guest", "gern", w.Clock.SimTime.Add(time.Hour), 6000, true)
	w.RendezvousArm.Approach = true
	w.RendezvousRate = sim.RendezvousRateState{Seat: sim.RendezvousSeatPilot, Handle: "gern"}

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	if w.Clock.WarpIdx != 1 {
		t.Errorf("pilot's [.] did not step Selected Warp: WarpIdx = %d", w.Clock.WarpIdx)
	}
	if w.RendezvousArm == nil {
		t.Fatal("pilot's warp key tore down the agreement")
	}
}
