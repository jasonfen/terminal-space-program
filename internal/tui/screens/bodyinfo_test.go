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
	// The last row is a blank the App overlays flashes on (#495); the footer
	// is the last non-empty row.
	footer := out[strings.LastIndex(strings.TrimRight(out, "\n"), "\n")+1:]
	footer = strings.TrimRight(footer, "\n")
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
		if !strings.Contains(footer, k) {
			t.Errorf("footer does not advertise %q: %q", k, footer)
		}
	}
	if !strings.Contains(footer, "[esc]") {
		t.Errorf("footer lost the [esc] back exit: %q", footer)
	}
}
