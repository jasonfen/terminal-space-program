package screens

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestBodyInfoFooterNamesLiveKeys (#423): the footer used to advertise
// [←/→] prev/next body and [q] quit. On this screen ←/→ pan the map behind
// the panel (Keymap.PanLeft/PanRight) and `q` is radial+; the keys that
// actually walk the body cursor are h/l (Keymap.PrevBody/NextBody).
func TestBodyInfoFooterNamesLiveKeys(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := NewBodyInfo(chipTestTheme()).Render(w, 0, 100, 40)
	// The legend rides the frame's bottom edge (B11); t/H/P live in the
	// ACTIONS box above it so a flash on that edge cannot hide them.
	footer := out[strings.LastIndex(out, "\n")+1:]
	t.Logf("body info footer: %q", footer)

	if !strings.Contains(footer, "[h/l]") {
		t.Errorf("footer does not name h/l, the keys that actually step the body cursor: %q", footer)
	}
	if strings.Contains(footer, "←/→") {
		t.Errorf("footer still advertises ←/→, which pan the map behind this screen: %q", footer)
	}
	if strings.Contains(footer, "[q]") {
		t.Errorf("footer still advertises [q] quit; q is radial+ here: %q", footer)
	}
	for _, k := range []string{"[t] target", "[H] transfer", "[P] porkchop"} {
		if !strings.Contains(out, k) {
			t.Errorf("screen does not advertise %q:\n%s", k, out)
		}
		if strings.Contains(footer, k) {
			t.Errorf("%q is on the bottom edge, where a flash would hide it: %q", k, footer)
		}
	}
	if !strings.Contains(footer, "[esc]") {
		t.Errorf("footer lost the [esc] back exit: %q", footer)
	}
}

// TestBodyInfoNumbersFollowReadoutContract (#598 numbers half): mass,
// radius, semimajor axis and age used to print in exponent notation
// (`5.972e+24 kg`). The rendered frame must carry none, for a planet and
// for the Sun (which has the STELLAR box and its age).
func TestBodyInfoNumbersFollowReadoutContract(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	exp := regexp.MustCompile(`\d[eE][+-]?\d`)
	// Instrument check: the pattern catches the notation we are banning.
	if !exp.MatchString("Mass: 5.972e+24 kg") || !exp.MatchString("4.60e+09 years") {
		t.Fatal("exponent regexp cannot match exponent notation")
	}
	for _, name := range []string{"Earth", "Sun"} {
		idx := -1
		for i, b := range w.System().Bodies {
			if b.EnglishName == name {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatalf("%s not in the default system", name)
		}
		out := NewBodyInfo(chipTestTheme()).Render(w, idx, 140, 40)
		if loc := exp.FindStringIndex(out); loc != nil {
			t.Errorf("%s body info prints exponent notation (%q):\n%s", name, out[loc[0]:loc[1]], out)
		}
	}
}
