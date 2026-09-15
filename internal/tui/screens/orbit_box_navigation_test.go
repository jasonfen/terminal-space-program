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
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// inclinedCircularEarthOrbitCraft parks the active craft in a circular
// Earth orbit of the given inclination, positioned exactly at its own
// ascending node. R/V are composed directly in Earth's OWN equatorial
// frame (orbital.ReferenceFrameForPrimary), then rotated to world
// coordinates via BodyFrame.ToWorld, not laid out along the raw
// world X/Y/Z axes, which sit at Earth's axial tilt relative to its
// equatorial frame and would give neither the intended inclination nor
// Ω = 0 once craftLiveElements/navigationPlanRow read them back in
// that same body frame. Built this way, the resulting orbit's own
// longitude of ascending node is exactly 0° by construction, so the
// plan: row tests below can assert AN/DN exactly, not just "some
// angle".
func inclinedCircularEarthOrbitCraft(t *testing.T, incDeg, altM float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	for _, b := range w.System().Bodies {
		if b.ID == "earth" {
			c.Primary = b
		}
	}
	c.Landed = false
	c.Crashed = false
	r := c.Primary.RadiusMeters() + altM
	mu := c.Primary.GravitationalParameter()
	speed := math.Sqrt(mu / r)
	inc := incDeg * math.Pi / 180
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	rFrame := orbital.Vec3{X: r}
	vFrame := orbital.Vec3{Y: speed * math.Cos(inc), Z: speed * math.Sin(inc)}
	c.State.R = frame.ToWorld(rFrame)
	c.State.V = frame.ToWorld(vFrame)
	c.State.M = c.TotalMass()
	return w
}

// plantProgradeNode gives c a single resolved prograde node, dv m/s,
// firing an hour from now in world w's clock. primaryID mirrors
// ManeuverNode.PrimaryID (empty = the craft's current primary at plant
// time, matching an ordinary same-primary burn; a real body id like
// "moon" mirrors what an arrival-burn node from a transfer plant
// carries, forcing PredictedFinalOrbit's own frame-rebase path so the
// plan: row's encounter branch can be tested without a full Lambert
// solve).
func plantProgradeNode(w *sim.World, c *spacecraft.Spacecraft, dv float64, primaryID string) {
	c.Nodes = []spacecraft.ManeuverNode{{
		TriggerTime: w.Clock.SimTime.Add(time.Hour),
		Mode:        spacecraft.BurnPrograde,
		DV:          dv,
		PrimaryID:   primaryID,
	}}
}

// TestNavigationPlanRowDashWithNoPlan is the tracer bullet: no nodes at
// all, plan: reads a bare dash (unchanged from
// TestNavigationPlanRowIsPermanentDash: kept as its own vertical-slice
// proof that the new plan-row machinery doesn't fire without a plan).
func TestNavigationPlanRowDashWithNoPlan(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := inclinedCircularEarthOrbitCraft(t, 45, 500e3)
	lines := v.buildNavigationBox(w)
	planRow := lines[7]
	if !strings.Contains(planRow, "plan:") || !strings.Contains(planRow, "—") {
		t.Errorf("plan row with no nodes = %q, want plan: —", planRow)
	}
}

// TestNavigationPlanRowShowsOrbitWithNodeAngles (re-grill Q1): a plan
// that stays around the current world reads "plan: Earth orbit  AN
// ...  DN ...". The fixture starts exactly at its own ascending node
// (inclinedCircularEarthOrbitCraft), so Ω = 0° and AN/DN must read
// exactly "0.0°"/"180.0°".
func TestNavigationPlanRowShowsOrbitWithNodeAngles(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := inclinedCircularEarthOrbitCraft(t, 45, 500e3)
	c := w.ActiveCraft()
	plantProgradeNode(w, c, 50, "") // small burn: stays bound around Earth

	planRow := v.buildNavigationBox(w)[7]
	for _, want := range []string{"plan:", "Earth orbit", "AN 0.00°", "DN 180.0°"} {
		if !strings.Contains(planRow, want) {
			t.Errorf("plan row = %q, missing %q", planRow, want)
		}
	}
}

