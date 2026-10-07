package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// #566 follow-up, the docking lesson from a fresh start: spawn the default
// partner (press n, Enter, nothing changed), target it from the seed, and
// the pair needs no plane change and a rendezvous that fits the S-IVB-1's
// Δv. Before the INCLINATION row the partner was 51.6 deg off-plane
// (plane match 6630 m/s against 6129 m/s of Δv).
func TestFreshStartDefaultPartnerDockingLessonIsAffordable(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world
	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	pressMsg(a, tea.KeyMsg{Type: tea.KeyEnter})
	if len(w.Crafts) != 2 {
		t.Fatalf("no partner spawned (crafts=%d)", len(w.Crafts))
	}
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	seed := w.ActiveCraft()
	budget := seed.RemainingDeltaV()

	if _, err := w.PlanVesselPlaneMatch(); !errors.Is(err, planner.ErrInclinationNoOp) {
		t.Errorf("plane match against the default partner: err=%v, want ErrInclinationNoOp (already coplanar)", err)
	}
	ladder, err := w.RecommendRendezvousLadder(planner.RendezvousTheirOrbit)
	if err != nil {
		t.Fatalf("rendezvous ladder from the seed to the default partner: %v", err)
	}
	best := -1.0
	for _, r := range ladder.Rows {
		if r.Ok && (best < 0 || r.DV < best) {
			best = r.DV
		}
	}
	t.Logf("default partner: plane match = no-op; cheapest Ok rendezvous row = %.1f m/s (seed Δv %.0f m/s, %d rows)", best, budget, len(ladder.Rows))
	if best < 0 {
		t.Fatalf("no Ok rendezvous row: %+v", ladder.Rows)
	}
	if best > budget/2 {
		t.Errorf("cheapest rendezvous row %.0f m/s is over half the seed's Δv %.0f", best, budget)
	}
}
