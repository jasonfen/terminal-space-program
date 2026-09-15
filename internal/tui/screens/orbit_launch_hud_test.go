package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// The three-band altitude formatter this used to pin
// (TestFormatAltKmThresholds, pre-ADR-0049), the duration-band coverage
// (TestFormatDurationShortBands), and the period formatter
// (TestFormatPeriodKeepsSeconds) all now live on internal/tui/readout's
// own TestDistance / TestDuration / TestCountdown / TestPeriod:
// formatAltKm, formatDurationShort and formatPeriod are deleted, every
// screens/ call site routes through readout.Distance / readout.Duration /
// readout.Countdown / readout.Period instead (ADR 0049 stage A2).

// (launchMissionProgress, the circularize_from_pad-specific "pe / target"
// progress line the retired SURFACE chip used to append, is deleted along
// with buildLaunchChip: it had no other caller, and no ADR 0051 box
// reproduces it, the MISSION box's own objective/description display is
// a different thing. This is a real dropped feature, not covered by any
// live code path today; flagged in the slice 2a progress log rather than
// silently carried as a dead-code test.)

// TestLaunchHUDRendersOrbitReadyOnApAboveFloor — drives the LAUNCH
// HUD directly by mutating the active craft's state into an
// apoapsis-above-floor configuration, then checks for the ORBIT
// READY callout in the rendered output. This is the rendezvous-
// style live-signal pattern (DOCK READY at threshold ↔ ORBIT READY
// at threshold) ported to the launch flow. v0.9.4+.
func TestLaunchHUDRendersOrbitReadyOnApAboveFloor(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
	v := NewOrbitView(th)
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = false // ensure orbital math runs
	c.Throttle = 0
	c.AttitudeMode = spacecraft.BurnPrograde
	// Sub-orbital arc with apoapsis at +250 km altitude (above the
	// 200 km mission floor) and periapsis at -100 km altitude
	// (impactor — exactly the post-engine-cut state where the player
	// needs to plant `C`).
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	rApo := primaryR + 250e3
	rPeri := primaryR - 100e3
	a := (rPeri + rApo) / 2
	vAtPeri := math.Sqrt(mu * (2/rPeri - 1/a))
	c.State.R.X, c.State.R.Y, c.State.R.Z = rPeri, 0, 0
	c.State.V.X, c.State.V.Y, c.State.V.Z = 0, vAtPeri, 0

	out := v.Render(w, 0, 200, 60)
	if !strings.Contains(out, "ORBIT READY") {
		t.Errorf("expected LAUNCH HUD to surface ORBIT READY callout for "+
			"sub-orbital arc with apo above 200km floor; rendered output:\n%s",
			out)
	}
	if !strings.Contains(out, "Ap:") {
		t.Errorf("expected LAUNCH HUD to surface live Ap row")
	}
	if !strings.Contains(out, "Δv→circ") {
		t.Errorf("expected LAUNCH HUD to surface Δv→circ row")
	}
}

// TestLaunchChipSteadyOnPad: a Landed craft sits at the apoapsis of its
// co-rotation pseudo-orbit, so apoAlt hovers at exactly 0 and the
// apoAlt>0 / rApo>primaryR gates would flip on numerical noise tick-to-
// tick, flashing Ap / apo / Δv→circ between a value and "—". On the
// pad those predictions are suppressed to a steady "—" (no real orbit
// yet); TWR / SAS still render. Regression for the launchpad flicker.
func TestLaunchChipSteadyOnPad(t *testing.T) {
	v := NewOrbitView(Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle(),
	})
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed || !shouldShowLaunchHUD(c) {
		t.Fatalf("setup: want a Landed craft with the LAUNCH chip up (landed=%v, show=%v)", c.Landed, shouldShowLaunchHUD(c))
	}
	row := func(lines []string, prefix string) string {
		for _, l := range lines {
			if s := strings.TrimSpace(l); strings.HasPrefix(s, prefix) {
				return s
			}
		}
		return ""
	}
	// Ap:/Δv→circ: split across NAVIGATION and PROPELLANT now (decision 1);
	// "apo:" retired for good (folded onto the Ap: cell itself, decision
	// 10) so there's no separate row to check.
	navRow := row(v.buildNavigationBox(w), "Ap:")
	if !strings.HasSuffix(navRow, "—") {
		t.Errorf("on the pad, NAVIGATION's Ap: row should be a steady em-dash; got %q", navRow)
	}
	propRow := row(v.buildPropellantBox(w), "Δv:")
	if !strings.Contains(propRow, "Δv→circ:") || !strings.HasSuffix(strings.TrimSpace(propRow), "—") {
		t.Errorf("on the pad, PROPELLANT's Δv→circ: cell should be a steady em-dash; got %q", propRow)
	}
	// TWR:/hold: are now permanent rows on ENGINE/GUIDANCE (decision 2:
	// every box, every row, always drawn), always present by
	// construction, but confirm they're not somehow blank on the pad.
	engineRow := row(v.buildEngineBox(w), "TWR:")
	if engineRow == "" {
		t.Errorf("ENGINE lost its TWR: row on the pad")
	}
	guidanceRow := row(v.buildGuidanceBox(w), "hold:")
	if guidanceRow == "" {
		t.Errorf("GUIDANCE lost its hold: row on the pad")
	}
}

// TestEngineBoxThrottleRowShowsLitIndicator, #427 / ADR 0048 §3: the
// HUD had no engine-lit state at all (the review's own finding: after
// pressing z then b there was no way to tell from the screen whether the
// engine fired). ADR 0051 decision 1 moves this onto ENGINE's own
// throttle: row, which now carries the ignition indicator directly
// ("idle" before ignition, "● FIRING" once the engine is actually
// producing thrust, a live ManualBurn or ActiveBurn) instead of a
// separate "engine:" tag riding on the TWR: row.
func TestEngineBoxThrottleRowShowsLitIndicator(t *testing.T) {
	v := NewOrbitView(Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle(),
	})
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}

	// Pre-ignition: throttle: reads idle even though the setting is at
	// its loadout default (100%), the point of the fix is that
	// ignition, not throttle setting, is what "firing" answers.
	throttleRow := func() string {
		for _, l := range v.buildEngineBox(w) {
			if strings.Contains(l, "throttle:") {
				return l
			}
		}
		return ""
	}
	row := throttleRow()
	if row == "" {
		t.Fatalf("no throttle: row in ENGINE box")
	}
	if !strings.Contains(row, "idle") {
		t.Errorf("pre-ignition throttle: row should read idle, got %q", row)
	}
	if strings.Contains(row, "FIRING") {
		t.Errorf("pre-ignition throttle: row already reads FIRING: %q", row)
	}

	// Ignite (mirrors the `b` key: ToggleManualBurn) — the same row must
	// now read FIRING.
	w.ToggleManualBurn()
	if c.ManualBurn == nil {
		t.Fatal("setup: ToggleManualBurn did not arm ManualBurn")
	}
	row = throttleRow()
	if !strings.Contains(row, "FIRING") {
		t.Errorf("after ignition the throttle: row should read FIRING, got %q", row)
	}
}
