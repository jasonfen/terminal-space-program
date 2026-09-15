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
// internal/sim. Decision 5 is confirmed no-change. Decision 2's original
// implementation (canvas-border color swap + title-bar badge) was
// revised after live playtesting found the whole-screen treatment too
// loud — the `● BURN` cue now lives on the VESSEL chip instead
// (vesselBurnBadge, orbit_chips.go); see this file's TestVesselChip*
// tests, not a border/title-bar test.

// TestThrottleRowIdleVsFiring, decision 1: ENGINE's throttle row always
// shows the throttle SETTING, suffixed with "idle" (Dim) while neither
// ActiveBurn nor ManualBurn is live on the active craft, or "● FIRING"
// (Warning) the instant either is, never gated on the number itself.
// Migrated from the retired VESSEL chip onto buildEngineBox (ADR 0051);
// the box's own engineThrottleLabel carries this logic now.
func TestThrottleRowIdleVsFiring(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	out := strings.Join(v.buildEngineBox(w), "\n")
	if !strings.Contains(out, "100% idle") {
		t.Errorf("idle throttle row missing the idle suffix:\n%s", out)
	}
	if strings.Contains(out, "FIRING") {
		t.Errorf("idle throttle row should not read FIRING:\n%s", out)
	}

	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	out = strings.Join(v.buildEngineBox(w), "\n")
	if !strings.Contains(out, "100%") || !strings.Contains(out, "● FIRING") {
		t.Errorf("live-ActiveBurn throttle row missing '● FIRING':\n%s", out)
	}
	if strings.Contains(out, "idle") {
		t.Errorf("firing throttle row should not still read idle:\n%s", out)
	}

	c.ActiveBurn = nil
	c.ManualBurn = &spacecraft.ManualBurn{}
	out = strings.Join(v.buildEngineBox(w), "\n")
	if !strings.Contains(out, "● FIRING") {
		t.Errorf("live ManualBurn throttle row missing '● FIRING':\n%s", out)
	}
}

// TestVesselChipBurnBadge — decision 2 (grilled 2026-09-06, revised on
// live playtest feedback): while any craft in the slate is thrusting,
// ENGINE's header carries a "● BURN" badge, clearing the instant
// nothing is thrusting. Originally a canvas-border color swap plus a
// title-bar badge; that whole-screen treatment read as too loud in
// play, so the cue now lives on one instrument box instead (still named
// vesselBurnBadge, shared by the box builder). Also locks in that the
// removed locations (border color, title bar) stay gone. Migrated from
// the retired VESSEL chip onto buildEngineBox (ADR 0051); the Compact
// Form assertions are dropped since none of the fixed instrument boxes
// has a Compact Form (decision 2: never resize).
func TestVesselChipBurnBadge(t *testing.T) {
	v := NewOrbitView(plainThemeColored())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	out := strings.Join(v.buildEngineBox(w), "\n")
	if strings.Contains(out, "BURN") {
		t.Errorf("idle ENGINE box should not carry a BURN badge:\n%s", out)
	}
	title := v.renderTitleBar("Sol", w, 140)
	if strings.Contains(title, "BURN") {
		t.Errorf("title bar should never carry a BURN badge (moved to ENGINE):\n%s", title)
	}

	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	out = strings.Join(v.buildEngineBox(w), "\n")
	if !strings.Contains(out, "BURN") {
		t.Errorf("firing ENGINE box missing the BURN badge:\n%s", out)
	}
	title = v.renderTitleBar("Sol", w, 140)
	if strings.Contains(title, "BURN") {
		t.Errorf("title bar picked up a BURN badge, want it only on ENGINE:\n%s", title)
	}

	c.ActiveBurn = nil
	out = strings.Join(v.buildEngineBox(w), "\n")
	if strings.Contains(out, "BURN") {
		t.Errorf("BURN badge did not clear when thrust stopped:\n%s", out)
	}
}

// TestVesselChipBurnBadgeNonActiveCraft — decision 2's "the whole
// screen" framing survives the move: AnyCraftThrusting walks the whole
// slate, so a burn on a non-active craft still badges the VESSEL chip
// (of whichever craft you're currently flying) — the player needs to
// know why warp is capped even when the burning craft isn't the one on
// screen.
func TestVesselChipBurnBadgeNonActiveCraft(t *testing.T) {
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

	out := strings.Join(v.buildEngineBox(w), "\n")
	if !strings.Contains(out, "BURN") {
		t.Errorf("ENGINE box should badge for a non-active craft's burn:\n%s", out)
	}
}

// TestVesselChipBurnBadgeOtherSystem — #455 review finding 2:
// AnyCraftThrusting is slate-wide AND system-blind, same as the 10x
// burn-warp cap, so the badge must still show while the camera has
// browsed away from the active craft's own system, otherwise a player
// watching a friend in another system while their own craft executes a
// planted node loses every on-screen trace of why warp just clamped to
// 10x. Migrated onto buildEngineBox, which (unlike the retired VESSEL
// chip) has no camera-visibility placeholder branch at all, it always
// reads the active craft's own state, so this only re-confirms the
// badge itself is CraftVisibleHere-blind, not the placeholder text.
func TestVesselChipBurnBadgeOtherSystem(t *testing.T) {
	v := NewOrbitView(plainThemeColored())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}

	if len(w.Systems) < 2 {
		t.Skip("need a second loaded system to browse away from the craft's own")
	}
	w.CycleSystem()
	if w.CraftVisibleHere() {
		t.Fatal("test setup: craft is still visible after CycleSystem")
	}

	out := strings.Join(v.buildEngineBox(w), "\n")
	if !strings.Contains(out, "BURN") {
		t.Errorf("ENGINE box should still badge for a burning craft in a system the camera isn't showing:\n%s", out)
	}
}