// TestNavigationPlanRowShowsEquatorial (re-grill Q1): an equatorial plan
// with no nodes carries the word "equatorial" in place of the angles.
func TestNavigationPlanRowShowsEquatorial(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := inclinedCircularEarthOrbitCraft(t, 0, 500e3)
	c := w.ActiveCraft()
	plantProgradeNode(w, c, 50, "")

	planRow := v.buildNavigationBox(w)[7]
	if !strings.Contains(planRow, "Earth orbit") || !strings.Contains(planRow, "equatorial") {
		t.Errorf("equatorial plan row = %q, want \"Earth orbit\" and \"equatorial\"", planRow)
	}
	if strings.Contains(planRow, "AN ") || strings.Contains(planRow, "DN ") {
		t.Errorf("equatorial plan row = %q, must not also print AN/DN angles", planRow)
	}
}

// TestNavigationPlanRowShowsEncounter (re-grill Q1): a transfer that
// ends at another world reads "plan: Moon encounter  AN ...  DN ...",
// regardless of whether the arrival state PredictedFinalOrbit computes
// resolves elliptical or hyperbolic relative to the Moon: arriving
// AT another world always wins the "encounter" wording (a flyby is
// still an encounter).
func TestNavigationPlanRowShowsEncounter(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := inclinedCircularEarthOrbitCraft(t, 10, 500e3)
	c := w.ActiveCraft()
	plantProgradeNode(w, c, 3100, "moon") // ~Trans-Lunar-Injection-sized burn, planted in Moon's frame

	planRow := v.buildNavigationBox(w)[7]
	if !strings.Contains(planRow, "Moon encounter") {
		t.Errorf("plan row = %q, want \"Moon encounter\"", planRow)
	}
	if strings.Contains(planRow, "Earth") {
		t.Errorf("plan row = %q, must not still name Earth once the plan ends at the Moon", planRow)
	}
}

// TestNavigationPlanRowShowsEscape (re-grill Q1): a plan that stays at
// the current world but resolves hyperbolic reads "plan: Earth escape"
// with no angles at all.
func TestNavigationPlanRowShowsEscape(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := inclinedCircularEarthOrbitCraft(t, 10, 500e3)
	c := w.ActiveCraft()
	plantProgradeNode(w, c, 20000, "") // far above local escape velocity

	planRow := v.buildNavigationBox(w)[7]
	if !strings.Contains(planRow, "plan:") || !strings.Contains(planRow, "Earth escape") {
		t.Errorf("plan row = %q, want \"plan: Earth escape\"", planRow)
	}
	if strings.Contains(planRow, "AN ") || strings.Contains(planRow, "DN ") || strings.Contains(planRow, "orbit") {
		t.Errorf("plan row = %q, an escaping plan must not carry angles or the word \"orbit\"", planRow)
	}
}

// TestNavigationArrowsAnnotateApPeInclPeriod (re-grill Q3): once a plan
// exists, Ap:/Pe:/incl:/period: each gain a trailing "→ becomes"
// value naming PredictedFinalOrbit's own numbers, never a trend, the
// Ap cell's own ↑/↓ (tested separately) is untouched.
func TestNavigationArrowsAnnotateApPeInclPeriod(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := inclinedCircularEarthOrbitCraft(t, 45, 500e3)
	c := w.ActiveCraft()
	plantProgradeNode(w, c, 50, "")

	lines := v.buildNavigationBox(w)
	apPeRow, inclPeriodRow := lines[3], lines[4]
	for _, row := range []string{apPeRow, inclPeriodRow} {
		if !strings.Contains(row, "→") {
			t.Errorf("row %q missing the plan → annotation", row)
		}
	}
}

// TestNavigationArrowsOmitApPeriodWhenEscaping: an escaping plan has no
// final apoapsis or period to name, so Ap:/Pe:/period: stay
// unannotated, but incl: still gets its arrow (orientation is
// well-defined even hyperbolic).
func TestNavigationArrowsOmitApPeriodWhenEscaping(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := inclinedCircularEarthOrbitCraft(t, 10, 500e3)
	c := w.ActiveCraft()
	plantProgradeNode(w, c, 20000, "")

	lines := v.buildNavigationBox(w)
	apPeRow, inclPeriodRow := lines[3], lines[4]
	if strings.Contains(apPeRow, "→") {
		t.Errorf("Ap/Pe row = %q, an escaping plan must not annotate Ap/Pe", apPeRow)
	}
	if !strings.Contains(inclPeriodRow, "→") {
		t.Errorf("incl/period row = %q, incl: should still get its arrow while escaping", inclPeriodRow)
	}
}

