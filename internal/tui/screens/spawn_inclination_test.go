package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #566: the spawn form's INCLINATION row (orbit mode only) is typed like
// ALTITUDE (ADR 0044): Enter opens a box, digits and one decimal point,
// Enter keeps, Esc reverts without closing the form, and only once back out
// does Enter launch. Default is the seed's inclination so a default partner
// shares the seed's plane.

func inclForm(t *testing.T) *SpawnCraft {
	t.Helper()
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "", "", nil)
	return s
}

func focusIncl(t *testing.T, s *SpawnCraft) {
	t.Helper()
	s.HandleKey("tab") // POSITION
	s.HandleKey("tab") // INCLINATION is the next stop in orbit mode
	if s.fieldIdx != inclFieldIdx {
		t.Fatalf("two tabs from VESSEL TYPE landed on field %d, want INCLINATION (%d)", s.fieldIdx, inclFieldIdx)
	}
}

func typeKeys(s *SpawnCraft, keys ...string) {
	for _, k := range keys {
		s.HandleKey(k)
	}
}

func TestInclinationDefaultsToSeedInclination(t *testing.T) {
	s := inclForm(t)
	if got := s.SelectedInclinationDeg(); got != spacecraft.SeedInclinationDeg {
		t.Errorf("default inclination %v, want %v", got, spacecraft.SeedInclinationDeg)
	}
	out := s.Render(140, 40)
	if !strings.Contains(out, "51.6°") {
		t.Errorf("form does not show 51.6°:\n%s", out)
	}
}

func TestInclinationTypedEntryKeepsAndEscReverts(t *testing.T) {
	s := inclForm(t)
	focusIncl(t, s)
	if got := s.HandleKey("enter"); got != SpawnActionNone || !s.CapturingText() {
		t.Fatalf("Enter on INCLINATION: action %v capturing %v, want the edit box open", got, s.CapturingText())
	}
	typeKeys(s, "2", "8", ".", "5")
	if got := s.HandleKey("enter"); got != SpawnActionNone || s.CapturingText() {
		t.Fatalf("committing returned %v capturing %v", got, s.CapturingText())
	}
	if got := s.SelectedInclinationDeg(); got != 28.5 {
		t.Errorf("typed 28.5, got %v", got)
	}
	// Esc discards and never cancels the form.
	s.HandleKey("tab")
	s.fieldIdx = inclFieldIdx
	s.HandleKey("enter")
	typeKeys(s, "9", "0")
	if got := s.HandleKey("esc"); got != SpawnActionNone {
		t.Fatalf("Esc in the box returned %v, want SpawnActionNone", got)
	}
	if got := s.SelectedInclinationDeg(); got != 28.5 {
		t.Errorf("Esc changed the value to %v", got)
	}
	// Back out: the next Enter launches.
	if got := s.HandleKey("enter"); got != SpawnActionConfirm {
		t.Errorf("Enter after leaving the box returned %v, want SpawnActionConfirm", got)
	}
}

func TestInclinationNeverLaunchesHalfTyped(t *testing.T) {
	s := inclForm(t)
	focusIncl(t, s)
	s.HandleKey("enter")
	for _, k := range []string{"5", "1", ".", "enter"} {
		if got := s.HandleKey(k); got != SpawnActionNone {
			t.Fatalf("key %q returned %v while typing", k, got)
		}
	}
}

func TestInclinationClampsToZeroAndOneEighty(t *testing.T) {
	s := inclForm(t)
	focusIncl(t, s)
	s.HandleKey("enter")
	typeKeys(s, "2", "5", "0", "enter")
	if got := s.SelectedInclinationDeg(); got != 180 {
		t.Errorf("typed 250, got %v want 180", got)
	}
	if !strings.Contains(s.Render(140, 40), "180") {
		t.Error("no clamp note after typing past 180")
	}
	s.HandleKey("tab")
	s.fieldIdx = inclFieldIdx
	s.HandleKey("enter")
	typeKeys(s, "0", "enter")
	if got := s.SelectedInclinationDeg(); got != 0 {
		t.Errorf("typed 0, got %v", got)
	}
}

func TestInclinationBufferOnlyDigitsAndOnePoint(t *testing.T) {
	s := inclForm(t)
	focusIncl(t, s)
	s.HandleKey("enter")
	typeKeys(s, "a", "-", "1", ".", ".", "2", "x", "3", "4", "5", "6", "7", "8")
	if s.inclInput != "1.234" && len(s.inclInput) > maxInclInputChars {
		t.Errorf("buffer %q breaks the rules", s.inclInput)
	}
	if strings.Count(s.inclInput, ".") > 1 || len(s.inclInput) > maxInclInputChars {
		t.Errorf("buffer %q: more than one point or over %d chars", s.inclInput, maxInclInputChars)
	}
}

func TestInclinationArrowsStepOneDegreeAndHold(t *testing.T) {
	s := inclForm(t)
	focusIncl(t, s)
	s.HandleKey("right")
	if got := s.SelectedInclinationDeg(); got != 52.6 {
		t.Errorf("right from 51.6 gave %v, want 52.6", got)
	}
	s.inclDeg = 180
	s.HandleKey("right")
	if s.SelectedInclinationDeg() != 180 {
		t.Errorf("right at 180 moved to %v", s.SelectedInclinationDeg())
	}
	s.inclDeg = 0
	s.HandleKey("left")
	if s.SelectedInclinationDeg() != 0 {
		t.Errorf("left at 0 moved to %v", s.SelectedInclinationDeg())
	}
}

func TestInclinationRowOnlyInOrbitMode(t *testing.T) {
	s := inclForm(t)
	if !strings.Contains(s.Render(140, 40), "incl") {
		t.Error("orbit mode shows no inclination row")
	}
	for _, mode := range []spawnPosMode{posAlongside, posLaunchpad} {
		s.posMode = mode
		if mode == posLaunchpad && !s.launchpadAllowed() {
			continue
		}
		if strings.Contains(s.Render(140, 40), "incl") {
			t.Errorf("mode %v still shows an inclination row", mode)
		}
		for _, f := range s.fieldOrder() {
			if f == inclFieldIdx {
				t.Errorf("mode %v: Tab order still reaches INCLINATION", mode)
			}
		}
	}
}

func TestInclinationRowSaysAboveNinetyIsRetrograde(t *testing.T) {
	s := inclForm(t)
	if out := s.Render(140, 40); !strings.Contains(out, "retrograde") || !strings.Contains(out, "90") {
		t.Errorf("row hint does not say above 90° is retrograde:\n%s", out)
	}
}

func TestInclinationFormFitsAtDesignSizes(t *testing.T) {
	for _, sz := range [][2]int{{104, 24}, {140, 40}, {181, 49}} {
		s := inclForm(t)
		out := s.Render(sz[0], sz[1])
		if n := len(strings.Split(out, "\n")); n > sz[1] {
			t.Errorf("%dx%d: %d lines", sz[0], sz[1], n)
		}
		for _, ln := range strings.Split(out, "\n") {
			if w := len([]rune(stripANSI(ln))); w > sz[0] {
				t.Errorf("%dx%d: a line is %d cells wide", sz[0], sz[1], w)
				break
			}
		}
		if !strings.Contains(out, "incl") {
			t.Errorf("%dx%d: inclination row missing", sz[0], sz[1])
		}
	}
}
