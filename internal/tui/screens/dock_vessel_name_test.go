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

// An empty handle must read "their stack"/"their Skylab", never "their's".
func TestDockGuestLabelWithoutHandleHasNoPossessiveS(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := dockGuestStackGhostWorld(t)
	w.DockGuest.OwnerHandle = ""
	if got := strings.Join(v.buildDockGuestChip(w), "\n"); !strings.Contains(got, "docked to their Skylab") || strings.Contains(got, "their's") {
		t.Errorf("named, no handle:\n%s", got)
	}
	w.Ghosts = nil
	if got := strings.Join(v.buildDockGuestChip(w), "\n"); !strings.Contains(got, "docked to their stack") || strings.Contains(got, "their's") {
		t.Errorf("unnamed, no handle:\n%s", got)
	}
}

// docked/undocked rows name the player's vessel from their ghost; events
// carry a handle only, so two ghosts under one handle is ambiguous and
// falls back to "<handle>'s stack" rather than guessing.
func TestDockedUndockedRowsNameTheVessel(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:gern", CraftID: 1, Handle: "gern", Name: "Skylab"}}
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventDocked, Handle: "gern", At: time.Now()},
		{Kind: sim.SessionEventUndocked, Handle: "gern", At: time.Now()},
	}
	got := strings.Join(v.buildSessionEventsChip(w), "\n")
	for _, want := range []string{"◇ docked to gern's Skylab", "◇ undocked from gern's Skylab"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}

	w.Ghosts = nil // no ghost known
	got = strings.Join(v.buildSessionEventsChip(w), "\n")
	for _, want := range []string{"◇ docked to gern's stack", "◇ undocked from gern's stack"} {
		if !strings.Contains(got, want) {
			t.Errorf("fallback missing %q:\n%s", want, got)
		}
	}

	w.Ghosts = []sim.Ghost{
		{Owner: "SHA256:gern", CraftID: 1, Handle: "gern", Name: "Skylab"},
		{Owner: "SHA256:gern", CraftID: 2, Handle: "gern", Name: "Probe"},
	}
	got = strings.Join(v.buildSessionEventsChip(w), "\n")
	if !strings.Contains(got, "◇ docked to gern's stack") || strings.Contains(got, "Skylab") || strings.Contains(got, "Probe") {
		t.Errorf("ambiguous handle guessed a vessel:\n%s", got)
	}
}
