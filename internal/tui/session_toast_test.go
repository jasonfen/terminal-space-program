package tui

import (
	"strings"
	"testing"
	"time"
)

// The Session screen's refusal toasts (v0.30.1) are only a fix if they
// actually reach the frame. The status flash is overlaid onto the
// rendered base's bottom border, which is a canvas affordance the roster
// doesn't obviously have — so pin that a status message set while the
// Session screen is active is really visible. Without this, "fixing" a
// silent no-op by emitting a toast nobody can see would look green in
// the screen-level tests and still be broken in the player's hands.
func TestSessionScreenStatusFlashIsVisible(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.width, a.height = 140, 45
	a.active = screenSession
	a.statusMsg = "you can't promote yourself"
	a.statusExpires = time.Now().Add(time.Minute)

	if out := a.View(); !strings.Contains(out, "you can't promote yourself") {
		t.Errorf("session-screen status flash never rendered:\n%s", out)
	}
}
