package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// formShowAll returns a spawn form with the scale-class system filter OFF, so
// every catalog loadout is visible — the baseline for the grouping invariants
// (which are about all loadouts, independent of the S10 filter).
func formShowAll() *SpawnCraft {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "")
	s.showAll = true
	return s
}

// TestGroupedLoadoutsIsPermutationOfLoadoutOrder — grouping reorders the
// catalog into category groups but must neither drop, duplicate, nor invent a
// loadout: with the filter off, the flattened grouped order is a permutation of
// LoadoutOrder (ADR 0031 / S8). This invariant keeps the loadoutIdx arithmetic
// valid under grouping — same count, just reordered.
func TestGroupedLoadoutsIsPermutationOfLoadoutOrder(t *testing.T) {
	s := formShowAll()
	ids := s.orderedLoadoutIDs()
	if len(ids) != len(spacecraft.LoadoutOrder) {
		t.Fatalf("grouped order has %d ids, LoadoutOrder has %d", len(ids), len(spacecraft.LoadoutOrder))
	}
	seen := map[string]int{}
	for _, id := range ids {
		seen[id]++
	}
	for _, id := range spacecraft.LoadoutOrder {
		if seen[id] != 1 {
			t.Errorf("loadout %q appears %d times in grouped order, want exactly 1", id, seen[id])
		}
	}
}

// TestEveryEmbeddedLoadoutMapsToKnownCategory — no shipped loadout falls into
// the trailing "Other" bucket (ADR 0031 / S8). "Other" is the safety net for
// mod / future loadouts with an unknown category, not a home for the built-in
// fleet; if a built-in lands there, its category key is wrong or missing.
func TestEveryEmbeddedLoadoutMapsToKnownCategory(t *testing.T) {
	s := formShowAll()
	for _, g := range s.groupedLoadouts() {
		if g.label == craftCategoryOtherLabel {
			t.Errorf("built-in loadouts fell into %q: %v — assign each a known category",
				craftCategoryOtherLabel, g.ids)
		}
	}
}

// TestGroupedLoadoutsRespectCategoryOrder — groups appear in the craftCategories
// order, and within a group loadouts keep LoadoutOrder order. Pins the on-screen
// ordering contract (ADR 0031 / S8).
func TestGroupedLoadoutsRespectCategoryOrder(t *testing.T) {
	s := formShowAll()
	groups := s.groupedLoadouts()
	lastRank := -1
	for _, g := range groups {
		rank := len(craftCategories) // "Other" default — sorts last
		for i, c := range craftCategories {
			if c.label == g.label {
				rank = i
				break
			}
		}
		if rank < lastRank {
			t.Errorf("group %q (rank %d) appears after a higher-ranked group (%d)", g.label, rank, lastRank)
		}
		lastRank = rank
	}
	pos := map[string]int{}
	for i, id := range spacecraft.LoadoutOrder {
		pos[id] = i
	}
	for _, g := range groups {
		for i := 1; i < len(g.ids); i++ {
			if pos[g.ids[i-1]] > pos[g.ids[i]] {
				t.Errorf("group %q not in LoadoutOrder order: %q before %q", g.label, g.ids[i-1], g.ids[i])
			}
		}
	}
}

// TestSelectedLoadoutIDWalksGroupedOrder — cycling the CRAFT TYPE cursor across
// the catalog rows yields each loadout exactly once, in grouped order; the row
// after the last catalog loadout is the Custom entry (blank loadout id). The
// cursor never lands on a header — headers are not in the index space
// (ADR 0031 / S8).
func TestSelectedLoadoutIDWalksGroupedOrder(t *testing.T) {
	s := formShowAll()
	want := s.orderedLoadoutIDs()
	for i, id := range want {
		s.loadoutIdx = i
		if got := s.SelectedLoadoutID(); got != id {
			t.Errorf("loadoutIdx %d: SelectedLoadoutID = %q, want %q", i, got, id)
		}
	}
	s.loadoutIdx = len(want)
	if !s.IsCustomSelected() || s.SelectedLoadoutID() != "" {
		t.Errorf("index %d should be the Custom entry (blank loadout id); got %q (custom=%v)",
			len(want), s.SelectedLoadoutID(), s.IsCustomSelected())
	}
}

// TestCustomCyclingStillReachesCustomAndDesigns — the right-cycling path through
// every catalog row still lands on Custom (ADR 0031 / S8 must not regress the
// v0.24 design rows). With the filter off, Custom sits at len(LoadoutOrder).
func TestCustomCyclingStillReachesCustomAndDesigns(t *testing.T) {
	s := formShowAll()
	s.fieldIdx = 0
	for !s.IsCustomSelected() {
		s.HandleKey("right")
	}
	if s.loadoutIdx != len(spacecraft.LoadoutOrder) {
		t.Errorf("Custom landed at loadoutIdx %d, want %d", s.loadoutIdx, len(spacecraft.LoadoutOrder))
	}
}

// TestRenderShowsCategoryHeaders — the rendered form contains every non-empty
// category header and the trailing "Custom & Designs" header (render smoke;
// ADR 0031 / S8).
func TestRenderShowsCategoryHeaders(t *testing.T) {
	s := formShowAll()
	out := s.Render(100)
	for _, g := range s.groupedLoadouts() {
		if !strings.Contains(out, g.label) {
			t.Errorf("render missing category header %q", g.label)
		}
	}
	if !strings.Contains(out, "Custom & Designs") {
		t.Error("render missing the 'Custom & Designs' trailing header")
	}
}
