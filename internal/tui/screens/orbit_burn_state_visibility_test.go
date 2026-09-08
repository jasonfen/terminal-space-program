package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// This file covers the UX batch grilled 2026-09-06 ("burn state is
// invisible", designdocs/terminal-space-program/ux-reviews/20260902-1059/
// triage/action-plan.md): decisions 1, 2, 3, 6, 7. Decision 4 (the burn
// fired/finished Event Flash) is a sim-level hook and gets its own tests
// alongside LastDockEvent/LastNodeTargetRefusal's pattern in
// internal/sim. Decision 5 is confirmed no-change.

// TestThrottleRowIdleVsFiring — decision 1: the VESSEL chip's throttle
// row always shows the throttle SETTING, suffixed with "(idle)" (Dim)
// while neither ActiveBurn nor ManualBurn is live on the active craft,
// or "● FIRING" (Warning) the instant either is — never gated on the
// number itself.
func TestThrottleRowIdleVsFiring(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	out := strings.Join(v.buildVesselChip(w), "\n")
	if !strings.Contains(out, "throttle:  100% (idle)") {
		t.Errorf("idle throttle row missing '(idle)' suffix:\n%s", out)
	}
	if strings.Contains(out, "FIRING") {
		t.Errorf("idle throttle row should not read FIRING:\n%s", out)
	}

	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	out = strings.Join(v.buildVesselChip(w), "\n")
	if !strings.Contains(out, "throttle:  100% ● FIRING") {
		t.Errorf("live-ActiveBurn throttle row missing '● FIRING':\n%s", out)
	}
	if strings.Contains(out, "(idle)") {
		t.Errorf("firing throttle row should not still read (idle):\n%s", out)
	}

	c.ActiveBurn = nil
	c.ManualBurn = &spacecraft.ManualBurn{}
	out = strings.Join(v.buildVesselChip(w), "\n")
	if !strings.Contains(out, "● FIRING") {
		t.Errorf("live ManualBurn throttle row missing '● FIRING':\n%s", out)
	}
}

// TestEngineLitBorderAndBadge — decision 2: while any craft in the slate
// is thrusting, the map canvas border switches from Primary to Warning,
// and the title bar carries a "● BURN" badge — both clear the instant
// nothing is thrusting.
func TestEngineLitBorderAndBadge(t *testing.T) {
	v := NewOrbitView(plainThemeColored())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	if got, want := v.canvasBorderColor(w), v.theme.Primary.GetForeground(); got != want {
		t.Errorf("idle border color = %v, want Primary %v", got, want)
	}
	title := v.renderTitleBar("Sol", w, 140)
	if strings.Contains(title, "BURN") {
		t.Errorf("idle title bar should not carry a BURN badge:\n%s", title)
	}

	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	if got, want := v.canvasBorderColor(w), v.theme.Warning.GetForeground(); got != want {
		t.Errorf("firing border color = %v, want Warning %v", got, want)
	}
	title = v.renderTitleBar("Sol", w, 140)
	if !strings.Contains(title, "BURN") {
		t.Errorf("firing title bar missing the BURN badge:\n%s", title)
	}

	c.ActiveBurn = nil
	if got, want := v.canvasBorderColor(w), v.theme.Primary.GetForeground(); got != want {
		t.Errorf("border color did not clear when thrust stopped: got %v, want Primary %v", got, want)
	}
	title = v.renderTitleBar("Sol", w, 140)
	if strings.Contains(title, "BURN") {
		t.Errorf("BURN badge did not clear when thrust stopped:\n%s", title)
	}
}

// TestEngineLitBorderNonActiveCraft — decision 2's "the whole screen"
// framing: AnyCraftThrusting walks the whole slate, so a burn on a
// non-active craft still lights the border/badge while the player flies
// a different vessel.
func TestEngineLitBorderNonActiveCraft(t *testing.T) {
	v := NewOrbitView(plainThemeColored())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 500e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	w.Crafts[1].ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}

	if got, want := v.canvasBorderColor(w), v.theme.Warning.GetForeground(); got != want {
		t.Errorf("border should light for a non-active craft's burn: got %v, want Warning %v", got, want)
	}
}

