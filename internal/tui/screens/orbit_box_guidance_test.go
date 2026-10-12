// ADR 0051 slice 2a: GUIDANCE's L7 fix (review side lead) is the
// load-bearing behaviour here, fpa: and orbit fpa: must agree about
// whether Landed means "nothing to show", not silently disagree because
// they threshold two different velocities.

package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestGuidanceBoxFPACellsAgreeWhileLanded is L7's regression: pre-fix,
// a landed craft's inertial co-rotation speed clears the fpa floor even
// though its surface-relative speed sits at zero, producing a dashed
// fpa: cell beside a populated "orbit fpa: 0°" (P3-11). Both cells must
// dash together.
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

// TestGuidanceBoxHoldAndNavAlwaysPresent: decision 2, every row always
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

// Review #629 M1 (HUD half, Jason 2026-10-12: "Hold should stay put but say
// so"). A hold keeps the frame it was pressed in (ADR 0050), so after the
// automatic SURF to ORBIT switch on the way up a surface hold sits beside
// nav: ORBIT. GUIDANCE says so: the hold's frame word reads in the Warning
// colour whenever it differs from the navball's, and in the plain colour when
// the two agree.
func TestGuidanceHoldFrameWordIsMarkedWhenItDiffersFromNav(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	th := plainThemeColored()
	marked := th.Warning.Render("SURF")
	if marked == "SURF" {
		t.Fatal("instrument is blind: the Warning style adds no escape under this colour profile")
	}

	w, c := spawnSaturnVOnPad(t)
	c.AttitudeMode = spacecraft.BurnSurfacePrograde
	v := NewOrbitView(th)
	holdRow := func() string { return v.buildGuidanceBox(w)[1] }

	// On the pad: nav SURF, hold SURF. Agreement, no mark.
	if w.NavMode != sim.NavSurface {
		t.Fatalf("setup: pad NavMode = %v, want SURF", w.NavMode)
	}
	if row := holdRow(); !strings.Contains(row, "(SURF)") || strings.Contains(row, marked) {
		t.Errorf("hold and nav agree on SURF but the row marks the frame: %q", row)
	}

	// Climb through the ascent line: the REAL automatic switch, then the row.
	w.Tick()
	up := w.NavAutoAscentAltitudeFor(c.Primary)
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + up*1.1)
	c.Landed, c.OnPad = false, false
	w.Tick()
	if w.NavMode != sim.NavOrbit {
		t.Fatalf("setup: NavMode = %v after the climb, want the automatic switch to ORBIT", w.NavMode)
	}
	if c.AttitudeMode != spacecraft.BurnSurfacePrograde {
		t.Fatalf("setup: the hold moved to %v; ADR 0050 keeps it", c.AttitudeMode)
	}
	row := holdRow()
	if !strings.Contains(row, "Prograde (") || !strings.Contains(row, marked) {
		t.Errorf("surface hold under nav: ORBIT does not mark its frame word: %q", row)
	}
	if !strings.Contains(row, "ORBIT") {
		t.Errorf("nav: cell lost its frame: %q", row)
	}

	// An orbit-frame hold beside nav: ORBIT is plain again.
	c.AttitudeMode = spacecraft.BurnPrograde
	if row := holdRow(); strings.Contains(row, th.Warning.Render("ORBIT")) {
		t.Errorf("an orbit hold under nav: ORBIT is marked: %q", row)
	}
}
