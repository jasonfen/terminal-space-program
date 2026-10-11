package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
)

func TestControlsCycleAndCancel(t *testing.T) {
	c := NewControlsScreen(chipTestTheme())

	for _, k := range []string{" ", "enter", "left", "right"} {
		if got := c.HandleKey(k); got != ControlsActionCycleLayout {
			t.Errorf("HandleKey(%q) = %v, want ControlsActionCycleLayout", k, got)
		}
	}
	if got := c.HandleKey("esc"); got != ControlsActionCancel {
		t.Errorf("HandleKey(esc) = %v, want ControlsActionCancel", got)
	}
	if got := c.HandleKey("x"); got != ControlsActionNone {
		t.Errorf("HandleKey(x) = %v, want ControlsActionNone", got)
	}
}

func TestControlsRenderShowsLayout(t *testing.T) {
	c := NewControlsScreen(chipTestTheme())

	out := c.Render(keylayout.QWERTY, 80, 0)
	if !strings.Contains(out, "QWERTY") {
		t.Errorf("render missing active layout label:\n%s", out)
	}
	// The Title Row (name, screen, [Back]) is the App's now (B11).

	out = c.Render(keylayout.QWERTZ, 80, 0)
	if !strings.Contains(out, "QWERTZ") {
		t.Errorf("render missing QWERTZ label:\n%s", out)
	}
}

// TestControlsClickCyclesRow — a click on the layout row cycles, a click on
// [Back] cancels, and the hit ranges are recomputed by Render.
func TestControlsClickCyclesRow(t *testing.T) {
	c := NewControlsScreen(chipTestTheme())
	c.Render(keylayout.QWERTY, 80, 0) // populate click ranges

	if got := c.HandleClick(3, c.layoutBtn.row+frameInset); got != ControlsActionCycleLayout {
		t.Errorf("click on layout row = %v, want ControlsActionCycleLayout", got)
	}
}

// TestKeyboardLayoutScreenNamesWhatMoves (#590): the QWERTZ map swaps only
// Y and Z (internal/keylayout), so the screen must say letters move with the
// layout and name the punctuation keys that do not, not promise that every
// binding stays under the same finger.
func TestKeyboardLayoutScreenNamesWhatMoves(t *testing.T) {
	c := NewControlsScreen(chipTestTheme())
	out := ansi.Strip(c.Render(keylayout.QWERTZ, 140, 40))
	if strings.Contains(out, "same finger and relabels") {
		t.Errorf("screen still promises every binding moves:\n%s", out)
	}
	for _, want := range []string{"Letter bindings", "Punctuation does not move"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen omits %q:\n%s", want, out)
		}
	}
	// At least the zoom, pan and time punctuation keys are named.
	for _, k := range []string{"+", "-", "<", ">", "{", "}", ";", "|"} {
		if !strings.Contains(out, " "+k+" ") && !strings.Contains(out, " "+k+",") {
			t.Errorf("screen does not name the punctuation key %q:\n%s", k, out)
		}
	}
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "…") {
			t.Errorf("a row is truncated: %q", ln)
		}
	}
}
