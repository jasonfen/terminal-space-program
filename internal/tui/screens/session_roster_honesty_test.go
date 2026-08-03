package screens

import (
	"regexp"
	"strings"
	"testing"
)

// #297: a player the server has no live report for must not be described
// as having an empty fleet. Right after a server restart every enrolled
// player is in that state, and "0 vessels" read as "the fleet reset wiped
// them" — minutes after a --reset-fleet deploy, with every save intact on
// disk holding exactly one vessel.
//
// LOCATION already falls back to "—" for the same row; the vessel column
// has to mirror that honesty, reserving "0 vessels" for a player who
// actually reported an empty slate.
func TestRosterCraftColumnBlankWithoutReport(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	out := s.Render(w, 120)

	// "pat" is the enrolled-but-never-reported row in the fixture.
	var patRow string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "pat") {
			patRow = line
		}
	}
	if patRow == "" {
		t.Fatalf("no pat row in the roster:\n%s", out)
	}
	if strings.Contains(patRow, "vessel") {
		t.Errorf("report-less row claims a vessel count: %q", patRow)
	}
	// Two "—" cells: location and vessel, both honest about missing data.
	if n := strings.Count(patRow, "—"); n < 2 {
		t.Errorf("report-less row has %d \"—\" cells, want LOCATION and VESSEL both blank: %q", n, patRow)
	}

	// A player who DID report keeps a real count — including a genuine zero.
	w.Session.Players[1].CraftCount = 0
	out = s.Render(w, 120)
	if !regexp.MustCompile(`gern.*0 vessels`).MatchString(out) {
		t.Errorf("a reported empty slate must still read \"0 vessels\":\n%s", out)
	}
}