// TestLaunchViewEngineLitBorderAndBadge — decision 2's Launch View parity:
// the pad canvas shares the same border tint and title badge as the map.
func TestLaunchViewEngineLitBorderAndBadge(t *testing.T) {
	orbitV := NewOrbitView(plainThemeColored())
	lv := NewLaunchView(plainThemeColored(), orbitV)
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

	out := lv.Render(w, 140, 40)
	if strings.Contains(out, "BURN") {
		t.Errorf("idle launch title should not carry a BURN badge:\n%s", firstLine(out))
	}

	c.Landed = false
	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	out = lv.Render(w, 140, 40)
	if !strings.Contains(out, "BURN") {
		t.Errorf("firing launch title missing the BURN badge:\n%s", firstLine(out))
	}
}

// TestTargetChipRecomputesDuringOwnBurn — decision 3: while the active
// craft has a live ActiveBurn, the predicted-encounter row group
// (perilune/approach + TCA for a body target, TCA/CA for a craft target)
// collapses to one "encounter: recomputing…" row; live rows above it
// (range:, Δi:, |v_rel|:, closing:, lead:) are unaffected.
func TestTargetChipRecomputesDuringOwnBurn(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}

	out := strings.Join(v.buildTargetChip(w), "\n")
	if strings.Contains(out, "recomputing") {
		t.Errorf("no-burn TARGET chip should not read recomputing:\n%s", out)
	}
	if !strings.Contains(out, "range:") {
		t.Errorf("body-target chip missing range: row:\n%s", out)
	}

	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	out = strings.Join(v.buildTargetChip(w), "\n")
	if !strings.Contains(out, "encounter:") || !strings.Contains(out, "recomputing") {
		t.Errorf("mid-burn body-target chip missing 'encounter: recomputing…':\n%s", out)
	}
	if strings.Contains(out, "perilune:") || strings.Contains(out, "approach:") || strings.Contains(out, "TCA:") {
		t.Errorf("mid-burn body-target chip still carries stale perilune/approach/TCA rows:\n%s", out)
	}
	if !strings.Contains(out, "range:") {
		t.Errorf("mid-burn body-target chip lost its live range: row:\n%s", out)
	}
}

// TestTargetChipCraftRecomputesDuringOwnBurn — decision 3's craft-target
// branch: closestApproachRows' TCA:/CA: pair is replaced the same way.
func TestTargetChipCraftRecomputesDuringOwnBurn(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := leadTestWorld(t, 82)
	c := w.ActiveCraft()

	out := strings.Join(v.buildTargetChip(w), "\n")
	if strings.Contains(out, "recomputing") {
		t.Errorf("no-burn craft-target chip should not read recomputing:\n%s", out)
	}

	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	out = strings.Join(v.buildTargetChip(w), "\n")
	if !strings.Contains(out, "encounter:") || !strings.Contains(out, "recomputing") {
		t.Errorf("mid-burn craft-target chip missing 'encounter: recomputing…':\n%s", out)
	}
	if strings.Contains(out, "TCA:") {
		t.Errorf("mid-burn craft-target chip still carries a stale TCA: row:\n%s", out)
	}
	if !strings.Contains(out, "|v_rel|:") || !strings.Contains(out, "closing:") || !strings.Contains(out, "lead:") {
		t.Errorf("mid-burn craft-target chip lost its live relative-state rows:\n%s", out)
	}
}

// TestTargetChipEllipsisCellWidthConsistent guards the same chip →
// canvas width contract assertChipCellWidthConsistent exists for
// (orbit_chips_test.go): the "recomputing…" row's ellipsis must measure
// the same in lipgloss.Width as it does spliced per-cell, or the chip's
// right edge silently misaligns against the canvas overlay.
func TestTargetChipEllipsisCellWidthConsistent(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	lines := v.buildTargetChip(w)
	assertChipCellWidthConsistent(t, "TARGET recomputing", lines)
}

// TestDeclutterFooterAndTitleTag — decision 6: while F2 declutter is on,
// the canvas footer's view label gets a trailing " · declutter" and the
// title bar carries a Dim "[F2 declutter]" tag; both clear the instant
// F2 toggles back off.
func TestDeclutterFooterAndTitleTag(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	out := v.Render(w, 0, 140, 40)
	if strings.Contains(out, "declutter") {
		t.Errorf("declutter cue showing while F2 is off:\n%s", lastLine(out))
	}

	v.SetDeclutter(true)
	out = v.Render(w, 0, 140, 40)
	if !strings.Contains(out, "· declutter") {
		t.Errorf("footer missing '· declutter' while F2 is on:\n%s", lastLine(out))
	}
	if !strings.Contains(out, "[F2 declutter]") {
		t.Errorf("title bar missing '[F2 declutter]' while F2 is on:\n%s", firstLine(out))
	}

	v.SetDeclutter(false)
	out = v.Render(w, 0, 140, 40)
	if strings.Contains(out, "declutter") {
		t.Errorf("declutter cue did not clear when F2 toggled back off:\n%s", out)
	}
}

