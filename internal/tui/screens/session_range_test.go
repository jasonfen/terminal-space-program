package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0037 §5: nothing in the game named the lock-arming neighbourhood —
// the 35 km / 100 m/s gate lived in docs only, so a player closing on a
// partner had no way to know when their warps would couple, or how close
// they already were. The roster now carries the live range to each nearby
// player plus a one-line statement of the rule.
func TestSessionRosterShowsRangeAndTheLockRule(t *testing.T) {
	s := NewSessionScreen(Theme{})
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Session = &sim.SessionInfo{
		Self: "SHA256:me",
		Players: []sim.SessionPlayer{
			{Fingerprint: "SHA256:me", Handle: "me", HasReport: true},
			{Fingerprint: "SHA256:gern", Handle: "gern", HasReport: true,
				System: w.System().Name, HasRange: true, RangeM: 12_400},
			{Fingerprint: "SHA256:far", Handle: "far", HasReport: true},
		},
	}
	out := s.Render(w, 120)
	if !strings.Contains(out, "RANGE") {
		t.Errorf("roster has no RANGE column:\n%s", out)
	}
	if !strings.Contains(out, formatRangeM(12_400)) {
		t.Errorf("roster does not show the live range to gern:\n%s", out)
	}
	// A player with no measurable range reads blank, not "0 m" — the same
	// honesty rule the craft count and LOCATION columns already follow
	// (#297: a zero value read as a fact).
	if strings.Contains(out, "0 m ") {
		t.Errorf("a player with no range rendered as a zero distance:\n%s", out)
	}
	// The rule itself, in the numbers the sim actually gates on.
	if !strings.Contains(out, "35 km") || !strings.Contains(out, "100 m/s") {
		t.Errorf("roster never states the warp-lock rule:\n%s", out)
	}
}
