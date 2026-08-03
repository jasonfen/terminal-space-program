package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0035 S4: the chat chip. Its own builder and corner (session
// moments own top-left), ~30 s TTL — a coordination line must survive a
// glance at the navball — showing the last few lines only.

func chatChipWorld(t *testing.T) *sim.World {
	t.Helper()
	w := rendezvousChipWorld(t)
	w.Session = &sim.SessionInfo{Self: "SHA256:me"}
	return w
}

func TestChatChipRendersBroadcastAndDMs(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := chatChipWorld(t)
	now := time.Now()
	w.ChatLines = []sim.ChatLine{
		{Owner: "SHA256:gern", Handle: "gern", Text: "node is 200m off", At: now},
		{Owner: "SHA256:me", Handle: "me", Text: "copy", At: now},
		{Owner: "SHA256:gern", Handle: "gern", To: "SHA256:me", ToHandle: "me", Text: "hold your burn", At: now},
		{Owner: "SHA256:me", Handle: "me", To: "SHA256:gern", ToHandle: "gern", Text: "on my way", At: now},
	}
	joined := strings.Join(v.buildChatChip(w), "\n")
	for _, want := range []string{
		"gern: node is 200m off",   // broadcast, sender named
		"me: copy",                 // own broadcast — you see what you said
		"gern>you: hold your burn", // DM received — visibly distinct
		">gern: on my way",         // DM sent — the own echo names the target
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("chat chip missing %q:\n%s", want, joined)
		}
	}
}

func TestChatChipTTLAndDepth(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := chatChipWorld(t)
	now := time.Now()
	w.ChatLines = []sim.ChatLine{
		{Owner: "SHA256:gern", Handle: "gern", Text: "ancient", At: now.Add(-chatChipTTL - time.Second)},
		{Owner: "SHA256:gern", Handle: "gern", Text: "one", At: now},
		{Owner: "SHA256:gern", Handle: "gern", Text: "two", At: now},
		{Owner: "SHA256:gern", Handle: "gern", Text: "three", At: now},
		{Owner: "SHA256:gern", Handle: "gern", Text: "four", At: now},
		{Owner: "SHA256:gern", Handle: "gern", Text: "five", At: now},
	}
	joined := strings.Join(v.buildChatChip(w), "\n")
	if strings.Contains(joined, "ancient") {
		t.Errorf("a line older than the chat TTL must age out:\n%s", joined)
	}
	if strings.Contains(joined, "gern: one") {
		t.Errorf("depth cap: only the last %d fresh lines show:\n%s", chatChipDepth, joined)
	}
	for _, want := range []string{"two", "three", "four", "five"} {
		if !strings.Contains(joined, want) {
			t.Errorf("fresh line %q missing:\n%s", want, joined)
		}
	}
}

func TestChatChipNilWhenQuiet(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := chatChipWorld(t)
	if chip := v.buildChatChip(w); chip != nil {
		t.Errorf("no chat lines → no chip; got %v", chip)
	}
	w.ChatLines = []sim.ChatLine{
		{Owner: "SHA256:gern", Handle: "gern", Text: "old", At: time.Now().Add(-chatChipTTL - time.Minute)},
	}
	if chip := v.buildChatChip(w); chip != nil {
		t.Errorf("every line aged out → no chip; got %v", chip)
	}
}

// The chat chip feeds the width-guard discipline like every
// glyph-bearing chip state (#253 review).
func TestChatChipCellWidthConsistent(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := chatChipWorld(t)
	now := time.Now()
	w.ChatLines = []sim.ChatLine{
		{Owner: "SHA256:gern", Handle: "gern", Text: "node is 200m off", At: now},
		{Owner: "SHA256:me", Handle: "me", To: "SHA256:gern", ToHandle: "gern", Text: "on my way", At: now},
		{Owner: "SHA256:gern", Handle: "gern", To: "SHA256:me", ToHandle: "me", Text: "hold", At: now},
	}
	assertChipCellWidthConsistent(t, "chat chip", v.buildChatChip(w))
}

// The pre-existing gap ADR 0035 flags as adjacent: kinds 6/7/8 flow
// through serve/dock.go but had no case in the session chip switch and
// rendered nothing.
func TestSessionEventsChipDockKinds(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	now := time.Now()
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventDocked, Handle: "gern", At: now},
		{Kind: sim.SessionEventUndocked, Handle: "gern", At: now},
		{Kind: sim.SessionEventTransfer, Handle: "gern", At: now},
	}
	chip := v.buildSessionEventsChip(w)
	joined := strings.Join(chip, "\n")
	for _, want := range []string{
		"docked with gern",
		"undocked from gern",
		"control handed to gern",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("session chip missing %q:\n%s", want, joined)
		}
	}
	assertChipCellWidthConsistent(t, "session dock kinds", chip)
}
