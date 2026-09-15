package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestEmptySlateSaysSo recovers the retired #310 test (see git log -S on
// TestEmptySlateSaysSo; removed by the ADR 0051 box consolidation, which
// left every one of the eight boxes reading a bare dash row with no
// player-facing explanation or way out). The old VESSEL chip's message
// and its [n] way out — and the DockGuest variant's own exit — now render
// as a notice in the bay instead: the player must still be told the
// slate is empty and given the key that fixes it, and a docked-as-guest
// slate must never suggest a new launch, since that isn't the way out.
func TestEmptySlateSaysSo(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Crafts = nil
	w.ActiveCraftIdx = 0

	out := strings.Join(v.buildEmptySlateChip(w), "\n")
	if out == "" {
		t.Fatal("empty craft slate renders nothing, the state the player cannot decode")
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("empty-slate notice does not say the slate is empty:\n%s", out)
	}
	if !strings.Contains(out, "[n]") {
		t.Errorf("empty-slate notice offers no way out:\n%s", out)
	}

	// Docked as guest: the slate is empty for a reason we know, and
	// "launch a new flight" would be the wrong advice.
	w.DockGuest = &sim.DockGuestLink{OwnerFP: "SHA256:bob", OwnerHandle: "bob"}
	out = strings.Join(v.buildEmptySlateChip(w), "\n")
	if !strings.Contains(out, "bob") {
		t.Errorf("docked-as-guest empty slate does not name the stack:\n%s", out)
	}
	if strings.Contains(out, "[n]") {
		t.Errorf("docked-as-guest notice offers a new launch as if the craft were gone:\n%s", out)
	}
}

// TestEmptySlateChipNilWithActiveCraft: the notice must not appear while
// there IS an active craft — it names an absence, not a standing status.
func TestEmptySlateChipNilWithActiveCraft(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.ActiveCraft() == nil {
		t.Fatal("expected an active craft")
	}
	if lines := v.buildEmptySlateChip(w); lines != nil {
		t.Errorf("buildEmptySlateChip returned %v with an active craft, want nil", lines)
	}
}

// TestEmptySlateNoticeSurvivesDeclutter: an empty slate with no way to
// tell the player why is exactly the "safety/continuity fact the player
// has no other way to see" CONTEXT.md's Standing Alert rule exists for
// (the same reasoning VESSEL DESTROYED and DOCKED already get) — F2 must
// not be able to hide the only key that gets the player flying again.
func TestEmptySlateNoticeSurvivesDeclutter(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Crafts = nil
	w.ActiveCraftIdx = 0

	s := settings.Default()
	for _, chipID := range settings.AllChips {
		s.SetChip(chipID, false)
	}
	v.SetSettings(s)
	v.SetDeclutter(true)

	out := v.Render(w, 0, DesignWidth, DesignHeight)
	if !strings.Contains(out, "[n]") {
		t.Errorf("empty-slate notice hidden by declutter/settings, want it to survive both:\n%s", out)
	}
}