// TestNavigationBoxWidthAtDesignSizeWithPlan confirms the plan: row
// doesn't blow NAVIGATION's column budget at the Design Size (140x40):
// every one of its rows must stay under the ADR's own measured 73-cell
// ascent ceiling (re-grill Q1), and ENGINE's own node: row (rendered in
// the SAME frame, the left column) must still read intact, not
// clobbered by a NAVIGATION overrun.
func TestNavigationBoxWidthAtDesignSizeWithPlan(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	w := inclinedCircularEarthOrbitCraft(t, 45, 500e3)
	c := w.ActiveCraft()
	plantProgradeNode(w, c, 3100, "moon")

	navLines := v.buildNavigationBox(w)
	for i, l := range navLines {
		if width := lipgloss.Width(l); width > 73 {
			t.Errorf("NAVIGATION row %d width %d exceeds the measured 73-cell ceiling: %q", i, width, l)
		}
	}
	engineLines := v.buildEngineBox(w)
	joined := strings.Join(engineLines, "\n")
	if !strings.Contains(joined, "node:") {
		t.Errorf("ENGINE's node: row missing/clobbered once NAVIGATION carries a wide plan:\n%s", joined)
	}
}

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

// subOrbitalClimbCraft parks the world's active craft on Earth in a
// sub-orbital climbing arc (positive radial velocity, periapsis below
// the surface, the impactor shape isSubOrbitalClimb requires) with its
// apoapsis at exactly apoAltM. Mirrors
// TestLaunchHUDRendersOrbitReadyOnApAboveFloor's own construction
// (orbit_launch_hud_test.go), parameterised on apoapsis so the ORBIT
// READY threshold pair below can probe either side of Earth's Orbit
// Floor exactly.
func subOrbitalClimbCraft(t *testing.T, apoAltM float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active craft")
	}
	c.Landed = false
	c.Throttle = 0
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	rApo := primaryR + apoAltM
	rPeri := primaryR - 100e3 // sub-surface periapsis: the impactor shape
	a := (rPeri + rApo) / 2
	vAtPeri := math.Sqrt(mu * (2/rPeri - 1/a))
	c.State.R = orbital.Vec3{X: rPeri}
	c.State.V = orbital.Vec3{Y: vAtPeri}
	c.State.M = c.TotalMass()
	return w
}

// TestNavigationOrbitReadyThresholdOnEarth (ADR 0051 slice 3 item 4):
// permanent version of the orchestrator's own throwaway proof (see the
// slice 3 vault log). Earth's Orbit Floor is its atmosphere cutoff
// (150 km) + OrbitFloorMarginM (25 km) = 175 km (re-grill Q7's own
// stated value), so the badge must light at 185 km apoapsis and stay
// dark at 165 km. Both bracket the retired flat 200 km
// LaunchMissionFloorM this replaced (re-grill Q7): sabotage-checked by
// forcing the gate to a literal 200e3 (185 km failed to light, matching
// the old flat floor exactly) before restoring the real
// sim.OrbitFloorForCraft(c) call.
func TestNavigationOrbitReadyThresholdOnEarth(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())

	lit := subOrbitalClimbCraft(t, 185e3)
	if out := strings.Join(v.buildNavigationBox(lit), "\n"); !strings.Contains(out, "ORBIT READY") {
		t.Errorf("Ap 185 km (above Earth's 175 km Orbit Floor) did not light ORBIT READY:\n%s", out)
	}

	dark := subOrbitalClimbCraft(t, 165e3)
	if out := strings.Join(v.buildNavigationBox(dark), "\n"); strings.Contains(out, "ORBIT READY") {
		t.Errorf("Ap 165 km (below Earth's 175 km Orbit Floor) lit ORBIT READY:\n%s", out)
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
