package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func pressU(a *App) tea.Cmd {
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	return cmd
}

// cargoApp docks a Cargo Dragon to a station built from the palette (a probe
// core, a Docking Port and, with hold, a Cargo Hold) in the App's world.
func cargoApp(t *testing.T, hold bool) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	w := a.World()
	ids := []string{"probe-core", "docking-port"}
	if hold {
		ids = append(ids, "cargo-hold")
	}
	st, warn := spacecraft.ComposeStage(ids, spacecraft.Components)
	if warn != "" {
		t.Fatal(warn)
	}
	station := spacecraft.NewFromStages([]spacecraft.Stage{st})
	station.Name = "Depot-1"
	dragon := spacecraft.NewFromLoadout("Cargo-Dragon")
	ref := w.ActiveCraft()
	for _, c := range []*spacecraft.Spacecraft{dragon, station} {
		c.Primary = ref.Primary
		c.State = ref.State
		c.State.M = c.TotalMass()
	}
	w.Crafts = []*spacecraft.Spacecraft{dragon, station}
	w.ActiveCraftIdx = 0
	w.DockCrafts(0, 1)
	w.RecomputeCommGraph()
	return a
}

// ADR 0057 5.4: the flash states the mass in the readout contract's words; the
// refusals state the vessel's state and nothing else.
func TestUKeyFlashes(t *testing.T) {
	a := cargoApp(t, true)
	if cmd := pressU(a); cmd != nil {
		t.Errorf("u on a solo stack produced a command %T", cmd())
	}
	if a.statusMsg != "3.307 t cargo handed over" {
		t.Errorf("flash = %q, want %q", a.statusMsg, "3.307 t cargo handed over")
	}
	pressU(a)
	if a.statusMsg != "no cargo aboard" {
		t.Errorf("second u flash = %q, want no cargo aboard", a.statusMsg)
	}

	a = cargoApp(t, false)
	pressU(a)
	if a.statusMsg != "no hold aboard" {
		t.Errorf("no-hold flash = %q", a.statusMsg)
	}

	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	pressU(a)
	if a.statusMsg != "not docked" {
		t.Errorf("not-docked flash = %q", a.statusMsg)
	}
}

// Conflict 2a: riding in someone else's stack, the Dragon's owner presses u
// and the ask goes up to the serve wrapper; nothing is decided in this World.
func TestUKeyAsRiderEmitsTheIntent(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a.World().DockGuest = &sim.DockGuestLink{OwnerFP: "SHA256:host", GuestCraftID: 9}
	cmd := pressU(a)
	if cmd == nil {
		t.Fatal("rider u produced no intent")
	}
	if _, ok := cmd().(HandOverCargoMsg); !ok {
		t.Fatalf("rider u emitted %T, want HandOverCargoMsg", cmd())
	}
	if a.statusMsg != "" {
		t.Errorf("rider u flashed %q locally", a.statusMsg)
	}
}

// u is not U: a missed shift undocks (U has no card), a lowercase u never does.
func TestLowercaseUDoesNotUndock(t *testing.T) {
	a := cargoApp(t, true)
	pressU(a)
	if len(a.World().Crafts) != 1 {
		t.Errorf("u split the stack into %d vessels", len(a.World().Crafts))
	}
}
