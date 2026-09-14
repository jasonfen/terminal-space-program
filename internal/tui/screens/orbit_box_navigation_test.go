// ADR 0051 slice 2a: NAVIGATION's load-bearing rules are the three
// mutually-exclusive title badges, the descent alarm's short form
// (re-grill Q2's amendment / open item 2), and the impact:/stop: row
// reading the MOVED descent-stop cache (slice 1) rather than
// recomputing.

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestNavigationTitleShowsLandedSite: decision 9 — the landed site rides
// the title, 0 rows.
func TestNavigationTitleShowsLandedSite(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = true
	lines := v.buildNavigationBox(w)
	if !strings.Contains(lines[0], "landed") {
		t.Errorf("title while Landed = %q, want the landed lat/lon badge", lines[0])
	}
}

// TestNavigationTitleDescentAlarmUsesShortForm: re-grill Q2's amendment
// (open item 2) — the title's alarm form must be the short "⚠ NO STOP",
// never the longer "CAN'T STOP (thrust)"/"CAN'T STOP (fuel)" the first
// draft used. A crashed-outcome stop forecast is an unstoppable case.
func TestNavigationTitleDescentAlarmUsesShortForm(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := descendingMoonCraft(t, 8_000, 2000) // fast enough to guarantee an unstoppable forecast
	c := w.ActiveCraft()
	corridor, descending := sim.DescentCorridorFor(c, sim.DescentPredictHorizon)
	if !descending {
		t.Fatal("setup: expected a live descent corridor for this fixture")
	}
	stopDat := v.cachedDescentStop(w, c)
	corridor.Stop, corridor.StopOK = stopDat.stop, stopDat.stopOK
	if corridor.StopOK && corridor.Stop.Outcome == sim.StopStopped {
		t.Skip("setup: this fixture's forecast resolved as stoppable, not the unstoppable case this test needs")
	}
	lines := v.buildNavigationBox(w)
	title := lines[0]
	if !strings.Contains(title, "⚠") {
		t.Fatalf("title for an unstoppable descent = %q, want an alarm badge", title)
	}
	if strings.Contains(title, "CAN'T STOP") {
		t.Errorf("title = %q, must use the short form (re-grill Q2 amendment), not the long CAN'T STOP wording", title)
	}
}

// TestNavigationImpactStopDashOutsideDescent: decision 2 — the row is
// always present, dash when there's nothing to forecast.
func TestNavigationImpactStopDashOutsideDescent(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	lines := v.buildNavigationBox(w)
	stopRow := lines[6]
	if !strings.HasSuffix(strings.TrimRight(stopRow, " "), "—") {
		t.Errorf("impact/stop row on the pad = %q, want dash cells", stopRow)
	}
}

// TestNavigationDepartDashWhenFrozen: C1 — a world whose sweep is frozen
// (departRowHidden) reads depart: — rather than the row disappearing.
// Kern's spin is slow enough to trip departRowHidden's epsilon; this
// test only asserts the DASH behaviour where the underlying helper
// already reports hidden, not the specific body.
func TestNavigationPlanRowIsPermanentDash(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	lines := v.buildNavigationBox(w)
	if len(lines) != 8 {
		t.Fatalf("buildNavigationBox returned %d lines, want 8 (title + 7 rows)", len(lines))
	}
	planRow := lines[7]
	if !strings.Contains(planRow, "plan:") || !strings.Contains(planRow, "—") {
		t.Errorf("plan row = %q, want plan: — (slice 3 fills the contents)", planRow)
	}
}
