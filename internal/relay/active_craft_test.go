package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// #288: the roster's LOCATION column told partners where a player's
// FIRST craft slot was, not where the player actually is. Live, a pilot
// flying 577 km from their rendezvous partner at Earth was listed at the
// Sun for the whole session, because slot 0 happened to hold a
// heliocentric craft. Single-craft players display correctly, which is
// what masked it.
//
// The report is the only thing a roster consumer has to read, so the
// active craft has to be marked on the wire.
func TestReportMarksTheActiveCraft(t *testing.T) {
	w := newWorld(t)
	moon := w.Systems[0].FindBody("Moon")
	if moon == nil {
		t.Skip("Moon not in catalog")
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("crafts = %d, want 2", len(w.Crafts))
	}
	w.Crafts[1].Primary = *moon

	store := NewStore()
	rep := NewReporter(store, "SHA256:alice")

	// Slot 0 (Earth) active.
	w.SetActiveCraftIdx(0)
	rep.Tick(w, time.Now())
	got := storedReport(t, store, "SHA256:alice")
	active, ok := got.ActiveCraft()
	if !ok {
		t.Fatal("report carries no active craft")
	}
	if active.ID != w.Crafts[0].ID {
		t.Errorf("active craft ID = %d, want slot 0's %d", active.ID, w.Crafts[0].ID)
	}

	// Switch to the Moon craft: the marker follows, and the location a
	// roster reads off it changes with it.
	earthPrimary := active.Primary
	w.SetActiveCraftIdx(1)
	rep.Tick(w, time.Now().Add(2*Heartbeat))
	got = storedReport(t, store, "SHA256:alice")
	active, ok = got.ActiveCraft()
	if !ok {
		t.Fatal("report carries no active craft after the switch")
	}
	if active.ID != w.Crafts[1].ID {
		t.Errorf("active craft ID = %d, want slot 1's %d", active.ID, w.Crafts[1].ID)
	}
	if active.Primary == earthPrimary {
		t.Errorf("location still reads %q after switching to the Moon craft", active.Primary)
	}
}

// A craft switch has to reach partners promptly — it changes nothing
// about any orbit, so the element-based change detector cannot see it,
// and without its own trigger the roster would lie until the next
// heartbeat.
func TestCraftSwitchForcesAReport(t *testing.T) {
	w := newWorld(t)
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	store := NewStore()
	rep := NewReporter(store, "SHA256:alice")
	w.SetActiveCraftIdx(0)
	at := time.Now()
	rep.Tick(w, at)

	// Well inside the heartbeat, nothing else changed: a switch alone must
	// still publish.
	w.SetActiveCraftIdx(1)
	rep.Tick(w, at.Add(100*time.Millisecond))
	active, ok := storedReport(t, store, "SHA256:alice").ActiveCraft()
	if !ok || active.ID != w.Crafts[1].ID {
		t.Errorf("active craft on the wire = %+v, want slot 1 (ID %d) before the heartbeat",
			active, w.Crafts[1].ID)
	}
}

// An older report with no marker (or a player with no craft at all) must
// degrade to the previous behaviour rather than losing the row.
func TestActiveCraftFallsBackToFirstSlot(t *testing.T) {
	r := CraftReport{Crafts: []CraftState{{ID: 7, Primary: "earth"}, {ID: 9, Primary: "moon"}}}
	got, ok := r.ActiveCraft()
	if !ok || got.ID != 7 {
		t.Errorf("unmarked report ActiveCraft = %+v/%v, want the first slot", got, ok)
	}
	if _, ok := (CraftReport{}).ActiveCraft(); ok {
		t.Error("an empty report claims an active craft")
	}
	// A marker naming a craft that is no longer in the set (staged away
	// between the mark and the send) also falls back rather than vanishing.
	r.ActiveCraftID = 999
	if got, ok := r.ActiveCraft(); !ok || got.ID != 7 {
		t.Errorf("stale marker ActiveCraft = %+v/%v, want the first slot", got, ok)
	}
}

// storedReport pulls one owner's latest report back out of a store —
// the wire's-eye view, as a partner's roster would read it.
func storedReport(t *testing.T, store *Store, owner string) CraftReport {
	t.Helper()
	for _, r := range store.Snapshot("SHA256:nobody") {
		if r.Owner == owner {
			return r
		}
	}
	t.Fatalf("no report for %s", owner)
	return CraftReport{}
}
