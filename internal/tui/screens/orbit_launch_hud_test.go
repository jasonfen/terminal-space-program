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

// TestFormatAltKmThresholds exercises the three-band formatter the
// LAUNCH HUD's ap / pe rows render through. Bands chosen so altitude
// reads naturally on the pad (`+0 m`), in mid-ascent (`+12.3 km`),
// and once orbit lifts out to body-radius scale (`+1234 km`).
// v0.9.5+.
func TestFormatAltKmThresholds(t *testing.T) {
	cases := []struct {
		altM float64
		want string
	}{
		{0, "+0 m"},
		{0.5, "+0 m"},
		{12, "+12 m"},
		{1500, "+1.5 km"},
		{50_000, "+50.0 km"},
		{200_000, "+200.0 km"},
		{1_500_000, "+1500 km"},
		{-2_840_000, "-2840 km"},
		{-100, "-100 m"},
	}
	for _, c := range cases {
		got := formatAltKm(c.altM)
		if got != c.want {
			t.Errorf("formatAltKm(%g) = %q, want %q", c.altM, got, c.want)
		}
	}
}

// TestFormatDurationShortBands exercises the three duration bands:
// seconds (`12s`), minutes (`3m45s`), and hours (`1h22m`). Used by
// the LAUNCH HUD's t_to_apo row. v0.9.5+.
func TestFormatDurationShortBands(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{0, "0s"},
		{12, "12s"},
		{59.4, "59s"},
		{60, "1m00s"},
		{225, "3m45s"},
		{3599, "59m59s"},
		{3600, "1h00m"},
		{4920, "1h22m"},
	}
	for _, c := range cases {
		got := formatDurationShort(c.sec)
		if got != c.want {
			t.Errorf("formatDurationShort(%g) = %q, want %q", c.sec, got, c.want)
		}
	}
}

// TestLaunchMissionProgressMatchesCircularizeFromPad — when the world
// has an in-flight circularize_from_pad mission for the active
// craft's primary, the progress line shows current pe / target.
// v0.9.5+.
func TestLaunchMissionProgressMatchesCircularizeFromPad(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected active craft from NewWorld")
	}
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
// so the LAUNCH HUD doesn't emit a stray row. v0.9.5+.
func TestLaunchMissionProgressEmptyWithoutMission(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected active craft from NewWorld")
	}
	for i := range w.Missions {
		if w.Missions[i].Type == missions.TypeCircularizeFromPad {
			w.Missions[i].Status = missions.Passed
		}
	}
	if got := launchMissionProgress(w, c, 130_000); got != "" {
		t.Errorf("progress with no in-flight mission = %q, want \"\"", got)
	}
}

// TestLaunchHUDRendersOrbitReadyOnApAboveFloor — drives the LAUNCH
// HUD directly by mutating the active craft's state into an
// apoapsis-above-floor configuration, then checks for the ORBIT
// READY callout in the rendered output. This is the rendezvous-
// style live-signal pattern (DOCK READY at threshold ↔ ORBIT READY
// at threshold) ported to the launch flow. v0.9.5+.
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
	if !strings.Contains(out, "ap:") {
		t.Errorf("expected LAUNCH HUD to surface live ap row")
	}
	if !strings.Contains(out, "Δv→circ") {
		t.Errorf("expected LAUNCH HUD to surface Δv→circ row")
	}
}
