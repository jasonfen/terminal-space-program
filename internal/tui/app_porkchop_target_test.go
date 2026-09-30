package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func bodyIdx(t *testing.T, a *App, id string) int {
	t.Helper()
	for i, b := range a.world.System().Bodies {
		if b.ID == id {
			return i
		}
	}
	t.Fatalf("no body %q in system", id)
	return 0
}

// #502: P plots the TARGET body, not the body the h/l cursor is on.
func TestPorkchopOpensForTargetBody(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	venus := bodyIdx(t, a, "venus")
	a.active = screenOrbit
	a.selectedBody = bodyIdx(t, a, "mars") // cursor elsewhere
	a.world.SetTargetBody(venus)

	pressKey(a, 'P')

	if a.active != screenPorkchop {
		t.Fatalf("active = %v, want porkchop (statusMsg %q)", a.active, a.statusMsg)
	}
	if got := a.porkchop.TargetIdx(); got != venus {
		t.Errorf("porkchop target idx = %d, want Venus %d", got, venus)
	}
}

func TestPorkchopRefusesVesselTarget(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.active = screenOrbit
	a.selectedBody = bodyIdx(t, a, "venus")
	a.world.Target = sim.Target{Kind: sim.TargetCraft, CraftID: 99}

	pressKey(a, 'P')

	want := "porkchop: target is not a planet, press t"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
	if a.active != screenOrbit {
		t.Errorf("active = %v, want orbit on a refused P", a.active)
	}
}

// Targets that share the vessel's system are H trips; say so in one phrase.
func TestPorkchopRefusesSameSystemTarget(t *testing.T) {
	for _, id := range []string{"earth", "moon"} {
		a, err := New(nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		a.active = screenOrbit
		a.world.SetTargetBody(bodyIdx(t, a, id))

		pressKey(a, 'P')

		want := "porkchop: same system as your orbit, use H"
		if a.statusMsg != want || a.active != screenOrbit {
			t.Errorf("%s: statusMsg = %q active = %v, want refusal %q", id, a.statusMsg, a.active, want)
		}
	}
}
