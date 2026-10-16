package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0054 decision 5 + Jason's call 8: a dropped friend's departure says
// what happened and who did it. "kicked", never "removed".
func TestSessionEventsChipNamesWhoDroppedThem(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	now := time.Now()
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventDisconnectedBy, Handle: "pilot2", Detail: "jason", At: now},
		{Kind: sim.SessionEventKickedBy, Handle: "pilot3", Detail: "jason", At: now},
	}
	joined := strings.Join(v.buildSessionEventsChip(w), "\n")
	for _, want := range []string{
		"◇ pilot2 was disconnected by jason",
		"◇ pilot3 was kicked by jason",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("chip missing %q:\n%s", want, joined)
		}
	}
	for _, bad := range []string{"left", "removed"} {
		if strings.Contains(joined, bad) {
			t.Errorf("chip says %q:\n%s", bad, joined)
		}
	}
}
