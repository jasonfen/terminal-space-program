package screens

import (
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