// TestLaunchViewVesselChipBurnBadge — decision 2's Launch View parity,
// via the shared hudSource: the pad canvas's VESSEL chip carries the
// same badge the map does, since LaunchView composites its side HUD
// chips from the same OrbitView.
func TestLaunchViewVesselChipBurnBadge(t *testing.T) {
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
		t.Errorf("idle launch view should not carry a BURN badge:\n%s", out)
	}

	c.Landed = false
	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	out = lv.Render(w, 140, 40)
	if !strings.Contains(out, "BURN") {
		t.Errorf("firing launch view missing the BURN badge:\n%s", out)
	}
}

// TestTargetChipRecomputesDuringOwnBurn — decision 3: while the active
// craft has a live ActiveBurn, the predicted-encounter cell that would
// otherwise carry a fresh approach/TCA reading instead reads
// "recomputing…"; the live rows above it (range:, Δincl:, lead:) are
// unaffected. Migrated onto buildTargetBox (ADR 0051): decision 2 keeps
// every row/label always present now, so there is no longer a single
// collapsing "encounter:" row, only the affected cell's VALUE changes.
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

	out := strings.Join(v.buildTargetBox(w), "\n")
	if strings.Contains(out, "recomputing") {
		t.Errorf("no-burn TARGET box should not read recomputing:\n%s", out)
	}
	if !strings.Contains(out, "range:") {
		t.Errorf("body-target box missing range: row:\n%s", out)
	}

	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	out = strings.Join(v.buildTargetBox(w), "\n")
	if !strings.Contains(out, "approach:") || !strings.Contains(out, "recomputing") {
		t.Errorf("mid-burn body-target box missing 'approach: recomputing…':\n%s", out)
	}
	if !strings.Contains(out, "range:") {
		t.Errorf("mid-burn body-target box lost its live range: row:\n%s", out)
	}
}

// TestTargetChipCraftRecomputesDuringOwnBurn — decision 3's craft-target
// branch: closestApproachCells' TCA:/approach: pair is replaced the same
// way, on the TCA: cell.
func TestTargetChipCraftRecomputesDuringOwnBurn(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := leadTestWorld(t, 82)
	c := w.ActiveCraft()

	out := strings.Join(v.buildTargetBox(w), "\n")
	if strings.Contains(out, "recomputing") {
		t.Errorf("no-burn craft-target box should not read recomputing:\n%s", out)
	}

	c.ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * time.Second)}
	out = strings.Join(v.buildTargetBox(w), "\n")
	if !strings.Contains(out, "TCA:") || !strings.Contains(out, "recomputing") {
		t.Errorf("mid-burn craft-target box missing 'TCA: recomputing…':\n%s", out)
	}
	if !strings.Contains(out, "closing:") || !strings.Contains(out, "lead:") {
		t.Errorf("mid-burn craft-target box lost its live relative-state rows:\n%s", out)
	}
}

// TestTargetChipEllipsisCellWidthConsistent guards the same box →
// canvas width contract assertChipCellWidthConsistent exists for
// (orbit_chips_test.go): the "recomputing…" cell's ellipsis must measure
// the same in lipgloss.Width as it does spliced per-cell, or the box's
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
	lines := v.buildTargetBox(w)
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

	out := strings.Join(v.buildEngineBox(w), "\n")
	if !strings.Contains(out, "ignition in 30s") {
		t.Errorf("waiting node row should read 'ignition in 30s' (BurnStart, not TriggerTime):\n%s", out)
	}
	if strings.Contains(out, "T-66s") || strings.Contains(out, "T+66s") {
		t.Errorf("waiting node row still counts to TriggerTime:\n%s", out)
	}

	c.Nodes = nil
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 3054,
		EndTime:     w.Clock.SimTime.Add(61 * time.Second),
	}
	out = strings.Join(v.buildEngineBox(w), "\n")
	if !strings.Contains(out, "burning, 1m01s left") {
		t.Errorf("live-burn node row should read 'burning, 1m01s left' (BurnEnd):\n%s", out)
	}
	if strings.Contains(out, "T-61s") {
		t.Errorf("live-burn node row still uses the old T-Ns wording:\n%s", out)
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

	out := strings.Join(v.buildEngineBox(w), "\n")
	if !strings.Contains(out, "ignition in 0s") {
		t.Errorf("overdue-but-unfired node row should clamp to 'ignition in 0s':\n%s", out)
	}
	if strings.Contains(out, "ignition in -") {
		t.Errorf("node row printed a raw negative duration:\n%s", out)
	}
}

// (TestNodesChipHeadRowSaysAfterBurnWhenHeldBehindLiveBurn retired: the
// retired NODES chip special-cased a queued node held behind THIS
// craft's own live burn with a distinct "ignition after burn" wording
// (#447 review finding 7). ADR 0051 decision 12's engineNodeLine drops
// that special case: engineBurnLine is checked first and returns early
// whenever ActiveBurn is live, so the ENGINE box's node row always shows
// the live burn line in that state, the queued node never reaches its
// own line at all while the engine is already firing. There is no
// "held" state left to word distinctly; the burning line already tells
// the player the engine is busy.)

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
