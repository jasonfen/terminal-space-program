package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/missions"
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

// TestLaunchMissionProgressMatchesCircularizeFromPad — when the world
// has an in-flight circularize_from_pad mission for the active
// craft's primary, the progress line shows current pe / target.
// v0.9.4+.
func TestLaunchMissionProgressMatchesCircularizeFromPad(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected active craft from NewWorld")
	}
	// Inject an in-flight circularize_from_pad mission for the craft's primary
	// (the embedded ladder no longer ships one — it can't verify a pad launch
	// — so this exercises the launch-HUD path directly).
	w.Missions = []missions.Mission{{
		ID: "pad",
		Objectives: []missions.Objective{{
			Kind:   missions.KindCircularizeFromPad,
			Params: missions.Params{PrimaryID: c.Primary.ID, MinPeriapsisAltM: 200_000},
		}},
	}}
	got := launchMissionProgress(w, c, 130_000)
	if got == "" {
		t.Fatal("expected non-empty progress for active circularize_from_pad mission")
	}
	if !strings.Contains(got, "200") {
		t.Errorf("progress %q should reference the 200 km mission floor", got)
	}
	if !strings.Contains(got, "130") {
		t.Errorf("progress %q should reference the current pe altitude", got)
	}
}

// TestLaunchMissionProgressEmptyWithoutMission — with the bundled
// circularize_from_pad mission marked Passed, the helper returns ""
// so the LAUNCH HUD doesn't emit a stray row. v0.9.4+.
func TestLaunchMissionProgressEmptyWithoutMission(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected active craft from NewWorld")
	}
	// A Passed circularize_from_pad mission yields no launch-HUD row.
	w.Missions = []missions.Mission{{
		ID:     "pad",
		Status: missions.Passed,
		Objectives: []missions.Objective{{
			Kind:   missions.KindCircularizeFromPad,
			Params: missions.Params{PrimaryID: c.Primary.ID, MinPeriapsisAltM: 200_000},
		}},
	}}
	if got := launchMissionProgress(w, c, 130_000); got != "" {
		t.Errorf("progress with no in-flight mission = %q, want \"\"", got)
	}
}

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
	lines := v.buildLaunchChip(w)
	for _, prefix := range []string{"Ap:", "apo:", "Δv→circ:"} {
		got := row(lines, prefix)
		if !strings.HasSuffix(got, "—") {
			t.Errorf("on the pad, %q row should be a steady em-dash; got %q", prefix, got)
		}
	}
	// The pad-relevant rows must still be present.
	if row(lines, "TWR:") == "" || row(lines, "hold:") == "" {
		t.Errorf("LAUNCH chip on the pad lost TWR/hold rows:\n%s", strings.Join(lines, "\n"))
	}
}

// TestLaunchChipEngineLitIndicator — #427 / ADR 0048 §3: the launch HUD
// had no engine-lit state at all (the review's own finding: after
// pressing z then b there was no way to tell from the screen whether the
// engine fired). The TWR: row now carries an ignition indicator that
// reads "off" before ignition and "LIT" once the engine is actually
// producing thrust (a live ManualBurn or ActiveBurn) — not the
// throttle: setting, which sits at its loadout default whether or not
// anything is burning.
func TestLaunchChipEngineLitIndicator(t *testing.T) {
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

	// Pre-ignition: engine reads off even though throttle is at its
	// loadout default (100%) — the point of the fix is that ignition, not
	// throttle setting, is what "LIT" answers.
	lines := v.buildLaunchChip(w)
	twrRow := ""
	for _, l := range lines {
		if strings.Contains(l, "TWR:") {
			twrRow = l
			break
		}
	}
	if twrRow == "" {
		t.Fatalf("no TWR: row in LAUNCH chip:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(twrRow, "engine:") || !strings.Contains(twrRow, "off") {
		t.Errorf("pre-ignition TWR: row should show the engine off, got %q", twrRow)
	}
	if strings.Contains(twrRow, "LIT") {
		t.Errorf("pre-ignition TWR: row already reads LIT: %q", twrRow)
	}

	// Ignite (mirrors the `b` key: ToggleManualBurn) — the same row must
	// now read LIT.
	w.ToggleManualBurn()
	if c.ManualBurn == nil {
		t.Fatal("setup: ToggleManualBurn did not arm ManualBurn")
	}
	lines = v.buildLaunchChip(w)
	twrRow = ""
	for _, l := range lines {
		if strings.Contains(l, "TWR:") {
			twrRow = l
			break
		}
	}
	if !strings.Contains(twrRow, "LIT") {
		t.Errorf("after ignition the TWR: row should read LIT, got %q", twrRow)
	}
}