// TestLaunchViewDeclutterTitleTag — decision 6's Launch View parity: the
// shared OrbitView declutter state also tags the launch title bar.
func TestLaunchViewDeclutterTitleTag(t *testing.T) {
	orbitV := NewOrbitView(plainThemeColored())
	lv := NewLaunchView(plainThemeColored(), orbitV)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}

	out := lv.Render(w, 140, 40)
	if strings.Contains(out, "declutter") {
		t.Errorf("launch title carries declutter cue while F2 is off:\n%s", firstLine(out))
	}

	orbitV.SetDeclutter(true)
	out = lv.Render(w, 140, 40)
	if !strings.Contains(firstLine(out), "[F2 declutter]") {
		t.Errorf("launch title missing '[F2 declutter]' while F2 is on:\n%s", firstLine(out))
	}
}

// TestNodesChipHeadRowCountsToIgnitionThenBurnEnd — decision 7: the
// head row for the next resolved node counts to BurnStart while waiting
// ("ignition in Ns"), and once that craft's burn is live, the equivalent
// row (now sourced from ActiveBurn) counts to BurnEnd ("burning, Ns
// left") instead of the old bare "T-Ns" wording — same vessel/mode/Δv
// figures either way.
func TestNodesChipHeadRowCountsToIgnitionThenBurnEnd(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	// A finite (Duration>0) node so BurnStart precedes TriggerTime: with
	// a 72s duration centered on a node 66s out, ignition is 30s away.
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		DV:          3054,
		Mode:        spacecraft.BurnPrograde,
		Duration:    72 * time.Second,
		TriggerTime: w.Clock.SimTime.Add(66 * time.Second),
	})

	out := strings.Join(v.buildNodesChip(w), "\n")
	if !strings.Contains(out, "ignition in 30s") {
		t.Errorf("waiting head row should read 'ignition in 30s' (BurnStart, not TriggerTime):\n%s", out)
	}
	if strings.Contains(out, "T-66s") || strings.Contains(out, "T+66s") {
		t.Errorf("waiting head row still counts to TriggerTime:\n%s", out)
	}

	c.Nodes = nil
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 3054,
		EndTime:     w.Clock.SimTime.Add(61 * time.Second),
	}
	out = strings.Join(v.buildNodesChip(w), "\n")
	if !strings.Contains(out, "burning, 61s left") {
		t.Errorf("live-burn head row should read 'burning, 61s left' (BurnEnd):\n%s", out)
	}
	if strings.Contains(out, "T-61s") {
		t.Errorf("live-burn head row still uses the old T-Ns wording:\n%s", out)
	}
}

// TestNodesChipHeadRowClampsOverdueIgnitionToZero — code-review finding
// 4: a node whose BurnStart has already passed but which hasn't fired
// yet (paused right at the boundary, or held past due) must not print a
// raw negative duration ("ignition in -47s"); it clamps to "ignition in
// 0s".
func TestNodesChipHeadRowClampsOverdueIgnitionToZero(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	// BurnStart = TriggerTime - Duration/2 = now - 12s - 36s = now - 48s:
	// 48s past due, still queued (not fired — this test doesn't tick the
	// world at all).
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		DV:          3054,
		Mode:        spacecraft.BurnPrograde,
		Duration:    72 * time.Second,
		TriggerTime: w.Clock.SimTime.Add(-12 * time.Second),
	})

	out := strings.Join(v.buildNodesChip(w), "\n")
	if !strings.Contains(out, "ignition in 0s") {
		t.Errorf("overdue-but-unfired head row should clamp to 'ignition in 0s':\n%s", out)
	}
	if strings.Contains(out, "ignition in -") {
		t.Errorf("head row printed a raw negative duration:\n%s", out)
	}
}

// plainThemeColored gives Primary/Warning/Dim distinguishable ANSI
// colors (unlike chipTestTheme's no-op styles) so tests can assert an
// actual color switch, not just presence of text.
func plainThemeColored() Theme {
	return Theme{
		Primary: lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		Alert:   lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		Dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}
