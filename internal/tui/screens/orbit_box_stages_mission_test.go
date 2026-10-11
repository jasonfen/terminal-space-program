package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestStagesBoxRendersSingleStageVessel: decision 5 amends the retired
// buildStagesChip's "nil for len(Stages) <= 1", every vessel gets a
// STAGES box now, single-stage vehicles included.
func TestStagesBoxRendersSingleStageVessel(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{{Name: "solo stage", FuelMass: 100, FuelCapacity: 100}}
	lines := v.buildStagesBox(w)
	if len(lines) != 1 {
		t.Fatalf("buildStagesBox returned %d lines, want 1 (STAGES is a single combined line)", len(lines))
	}
	if !strings.Contains(lines[0], "solo stage") {
		t.Errorf("STAGES line = %q, want the single stage named", lines[0])
	}
}

// TestMissionBoxDashWhenNothingToReport: decision 2, MISSION never
// vanishes; with no active mission and no ladder sendoff, it reads a
// dash rather than dropping (the retired buildMissionsChip returned nil
// here).
func TestMissionBoxDashWhenNothingToReport(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = nil
	lines := v.buildMissionBox(w)
	if len(lines) != 1 || !strings.Contains(lines[0], "—") {
		t.Errorf("buildMissionBox with no mission = %v, want a single dash line", lines)
	}
}

// TestStagesBoxNamesSpaceAndFitsTheTier (ADR 0052 decision 5): with more than
// one stage the row names what [space] drops, and a long stage name is
// truncated to make room rather than pushing the row past its tier width.
func TestStagesBoxNamesSpaceAndFitsTheTier(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "S-IC", FuelMass: 1, FuelCapacity: 1},
		{Name: "S-II", FuelMass: 1, FuelCapacity: 1},
		{Name: "S-IVB", FuelMass: 1, FuelCapacity: 1},
	}
	line := v.buildStagesBox(w)[0]
	if !strings.Contains(line, "▸ S-IC (1/3) [space]") {
		t.Errorf("STAGES row = %q, want it to contain %q", line, "▸ S-IC (1/3) [space]")
	}

	c.Stages[0].Name = "An Extremely Long Booster Stage Name Indeed"
	line = v.buildStagesBox(w)[0]
	if !strings.Contains(line, "(1/3) [space]") {
		t.Errorf("long name pushed the suffix out: %q", line)
	}
	if got := lipgloss.Width(line); got > tierBottomLeftWidth-2 {
		t.Errorf("STAGES row is %d cells, budget is %d: %q", got, tierBottomLeftWidth-2, line)
	}

	// A lone stage cannot be dropped, so the row does not promise a drop.
	c.Stages = c.Stages[:1]
	if line := v.buildStagesBox(w)[0]; strings.Contains(line, "[space]") {
		t.Errorf("single-stage row advertises [space]: %q", line)
	}
}

// stagePipSGR returns the escape sequence that opens the styled run
// holding glyph in line ("" if the glyph is absent or unstyled).
func stagePipSGR(line, glyph string) string {
	i := strings.Index(line, glyph)
	if i < 0 {
		return ""
	}
	j := strings.LastIndex(line[:i], "\x1b[")
	if j < 0 {
		return ""
	}
	k := strings.Index(line[j:], "m")
	if j+k+1 != i || line[j:j+k+1] == "\x1b[0m" {
		return ""
	}
	return line[j : j+k+1]
}

// TestStagePipsDistinguishLitSpentAndTankless (#600): the lit stage
// (Stages[0], fuel aboard), a fuelled stage waiting above it, a spent
// stage and a tankless stage (payload, pod) read as four different pips,
// each its own glyph in its own colour. Colour assertions
// are vacuous without a forced profile, so TrueColor is forced and the
// instrument is proven to emit an escape before any negative is trusted.
func TestStagePipsDistinguishLitSpentAndTankless(t *testing.T) {
	orig := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(orig) })

	theme := launchThemeForTest()
	theme.Primary = lipgloss.NewStyle().Foreground(lipgloss.Color("#5FD7FF"))
	theme.Warning = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAF00"))
	theme.Alert = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F5F"))
	theme.Dim = lipgloss.NewStyle().Foreground(lipgloss.Color("#5F5F5F"))
	if !strings.Contains(theme.Alert.Render("x"), "\x1b[") {
		t.Fatal("setup: forcing TrueColor did not make a styled render emit an escape")
	}

	v := NewOrbitView(theme)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "Lit", FuelMass: 100, FuelCapacity: 100},
		{Name: "Waiting", FuelMass: 100, FuelCapacity: 100},
		{Name: "Spent", FuelMass: 0, FuelCapacity: 100},
		{Name: "Pod"},
	}
	line := v.buildStagesBox(w)[0]

	type pip struct{ glyph, sgr string }
	var pips []pip
	for _, g := range []string{"◉", "●", "○", "◇"} {
		if !strings.Contains(line, g) {
			t.Errorf("STAGES row %q is missing glyph %q (lit ◉, waiting ●, spent ○, tankless ◇)", line, g)
			continue
		}
		s := stagePipSGR(line, g)
		if s == "" {
			t.Errorf("glyph %q is unstyled in %q", g, line)
		}
		pips = append(pips, pip{g, s})
	}
	for i := range pips {
		for j := i + 1; j < len(pips); j++ {
			if pips[i].sgr == pips[j].sgr {
				t.Errorf("pips %q and %q share colour %q, want four distinct colours", pips[i].glyph, pips[j].glyph, pips[i].sgr)
			}
		}
	}
	// Review #614 M2 S4s: the spent pip is the same Warning amber as
	// ENGINE's "✕ DRY" (engineThrottleLabel), not merely a distinct colour.
	want := stagePipSGR(theme.Warning.Render("○"), "○")
	if want == "" {
		t.Fatal("setup: Warning.Render did not style the glyph")
	}
	if got := stagePipSGR(line, "○"); got != want {
		t.Errorf("spent pip paints %q, want ENGINE's DRY colour (Warning) %q", got, want)
	}
}

// TestStagePipsStayWithinCap: a long stack's pips are cut to stagePipsMax
// cells with a trailing ellipsis, whatever the styling.
func TestStagePipsStayWithinCap(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Stages = nil
	for i := 0; i < 12; i++ {
		c.Stages = append(c.Stages, spacecraft.Stage{Name: "s", FuelMass: 1, FuelCapacity: 1})
	}
	line := v.buildStagesBox(w)[0]
	want := "◉" + strings.Repeat("●", stagePipsMax-2) + "…"
	if !strings.Contains(line, want) {
		t.Errorf("STAGES row = %q, want pips truncated to %q", line, want)
	}
}
