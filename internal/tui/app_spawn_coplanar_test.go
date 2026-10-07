package tui

import (
	"math"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func planeAngleBetween(a, b *spacecraft.Spacecraft) float64 {
	na := a.State.R.Cross(a.State.V).Unit()
	nb := b.State.R.Cross(b.State.V).Unit()
	d := na.Dot(nb)
	if d > 1 {
		d = 1
	} else if d < -1 {
		d = -1
	}
	return math.Acos(d) * 180 / math.Pi
}

// #566 follow-up: the spawn form's DEFAULT orbit partner is coplanar with
// the default seed (same inclination AND same node line), so a fresh
// player can rendezvous and dock without a plane change. Goes through the
// real 'n' handler and Enter, changing nothing in the form.
func TestSpawnFormDefaultPartnerIsCoplanarWithSeed(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	seed := a.world.ActiveCraft()
	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if a.active != screenSpawn {
		t.Fatalf("'n' did not open the spawn form")
	}
	pressMsg(a, tea.KeyMsg{Type: tea.KeyEnter})
	if len(a.world.Crafts) != 2 || a.world.ActiveCraft() == seed {
		t.Fatalf("Enter did not spawn a partner (crafts=%d)", len(a.world.Crafts))
	}
	if got := planeAngleBetween(seed, a.world.ActiveCraft()); got >= 0.01 {
		t.Errorf("default partner is %.4f deg off the seed's plane, want < 0.01", got)
	}
}
