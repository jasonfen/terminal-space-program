package screens

import (
	"strings"
	"testing"

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

	out := c.Render(keylayout.QWERTY, 80)
	if !strings.Contains(out, "QWERTY") {
		t.Errorf("render missing active layout label:\n%s", out)
	}
	if !strings.Contains(out, "controls") || !strings.Contains(out, "[Back]") {
		t.Errorf("render missing title chrome:\n%s", out)
	}

	out = c.Render(keylayout.QWERTZ, 80)
	if !strings.Contains(out, "QWERTZ") {
		t.Errorf("render missing QWERTZ label:\n%s", out)
	}
}

// TestControlsClickCyclesRow — a click on the layout row cycles, a click on
// [Back] cancels, and the hit ranges are recomputed by Render.
func TestControlsClickCyclesRow(t *testing.T) {
	c := NewControlsScreen(chipTestTheme())
	c.Render(keylayout.QWERTY, 80) // populate click ranges

	if got := c.HandleClick(2, c.layoutBtn.row); got != ControlsActionCycleLayout {
		t.Errorf("click on layout row = %v, want ControlsActionCycleLayout", got)
	}
	if got := c.HandleClick(c.backBtn.colStart, 0); got != ControlsActionCancel {
		t.Errorf("click on [Back] = %v, want ControlsActionCancel", got)
	}
}
