package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// holdRow returns the ATTITUDE chip's "  hold:    ..." row, or "" if the
// chip has fewer than 3 rows (title, nav, hold).
func holdRow(t *testing.T, chip []string) string {
	t.Helper()
	if len(chip) < 3 {
		t.Fatalf("buildAttitudeChip returned %d rows, want at least 3 (title, nav, hold)", len(chip))
	}
	return chip[2]
}

// TestAttitudeHoldLabelOrbitModeUnaffected: the baseline — NavOrbit with a
// plain orbit-frame hold reads exactly as it always has. Not a #421
// scenario; pins that the fix doesn't touch the common case.
func TestAttitudeHoldLabelOrbitModeUnaffected(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 1_000})
	w.NavMode = sim.NavOrbit
	c := w.ActiveCraft()
	c.AttitudeMode = spacecraft.BurnPrograde

	v := newProximityTestView(t, 80, 24)
	row := holdRow(t, v.buildAttitudeChip(w))
	if !strings.Contains(row, "Prograde") || strings.Contains(row, "Target") {
		t.Errorf("hold row = %q, want plain %q under NavOrbit", row, "Prograde")
	}
}

// TestAttitudeHoldLabelNamesTargetFrame is the #421 acceptance test: the
// six-lane UX review's reproduction was `;;` (nav → TARGET) then `q`
// (an RCS pulse — internal/sim/rcs.go never touches AttitudeMode), which
// leaves AttitudeMode sitting at whatever the orbit-frame hold was before
// the switch while `nav:` already reads TARGET. The hold: row must name
// the SAME frame the nav: row does once NavMode is NavTarget and a
// relative target actually resolves, for every base axis that has a
// target-relative counterpart.
func TestAttitudeHoldLabelNamesTargetFrame(t *testing.T) {
	cases := []struct {
		name    string
		stale   spacecraft.BurnMode // what AttitudeMode is stuck at (RCS never updated it)
		want    string
		mustNot string
	}{
		{"prograde", spacecraft.BurnPrograde, "Target Prograde", ""},
		{"retrograde", spacecraft.BurnRetrograde, "Target Retrograde", ""},
		{"radial-out", spacecraft.BurnRadialOut, "Target", "Radial"},
		{"radial-in", spacecraft.BurnRadialIn, "Anti-Target", "Radial"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := proximityWorld(t, orbital.Vec3{X: 1_000})
			w.NavMode = sim.NavTarget
			c := w.ActiveCraft()
			c.AttitudeMode = tc.stale

			v := newProximityTestView(t, 80, 24)
			row := holdRow(t, v.buildAttitudeChip(w))
			if !strings.Contains(row, tc.want) {
				t.Errorf("hold row = %q, want it to contain %q under nav:TARGET", row, tc.want)
			}
			if tc.mustNot != "" && strings.Contains(row, tc.mustNot) {
				t.Errorf("hold row = %q, must not contain the stale orbital label %q", row, tc.mustNot)
			}
		})
	}
}

// TestAttitudeHoldLabelNormalHasNoTargetCounterpart: NormalPlus/Minus
// have no target-relative equivalent — ResolveAttitudeIntent itself
// leaves them orbit-frame under NavTarget too, so the display must match
// rather than inventing a label that no keypress could ever produce.
func TestAttitudeHoldLabelNormalHasNoTargetCounterpart(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 1_000})
	w.NavMode = sim.NavTarget
	c := w.ActiveCraft()
	c.AttitudeMode = spacecraft.BurnNormalPlus

	v := newProximityTestView(t, 80, 24)
	row := holdRow(t, v.buildAttitudeChip(w))
	if !strings.Contains(row, "Normal+") {
		t.Errorf("hold row = %q, want it to stay %q (no target-relative counterpart)", row, "Normal+")
	}
}

// TestAttitudeHoldLabelNoRelativeTargetStaysOrbitFrame: NavMode can only
// read NavTarget while HasRelativeTarget is true in ordinary play
// (CycleNavMode / reconcileNavMode both guard it), but the display must
// not misname the hold if that invariant is ever bypassed — falls back to
// the plain orbit-frame label exactly like ResolveAttitudeIntent's own
// NavTarget-without-a-target guard.
func TestAttitudeHoldLabelNoRelativeTargetStaysOrbitFrame(t *testing.T) {
	w := proximityWorld(t, orbital.Vec3{X: 1_000})
	w.NavMode = sim.NavTarget
	w.Target = sim.Target{} // no craft target bound
	c := w.ActiveCraft()
	c.AttitudeMode = spacecraft.BurnPrograde

	v := newProximityTestView(t, 80, 24)
	row := holdRow(t, v.buildAttitudeChip(w))
	if strings.Contains(row, "Target") {
		t.Errorf("hold row = %q, must not claim a target frame with no relative target bound", row)
	}
	if !strings.Contains(row, "Prograde") {
		t.Errorf("hold row = %q, want it to fall back to plain %q", row, "Prograde")
	}
}
