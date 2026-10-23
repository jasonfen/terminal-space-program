package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// #723: dock text names the other player's vessel ("gern's Skylab"), and
// falls back to "<handle>'s stack" when no name is known.

func TestDockGuestChipFallsBackToStackWhenNameUnknown(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.DockGuest = &sim.DockGuestLink{OwnerFP: "SHA256:vex", OwnerHandle: "vex"} // no ghost report yet
	got := strings.Join(v.buildDockGuestChip(w), "\n")
	if !strings.Contains(got, "docked to vex's stack") {
		t.Errorf("unknown-name fallback missing:\n%s", got)
	}
}

func TestDockGuestChipAndBoxesNameTheOwnersVessel(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := dockGuestStackGhostWorld(t) // bob's ghost is named "Skylab"
	if got := strings.Join(v.buildDockGuestChip(w), "\n"); !strings.Contains(got, "docked to bob's Skylab") {
		t.Errorf("DOCKED chip does not name the vessel:\n%s", got)
	}
	if got := strings.Join(v.buildNavigationBox(w), "\n"); !strings.Contains(got, "bob's Skylab") {
		t.Errorf("NAVIGATION title does not name the vessel:\n%s", got)
	}
	if got := strings.Join(v.buildEmptySlateChip(w), "\n"); !strings.Contains(got, "docked to bob's Skylab") {
		t.Errorf("empty-slate chip does not name the vessel:\n%s", got)
	}
}

func TestSessionEventDockTextNamesTheVessel(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventControlReclaimed, Handle: "gern", At: time.Now()},
		{Kind: sim.SessionEventDockLost, Handle: "gern", At: time.Now()},
	}
	got := strings.Join(v.buildSessionEventsChip(w), "\n")
	for _, want := range []string{"they were docked to it and your seat was empty", "dock to gern's stack ended"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"riding", "dock with"} {
		if strings.Contains(got, bad) {
			t.Errorf("leftover %q:\n%s", bad, got)
		}
	}
}

// A long vessel name goes through the chip compositor at the 140x40
// design floor; the full name must survive on the DOCKED row.
func TestDockGuestChipLongNameAt140x40(t *testing.T) {
	v := NewOrbitView(riderViewTheme())
	v.Resize(140, 40)
	w := dockGuestStackGhostWorld(t)
	const long = "Skylab Deep Space Gateway Mk2"
	w.Ghosts[0].Name = long
	out := v.Render(w, 0, 140, 40)
	t.Logf("=== 140x40, name %q ===\n%s", long, out)
	if !strings.Contains(out, "docked to bob's "+long) {
		t.Errorf("long vessel name clipped on the DOCKED row")
	}
}
