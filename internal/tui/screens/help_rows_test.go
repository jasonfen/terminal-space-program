package screens

import (
	"strings"
	"testing"
)

// helpRow returns the description the overlay prints for a key token,
// searching the authored sections rather than the rendered frame so the
// assertion doesn't depend on scroll position.
func helpRow(t *testing.T, token string) (section, desc string) {
	t.Helper()
	for _, s := range helpSections {
		for _, r := range s.rows {
			if r[0] == token {
				return s.header, r[1]
			}
		}
	}
	t.Fatalf("no help row for token %q", token)
	return "", ""
}

// TestHelpQuitRowIsMenuScoped (#423): `q` is AttitudeRadialOut on the
// flight screen — it only quits inside the pause menu (menu.go). The
// overlay used to open with "q — quit (confirm + autosave)" in GENERAL,
// which is the first line a player reads and the one that misleads worst.
func TestHelpQuitRowIsMenuScoped(t *testing.T) {
	for _, s := range helpSections {
		if s.header != "GENERAL" {
			continue
		}
		for _, r := range s.rows {
			if r[0] == "q" {
				t.Errorf("GENERAL still lists a bare `q` row (%q) — on the flight screen q is radial+, not quit", r[1])
			}
		}
	}
	section, desc := helpRow(t, "q")
	if !strings.Contains(strings.ToLower(section), "menu") {
		t.Errorf("the quit `q` row sits under %q; it must be in a clearly menu-scoped section", section)
	}
	if !strings.Contains(desc, "quit") {
		t.Errorf("menu-scoped `q` row no longer describes quit: %q", desc)
	}
}

// TestHelpPitchTrimNamesBothDirections (#423): `<` is west, `>` is east.
// The row named only "east" while showing the keys in west-then-east order.
func TestHelpPitchTrimNamesBothDirections(t *testing.T) {
	_, desc := helpRow(t, "< / >")
	west := strings.Index(desc, "west")
	east := strings.Index(desc, "east")
	if west < 0 || east < 0 {
		t.Fatalf("pitch trim row names only one direction: %q", desc)
	}
	if west > east {
		t.Errorf("pitch trim row reads east-before-west (%q) but the keys are `<` west then `>` east", desc)
	}
}

// TestHelpNavModeStatesSkipRule (#423): the `;` cycle skips Target when no
// vessel target is bound, so a player watching it toggle Orbit⇄Surface with
// a body targeted otherwise concludes the key is broken. docs/controls.md
// already says this.
func TestHelpNavModeStatesSkipRule(t *testing.T) {
	_, desc := helpRow(t, ";")
	if !strings.Contains(strings.ToLower(desc), "skip") {
		t.Errorf("NavMode row omits the skip-Target rule: %q", desc)
	}
}

// TestHelpDocumentsEndFlight (#423): `E` is the only way to clear a crashed
// vessel and was absent from the overlay entirely.
func TestHelpDocumentsEndFlight(t *testing.T) {
	_, desc := helpRow(t, "E")
	if !strings.Contains(strings.ToLower(desc), "end flight") && !strings.Contains(strings.ToLower(desc), "crashed") {
		t.Errorf("`E` row does not describe ending the flight of a crashed vessel: %q", desc)
	}
}
