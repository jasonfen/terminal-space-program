package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Join/leave/sync moments render as a transient SESSION chip; aged
// events drop out.
func TestSessionEventsChip(t *testing.T) {
	v := NewOrbitView(sessionTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	if lines := v.buildSessionEventsChip(w); lines != nil {
		t.Fatalf("chip rendered with no events: %v", lines)
	}

	now := time.Now()
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventJoin, Handle: "gern", At: now},
		{Kind: sim.SessionEventLeave, Handle: "dave", At: now},
		{Kind: sim.SessionEventSync, Handle: "pat", At: now},
		{Kind: sim.SessionEventJoin, Handle: "ancient", At: now.Add(-time.Minute)},
	}
	lines := v.buildSessionEventsChip(w)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"SESSION", "gern joined", "dave left", "pat synced to you"} {
		if !strings.Contains(joined, want) {
			t.Errorf("chip missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "ancient") {
		t.Error("aged-out event still on the chip")
	}

	// All aged → chip disappears entirely.
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventJoin, Handle: "old", At: now.Add(-time.Hour)},
	}
	if lines := v.buildSessionEventsChip(w); lines != nil {
		t.Errorf("chip rendered from stale events only: %v", lines)
	}
}
