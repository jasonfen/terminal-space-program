package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The Reprieve moments, addressed at the partner whose own flight now
// depends on an empty chair (ADR 0036 S5).
func TestSessionEventsChipReprieveKinds(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	now := time.Now()
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventWentQuiet, Handle: "vex", Detail: "rendezvous", At: now},
		{Kind: sim.SessionEventWentQuiet, Handle: "kes", Detail: "dock", At: now},
		{Kind: sim.SessionEventBack, Handle: "vex", At: now},
		{Kind: sim.SessionEventTimedOut, Handle: "kes", At: now},
	}
	joined := strings.Join(v.buildSessionEventsChip(w), "\n")
	for _, want := range []string{
		// Naming what is held is the point: "went quiet" alone tells the
		// partner nothing about whether their coast survives.
		"vex went quiet — rendezvous held",
		"kes went quiet — dock held",
		"vex is back",
		"kes's session timed out — they never came back",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("session chip missing %q:\n%s", want, joined)
		}
	}
	// A session that timed out did not leave; the wording must not be
	// interchangeable with the ordinary departure.
	if strings.Contains(joined, "kes left") {
		t.Errorf("a timed-out session rendered as an ordinary leave:\n%s", joined)
	}
}

// The roster is what made slice 5 necessary: a reprieved session stays
// Online for hours, so an away player must not render identically to one
// sitting at the keyboard.
func TestSessionScreenMarksAway(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Session.Players[1].Away = true // gern, still online

	out := s.Render(w, 120)

	if !strings.Contains(out, "away") {
		t.Errorf("an away player is indistinguishable from an attended one:\n%s", out)
	}
	if !strings.Contains(out, "◐") {
		t.Errorf("no away glyph on the roster:\n%s", out)
	}
	// Away is not offline: their session is still simulating, and the row
	// must not imply their craft have stopped.
	attended := s.Render(sessionWorld(t, true), 120)
	if strings.Contains(attended, "away") || strings.Contains(attended, "◐") {
		t.Errorf("away rendering leaked onto a roster with nobody away:\n%s", attended)
	}
}

// The returning player's account of the interval they missed (ADR 0036
// S6), opening the replay of the moments that fell while they were gone.
func TestSessionEventsChipResumed(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	now := time.Now()
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventResumed, Elapsed: 2*time.Hour + 19*time.Minute, At: now},
		{Kind: sim.SessionEventRendezvousArrived, Handle: "ansi", At: now},
	}
	joined := strings.Join(v.buildSessionEventsChip(w), "\n")
	if !strings.Contains(joined, "resumed — 2h19m ran while you were away") {
		t.Errorf("resume chip missing or misworded:\n%s", joined)
	}
	// The replayed moment rides right behind it, so the elapsed figure has
	// something to account for.
	if !strings.Contains(joined, "encounter reached") {
		t.Errorf("the replayed moment did not render alongside the resume:\n%s", joined)
	}
}

// Detail is display context, not a required field: a went-quiet chip that
// somehow arrives without it must still read as a sentence.
func TestWentQuietChipWithoutDetail(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventWentQuiet, Handle: "vex", At: time.Now()},
	}
	joined := strings.Join(v.buildSessionEventsChip(w), "\n")
	if !strings.Contains(joined, "vex went quiet") || strings.Contains(joined, "—  held") {
		t.Errorf("chip reads badly without Detail:\n%s", joined)
	}
}
