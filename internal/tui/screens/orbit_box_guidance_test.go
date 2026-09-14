// ADR 0051 slice 2a: GUIDANCE's L7 fix (review side lead) is the
// load-bearing behaviour here — fpa: and orbit fpa: must agree about
// whether Landed means "nothing to show", not silently disagree because
// they threshold two different velocities.

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestGuidanceBoxFPACellsAgreeWhileLanded is L7's regression: pre-fix,
// a landed craft's inertial co-rotation speed clears the fpa floor even
// though its surface-relative speed sits at zero, producing "fpa: —
// orbit fpa: 0°" side by side (P3-11). Both cells must dash together.
func TestGuidanceBoxFPACellsAgreeWhileLanded(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = true
	lines := v.buildGuidanceBox(w)
	fpaRow := lines[3]
	if !strings.Contains(fpaRow, "fpa:") {
		t.Fatalf("fpa row missing: %v", lines)
	}
	dashCount := strings.Count(fpaRow, "—")
	if dashCount != 2 {
		t.Errorf("fpa row while Landed = %q, want both fpa: and orbit fpa: to read a dash together (L7), got %d dash(es)", fpaRow, dashCount)
	}
}

// TestGuidanceBoxHoldAndNavAlwaysPresent: decision 2 — every row always
// present, even with no active craft.
func TestGuidanceBoxHoldAndNavAlwaysPresent(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	lines := v.buildGuidanceBox(w)
	if len(lines) != 4 {
		t.Fatalf("buildGuidanceBox returned %d lines, want 4 (title, hold/nav, heading/trim, fpa/orbit fpa)", len(lines))
	}
	if !strings.Contains(lines[1], "hold:") || !strings.Contains(lines[1], "nav:") {
		t.Errorf("hold/nav row = %q, want both cells present", lines[1])
	}
}
