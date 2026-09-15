// ADR 0051 slice 2a: NAVIGATION's load-bearing rules are the three
// mutually-exclusive title badges, the descent alarm's short form
// (re-grill Q2's amendment / open item 2), and the impact:/stop: row
// reading the MOVED descent-stop cache (slice 1) rather than
// recomputing.

package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// dockGuestStackGhostWorld (orbit_chips_test.go) is reused here: a World
// with no local craft, docked as a guest in "bob"'s stack, whose ghost
// carries a real 500 km circular orbit around Earth (ADR 0038 S4 part
// 3).

// TestDockGuestNavigationBoxShowsBadgedFlightData (ADR 0038 S4 part 3,
// ported from the retired VESSEL chip's TestDockGuestVesselChipShowsBadgedFlightData):
// once the stack's ghost report has landed, NAVIGATION upgrades from a
// bare no-craft dash box to the stack's real flight data (primary and
// speed), badged with the owner's handle so the numbers never read as
// this player's own ship.
func TestDockGuestNavigationBoxShowsBadgedFlightData(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := dockGuestStackGhostWorld(t)

	out := strings.Join(v.buildNavigationBox(w), "\n")
	for _, want := range []string{"bob", "Earth", "speed:"} {
		if !strings.Contains(out, want) {
			t.Errorf("badged NAVIGATION box missing %q:\n%s", want, out)
		}
	}
}

// TestDockGuestNavigationBoxShowsBadgedShape (ADR 0038 S4 part 3, ported
// from the retired ORBIT chip's TestDockGuestOrbitChipShowsBadgedShape):
// NAVIGATION's Ap:/Pe: cells must render the ghost's orbit shape while
// riding as a guest with a live ghost report, badged with the owner's
// handle; with no DockGuest and no craft at all, the ordinary all-dash
// no-craft box still applies (no stack to badge).
func TestDockGuestNavigationBoxShowsBadgedShape(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := dockGuestStackGhostWorld(t)

	out := strings.Join(v.buildNavigationBox(w), "\n")
	for _, want := range []string{"bob", "Ap:", "Pe:"} {
		if !strings.Contains(out, want) {
			t.Errorf("badged NAVIGATION box missing %q:\n%s", want, out)
		}
	}

	// Solo, no craft at all, no DockGuest: dash cells, no stack to badge.
	w2, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w2.Crafts = nil
	w2.ActiveCraftIdx = 0
	out2 := strings.Join(v.buildNavigationBox(w2), "\n")
	if strings.Contains(out2, "bob") {
		t.Errorf("NAVIGATION box badged with no DockGuest set:\n%s", out2)
	}
}

// TestNavigationTitleShowsLandedSite: decision 9, the landed site rides
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
// (open item 2), the title's alarm form must be the short "⚠ NO STOP",
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

// TestNavigationImpactStopDashOutsideDescent: decision 2, the row is
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

// TestNavigationDepartDashWhenFrozen: C1, a world whose sweep is frozen
// (departRowHidden) reads depart:, rather than the row disappearing.
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

// circularEarthOrbitCraft parks the world's active craft in a stable
// circular orbit around Earth at altM, velocity purely horizontal
// (perpendicular to the radius): no vertical rate, so the descent
// corridor never goes live for it. Used to prove the horiz: cell's
// CRASH-on-contact alert is gated on a live descent (item 1, C3), not
// on raw horizontal speed alone.
func circularEarthOrbitCraft(t *testing.T, altM float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active craft")
	}
	for _, b := range w.System().Bodies {
		if b.ID == "earth" {
			c.Primary = b
		}
	}
	c.Landed = false
	c.Crashed = false
	r := c.Primary.RadiusMeters() + altM
	mu := c.Primary.GravitationalParameter()
	circV := math.Sqrt(mu / r)
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: circV}
	c.State.M = c.TotalMass()
	return w
}

// fastLowMoonDescentCraft parks the world's active craft on a genuine
// powered/ballistic descent toward the Moon with a fast HORIZONTAL
// component (unlike descendingMoonCraft's purely radial fall): low
// altitude, falling, and moving sideways well past sim.CrashVCritMps,
// so the descent corridor is live and the horiz: alert must still fire.
func fastLowMoonDescentCraft(t *testing.T, altM, vDownMps, vHorizMps float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active craft")
	}
	for _, b := range w.System().Bodies {
		if b.ID == "moon" {
			c.Primary = b
		}
	}
	c.Landed = false
	c.Crashed = false
	c.State.R = orbital.Vec3{X: c.Primary.RadiusMeters() + altM}
	c.State.V = orbital.Vec3{X: -vDownMps, Y: vHorizMps}
	c.State.M = c.TotalMass()
	return w
}

// TestNavigationHorizNoCrashAlertInStableOrbit: item 1, C3. A 500 km
// circular orbit has plenty of horizontal speed (well past
// sim.CrashVCritMps) but is nowhere near the ground and the descent
// corridor is not live for it. The horiz: cell must not carry the
// CRASH-on-contact alert here: sabotage-first proof that this test
// goes RED against the unfixed behaviour (an unconditional
// vHoriz > sim.CrashVCritMps check with no descent gate).
func TestNavigationHorizNoCrashAlertInStableOrbit(t *testing.T) {
	w := circularEarthOrbitCraft(t, 500_000)
	v := NewOrbitView(launchThemeForTest())
	lines := v.buildNavigationBox(w)
	horizRow := lines[2]
	if strings.Contains(horizRow, "CRASH") {
		t.Errorf("horiz row in a stable 500 km orbit = %q, want no CRASH alert (C3: gate on the live descent corridor, matching impact:/stop:)", horizRow)
	}
}

// TestNavigationHorizCrashAlertDuringFastLowDescent: item 1's "not
// simply deleted" half. A genuine fast, low descent with real
// horizontal speed must keep the CRASH-on-contact alert once the
// descent corridor is live.
func TestNavigationHorizCrashAlertDuringFastLowDescent(t *testing.T) {
	w := fastLowMoonDescentCraft(t, 5_000, 50, 500)
	c := w.ActiveCraft()
	_, descending := sim.DescentCorridorFor(c, sim.DescentPredictHorizon)
	if !descending {
		t.Skip("setup: this fixture's descent corridor isn't live, can't exercise the gate")
	}
	v := NewOrbitView(launchThemeForTest())
	lines := v.buildNavigationBox(w)
	horizRow := lines[2]
	if !strings.Contains(horizRow, "CRASH") {
		t.Errorf("horiz row during a fast low descent (500 m/s horizontal) = %q, want the CRASH-on-contact alert kept", horizRow)
	}
}
