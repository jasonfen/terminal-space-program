package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func visibleIDs(s *SpawnCraft) map[string]bool {
	m := map[string]bool{}
	for _, id := range s.orderedLoadoutIDs() {
		m[id] = true
	}
	return m
}

// TestSystemFilterByScaleClass — the CRAFT TYPE catalog is filtered to the
// active system's Scale Class (ADR 0031 / S10, amending ADR 0014): a real
// system shows the real fleet and hides the stripped-back Kern Stack; a
// stripped-back system does the reverse.
func TestSystemFilterByScaleClass(t *testing.T) {
	real := NewSpawnCraft(Theme{})
	real.Reset(nil, "", nil, bodies.ScaleReal)
	rv := visibleIDs(real)
	if !rv["Saturn-V"] {
		t.Error("real system should show Saturn V")
	}
	if rv["Kern-Stack"] {
		t.Error("real system must hide the stripped-back Kern Stack")
	}

	strip := NewSpawnCraft(Theme{})
	strip.Reset(nil, "", nil, bodies.ScaleStrippedBack)
	sv := visibleIDs(strip)
	if !sv["Kern-Stack"] {
		t.Error("stripped-back system should show Kern Stack")
	}
	if sv["Saturn-V"] {
		t.Error("stripped-back system must hide the real-scale Saturn V")
	}
}

// TestUnsetSystemScaleShowsRealFleet — a system with no Scale Class (the empty
// value) normalizes to real, so it shows the real fleet (ADR 0031 / S10).
func TestUnsetSystemScaleShowsRealFleet(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "")
	v := visibleIDs(s)
	if !v["Saturn-V"] || v["Kern-Stack"] {
		t.Error("unset system scale should behave as real (show real, hide stripped-back)")
	}
}

// TestShowAllToggleRevealsAndRefilters — [f] flips the filter off (every craft
// visible, ADR 0014's escape hatch) and back on, and re-points the cursor to
// the top of CRAFT TYPE so it can't strand on a now-hidden row (ADR 0031 / S10).
func TestShowAllToggleRevealsAndRefilters(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, bodies.ScaleReal)
	if visibleIDs(s)["Kern-Stack"] {
		t.Fatal("precondition: Kern Stack hidden under the real filter")
	}

	s.HandleKey("f")
	if !s.showAll {
		t.Error("[f] should enable show-all")
	}
	all := visibleIDs(s)
	if !all["Kern-Stack"] || !all["Saturn-V"] {
		t.Error("show-all should reveal every craft (real and stripped-back)")
	}
	if s.loadoutIdx != 0 {
		t.Errorf("toggle should re-point the cursor to the top of the re-filtered list, got idx=%d", s.loadoutIdx)
	}

	s.HandleKey("f")
	if s.showAll {
		t.Error("[f] should toggle back to filtered")
	}
	if visibleIDs(s)["Kern-Stack"] {
		t.Error("re-filter should hide the Kern Stack again")
	}
}

// TestFilterToggleIgnoredInStackEditor — [f] is a no-op while focused on the
// Custom STACK editor, so it can't yank the player out mid-build (ADR 0031 /
// S11 review fix). Mirrors the field-guarding of the [a]/[x]/[d] stack keys.
func TestFilterToggleIgnoredInStackEditor(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, bodies.ScaleReal)
	// Select Custom and focus the STACK editor.
	s.fieldIdx = 0
	for !s.IsCustomSelected() {
		s.HandleKey("right")
	}
	s.fieldIdx = stackFieldIdx
	beforeShowAll := s.showAll
	s.HandleKey("f")
	if s.showAll != beforeShowAll {
		t.Error("[f] toggled the filter while in the STACK editor (should be a no-op)")
	}
	if s.fieldIdx != stackFieldIdx || !s.IsCustomSelected() {
		t.Errorf("[f] yanked focus out of the STACK editor (field=%d custom=%v)", s.fieldIdx, s.IsCustomSelected())
	}
}

// TestCustomAndDesignsExemptFromFilter — the Custom builder and saved designs
// are never filtered out; they remain selectable after the visible catalog
// (ADR 0031 / S10).
func TestCustomAndDesignsExemptFromFilter(t *testing.T) {
	designs := []spacecraft.Design{
		{Loadout: spacecraft.LoadoutDef{ID: "lumen-probe", Name: "Lumen Probe", Parts: []spacecraft.PartRef{{PartID: "x"}}}},
	}
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", designs, bodies.ScaleReal)
	v := s.visibleCatalogCount()
	s.loadoutIdx = v
	if !s.IsCustomSelected() {
		t.Error("Custom must remain selectable under the filter")
	}
	s.loadoutIdx = v + 1
	if !s.IsDesignSelected() || s.SelectedDesignID() != "lumen-probe" {
		t.Errorf("saved design must remain selectable under the filter (design=%v id=%q)",
			s.IsDesignSelected(), s.SelectedDesignID())
	}
}

// TestEmptyFilterDegradesGracefully — a scale class no loadout matches yields an
// empty catalog; the form must not panic and the Custom entry becomes the first
// row (ADR 0031 / S10).
func TestEmptyFilterDegradesGracefully(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, bodies.ScaleClass("exotic"))
	if got := s.visibleCatalogCount(); got != 0 {
		t.Fatalf("expected no catalog loadouts for an unknown scale, got %d", got)
	}
	if !s.IsCustomSelected() { // loadoutIdx 0 == visibleCatalogCount 0
		t.Error("with no catalog craft, the Custom entry should be the first row")
	}
	if s.SelectedLoadoutID() != "" {
		t.Errorf("no catalog craft selected, SelectedLoadoutID = %q, want empty", s.SelectedLoadoutID())
	}
	_ = s.Render(80) // must not panic
}
